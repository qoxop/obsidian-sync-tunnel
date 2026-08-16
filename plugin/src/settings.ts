import { App, PluginSettingTab, SecretComponent, Setting } from "obsidian";

import SyncTunnelPlugin from "./main";
import type { SyncProfile } from "./types";

export class SyncTunnelSettingTab extends PluginSettingTab {
  constructor(app: App, private readonly plugin: SyncTunnelPlugin) {
    super(app, plugin);
  }

  display(): void {
    const { containerEl } = this;
    containerEl.empty();

    containerEl.createEl("h2", { text: "Sync Tunnel" });
    containerEl.createEl("div", {
      cls: "sync-tunnel-settings-warning",
      text: "首次使用请先在测试仓库验证，并保留独立备份。同步不是备份。"
    });

    new Setting(containerEl)
      .setName("Server URL")
      .setDesc("Cloudflare Tunnel HTTPS 地址；本机测试可填 http://127.0.0.1:8787。")
      .addText((text) => text
        .setPlaceholder("https://sync.example.com")
        .setValue(this.plugin.data.settings.serverUrl)
        .onChange(async (value) => {
          this.plugin.data.settings.serverUrl = value.trim();
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
      .setName("Vault ID")
      .setDesc("同一仓库的所有设备必须完全一致；只允许字母、数字、点、下划线和短横线。")
      .addText((text) => text
        .setPlaceholder("my-vault")
        .setValue(this.plugin.data.settings.vaultId)
        .onChange(async (value) => {
          this.plugin.data.settings.vaultId = value.trim();
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
      .setName("Device ID")
      .setDesc("此设备的唯一标识，用于冲突副本命名。")
      .addText((text) => text
        .setValue(this.plugin.data.settings.deviceId)
        .onChange(async (value) => {
          this.plugin.data.settings.deviceId = value.trim();
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
      .setName("API token")
      .setDesc("选择或新建一个 Obsidian SecretStorage 条目，并填入服务安装脚本生成的 Token。")
      .addComponent((component) => new SecretComponent(this.app, component)
        .setValue(this.plugin.data.settings.apiTokenSecretName)
        .onChange(async (value) => {
          this.plugin.data.settings.apiTokenSecretName = value;
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
      .setName("Cloudflare Access client ID")
      .setDesc("可选。启用 Cloudflare Access Service Token 时填写。")
      .addText((text) => text
        .setValue(this.plugin.data.settings.accessClientId)
        .onChange(async (value) => {
          this.plugin.data.settings.accessClientId = value.trim();
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
      .setName("Cloudflare Access client secret")
      .setDesc("可选。与上面的 Client ID 配套。")
      .addComponent((component) => new SecretComponent(this.app, component)
        .setValue(this.plugin.data.settings.accessClientSecretName)
        .onChange(async (value) => {
          this.plugin.data.settings.accessClientSecretName = value;
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
      .setName("Test connection")
      .setDesc("验证 Tunnel、Access、API token、Vault ID 和服务端协议版本。")
      .addButton((button) => button
        .setButtonText("Test")
        .onClick(async () => this.plugin.testConnection()));

    if (!this.plugin.data.initialSyncCompleted && !this.plugin.data.pendingInitialSyncMode) {
      new Setting(containerEl)
        .setName("First sync preview")
        .setDesc("比较本地与远端快照，并在开始写入前选择初始化方式。")
        .addButton((button) => button
          .setButtonText("Preview")
          .setCta()
          .onClick(async () => this.plugin.previewInitialSync()));
    }

    new Setting(containerEl)
      .setName("Automatic sync")
      .setDesc("按下面的时间间隔扫描并同步。")
      .addToggle((toggle) => toggle
        .setValue(this.plugin.data.settings.automaticSync)
        .onChange(async (value) => {
          this.plugin.data.settings.automaticSync = value;
          await this.plugin.savePluginData();
          this.plugin.restartTimer();
        }));

    new Setting(containerEl)
      .setName("Sync on startup")
      .addToggle((toggle) => toggle
        .setValue(this.plugin.data.settings.syncOnStartup)
        .onChange(async (value) => {
          this.plugin.data.settings.syncOnStartup = value;
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
      .setName("Sync profile")
      .setDesc("推荐模式同步笔记、附件、常用设置和插件程序，但不复制其他插件的 data.json。完整模式可能复制其中的明文密钥。")
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
      .setName("Interval (seconds)")
      .setDesc("最小 30 秒。大型仓库建议 300 秒以上。")
      .addText((text) => text
        .setValue(String(this.plugin.data.settings.syncIntervalSeconds))
        .onChange(async (value) => {
          const parsed = Number.parseInt(value, 10);
          if (Number.isFinite(parsed)) this.plugin.data.settings.syncIntervalSeconds = Math.max(30, parsed);
          await this.plugin.savePluginData();
          this.plugin.restartTimer();
        }));

    new Setting(containerEl)
      .setName("Excluded paths")
      .setDesc("每行一个 glob。** 匹配任意目录，* 匹配单层。")
      .addTextArea((area) => area
        .setValue(this.plugin.data.settings.excludedPatterns.join("\n"))
        .onChange(async (value) => {
          this.plugin.data.settings.excludedPatterns = value.split(/\r?\n/u).map((line) => line.trim()).filter(Boolean);
          await this.plugin.savePluginData();
        }));

    new Setting(containerEl)
      .setName("Sync now")
      .setDesc("立即执行一次双向同步。")
      .addButton((button) => button.setButtonText("Sync").setCta().onClick(async () => this.plugin.runSync(true)));
  }
}
