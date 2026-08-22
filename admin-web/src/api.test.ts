import { afterEach, describe, expect, it, vi } from "vitest";
import { AdminAPI, getAdminSession } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("admin API", () => {
  it("discovers tokenless local mode", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ authentication: "none", local_only: true }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(getAdminSession()).resolves.toEqual({ authentication: "none", local_only: true });
    expect(fetchMock).toHaveBeenCalledWith("/admin/v1/session", expect.objectContaining({ credentials: "same-origin" }));
  });

  it("omits Authorization when token mode is disabled", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ vaults: 0 }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await new AdminAPI("").stats();
	const headers = fetchMock.mock.calls[0]![1].headers as Headers;
	expect(headers.has("Authorization")).toBe(false);
  });

  it("sends the token only when configured", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ vaults: 0 }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await new AdminAPI("secret").stats();
	const headers = fetchMock.mock.calls[0]![1].headers as Headers;
    expect(headers.get("Authorization")).toBe("Bearer secret");
  });

  it("loads recent structured server logs", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ entries: [{ level: "INFO", msg: "ready" }] }), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(new AdminAPI("").logs(200)).resolves.toEqual([{ level: "INFO", msg: "ready" }]);
    expect(fetchMock.mock.calls[0]![0]).toBe("/admin/v1/logs?limit=200");
  });

  it("runs a connectivity check without persisting Access credentials", async () => {
    const report = { checked_at: 1, overall: "healthy", summary: "ok", checks: [] };
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(report), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(new AdminAPI("").checkConnectivity({
      public_url: "https://sync.example.com",
      access_client_id: "client-id",
      access_client_secret: "client-secret"
    })).resolves.toEqual(report);
    expect(fetchMock.mock.calls[0]![0]).toBe("/admin/v1/connectivity/check");
    expect(fetchMock.mock.calls[0]![1]).toEqual(expect.objectContaining({ method: "POST" }));
  });
});
