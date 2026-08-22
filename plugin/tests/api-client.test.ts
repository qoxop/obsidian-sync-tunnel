import { afterEach, describe, expect, it } from "vitest";

import { SyncApiClient } from "../src/api-client";
import { resetRequestUrlHandler, setRequestUrlHandler } from "./obsidian-stub";

interface CapturedRequest {
  url: string;
  method: string;
  headers: Record<string, string>;
  body?: unknown;
}

describe("SyncApiClient final 1.0 protocol", () => {
  afterEach(() => resetRequestUrlHandler());

  it("uses only /api/v1, a device credential, and Cloudflare Access headers", async () => {
    const requests: CapturedRequest[] = [];
    setRequestUrlHandler(async (raw) => {
      const request = raw as unknown as CapturedRequest;
      requests.push(request);
      if (request.url.endsWith("/server-info")) return response(200, finalServerInfo());
      if (request.url.includes("/files/content")) return response(200, mutation("note.md", 1));
      if (request.url.endsWith("/ack")) return response(204, {});
      throw new Error(`Unexpected URL ${request.url}`);
    });
    const client = createClient();

    await client.serverInfo();
    await client.putFile("目录/note.md", 0, 123, "a".repeat(64), new Uint8Array([1]).buffer, "11111111-1111-4111-8111-111111111111");
    await client.acknowledge(1);

    expect(requests).toHaveLength(3);
    for (const request of requests) {
      expect(request.url).toContain("/api/v1/");
      expect(request.url).not.toContain("/api/v2/");
      expect(request.headers.Authorization).toBe("Bearer device-credential");
      expect(request.headers["CF-Access-Client-Id"]).toBe("access-id");
      expect(request.headers["CF-Access-Client-Secret"]).toBe("access-secret");
      expect(request.headers["X-Device-ID"]).toBeUndefined();
    }
    expect(requests[1]?.url).toContain("path=%E7%9B%AE%E5%BD%95%2Fnote.md");
    expect(requests[1]?.headers["X-Operation-ID"]).toBe("11111111-1111-4111-8111-111111111111");
  });

  it("pairs without a bearer token and accepts the server-assigned device identity", async () => {
    let captured: CapturedRequest | undefined;
    setRequestUrlHandler(async (raw) => {
      captured = raw as unknown as CapturedRequest;
      return response(201, {
        vault: { id: "vault-a", display_name: "Vault A", quota_bytes: 0, max_files: 0, status: "active" },
        device: { vault_id: "vault-a", id: "server-device", name: "Mac", platform: "macOS", client_version: "1.0.0", status: "active", registered_at: 1, last_seen_at: 1, last_ack_revision: 0 },
        token: "new-device-token"
      });
    });

    const paired = await createClient().pairDevice("PAIR-CODE", "Mac", "macOS", "1.0.0");

    expect(captured?.url).toBe("https://sync.example.com/api/v1/pair");
    expect(captured?.headers.Authorization).toBeUndefined();
    expect(captured?.headers["CF-Access-Client-Id"]).toBe("access-id");
    expect(JSON.parse(String(captured?.body))).toMatchObject({ vault_id: "vault-a", code: "PAIR-CODE", device_name: "Mac" });
    expect(paired.device.id).toBe("server-device");
    expect(paired.token).toBe("new-device-token");
  });

  it("surfaces structured non-retryable API errors", async () => {
    setRequestUrlHandler(async () => response(409, {
      error: { code: "revision_conflict", message: "stale revision" },
      current: mutation("note.md", 9).change
    }));

    await expect(createClient().deleteFile("note.md", 1, 2)).rejects.toMatchObject({
      status: 409,
      code: "revision_conflict",
      message: "stale revision",
      current: { revision: 9, path: "note.md" }
    });
  });
});

function createClient(): SyncApiClient {
  return new SyncApiClient({
    serverUrl: "https://sync.example.com/",
    vaultId: "vault-a",
    token: "device-credential",
    accessClientId: "access-id",
    accessClientSecret: "access-secret"
  });
}

function response(status: number, json: unknown) {
  const text = JSON.stringify(json);
  return { status, json, text, arrayBuffer: new TextEncoder().encode(text).buffer, headers: {} };
}

function mutation(path: string, revision: number) {
  return {
    changed: true,
    change: { revision, path, blob_hash: "a".repeat(64), size: 1, modified_at: 1, deleted: false, device_id: "device" }
  };
}

function finalServerInfo() {
  return {
    server_version: "test",
    protocol: { version: 1 },
    capabilities: ["snapshot", "idempotent-operations", "chunk-transfer", "rename", "batch-delete", "device-ack"],
    database: { schema_version: 7 },
    limits: { max_file_bytes: 1024, max_page_size: 1000, chunk_size: 4, max_chunk_query: 1000, chunk_concurrency: 2 }
  };
}
