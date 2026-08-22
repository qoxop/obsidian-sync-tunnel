import { Notice, Platform, Plugin, TFile } from "obsidian";

import { SyncApiClient } from "./api-client";
import { migrateData } from "./data";
import { buildInitialSyncPreview } from "./initial-sync";
import { InitialSyncModal } from "./initial-sync-modal";
import { SyncTunnelSettingTab } from "./settings";
import { SyncEngine, SyncPausedError } from "./sync-engine";
import { ActivityRecord, InitialSyncMode, PersistedData, SyncProgress, SyncSummary } from "./types";
import { normalizeVaultPath } from "./path";
import { VaultScanner } from "./vault-scanner";
import { ActivityModal, ConflictModal, HistoryModal, SetupWizardModal } from "./management-modals";

export default class SyncTunnelPlugin extends Plugin {
  data!: PersistedData;
  private statusElement?: HTMLElement;
  private settingTab?: SyncTunnelSettingTab;
  private timerId?: number;
  private eventSaveTimerId?: number;
  private eventSyncTimerId?: number;
  private syncPromise?: Promise<SyncSummary>;
	private currentProgress: SyncProgress = { phase: "idle", completedFiles: 0, totalFiles: 0, completedBytes: 0, totalBytes: 0 };
	private cancelRequested = false;

  async onload(): Promise<void> {
    this.data = migrateData(await this.loadData());
    await this.savePluginData();

    this.statusElement = this.addStatusBarItem();
    this.setStatus("待同步");
    this.addRibbonIcon("refresh-cw", "Sync Tunnel: sync now", () => void this.runSync(true));
    this.addCommand({ id: "sync-now", name: "Sync now", callback: () => void this.runSync(true) });
	this.addCommand({ id: "pause-sync", name: "Pause sync", callback: () => void this.setPaused(true) });
	this.addCommand({ id: "resume-sync", name: "Resume sync", callback: () => void this.setPaused(false) });
	this.addCommand({ id: "cancel-sync", name: "Cancel current sync", callback: () => this.cancelSync() });
	this.addCommand({ id: "open-sync-activity", name: "Open sync activity", callback: () => this.openActivity() });
	this.addCommand({ id: "open-conflict-center", name: "Open conflict center", callback: () => this.openConflicts() });
	this.addCommand({ id: "open-version-history", name: "Open version history", callback: () => void this.openHistory() });
    this.settingTab = new SyncTunnelSettingTab(this.app, this);
    this.addSettingTab(this.settingTab);
    this.registerEvent(this.app.vault.on("create", (file) => this.recordVaultChange(file.path, file instanceof TFile)));
    this.registerEvent(this.app.vault.on("modify", (file) => this.recordVaultChange(file.path, file instanceof TFile)));
    this.registerEvent(this.app.vault.on("delete", (file) => this.recordVaultChange(file.path, file instanceof TFile)));
    this.registerEvent(this.app.vault.on("rename", (file, oldPath) => {
      if (file instanceof TFile) this.recordVaultRename(oldPath, file.path);
      else {
        this.recordVaultChange(oldPath, false);
        this.recordVaultChange(file.path, false);
      }
    }));

    this.app.workspace.onLayoutReady(() => {
      this.restartTimer();
      if (this.data.settings.syncOnStartup && this.isConfigured()) void this.runSync(false);
    });
  }

