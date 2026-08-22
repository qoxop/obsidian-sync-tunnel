# 升级到 0.2.0

> 已归档：1.0 不支持此就地升级路径。请使用[从任何 0.x 升级到 1.0](UPGRADE_TO_1.0.zh-CN.md)。

0.2.0 引入 SQLite schema version、Protocol v2 能力发现和固定 revision 快照。升级顺序必须是“先服务端，后插件”，否则新插件在第一次快照对账时会报告服务端协议过旧并停止同步。

## 1. 升级前

1. 确认当前同步已经完成，Obsidian 状态栏没有正在运行的任务；
2. 保留原始 Vault 的独立备份；
3. 为服务端数据库创建一致备份：

```powershell
.\scripts\docker-backup.ps1
```

记录脚本输出的备份目录。备份包含明文 Vault 数据，应位于加密存储中。

## 2. 更新服务端

在仓库目录执行：

```powershell
git pull --ff-only
.\scripts\docker-set-version.ps1 -Version 0.2.0
.\scripts\docker-up.ps1
Invoke-RestMethod http://127.0.0.1:8787/healthz
```

`docker-up.ps1` 会构建新镜像并等待容器健康。首次打开现有数据库时会以事务方式把 `PRAGMA user_version` 升级为 1，并创建快照查询索引；不会改写已有文件和变更内容。

## 3. 验证服务端协议

打开 Obsidian 的 Sync Tunnel 设置，点击 **Test connection**。预期显示：

- 连接成功；
- 服务端版本 `0.2.0`；
- 支持 Protocol 2 和 `snapshot-v1`。

如果插件还没有更新，可先只验证 `/healthz`，等本地插件安装完成后再执行连接测试。

## 4. 安装插件候选版本

正式 GitHub Release 发布前，在一台测试 Vault 上从本地构建安装：

```powershell
.\scripts\build.ps1
.\scripts\install-plugin.ps1 -VaultPath "C:\你的\测试Vault"
```

重启 Obsidian 后启用 Sync Tunnel。不要先在唯一的真实 Vault 上验证候选版本。

## 5. 升级行为

- 已经使用 0.1 的设备不会重新询问首次同步方向；
- 第一次 0.2 同步会读取一次服务端快照，之后保存新的 filter fingerprint；
- 新安装设备必须先查看首次同步预览，并选择安全合并、以远端为主或以本地为主；
- Sync Tunnel 自身目录不再同步，后续更新统一由 BRAT、GitHub Release 或本地安装脚本完成；
- 如果其他插件、主题或 CSS 被下载，插件会提示重启 Obsidian。

## 6. 失败处理

如果容器无法启动：

```powershell
.\scripts\docker-logs.ps1
```

不要删除 `runtime-data`、数据库或备份。0.2 的数据库迁移是附加式的，但回退前仍应保留升级前备份。将错误日志中不含 Token、域名和 Vault 内容的部分提供给维护者，再决定是修复配置、暂时启动旧镜像，还是恢复备份。

插件连接失败时先点击 **Test connection**，分别排查 Tunnel、Cloudflare Access、API Token、Vault ID 和协议版本。不要把 Cloudflare Tunnel Token 填入插件。
