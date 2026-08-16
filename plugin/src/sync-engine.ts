import { DataAdapter, FileSystemAdapter, Platform, Vault } from "obsidian";

import { ApiError, SyncApiClient } from "./api-client";
import { sha256 } from "./hash";
import { conflictPath, pathRequiresObsidianRestart } from "./path";
import { BatchDeleteItem, BatchMutationResponse, Change, ChunkRef, FileState, InboxDownload, InitialSyncMode, LocalFile, MutationResponse, OutboxOperation, PersistedData, SyncSummary } from "./types";
import { VaultScanner } from "./vault-scanner";

const FULL_SCAN_INTERVAL_MS = 60 * 60 * 1000;
const INTEGRITY_SCAN_INTERVAL_MS = 24 * 60 * 60 * 1000;

export class SyncEngine {
  private running = false;
  private supportsOperationIDs = false;
  private supportsChunkUpload = false;
  private supportsChunkDownload = false;
  private supportsRename = false;
  private supportsBatchDelete = false;
  private chunkSize = 4 * 1024 * 1024;
  private chunkConcurrency = 3;
  private maxChunkQuery = 1000;
  private readonly deferredRemotePaths = new Set<string>();
  private readonly adapter: DataAdapter;

  constructor(
    private readonly vault: Vault,
    private readonly data: PersistedData,
    private readonly client: SyncApiClient,
    private readonly scanner: VaultScanner,
    private readonly persist: () => Promise<void>
  ) {
    this.adapter = vault.adapter;
  }

  async run(): Promise<SyncSummary> {
    if (this.running) throw new Error("A sync is already running");
    this.running = true;
    const summary: SyncSummary = {
      uploaded: 0,
      downloaded: 0,
      deletedRemote: 0,
      deletedLocal: 0,
      conflicts: 0,
      skipped: 0,
      restartRequired: false,
      renamed: 0
    };
    try {
      const initialMode = this.data.initialSyncCompleted ? null : this.data.pendingInitialSyncMode;
      if (!this.data.initialSyncCompleted && !initialMode) throw new Error("Initial sync requires explicit confirmation");
      await this.resumeInbox(summary);
      const serverInfo = await this.client.serverInfo();
      this.supportsOperationIDs = serverInfo.capabilities.includes("operation-id");
      this.supportsChunkUpload = this.supportsOperationIDs && serverInfo.capabilities.includes("chunk-upload-v1");
      this.supportsChunkDownload = serverInfo.capabilities.includes("chunk-download-v1");
      this.supportsRename = this.supportsOperationIDs && serverInfo.capabilities.includes("rename-v1");
      this.supportsBatchDelete = this.supportsOperationIDs && serverInfo.capabilities.includes("batch-delete-v1");
      this.chunkSize = positiveInteger(serverInfo.limits?.chunk_size, this.chunkSize);
      this.chunkConcurrency = Math.min(8, positiveInteger(serverInfo.limits?.chunk_concurrency, this.chunkConcurrency));
      this.maxChunkQuery = Math.min(1000, positiveInteger(serverInfo.limits?.max_chunk_query, this.maxChunkQuery));
      if (!this.supportsOperationIDs && Object.keys(this.data.outbox).length > 0) {
        throw new Error("服务器不支持 operation-id，无法安全恢复尚未确认的写入；请先重新升级服务端");
      }
      if (this.supportsOperationIDs) await this.resumeOutbox(summary);
      const status = await this.client.status();
      const filterFingerprint = this.scanner.filterFingerprint();
      const filterChanged = this.data.filterFingerprint !== filterFingerprint;
      if (filterChanged || initialMode) {
        await this.reconcileSnapshot(filterFingerprint, summary, initialMode);
      }
      if (this.supportsRename) {
        await this.processPendingRenames(summary);
      } else if (Object.keys(this.data.pendingRenames).length > 0) {
        this.data.pendingRenames = {};
        await this.persist();
      }
      const now = Date.now();
      const pendingPaths = { ...this.data.pendingPaths };
      const integrityScanDue = now - this.data.lastIntegrityScanAt >= INTEGRITY_SCAN_INTERVAL_MS;
      const fullScan = filterChanged
        || Boolean(initialMode)
        || this.data.needsFullScan
        || Object.keys(this.data.scanCache).length === 0
        || now - this.data.lastFullScanAt >= FULL_SCAN_INTERVAL_MS
        || integrityScanDue;
      const paths = fullScan ? undefined : Object.keys(pendingPaths);
      const localFiles = await this.scanner.scan({
        cache: this.data.scanCache,
        paths,
        // A queued adapter event is positive evidence that the file changed.
        // Hash it even when a tool preserved mtime and size.
        forceHash: integrityScanDue || !fullScan
      });
      this.updateScanCache(localFiles, fullScan, paths ?? []);
      await this.pushLocalChanges(
        localFiles,
        status.max_upload_bytes,
        summary,
        fullScan ? undefined : new Set(paths)
      );
      await this.pullRemoteChanges(summary);
      if (initialMode) {
        this.data.initialSyncCompleted = true;
        this.data.pendingInitialSyncMode = null;
      }
      if (fullScan) {
        this.data.needsFullScan = false;
        this.data.lastFullScanAt = now;
      }
      if (integrityScanDue) this.data.lastIntegrityScanAt = now;
      this.acknowledgePendingPaths(pendingPaths);
      await this.persist();
      return summary;
    } finally {
      this.running = false;
    }
  }

