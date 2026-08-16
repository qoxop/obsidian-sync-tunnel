import { requestUrl, RequestUrlParam, RequestUrlResponse } from "obsidian";

import { Change, ChangesResponse, ChunkRef, MutationResponse, ServerInfoResponse, SnapshotResponse, StatusResponse } from "./types";

interface ClientOptions {
  serverUrl: string;
  vaultId: string;
  deviceId: string;
  token: string;
  accessClientId?: string;
  accessClientSecret?: string;
}

interface ErrorBody {
  error?: { code?: string; message?: string };
  current?: Change;
}

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string,
    readonly current?: Change
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export class SyncApiClient {
  private readonly baseUrl: string;

  constructor(private readonly options: ClientOptions) {
    this.baseUrl = options.serverUrl.replace(/\/+$/u, "");
  }

  async status(): Promise<StatusResponse> {
    return this.jsonRequest<StatusResponse>({
      url: `${this.vaultUrl()}/status`,
      method: "GET"
    });
  }

  async serverInfo(): Promise<ServerInfoResponse> {
    return this.jsonRequest<ServerInfoResponse>({
      url: `${this.baseUrl}/api/v2/server-info`,
      method: "GET"
    });
  }

  async listSnapshot(at?: number, after = "", limit = 250): Promise<SnapshotResponse> {
    const query = new URLSearchParams({ limit: String(limit) });
    if (at !== undefined) query.set("at", String(at));
    if (after) query.set("after", after);
    return this.jsonRequest<SnapshotResponse>({
      url: `${this.baseUrl}/api/v2/vaults/${encodeURIComponent(this.options.vaultId)}/snapshot?${query.toString()}`,
      method: "GET"
    });
  }

  async listChanges(after: number, limit = 250): Promise<ChangesResponse> {
    return this.jsonRequest<ChangesResponse>({
      url: `${this.vaultUrl()}/changes?after=${after}&limit=${limit}`,
      method: "GET"
    });
  }

  async findOperation(operationId: string): Promise<MutationResponse | null> {
    try {
      return await this.jsonRequest<MutationResponse>({
        url: `${this.baseUrl}/api/v2/vaults/${encodeURIComponent(this.options.vaultId)}/operations/${encodeURIComponent(operationId)}`,
        method: "GET",
        headers: { "X-Device-ID": this.options.deviceId }
      });
    } catch (error) {
      if (error instanceof ApiError && error.status === 404 && error.code === "not_found") return null;
      throw error;
    }
  }

  async missingChunks(hashes: string[]): Promise<string[]> {
    const response = await this.jsonRequest<{ missing: string[] }>({
      url: `${this.baseUrl}/api/v2/vaults/${encodeURIComponent(this.options.vaultId)}/chunks/missing`,
      method: "POST",
      contentType: "application/json",
      body: JSON.stringify({ hashes })
    });
    return response.missing;
  }

  async putChunk(hash: string, data: ArrayBuffer): Promise<void> {
    await this.jsonRequest<{ changed: boolean }>({
      url: `${this.baseUrl}/api/v2/vaults/${encodeURIComponent(this.options.vaultId)}/chunks/${encodeURIComponent(hash)}`,
      method: "PUT",
      contentType: "application/octet-stream",
      body: data
    });
  }

  async commitManifest(
    path: string,
    baseRevision: number,
    modifiedAt: number,
    hash: string,
    size: number,
    chunks: ChunkRef[],
    operationId: string
  ): Promise<MutationResponse> {
    return this.jsonRequest<MutationResponse>({
      url: `${this.baseUrl}/api/v2/vaults/${encodeURIComponent(this.options.vaultId)}/files/commit?path=${encodeURIComponent(path)}`,
      method: "POST",
      contentType: "application/json",
      headers: this.mutationHeaders(operationId, baseRevision, modifiedAt, { "X-Content-SHA256": hash }),
      body: JSON.stringify({ size, chunks })
    });
  }

  async downloadChunk(hash: string): Promise<ArrayBuffer> {
    const response = await this.request({
      url: `${this.baseUrl}/api/v2/vaults/${encodeURIComponent(this.options.vaultId)}/chunks/${encodeURIComponent(hash)}`,
      method: "GET"
    });
    return response.arrayBuffer;
  }

  async findManifest(hash: string): Promise<{ size: number; chunks: ChunkRef[] } | null> {
    try {
      return await this.jsonRequest<{ size: number; chunks: ChunkRef[] }>({
        url: `${this.baseUrl}/api/v2/vaults/${encodeURIComponent(this.options.vaultId)}/manifests/${encodeURIComponent(hash)}`,
        method: "GET"
      });
    } catch (error) {
      if (error instanceof ApiError && error.status === 404 && error.code === "not_found") return null;
      throw error;
    }
  }

  async putFile(path: string, baseRevision: number, modifiedAt: number, hash: string, data: ArrayBuffer, operationId: string = crypto.randomUUID()): Promise<MutationResponse> {
    return this.jsonRequest<MutationResponse>({
      url: `${this.vaultUrl()}/file?path=${encodeURIComponent(path)}`,
      method: "PUT",
      contentType: "application/octet-stream",
      headers: this.mutationHeaders(operationId, baseRevision, modifiedAt, { "X-Content-SHA256": hash }),
      body: data
    });
  }

  async deleteFile(path: string, baseRevision: number, modifiedAt: number, operationId: string = crypto.randomUUID()): Promise<MutationResponse> {
    return this.jsonRequest<MutationResponse>({
      url: `${this.vaultUrl()}/file?path=${encodeURIComponent(path)}`,
      method: "DELETE",
      headers: this.mutationHeaders(operationId, baseRevision, modifiedAt)
    });
  }

  async downloadBlob(hash: string): Promise<ArrayBuffer> {
    const response = await this.request({
      url: `${this.vaultUrl()}/blobs/${encodeURIComponent(hash)}`,
      method: "GET"
    });
    return response.arrayBuffer;
  }

  private vaultUrl(): string {
    return `${this.baseUrl}/api/v1/vaults/${encodeURIComponent(this.options.vaultId)}`;
  }

  private mutationHeaders(operationId: string, baseRevision: number, modifiedAt: number, extra: Record<string, string> = {}): Record<string, string> {
    return {
      "X-Device-ID": this.options.deviceId,
      "X-Operation-ID": operationId,
      "X-Base-Revision": String(baseRevision),
      "X-Modified-At": String(modifiedAt),
      ...extra
    };
  }

  private headers(extra: Record<string, string> = {}): Record<string, string> {
    const headers: Record<string, string> = {
      Authorization: `Bearer ${this.options.token}`,
      ...extra
    };
    if (this.options.accessClientId && this.options.accessClientSecret) {
      headers["CF-Access-Client-Id"] = this.options.accessClientId;
      headers["CF-Access-Client-Secret"] = this.options.accessClientSecret;
    }
    return headers;
  }

  private async jsonRequest<T>(parameters: RequestUrlParam): Promise<T> {
    const response = await this.request(parameters);
    try {
      return response.json as T;
    } catch {
      throw new ApiError("Server returned invalid JSON", response.status, "invalid_response");
    }
  }

  private async request(parameters: RequestUrlParam): Promise<RequestUrlResponse> {
    let lastError: unknown;
    for (let attempt = 0; attempt < 3; attempt += 1) {
      try {
        const response = await requestUrl({
          ...parameters,
          headers: this.headers(parameters.headers),
          throw: false
        });
        if (response.status >= 200 && response.status < 300) return response;
        const body = safelyReadError(response);
        const error = new ApiError(
          body.error?.message ?? `Server returned HTTP ${response.status}`,
          response.status,
          body.error?.code ?? "http_error",
          body.current
        );
        if (response.status !== 429 && response.status < 500) throw error;
        lastError = error;
      } catch (error) {
        if (error instanceof ApiError && error.status < 500 && error.status !== 429) throw error;
        lastError = error;
      }
      await delay(500 * 2 ** attempt);
    }
    if (lastError instanceof Error) throw lastError;
    throw new ApiError("Request failed", 0, "network_error");
  }
}

function safelyReadError(response: RequestUrlResponse): ErrorBody {
  try {
    return response.json as ErrorBody;
  } catch {
    return {};
  }
}

function delay(milliseconds: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, milliseconds));
}
