import type { SyncApiClient } from "./api-client";
import type { Change, InitialSyncPreview } from "./types";
import type { VaultScanner } from "./vault-scanner";

export async function buildInitialSyncPreview(scanner: VaultScanner, client: SyncApiClient): Promise<InitialSyncPreview> {
  const local = await scanner.scan();
  const remote = new Map<string, Change>();
  let snapshotRevision: number | undefined;
  let cursor = "";
  let hasMore = true;
  while (hasMore) {
    const page = await client.listSnapshot(snapshotRevision, cursor);
    if (snapshotRevision === undefined) {
      snapshotRevision = page.snapshot_revision;
    } else if (page.snapshot_revision !== snapshotRevision) {
      throw new Error("Server changed the snapshot revision while building the preview");
    }
    for (const change of page.files) {
      if (!scanner.isExcluded(change.path)) remote.set(change.path, change);
    }
    cursor = page.cursor;
    hasMore = page.has_more;
  }

  const preview: InitialSyncPreview = {
    localFiles: local.size,
    remoteFiles: 0,
    same: 0,
    different: 0,
    localOnly: 0,
    remoteOnly: 0,
    localAgainstRemoteDelete: 0,
    snapshotRevision: snapshotRevision ?? 0
  };
  for (const [path, localFile] of local) {
    const remoteFile = remote.get(path);
    if (!remoteFile) {
      preview.localOnly += 1;
    } else if (remoteFile.deleted) {
      preview.localAgainstRemoteDelete += 1;
    } else if (remoteFile.blob_hash === localFile.hash) {
      preview.same += 1;
    } else {
      preview.different += 1;
    }
  }
  for (const [path, remoteFile] of remote) {
    if (remoteFile.deleted) continue;
    preview.remoteFiles += 1;
    if (!local.has(path)) preview.remoteOnly += 1;
  }
  return preview;
}
