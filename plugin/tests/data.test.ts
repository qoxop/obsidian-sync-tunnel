import { describe, expect, it } from "vitest";

import { DATA_SCHEMA_VERSION, migrateData } from "../src/data";

describe("migrateData", () => {
  it("requires explicit first-sync confirmation for a new installation", () => {
    const data = migrateData(null, "generated-device");

    expect(data.schemaVersion).toBe(DATA_SCHEMA_VERSION);
    expect(data.settings.deviceId).toBe("generated-device");
    expect(data.settings.syncProfile).toBe("recommended");
    expect(data.initialSyncCompleted).toBe(false);
    expect(data.pendingInitialSyncMode).toBeNull();
    expect(data.filterFingerprint).toBe("");
    expect(data.needsFullScan).toBe(true);
    expect(data.scanCache).toEqual({});
    expect(data.pendingPaths).toEqual({});
    expect(data.outbox).toEqual({});
  });

  it("keeps an existing schema-v1 installation initialized and schedules a snapshot", () => {
    const data = migrateData({
      schemaVersion: 1,
      settings: { vaultId: "existing-vault", deviceId: "existing-device" },
      cursor: 12,
      files: {
        "note.md": { hash: "abc", revision: 12, size: 3, modifiedAt: 1, deleted: false }
      }
    }, "unused-device");

    expect(data.settings.vaultId).toBe("existing-vault");
    expect(data.settings.deviceId).toBe("existing-device");
    expect(data.settings.syncProfile).toBe("full");
    expect(data.initialSyncCompleted).toBe(true);
    expect(data.filterFingerprint).toBe("");
    expect(data.cursor).toBe(12);
    expect(data.files["note.md"]?.revision).toBe(12);
    expect(data.needsFullScan).toBe(true);
  });

  it("retains a pending approved initialization mode after a failed run", () => {
    const data = migrateData({
      schemaVersion: 2,
      initialSyncCompleted: false,
      pendingInitialSyncMode: "remote"
    }, "device");

    expect(data.initialSyncCompleted).toBe(false);
    expect(data.pendingInitialSyncMode).toBe("remote");
  });
});
