import { DataAdapter, Vault } from "obsidian";

import { sha256 } from "./hash";
import { assertNoPortablePathCollisions, globMatches, normalizeVaultPath, pathIsWithin } from "./path";
import { LocalFile, ScanCacheEntry, SyncProfile } from "./types";

export interface ScanOptions {
  cache?: Record<string, ScanCacheEntry>;
  paths?: Iterable<string>;
  forceHash?: boolean;
}

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

  async scan(options: ScanOptions = {}): Promise<Map<string, LocalFile>> {
    const requestedPaths = options.paths
      ? [...new Set([...options.paths].map(normalizeVaultPath))]
      : undefined;
    const paths = requestedPaths ?? await this.listFiles("");
    const includedPaths = paths.filter((path) => !this.isExcluded(path));
    const entries = new Map<string, LocalFile>();
    for (const path of includedPaths.sort((left, right) => left.localeCompare(right))) {
      const stat = await this.adapter.stat(path);
      if (!stat || stat.type !== "file") continue;
      const cached = options.cache?.[path];
      const canReuseHash = !options.forceHash
        && cached
        && cached.size === stat.size
        && cached.modifiedAt === stat.mtime;
      const hash = canReuseHash
        ? cached.hash
        : await sha256(await this.adapter.readBinary(path));
      entries.set(path, {
        path,
        hash,
        size: stat.size,
        modifiedAt: stat.mtime
      });
    }
    const knownPaths = requestedPaths
      ? new Set(Object.keys(options.cache ?? {}))
      : new Set<string>();
    if (requestedPaths) {
      for (const path of includedPaths) {
        if (entries.has(path)) knownPaths.add(path);
        else knownPaths.delete(path);
      }
    } else {
      for (const path of entries.keys()) knownPaths.add(path);
    }
    assertNoPortablePathCollisions([...knownPaths]);
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
