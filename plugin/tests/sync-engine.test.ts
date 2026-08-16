import type { DataAdapter, Vault } from "obsidian";
import { describe, expect, it } from "vitest";

import type { SyncApiClient } from "../src/api-client";
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
      serverInfo: async () => ({ capabilities: [] }),
      status: async () => ({ latest_revision: 5, max_upload_bytes: 1024 }),
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
        apiTokenSecretName: "token",
        accessClientId: "",
        accessClientSecretName: "",
        automaticSync: false,
        syncOnStartup: false,
        syncIntervalSeconds: 300,
        syncProfile: "full",
        excludedPatterns: []
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
      outbox: {}
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
      serverInfo: async () => ({ capabilities: [] }),
      status: async () => ({ latest_revision: 5, max_upload_bytes: 1024 }),
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
      outbox: {}
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
      serverInfo: async () => ({ capabilities: [] }),
      status: async () => ({ latest_revision: 0, max_upload_bytes: 1024 }),
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
      outbox: {}
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
      serverInfo: async () => ({ capabilities: [] }),
      status: async () => ({ latest_revision: 2, max_upload_bytes: 1024 }),
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
      outbox: {}
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
      outbox: {}
    };
    const client = {
      serverInfo: async () => ({ capabilities: [] }),
      status: async () => ({ latest_revision: 1, max_upload_bytes: 1024 }),
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
      }
    };
    const client = {
      serverInfo: async () => ({ capabilities: ["operation-id"] }),
      findOperation: async (id: string) => {
        expect(id).toBe(operationId);
        return { change: committed, changed: true };
      },
      status: async () => ({ latest_revision: 5, max_upload_bytes: 1024 }),
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
      outbox: {}
    };
    let stagedBeforeRequest = false;
    const client = {
      serverInfo: async () => ({ capabilities: ["operation-id"] }),
      findOperation: async () => null,
      status: async () => ({ latest_revision: 0, max_upload_bytes: 1024 }),
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
});

function createMemoryAdapter(files: Map<string, ArrayBuffer>): DataAdapter {
  return {
    list: async (directory: string) => {
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
    apiTokenSecretName: "token",
    accessClientId: "",
    accessClientSecretName: "",
    automaticSync: false,
    syncOnStartup: false,
    syncIntervalSeconds: 300,
    syncProfile: "full",
    excludedPatterns: []
  };
}
