export interface Vault {
  id: string;
  display_name: string;
  quota_bytes: number;
  max_files: number;
  status: "active" | "suspended";
  created_at: number;
  updated_at: number;
}

export interface Device {
  vault_id: string;
  id: string;
  name: string;
  platform: string;
  client_version: string;
  status: "active" | "retired" | "revoked";
  registered_at: number;
  last_seen_at: number;
  last_ack_revision: number;
  retired_at?: number;
  revoked_at?: number;
}

export interface AuditEvent {
  id: number;
  event_type: string;
  vault_id?: string;
  device_id?: string;
  actor: string;
  request_id?: string;
  details: Record<string, unknown>;
  created_at: number;
}

export interface ServerStats {
  vaults: number;
  active_devices: number;
  current_files: number;
  logical_bytes: number;
  revisions: number;
  chunks: number;
  chunk_bytes: number;
}

export interface DoctorReport {
  ok: boolean;
  integrity: string;
  missing_chunk_hashes: string[];
  corrupt_chunk_hashes: string[];
  orphan_chunk_files: string[];
}

export interface VaultWatermark {
  vault_id: string;
  revision: number;
}

export interface GCPlan {
  id: string;
  hash: string;
  created_at: number;
  retention_days: number;
  keep_versions: number;
  safe_revision: number;
  vault_watermarks: VaultWatermark[];
  change_revisions: number[];
  deleted_paths: Array<{ vault_id: string; path: string }>;
  blob_hashes: string[];
  manifest_hashes: string[];
  chunk_hashes: string[];
  operation_cutoff: number;
  estimated_bytes: number;
}

export interface GCResult {
  plan_id: string;
  changes_deleted: number;
  paths_deleted: number;
  blobs_deleted: number;
  manifests_deleted: number;
  chunks_deleted: number;
  operations_deleted: number;
  bytes_reclaimed: number;
}

export interface BackupManifest {
  format_version: number;
  created_at: number;
  schema_version: number;
  files: Record<string, string>;
}

export interface BackupRun {
  id: string;
  destination: string;
  status: string;
  manifest_hash?: string;
  created_at: number;
  completed_at?: number;
  error_text?: string;
}

export interface BackupResult {
  destination: string;
  manifest: BackupManifest;
}

export interface AdminSession {
  authentication: "none" | "token";
  local_only: boolean;
}

export interface ServerLogEntry {
  time?: string;
  level?: string;
  msg?: string;
  [key: string]: unknown;
}

export type ConnectivityCheckStatus = "pass" | "warning" | "fail" | "info";

export interface ConnectivityCheck {
  id: string;
  label: string;
  status: ConnectivityCheckStatus;
  detail: string;
  suggestion?: string;
}

export interface ConnectivityReport {
  checked_at: number;
  overall: "healthy" | "warning" | "error";
  summary: string;
  checks: ConnectivityCheck[];
}
