import type {
  AdminSession,
  AuditEvent,
  BackupResult,
  BackupRun,
  ConnectivityReport,
  Device,
  DoctorReport,
  GCPlan,
  GCResult,
  ServerLogEntry,
  ServerStats,
  Vault
} from "./types";

interface APIErrorBody {
  error?: {
    code?: string;
    message?: string;
  };
}

export class AdminAPIError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string
  ) {
    super(message);
    this.name = "AdminAPIError";
  }
}

export class AdminAPI {
  constructor(private readonly token: string) {}

  stats(): Promise<ServerStats> {
    return this.request("/stats");
  }

  doctor(): Promise<DoctorReport> {
    return this.request("/doctor");
  }

  async vaults(): Promise<Vault[]> {
    const response = await this.request<{ vaults: Vault[] }>("/vaults");
    return response.vaults;
  }

  createVault(input: { id: string; display_name: string; quota_bytes: number; max_files: number }): Promise<Vault> {
    return this.request("/vaults", { method: "POST", body: JSON.stringify(input) });
  }

  updateVault(vaultID: string, input: { display_name: string; status: Vault["status"]; quota_bytes: number; max_files: number }): Promise<Vault> {
    return this.request(`/vaults/${encodeURIComponent(vaultID)}`, { method: "PUT", body: JSON.stringify(input) });
  }

  async devices(vaultID: string): Promise<Device[]> {
    const response = await this.request<{ devices: Device[] }>(`/vaults/${encodeURIComponent(vaultID)}/devices`);
    return response.devices;
  }

  setDeviceStatus(vaultID: string, deviceID: string, status: "retired" | "revoked"): Promise<void> {
    return this.request(`/vaults/${encodeURIComponent(vaultID)}/devices/${encodeURIComponent(deviceID)}/status`, {
      method: "POST",
      body: JSON.stringify({ status })
    });
  }

  createPairingCode(vaultID: string, ttlSeconds: number, scopes: string): Promise<{ code: string; expires_at: number }> {
    return this.request(`/vaults/${encodeURIComponent(vaultID)}/pairing-codes`, {
      method: "POST",
      body: JSON.stringify({ ttl_seconds: ttlSeconds, scopes })
    });
  }

  async audit(limit: number): Promise<AuditEvent[]> {
    const response = await this.request<{ events: AuditEvent[] }>(`/audit?limit=${limit}`);
    return response.events;
  }

  async logs(limit: number): Promise<ServerLogEntry[]> {
    const response = await this.request<{ entries: ServerLogEntry[] }>(`/logs?limit=${limit}`);
    return response.entries;
  }

  planGC(retentionDays: number, keepVersions: number): Promise<GCPlan> {
    return this.request("/gc/plans", {
      method: "POST",
      body: JSON.stringify({ retention_days: retentionDays, keep_versions: keepVersions })
    });
  }

  executeGC(planID: string, planHash: string): Promise<GCResult> {
    return this.request(`/gc/plans/${encodeURIComponent(planID)}/execute`, {
      method: "POST",
      body: JSON.stringify({ plan_hash: planHash })
    });
  }

  async backups(limit = 50): Promise<BackupRun[]> {
    const response = await this.request<{ backups: BackupRun[] }>(`/backups?limit=${limit}`);
    return response.backups;
  }

  createBackup(): Promise<BackupResult> {
    return this.request("/backups", { method: "POST", body: "{}" });
  }

  verifyBackup(destination: string): Promise<BackupResult> {
    return this.request("/backups/verify", {
      method: "POST",
      body: JSON.stringify({ destination })
    });
  }

  checkConnectivity(input: { public_url: string; access_client_id?: string; access_client_secret?: string }): Promise<ConnectivityReport> {
    return this.request("/connectivity/check", {
      method: "POST",
      body: JSON.stringify(input)
    });
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers);
    if (this.token) headers.set("Authorization", `Bearer ${this.token}`);
    if (init.body) headers.set("Content-Type", "application/json");
    const response = await fetch(`/admin/v1${path}`, {
      ...init,
      cache: "no-store",
      credentials: "same-origin",
      headers
    });
    if (!response.ok) {
      let body: APIErrorBody = {};
      try {
        body = await response.json() as APIErrorBody;
      } catch {
        // The status and a generic message remain available.
      }
      throw new AdminAPIError(
        body.error?.message || `管理请求失败（HTTP ${response.status}）`,
        response.status,
        body.error?.code || "request_failed"
      );
    }
    if (response.status === 204) return undefined as T;
    return await response.json() as T;
  }
}

export async function getAdminSession(): Promise<AdminSession> {
  const response = await fetch("/admin/v1/session", {
    cache: "no-store",
    credentials: "same-origin"
  });
  if (!response.ok) {
    throw new AdminAPIError(`无法读取管理端配置（HTTP ${response.status}）`, response.status, "session_failed");
  }
  return await response.json() as AdminSession;
}
