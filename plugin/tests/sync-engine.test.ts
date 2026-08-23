import type { DataAdapter, Vault } from "obsidian";
import { describe, expect, it } from "vitest";

import { ApiError, type SyncApiClient } from "../src/api-client";
import { sha256 } from "../src/hash";
import { SyncEngine } from "../src/sync-engine";
import type { Change, PersistedData } from "../src/types";
import { VaultScanner } from "../src/vault-scanner";

describe("SyncEngine snapshot reconciliation", () => {
  it("preserves an untracked local file before applying the remote snapshot", async () => {
    const localBytes = bytes("local version");
    const remoteBytes = bytes("remote version");
    const remoteHash = await sha256(remoteBytes);
    const files = new Map<string, ArrayBuffer>([["note.md", localBytes]]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const remoteChange: Change = {
      revision: 5,
      path: "note.md",
      blob_hash: remoteHash,
      size: remoteBytes.byteLength,
      modified_at: 1234,
      deleted: false,
      device_id: "remote-device"
    };
    let uploadedConflict = "";
    const client = {
      serverInfo: async () => finalServerInfo(),
      status: async () => ({ latest_revision: 5, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      listSnapshot: async () => ({
        files: [remoteChange],
        snapshot_revision: 5,
        cursor: "note.md",
        has_more: false
      }),
      downloadBlob: async (hash: string) => {
        expect(hash).toBe(remoteHash);
        return remoteBytes;
      },
      putFile: async (path: string, _baseRevision: number, modifiedAt: number, hash: string, content: ArrayBuffer) => {
        uploadedConflict = path;
        expect(text(content)).toBe("local version");
        return {
          changed: true,
          change: {
            revision: 6,
            path,
            blob_hash: hash,
            size: content.byteLength,
            modified_at: modifiedAt,
            deleted: false,
            device_id: "test-device"
          }
        };
      },
      deleteFile: async () => {
        throw new Error("Unexpected delete");
      },
      listChanges: async () => ({ changes: [], cursor: 6, has_more: false })
    } as unknown as SyncApiClient;
    const data: PersistedData = {
      schemaVersion: 4,
      settings: {
        serverUrl: "https://sync.example.com",
        vaultId: "test-vault",
        deviceId: "test-device",
        deviceName: "Test device",
        credentialSecretName: "token",
        accessClientId: "",
        accessClientSecretName: "",
        automaticSync: false,
        syncOnStartup: false,
        syncIntervalSeconds: 300,
        syncProfile: "full",
        excludedPatterns: [],
        language: "zh-CN",
        mobileMaxFileBytes: 32 * 1024 * 1024
      },
      cursor: 0,
      filterFingerprint: "old-filter",
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: {},
      scanCache: {},
      pendingPaths: {},
      needsFullScan: true,
      lastFullScanAt: 0,
      lastIntegrityScanAt: 0,
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    let persistCount = 0;
    const engine = new SyncEngine(vault, data, client, scanner, async () => {
      persistCount += 1;
    });

    const summary = await engine.run();

    expect(text(files.get("note.md"))).toBe("remote version");
    expect(uploadedConflict).toMatch(/^note\.conflict-test-device-\d{8}-\d{6}Z\.md$/u);
    expect(text(files.get(uploadedConflict))).toBe("local version");
    expect(summary.conflicts).toBe(1);
    expect(summary.downloaded).toBe(1);
    expect(summary.uploaded).toBe(1);
    expect(data.filterFingerprint).toBe(scanner.filterFingerprint());
    expect(data.cursor).toBe(6);
    expect(persistCount).toBeGreaterThan(0);
  });

  it("uses the local Vault as authority only after explicit initial approval", async () => {
    const localBytes = bytes("local authority");
    const remoteBytes = bytes("remote old version");
    const remoteOnlyBytes = bytes("remote only");
    const remoteHash = await sha256(remoteBytes);
    const remoteOnlyHash = await sha256(remoteOnlyBytes);
    const files = new Map<string, ArrayBuffer>([["note.md", localBytes]]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const snapshot: Change[] = [
      {
        revision: 4,
        path: "note.md",
        blob_hash: remoteHash,
        size: remoteBytes.byteLength,
        modified_at: 1,
        deleted: false,
        device_id: "remote"
      },
      {
        revision: 5,
        path: "remote-only.md",
        blob_hash: remoteOnlyHash,
        size: remoteOnlyBytes.byteLength,
        modified_at: 1,
        deleted: false,
        device_id: "remote"
      }
    ];
    let uploadedBase = 0;
    let deletedBase = 0;
    const client = {
      serverInfo: async () => finalServerInfo(),
      status: async () => ({ latest_revision: 5, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      listSnapshot: async () => ({ files: snapshot, snapshot_revision: 5, cursor: "remote-only.md", has_more: false }),
      putFile: async (path: string, baseRevision: number, modifiedAt: number, hash: string, content: ArrayBuffer) => {
        uploadedBase = baseRevision;
        return {
          changed: true,
          change: {
            revision: 6,
            path,
            blob_hash: hash,
            size: content.byteLength,
            modified_at: modifiedAt,
            deleted: false,
            device_id: "local"
          }
        };
      },
      deleteFile: async (path: string, baseRevision: number) => {
        deletedBase = baseRevision;
        return {
          changed: true,
          change: {
            revision: 7,
            path,
            size: 0,
            modified_at: 1,
            deleted: true,
            device_id: "local"
          }
        };
      },
      downloadBlob: async () => {
        throw new Error("Local-authoritative initialization must not download remote content");
      },
      listChanges: async () => ({ changes: [], cursor: 7, has_more: false })
    } as unknown as SyncApiClient;
    const data: PersistedData = {
      schemaVersion: 4,
      settings: testSettings(),
      cursor: 0,
      filterFingerprint: "",
      initialSyncCompleted: false,
      pendingInitialSyncMode: "local",
      files: {},
      scanCache: {},
      pendingPaths: {},
      needsFullScan: true,
      lastFullScanAt: 0,
      lastIntegrityScanAt: 0,
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    const engine = new SyncEngine(vault, data, client, scanner, async () => undefined);

    const summary = await engine.run();

    expect(text(files.get("note.md"))).toBe("local authority");
    expect(files.has("remote-only.md")).toBe(false);
    expect(uploadedBase).toBe(4);
    expect(deletedBase).toBe(5);
    expect(summary.uploaded).toBe(1);
    expect(summary.deletedRemote).toBe(1);
    expect(data.initialSyncCompleted).toBe(true);
    expect(data.pendingInitialSyncMode).toBeNull();
  });

  it("keeps local-only content as a conflict copy in remote-authoritative mode", async () => {
    const files = new Map<string, ArrayBuffer>([["local-only.md", bytes("keep me")]]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    let uploadedPath = "";
    const client = {
      serverInfo: async () => finalServerInfo(),
      status: async () => ({ latest_revision: 0, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      listSnapshot: async () => ({ files: [], snapshot_revision: 0, cursor: "", has_more: false }),
      putFile: async (path: string, _baseRevision: number, modifiedAt: number, hash: string, content: ArrayBuffer) => {
        uploadedPath = path;
        return {
          changed: true,
          change: {
            revision: 1,
            path,
            blob_hash: hash,
            size: content.byteLength,
            modified_at: modifiedAt,
            deleted: false,
            device_id: "local"
          }
        };
      },
      deleteFile: async () => {
        throw new Error("Unexpected delete");
      },
      listChanges: async () => ({ changes: [], cursor: 1, has_more: false })
    } as unknown as SyncApiClient;
    const data: PersistedData = {
      schemaVersion: 4,
      settings: testSettings(),
      cursor: 0,
      filterFingerprint: "",
      initialSyncCompleted: false,
      pendingInitialSyncMode: "remote",
      files: {},
      scanCache: {},
      pendingPaths: {},
      needsFullScan: true,
      lastFullScanAt: 0,
      lastIntegrityScanAt: 0,
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    const engine = new SyncEngine(vault, data, client, scanner, async () => undefined);

    const summary = await engine.run();

    expect(files.has("local-only.md")).toBe(false);
    expect(uploadedPath).toMatch(/^local-only\.conflict-test-device-\d{8}-\d{6}Z\.md$/u);
    expect(text(files.get(uploadedPath))).toBe("keep me");
    expect(summary.conflicts).toBe(1);
    expect(summary.uploaded).toBe(1);
    expect(data.initialSyncCompleted).toBe(true);
  });

  it("only propagates deletion for a path recorded by the persistent event queue", async () => {
    const unrelatedBytes = bytes("unrelated");
    const unrelatedHash = await sha256(unrelatedBytes);
    const deletedHash = await sha256(bytes("deleted"));
    const files = new Map<string, ArrayBuffer>([["unrelated.md", unrelatedBytes]]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const now = Date.now();
    const deletedPaths: string[] = [];
    const client = {
      serverInfo: async () => finalServerInfo(),
      status: async () => ({ latest_revision: 2, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      deleteFile: async (path: string, baseRevision: number) => {
        deletedPaths.push(path);
        expect(baseRevision).toBe(1);
        return {
          changed: true,
          change: {
            revision: 3,
            path,
            size: 0,
            modified_at: now,
            deleted: true,
            device_id: "local"
          }
        };
      },
      putFile: async () => {
        throw new Error("Unexpected upload");
      },
      listChanges: async () => ({ changes: [], cursor: 3, has_more: false })
    } as unknown as SyncApiClient;
    const data: PersistedData = {
      schemaVersion: 4,
      settings: testSettings(),
      cursor: 2,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: {
        "deleted.md": { hash: deletedHash, revision: 1, size: 7, modifiedAt: 1, deleted: false },
        "unrelated.md": { hash: unrelatedHash, revision: 2, size: 9, modifiedAt: 1, deleted: false }
      },
      scanCache: {
        "deleted.md": { hash: deletedHash, size: 7, modifiedAt: 1 },
        "unrelated.md": { hash: unrelatedHash, size: 9, modifiedAt: 1 }
      },
      pendingPaths: { "deleted.md": 10 },
      needsFullScan: false,
      lastFullScanAt: now,
      lastIntegrityScanAt: now,
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    const engine = new SyncEngine(vault, data, client, scanner, async () => undefined);

    const summary = await engine.run();

    expect(deletedPaths).toEqual(["deleted.md"]);
    expect(data.files["deleted.md"]?.deleted).toBe(true);
    expect(data.files["unrelated.md"]?.deleted).toBe(false);
    expect(files.has("unrelated.md")).toBe(true);
    expect(data.pendingPaths).toEqual({});
    expect(summary.deletedRemote).toBe(1);
  });

  it("does not acknowledge a newer event for the same path", async () => {
    const content = bytes("content");
    const hash = await sha256(content);
    const files = new Map<string, ArrayBuffer>([["note.md", content]]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const now = Date.now();
    const data: PersistedData = {
      schemaVersion: 4,
      settings: testSettings(),
      cursor: 1,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: { "note.md": { hash, revision: 1, size: 7, modifiedAt: 1, deleted: false } },
      scanCache: { "note.md": { hash, size: 7, modifiedAt: 1 } },
      pendingPaths: { "note.md": 10 },
      needsFullScan: false,
      lastFullScanAt: now,
      lastIntegrityScanAt: now,
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    const client = {
      serverInfo: async () => finalServerInfo(),
      status: async () => ({ latest_revision: 1, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      putFile: async () => {
        throw new Error("Unexpected upload");
      },
      deleteFile: async () => {
        throw new Error("Unexpected delete");
      },
      listChanges: async () => {
        data.pendingPaths["note.md"] = 11;
        return { changes: [], cursor: 1, has_more: false };
      }
    } as unknown as SyncApiClient;
    const engine = new SyncEngine(vault, data, client, scanner, async () => undefined);

    await engine.run();

    expect(data.pendingPaths).toEqual({ "note.md": 11 });
  });

  it("recovers a committed outbox operation after restart without uploading again", async () => {
    const content = bytes("content");
    const hash = await sha256(content);
    const operationId = "11111111-1111-4111-8111-111111111111";
    const files = new Map<string, ArrayBuffer>([["note.md", content]]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const now = Date.now();
    const committed: Change = {
      revision: 5,
      path: "note.md",
      blob_hash: hash,
      size: content.byteLength,
      modified_at: 1,
      deleted: false,
      device_id: "test-device"
    };
    const data: PersistedData = {
      schemaVersion: 4,
      settings: testSettings(),
      cursor: 4,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: {},
      scanCache: { "note.md": { hash, size: content.byteLength, modifiedAt: 1 } },
      pendingPaths: {},
      needsFullScan: false,
      lastFullScanAt: now,
      lastIntegrityScanAt: now,
      outbox: {
        [operationId]: {
          operationId,
          kind: "put",
          path: "note.md",
          baseRevision: 0,
          modifiedAt: 1,
          hash,
          size: content.byteLength,
          createdAt: 1
        }
      },
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    const client = {
      serverInfo: async () => finalServerInfo(),
      findOperation: async (id: string) => {
        expect(id).toBe(operationId);
        return { change: committed, changed: true };
      },
      status: async () => ({ latest_revision: 5, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      putFile: async () => {
        throw new Error("A committed operation must not be uploaded again");
      },
      deleteFile: async () => {
        throw new Error("Unexpected delete");
      },
      listChanges: async () => ({ changes: [], cursor: 5, has_more: false })
    } as unknown as SyncApiClient;
    let persistCount = 0;
    const engine = new SyncEngine(vault, data, client, scanner, async () => {
      persistCount += 1;
    });

    const summary = await engine.run();

    expect(data.files["note.md"]?.revision).toBe(5);
    expect(data.outbox).toEqual({});
    expect(summary.uploaded).toBe(1);
    expect(persistCount).toBeGreaterThanOrEqual(2);
  });

  it("persists an outbox entry before sending a new mutation", async () => {
    const content = bytes("new note");
    const hash = await sha256(content);
    const files = new Map<string, ArrayBuffer>([["new.md", content]]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const data: PersistedData = {
      schemaVersion: 4,
      settings: testSettings(),
      cursor: 0,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: {},
      scanCache: {},
      pendingPaths: {},
      needsFullScan: true,
      lastFullScanAt: 0,
      lastIntegrityScanAt: 0,
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    let stagedBeforeRequest = false;
    const client = {
      serverInfo: async () => finalServerInfo(),
      findOperation: async () => null,
      status: async () => ({ latest_revision: 0, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      putFile: async (_path: string, _base: number, modifiedAt: number, requestHash: string, requestContent: ArrayBuffer, operationId: string) => {
        stagedBeforeRequest = Object.hasOwn(data.outbox, operationId);
        expect(requestHash).toBe(hash);
        return {
          changed: true,
          change: {
            revision: 1,
            path: "new.md",
            blob_hash: requestHash,
            size: requestContent.byteLength,
            modified_at: modifiedAt,
            deleted: false,
            device_id: "test-device"
          }
        };
      },
      deleteFile: async () => {
        throw new Error("Unexpected delete");
      },
      listChanges: async () => ({ changes: [], cursor: 1, has_more: false })
    } as unknown as SyncApiClient;
    let persistedStagedEntry = false;
    const engine = new SyncEngine(vault, data, client, scanner, async () => {
      if (Object.keys(data.outbox).length > 0) persistedStagedEntry = true;
    });

    const summary = await engine.run();

    expect(stagedBeforeRequest).toBe(true);
    expect(persistedStagedEntry).toBe(true);
    expect(data.outbox).toEqual({});
    expect(data.files["new.md"]?.revision).toBe(1);
    expect(summary.uploaded).toBe(1);
  });

  it("keeps the original file and a recoverable inbox record when download verification fails", async () => {
    const local = bytes("local");
    const localHash = await sha256(local);
    const expected = bytes("expected");
    const expectedHash = await sha256(expected);
    const corrupted = bytes("corrupt!");
    const files = new Map<string, ArrayBuffer>([["note.md", local]]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const now = Date.now();
    const remote: Change = {
      revision: 2,
      path: "note.md",
      blob_hash: expectedHash,
      size: expected.byteLength,
      modified_at: 2,
      deleted: false,
      device_id: "remote"
    };
    const data: PersistedData = {
      schemaVersion: 5,
      settings: testSettings(),
      cursor: 1,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: { "note.md": { hash: localHash, revision: 1, size: local.byteLength, modifiedAt: 1, deleted: false } },
      scanCache: { "note.md": { hash: localHash, size: local.byteLength, modifiedAt: 1 } },
      pendingPaths: {},
      needsFullScan: false,
      lastFullScanAt: now,
      lastIntegrityScanAt: now,
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    const client = {
      serverInfo: async () => finalServerInfo(),
      status: async () => ({ latest_revision: 2, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      listChanges: async () => ({ changes: [remote], cursor: 2, has_more: false }),
      downloadBlob: async () => corrupted,
      putFile: async () => {
        throw new Error("Unexpected upload");
      },
      deleteFile: async () => {
        throw new Error("Unexpected delete");
      }
    } as unknown as SyncApiClient;
    const engine = new SyncEngine(vault, data, client, scanner, async () => undefined);

    await expect(engine.run()).rejects.toThrow("Downloaded content verification failed");

    expect(text(files.get("note.md"))).toBe("local");
    expect(Object.values(data.inbox)).toHaveLength(1);
    expect(data.cursor).toBe(1);
  });

  it("finishes a verified inbox download after restart", async () => {
    const oldContent = bytes("old");
    const oldHash = await sha256(oldContent);
    const remoteContent = bytes("remote");
    const remoteHash = await sha256(remoteContent);
    const downloadId = "11111111-1111-4111-8111-111111111111";
    const tempPath = `.sync-tunnel-download-${downloadId}.tmp`;
    const backupPath = `.sync-tunnel-backup-${downloadId}.tmp`;
    const files = new Map<string, ArrayBuffer>([
      ["note.md", oldContent],
      [tempPath, remoteContent]
    ]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const now = Date.now();
    const data: PersistedData = {
      schemaVersion: 5,
      settings: testSettings(),
      cursor: 1,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: { "note.md": { hash: oldHash, revision: 1, size: oldContent.byteLength, modifiedAt: 1, deleted: false } },
      scanCache: { "note.md": { hash: oldHash, size: oldContent.byteLength, modifiedAt: 1 } },
      pendingPaths: {},
      needsFullScan: false,
      lastFullScanAt: now,
      lastIntegrityScanAt: now,
      outbox: {},
      inbox: {
        [downloadId]: {
          downloadId,
          path: "note.md",
          revision: 2,
          hash: remoteHash,
          size: remoteContent.byteLength,
          modifiedAt: 2,
          deviceId: "remote",
          tempPath,
          backupPath,
          stage: "verified",
          createdAt: 1
        }
      },
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    const client = {
      serverInfo: async () => finalServerInfo(),
      status: async () => ({ latest_revision: 2, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      listChanges: async () => ({ changes: [], cursor: 2, has_more: false }),
      downloadBlob: async () => {
        throw new Error("Verified inbox content must not be downloaded again");
      },
      putFile: async () => {
        throw new Error("Unexpected upload");
      },
      deleteFile: async () => {
        throw new Error("Unexpected delete");
      }
    } as unknown as SyncApiClient;
    const engine = new SyncEngine(vault, data, client, scanner, async () => undefined);

    const summary = await engine.run();

    expect(text(files.get("note.md"))).toBe("remote");
    expect(files.has(tempPath)).toBe(false);
    expect(files.has(backupPath)).toBe(false);
    expect(data.inbox).toEqual({});
    expect(data.files["note.md"]?.revision).toBe(2);
    expect(summary.downloaded).toBe(1);
  });

  it("uploads only server-missing chunks before committing a manifest", async () => {
    const content = bytes("abcdefghij");
    const contentHash = await sha256(content);
    const files = new Map<string, ArrayBuffer>([["large.bin", content]]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const data: PersistedData = {
      schemaVersion: 6,
      settings: testSettings(),
      cursor: 0,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: {},
      scanCache: {},
      pendingPaths: {},
      needsFullScan: true,
      lastFullScanAt: 0,
      lastIntegrityScanAt: 0,
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    let requestedHashes: string[] = [];
    const uploadedHashes: string[] = [];
    const client = {
      serverInfo: async () => finalServerInfo(4),
      findOperation: async () => null,
      status: async () => ({ latest_revision: 0, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      missingChunks: async (hashes: string[]) => {
        requestedHashes = hashes;
        return hashes.slice(1);
      },
      putChunk: async (hash: string, chunk: ArrayBuffer) => {
        expect(await sha256(chunk)).toBe(hash);
        uploadedHashes.push(hash);
      },
      commitManifest: async (_path: string, _base: number, modifiedAt: number, hash: string, size: number, chunks: Array<{ hash: string; size: number }>, operationId: string) => {
        expect(Object.hasOwn(data.outbox, operationId)).toBe(true);
        expect(hash).toBe(contentHash);
        expect(size).toBe(content.byteLength);
        expect(chunks.map((chunk) => chunk.size)).toEqual([4, 4, 2]);
        return {
          changed: true,
          change: {
            revision: 1,
            path: "large.bin",
            blob_hash: hash,
            size,
            modified_at: modifiedAt,
            deleted: false,
            device_id: "test-device"
          }
        };
      },
      putFile: async () => {
        throw new Error("Chunk-capable upload must not use the whole-file endpoint");
      },
      deleteFile: async () => {
        throw new Error("Unexpected delete");
      },
      listChanges: async () => ({ changes: [], cursor: 1, has_more: false })
    } as unknown as SyncApiClient;
    const engine = new SyncEngine(vault, data, client, scanner, async () => undefined);

    const summary = await engine.run();

    expect(requestedHashes).toHaveLength(3);
    expect(uploadedHashes).toEqual(expect.arrayContaining(requestedHashes.slice(1)));
    expect(uploadedHashes).toHaveLength(2);
    expect(data.outbox).toEqual({});
    expect(summary.uploaded).toBe(1);
  });

  it("downloads and verifies a file through its Chunk Manifest", async () => {
    const remoteContent = bytes("abcdefghij");
    const remoteHash = await sha256(remoteContent);
    const chunkData = [bytes("abcd"), bytes("efgh"), bytes("ij")];
    const chunks = await Promise.all(chunkData.map(async (data) => ({ hash: await sha256(data), size: data.byteLength })));
    const files = new Map<string, ArrayBuffer>();
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const remote: Change = {
      revision: 2,
      path: "large.bin",
      blob_hash: remoteHash,
      size: remoteContent.byteLength,
      modified_at: 2,
      deleted: false,
      device_id: "remote"
    };
    const data: PersistedData = {
      schemaVersion: 6,
      settings: testSettings(),
      cursor: 1,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: { "large.bin": { hash: "", revision: 1, size: 0, modifiedAt: 1, deleted: true } },
      scanCache: {},
      pendingPaths: {},
      needsFullScan: true,
      lastFullScanAt: 0,
      lastIntegrityScanAt: 0,
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    const client = {
      serverInfo: async () => finalServerInfo(4),
      status: async () => ({ latest_revision: 2, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      listChanges: async () => ({ changes: [remote], cursor: 2, has_more: false }),
      findManifest: async (hash: string) => {
        expect(hash).toBe(remoteHash);
        return { size: remoteContent.byteLength, chunks };
      },
      downloadChunk: async (hash: string) => {
        const index = chunks.findIndex((chunk) => chunk.hash === hash);
        const content = chunkData[index];
        if (!content) throw new Error(`Unexpected Chunk ${hash}`);
        return content;
      },
      downloadBlob: async () => {
        throw new Error("Chunk Manifest download must not use the whole-file endpoint");
      },
      putFile: async () => {
        throw new Error("Unexpected upload");
      },
      deleteFile: async () => {
        throw new Error("Unexpected delete");
      }
    } as unknown as SyncApiClient;
    const engine = new SyncEngine(vault, data, client, scanner, async () => undefined);

    const summary = await engine.run();

    expect(text(files.get("large.bin"))).toBe("abcdefghij");
    expect(data.files["large.bin"]?.revision).toBe(2);
    expect(data.inbox).toEqual({});
    expect(summary.downloaded).toBe(1);
  });

  it("commits a high-confidence local rename as one logical operation", async () => {
    const content = bytes("rename me");
    const hash = await sha256(content);
    const files = new Map<string, ArrayBuffer>([["new.md", content]]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const now = Date.now();
    const renameId = "rename-event";
    const data: PersistedData = {
      schemaVersion: 7,
      settings: testSettings(),
      cursor: 1,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: { "old.md": { hash, revision: 1, size: content.byteLength, modifiedAt: 1, deleted: false } },
      scanCache: { "old.md": { hash, size: content.byteLength, modifiedAt: 1 } },
      pendingPaths: { "old.md": 1, "new.md": 2 },
      needsFullScan: false,
      lastFullScanAt: now,
      lastIntegrityScanAt: now,
      outbox: {},
      inbox: {},
	  pendingRenames: { [renameId]: { renameId, from: "old.md", to: "new.md", queuedAt: 1 } },
	  paused: false,
	  activities: [],
	  conflicts: [],
	  lastAcknowledgedRevision: 0
    };
    const client = {
      serverInfo: async () => finalServerInfo(),
      findOperation: async () => null,
      status: async () => ({ latest_revision: 1, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      renameFile: async (from: string, to: string, baseRevision: number, modifiedAt: number, operationId: string) => {
        expect(from).toBe("old.md");
        expect(to).toBe("new.md");
        expect(baseRevision).toBe(1);
        expect(Object.hasOwn(data.outbox, operationId)).toBe(true);
        return {
          changed: true,
          change: {
            revision: 3,
            path: to,
            blob_hash: hash,
            size: content.byteLength,
            modified_at: modifiedAt,
            deleted: false,
            device_id: "test-device"
          },
          related_changes: [{
            revision: 2,
            path: from,
            size: 0,
            modified_at: modifiedAt,
            deleted: true,
            device_id: "test-device"
          }]
        };
      },
      putFile: async () => {
        throw new Error("Rename must not upload content");
      },
      deleteFile: async () => {
        throw new Error("Rename must not issue a separate delete");
      },
      listChanges: async () => ({ changes: [], cursor: 3, has_more: false })
    } as unknown as SyncApiClient;
    const engine = new SyncEngine(vault, data, client, scanner, async () => undefined);

    const summary = await engine.run();

    expect(data.files["old.md"]?.deleted).toBe(true);
    expect(data.files["new.md"]?.revision).toBe(3);
    expect(data.pendingRenames).toEqual({});
    expect(data.pendingPaths).toEqual({});
    expect(data.outbox).toEqual({});
    expect(summary.renamed).toBe(1);
    expect(summary.uploaded).toBe(0);
    expect(summary.deletedRemote).toBe(0);
  });

  it("commits multiple queued deletions in one atomic batch", async () => {
    const aHash = await sha256(bytes("a"));
    const bHash = await sha256(bytes("b"));
    const files = new Map<string, ArrayBuffer>();
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const now = Date.now();
    const data: PersistedData = {
      schemaVersion: 8,
      settings: testSettings(),
      cursor: 2,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: {
        "a.md": { hash: aHash, revision: 1, size: 1, modifiedAt: 1, deleted: false },
        "b.md": { hash: bHash, revision: 2, size: 1, modifiedAt: 1, deleted: false }
      },
      scanCache: {
        "a.md": { hash: aHash, size: 1, modifiedAt: 1 },
        "b.md": { hash: bHash, size: 1, modifiedAt: 1 }
      },
      pendingPaths: { "a.md": 1, "b.md": 2 },
      needsFullScan: false,
      lastFullScanAt: now,
      lastIntegrityScanAt: now,
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    let requestCount = 0;
    const client = {
      serverInfo: async () => finalServerInfo(),
      findOperation: async () => null,
      status: async () => ({ latest_revision: 2, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      deleteFiles: async (items: Array<{ path: string; base_revision: number; modified_at: number }>, operationId: string) => {
        requestCount += 1;
        expect(Object.hasOwn(data.outbox, operationId)).toBe(true);
        expect(items.map((item) => item.path).sort()).toEqual(["a.md", "b.md"]);
        return {
          changed: true,
          changes: items.map((item, index) => ({
            revision: 3 + index,
            path: item.path,
            size: 0,
            modified_at: item.modified_at,
            deleted: true,
            device_id: "test-device"
          }))
        };
      },
      deleteFile: async () => {
        throw new Error("Batch-capable deletion must not issue individual requests");
      },
      putFile: async () => {
        throw new Error("Unexpected upload");
      },
      listChanges: async () => ({ changes: [], cursor: 4, has_more: false })
    } as unknown as SyncApiClient;
    const engine = new SyncEngine(vault, data, client, scanner, async () => undefined);

    const summary = await engine.run();

    expect(requestCount).toBe(1);
    expect(data.files["a.md"]?.deleted).toBe(true);
    expect(data.files["b.md"]?.deleted).toBe(true);
    expect(data.outbox).toEqual({});
    expect(data.pendingPaths).toEqual({});
    expect(summary.deletedRemote).toBe(2);
  });

  it("records an optimistic upload conflict for the conflict center", async () => {
    const base = bytes("base");
    const local = bytes("local concurrent edit");
    const remote = bytes("remote concurrent edit");
    const baseHash = await sha256(base);
    const localHash = await sha256(local);
    const remoteHash = await sha256(remote);
    const files = new Map<string, ArrayBuffer>([["note.md", local]]);
    const adapter = createMemoryAdapter(files);
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const now = Date.now();
    const remoteChange: Change = {
      revision: 2,
      path: "note.md",
      blob_hash: remoteHash,
      size: remote.byteLength,
      modified_at: now + 1,
      deleted: false,
      device_id: "remote-device"
    };
    const data: PersistedData = {
      schemaVersion: 8,
      settings: testSettings(),
      cursor: 1,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: {
        "note.md": { hash: baseHash, revision: 1, size: base.byteLength, modifiedAt: 1, deleted: false }
      },
      scanCache: {
        "note.md": { hash: baseHash, size: base.byteLength, modifiedAt: 1 }
      },
      pendingPaths: { "note.md": now },
      needsFullScan: false,
      lastFullScanAt: now,
      lastIntegrityScanAt: now,
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 1
    };
    const client = {
      serverInfo: async () => finalServerInfo(),
      status: async () => ({ latest_revision: 2, max_file_bytes: 1024 }),
      acknowledge: async () => undefined,
      putFile: async () => {
        throw new ApiError("stale revision", 409, "revision_conflict", remoteChange);
      },
      downloadBlob: async (hash: string) => {
        expect(hash).toBe(remoteHash);
        return remote;
      },
      listChanges: async () => ({ changes: [remoteChange], cursor: 2, has_more: false }),
      deleteFile: async () => {
        throw new Error("Unexpected delete");
      }
    } as unknown as SyncApiClient;
    const engine = new SyncEngine(vault, data, client, scanner, async () => undefined);

    const summary = await engine.run();

    expect(summary.conflicts).toBe(1);
    expect(summary.downloaded).toBe(1);
    expect(text(files.get("note.md"))).toBe("remote concurrent edit");
    expect(data.conflicts).toHaveLength(1);
    expect(data.conflicts[0]).toMatchObject({
      originalPath: "note.md",
      localRevision: 1,
      remoteRevision: 2,
      localHash,
      remoteHash,
      remoteDeviceId: "remote-device"
    });
    expect(text(files.get(data.conflicts[0]!.conflictPath))).toBe("local concurrent edit");
  });
});

function createMemoryAdapter(files: Map<string, ArrayBuffer>): DataAdapter {
  return {
    list: async (directory: string) => {
      if (directory === ".obsidian") return { files: [], folders: [] };
      if (directory !== "") throw new Error(`Unexpected directory ${directory}`);
      return { files: [...files.keys()], folders: [] };
    },
    stat: async (path: string) => {
      const content = files.get(path);
      return content ? { type: "file", ctime: 1, mtime: 1, size: content.byteLength } : null;
    },
    exists: async (path: string) => files.has(path),
    readBinary: async (path: string) => {
      const content = files.get(path);
      if (!content) throw new Error(`Missing file ${path}`);
      return content.slice(0);
    },
    writeBinary: async (path: string, content: ArrayBuffer) => {
      files.set(path, content.slice(0));
    },
    remove: async (path: string) => {
      files.delete(path);
    },
    rename: async (path: string, destination: string) => {
      const content = files.get(path);
      if (!content) throw new Error(`Missing file ${path}`);
      files.set(destination, content);
      files.delete(path);
    },
    mkdir: async () => undefined
  } as unknown as DataAdapter;
}

function bytes(value: string): ArrayBuffer {
  return new TextEncoder().encode(value).buffer;
}

function text(value: ArrayBuffer | undefined): string {
  if (!value) throw new Error("Expected file content");
  return new TextDecoder().decode(value);
}

function testSettings(): PersistedData["settings"] {
  return {
    serverUrl: "https://sync.example.com",
    vaultId: "test-vault",
    deviceId: "test-device",
    deviceName: "Test device",
    credentialSecretName: "token",
    accessClientId: "",
    accessClientSecretName: "",
    automaticSync: false,
    syncOnStartup: false,
    syncIntervalSeconds: 300,
    syncProfile: "full",
    excludedPatterns: [],
    language: "zh-CN",
    mobileMaxFileBytes: 32 * 1024 * 1024
  };
}

function finalServerInfo(chunkSize = 4 * 1024 * 1024) {
  return {
    server_version: "test",
    protocol: { version: 1 },
    capabilities: [
      "snapshot",
      "idempotent-operations",
      "whole-file",
      "chunk-transfer",
      "rename",
      "batch-delete",
      "device-ack",
      "history",
      "restore",
      "scoped-credentials"
    ],
    database: { schema_version: 7 },
    limits: {
      max_file_bytes: 1024,
      max_page_size: 1000,
      chunk_size: chunkSize,
      max_chunk_query: 1000,
      chunk_concurrency: 3
    }
  };
}
