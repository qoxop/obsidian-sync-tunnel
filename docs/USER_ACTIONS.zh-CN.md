# 用户配合操作清单（Windows）

代码可以自动完成本机文件、构建和服务配置，但 Cloudflare 账号授权、域名选择、管理员权限、Obsidian SecretStorage 录入和真实 Vault 备份必须由你完成。

## A. 在使用任何真实笔记前

- [ ] 准备一个全新的测试 Vault，禁止直接拿主 Vault 做首次测试；
- [ ] 给真实 Vault 做一份不在同步目录内的完整离线备份；
- [ ] 确认备份包含隐藏的 `.obsidian` 目录，并随机恢复一两个文件验证备份可用；
- [ ] 决定一个 Vault ID，例如 `personal-notes`。同一 Vault 的设备必须一致，不同 Vault 必须不同；
- [ ] 给每台设备决定唯一 Device ID，例如 `desktop-home`、`laptop-work`、`phone`。
- [ ] 测试时关闭 Obsidian 官方 Sync、Syncthing、网盘双向同步等其他实时同步器，避免两个同步系统相互制造变更；
- [ ] 检查其他插件的 `data.json` 是否含明文 API Key。完整同步会把这些文件写入本机 SQLite；若不能接受，先把对应路径加入排除列表。

## B. 准备 Cloudflare

- [ ] 拥有 Cloudflare 账号；
- [ ] 准备一个由 Cloudflare 管理 DNS 的域名；
- [ ] 在 Cloudflare Dashboard 的 **Networking > Tunnels** 创建 remotely-managed Tunnel；
- [ ] 为 Tunnel 添加 Public Hostname，例如 `sync.example.com`；
- [ ] Service 类型选择 HTTP，Origin URL 填 `http://127.0.0.1:8787`；
- [ ] 从 Tunnel 的 “Add a replica” 页面复制 Windows 安装命令或 Tunnel Token；
- [ ] 不要把 Tunnel Token 发到聊天、提交到 Git 或写进普通脚本。

官方入口：

