import { DataAdapter, Vault } from "obsidian";

import { ApiError, SyncApiClient } from "./api-client";
import { sha256 } from "./hash";
import { conflictPath } from "./path";
import { Change, FileState, LocalFile, PersistedData, SyncSummary } from "./types";
import { VaultScanner } from "./vault-scanner";

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
    const summary: SyncSummary = { uploaded: 0, downloaded: 0, deletedRemote: 0, deletedLocal: 0, conflicts: 0, skipped: 0 };
    try {
      const status = await this.client.status();
      const localFiles = await this.scanner.scan();
      await this.pushLocalChanges(localFiles, status.max_upload_bytes, summary);
      await this.pullRemoteChanges(summary);
      await this.persist();
      return summary;
    } finally {
      this.running = false;
    }
  }

  private async pushLocalChanges(localFiles: Map<string, LocalFile>, maxUploadBytes: number, summary: SyncSummary): Promise<void> {
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
    if (exists && previous) {
      const currentData = await this.adapter.readBinary(change.path);
      const currentHash = await sha256(currentData);
      const remoteHash = change.deleted ? "" : change.blob_hash ?? "";
      if (preserveConcurrentLocal && !previous.deleted && currentHash !== previous.hash && currentHash !== remoteHash) {
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
      }
      this.data.files[change.path] = stateFromChange(change);
      return;
    }

    if (!change.blob_hash) throw new Error(`Server change ${change.revision} has no blob hash`);
    const content = await this.client.downloadBlob(change.blob_hash);
    await this.ensureParentDirectory(change.path);
    await this.adapter.writeBinary(change.path, content, { ctime: change.modified_at, mtime: change.modified_at });
    this.data.files[change.path] = stateFromChange(change);
    summary.downloaded += 1;
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
