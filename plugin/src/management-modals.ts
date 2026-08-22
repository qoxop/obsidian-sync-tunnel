import { App, Modal, Notice, Setting } from "obsidian";

import type { ActivityRecord, Change, ConflictRecord, PluginSettings, SyncProgress, SyncProfile } from "./types";

export class SetupWizardModal extends Modal {
  private pairingCode = "";

  constructor(
    app: App,
    private readonly settings: PluginSettings,
    private readonly pair: (code: string) => Promise<void>
  ) {
    super(app);
  }

  onOpen(): void {
    this.setTitle("Sync Tunnel 初始化向导");
    this.contentEl.createEl("p", { text: "同步不是备份。请先确认当前 Vault 已有独立备份，再配对设备。" });
    new Setting(this.contentEl).setName("1. Server URL").addText((text) => text
      .setPlaceholder("https://sync.example.com")
      .setValue(this.settings.serverUrl)
      .onChange((value) => { this.settings.serverUrl = value.trim(); }));
    new Setting(this.contentEl).setName("2. Vault ID").addText((text) => text
      .setValue(this.settings.vaultId)
      .onChange((value) => { this.settings.vaultId = value.trim(); }));
    new Setting(this.contentEl).setName("3. Device name").addText((text) => text
      .setValue(this.settings.deviceName)
      .onChange((value) => { this.settings.deviceName = value.trim(); }));
    new Setting(this.contentEl).setName("4. Pairing code").setDesc("由 Windows 本机管理脚本生成，10 分钟内一次有效。")
      .addText((text) => text.onChange((value) => { this.pairingCode = value.trim(); }));
    new Setting(this.contentEl).setName("5. Sync profile").setDesc("推荐安全模式不会同步其他插件的 data.json。")
      .addDropdown((dropdown) => dropdown
        .addOption("recommended", "Recommended safe")
        .addOption("notes", "Notes and attachments")
        .addOption("full", "Full Vault")
        .addOption("custom", "Custom")
        .setValue(this.settings.syncProfile)
        .onChange((value) => { this.settings.syncProfile = value as SyncProfile; }));
    new Setting(this.contentEl)
      .addButton((button) => button.setButtonText("取消").onClick(() => this.close()))
      .addButton((button) => button.setButtonText("配对设备").setCta().onClick(async () => {
        button.setDisabled(true);
        try {
          await this.pair(this.pairingCode);
          this.close();
        } catch (error) {
          new Notice(`Sync Tunnel 配对失败: ${error instanceof Error ? error.message : String(error)}`, 10000);
          button.setDisabled(false);
        }
      }));
  }

  onClose(): void { this.contentEl.empty(); }
}

export class ActivityModal extends Modal {
  constructor(app: App, private readonly activities: ActivityRecord[], private readonly progress: SyncProgress) { super(app); }

  onOpen(): void {
    this.setTitle("Sync Tunnel 同步活动");
    this.contentEl.createEl("p", { text: `当前阶段：${this.progress.phase}` });
    if (this.progress.totalFiles > 0) {
      const progress = this.contentEl.createEl("progress");
      progress.max = this.progress.totalFiles;
      progress.value = this.progress.completedFiles;
    }
    const list = this.contentEl.createEl("div", { cls: "sync-tunnel-record-list" });
    for (const item of [...this.activities].reverse()) {
      const row = list.createEl("div", { cls: "sync-tunnel-record" });
      row.createEl("strong", { text: `${new Date(item.startedAt).toLocaleString()} · ${item.status}` });
      row.createEl("div", { text: item.summary ? formatSummary(item.summary) : `阶段：${item.phase}${item.errorCode ? ` · ${item.errorCode}` : ""}` });
    }
    if (this.activities.length === 0) list.createEl("p", { text: "暂无同步记录。" });
  }

  onClose(): void { this.contentEl.empty(); }
}

export class HistoryModal extends Modal {
  constructor(app: App, private readonly versions: Change[], private readonly restore: (version: Change) => Promise<void>) { super(app); }

