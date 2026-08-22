import type { DataAdapter, Vault } from "obsidian";
import { describe, expect, it } from "vitest";

import type { SyncApiClient } from "../src/api-client";
import { SyncEngine } from "../src/sync-engine";
import type { PersistedData } from "../src/types";
import { VaultScanner } from "../src/vault-scanner";

describe("pre-1.0 protocol rejection", () => {
  it("refuses any server that is not the one final protocol", async () => {
    const adapter = {
      list: async () => ({ files: [], folders: [] }),
      stat: async () => null,
      exists: async () => false
    } as unknown as DataAdapter;
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);
    const data: PersistedData = {
      schemaVersion: 9,
      settings: {
        serverUrl: "https://sync.example.com",
        vaultId: "vault-a",
        deviceId: "device-a",
        deviceName: "Device A",
        credentialSecretName: "credential",
        accessClientId: "",
        accessClientSecretName: "",
        automaticSync: false,
        syncOnStartup: false,
        syncIntervalSeconds: 300,
        syncProfile: "recommended",
        excludedPatterns: [],
        language: "zh-CN",
        mobileMaxFileBytes: 32 * 1024 * 1024
      },
      cursor: 0,
      filterFingerprint: scanner.filterFingerprint(),
      initialSyncCompleted: true,
      pendingInitialSyncMode: null,
      files: {},
      scanCache: {},
      pendingPaths: {},
      needsFullScan: false,
      lastFullScanAt: Date.now(),
      lastIntegrityScanAt: Date.now(),
      outbox: {},
      inbox: {},
      pendingRenames: {},
      paused: false,
      activities: [],
      conflicts: [],
      lastAcknowledgedRevision: 0
    };
    const legacyClient = {
      serverInfo: async () => ({
        server_version: "0.3.0",
        protocol: { version: 0 },
        capabilities: ["snapshot-v1", "operation-id"],
        database: { schema_version: 4 },
        limits: { max_file_bytes: 1024, max_page_size: 1000 }
      }),
      status: async () => {
        throw new Error("Status must not be requested from an incompatible server");
      }
    } as unknown as SyncApiClient;

    const engine = new SyncEngine(vault, data, legacyClient, scanner, async () => undefined);

    await expect(engine.run()).rejects.toThrow("客户端与服务端必须同时升级");
  });
});
