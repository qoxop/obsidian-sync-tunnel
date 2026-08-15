import { DataAdapter, Vault } from "obsidian";

import { sha256 } from "./hash";
import { globMatches, normalizeVaultPath } from "./path";
import { LocalFile } from "./types";

export class VaultScanner {
  private readonly adapter: DataAdapter;

  constructor(
    private readonly vault: Vault,
    private readonly excludedPatterns: string[],
    private readonly protectedPaths: string[]
  ) {
    this.adapter = vault.adapter;
  }

  async scan(): Promise<Map<string, LocalFile>> {
    const paths = await this.listFiles("");
    const entries = new Map<string, LocalFile>();
    for (const path of paths.sort((left, right) => left.localeCompare(right))) {
      if (this.isExcluded(path)) continue;
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
    return this.protectedPaths.includes(normalized) || this.excludedPatterns.some((pattern) => globMatches(normalized, pattern));
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
