import type { InitialSyncMode, PersistedData, PluginSettings, SyncProfile } from "./types";

export const DATA_SCHEMA_VERSION = 3;

export function migrateData(raw: unknown, generatedDeviceId = generateDeviceId()): PersistedData {
  const parsed = isRecord(raw) ? raw : {};
  const settings = isRecord(parsed.settings) ? parsed.settings : {};
  const previousSchemaVersion = typeof parsed.schemaVersion === "number" ? parsed.schemaVersion : 0;
  const defaults: PluginSettings = {
    serverUrl: "",
    vaultId: "",
    deviceId: generatedDeviceId,
    apiTokenSecretName: "",
    accessClientId: "",
    accessClientSecretName: "",
    automaticSync: true,
    syncOnStartup: true,
    syncIntervalSeconds: 300,
    // Preserve 0.1 behavior for existing users. New installations start with
    // the safer profile that omits plugin data and device workspace state.
    syncProfile: previousSchemaVersion >= 1 ? "full" : "recommended",
    excludedPatterns: [".git/**", ".trash/**", "**/.DS_Store", "**/Thumbs.db"]
  };
  const mergedSettings = { ...defaults, ...settings } as PluginSettings;
  if (!isSyncProfile(mergedSettings.syncProfile)) mergedSettings.syncProfile = defaults.syncProfile;
  return {
    schemaVersion: DATA_SCHEMA_VERSION,
    settings: mergedSettings,
    cursor: typeof parsed.cursor === "number" && parsed.cursor >= 0 ? parsed.cursor : 0,
    // An empty fingerprint deliberately triggers one safe server snapshot after
    // upgrading from schema v1 or installing on a new device.
    filterFingerprint: typeof parsed.filterFingerprint === "string" ? parsed.filterFingerprint : "",
    // Existing 0.1 installations have already selected their initial direction.
    // Only a genuinely new installation is stopped for explicit confirmation.
    initialSyncCompleted: typeof parsed.initialSyncCompleted === "boolean"
      ? parsed.initialSyncCompleted
      : previousSchemaVersion >= 1,
    pendingInitialSyncMode: isInitialSyncMode(parsed.pendingInitialSyncMode) ? parsed.pendingInitialSyncMode : null,
    files: isRecord(parsed.files) ? parsed.files as PersistedData["files"] : {},
    scanCache: isRecord(parsed.scanCache) ? parsed.scanCache as PersistedData["scanCache"] : {},
    pendingPaths: isRecord(parsed.pendingPaths) ? sanitizePendingPaths(parsed.pendingPaths) : {},
    needsFullScan: typeof parsed.needsFullScan === "boolean" ? parsed.needsFullScan : true,
    lastFullScanAt: nonNegativeNumber(parsed.lastFullScanAt),
    lastIntegrityScanAt: nonNegativeNumber(parsed.lastIntegrityScanAt)
  };
}

function generateDeviceId(): string {
  const platform = navigator.userAgent.includes("Mobile") ? "mobile" : "device";
  return `${platform}-${crypto.randomUUID().slice(0, 8)}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isInitialSyncMode(value: unknown): value is InitialSyncMode {
  return value === "merge" || value === "remote" || value === "local";
}

function isSyncProfile(value: unknown): value is SyncProfile {
  return value === "notes" || value === "recommended" || value === "full" || value === "custom";
}

function sanitizePendingPaths(value: Record<string, unknown>): Record<string, number> {
  return Object.fromEntries(Object.entries(value).filter((entry): entry is [string, number] => (
    entry[0].length > 0 && typeof entry[1] === "number" && Number.isFinite(entry[1]) && entry[1] >= 0
  )));
}

function nonNegativeNumber(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : 0;
}
