import { App, PluginSettingTab, SecretComponent, Setting } from "obsidian";

import SyncTunnelPlugin from "./main";
import type { SyncProfile } from "./types";

export class SyncTunnelSettingTab extends PluginSettingTab {
  constructor(app: App, private readonly plugin: SyncTunnelPlugin) {
    super(app, plugin);
  }

  display(): void {
    const { containerEl } = this;
	const tr = (zh: string, en: string) => this.plugin.data.settings.language === "en" ? en : zh;
    containerEl.empty();

    containerEl.createEl("h2", { text: "Sync Tunnel" });
    containerEl.createEl("div", {
      cls: "sync-tunnel-settings-warning",
	  text: tr("首次使用请先在测试仓库验证，并保留独立备份。同步不是备份。", "Test with a disposable Vault first and keep an independent backup. Sync is not backup.")
    });
	new Setting(containerEl).setName(tr("界面语言", "Language")).addDropdown((dropdown) => dropdown
	  .addOption("zh-CN", "简体中文").addOption("en", "English")
	  .setValue(this.plugin.data.settings.language).onChange(async (value) => {
		this.plugin.data.settings.language = value === "en" ? "en" : "zh-CN";
		await this.plugin.savePluginData();
		this.display();
	  }));

	new Setting(containerEl)
	  .setName(tr("初始化向导", "Setup wizard"))
	  .setDesc(tr("使用一次性配对码注册此设备；Admin Token 永远不会填写到 Obsidian。", "Register this device with a one-time pairing code. Never enter the Admin Token in Obsidian."))
	  .addButton((button) => button.setButtonText(this.plugin.data.settings.deviceId ? tr("重新配对", "Pair again") : tr("开始设置", "Start setup")).setCta().onClick(() => this.plugin.openSetupWizard()));

    new Setting(containerEl)
      .setName("Server URL")
	  .setDesc(tr("Cloudflare Tunnel HTTPS 地址；本机测试可填 http://127.0.0.1:8787。", "Cloudflare Tunnel HTTPS URL; use http://127.0.0.1:8787 for local testing."))
      .addText((text) => text
        .setPlaceholder("https://sync.example.com")
        .setValue(this.plugin.data.settings.serverUrl)
		.setDisabled(Boolean(this.plugin.data.settings.deviceId))
        .onChange(async (value) => {
          this.plugin.data.settings.serverUrl = value.trim();
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
      .setName("Vault ID")
	  .setDesc(tr("同一仓库的所有设备必须完全一致；只允许字母、数字、点、下划线和短横线。", "Use the same ID on every device. Letters, digits, dots, underscores and hyphens only."))
      .addText((text) => text
        .setPlaceholder("my-vault")
        .setValue(this.plugin.data.settings.vaultId)
		.setDisabled(Boolean(this.plugin.data.settings.deviceId))
        .onChange(async (value) => {
          this.plugin.data.settings.vaultId = value.trim();
          await this.plugin.savePluginData();
        }));

	new Setting(containerEl)
	  .setName(tr("设备名称", "Device name"))
	  .setDesc(tr("仅在配对时发送到服务端，便于识别设备。", "Sent during pairing so you can identify this device."))
	  .addText((text) => text.setValue(this.plugin.data.settings.deviceName).onChange(async (value) => {
		this.plugin.data.settings.deviceName = value.trim();
		await this.plugin.savePluginData();
	  }));

	new Setting(containerEl)
	  .setName(tr("已注册设备", "Registered device"))
	  .setDesc(this.plugin.data.settings.deviceId || tr("尚未配对。Device ID 由服务端生成，插件中不可编辑。", "Not paired. The server assigns an immutable Device ID."));
	if (this.plugin.data.settings.deviceId) {
	  new Setting(containerEl).setName(tr("轮换设备凭据", "Rotate device credential")).setDesc(tr("生成新凭据并立即撤销旧凭据。", "Create a new credential and revoke the old one immediately."))
		.addButton((button) => button.setButtonText(tr("轮换", "Rotate")).onClick(async () => this.plugin.rotateCredential()));
	}

    new Setting(containerEl)
      .setName("Cloudflare Access client ID")
	  .setDesc(tr("可选。启用 Cloudflare Access Service Token 时填写。", "Optional. Required only when Cloudflare Access Service Tokens are enabled."))
      .addText((text) => text
        .setValue(this.plugin.data.settings.accessClientId)
        .onChange(async (value) => {
          this.plugin.data.settings.accessClientId = value.trim();
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
      .setName("Cloudflare Access client secret")
	  .setDesc(tr("可选。与上面的 Client ID 配套。", "Optional. Must match the Client ID above."))
      .addComponent((component) => new SecretComponent(this.app, component)
        .setValue(this.plugin.data.settings.accessClientSecretName)
        .onChange(async (value) => {
          this.plugin.data.settings.accessClientSecretName = value;
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
	  .setName(tr("测试连接", "Test connection"))
	  .setDesc(tr("验证 Tunnel、Access、设备凭据、Vault ID 和 1.0 协议。", "Validate Tunnel, Access, device credential, Vault ID and the 1.0 protocol."))
      .addButton((button) => button
		.setButtonText(tr("测试", "Test"))
        .onClick(async () => this.plugin.testConnection()));

    if (!this.plugin.data.initialSyncCompleted && !this.plugin.data.pendingInitialSyncMode) {
      new Setting(containerEl)
		.setName(tr("首次同步预览", "First sync preview"))
		.setDesc(tr("比较本地与远端快照，并在开始写入前选择初始化方式。", "Compare local and remote snapshots before choosing an initialization mode."))
        .addButton((button) => button
		  .setButtonText(tr("预览", "Preview"))
          .setCta()
          .onClick(async () => this.plugin.previewInitialSync()));
    }

    new Setting(containerEl)
	  .setName(tr("自动同步", "Automatic sync"))
	  .setDesc(tr("按下面的时间间隔扫描并同步。", "Scan and sync on the interval below."))
      .addToggle((toggle) => toggle
        .setValue(this.plugin.data.settings.automaticSync)
        .onChange(async (value) => {
          this.plugin.data.settings.automaticSync = value;
          await this.plugin.savePluginData();
          this.plugin.restartTimer();
        }));

	new Setting(containerEl)
	  .setName(tr("暂停同步", "Pause sync"))
	  .setDesc(tr("当前请求完成后在安全边界暂停，保留所有断点和队列。", "Pause at a safe boundary after the active request while preserving queues and checkpoints."))
	  .addToggle((toggle) => toggle.setValue(this.plugin.data.paused).onChange(async (value) => {
		await this.plugin.setPaused(value);
		this.display();
	  }));
	new Setting(containerEl).setName(tr("取消当前同步", "Cancel current sync"))
	  .setDesc(tr("在安全边界停止当前任务；队列和断点会保留，可稍后重试。", "Stop the current run at a safe boundary; queues and checkpoints remain available for retry."))
	  .addButton((button) => button.setButtonText(tr("取消任务", "Cancel run")).onClick(() => this.plugin.cancelSync()));

    new Setting(containerEl)
	  .setName(tr("启动时同步", "Sync on startup"))
      .addToggle((toggle) => toggle
        .setValue(this.plugin.data.settings.syncOnStartup)
        .onChange(async (value) => {
          this.plugin.data.settings.syncOnStartup = value;
          await this.plugin.savePluginData();
        }));

	new Setting(containerEl)
	  .setName(tr("移动端文件内存限制（MiB）", "Mobile file memory limit (MiB)"))
	  .setDesc(tr("移动端官方 API 无稳定的大文件流式接口；超过此限制的文件会明确报错，不会冒险耗尽内存。", "Files above this limit fail clearly because the official mobile API has no stable large-file streaming interface."))
	  .addText((text) => text.setValue(String(Math.floor(this.plugin.data.settings.mobileMaxFileBytes / 1024 / 1024))).onChange(async (value) => {
		const parsed = Number.parseInt(value, 10);
		if (Number.isFinite(parsed)) this.plugin.data.settings.mobileMaxFileBytes = Math.max(4, Math.min(256, parsed)) * 1024 * 1024;
		await this.plugin.savePluginData();
	  }));

    new Setting(containerEl)
	  .setName(tr("同步范围", "Sync profile"))
	  .setDesc(tr("推荐模式同步笔记、附件、常用设置和插件程序，但不复制其他插件的 data.json。完整模式可能复制其中的明文密钥。", "Recommended mode syncs notes, attachments, common settings and plugin bundles, but not other plugins' data.json files."))
      .addDropdown((dropdown) => dropdown
        .addOption("notes", "Notes and attachments")
        .addOption("recommended", "Recommended safe")
        .addOption("full", "Full Vault")
        .addOption("custom", "Custom exclusions")
        .setValue(this.plugin.data.settings.syncProfile)
        .onChange(async (value) => {
          this.plugin.data.settings.syncProfile = value as SyncProfile;
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
	  .setName(tr("同步间隔（秒）", "Interval (seconds)"))
	  .setDesc(tr("最小 30 秒。大型仓库建议 300 秒以上。", "Minimum 30 seconds; use 300 or more for large Vaults."))
      .addText((text) => text
        .setValue(String(this.plugin.data.settings.syncIntervalSeconds))
        .onChange(async (value) => {
          const parsed = Number.parseInt(value, 10);
          if (Number.isFinite(parsed)) this.plugin.data.settings.syncIntervalSeconds = Math.max(30, parsed);
          await this.plugin.savePluginData();
          this.plugin.restartTimer();
        }));

    new Setting(containerEl)
	  .setName(tr("排除路径", "Excluded paths"))
	  .setDesc(tr("每行一个 glob。** 匹配任意目录，* 匹配单层。", "One glob per line. ** crosses directories and * matches one segment."))
      .addTextArea((area) => area
        .setValue(this.plugin.data.settings.excludedPatterns.join("\n"))
        .onChange(async (value) => {
          this.plugin.data.settings.excludedPatterns = value.split(/\r?\n/u).map((line) => line.trim()).filter(Boolean);
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
	  .setName(tr("立即同步", "Sync now"))
	  .setDesc(tr("立即执行一次双向同步。", "Run one bidirectional sync now."))
	  .addButton((button) => button.setButtonText(tr("同步", "Sync")).setCta().onClick(async () => this.plugin.runSync(true)));

	containerEl.createEl("h3", { text: tr("管理与恢复", "Management and recovery") });
	new Setting(containerEl).setName(tr("同步活动", "Sync activity")).setDesc(tr("查看最近 100 次同步及当前进度。", "View the current progress and the last 100 sync runs."))
	  .addButton((button) => button.setButtonText(tr("打开", "Open")).onClick(() => this.plugin.openActivity()));
	new Setting(containerEl).setName(tr("冲突中心", "Conflict center")).setDesc(`${tr("待处理冲突", "Open conflicts")}: ${this.plugin.data.conflicts.filter((item) => !item.resolvedAt).length}`)
	  .addButton((button) => button.setButtonText(tr("打开", "Open")).onClick(() => this.plugin.openConflicts()));
	new Setting(containerEl).setName(tr("文件历史与恢复", "File history and recovery")).setDesc(tr("浏览版本、已删除文件并恢复为新的 revision。", "Browse versions and deleted files, then restore as a new revision."))
	  .addButton((button) => button.setButtonText(tr("打开", "Open")).onClick(async () => this.plugin.openHistory()));
	new Setting(containerEl).setName(tr("导出脱敏诊断", "Export sanitized diagnostics")).setDesc(tr("不包含 URL、Token、明文路径和笔记正文。", "Excludes URLs, tokens, plaintext paths and note content."))
	  .addButton((button) => button.setButtonText(tr("导出", "Export")).onClick(async () => this.plugin.exportDiagnostics()));
  }
}
