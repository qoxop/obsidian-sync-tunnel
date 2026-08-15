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
  excludedPatterns: string[];
}

export interface FileState {
  hash: string;
  revision: number;
  size: number;
  modifiedAt: number;
  deleted: boolean;
}

export interface PersistedData {
  schemaVersion: number;
  settings: PluginSettings;
  cursor: number;
  files: Record<string, FileState>;
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

export interface SyncSummary {
  uploaded: number;
  downloaded: number;
  deletedRemote: number;
  deletedLocal: number;
  conflicts: number;
  skipped: number;
}
