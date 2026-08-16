# 用户配合操作清单（Docker Desktop + Cloudflare Tunnel）

Go 同步服务由 Docker Desktop 承载；SQLite 和日志通过 bind mount 保存在 Windows 宿主机。`cloudflared` 仍作为独立 Windows 服务运行，并访问映射到 `127.0.0.1` 的容器端口。

账号授权、域名选择、管理员权限、Obsidian SecretStorage 录入和真实 Vault 备份必须由你完成。

## A. 在使用任何真实笔记前

- [ ] 准备一个全新的测试 Vault，禁止直接拿主 Vault 做首次测试；
- [ ] 给真实 Vault 做一份不在同步目录内的完整离线备份；
- [ ] 确认备份包含隐藏的 `.obsidian` 目录，并随机恢复一两个文件验证备份可用；
- [ ] 决定 Vault ID，例如 `personal-notes`。同一 Vault 的设备必须一致，不同 Vault 必须不同；
- [ ] 给每台设备决定唯一 Device ID，例如 `desktop-home`、`laptop-work`、`phone`；
- [ ] 测试时关闭 Obsidian 官方 Sync、Syncthing、网盘双向同步等其他实时同步器；
- [ ] 检查其他插件的 `data.json` 是否含明文 API Key。完整同步会把这些文件写入宿主机 SQLite；不能接受时先加入排除列表。

## B. 确认 Docker Desktop

- [ ] 启动 Docker Desktop；
- [ ] 确认 Docker Desktop 设置为随 Windows 登录启动；
- [ ] 在 PowerShell 运行以下命令并确认 Client、Server、Compose 都能返回版本：

```powershell
docker version
docker compose version
```

无需进入 WSL，也不需要在 WSL 中维护数据。Docker Desktop 内部仍使用 Linux VM，但 bind mount 的源目录是明确的 Windows 路径。

## C. 初始化宿主机数据和 Token

普通 PowerShell，从仓库根目录运行：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\docker-init.ps1
```

默认会创建：

```text
<仓库>\.env                         Compose 本地参数，不提交 Git
<仓库>\runtime-data\                SQLite 和 JSON 日志
<仓库>\secrets\api-token.txt       只读挂载到容器的 API Token
```

如希望把数据放到其他 Windows 磁盘：

```powershell
.\scripts\docker-init.ps1 `
  -DataDirectory 'D:\ObsidianSync\data' `
  -TokenFile 'D:\ObsidianSync\secrets\api-token.txt'
```

你必须：

- [ ] 把脚本首次显示的 API Token 保存到密码管理器；
- [ ] 不截图、不提交、不写进 Vault 普通笔记；
- [ ] 完成插件 SecretStorage 录入后删除任何额外临时明文副本；
- [ ] 记录所选择的数据目录，以便备份和重装 Docker Desktop 后重新挂载。

已有 `.env` 时脚本不会覆盖。确实需要重写路径或端口时，显式使用 `-ForceConfig`；这不会删除 SQLite 或 Token。

## D. 构建并启动 Go 容器

```powershell
.\scripts\docker-up.ps1
```

脚本会构建 Linux 镜像、启动 Compose 服务并等待 Docker 健康检查成功。检查结果：

```powershell
docker compose ps
Invoke-RestMethod http://127.0.0.1:8787/healthz
.\scripts\docker-logs.ps1 -Tail 100
```

预期：

- 容器 `obsidian-sync-server` 状态为 `healthy`；
- 宿主机只出现 `127.0.0.1:8787`，不应是 `0.0.0.0:8787`；
- 数据目录中出现 `sync.db`、`sync.db-wal`、`sync.db-shm` 和 `server.jsonl`；
- `docker compose down` 后这些宿主机文件仍存在。

如果 8787 已占用：

```powershell
.\scripts\docker-init.ps1 -HostPort 18787 -ForceConfig
.\scripts\docker-up.ps1
```

之后 Cloudflare Origin 也必须改为 `http://127.0.0.1:18787`。

## E. 准备 Cloudflare Tunnel

- [ ] 拥有 Cloudflare 账号；
- [ ] 准备一个由 Cloudflare 管理 DNS 的域名；
- [ ] 在 Cloudflare Dashboard 的 **Networking > Tunnels** 创建 remotely-managed Tunnel；
- [ ] 为 Tunnel 添加 Public Hostname，例如 `sync.example.com`；
- [ ] Service 类型选择 HTTP，Origin URL 填 `http://127.0.0.1:8787`；
- [ ] 从 Tunnel 的 “Add a replica” 页面取得 Windows Tunnel Token；
- [ ] 不要把 Tunnel Token 发到聊天、提交到 Git 或写进普通脚本。

官方入口：

