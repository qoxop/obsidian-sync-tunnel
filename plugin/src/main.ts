import { Notice, Plugin } from "obsidian";

import { SyncApiClient } from "./api-client";
import { migrateData } from "./data";
import { buildInitialSyncPreview } from "./initial-sync";
import { InitialSyncModal } from "./initial-sync-modal";
import { SyncTunnelSettingTab } from "./settings";
import { SyncEngine } from "./sync-engine";
import { InitialSyncMode, PersistedData, SyncSummary } from "./types";
import { VaultScanner } from "./vault-scanner";

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
    if (!this.data.initialSyncCompleted && !this.data.pendingInitialSyncMode) {
      this.setStatus("等待首次同步确认");
      if (showNotice) await this.previewInitialSync();
      return undefined;
    }
    try {
      const client = await this.createClient();
      const scanner = this.createScanner();
      const engine = new SyncEngine(this.app.vault, this.data, client, scanner, () => this.savePluginData());
      this.setStatus("同步中…");
      this.syncPromise = engine.run();
      const summary = await this.syncPromise;
      const text = formatSummary(summary);
      this.setStatus(`${summary.restartRequired ? "已同步，需重启" : "已同步"} ${new Date().toLocaleTimeString()}`);
      if (showNotice || summary.conflicts > 0 || summary.restartRequired) {
        const restartText = summary.restartRequired ? "；插件、主题或 CSS 已变化，请重启 Obsidian 使其生效" : "";
        new Notice(`Sync Tunnel: ${text}${restartText}`, summary.conflicts > 0 || summary.restartRequired ? 12000 : 5000);
      }
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

  async previewInitialSync(): Promise<void> {
    try {
      this.setStatus("正在生成首次同步预览…");
      const client = await this.createClient();
      const info = await client.serverInfo();
      if (info.protocol.max < 2 || !info.capabilities.includes("snapshot-v1")) {
        throw new Error(`服务器 ${info.server_version} 不支持安全快照协议，请先升级服务端`);
      }
      const preview = await buildInitialSyncPreview(this.createScanner(), client);
      this.setStatus("等待首次同步确认");
      new InitialSyncModal(this.app, preview, async (mode) => this.approveInitialSync(mode)).open();
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      this.setStatus("首次同步预览失败");
      new Notice(`Sync Tunnel 无法生成首次同步预览: ${message}`, 12000);
    }
  }

  private async approveInitialSync(mode: InitialSyncMode): Promise<void> {
    this.data.pendingInitialSyncMode = mode;
    await this.savePluginData();
    await this.runSync(true);
  }

  async testConnection(): Promise<void> {
    try {
      const client = await this.createClient();
      const info = await client.serverInfo();
      if (info.protocol.max < 2 || !info.capabilities.includes("snapshot-v1")) {
        throw new Error(`服务器 ${info.server_version} 不支持安全快照协议，请先升级服务端`);
      }
      const status = await client.status();
      new Notice(`Sync Tunnel: 连接成功；服务端 ${info.server_version}，Vault revision ${status.latest_revision}`, 8000);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      new Notice(`Sync Tunnel 连接测试失败: ${message}`, 12000);
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

  private createScanner(): VaultScanner {
    const configDirectory = this.app.vault.configDir;
    const protectedPaths = [
      // Sync Tunnel is installed and upgraded through Obsidian/BRAT. Synchronizing
      // its running bundle could replace another device's code mid-session.
      `${configDirectory}/plugins/${this.manifest.id}`,
      // Protect all files left by pre-release builds that used the old plugin ID.
      `${configDirectory}/plugins/obsidian-sync-tunnel`
    ];
    return new VaultScanner(
      this.app.vault,
      this.data.settings.excludedPatterns,
      protectedPaths,
      this.data.settings.syncProfile
    );
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

function formatSummary(summary: SyncSummary): string {
  return `上传 ${summary.uploaded}，下载 ${summary.downloaded}，远端删除 ${summary.deletedRemote}，本地删除 ${summary.deletedLocal}，冲突 ${summary.conflicts}`;
}
