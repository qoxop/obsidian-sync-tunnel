# macOS 第二设备脚本化验收

本流程用于把一台 Mac 作为设备 B，连接到现有测试 Vault，并验证 `0.3.0-beta.2` 的首次下载、反向上传、大文件分块、路径兼容和持久化队列收敛。它只适用于专用测试 Vault，不能替代备份。

脚本直接从 GitHub Release 下载 `main.js`、`manifest.json` 和 `styles.css`，因此不依赖 BRAT。后续可以继续用同一脚本指定新版本，或者在验收完成后改由 BRAT 管理更新。

## 1. Mac 上的准备

1. 安装 Obsidian，并创建一个全新的空 Vault；
2. 至少打开这个 Vault 一次，确认其中已经产生 `.obsidian` 目录；
3. 记录 Vault 的绝对路径；
4. 在密码管理器中准备现有 API Token。不要把 Token、Cloudflare Secret、真实域名或 Vault 正文发到聊天、截图或终端命令中；
5. 如果 Cloudflare Access 已启用，同时准备 Client ID 和保存在密码管理器中的 Client Secret。

第二台设备已有笔记时不要直接运行引导流程。先完整备份，另建空 Vault 完成验收，再人工迁移只存在于旧 Vault 的文件。

## 2. 下载并执行引导脚本

推荐先下载成文件，检查后再执行：

```bash
curl --fail --location --proto '=https' --tlsv1.2 \
  https://raw.githubusercontent.com/qoxop/obsidian-sync-tunnel/main/scripts/macos-device-test.sh \
  --output "$HOME/Downloads/macos-device-test.sh"
chmod 700 "$HOME/Downloads/macos-device-test.sh"
less "$HOME/Downloads/macos-device-test.sh"
"$HOME/Downloads/macos-device-test.sh" guided --vault "$HOME/Documents/Obsidian/SyncTunnelMacTest"
```

`guided` 会执行下列操作：

- 拒绝非空 Vault，避免把测试意外指向真实笔记；
- 通过 HTTPS 下载并校验固定版本 `0.3.0-beta.2` 的插件 ID、版本和必要附件；
- 更新已有插件前，把整个插件目录备份到 `~/Documents/ObsidianSyncBackups/client-state/`；
- 不复制设备 A 的 `data.json`，不读取或保存 API Token/Cloudflare Secret；
- 暂停并等待用户在 Obsidian 中完成 SecretStorage、首次同步预览和同步方向确认；
- 检查客户端游标、已跟踪文件和持久化队列；
- 生成 Markdown、Canvas、PNG、Unicode/空格路径、嵌套路径和 5 MiB 二进制探针；
- 在第二次同步后检查本地 SHA-256、服务端确认 revision 和 `.DS_Store` 排除规则。

脚本不会修改 `community-plugins.json`，所以首次安装后需要在 **Settings > Community plugins** 中手动启用 **Sync Tunnel**。

## 3. Obsidian 中只需要做的操作

脚本第一次暂停后：

1. 重载或重新打开 Obsidian，在空测试 Vault 中启用 **Sync Tunnel**；
2. Server URL 填设备 A 使用的地址；
3. Vault ID 填设备 A 使用的同一值；
4. 保留 Mac 自动生成的唯一 Device ID，不复制设备 A 的插件目录或 `data.json`；
5. 在 SecretStorage 中新建或选择 API Token 条目并粘贴 Token；
6. 如已启用 Cloudflare Access，填写 Client ID，并把 Client Secret 放进另一个 SecretStorage 条目；
7. 暂时关闭 **Automatic sync**；
8. 点击 **Test connection**，确认服务端为 `0.3.0-beta.1`；
9. 打开 **First sync preview**，选择 **Recommended safe**；
10. 点击两次 **Sync now**，确认第二次上传、下载、删除和冲突均为 0，然后回到终端按回车。

脚本第二次暂停前已经生成探针。再次点击两次 **Sync now**，确认第二次全 0，再回到终端按回车。最终出现以下内容才算设备 B 通过：

```text
[INFO] macOS device-B verification PASS
```

安全报告位于：

```text
<Vault>/.obsidian/plugins/sync-tunnel/macos-verification-report.txt
```

报告不包含 Server URL、Vault ID、Device ID、Token、Cloudflare Secret 或笔记正文。

## 4. 可重复运行的子命令

如果引导流程中断，不需要重新安装：

```bash
SCRIPT="$HOME/Downloads/macos-device-test.sh"
VAULT="$HOME/Documents/Obsidian/SyncTunnelMacTest"

"$SCRIPT" status --vault "$VAULT"
"$SCRIPT" create-probe --vault "$VAULT"
"$SCRIPT" verify-probe --vault "$VAULT"
```

重新下载或更新指定候选版：

```bash
"$SCRIPT" prepare --vault "$VAULT" --version 0.3.0-beta.2
```

默认不允许非空 Vault。`--allow-non-empty` 仅供已经完成备份并明确知道风险的维护者使用，不应用于首次验收。

## 5. 通过标准

Mac 端脚本通过后，还需要让设备 A 手动同步两次，并检查：

- 能收到 `_sync-tunnel-verification/mac-device-b-*` 目录；
- `SHA256SUMS` 中列出的文件在设备 A 上校验一致；
- 5 MiB 文件完整下载；
- `.DS_Store` 没有出现在设备 A；
- 第二次同步所有计数为 0。

把 Mac 终端最后的 PASS/FAIL 输出发回即可，不要附带 `data.json`。设备 A 的同步和哈希检查可以继续由 Codex 在 Windows 上完成。
