import { createHash } from "node:crypto";
import { createReadStream } from "node:fs";
import { mkdtemp, mkdir, open, readFile, rename, rm, stat, unlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";

import { DataAdapter, FileSystemAdapter, Platform, Vault } from "obsidian";
import { describe, expect, it } from "vitest";

import type { SyncApiClient } from "../src/api-client";
import { sha256 } from "../src/hash";
import { SyncEngine } from "../src/sync-engine";
import type { Change, PersistedData } from "../src/types";
import { VaultScanner } from "../src/vault-scanner";

describe("desktop Chunk streaming", () => {
  it("writes and verifies a Chunk Manifest without DataAdapter.readBinary", async () => {
    const root = await mkdtemp(join(tmpdir(), "sync-tunnel-stream-"));
    const previousDesktop = Platform.isDesktopApp;
    const runtime = globalThis as unknown as Record<string, unknown>;
    const previousWindow = runtime["window"];
    const requestedDesktopModules = new Set<string>();
    runtime["window"] = {
      require: (moduleId: string): unknown => {
        requestedDesktopModules.add(moduleId);
        if (moduleId === "node:fs") return { createReadStream };
        if (moduleId === "node:fs/promises") return { open };
        if (moduleId === "node:crypto") return { createHash };
        throw new Error(`Unexpected desktop module ${moduleId}`);
      }
    };
    Platform.isDesktopApp = true;
    try {
      let adapterReads = 0;
      const adapter = Object.assign(new FileSystemAdapter(), {
        getFullPath: (path: string) => join(root, ...path.split("/")),
        list: async () => ({ files: [], folders: [] }),
        exists: async (path: string) => {
          try {
            await stat(join(root, ...path.split("/")));
            return true;
          } catch {
            return false;
          }
        },
        stat: async (path: string) => {
          try {
            const value = await stat(join(root, ...path.split("/")));
            return { type: value.isFile() ? "file" : "folder", ctime: value.ctimeMs, mtime: value.mtimeMs, size: value.size };
          } catch {
            return null;
          }
        },
        readBinary: async (path: string) => {
          adapterReads += 1;
          const value = await readFile(join(root, ...path.split("/")));
          return Uint8Array.from(value).buffer;
        },
        writeBinary: async (path: string, content: ArrayBuffer) => {
          const fullPath = join(root, ...path.split("/"));
          await mkdir(dirname(fullPath), { recursive: true });
          await writeFile(fullPath, new Uint8Array(content));
        },
        rename: async (path: string, destination: string) => {
          await rename(join(root, ...path.split("/")), join(root, ...destination.split("/")));
        },
        remove: async (path: string) => unlink(join(root, ...path.split("/"))),
        mkdir: async (path: string) => mkdir(join(root, ...path.split("/")), { recursive: true })
      }) as unknown as DataAdapter;
      const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
      const scanner = new VaultScanner(vault, [], []);
      const content = new TextEncoder().encode("abcdefghij").buffer;
      const contentHash = await sha256(content);
      const chunkData = [content.slice(0, 4), content.slice(4, 8), content.slice(8)];
      const chunks = await Promise.all(chunkData.map(async (data) => ({ hash: await sha256(data), size: data.byteLength })));
      const remote: Change = {
        revision: 2,
        path: "large.bin",
        blob_hash: contentHash,
        size: content.byteLength,
        modified_at: Date.now(),
        deleted: false,
        device_id: "remote"
      };
      const now = Date.now();
      const data: PersistedData = {
        schemaVersion: 7,
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
        cursor: 1,
        filterFingerprint: scanner.filterFingerprint(),
        initialSyncCompleted: true,
        pendingInitialSyncMode: null,
        files: { "large.bin": { hash: "", revision: 1, size: 0, modifiedAt: 1, deleted: true } },
        scanCache: { "baseline.md": { hash: "unused", size: 1, modifiedAt: 1 } },
        pendingPaths: {},
        needsFullScan: false,
        lastFullScanAt: now,
        lastIntegrityScanAt: now,
        outbox: {},
        inbox: {},
        pendingRenames: {}
      };
      const client = {
        serverInfo: async () => ({ capabilities: ["chunk-download-v1"], limits: { chunk_size: 4 } }),
        status: async () => ({ latest_revision: 2, max_upload_bytes: 1024 }),
        listChanges: async () => ({ changes: [remote], cursor: 2, has_more: false }),
        findManifest: async () => ({ size: content.byteLength, chunks }),
        downloadChunk: async (hash: string) => {
          const index = chunks.findIndex((chunk) => chunk.hash === hash);
          const value = chunkData[index];
          if (!value) throw new Error(`Unexpected Chunk ${hash}`);
          return value;
        },
        downloadBlob: async () => {
          throw new Error("Desktop stream must use Chunk downloads");
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

      expect(new TextDecoder().decode(await readFile(join(root, "large.bin")))).toBe("abcdefghij");
      expect(adapterReads).toBe(0);
      expect(summary.downloaded).toBe(1);
      expect([...requestedDesktopModules].sort()).toEqual(["node:crypto", "node:fs", "node:fs/promises"]);
    } finally {
      Platform.isDesktopApp = previousDesktop;
      if (previousWindow === undefined) delete runtime["window"];
      else runtime["window"] = previousWindow;
      await rm(root, { recursive: true, force: true });
    }
  });
});