- [Cloudflare Tunnel 下载](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/)
- [创建 Tunnel](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/)
- [Tunnel Token](https://developers.cloudflare.com/tunnel/advanced/tunnel-tokens/)

## F. 安装 cloudflared Windows 服务

- [ ] 从官方页面安装最新 Windows x64 MSI 或 EXE；
- [ ] 用管理员 PowerShell 运行：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-cloudflare-tunnel.ps1
```

脚本会安全提示输入 Tunnel Token，不把它写入仓库或 PowerShell 命令历史。

检查：

```powershell
Get-Service cloudflared
Invoke-RestMethod https://sync.example.com/healthz
```

- [ ] `cloudflared` 服务状态为 Running；
- [ ] 公网健康检查返回 `status: ok`；
- [ ] Windows 版 `cloudflared` 不会自动更新，安排定期人工升级。

推荐但可选的第二层保护：

- [ ] 在 Cloudflare Zero Trust 中为主机名创建 Self-hosted Access Application；
- [ ] 创建 Service Token；
- [ ] Access Policy 使用 Service Auth，并只允许该 Service Token；
- [ ] 保存 Client ID 和 Client Secret；
- [ ] 注意 Access 也会保护 `/healthz`，公网检查需要同样的凭据或单独策略。

## G. 构建和安装 Obsidian 插件

插件仍通过本机 Node 工具链构建：

```powershell
.\scripts\build.ps1
.\scripts\install-plugin.ps1 -VaultPath '你的测试 Vault 路径'
```

也可以手工把以下三个文件复制到 `<Vault>\.obsidian\plugins\sync-tunnel`：

```text
dist\plugin\sync-tunnel\main.js
dist\plugin\sync-tunnel\manifest.json
dist\plugin\sync-tunnel\styles.css
```

启动或重载 Obsidian，在 **Settings > Community plugins** 中启用 Sync Tunnel。开发和首次同步必须使用专用测试 Vault：[Obsidian 官方插件开发指引](https://docs.obsidian.md/Plugins/Getting%20started/Build%20a%20plugin)。

## H. 配置插件

- [ ] Server URL：`https://sync.example.com`；
- [ ] Vault ID：填 A 步决定的值；
- [ ] Device ID：每台设备填不同值；
- [ ] API token：在 SecretStorage 组件中新建/选择条目，内容填 C 步 Token；
- [ ] 若启用了 Access，填写 Client ID，并把 Client Secret 放入另一个 SecretStorage 条目；
- [ ] 首次测试先关闭 Automatic sync；
- [ ] 检查排除列表；
- [ ] 点击 **Sync now**。

## I. 首次上线顺序

### 服务端为空

1. 只启用设备 A；
2. 在专用测试 Vault 点击 Sync now；
3. 检查上传数和冲突数；
4. 在设备 B 创建空测试 Vault，安装插件并使用同一 Vault ID；
5. B 点击 Sync now；
6. 比较 Markdown、图片和 `.obsidian` 文件的内容；
7. 测试修改、删除、离线冲突、容器重启和 Tunnel 断线；
8. 全部通过后才打开 Automatic sync。

### 第二台设备已有内容

先完整备份。首次同步会把双方不同的同路径文件判定为冲突，并生成冲突副本；最稳妥的方法是让第二台设备从空 Vault 拉取，再人工迁移只存在于旧 Vault 的内容。

## J. Docker 日常运维

启动或升级并重建镜像：

```powershell
.\scripts\docker-up.ps1
```

只启动已有镜像：

```powershell
.\scripts\docker-up.ps1 -NoBuild
```

查看日志：

```powershell
.\scripts\docker-logs.ps1 -Follow
```

一致性备份：

```powershell
.\scripts\docker-backup.ps1 -DestinationDirectory 'E:\Backups\ObsidianSync'
```

脚本会优雅停止容器、复制已经 checkpoint 的 SQLite、计算 SHA-256，再恢复原运行状态。备份不包含 API Token，但包含明文 Vault 内容，必须放在加密介质。

停止并删除容器及 Compose 网络，但保留数据：

```powershell
.\scripts\docker-down.ps1
```

日常检查：

- [ ] Windows 主机和 Docker Desktop保持运行；
- [ ] 定期检查 `docker compose ps` 和 `Get-Service cloudflared`；
- [ ] 定期运行 Docker 备份并复制到另一台设备；
- [ ] 继续保留 Vault 自身的独立版本化备份；
- [ ] 定期更新 `cloudflared`；
- [ ] 不要在同一 Vault 上同时启用另一套双向同步服务。

## K. 卸载和数据删除

```powershell
.\scripts\docker-down.ps1
```

这不会删除 bind-mounted SQLite 和 Token。只有在验证备份、确认永久放弃服务后，才手工删除 `.env` 所指向的 Windows 数据目录和 Token 文件。不要使用 `docker compose down -v` 代替数据管理；本方案的数据本来就不在匿名卷中。

原生 Windows EXE 服务脚本仍保留作为备选迁移路径，但 Docker 是当前推荐部署方式，不要同时运行两套服务占用相同端口或数据库。
