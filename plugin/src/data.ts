import type { InitialSyncMode, PersistedData, PluginSettings, SyncProfile } from "./types";

export const DATA_SCHEMA_VERSION = 9;

export function migrateData(raw: unknown, generatedDeviceName = generateDeviceName()): PersistedData {
  const parsed = isRecord(raw) ? raw : {};
  const settings = isRecord(parsed.settings) ? parsed.settings : {};
  const previousSchemaVersion = typeof parsed.schemaVersion === "number" ? parsed.schemaVersion : 0;
	const compatible = previousSchemaVersion === DATA_SCHEMA_VERSION;
  const defaults: PluginSettings = {
    serverUrl: "",
    vaultId: "",
    deviceId: "",
	deviceName: generatedDeviceName,
	credentialSecretName: "",
    accessClientId: "",
    accessClientSecretName: "",
    automaticSync: true,
    syncOnStartup: true,
    syncIntervalSeconds: 300,
	// New installations start with the safer profile that omits plugin data and
	// device workspace state.
	syncProfile: "recommended",
    excludedPatterns: [".git/**", ".trash/**", "**/.DS_Store", "**/Thumbs.db"],
	language: "zh-CN",
	mobileMaxFileBytes: 32 * 1024 * 1024
  };
	const mergedSettings: PluginSettings = {
		serverUrl: stringValue(settings.serverUrl, defaults.serverUrl),
		vaultId: stringValue(settings.vaultId, defaults.vaultId),
		deviceId: stringValue(settings.deviceId, defaults.deviceId),
		deviceName: stringValue(settings.deviceName, defaults.deviceName),
		credentialSecretName: stringValue(settings.credentialSecretName, defaults.credentialSecretName),
		accessClientId: stringValue(settings.accessClientId, defaults.accessClientId),
		accessClientSecretName: stringValue(settings.accessClientSecretName, defaults.accessClientSecretName),
		automaticSync: booleanValue(settings.automaticSync, defaults.automaticSync),
		syncOnStartup: booleanValue(settings.syncOnStartup, defaults.syncOnStartup),
		syncIntervalSeconds: positiveNumber(settings.syncIntervalSeconds, defaults.syncIntervalSeconds),
		syncProfile: isSyncProfile(settings.syncProfile) ? settings.syncProfile : defaults.syncProfile,
		excludedPatterns: Array.isArray(settings.excludedPatterns) ? settings.excludedPatterns.filter((value): value is string => typeof value === "string") : defaults.excludedPatterns,
		language: settings.language === "en" ? "en" : "zh-CN",
		mobileMaxFileBytes: positiveNumber(settings.mobileMaxFileBytes, defaults.mobileMaxFileBytes)
	};
	if (!compatible) {
		mergedSettings.deviceId = "";
		mergedSettings.credentialSecretName = "";
	}
  if (!isSyncProfile(mergedSettings.syncProfile)) mergedSettings.syncProfile = defaults.syncProfile;
  return {
    schemaVersion: DATA_SCHEMA_VERSION,
    settings: mergedSettings,
	cursor: compatible && typeof parsed.cursor === "number" && parsed.cursor >= 0 ? parsed.cursor : 0,
	// An empty fingerprint deliberately triggers a fresh server snapshot.
	filterFingerprint: compatible && typeof parsed.filterFingerprint === "string" ? parsed.filterFingerprint : "",
	// Only state written by this final schema may resume without confirmation.
	initialSyncCompleted: compatible && parsed.initialSyncCompleted === true,
    pendingInitialSyncMode: compatible && isInitialSyncMode(parsed.pendingInitialSyncMode) ? parsed.pendingInitialSyncMode : null,
    files: compatible && isRecord(parsed.files) ? parsed.files as PersistedData["files"] : {},
    scanCache: compatible && isRecord(parsed.scanCache) ? parsed.scanCache as PersistedData["scanCache"] : {},
    pendingPaths: compatible && isRecord(parsed.pendingPaths) ? sanitizePendingPaths(parsed.pendingPaths) : {},
	needsFullScan: compatible ? parsed.needsFullScan !== false : true,
    lastFullScanAt: compatible ? nonNegativeNumber(parsed.lastFullScanAt) : 0,
    lastIntegrityScanAt: compatible ? nonNegativeNumber(parsed.lastIntegrityScanAt) : 0,
	outbox: compatible && isRecord(parsed.outbox) ? sanitizeOutbox(parsed.outbox) : {},
    inbox: compatible && isRecord(parsed.inbox) ? sanitizeInbox(parsed.inbox) : {},
    pendingRenames: compatible && isRecord(parsed.pendingRenames) ? sanitizePendingRenames(parsed.pendingRenames) : {},
	paused: compatible && parsed.paused === true,
	activities: compatible && Array.isArray(parsed.activities) ? parsed.activities.slice(-100) as PersistedData["activities"] : [],
	conflicts: compatible && Array.isArray(parsed.conflicts) ? parsed.conflicts as PersistedData["conflicts"] : [],
	lastAcknowledgedRevision: compatible ? nonNegativeNumber(parsed.lastAcknowledgedRevision) : 0
  };
}

