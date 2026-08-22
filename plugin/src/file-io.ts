import { FileSystemAdapter, Platform, Vault } from "obsidian";

import { sha256 } from "./hash";

type DesktopModuleLoader = (moduleId: string) => unknown;

function desktopModule<T>(moduleId: string): T {
  const loader = (globalThis as typeof globalThis & { window?: { require?: DesktopModuleLoader } }).window?.require;
  if (typeof loader !== "function") throw new Error(`Desktop module loader unavailable for ${moduleId}`);
  return loader(moduleId) as T;
}

export async function hashVaultFile(vault: Vault, path: string): Promise<string> {
  if (!Platform.isDesktopApp || !(vault.adapter instanceof FileSystemAdapter)) {
    return sha256(await vault.adapter.readBinary(path));
  }
  const { createReadStream } = desktopModule<typeof import("node:fs")>("node:fs");
  const { createHash } = desktopModule<typeof import("node:crypto")>("node:crypto");
  const digest = createHash("sha256");
  for await (const chunk of createReadStream(vault.adapter.getFullPath(path))) digest.update(chunk);
  return digest.digest("hex");
}