  private async reconcileSnapshot(
    filterFingerprint: string,
    summary: SyncSummary,
    initialMode: InitialSyncMode | null
  ): Promise<void> {
    let snapshotRevision: number | undefined;
    let cursor = "";
    let hasMore = true;
    const snapshot = new Map<string, Change>();
    while (hasMore) {
      const page = await this.client.listSnapshot(snapshotRevision, cursor);
      if (snapshotRevision === undefined) {
        snapshotRevision = page.snapshot_revision;
      } else if (page.snapshot_revision !== snapshotRevision) {
        throw new Error("Server changed the snapshot revision while paging");
      }
      for (const change of page.files) {
        if (!this.scanner.isExcluded(change.path)) snapshot.set(change.path, change);
      }
      cursor = page.cursor;
      hasMore = page.has_more;
    }

    if (initialMode === "remote") {
      await this.preserveLocalOnlyFiles(new Set(snapshot.keys()), summary);
    }
    for (const change of snapshot.values()) {
      const previous = this.data.files[change.path];
      if (previous && previous.revision >= change.revision) continue;
      if (initialMode === "local") {
        this.data.files[change.path] = stateFromChange(change);
      } else {
        await this.applyRemoteChange(change, summary);
      }
    }
    this.data.cursor = snapshotRevision ?? 0;
    this.data.filterFingerprint = filterFingerprint;
    await this.persist();
  }

  private async preserveLocalOnlyFiles(remotePaths: Set<string>, summary: SyncSummary): Promise<void> {
    const localFiles = await this.scanner.scan();
    for (const path of localFiles.keys()) {
      if (remotePaths.has(path)) continue;
      const content = await this.adapter.readBinary(path);
      await this.preserveConflict(path, content);
      await this.adapter.remove(path);
      summary.conflicts += 1;
      if (pathRequiresObsidianRestart(path, this.vault.configDir)) summary.restartRequired = true;
    }
  }

  private async pushLocalChanges(
    localFiles: Map<string, LocalFile>,
    maxUploadBytes: number,
    summary: SyncSummary,
    deletionCandidates?: Set<string>
  ): Promise<void> {
    for (const [path, local] of localFiles) {
      if (this.deferredRemotePaths.has(path)) continue;
      const previous = this.data.files[path];
      if (previous && !previous.deleted && previous.hash === local.hash) {
        previous.modifiedAt = local.modifiedAt;
        previous.size = local.size;
        summary.skipped += 1;
        continue;
      }
      if (local.size > maxUploadBytes) {
        throw new Error(`${path} is ${local.size} bytes; server limit is ${maxUploadBytes} bytes`);
      }
      const content = await this.adapter.readBinary(path);
      const currentHash = await sha256(content);
      const stat = await this.adapter.stat(path);
      if (!stat || stat.type !== "file") continue;
      const chunks = this.supportsChunkUpload && content.byteLength > this.chunkSize
        ? await this.buildChunkRefs(content)
        : undefined;
      const operationID = await this.stageOperation(
        "put",
        path,
        previous?.revision ?? 0,
        stat.mtime,
        currentHash,
        content.byteLength,
        chunks
      );
      try {
        const result = chunks && operationID
          ? await this.uploadChunkedFile(path, previous?.revision ?? 0, stat.mtime, currentHash, content, chunks, operationID)
          : await this.client.putFile(path, previous?.revision ?? 0, stat.mtime, currentHash, content, operationID);
        this.data.files[path] = stateFromChange(result.change);
        this.data.scanCache[path] = {
          hash: currentHash,
          size: content.byteLength,
          modifiedAt: stat.mtime
        };
        await this.completeOperation(operationID);
        if (result.changed) summary.uploaded += 1;
        else summary.skipped += 1;
      } catch (error) {
        if (!(error instanceof ApiError) || error.code !== "revision_conflict" || !error.current) throw error;
        await this.discardOperation(operationID);
        await this.preserveConflict(path, content);
        summary.conflicts += 1;
        await this.applyRemoteChange(error.current, summary, false);
      }
    }

    const deletions = Object.entries(this.data.files).filter(([path, previous]) => {
      if (previous.deleted || localFiles.has(path) || this.scanner.isExcluded(path)) return false;
      if (this.deferredRemotePaths.has(path)) return false;
      if (deletionCandidates && !deletionCandidates.has(path)) return false;
      return true;
    });
    if (this.supportsBatchDelete && deletions.length > 1) {
      const completed = await this.pushBatchDeletes(deletions, summary);
      if (completed) {
        await this.persist();
        return;
      }
    }
    for (const [path, previous] of deletions) {
      const modifiedAt = Date.now();
      const operationID = await this.stageOperation("delete", path, previous.revision, modifiedAt, "", 0);
      try {
        const result = await this.client.deleteFile(path, previous.revision, modifiedAt, operationID);
        this.data.files[path] = stateFromChange(result.change);
        await this.completeOperation(operationID);
        if (result.changed) summary.deletedRemote += 1;
        else summary.skipped += 1;
      } catch (error) {
        if (!(error instanceof ApiError) || error.code !== "revision_conflict" || !error.current) throw error;
        await this.discardOperation(operationID);
        summary.conflicts += 1;
        await this.applyRemoteChange(error.current, summary);
      }
    }
    await this.persist();
  }

