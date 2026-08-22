# Docker Desktop 部署与运维

## 安全拓扑

| 端口 | 宿主机绑定 | 使用者 | 是否进入 Cloudflare |
|---|---|---|---|
| 8787 | `127.0.0.1` | `cloudflared` / 本机健康检查 | 是 |
| 8788 | `127.0.0.1` | Windows 管理脚本 | 否，禁止 |

容器内部两个端口显式监听 `0.0.0.0` 供 Docker NAT 使用，但 Compose 的 `host_ip` 固定为 `127.0.0.1`。服务要求两个独立的显式 non-loopback opt-in，防止原生部署误暴露管理端。

最终镜像使用非 root distroless、只读根文件系统、删除 capabilities、`no-new-privileges`，`/tmp` 为小型 tmpfs。`/data` 和 `/backups` 是 Windows bind mount；Admin Token 通过只读 Compose secret 挂入 `/run/secrets/sync_admin_token`。

## 全新初始化

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\docker-init.ps1
.\scripts\docker-up.ps1
```

默认创建：

```text
.env                         版本、回环端口、限制和宿主机路径
runtime-data/                sync.db、WAL、Chunk、JSONL 日志
runtime-backups/             在线一致备份
secrets/admin-token.txt      本机 Admin Token，不用于客户端
```

自定义路径：

```powershell
.\scripts\docker-init.ps1 `
  -DataDirectory 'D:\ObsidianSync\data' `
  -BackupDirectory 'E:\ObsidianSyncBackups' `
  -AdminTokenFile 'D:\ObsidianSync\secrets\admin-token.txt' `
  -HostPort 8787 -AdminHostPort 8788 `
  -MaxFileBytes 67108864 `
  -Version '1.0.0-rc.1'
```

已有 `.env` 默认不会覆盖。1.0 不兼容升级必须先保留旧数据，再用新目录和 `-ForceConfig`，见[升级指引](UPGRADE_TO_1.0.zh-CN.md)。

## 验证监听和容器

```powershell
docker compose config --quiet
docker compose ps
docker compose port sync-server 8787
docker compose port sync-server 8788
Invoke-RestMethod http://127.0.0.1:8787/healthz
Invoke-RestMethod http://127.0.0.1:8788/healthz
.\scripts\docker-logs.ps1 -Tail 100
```

两项 `docker compose port` 必须显示 `127.0.0.1`。不要把 Compose `host_ip` 改为 `0.0.0.0`，不要把 Admin Token 放到 `.env`，不要让 Tunnel 指向 8788。

## 管理 Vault 和设备

```powershell
.\scripts\admin.ps1 -ListVaults
.\scripts\admin.ps1 -CreateVault -VaultId personal-notes -DisplayName 'Personal notes'
.\scripts\admin.ps1 -UpdateVault -VaultId personal-notes -DisplayName 'Personal notes' -VaultStatus suspended
.\scripts\admin.ps1 -CreatePairing -VaultId personal-notes -TTLSeconds 600
.\scripts\admin.ps1 -ListDevices -VaultId personal-notes
.\scripts\admin.ps1 -SetDeviceStatus -VaultId personal-notes -DeviceId '<id>' -Status revoked
.\scripts\admin.ps1 -Stats
.\scripts\admin.ps1 -Doctor
.\scripts\admin.ps1 -ListAudit
```

`admin.ps1` 默认从 `secrets/admin-token.txt` 读取 Token，不打印 Token。配对码会显示一次，短期有效且只能用一次；每台设备生成新的码。

GC 始终两步：

```powershell
$plan = .\scripts\admin.ps1 -PlanGC -RetentionDays 90 -KeepVersions 20
$plan
.\scripts\admin.ps1 -ExecuteGC -PlanId $plan.id -PlanHash $plan.hash
```

执行前审查水位线、revision、路径数和预计字节。Hash 不匹配或计划已执行时服务拒绝操作。

## 在线备份、验证和恢复

服务运行时不要复制 live `sync.db` 或单独复制 `blobs/`。在线备份：

```powershell
.\scripts\docker-backup.ps1 -KeepLast 7
.\scripts\docker-verify-backup.ps1 -BackupDirectory '<备份目录>'
```

服务用 `VACUUM INTO` 生成 SQLite 快照，复制 Chunk，并生成逐文件 SHA-256 `backup.json`。`-KeepLast` 只删除配置备份根目录内、名称符合时间格式且含 manifest 的旧备份。

恢复仅用于已经验证的备份：

```powershell
.\scripts\docker-restore.ps1 -BackupDirectory '<备份目录>' -ConfirmRestore
```

脚本停止服务，把 live 数据移动为带时间戳 rollback 目录，新建数据目录、复制备份、启动并等待 healthy；失败时自动保留失败目录并恢复旧数据。成功后旧 live 数据仍保留，确认一段时间后再人工处理。

备份包含明文 Vault 内容但不包含 Admin/设备 Token。至少复制一份到服务器电脑以外的加密存储，并定期执行恢复演练。

## Cloudflare Tunnel

Windows `cloudflared` 保持独立服务，Origin 为：

```text
http://127.0.0.1:<OBSIDIAN_SYNC_PORT>
```

推荐在该 Public Hostname 上配置 Cloudflare Access Self-hosted Application + Service Token。Access 保护边缘，设备凭据保护 Vault，两者都需要。管理端永不创建 Public Hostname。

502 排查顺序：本机 8787 health → `docker compose ps`/logs → `Get-Service cloudflared` → Tunnel connector → Access policy。`ERR_INTERNET_DISCONNECTED` 是客户端网络层错误，恢复网络后持久化 outbox/inbox 继续。

## 生命周期

```powershell
.\scripts\docker-up.ps1            # 构建并启动/升级
.\scripts\docker-up.ps1 -NoBuild   # 启动已有镜像
.\scripts\docker-logs.ps1 -Follow
.\scripts\docker-down.ps1          # 删除容器/网络，保留 bind mount
```

Docker Desktop 和 `cloudflared` 应随 Windows 登录启动。升级前先做在线备份；镜像回滚不等于数据库回滚，必须恢复同一时点的整个 `sync.db + blobs` 数据集。
