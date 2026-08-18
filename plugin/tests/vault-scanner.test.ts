import type { DataAdapter, Vault } from "obsidian";
import { describe, expect, it } from "vitest";

import { VaultScanner } from "../src/vault-scanner";

describe("VaultScanner", () => {
  it("never descends into protected plugin directories", async () => {
    const listings: Record<string, { files: string[]; folders: string[] }> = {
      "": { files: ["note.md"], folders: [".obsidian"] },
      ".obsidian": { files: [".obsidian/app.json"], folders: [".obsidian/plugins"] },
      ".obsidian/plugins": {
        files: [],
        folders: [".obsidian/plugins/sync-tunnel", ".obsidian/plugins/another-plugin"]
      },
      ".obsidian/plugins/another-plugin": {
        files: [".obsidian/plugins/another-plugin/main.js"],
        folders: []
      }
    };
    const content = new TextEncoder().encode("content").buffer;
    const visited: string[] = [];
    const adapter = {
      list: async (path: string) => {
        visited.push(path);
        const result = listings[path];
        if (!result) throw new Error(`Unexpected directory ${path}`);
        return result;
      },
      stat: async () => ({ type: "file", ctime: 1, mtime: 1, size: content.byteLength }),
      readBinary: async () => content
    } as unknown as DataAdapter;
    const vault = { adapter } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], [".obsidian/plugins/sync-tunnel"]);

    const result = await scanner.scan();

    expect([...result.keys()]).toEqual([
      ".obsidian/app.json",
      ".obsidian/plugins/another-plugin/main.js",
      "note.md"
    ]);
    expect(visited).not.toContain(".obsidian/plugins/sync-tunnel");
  });

  it("uses the recommended profile to include plugin bundles but exclude plugin data", () => {
    const vault = { configDir: ".config", adapter: {} } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], [".config/plugins/sync-tunnel"], "recommended");

    expect(scanner.isExcluded("note.md")).toBe(false);
    expect(scanner.isExcluded(".config/app.json")).toBe(false);
    expect(scanner.isExcluded(".config/workspace.json")).toBe(true);
    expect(scanner.isExcluded(".config/plugins/example/main.js")).toBe(false);
    expect(scanner.isExcluded(".config/plugins/example/manifest.json")).toBe(false);
    expect(scanner.isExcluded(".config/plugins/example/styles.css")).toBe(false);
    expect(scanner.isExcluded(".config/plugins/example/data.json")).toBe(true);
    expect(scanner.isExcluded(".config/plugins/example/assets/icon.png")).toBe(true);
    expect(scanner.isExcluded(".config/plugins/sync-tunnel/main.js")).toBe(true);
    expect(scanner.isExcluded(".config/themes/example/theme.css")).toBe(false);
    expect(scanner.isExcluded(".config/snippets/example.css")).toBe(false);
    expect(scanner.isExcluded("folder/.sync-tunnel-download-11111111-1111-4111-8111-111111111111.tmp")).toBe(true);
    expect(scanner.isExcluded(".sync-tunnel-backup-11111111-1111-4111-8111-111111111111.tmp")).toBe(true);
  });

  it("descends into recommended profile directories that contain allowed files", async () => {
    const listings: Record<string, { files: string[]; folders: string[] }> = {
      // Obsidian adapters can hide the active config directory from the root listing.
      "": { files: ["note.md"], folders: [] },
      ".config": {
        files: [".config/app.json", ".config/workspace.json"],
        folders: [".config/plugins", ".config/themes", ".config/snippets", ".config/private"]
      },
      ".config/plugins": {
        files: [],
        folders: [".config/plugins/example", ".config/plugins/sync-tunnel"]
      },
      ".config/plugins/example": {
        files: [
          ".config/plugins/example/main.js",
          ".config/plugins/example/manifest.json",
          ".config/plugins/example/styles.css",
          ".config/plugins/example/data.json"
        ],
        folders: [".config/plugins/example/assets"]
      },
      ".config/themes": { files: [], folders: [".config/themes/example"] },
      ".config/themes/example": { files: [".config/themes/example/theme.css"], folders: [] },
      ".config/snippets": { files: [".config/snippets/example.css"], folders: [] }
    };
    const content = new TextEncoder().encode("content").buffer;
    const visited: string[] = [];
    const adapter = {
      list: async (path: string) => {
        visited.push(path);
        const result = listings[path];
        if (!result) throw new Error(`Unexpected directory ${path}`);
        return result;
      },
      stat: async () => ({ type: "file", ctime: 1, mtime: 1, size: content.byteLength }),
      readBinary: async () => content
    } as unknown as DataAdapter;
    const vault = { configDir: ".config", adapter } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], [".config/plugins/sync-tunnel"], "recommended");

    const result = await scanner.scan();

    expect([...result.keys()]).toEqual([
      ".config/app.json",
      ".config/plugins/example/main.js",
      ".config/plugins/example/manifest.json",
      ".config/plugins/example/styles.css",
      ".config/snippets/example.css",
      ".config/themes/example/theme.css",
      "note.md"
    ]);
    expect(visited).not.toContain(".config/plugins/example/assets");
    expect(visited).not.toContain(".config/plugins/sync-tunnel");
    expect(visited).not.toContain(".config/private");
  });

  it("reuses a cached hash when file metadata is unchanged", async () => {
    let readCount = 0;
    const adapter = {
      list: async () => ({ files: ["note.md"], folders: [] }),
      stat: async () => ({ type: "file", ctime: 1, mtime: 42, size: 7 }),
      readBinary: async () => {
        readCount += 1;
        return new TextEncoder().encode("content").buffer;
      }
    } as unknown as DataAdapter;
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);

    const result = await scanner.scan({
      cache: { "note.md": { hash: "cached-hash", size: 7, modifiedAt: 42 } }
    });

    expect(result.get("note.md")?.hash).toBe("cached-hash");
    expect(readCount).toBe(0);
  });

  it("rehashes an explicitly queued path even when metadata is unchanged", async () => {
    let readCount = 0;
    const adapter = {
      stat: async () => ({ type: "file", ctime: 1, mtime: 42, size: 7 }),
      readBinary: async () => {
        readCount += 1;
        return new TextEncoder().encode("changed").buffer;
      }
    } as unknown as DataAdapter;
    const vault = { adapter, configDir: ".obsidian" } as unknown as Vault;
    const scanner = new VaultScanner(vault, [], []);

    const result = await scanner.scan({
      cache: { "note.md": { hash: "old-hash", size: 7, modifiedAt: 42 } },
      paths: ["note.md"],
      forceHash: true
    });

    expect(result.get("note.md")?.hash).not.toBe("old-hash");
    expect(readCount).toBe(1);
  });
});