  private async pullRemoteChanges(summary: SyncSummary): Promise<void> {
    let hasMore = true;
    while (hasMore) {
      const page = await this.client.listChanges(this.data.cursor);
      for (const change of page.changes) {
        if (this.scanner.isExcluded(change.path)) continue;
        const previous = this.data.files[change.path];
        if (previous && previous.revision >= change.revision) continue;
        await this.applyRemoteChange(change, summary);
      }
      this.data.cursor = page.cursor;
      hasMore = page.has_more;
      await this.persist();
    }
  }

  private async applyRemoteChange(change: Change, summary: SyncSummary, preserveConcurrentLocal = true): Promise<void> {
    const previous = this.data.files[change.path];
    const exists = await this.adapter.exists(change.path);
    if (exists) {
      const currentData = await this.adapter.readBinary(change.path);
      const currentHash = await sha256(currentData);
      const remoteHash = change.deleted ? "" : change.blob_hash ?? "";
      const locallyModified = previous
        ? !previous.deleted && currentHash !== previous.hash && currentHash !== remoteHash
        : currentHash !== remoteHash;
      if (preserveConcurrentLocal && locallyModified) {
        await this.preserveConflict(change.path, currentData);
        summary.conflicts += 1;
      }
      if (!change.deleted && currentHash === remoteHash) {
        this.data.files[change.path] = stateFromChange(change);
        summary.skipped += 1;
        return;
      }
    }

    if (change.deleted) {
      if (exists) {
        await this.adapter.remove(change.path);
        summary.deletedLocal += 1;
        if (pathRequiresObsidianRestart(change.path, this.vault.configDir)) summary.restartRequired = true;
      }
      this.data.files[change.path] = stateFromChange(change);
      delete this.data.scanCache[change.path];
      return;
    }

    if (!change.blob_hash) throw new Error(`Server change ${change.revision} has no blob hash`);
    const download = await this.stageDownload(change);
    await this.downloadToTemporaryFile(change.blob_hash, change.size, download);
    if (!await this.fileMatches(download.tempPath, download.hash, download.size)) {
      throw new Error(`Temporary download verification failed for ${change.path}`);
    }
    download.stage = "verified";
    await this.persist();
    await this.installDownload(download);
    await this.finishDownload(download);
    summary.downloaded += 1;
    if (pathRequiresObsidianRestart(change.path, this.vault.configDir)) summary.restartRequired = true;
  }

  private async preserveConflict(originalPath: string, content: ArrayBuffer): Promise<string> {
    const base = conflictPath(originalPath, this.data.settings.deviceId);
    let destination = base;
    let counter = 2;
    while (await this.adapter.exists(destination)) {
      const dot = base.lastIndexOf(".");
      destination = dot > base.lastIndexOf("/") ? `${base.slice(0, dot)}-${counter}${base.slice(dot)}` : `${base}-${counter}`;
      counter += 1;
    }
    await this.ensureParentDirectory(destination);
    await this.adapter.writeBinary(destination, content);
    return destination;
  }

  private async ensureParentDirectory(path: string): Promise<void> {
    const parts = path.split("/");
    parts.pop();
    let current = "";
    for (const part of parts) {
      current = current ? `${current}/${part}` : part ?? "";
      if (current && !(await this.adapter.exists(current))) await this.adapter.mkdir(current);
    }
  }

  private updateScanCache(localFiles: Map<string, LocalFile>, fullScan: boolean, scannedPaths: string[]): void {
    if (fullScan) this.data.scanCache = {};
    for (const path of scannedPaths) delete this.data.scanCache[path];
    for (const [path, local] of localFiles) {
      this.data.scanCache[path] = {
        hash: local.hash,
        size: local.size,
        modifiedAt: local.modifiedAt
      };
    }
  }

  private acknowledgePendingPaths(snapshot: Record<string, number>): void {
    for (const [path, queuedAt] of Object.entries(snapshot)) {
      if (this.data.pendingPaths[path] === queuedAt) delete this.data.pendingPaths[path];
    }
  }

