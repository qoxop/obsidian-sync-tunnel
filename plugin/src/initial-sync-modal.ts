import { App, Modal, Setting } from "obsidian";

import type { InitialSyncMode, InitialSyncPreview } from "./types";

export class InitialSyncModal extends Modal {
  constructor(
    app: App,
    private readonly preview: InitialSyncPreview,
    private readonly confirm: (mode: InitialSyncMode) => Promise<void>
  ) {
    super(app);
  }

  onOpen(): void {
    this.setTitle("Sync Tunnel 首次同步预览");
    this.contentEl.createEl("p", {
      text: "首次同步不会静默丢弃差异。请选择初始化策略；发生冲突时仍会保留副本。"
    });
    const list = this.contentEl.createEl("ul");
    const rows: Array<[string, number]> = [
      ["本地文件", this.preview.localFiles],
      ["远端文件", this.preview.remoteFiles],
      ["内容相同", this.preview.same],
      ["同路径内容不同", this.preview.different],
      ["仅本地存在", this.preview.localOnly],
      ["仅远端存在", this.preview.remoteOnly],
      ["本地文件对应远端删除记录", this.preview.localAgainstRemoteDelete]
    ];
    for (const [label, value] of rows) list.createEl("li", { text: `${label}: ${value}` });
    this.contentEl.createEl("p", { text: `远端快照 revision: ${this.preview.snapshotRevision}` });

    new Setting(this.contentEl)
      .setName("安全合并（推荐）")
      .setDesc("下载远端文件、上传仅本地文件；同路径差异保留本地冲突副本后采用远端版本。")
      .addButton((button) => button.setButtonText("安全合并").setCta().onClick(() => void this.choose("merge")));

    new Setting(this.contentEl)
      .setName("以远端为主")
      .setDesc("远端路径成为主版本；仅本地文件改名为冲突副本后保留，不静默删除。")
      .addButton((button) => button.setButtonText("使用远端").onClick(() => void this.choose("remote")));

    new Setting(this.contentEl)
      .setName("以本地为主")
      .setDesc("本地文件覆盖或删除远端对应路径。请仅在确认本地 Vault 是权威副本时使用。")
      .addButton((button) => button.setButtonText("使用本地").setWarning().onClick(() => void this.choose("local")));
  }

  onClose(): void {
    this.contentEl.empty();
  }

  private async choose(mode: InitialSyncMode): Promise<void> {
    this.close();
    await this.confirm(mode);
  }
}
