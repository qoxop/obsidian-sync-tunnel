# Sync Tunnel 1.0 人工验收清单

本文用于当前 1.0 release candidate 的专用测试数据。每项记录日期、平台、Obsidian 版本、插件版本、服务镜像版本、结果和脱敏证据。任何 FAIL 都停止进入下一阶段。

## 0. 安全准备

- [ ] 真实 Vault 已做一份不在当前电脑上的加密备份，并实际恢复过随机文件；
- [ ] 现有 0.x 服务数据、`.env` 和 Token 不删除，保留为只读回滚材料；
- [ ] 创建两个全新的本地 Obsidian 测试目录 `device-a`、`device-b`；
- [ ] 关闭测试 Vault 上的 Obsidian Sync、Syncthing、iCloud/网盘双向同步；
- [ ] Docker Desktop、Windows `cloudflared`、Mac 和至少一台 Android/iOS 真机可用；
- [ ] 全程不发送 Admin Token、配对码、设备凭据、Access Secret、真实域名或 `data.json`。

## 1. 新建 1.0 RC 服务环境

不要覆盖旧数据，使用新目录：

```powershell
$VERSION = (Get-Content .\manifest.json -Raw | ConvertFrom-Json).version
$DATA_DIR = Join-Path $PWD 'runtime-data-acceptance'
$BACKUP_DIR = Join-Path $PWD 'runtime-backups-acceptance'
$ADMIN_TOKEN_FILE = Join-Path $PWD 'secrets\admin-token-acceptance.txt'

.\scripts\docker-down.ps1
.\scripts\docker-init.ps1 `
  -DataDirectory $DATA_DIR `
  -BackupDirectory $BACKUP_DIR `
  -AdminTokenFile $ADMIN_TOKEN_FILE `
  -Version $VERSION `
  -ForceConfig
.\scripts\docker-up.ps1
```

验证：

```powershell
docker compose ps
Invoke-RestMethod http://127.0.0.1:8787/healthz
Invoke-RestMethod http://127.0.0.1:8788/healthz
.\scripts\admin.ps1 -Doctor -AdminTokenFile $ADMIN_TOKEN_FILE
.\scripts\admin.ps1 -Stats -AdminTokenFile $ADMIN_TOKEN_FILE
```

通过标准：容器 healthy；两个 health 均为 `ok`；doctor `ok=true`；`docker compose port` 显示宿主机为 `127.0.0.1`。确认 Cloudflare Public Hostname 仍只指向 `http://127.0.0.1:8787`，绝不能指向 `8788`。

## 2. 创建逻辑 Vault 与设备 A

```powershell
.\scripts\admin.ps1 -CreateVault -VaultId test-1-0 -DisplayName '1.0 acceptance' -AdminTokenFile $ADMIN_TOKEN_FILE
.\scripts\admin.ps1 -CreatePairing -VaultId test-1-0 -AdminTokenFile $ADMIN_TOKEN_FILE
```

一次性配对码只在插件向导中临时使用。设备 A：

1. 构建并安装插件到新的 `device-a` Vault；
2. 启用 Sync Tunnel；
3. 向导填写 Cloudflare HTTPS URL、Vault ID `test-1-0`、易识别的设备名和配对码；
4. 若启用 Access，Client ID 可填设置，Client Secret 进入 SecretStorage；
5. 使用 Recommended，首次模式选择“安全合并”；
6. 暂时关闭自动同步，点击连接测试，再手动同步两次。

通过标准：连接显示协议 1，服务版本等于 `$VERSION`；第二次上传/下载/删除/冲突为 0；Activity 完成；设置中显示服务端分配的设备 ID；客户端 `data.json` 不包含设备 Token 或 Access Secret 明文。

## 3. Windows 同机双客户端收敛

为 `device-b` 生成新的配对码，不能复用 A 的码。B 从空 Vault 配对同一 `test-1-0`，首次模式选安全合并。

在 A 创建：Markdown、Canvas、PNG、PDF、Unicode/空格路径、大小写不同但不碰撞的路径、5 MiB 与 64 MiB 二进制；同步两次。B 同步两次并逐文件 SHA-256 比较。然后反向从 B 修改/新增，A 拉取。

- [ ] 第二次同步全 0；
- [ ] 正文、二进制、mtime 变化不影响内容哈希正确性；
- [ ] `.DS_Store`、`Thumbs.db`、Sync Tunnel 自身目录未传播；
- [ ] Recommended 同步其他插件 `main.js/manifest.json/styles.css`，不传播其 `data.json`；
- [ ] Full 模式只有在明确确认警告后才传播其他插件 `data.json`；
- [ ] 插件/主题/CSS 变化列出准确重启路径并提示重载。