  private async stageOperation(
    kind: OutboxOperation["kind"],
    path: string,
    baseRevision: number,
    modifiedAt: number,
    hash: string,
    size: number,
    chunks?: ChunkRef[],
    sourcePath?: string,
    batchDeletes?: BatchDeleteItem[]
  ): Promise<string | undefined> {
    if (!this.supportsOperationIDs) return undefined;
    const operationId = crypto.randomUUID();
    this.data.outbox[operationId] = {
      operationId,
      kind,
      path,
      baseRevision,
      modifiedAt,
      hash,
      size,
      createdAt: Date.now(),
      transport: chunks ? "chunks" : "whole",
      chunks,
      sourcePath,
      batchDeletes
    };
    await this.persist();
    return operationId;
  }

  private async completeOperation(operationID: string | undefined): Promise<void> {
    if (!operationID) return;
    // Save the returned file revision while the outbox entry still exists. If
    // this save or the process fails, the next run can query the same operation.
    await this.persist();
    delete this.data.outbox[operationID];
  }

  private async discardOperation(operationID: string | undefined): Promise<void> {
    if (!operationID) return;
    delete this.data.outbox[operationID];
    await this.persist();
  }

  private async resumeOutbox(summary: SyncSummary): Promise<void> {
    const operations = Object.values(this.data.outbox).sort((left, right) => left.createdAt - right.createdAt);
    for (const operation of operations) {
      const committed = await this.client.findOperation(operation.operationId);
      if (committed) {
        if (operation.kind === "rename") {
          await this.acceptRecoveredRename(operation, committed, summary);
          continue;
        }
        if (operation.kind === "batch-delete") {
          await this.acceptRecoveredBatchDelete(operation, committed, summary);
          continue;
        }
        await this.acceptRecoveredOperation(operation, committed.change, committed.changed, summary);
        continue;
      }
      if (operation.kind === "rename") {
        await this.resumeRenameOperation(operation, summary);
        continue;
      }
      if (operation.kind === "batch-delete") {
        await this.resumeBatchDeleteOperation(operation, summary);
        continue;
      }
      if (this.scanner.isExcluded(operation.path)) {
        await this.abandonUncommittedOperation(operation);
        continue;
      }
      const exists = await this.adapter.exists(operation.path);
      if (operation.kind === "put") {
        if (!exists) {
          await this.abandonUncommittedOperation(operation);
          continue;
        }
        const content = await this.adapter.readBinary(operation.path);
        if (content.byteLength !== operation.size || await sha256(content) !== operation.hash) {
          await this.abandonUncommittedOperation(operation);
          continue;
        }
        try {
          const chunks = operation.transport === "chunks"
            ? await this.buildChunkRefs(content)
            : undefined;
          const result = chunks && this.supportsChunkUpload
            ? await this.uploadChunkedFile(
              operation.path,
              operation.baseRevision,
              operation.modifiedAt,
              operation.hash,
              content,
              chunks,
              operation.operationId
            )
            : await this.client.putFile(
              operation.path,
              operation.baseRevision,
              operation.modifiedAt,
              operation.hash,
              content,
              operation.operationId
            );
          await this.acceptRecoveredOperation(operation, result.change, result.changed, summary);
        } catch (error) {
          if (!(error instanceof ApiError) || error.code !== "revision_conflict" || !error.current) throw error;
          await this.discardOperation(operation.operationId);
          await this.preserveConflict(operation.path, content);
          summary.conflicts += 1;
          await this.applyRemoteChange(error.current, summary, false);
        }
        continue;
      }
      if (exists) {
        await this.abandonUncommittedOperation(operation);
        continue;
      }
      try {
        const result = await this.client.deleteFile(
          operation.path,
          operation.baseRevision,
          operation.modifiedAt,
          operation.operationId
        );
        await this.acceptRecoveredOperation(operation, result.change, result.changed, summary);
      } catch (error) {
        if (!(error instanceof ApiError) || error.code !== "revision_conflict" || !error.current) throw error;
        await this.discardOperation(operation.operationId);
        summary.conflicts += 1;
        await this.applyRemoteChange(error.current, summary);
      }
    }
  }

  private async acceptRecoveredOperation(
    operation: OutboxOperation,
    change: Change,
    changed: boolean,
    summary: SyncSummary
  ): Promise<void> {
    this.data.files[operation.path] = stateFromChange(change);
    if (change.deleted) delete this.data.scanCache[operation.path];
    this.queuePath(operation.path);
    if (changed) {
      if (operation.kind === "put") summary.uploaded += 1;
      else summary.deletedRemote += 1;
    } else {
      summary.skipped += 1;
    }
    await this.completeOperation(operation.operationId);
    await this.persist();
  }

  private async abandonUncommittedOperation(operation: OutboxOperation): Promise<void> {
    delete this.data.outbox[operation.operationId];
    this.queuePath(operation.path);
    if (operation.sourcePath) this.queuePath(operation.sourcePath);
    await this.persist();
  }

