export interface PluginSettings {
  serverUrl: string;
  vaultId: string;
  deviceId: string;
  apiTokenSecretName: string;
  accessClientId: string;
  accessClientSecretName: string;
  automaticSync: boolean;
  syncOnStartup: boolean;
  syncIntervalSeconds: number;
  syncProfile: SyncProfile;
  excludedPatterns: string[];
}

export type SyncProfile = "notes" | "recommended" | "full" | "custom";

export interface FileState {
  hash: string;
  revision: number;
  size: number;
  modifiedAt: number;
  deleted: boolean;
}

export interface ScanCacheEntry {
  hash: string;
  size: number;
  modifiedAt: number;
}

export interface PersistedData {
  schemaVersion: number;
  settings: PluginSettings;
  cursor: number;
  filterFingerprint: string;
  initialSyncCompleted: boolean;
  pendingInitialSyncMode: InitialSyncMode | null;
  files: Record<string, FileState>;
  scanCache: Record<string, ScanCacheEntry>;
  pendingPaths: Record<string, number>;
  needsFullScan: boolean;
  lastFullScanAt: number;
  lastIntegrityScanAt: number;
}

export type InitialSyncMode = "merge" | "remote" | "local";

export interface InitialSyncPreview {
  localFiles: number;
  remoteFiles: number;
  same: number;
  different: number;
  localOnly: number;
  remoteOnly: number;
  localAgainstRemoteDelete: number;
  snapshotRevision: number;
}

export interface LocalFile {
  path: string;
  hash: string;
  size: number;
  modifiedAt: number;
}

export interface Change {
  revision: number;
  path: string;
  blob_hash?: string;
  size: number;
  modified_at: number;
  deleted: boolean;
  device_id: string;
}

export interface MutationResponse {
  change: Change;
  changed: boolean;
}

export interface ChangesResponse {
  changes: Change[];
  cursor: number;
  has_more: boolean;
}

export interface StatusResponse {
  latest_revision: number;
  max_upload_bytes: number;
}

export interface ServerInfoResponse {
  server_version: string;
  protocol: { min: number; max: number };
  capabilities: string[];
  database: { schema_version: number };
  limits: { max_upload_bytes: number; max_page_size: number };
}

export interface SnapshotResponse {
  files: Change[];
  snapshot_revision: number;
  cursor: string;
  has_more: boolean;
}

export interface SyncSummary {
  uploaded: number;
  downloaded: number;
  deletedRemote: number;
  deletedLocal: number;
  conflicts: number;
  skipped: number;
  restartRequired: boolean;
}
