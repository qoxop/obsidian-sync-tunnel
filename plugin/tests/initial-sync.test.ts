import { describe, expect, it } from "vitest";

import type { SyncApiClient } from "../src/api-client";
import { buildInitialSyncPreview } from "../src/initial-sync";
import type { LocalFile } from "../src/types";
import type { VaultScanner } from "../src/vault-scanner";

describe("buildInitialSyncPreview", () => {
  it("classifies equal, different, local-only, remote-only, and deleted paths", async () => {
    const local = new Map<string, LocalFile>([
      ["same.md", localFile("same.md", "same")],
      ["different.md", localFile("different.md", "local")],
      ["local-only.md", localFile("local-only.md", "local-only")],
      ["deleted-remotely.md", localFile("deleted-remotely.md", "local")]
    ]);
    const scanner = {
      scan: async () => local,
      isExcluded: (path: string) => path === "excluded.md"
    } as unknown as VaultScanner;
    const client = {
      listSnapshot: async () => ({
        files: [
          change("same.md", "same"),
          change("different.md", "remote"),
          change("remote-only.md", "remote-only"),
          change("deleted-remotely.md", "", true),
          change("excluded.md", "excluded")
        ],
        snapshot_revision: 10,
        cursor: "remote-only.md",
        has_more: false
      })
    } as unknown as SyncApiClient;

    const preview = await buildInitialSyncPreview(scanner, client);

    expect(preview).toEqual({
      localFiles: 4,
      remoteFiles: 3,
      same: 1,
      different: 1,
      localOnly: 1,
      remoteOnly: 1,
      localAgainstRemoteDelete: 1,
      snapshotRevision: 10
    });
  });
});

function localFile(path: string, hash: string): LocalFile {
  return { path, hash, size: hash.length, modifiedAt: 1 };
}

function change(path: string, hash: string, deleted = false) {
  return {
    revision: 10,
    path,
    blob_hash: deleted ? undefined : hash,
    size: deleted ? 0 : hash.length,
    modified_at: 1,
    deleted,
    device_id: "remote"
  };
}
