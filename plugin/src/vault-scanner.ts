import { DataAdapter, Vault } from "obsidian";

import { sha256 } from "./hash";
import { assertNoPortablePathCollisions, globMatches, normalizeVaultPath, pathIsWithin } from "./path";
import { LocalFile, SyncProfile } from "./types";

export class VaultScanner {
  private readonly adapter: DataAdapter;

  constructor(
    private readonly vault: Vault,
    private readonly excludedPatterns: string[],
    private readonly protectedPaths: string[],
    private readonly syncProfile: SyncProfile = "full"
  ) {
    this.adapter = vault.adapter;
  }

  async scan(): Promise<Map<string, LocalFile>> {
    const paths = await this.listFiles("");
    const includedPaths = paths.filter((path) => !this.isExcluded(path));
    assertNoPortablePathCollisions(includedPaths);
    const entries = new Map<string, LocalFile>();
    for (const path of includedPaths.sort((left, right) => left.localeCompare(right))) {
      const stat = await this.adapter.stat(path);
      if (!stat || stat.type !== "file") continue;
      const data = await this.adapter.readBinary(path);
      entries.set(path, {
        path,
        hash: await sha256(data),
        size: data.byteLength,
        modifiedAt: stat.mtime
      });
    }
    return entries;
  }

  isExcluded(path: string): boolean {
    const normalized = normalizeVaultPath(path);
    return this.protectedPaths.some((protectedPath) => pathIsWithin(normalized, protectedPath))
      || this.excludedPatterns.some((pattern) => globMatches(normalized, pattern))
      || this.profileExcludes(normalized);
  }

  filterFingerprint(): string {
    return JSON.stringify({
      excluded: this.excludedPatterns.map((pattern) => pattern.trim()).filter(Boolean).sort(),
      protected: this.protectedPaths.map(normalizeVaultPath).sort(),
      profile: this.syncProfile
    });
  }

  private profileExcludes(path: string): boolean {
    if (this.syncProfile === "full" || this.syncProfile === "custom") return false;
    const configDirectory = normalizeVaultPath(this.vault.configDir);
    if (!pathIsWithin(path, configDirectory)) return false;
    if (this.syncProfile === "notes") return true;
    const relative = path.slice(configDirectory.length).replace(/^\/+/, "");
    if ([
      "app.json",
      "appearance.json",
      "community-plugins.json",
      "core-plugins.json",
      "core-plugins-migration.json",
      "hotkeys.json"
    ].includes(relative)) return false;
    if (pathIsWithin(relative, "themes") || pathIsWithin(relative, "snippets")) return false;
    return !/^plugins\/[^/]+\/(?:main\.js|manifest\.json|styles\.css)$/u.test(relative);
  }

  private async listFiles(directory: string): Promise<string[]> {
    const listed = await this.adapter.list(directory);
    const files = listed.files.map(normalizeVaultPath);
    for (const folder of listed.folders) {
      const normalized = normalizeVaultPath(folder);
      if (this.isExcluded(`${normalized}/placeholder`) || this.isExcluded(normalized)) continue;
      files.push(...(await this.listFiles(normalized)));
    }
    return files;
  }
}