  private queuePath(path: string): void {
    this.data.pendingPaths[path] = Math.max(Date.now(), (this.data.pendingPaths[path] ?? 0) + 1);
  }

  private async pushBatchDeletes(
    deletions: Array<[string, FileState]>,
    summary: SyncSummary
  ): Promise<boolean> {
    const now = Date.now();
    const items: BatchDeleteItem[] = deletions.slice(0, 100).map(([path, previous]) => ({
      path,
      base_revision: previous.revision,
      modified_at: now
    }));
    const first = items[0];
    if (!first) return false;
    const operationId = await this.stageOperation(
      "batch-delete",
      first.path,
      first.base_revision,
      now,
      "",
      0,
      undefined,
      undefined,
      items
    );
    if (!operationId) return false;
    try {
      const result = await this.client.deleteFiles(items, operationId);
      await this.acceptBatchDeleteResult(operationId, items, result, summary);
      // More than 100 candidates are handled by the ordinary loop below.
      for (const item of items) {
        const index = deletions.findIndex(([path]) => path === item.path);
        if (index >= 0) deletions.splice(index, 1);
      }
      return deletions.length === 0;
    } catch (error) {
      if (!(error instanceof ApiError) || error.code !== "revision_conflict") throw error;
      await this.discardOperation(operationId);
      return false;
    }
  }

  private async resumeBatchDeleteOperation(operation: OutboxOperation, summary: SyncSummary): Promise<void> {
    const items = operation.batchDeletes;
    if (!items || items.length === 0) {
      await this.abandonUncommittedOperation(operation);
      return;
    }
    for (const item of items) {
      if (await this.adapter.exists(item.path)) {
        await this.abandonBatchDeleteOperation(operation, false);
        return;
      }
    }
    try {
      const result = await this.client.deleteFiles(items, operation.operationId);
      await this.acceptBatchDeleteResult(operation.operationId, items, result, summary);
    } catch (error) {
      if (!(error instanceof ApiError) || error.code !== "revision_conflict") throw error;
      await this.abandonBatchDeleteOperation(operation, true);
    }
  }

  private async acceptRecoveredBatchDelete(
    operation: OutboxOperation,
    result: MutationResponse,
    summary: SyncSummary
  ): Promise<void> {
    if (!result || !operation.batchDeletes) throw new Error("Recovered batch delete result is incomplete");
    await this.acceptBatchDeleteResult(operation.operationId, operation.batchDeletes, {
      changes: [result.change, ...(result.related_changes ?? [])],
      changed: result.changed
    }, summary);
  }

  private async acceptBatchDeleteResult(
    operationId: string,
    items: BatchDeleteItem[],
    result: BatchMutationResponse,
    summary: SyncSummary
  ): Promise<void> {
    const expected = new Set(items.map((item) => item.path));
    if (result.changes.length !== expected.size || result.changes.some((change) => !change.deleted || !expected.has(change.path))) {
      throw new Error("Server returned an incomplete batch delete result");
    }
    for (const change of result.changes) {
      this.data.files[change.path] = stateFromChange(change);
      delete this.data.scanCache[change.path];
    }
    if (result.changed) summary.deletedRemote += result.changes.length;
    else summary.skipped += result.changes.length;
    await this.completeOperation(operationId);
    await this.persist();
  }

  private async abandonBatchDeleteOperation(operation: OutboxOperation, deferRemote: boolean): Promise<void> {
    delete this.data.outbox[operation.operationId];
    for (const item of operation.batchDeletes ?? []) {
      this.queuePath(item.path);
      if (deferRemote) this.deferredRemotePaths.add(item.path);
    }
    await this.persist();
  }

  private async processPendingRenames(summary: SyncSummary): Promise<void> {
    const renames = Object.values(this.data.pendingRenames).sort((left, right) => left.queuedAt - right.queuedAt);
    for (const rename of renames) {
      const previous = this.data.files[rename.from];
      const sourceExists = await this.adapter.exists(rename.from);
      const destinationExists = await this.adapter.exists(rename.to);
      if (!previous || previous.deleted || sourceExists || !destinationExists) {
        delete this.data.pendingRenames[rename.renameId];
        continue;
      }
      const content = await this.adapter.readBinary(rename.to);
      if (await sha256(content) !== previous.hash) {
        delete this.data.pendingRenames[rename.renameId];
        continue;
      }
      const stat = await this.adapter.stat(rename.to);
      if (!stat || stat.type !== "file") {
        delete this.data.pendingRenames[rename.renameId];
        continue;
      }
      const operationId = await this.stageOperation(
        "rename",
        rename.to,
        previous.revision,
        stat.mtime,
        previous.hash,
        content.byteLength,
        undefined,
        rename.from
      );
      if (!operationId) continue;
      try {
        const result = await this.client.renameFile(rename.from, rename.to, previous.revision, stat.mtime, operationId);
        await this.acceptRenameResult(rename.renameId, operationId, rename.from, rename.to, result, summary);
      } catch (error) {
        if (!(error instanceof ApiError) || error.code !== "revision_conflict") throw error;
        await this.discardOperation(operationId);
        delete this.data.pendingRenames[rename.renameId];
      }
    }
    await this.persist();
  }

