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
  });
});
