# macOS 第二设备 1.0 验收

本流程把 Mac 作为设备 B 接入已经由 Windows 设备 A 初始化的逻辑测试 Vault，验证 `1.0.0-rc.1` 的配对、首次下载、反向上传、分块、路径和重启恢复。只用于专用测试 Vault。

## 准备

1. Mac 创建全新的空 Obsidian Vault，至少打开一次产生 `.obsidian`；
2. Windows 管理端为同一逻辑 Vault 创建新的、尚未使用的一次性配对码；
3. 准备 Cloudflare HTTPS URL；若启用 Access，准备 Client ID/Secret；
4. 不复制设备 A 的插件目录、`data.json`、Device ID 或任何凭据；
5. 配对码和 Access Secret 不发送到聊天或写入普通笔记。

## 下载脚本和 RC

RC Release 发布后执行：

```bash
curl --fail --location --proto '=https' --tlsv1.2 \
  https://raw.githubusercontent.com/qoxop/obsidian-sync-tunnel/main/scripts/macos-device-test.sh \
  --output "$HOME/Downloads/macos-device-test.sh"
chmod 700 "$HOME/Downloads/macos-device-test.sh"
less "$HOME/Downloads/macos-device-test.sh"

VAULT="$HOME/Documents/Obsidian/SyncTunnelMacTest"
SCRIPT="$HOME/Downloads/macos-device-test.sh"
"$SCRIPT" guided --vault "$VAULT" --version 1.0.0-rc.1
```

脚本从 GitHub Release 下载并校验 `main.js`、`manifest.json`、`styles.css`；更新已有插件前备份整个插件目录，但不读取/复制 SecretStorage。它不会修改 `community-plugins.json`，因此首次安装后需要手动启用 Sync Tunnel。

## Obsidian 中的操作

脚本暂停后：

1. 重载 Obsidian并启用 Sync Tunnel；
2. 打开配置向导；
3. Server URL 填 Cloudflare 地址，Vault ID 填设备 A 的逻辑 Vault ID；
4. Device name 使用可识别名称，Device ID 由服务器配对后返回；
5. 输入这台 Mac 独立的一次性配对码；
6. 若启用 Access，Client Secret 只进入 SecretStorage；
7. 选择 Recommended，首次模式选择安全合并；
8. 暂时关闭自动同步，连接测试必须显示协议 1/版本 `1.0.0-rc.1`；
9. 点击同步两次，第二次所有计数为 0，再回终端按回车。

脚本检查最终 schema、设备身份、Credential 引用、cursor、ACK、文件索引和所有持久化队列。它不输出 URL、Vault/Device ID 或 Secret。

之后脚本创建 Markdown、Canvas、PNG、Unicode/空格路径、嵌套路径、5 MiB 二进制和应被排除的 `.DS_Store`。在 Mac 再同步两次，第二次全 0后回车。通过标志：

```text
[INFO] macOS device-B verification PASS
```

## 中断续传

```bash
"$SCRIPT" create-resume-probe --vault "$VAULT" --probe-size-mib 64
```

点击 Sync now，在上传仍进行时 `Command-Q`。Obsidian 完全退出后：

```bash
"$SCRIPT" check-resume-interrupted --vault "$VAULT"
```

只有 `CLIENT_RESTART_INTERRUPTED_STATE_PASS` 才继续。重开 Vault，同步完成后再点一次确认全 0：

```bash
"$SCRIPT" verify-resume-probe --vault "$VAULT"
```

通过标志为 `CLIENT_RESTART_RESUME_PASS`。

## 可重复子命令

```bash
"$SCRIPT" status --vault "$VAULT" --version 1.0.0-rc.1
"$SCRIPT" create-probe --vault "$VAULT"
"$SCRIPT" verify-probe --vault "$VAULT" --version 1.0.0-rc.1
"$SCRIPT" prepare --vault "$VAULT" --version 1.0.0-rc.1
```

默认拒绝非空 Vault。`--allow-non-empty` 只供已经完整备份且明确接受风险的维护者。

## 设备 A 反向检查

Mac PASS 后让 Windows A 手动同步两次：

- 收到 `_sync-tunnel-verification/mac-device-b-*`；
- `SHA256SUMS` 所列文件一致；
- 5/64 MiB 文件完整；
- `.DS_Store` 未跟踪；
- 第二次全 0；
- 管理端设备列表中 Mac ACK revision 不落后于其 cursor。

只发送终端 PASS/FAIL 和脱敏计数，不发送 `data.json`。