- [Cloudflare Tunnel 下载](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/)
- [创建 Tunnel](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/get-started/)
- [Tunnel Token](https://developers.cloudflare.com/tunnel/advanced/tunnel-tokens/)

推荐但可选的第二层保护：

- [ ] 在 Cloudflare Zero Trust 中为 `sync.example.com` 创建 Self-hosted Access Application；
- [ ] 创建 Service Token；
- [ ] Access Policy 使用 Service Auth，并只允许该 Service Token；
- [ ] 保存 Client ID 和 Client Secret。Secret 之后放入 Obsidian SecretStorage；
- [ ] 注意 Access 规则也会保护 `/healthz`，公网健康检查需要相同凭据或单独策略。

## C. 构建项目

在普通 PowerShell 中，从仓库根目录运行：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\build.ps1
```

预期产物：

```text
dist\server\obsidian-sync-server.exe
dist\plugin\obsidian-sync-tunnel\main.js
dist\plugin\obsidian-sync-tunnel\manifest.json
dist\plugin\obsidian-sync-tunnel\styles.css
dist\obsidian-sync-tunnel-plugin-0.1.0.zip
```

## D. 安装 Go 同步服务

用“以管理员身份运行”的 PowerShell：

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\scripts\install-server.ps1
```

脚本会：

1. 安装文件到 `C:\ProgramData\ObsidianSyncTunnel`；
2. 首次生成 32 字节随机同步 Token；
3. 限制 Token 和配置文件 ACL；
4. 创建并启动自动启动的 `ObsidianSyncTunnel` Windows 服务；
5. 在终端显示同步 Token。

你必须：

- [ ] 把同步 Token 临时保存到密码管理器；
- [ ] 不截图、不提交、不放进 Vault 普通笔记；
- [ ] 完成插件 SecretStorage 录入后删除任何临时明文副本。

检查本机服务：

```powershell
Get-Service ObsidianSyncTunnel
Invoke-RestMethod http://127.0.0.1:8787/healthz
```

## E. 安装 cloudflared 服务

- [ ] 从官方页面安装最新 Windows x64 MSI 或 EXE；
- [ ] 以管理员身份运行 Cloudflare Dashboard 提供的 `cloudflared.exe service install <TOKEN>`；
- [ ] 检查服务：`Get-Service cloudflared`；
- [ ] 访问 `https://sync.example.com/healthz`，应返回 `status: ok`；
- [ ] Windows 版不会自动更新，安排定期人工更新。

也可以使用仓库脚本，它不会保存 Token：

```powershell
.\scripts\install-cloudflare-tunnel.ps1
```

脚本会安全提示输入 Token，避免把 Token 留在 PowerShell 命令历史中。

## F. 安装 Obsidian 插件

对每个设备的 Vault：

1. 关闭 Obsidian 或先禁用旧版本插件；
2. 创建 `<Vault>\.obsidian\plugins\obsidian-sync-tunnel`；
3. 把 `dist\plugin\obsidian-sync-tunnel` 中的三个文件复制进去；
4. 启动 Obsidian；
5. **Settings > Community plugins** 中启用 Sync Tunnel；
6. 打开插件设置。

Obsidian 官方手动安装要求复制 `main.js`、`manifest.json` 和可选的 `styles.css` 到插件 ID 对应目录。开发时应始终使用专用测试 Vault：[官方插件开发指引](https://docs.obsidian.md/Plugins/Getting%20started/Build%20a%20plugin)。

## G. 配置插件

- [ ] Server URL：`https://sync.example.com`；
- [ ] Vault ID：填 A 步决定的值；
- [ ] Device ID：每台设备填不同值；
- [ ] API token：在 SecretStorage 组件中新建/选择条目，内容填 D 步 Token；
- [ ] 若启用了 Access，填写 Client ID，并把 Client Secret 放入另一个 SecretStorage 条目；
- [ ] 首次测试先关闭 Automatic sync；
- [ ] 检查排除列表是否符合需要；
- [ ] 点击 **Sync now**。

## H. 首次上线顺序

### 服务端为空

1. 只启用设备 A；
2. 在专用测试 Vault 点击 Sync now；
3. 检查提示中的上传数和冲突数；
4. 在设备 B 创建空测试 Vault，安装插件并使用同一 Vault ID；
5. B 点击 Sync now；
6. 逐字节比较几个 Markdown、图片和 `.obsidian` 文件；
7. 按开发方案中的人工验收矩阵测试修改、删除、离线冲突和 Tunnel 断线；
8. 全部通过后才打开 Automatic sync。

### 第二台设备已有内容

先完整备份。首次同步会把双方不同的同路径文件判定为冲突，并生成冲突副本；文件很多时可能出现大量副本。最稳妥的方式是让第二台设备从空 Vault 拉取，再人工迁移只存在于旧 Vault 的内容。

## I. 日常运维

- [ ] Windows 主机保持开机、联网，并避免长期睡眠；
- [ ] 定期执行 `.\scripts\backup-server.ps1`，把备份复制到另一块磁盘；
- [ ] 继续保留 Vault 自身的独立版本化备份；
- [ ] 定期检查 `Get-Service ObsidianSyncTunnel, cloudflared`；
- [ ] 定期更新 `cloudflared`；Windows 安装不会自动更新；
- [ ] 插件或服务升级前做服务数据备份；
- [ ] 不要在同一 Vault 上同时启用另一套会写入相同文件的双向同步服务；
- [ ] Token 泄露时，重新生成同步 Token、更新服务端 token 文件并在所有设备更新 SecretStorage；
- [ ] Tunnel Token 泄露时在 Cloudflare 撤销并重新安装连接器；
- [ ] Access Token 泄露时在 Zero Trust 撤销并替换 Service Token。

查看服务日志：

```powershell
Get-Content C:\ProgramData\ObsidianSyncTunnel\logs\server.jsonl -Tail 100
```

开发环境前台运行时，结构化日志直接输出到终端。

## J. 卸载与恢复

卸载服务但保留数据：

```powershell
.\scripts\uninstall-server.ps1
```

删除 `C:\ProgramData\ObsidianSyncTunnel` 会永久删除服务端数据库和 Token，必须在确认备份后手工执行；卸载脚本默认不会删除它。

恢复时先停止服务，用备份替换数据目录，再启动服务。恢复旧数据库后，各客户端可能拥有更大的游标；首版应在插件停用状态下备份各设备，并清除本插件 `data.json` 让设备从游标 0 重新核对。正式恢复脚本和设备水位协调属于阶段 B。
