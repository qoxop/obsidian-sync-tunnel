# Docker Desktop 部署与恢复

## 正常安装与升级

确保 Docker Desktop 已启动，在项目目录运行：

```powershell
Set-ExecutionPolicy -Scope Process Bypass -Force
.\scripts\setup.ps1
```

首次运行会创建本地配置、持久化数据目录和备份目录；之后运行同一个命令会使用现有配置重新构建并升级服务。脚本完成后会自动打开：

<http://127.0.0.1:8788/admin/>

需要自定义数据位置时，仅在首次运行时传入：

```powershell
.\scripts\setup.ps1 `
  -DataDirectory 'D:\ObsidianSync\data' `
  -BackupDirectory 'E:\ObsidianSyncBackups'
```

如需重新生成 `.env`，使用 `-ResetConfiguration`。这不会主动删除旧数据，但必须确认新配置仍指向正确的数据目录。

## 日常管理

管理页面提供：

- Vault 创建、暂停和配额；
- 一次性配对码；
- 设备查看、退役和撤销；
- 服务统计、运行日志和审计日志；
- SQLite 与 Chunk 完整性检查；
- 在线一致性备份及校验；
- 两阶段垃圾回收预览与执行。

普通用户不需要运行管理、日志、备份或校验脚本。容器的 `restart: unless-stopped` 会在 Docker Desktop 启动后恢复服务。

## 网络边界

| 端口 | 用途 | 允许访问范围 |
|---|---|---|
| `8787` | Obsidian 同步 API | 本机 `cloudflared` |
| `8788` | 管理页面 | 仅服务器电脑本机 |

两个端口都必须保持绑定到 `127.0.0.1`。Cloudflare Tunnel 只能指向 `http://127.0.0.1:8787`，绝不能转发 `8788`。

默认管理模式不要求 Admin Token，因为管理端口不会离开本机。若本机为多人共用，可重新初始化时使用 `-AdminAuth token`。

## 备份

在管理页面的 **维护与备份** 中点击 **立即备份**，再点击 **校验**。备份保存在 `.env` 配置的宿主机备份目录中。

服务端和同机备份都可能因磁盘故障同时丢失。应定期把已经校验的备份复制到另一台设备的加密存储。

## 灾难恢复

恢复会替换整个运行数据集，不能由正在使用该 SQLite 的服务安全执行。因此这是唯一保留的停服命令行操作。

1. 在管理页面确认目标备份已通过校验；
2. 关闭所有客户端的自动同步；
3. 在服务器项目目录运行：

   ```powershell
   .\scripts\docker-restore.ps1 `
     -BackupDirectory '<已校验的备份目录>' `
     -ConfirmRestore
   ```

脚本会再次校验备份、停止容器、保留旧数据回滚目录、恢复并等待健康检查。恢复失败时会自动尝试回滚。

## 无法打开管理页面

按以下顺序检查：

1. Docker Desktop 是否正在运行；
2. Docker Desktop 的 Containers 页面中 `obsidian-sync-server` 是否为 healthy；
3. 重新运行 `.\scripts\setup.ps1`；
4. 查看 Docker Desktop 中该容器的 Logs。

只有服务能够启动后，管理页面中的运行日志才可用于进一步排查。
