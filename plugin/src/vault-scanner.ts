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
    const paths = requestedPaths ?? await this.listAllFiles();
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
    return /(?:^|\/)\.sync-tunnel-(?:download|backup)-[0-9a-f-]+\.tmp$/iu.test(normalized)
      || this.protectedPaths.some((protectedPath) => pathIsWithin(normalized, protectedPath))
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

  private async listAllFiles(): Promise<string[]> {
    const visited = new Set<string>();
    const files = await this.listFiles("", visited);
    const configDirectory = normalizeVaultPath(this.vault.configDir ?? "");
    if (configDirectory && !this.isDirectoryExcluded(configDirectory)) {
      files.push(...await this.listFiles(configDirectory, visited));
    }
    return [...new Set(files)];
  }

  private async listFiles(directory: string, visited = new Set<string>()): Promise<string[]> {
    const normalizedDirectory = normalizeVaultPath(directory);
    if (visited.has(normalizedDirectory)) return [];
    visited.add(normalizedDirectory);
    const listed = await this.adapter.list(normalizedDirectory);
    const files = listed.files.map(normalizeVaultPath);
    for (const folder of listed.folders) {
      const normalized = normalizeVaultPath(folder);
      if (this.isDirectoryExcluded(normalized)) continue;
      files.push(...(await this.listFiles(normalized, visited)));
    }
    return files;
  }

  private isDirectoryExcluded(path: string): boolean {
    const normalized = normalizeVaultPath(path);
    return this.protectedPaths.some((protectedPath) => pathIsWithin(normalized, protectedPath))
      || this.excludedPatterns.some((pattern) =>
        globMatches(normalized, pattern) || globMatches(`${normalized}/placeholder`, pattern))
      || this.profileExcludesDirectory(normalized);
  }

  private profileExcludesDirectory(path: string): boolean {
    if (this.syncProfile === "full" || this.syncProfile === "custom") return false;
    const configDirectory = normalizeVaultPath(this.vault.configDir);
    if (!pathIsWithin(path, configDirectory)) return false;
    if (this.syncProfile === "notes") return true;
    const relative = path.slice(configDirectory.length).replace(/^\/+/, "");
    if (!relative || relative === "plugins" || /^plugins\/[^/]+$/u.test(relative)) return false;
    if (pathIsWithin(relative, "themes") || pathIsWithin(relative, "snippets")) return false;
    return true;
  }
}
