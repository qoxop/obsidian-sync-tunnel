import { describe, expect, it } from "vitest";

import { DATA_SCHEMA_VERSION, migrateData } from "../src/data";

describe("migrateData", () => {
  it("creates an unpaired installation that requires explicit initialization", () => {
    const data = migrateData(null, "generated-device-name");

    expect(data.schemaVersion).toBe(DATA_SCHEMA_VERSION);
    expect(data.settings.deviceId).toBe("");
    expect(data.settings.deviceName).toBe("generated-device-name");
    expect(data.settings.credentialSecretName).toBe("");
    expect(data.settings.syncProfile).toBe("recommended");
    expect(data.initialSyncCompleted).toBe(false);
    expect(data.pendingInitialSyncMode).toBeNull();
    expect(data.cursor).toBe(0);
    expect(data.files).toEqual({});
    expect(data.outbox).toEqual({});
    expect(data.inbox).toEqual({});
    expect(data.activities).toEqual([]);
    expect(data.conflicts).toEqual([]);
  });

  it("intentionally resets pre-1.0 protocol state while retaining harmless setup choices", () => {
    const data = migrateData({
      schemaVersion: 2,
      settings: {
        serverUrl: "https://sync.example.com",
        vaultId: "existing-vault",
        deviceId: "legacy-device",
        apiTokenSecretName: "legacy-token",
        syncProfile: "full"
      },
      cursor: 12,
      initialSyncCompleted: true,
      pendingInitialSyncMode: "remote",
      files: {
        "note.md": { hash: "abc", revision: 12, size: 3, modifiedAt: 1, deleted: false }
      }
    }, "new-device-name");

    expect(data.settings.serverUrl).toBe("https://sync.example.com");
    expect(data.settings.vaultId).toBe("existing-vault");
    expect(data.settings.syncProfile).toBe("full");
    expect(data.settings.deviceId).toBe("");
    expect(data.settings.credentialSecretName).toBe("");
    expect("apiTokenSecretName" in data.settings).toBe(false);
    expect(data.initialSyncCompleted).toBe(false);
    expect(data.pendingInitialSyncMode).toBeNull();
    expect(data.cursor).toBe(0);
    expect(data.files).toEqual({});
    expect(data.needsFullScan).toBe(true);
  });

  it("preserves only state written by the final 1.0 data schema", () => {
    const raw = migrateData(null, "device-name");
    raw.settings.deviceId = "device-server-id";
    raw.settings.credentialSecretName = "credential-key";
    raw.initialSyncCompleted = true;
    raw.cursor = 42;
    raw.lastAcknowledgedRevision = 42;
    raw.paused = true;
    raw.files["note.md"] = { hash: "abc", revision: 42, size: 3, modifiedAt: 1, deleted: false };
    raw.activities.push({ id: "activity", startedAt: 1, status: "completed", phase: "idle" });

    const data = migrateData(raw, "unused-name");

    expect(data.settings.deviceId).toBe("device-server-id");
    expect(data.settings.credentialSecretName).toBe("credential-key");
    expect(data.initialSyncCompleted).toBe(true);
    expect(data.cursor).toBe(42);
    expect(data.lastAcknowledgedRevision).toBe(42);
    expect(data.paused).toBe(true);
    expect(data.files["note.md"]?.revision).toBe(42);
    expect(data.activities).toHaveLength(1);
  });
});