  private async resumeRenameOperation(operation: OutboxOperation, summary: SyncSummary): Promise<void> {
    const sourcePath = operation.sourcePath;
    if (!sourcePath || await this.adapter.exists(sourcePath) || !await this.adapter.exists(operation.path)) {
      await this.abandonUncommittedOperation(operation);
      return;
    }
    const content = await this.adapter.readBinary(operation.path);
    if (content.byteLength !== operation.size || await sha256(content) !== operation.hash) {
      await this.abandonUncommittedOperation(operation);
      return;
    }
    try {
      const result = await this.client.renameFile(
        sourcePath,
        operation.path,
        operation.baseRevision,
        operation.modifiedAt,
        operation.operationId
      );
      await this.acceptRecoveredRename(operation, result, summary);
    } catch (error) {
      if (!(error instanceof ApiError) || error.code !== "revision_conflict") throw error;
      await this.discardOperation(operation.operationId);
      this.queuePath(sourcePath);
      this.queuePath(operation.path);
      await this.persist();
    }
  }

  private async acceptRecoveredRename(operation: OutboxOperation, result: Awaited<ReturnType<SyncApiClient["renameFile"]>>, summary: SyncSummary): Promise<void> {
    if (!operation.sourcePath) throw new Error("Recovered rename operation has no source path");
    await this.acceptRenameResult(undefined, operation.operationId, operation.sourcePath, operation.path, result, summary);
  }

  private async acceptRenameResult(
    renameId: string | undefined,
    operationId: string,
    sourcePath: string,
    destinationPath: string,
    result: Awaited<ReturnType<SyncApiClient["renameFile"]>>,
    summary: SyncSummary
  ): Promise<void> {
    const sourceChange = result.related_changes?.find((change) => change.path === sourcePath && change.deleted);
    if (!sourceChange || result.change.path !== destinationPath) {
      throw new Error("Server returned an incomplete rename result");
    }
    this.data.files[sourcePath] = stateFromChange(sourceChange);
    this.data.files[destinationPath] = stateFromChange(result.change);
    delete this.data.scanCache[sourcePath];
    const stat = await this.adapter.stat(destinationPath);
    if (stat?.type === "file") {
      this.data.scanCache[destinationPath] = {
        hash: result.change.blob_hash ?? "",
        size: stat.size,
        modifiedAt: stat.mtime
      };
    }
    delete this.data.pendingPaths[sourcePath];
    delete this.data.pendingPaths[destinationPath];
    if (renameId) delete this.data.pendingRenames[renameId];
    summary.renamed += result.changed ? 1 : 0;
    if (!result.changed) summary.skipped += 1;
    await this.completeOperation(operationId);
    await this.persist();
  }

  private async buildChunkRefs(content: ArrayBuffer): Promise<ChunkRef[]> {
    const chunks: ChunkRef[] = [];
    for (let offset = 0; offset < content.byteLength; offset += this.chunkSize) {
      const data = content.slice(offset, Math.min(content.byteLength, offset + this.chunkSize));
      chunks.push({ hash: await sha256(data), size: data.byteLength });
    }
    return chunks;
  }

  private async uploadChunkedFile(
    path: string,
    baseRevision: number,
    modifiedAt: number,
    hash: string,
    content: ArrayBuffer,
    chunks: ChunkRef[],
    operationId: string
  ) {
    const locations = new Map<string, { offset: number; size: number }>();
    let offset = 0;
    for (const chunk of chunks) {
      if (!locations.has(chunk.hash)) locations.set(chunk.hash, { offset, size: chunk.size });
      offset += chunk.size;
    }
    if (offset !== content.byteLength) throw new Error(`Chunk manifest size mismatch for ${path}`);
    const hashes = [...locations.keys()];
    const missing: string[] = [];
    for (let index = 0; index < hashes.length; index += this.maxChunkQuery) {
      const requested = hashes.slice(index, index + this.maxChunkQuery);
      const response = await this.client.missingChunks(requested);
      const requestedSet = new Set(requested);
      for (const missingHash of response) {
        if (!requestedSet.has(missingHash)) throw new Error("Server returned an unknown missing Chunk hash");
        missing.push(missingHash);
      }
    }
    let next = 0;
    const uploadNext = async (): Promise<void> => {
      while (next < missing.length) {
        const hashToUpload = missing[next];
        next += 1;
        if (!hashToUpload) continue;
        const location = locations.get(hashToUpload);
        if (!location) throw new Error("Missing Chunk has no local location");
        const data = content.slice(location.offset, location.offset + location.size);
        if (await sha256(data) !== hashToUpload) throw new Error("Local Chunk changed before upload");
        await this.client.putChunk(hashToUpload, data);
      }
    };
    await Promise.all(Array.from(
      { length: Math.min(this.chunkConcurrency, Math.max(1, missing.length)) },
      () => uploadNext()
    ));
    return this.client.commitManifest(path, baseRevision, modifiedAt, hash, content.byteLength, chunks, operationId);
  }