  onunload(): void {
    this.clearTimer();
    if (this.eventSaveTimerId !== undefined) window.clearTimeout(this.eventSaveTimerId);
    if (this.eventSyncTimerId !== undefined) window.clearTimeout(this.eventSyncTimerId);
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
	if (this.data.paused) {
	  this.setStatus("已暂停");
	  if (showNotice) new Notice("Sync Tunnel: 同步已暂停，请先继续同步");
	  return undefined;
	}
    if (this.syncPromise) {
      if (showNotice) new Notice("Sync Tunnel: 已有同步任务正在运行");
      return this.syncPromise;
    }
    if (!this.data.initialSyncCompleted && !this.data.pendingInitialSyncMode) {
      this.setStatus("等待首次同步确认");
      if (showNotice) await this.previewInitialSync();
      return undefined;
    }
	const activity: ActivityRecord = { id: crypto.randomUUID(), startedAt: Date.now(), status: "running", phase: "scanning" };
	this.data.activities.push(activity);
	this.data.activities = this.data.activities.slice(-100);
	try {
      const wasInitialSync = !this.data.initialSyncCompleted;
      const client = await this.createClient();
      const scanner = this.createScanner();
	  const engine = new SyncEngine(
		this.app.vault, this.data, client, scanner, () => this.savePluginData(),
		(progress) => this.updateProgress(activity, progress), () => this.data.paused || this.cancelRequested
	  );
      this.setStatus("同步中…");
      this.syncPromise = engine.run();
      const summary = await this.syncPromise;
	  activity.status = "completed";
	  activity.summary = summary;
	  activity.completedAt = Date.now();
      if (wasInitialSync && this.data.initialSyncCompleted) this.settingTab?.display();
      const text = formatSummary(summary);
      this.setStatus(`${summary.restartRequired ? "已同步，需重启" : "已同步"} ${new Date().toLocaleTimeString()}`);
      if (showNotice || summary.conflicts > 0 || summary.restartRequired) {
		const restartText = summary.restartRequired
		  ? `；请重启 Obsidian 以应用：${summary.restartPaths.slice(0, 5).join("、")}${summary.restartPaths.length > 5 ? "…" : ""}`
		  : "";
        new Notice(`Sync Tunnel: ${text}${restartText}`, summary.conflicts > 0 || summary.restartRequired ? 12000 : 5000);
      }
      return summary;
    } catch (error) {
	  if (error instanceof SyncPausedError) {
		activity.status = this.data.paused ? "paused" : "cancelled";
		activity.phase = this.data.paused ? "paused" : "idle";
		activity.completedAt = Date.now();
		this.setStatus(this.data.paused ? "已暂停" : "已取消");
		if (showNotice) new Notice(this.data.paused ? "Sync Tunnel: 已在安全边界暂停" : "Sync Tunnel: 已取消当前同步，队列和断点已保留");
		return undefined;
	  }
      const message = error instanceof Error ? error.message : String(error);
	  activity.status = "failed";
	  activity.phase = "error";
	  activity.errorCode = error instanceof Error ? error.name : "unknown";
	  activity.completedAt = Date.now();
      this.setStatus("同步失败");
      console.error("Sync Tunnel failed", error);
      if (showNotice) new Notice(`Sync Tunnel 失败: ${message}`, 10000);
      return undefined;
    } finally {
	  this.cancelRequested = false;
      this.syncPromise = undefined;
      await this.savePluginData();
    }
  }