  onOpen(): void {
    this.setTitle("Sync Tunnel 文件历史与恢复");
    for (const version of this.versions) {
      new Setting(this.contentEl)
        .setName(version.path)
        .setDesc(`revision ${version.revision} · ${new Date(version.modified_at).toLocaleString()} · ${version.deleted ? "已删除" : `${version.size} bytes`} · ${version.device_id}`)
        .addButton((button) => button.setButtonText("恢复为新版本").onClick(async () => {
          button.setDisabled(true);
          try {
            await this.restore(version);
            new Notice(`Sync Tunnel: 已提交 ${version.path} 的恢复操作`);
            this.close();
          } catch (error) {
            new Notice(`恢复失败: ${error instanceof Error ? error.message : String(error)}`, 10000);
            button.setDisabled(false);
          }
        }));
    }
    if (this.versions.length === 0) this.contentEl.createEl("p", { text: "没有可显示的历史版本。" });
  }

  onClose(): void { this.contentEl.empty(); }
}

export class ConflictModal extends Modal {
  constructor(
    app: App,
    private readonly conflicts: ConflictRecord[],
	private readonly resolve: (id: string, resolution: "local" | "remote" | "both" | "manual") => Promise<void>,
	private readonly preview: (conflict: ConflictRecord) => Promise<{ local: string; remote: string } | null>
  ) { super(app); }

  onOpen(): void {
    this.setTitle("Sync Tunnel 冲突中心");
    const open = this.conflicts.filter((item) => !item.resolvedAt);
    for (const conflict of open) {
      new Setting(this.contentEl)
        .setName(conflict.originalPath)
        .setDesc(`本地副本：${conflict.conflictPath}；本地 revision ${conflict.localRevision}，远端 revision ${conflict.remoteRevision}`)
		.addButton((button) => button.setButtonText("文本对比").onClick(async () => {
		  const content = await this.preview(conflict);
		  if (!content) {
			new Notice("该冲突不是可安全预览的小型文本文件");
			return;
		  }
		  new TextDiffModal(this.app, conflict.originalPath, content.local, content.remote).open();
		}))
        .addButton((button) => button.setButtonText("使用本地").onClick(async () => { await this.resolve(conflict.id, "local"); this.display(); }))
        .addButton((button) => button.setButtonText("使用远端").onClick(async () => { await this.resolve(conflict.id, "remote"); this.display(); }))
        .addButton((button) => button.setButtonText("保留两份").onClick(async () => { await this.resolve(conflict.id, "both"); this.display(); }))
        .addButton((button) => button.setButtonText("已手动合并").onClick(async () => { await this.resolve(conflict.id, "manual"); this.display(); }));
    }
    if (open.length === 0) this.contentEl.createEl("p", { text: "没有待处理冲突。" });
  }

  display(): void { this.contentEl.empty(); this.onOpen(); }
  onClose(): void { this.contentEl.empty(); }
}

class TextDiffModal extends Modal {
	constructor(app: App, private readonly path: string, private readonly local: string, private readonly remote: string) { super(app); }
	onOpen(): void {
	  this.setTitle(`冲突对比：${this.path}`);
	  const grid = this.contentEl.createEl("div", { cls: "sync-tunnel-diff-grid" });
	  const left = grid.createEl("div");
	  left.createEl("strong", { text: "本地冲突副本" });
	  const localArea = left.createEl("textarea", { cls: "sync-tunnel-diff-text" });
	  localArea.value = this.local;
	  localArea.readOnly = true;
	  const right = grid.createEl("div");
	  right.createEl("strong", { text: "远端版本" });
	  const remoteArea = right.createEl("textarea", { cls: "sync-tunnel-diff-text" });
	  remoteArea.value = this.remote;
	  remoteArea.readOnly = true;
	}
	onClose(): void { this.contentEl.empty(); }
}

function formatSummary(summary: ActivityRecord["summary"]): string {
  if (!summary) return "";
  return `上传 ${summary.uploaded}，下载 ${summary.downloaded}，删除 ${summary.deletedLocal + summary.deletedRemote}，冲突 ${summary.conflicts}`;
}