  private async downloadToTemporaryFile(hash: string, expectedSize: number, download: InboxDownload): Promise<void> {
    const manifest = this.supportsChunkDownload && expectedSize > this.chunkSize
      ? await this.client.findManifest(hash)
      : null;
    if (manifest && Platform.isDesktopApp && this.adapter instanceof FileSystemAdapter) {
      await this.streamManifestToDesktopFile(hash, expectedSize, manifest, download);
      return;
    }
    const content = manifest
      ? await this.assembleManifestInMemory(expectedSize, manifest)
      : await this.client.downloadBlob(hash);
    if (content.byteLength !== expectedSize || await sha256(content) !== hash) {
      throw new Error(`Downloaded content verification failed for ${download.path}`);
    }
    await this.adapter.writeBinary(download.tempPath, content, { ctime: download.modifiedAt, mtime: download.modifiedAt });
  }

  private async assembleManifestInMemory(
    expectedSize: number,
    manifest: { size: number; chunks: ChunkRef[] }
  ): Promise<ArrayBuffer> {
    this.validateManifest(expectedSize, manifest);
    const result = new Uint8Array(expectedSize);
    let offset = 0;
    for (const chunk of manifest.chunks) {
      const data = await this.downloadVerifiedChunk(chunk);
      result.set(new Uint8Array(data), offset);
      offset += chunk.size;
    }
    return result.buffer;
  }

  private async streamManifestToDesktopFile(
    expectedHash: string,
    expectedSize: number,
    manifest: { size: number; chunks: ChunkRef[] },
    download: InboxDownload
  ): Promise<void> {
    this.validateManifest(expectedSize, manifest);
    const adapter = this.adapter;
    if (!(adapter instanceof FileSystemAdapter)) throw new Error("Desktop streaming requires a filesystem adapter");
    const [{ open }, { createHash }] = await Promise.all([
      import("node:fs/promises"),
      import("node:crypto")
    ]);
    const handle = await open(adapter.getFullPath(download.tempPath), "w", 0o600);
    const digest = createHash("sha256");
    let written = 0;
    try {
      for (const chunk of manifest.chunks) {
        const data = await this.downloadVerifiedChunk(chunk);
        const bytes = new Uint8Array(data);
        let chunkOffset = 0;
        while (chunkOffset < bytes.byteLength) {
          const result = await handle.write(bytes, chunkOffset, bytes.byteLength - chunkOffset);
          if (result.bytesWritten < 1) throw new Error("Could not make progress while writing a downloaded Chunk");
          chunkOffset += result.bytesWritten;
        }
        digest.update(bytes);
        written += bytes.byteLength;
      }
      const modified = new Date(download.modifiedAt);
      await handle.utimes(modified, modified);
      await handle.sync();
    } finally {
      await handle.close();
    }
    if (written !== expectedSize || digest.digest("hex") !== expectedHash) {
      throw new Error(`Streamed download verification failed for ${download.path}`);
    }
  }

  private validateManifest(expectedSize: number, manifest: { size: number; chunks: ChunkRef[] }): void {
    if (manifest.size !== expectedSize) throw new Error("Server Manifest size does not match the file change");
    if (manifest.chunks.length !== Math.ceil(expectedSize / this.chunkSize)) {
      throw new Error("Server Manifest has an invalid Chunk count");
    }
    let offset = 0;
    for (const chunk of manifest.chunks) {
      if (chunk.size < 1 || chunk.size > this.chunkSize || offset + chunk.size > expectedSize) {
        throw new Error("Server returned an invalid Chunk Manifest");
      }
      if (offset + chunk.size < expectedSize && chunk.size !== this.chunkSize) {
        throw new Error("Server returned a non-fixed intermediate Chunk");
      }
      offset += chunk.size;
    }
    if (offset !== expectedSize) throw new Error("Chunk Manifest does not cover the file size");
  }

  private async downloadVerifiedChunk(chunk: ChunkRef): Promise<ArrayBuffer> {
    const data = await this.client.downloadChunk(chunk.hash);
    if (data.byteLength !== chunk.size || await sha256(data) !== chunk.hash) {
      throw new Error("Downloaded Chunk verification failed");
    }
    return data;
  }