  async previewInitialSync(): Promise<void> {
    try {
      this.setStatus("正在生成首次同步预览…");
      const client = await this.createClient();
      const info = await client.serverInfo();
	  if (info.protocol.version !== 1 || !info.capabilities.includes("snapshot")) {
		throw new Error(`服务器 ${info.server_version} 与当前插件协议不一致，请同时升级`);
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
	  if (info.protocol.version !== 1 || !info.capabilities.includes("snapshot")) {
		throw new Error(`服务器 ${info.server_version} 与当前插件协议不一致，请同时升级`);
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
	if (!settings.serverUrl || !settings.vaultId || !settings.deviceId || !settings.credentialSecretName) {
	  throw new Error("请先使用配对码完成设备配对")
	}
	validateServerURL(settings.serverUrl);
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/u.test(settings.vaultId)) throw new Error("Vault ID 格式无效");
    if (!/^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/u.test(settings.deviceId)) throw new Error("Device ID 格式无效");
	const token = this.app.secretStorage.getSecret(settings.credentialSecretName);
	if (!token) throw new Error("设备凭据不存在，请重新配对");
    const accessClientSecret = settings.accessClientSecretName
      ? this.app.secretStorage.getSecret(settings.accessClientSecretName) ?? undefined
      : undefined;
    return new SyncApiClient({
      serverUrl: settings.serverUrl,
      vaultId: settings.vaultId,
      token,
      accessClientId: settings.accessClientId || undefined,
      accessClientSecret
    });
  }

	async pairDevice(code: string): Promise<void> {
	  const settings = this.data.settings;
	  if (!settings.serverUrl || !settings.vaultId || !settings.deviceName || !code.trim()) {
		throw new Error("Server URL、Vault ID、设备名称和配对码不能为空");
	  }
	  validateServerURL(settings.serverUrl);
	  const client = new SyncApiClient({
		serverUrl: settings.serverUrl, vaultId: settings.vaultId, token: "",
		accessClientId: settings.accessClientId || undefined,
		accessClientSecret: settings.accessClientSecretName ? this.app.secretStorage.getSecret(settings.accessClientSecretName) ?? undefined : undefined
	  });
	  const result = await client.pairDevice(code.trim(), settings.deviceName, Platform.isMobile ? "mobile" : Platform.isMacOS ? "macos" : Platform.isWin ? "windows" : "desktop", this.manifest.version);
	  const secretName = `sync-tunnel-${result.vault.id}-${result.device.id}`;
	  this.app.secretStorage.setSecret(secretName, result.token);
	  settings.vaultId = result.vault.id;
	  settings.deviceId = result.device.id;
	  settings.credentialSecretName = secretName;
	  this.resetSyncState();
	  await this.savePluginData();
	  this.settingTab?.display();
	  new Notice(`Sync Tunnel: 已配对设备 ${result.device.name}，请预览首次同步`, 8000);
	}

	openSetupWizard(): void {
	  new SetupWizardModal(this.app, this.data.settings, async (code) => this.pairDevice(code)).open();
	}

	async setPaused(paused: boolean): Promise<void> {
	  this.data.paused = paused;
	  await this.savePluginData();
	  this.setStatus(paused ? "暂停请求已提交" : "待同步");
	  if (!paused && this.isConfigured()) void this.runSync(true);
	}

	cancelSync(): void {
	  if (!this.syncPromise) {
		new Notice("Sync Tunnel: 当前没有同步任务");
		return;
	  }
	  this.cancelRequested = true;
	  this.setStatus("正在安全取消…");
	}

	async rotateCredential(): Promise<void> {
	  const client = await this.createClient();
	  const token = await client.rotateCredential();
	  this.app.secretStorage.setSecret(this.data.settings.credentialSecretName, token);
	  new Notice("Sync Tunnel: 设备凭据已轮换，旧凭据立即失效", 8000);
	}

	openActivity(): void {
	  new ActivityModal(this.app, this.data.activities, this.currentProgress).open();
	}

	openConflicts(): void {
	  new ConflictModal(
		this.app, this.data.conflicts,
		async (id, resolution) => this.resolveConflict(id, resolution),
		async (conflict) => this.previewConflict(conflict.originalPath, conflict.conflictPath)
	  ).open();
	}

	async openHistory(path = ""): Promise<void> {
	  try {
		const client = await this.createClient();
		const page = await client.history(path);
		new HistoryModal(this.app, page.versions, async (version) => {
		  const base = this.data.files[version.path]?.revision ?? 0;
		  await client.restore(version.path, version.revision, base);
		  this.data.pendingPaths[version.path] = Date.now();
		  await this.savePluginData();
		  await this.runSync(true);
		}).open();
	  } catch (error) {
		new Notice(`Sync Tunnel 历史记录失败: ${error instanceof Error ? error.message : String(error)}`, 10000);
	  }
	}

	async exportDiagnostics(): Promise<string> {
	  const target = `${this.app.vault.configDir}/plugins/${this.manifest.id}/diagnostics.json`;
	  const payload = {
		generated_at: new Date().toISOString(), plugin_version: this.manifest.version,
		platform: Platform.isMobile ? "mobile" : "desktop", configured: this.isConfigured(),
		initial_sync_completed: this.data.initialSyncCompleted, cursor: this.data.cursor,
		last_acknowledged_revision: this.data.lastAcknowledgedRevision,
		queues: { pending_paths: Object.keys(this.data.pendingPaths).length, outbox: Object.keys(this.data.outbox).length, inbox: Object.keys(this.data.inbox).length, renames: Object.keys(this.data.pendingRenames).length },
		activities: this.data.activities.slice(-20), conflicts_open: this.data.conflicts.filter((item) => !item.resolvedAt).length
	  };
	  await this.app.vault.adapter.write(target, JSON.stringify(payload, null, 2));
	  new Notice(`Sync Tunnel: 脱敏诊断已写入 ${target}`, 8000);
	  return target;
	}

	private updateProgress(activity: ActivityRecord, progress: SyncProgress): void {
	  this.currentProgress = progress;
	  activity.phase = progress.phase;
	  const suffix = progress.totalFiles > 0 ? ` ${progress.completedFiles}/${progress.totalFiles}` : "";
	  this.setStatus(`${progress.phase}${suffix}`);
	}

	private async resolveConflict(id: string, resolution: "local" | "remote" | "both" | "manual"): Promise<void> {
	  const conflict = this.data.conflicts.find((item) => item.id === id);
	  if (!conflict || conflict.resolvedAt) return;
	  if (resolution === "local") {
		const content = await this.app.vault.adapter.readBinary(conflict.conflictPath);
		await this.app.vault.adapter.writeBinary(conflict.originalPath, content);
		this.data.pendingPaths[conflict.originalPath] = Date.now();
	  } else if (resolution === "remote" && await this.app.vault.adapter.exists(conflict.conflictPath)) {
		await this.app.vault.adapter.remove(conflict.conflictPath);
	  }
	  conflict.resolution = resolution;
	  conflict.resolvedAt = Date.now();
	  await this.savePluginData();
	  if (resolution === "local") await this.runSync(true);
	}

	private async previewConflict(remotePath: string, localPath: string): Promise<{ local: string; remote: string } | null> {
	  const localStat = await this.app.vault.adapter.stat(localPath);
	  const remoteStat = await this.app.vault.adapter.stat(remotePath);
	  if (!localStat || !remoteStat || localStat.type !== "file" || remoteStat.type !== "file" || localStat.size > 1024 * 1024 || remoteStat.size > 1024 * 1024) return null;
	  const decoder = new TextDecoder("utf-8", { fatal: true });
	  try {
		return {
		  local: decoder.decode(await this.app.vault.adapter.readBinary(localPath)),
		  remote: decoder.decode(await this.app.vault.adapter.readBinary(remotePath))
		};
	  } catch {
		return null;
	  }
	}

	private resetSyncState(): void {
	  this.data.cursor = 0;
	  this.data.lastAcknowledgedRevision = 0;
	  this.data.initialSyncCompleted = false;
	  this.data.pendingInitialSyncMode = null;
	  this.data.files = {};
	  this.data.scanCache = {};
	  this.data.pendingPaths = {};
	  this.data.outbox = {};
	  this.data.inbox = {};
	  this.data.pendingRenames = {};
	  this.data.conflicts = [];
	  this.data.needsFullScan = true;
	}

  private createScanner(): VaultScanner {
    const configDirectory = this.app.vault.configDir;
    const protectedPaths = [
      // Sync Tunnel is installed and upgraded through Obsidian/BRAT. Synchronizing
      // its running bundle could replace another device's code mid-session.
	  `${configDirectory}/plugins/${this.manifest.id}`
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
	return Boolean(settings.serverUrl && settings.vaultId && settings.deviceId && settings.credentialSecretName);
  }

  private clearTimer(): void {
    if (this.timerId !== undefined) window.clearInterval(this.timerId);
    this.timerId = undefined;
  }

  private recordVaultChange(path: string, isFile: boolean): void {
    const normalized = normalizeVaultPath(path);
    if (!normalized || this.createScanner().isExcluded(normalized)) return;
    if (isFile) {
      this.data.pendingPaths[normalized] = Math.max(
        Date.now(),
        (this.data.pendingPaths[normalized] ?? 0) + 1
      );
    } else {
      this.data.needsFullScan = true;
    }
    this.scheduleEventStateSave();
    if (this.data.settings.automaticSync && this.isConfigured()) this.scheduleEventSync();
  }

  private scheduleEventStateSave(): void {
    if (this.eventSaveTimerId !== undefined) window.clearTimeout(this.eventSaveTimerId);
    this.eventSaveTimerId = window.setTimeout(() => {
      this.eventSaveTimerId = undefined;
      void this.savePluginData();
    }, 500);
  }

  private recordVaultRename(oldPath: string, newPath: string): void {
    const from = normalizeVaultPath(oldPath);
    const to = normalizeVaultPath(newPath);
    this.recordVaultChange(from, true);
    this.recordVaultChange(to, true);
    if (!from || !to || this.createScanner().isExcluded(from) || this.createScanner().isExcluded(to)) return;
    const renameId = crypto.randomUUID();
    this.data.pendingRenames[renameId] = { renameId, from, to, queuedAt: Date.now() };
    this.scheduleEventStateSave();
  }

  private scheduleEventSync(): void {
    if (this.eventSyncTimerId !== undefined) window.clearTimeout(this.eventSyncTimerId);
    this.eventSyncTimerId = window.setTimeout(() => {
      this.eventSyncTimerId = undefined;
      void this.runSync(false);
    }, 2000);
  }

  private setStatus(text: string): void {
    this.statusElement?.setText(`Sync Tunnel: ${text}`);
    this.statusElement?.addClass("sync-tunnel-status");
  }
}

function formatSummary(summary: SyncSummary): string {
  return `上传 ${summary.uploaded}，下载 ${summary.downloaded}，重命名 ${summary.renamed}，远端删除 ${summary.deletedRemote}，本地删除 ${summary.deletedLocal}，冲突 ${summary.conflicts}`;
}

function validateServerURL(value: string): void {
	let parsed: URL;
	try { parsed = new URL(value); } catch { throw new Error("Server URL 格式无效"); }
	const loopback = parsed.hostname === "127.0.0.1" || parsed.hostname === "localhost" || parsed.hostname === "::1" || parsed.hostname === "[::1]";
	if (parsed.protocol !== "https:" && !(loopback && parsed.protocol === "http:")) {
	  throw new Error("远程 Server URL 必须使用 HTTPS；HTTP 仅允许本机回环地址");
	}
	if (parsed.username || parsed.password || parsed.search || parsed.hash) throw new Error("Server URL 不能包含凭据、查询参数或片段");
}
