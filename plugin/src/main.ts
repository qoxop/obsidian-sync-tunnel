import { Notice, Plugin } from "obsidian";

import { SyncApiClient } from "./api-client";
import { SyncTunnelSettingTab } from "./settings";
import { SyncEngine } from "./sync-engine";
import { PersistedData, PluginSettings, SyncSummary } from "./types";
import { VaultScanner } from "./vault-scanner";

const SCHEMA_VERSION = 1;

export default class SyncTunnelPlugin extends Plugin {
  data!: PersistedData;
  private statusElement?: HTMLElement;
  private timerId?: number;
  private syncPromise?: Promise<SyncSummary>;

  async onload(): Promise<void> {
    this.data = migrateData(await this.loadData());
    await this.savePluginData();

    this.statusElement = this.addStatusBarItem();
    this.setStatus("待同步");
    this.addRibbonIcon("refresh-cw", "Sync Tunnel: sync now", () => void this.runSync(true));
    this.addCommand({ id: "sync-now", name: "Sync now", callback: () => void this.runSync(true) });
    this.addSettingTab(new SyncTunnelSettingTab(this.app, this));

    this.app.workspace.onLayoutReady(() => {
      this.restartTimer();
      if (this.data.settings.syncOnStartup && this.isConfigured()) void this.runSync(false);
    });
  }

  onunload(): void {
    this.clearTimer();
  }

  async savePluginData(): Promise<void> {
    await this.saveData(this.data);
  }

  restartTimer(): void {
    this.clearTimer();
    if (!this.data.settings.automaticSync) return;
    const milliseconds = Math.max(30, this.data.settings.syncIntervalSeconds) * 1000;
    this.timerId = window.setInterval(() => {
      if (this.isConfigured()) void this.runSync(false);
    }, milliseconds);
    this.registerInterval(this.timerId);
  }

  async runSync(showNotice: boolean): Promise<SyncSummary | undefined> {
    if (this.syncPromise) {
      if (showNotice) new Notice("Sync Tunnel: 已有同步任务正在运行");
      return this.syncPromise;
    }
    try {
      const client = await this.createClient();
      const configDirectory = this.app.vault.configDir;
      const protectedPaths = [`${configDirectory}/plugins/${this.manifest.id}/data.json`];
      const scanner = new VaultScanner(this.app.vault, this.data.settings.excludedPatterns, protectedPaths);
      const engine = new SyncEngine(this.app.vault, this.data, client, scanner, () => this.savePluginData());
      this.setStatus("同步中…");
      this.syncPromise = engine.run();
      const summary = await this.syncPromise;
      const text = formatSummary(summary);
      this.setStatus(`已同步 ${new Date().toLocaleTimeString()}`);
      if (showNotice || summary.conflicts > 0) new Notice(`Sync Tunnel: ${text}`, summary.conflicts > 0 ? 10000 : 5000);
      return summary;
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      this.setStatus("同步失败");
      console.error("Sync Tunnel failed", error);
      if (showNotice) new Notice(`Sync Tunnel 失败: ${message}`, 10000);
      return undefined;
    } finally {
      this.syncPromise = undefined;
      await this.savePluginData();
    }
  }

  private async createClient(): Promise<SyncApiClient> {
    const settings = this.data.settings;
    if (!settings.serverUrl || !settings.vaultId || !settings.deviceId || !settings.apiTokenSecretName) {
      throw new Error("请先完整填写 Server URL、Vault ID、Device ID 和 API token")
    }
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/u.test(settings.vaultId)) throw new Error("Vault ID 格式无效");
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/u.test(settings.deviceId)) throw new Error("Device ID 格式无效");
    const token = this.app.secretStorage.getSecret(settings.apiTokenSecretName);
    if (!token) throw new Error("所选 API token SecretStorage 条目为空");
    const accessClientSecret = settings.accessClientSecretName
      ? this.app.secretStorage.getSecret(settings.accessClientSecretName) ?? undefined
      : undefined;
    return new SyncApiClient({
      serverUrl: settings.serverUrl,
      vaultId: settings.vaultId,
      deviceId: settings.deviceId,
      token,
      accessClientId: settings.accessClientId || undefined,
      accessClientSecret
    });
  }

  private isConfigured(): boolean {
    const settings = this.data.settings;
    return Boolean(settings.serverUrl && settings.vaultId && settings.deviceId && settings.apiTokenSecretName);
  }

  private clearTimer(): void {
    if (this.timerId !== undefined) window.clearInterval(this.timerId);
    this.timerId = undefined;
  }

  private setStatus(text: string): void {
    this.statusElement?.setText(`Sync Tunnel: ${text}`);
    this.statusElement?.addClass("sync-tunnel-status");
  }
}

function migrateData(raw: unknown): PersistedData {
  const defaults: PluginSettings = {
    serverUrl: "",
    vaultId: "",
    deviceId: generateDeviceId(),
    apiTokenSecretName: "",
    accessClientId: "",
    accessClientSecretName: "",
    automaticSync: true,
    syncOnStartup: true,
    syncIntervalSeconds: 300,
    excludedPatterns: [".git/**", ".trash/**", "**/.DS_Store", "**/Thumbs.db"]
  };
  const parsed = isRecord(raw) ? raw : {};
  const settings = isRecord(parsed.settings) ? parsed.settings : {};
  return {
    schemaVersion: SCHEMA_VERSION,
    settings: { ...defaults, ...settings } as PluginSettings,
    cursor: typeof parsed.cursor === "number" && parsed.cursor >= 0 ? parsed.cursor : 0,
    files: isRecord(parsed.files) ? parsed.files as Record<string, PersistedData["files"][string]> : {}
  };
}

function generateDeviceId(): string {
  const platform = navigator.userAgent.includes("Mobile") ? "mobile" : "device";
  return `${platform}-${crypto.randomUUID().slice(0, 8)}`;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function formatSummary(summary: SyncSummary): string {
  return `上传 ${summary.uploaded}，下载 ${summary.downloaded}，远端删除 ${summary.deletedRemote}，本地删除 ${summary.deletedLocal}，冲突 ${summary.conflicts}`;
}
