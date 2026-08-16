import { DataAdapter, Vault } from "obsidian";

import { ApiError, SyncApiClient } from "./api-client";
import { sha256 } from "./hash";
import { conflictPath, pathRequiresObsidianRestart } from "./path";
import { Change, FileState, InitialSyncMode, LocalFile, PersistedData, SyncSummary } from "./types";
import { VaultScanner } from "./vault-scanner";

const FULL_SCAN_INTERVAL_MS = 60 * 60 * 1000;
const INTEGRITY_SCAN_INTERVAL_MS = 24 * 60 * 60 * 1000;

export class SyncEngine {
  private running = false;
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
      restartRequired: false
    };
    try {
      const initialMode = this.data.initialSyncCompleted ? null : this.data.pendingInitialSyncMode;
      if (!this.data.initialSyncCompleted && !initialMode) throw new Error("Initial sync requires explicit confirmation");
      const status = await this.client.status();
      const filterFingerprint = this.scanner.filterFingerprint();
      const filterChanged = this.data.filterFingerprint !== filterFingerprint;
      if (filterChanged || initialMode) {
        await this.reconcileSnapshot(filterFingerprint, summary, initialMode);
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
      try {
        const result = await this.client.putFile(path, previous?.revision ?? 0, stat.mtime, currentHash, content);
        this.data.files[path] = stateFromChange(result.change);
        this.data.scanCache[path] = {
          hash: currentHash,
          size: content.byteLength,
          modifiedAt: stat.mtime
        };
        if (result.changed) summary.uploaded += 1;
        else summary.skipped += 1;
      } catch (error) {
        if (!(error instanceof ApiError) || error.code !== "revision_conflict" || !error.current) throw error;
        await this.preserveConflict(path, content);
        summary.conflicts += 1;
        await this.applyRemoteChange(error.current, summary, false);
      }
    }

    for (const [path, previous] of Object.entries(this.data.files)) {
      if (previous.deleted || localFiles.has(path) || this.scanner.isExcluded(path)) continue;
      if (deletionCandidates && !deletionCandidates.has(path)) continue;
      try {
        const result = await this.client.deleteFile(path, previous.revision, Date.now());
        this.data.files[path] = stateFromChange(result.change);
        if (result.changed) summary.deletedRemote += 1;
        else summary.skipped += 1;
      } catch (error) {
        if (!(error instanceof ApiError) || error.code !== "revision_conflict" || !error.current) throw error;
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
    const content = await this.client.downloadBlob(change.blob_hash);
    await this.ensureParentDirectory(change.path);
    await this.adapter.writeBinary(change.path, content, { ctime: change.modified_at, mtime: change.modified_at });
    this.data.files[change.path] = stateFromChange(change);
    this.data.scanCache[change.path] = {
      hash: change.blob_hash,
      size: change.size,
      modifiedAt: change.modified_at
    };
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