function generateDeviceName(): string {
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

function positiveNumber(value: unknown, fallback: number): number {
	return typeof value === "number" && Number.isFinite(value) && value > 0 ? value : fallback;
}

function stringValue(value: unknown, fallback: string): string {
	return typeof value === "string" ? value : fallback;
}

function booleanValue(value: unknown, fallback: boolean): boolean {
	return typeof value === "boolean" ? value : fallback;
}

function sanitizeOutbox(value: Record<string, unknown>): PersistedData["outbox"] {
  const result: PersistedData["outbox"] = {};
  for (const [operationId, raw] of Object.entries(value)) {
    if (!isRecord(raw) || raw.operationId !== operationId) continue;
    if (raw.kind !== "put" && raw.kind !== "delete" && raw.kind !== "rename" && raw.kind !== "batch-delete") continue;
    if (typeof raw.path !== "string" || typeof raw.hash !== "string") continue;
    const numbers = [raw.baseRevision, raw.modifiedAt, raw.size, raw.createdAt];
    if (!numbers.every((item) => typeof item === "number" && Number.isFinite(item) && item >= 0)) continue;
    result[operationId] = raw as unknown as PersistedData["outbox"][string];
  }
  return result;
}

function sanitizeInbox(value: Record<string, unknown>): PersistedData["inbox"] {
  const result: PersistedData["inbox"] = {};
  for (const [downloadId, raw] of Object.entries(value)) {
    if (!isRecord(raw) || raw.downloadId !== downloadId) continue;
    if (raw.stage !== "downloading" && raw.stage !== "verified" && raw.stage !== "replacing") continue;
    const strings = [raw.path, raw.hash, raw.deviceId, raw.tempPath, raw.backupPath];
    if (!strings.every((item) => typeof item === "string")) continue;
    const numbers = [raw.revision, raw.size, raw.modifiedAt, raw.createdAt];
    if (!numbers.every((item) => typeof item === "number" && Number.isFinite(item) && item >= 0)) continue;
    result[downloadId] = raw as unknown as PersistedData["inbox"][string];
  }
  return result;
}

function sanitizePendingRenames(value: Record<string, unknown>): PersistedData["pendingRenames"] {
  const result: PersistedData["pendingRenames"] = {};
  for (const [renameId, raw] of Object.entries(value)) {
    if (!isRecord(raw) || raw.renameId !== renameId) continue;
    if (typeof raw.from !== "string" || typeof raw.to !== "string") continue;
    if (typeof raw.queuedAt !== "number" || !Number.isFinite(raw.queuedAt) || raw.queuedAt < 0) continue;
    result[renameId] = raw as unknown as PersistedData["pendingRenames"][string];
  }
  return result;
}