  private async stageDownload(change: Change): Promise<InboxDownload> {
    if (!change.blob_hash) throw new Error(`Server change ${change.revision} has no blob hash`);
    const downloadId = crypto.randomUUID();
    const parts = change.path.split("/");
    parts.pop();
    const directory = parts.join("/");
    const prefix = directory ? `${directory}/` : "";
    const download: InboxDownload = {
      downloadId,
      path: change.path,
      revision: change.revision,
      hash: change.blob_hash,
      size: change.size,
      modifiedAt: change.modified_at,
      deviceId: change.device_id,
      tempPath: `${prefix}.sync-tunnel-download-${downloadId}.tmp`,
      backupPath: `${prefix}.sync-tunnel-backup-${downloadId}.tmp`,
      stage: "downloading",
      createdAt: Date.now()
    };
    await this.ensureParentDirectory(change.path);
    this.data.inbox[downloadId] = download;
    await this.persist();
    return download;
  }

  private async installDownload(download: InboxDownload): Promise<void> {
    download.stage = "replacing";
    await this.persist();
    const targetExists = await this.adapter.exists(download.path);
    if (targetExists && !await this.adapter.exists(download.backupPath)) {
      await this.adapter.rename(download.path, download.backupPath);
    }
    try {
      if (!await this.adapter.exists(download.tempPath)) {
        throw new Error(`Temporary download is missing for ${download.path}`);
      }
      await this.adapter.rename(download.tempPath, download.path);
    } catch (error) {
      if (!await this.adapter.exists(download.path) && await this.adapter.exists(download.backupPath)) {
        await this.adapter.rename(download.backupPath, download.path);
      }
      throw error;
    }
    if (!await this.fileMatches(download.path, download.hash, download.size)) {
      if (await this.adapter.exists(download.path)) await this.adapter.remove(download.path);
      if (await this.adapter.exists(download.backupPath)) await this.adapter.rename(download.backupPath, download.path);
      throw new Error(`Installed download verification failed for ${download.path}`);
    }
    if (await this.adapter.exists(download.backupPath)) await this.adapter.remove(download.backupPath);
  }

  private async finishDownload(download: InboxDownload): Promise<void> {
    this.data.files[download.path] = stateFromChange({
      revision: download.revision,
      path: download.path,
      blob_hash: download.hash,
      size: download.size,
      modified_at: download.modifiedAt,
      deleted: false,
      device_id: download.deviceId
    });
    this.data.scanCache[download.path] = {
      hash: download.hash,
      size: download.size,
      modifiedAt: download.modifiedAt
    };
    await this.persist();
    delete this.data.inbox[download.downloadId];
    await this.persist();
  }

  private async resumeInbox(summary: SyncSummary): Promise<void> {
    const downloads = Object.values(this.data.inbox).sort((left, right) => left.createdAt - right.createdAt);
    for (const download of downloads) {
      if (await this.fileMatches(download.path, download.hash, download.size)) {
        await this.cleanupDownloadArtifacts(download);
        await this.finishDownload(download);
        summary.downloaded += 1;
        if (pathRequiresObsidianRestart(download.path, this.vault.configDir)) summary.restartRequired = true;
        continue;
      }
      if (await this.fileMatches(download.tempPath, download.hash, download.size)) {
        await this.installDownload(download);
        await this.finishDownload(download);
        summary.downloaded += 1;
        if (pathRequiresObsidianRestart(download.path, this.vault.configDir)) summary.restartRequired = true;
        continue;
      }
      if (!await this.adapter.exists(download.path) && await this.adapter.exists(download.backupPath)) {
        await this.adapter.rename(download.backupPath, download.path);
      }
      await this.cleanupDownloadArtifacts(download);
      delete this.data.inbox[download.downloadId];
      this.deferredRemotePaths.add(download.path);
      await this.persist();
    }
  }

  private async cleanupDownloadArtifacts(download: InboxDownload): Promise<void> {
    if (await this.adapter.exists(download.tempPath)) await this.adapter.remove(download.tempPath);
    if (await this.adapter.exists(download.backupPath)) await this.adapter.remove(download.backupPath);
  }

  private async fileMatches(path: string, expectedHash: string, expectedSize: number): Promise<boolean> {
    const stat = await this.adapter.stat(path);
    if (!stat || stat.type !== "file" || stat.size !== expectedSize) return false;
    return await this.hashLocalFile(path) === expectedHash;
  }

  private async hashLocalFile(path: string): Promise<string> {
    const adapter = this.adapter;
    if (!Platform.isDesktopApp || !(adapter instanceof FileSystemAdapter)) {
      return sha256(await this.adapter.readBinary(path));
    }
    const [{ createReadStream }, { createHash }] = await Promise.all([
      import("node:fs"),
      import("node:crypto")
    ]);
    const digest = createHash("sha256");
    for await (const chunk of createReadStream(adapter.getFullPath(path))) digest.update(chunk);
    return digest.digest("hex");
  }
}

function stateFromChange(change: Change): FileState {
  return {
    hash: change.blob_hash ?? "",
    revision: change.revision,
    size: change.size,
    modifiedAt: change.modified_at,
    deleted: change.deleted
  };
}

function positiveInteger(value: number | undefined, fallback: number): number {
  return typeof value === "number" && Number.isInteger(value) && value > 0 ? value : fallback;
}