## 4. 冲突、删除、重命名与恢复

1. A、B 都关闭自动同步；
2. 在同一路径离线写入不同正文；
3. A 同步，再让 B 同步；
4. 在冲突中心检查原路径、冲突副本和小文本并排差异；分别验证“本地、远端、保留两份、手工合并”；
5. 创建 20 个文件，在另一设备执行批量删除；
6. 测试文件/目录重命名；
7. 打开历史，恢复旧版本和已删除文件。

通过标准：任何一方原始字节都未消失；冲突记录可解释且可关闭；rename 不产生重复活文件；批量删除要么全部成功要么冲突失败；恢复产生更高 revision，并标出来源 revision。

## 5. 网络、进程和服务故障恢复

- [ ] 64 MiB 上传中断网络，恢复后只补缺 Chunk，第二次全 0；
- [ ] 上传中退出 Obsidian，重开后 outbox 续传；
- [ ] 下载中退出 Obsidian，原文件仍在，重开后 inbox 校验并替换；
- [ ] `docker compose restart sync-server` 后客户端继续同步；
- [ ] Docker Desktop/Windows 整机重启后服务、Tunnel 和数据仍可用；
- [ ] 网络离线期间 Activity 显示可理解错误，暂停/恢复/取消只在安全边界生效；
- [ ] Cloudflare Access 错误凭据不会退化成绕过访问。

## 6. 权限、配额和运维

```powershell
.\scripts\admin.ps1 -ListDevices -VaultId test-1-0 -AdminTokenFile $ADMIN_TOKEN_FILE
.\scripts\admin.ps1 -ListAudit -AdminTokenFile $ADMIN_TOKEN_FILE
```

- [ ] 插件执行“轮换设备凭据”后继续工作，旧凭据失效；
- [ ] 管理端 revoke 测试设备后立刻返回未授权；
- [ ] suspended Vault 拒绝同步；
- [ ] 超过单文件、Vault quota、文件数和磁盘安全余量时给出明确错误且不产生半提交；
- [ ] Admin API 从 Cloudflare 域名不可访问；
- [ ] 日志、诊断包和审计不含 Secret、正文或完整路径；
- [ ] GC 先显示计划 ID、哈希和预计回收，再用完全相同哈希执行；有 active 未 ACK 设备时不越过其水位线。

## 7. 在线备份与恢复演练

目标必须是另一块加密磁盘或同步到另一台机器的目录：

```powershell
.\scripts\docker-backup.ps1 -KeepLast 3
.\scripts\docker-verify-backup.ps1 -BackupDirectory '<刚生成的备份目录>'
```

记录当前 Vault revision 和探针 SHA-256，然后在测试数据上执行：

```powershell
.\scripts\docker-restore.ps1 -BackupDirectory '<备份目录>' -ConfirmRestore
```

通过标准：manifest 校验通过；恢复后 doctor 正常、revision 和所有探针一致；客户端再次同步收敛；脚本保留旧 live data rollback 目录。之后把备份复制到不在服务器电脑上的加密位置并再次校验。

## 8. macOS 与移动真机

macOS 使用新的配对码作为第二设备，完成双向收敛、断网续传、重命名、批量删除以及客户端/服务端重启恢复。Android/iOS 至少一台真机完成：首次配对、前后台切换、Wi-Fi/蜂窝切换、拍照附件、Unicode 路径、冲突、删除恢复、32 MiB 默认移动限制和重启收敛。

移动端不得调用 Node API；超过移动限制必须在传输前明确拒绝，不能导致 Obsidian 崩溃。iOS 与 Android 均未覆盖时，不得把 1.0 标为全平台稳定，可在 Release 明确标记未验证平台。

## 9. RC 通过标准

- [ ] 上述 0–8 全部通过；
- [ ] Windows A/B、Mac、至少一台移动设备的第二次同步全 0；
- [ ] 至少连续 7 天日常测试无静默丢失、不可恢复冲突或数据库损坏；
- [ ] 一次真实 Docker Desktop 重启和一次异机备份恢复成功；
- [ ] 所有失败均有 Issue、复现步骤和处理结论；
- [ ] 重新运行全部自动化门禁，CI/CodeQL/依赖扫描通过。

通过后才进入 `1.0.0` stable 发布，不直接把未经人工验收的 RC 改名为正式版。
