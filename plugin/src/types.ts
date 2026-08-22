export interface PluginSettings {
  serverUrl: string;
  vaultId: string;
  deviceId: string;
  deviceName: string;
  credentialSecretName: string;
  accessClientId: string;
  accessClientSecretName: string;
  automaticSync: boolean;
  syncOnStartup: boolean;
  syncIntervalSeconds: number;
  syncProfile: SyncProfile;
  excludedPatterns: string[];
  language: "zh-CN" | "en";
  mobileMaxFileBytes: number;
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

export interface OutboxOperation {
  operationId: string;
  kind: "put" | "delete" | "rename" | "batch-delete";
  path: string;
  sourcePath?: string;
  baseRevision: number;
  modifiedAt: number;
  hash: string;
  size: number;
  createdAt: number;
  transport?: "whole" | "chunks";
  chunks?: ChunkRef[];
  batchDeletes?: BatchDeleteItem[];
}

export interface BatchDeleteItem {
  path: string;
  base_revision: number;
  modified_at: number;
}

export interface PendingRename {
  renameId: string;
  from: string;
  to: string;
  queuedAt: number;
}

export interface ChunkRef {
  hash: string;
  size: number;
}

export interface InboxDownload {
  downloadId: string;
  path: string;
  revision: number;
  hash: string;
  size: number;
  modifiedAt: number;
  deviceId: string;
  tempPath: string;
  backupPath: string;
  stage: "downloading" | "verified" | "replacing";
  createdAt: number;
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
  outbox: Record<string, OutboxOperation>;
  inbox: Record<string, InboxDownload>;
  pendingRenames: Record<string, PendingRename>;
  paused: boolean;
  activities: ActivityRecord[];
  conflicts: ConflictRecord[];
  lastAcknowledgedRevision: number;
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
  operation_kind?: string;
  restored_from_revision?: number;
}

export interface MutationResponse {
  change: Change;
  related_changes?: Change[];
  changed: boolean;
}

export interface BatchMutationResponse {
  changes: Change[];
  changed: boolean;
}

export interface ChangesResponse {
  changes: Change[];
  cursor: number;
  has_more: boolean;
}

export interface StatusResponse {
  latest_revision: number;
  max_file_bytes: number;
}

export interface ServerInfoResponse {
  server_version: string;
  protocol: { version: number };
  capabilities: string[];
  database: { schema_version: number };
  limits: {
    max_file_bytes: number;
    max_page_size: number;
    chunk_size?: number;
    max_chunk_query?: number;
    chunk_concurrency?: number;
  };
}

export interface PairResponse {
  vault: VaultInfo;
  device: DeviceInfo;
  token: string;
}

export interface VaultInfo {
  id: string;
  display_name: string;
  quota_bytes: number;
  max_files: number;
  status: string;
}

export interface DeviceInfo {
  vault_id: string;
  id: string;
  name: string;
  platform: string;
  client_version: string;
  status: string;
  registered_at: number;
  last_seen_at: number;
  last_ack_revision: number;
}

export interface HistoryPage {
  versions: Change[];
  cursor: number;
  has_more: boolean;
}

export type ActivityStatus = "running" | "completed" | "failed" | "paused" | "cancelled";

export interface ActivityRecord {
  id: string;
  startedAt: number;
  completedAt?: number;
  status: ActivityStatus;
  phase: SyncPhase;
  summary?: SyncSummary;
  errorCode?: string;
}

export type SyncPhase = "idle" | "scanning" | "comparing" | "uploading" | "downloading" | "applying" | "paused" | "error";

export interface SyncProgress {
  phase: SyncPhase;
  completedFiles: number;
  totalFiles: number;
  completedBytes: number;
  totalBytes: number;
  currentPathHash?: string;
}

export interface ConflictRecord {
  id: string;
  originalPath: string;
  conflictPath: string;
  localRevision: number;
  remoteRevision: number;
  localHash: string;
  remoteHash: string;
  remoteDeviceId: string;
  createdAt: number;
  resolvedAt?: number;
  resolution?: "local" | "remote" | "both" | "manual";
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
	restartPaths: string[];
  renamed: number;
}
