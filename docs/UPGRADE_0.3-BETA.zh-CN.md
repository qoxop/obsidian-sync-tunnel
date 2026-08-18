# 升级到 0.3.0 Beta

0.3 候选版用于专用测试 Vault，不应直接用于唯一的真实数据。它加入事件队列、持久化 outbox/inbox、分块断点续传、原子重命名和批量删除，并把服务端数据库升级到 schema 4。

## 1. 升级边界

- 保持单用户、多 Vault、多设备；默认使用“推荐安全模式”，完整 Vault 必须主动选择；
- 服务端仍为明文存储，端到端加密安排在 2.0；
- 先用两台桌面设备和两个测试 Vault 验证；移动端纳入 1.0 验收，但本次 Beta 先验证桌面流式传输；
- Cloudflare Tunnel 继续作为入口，Cloudflare Access Service Token 作为推荐的第二层认证；
- 同步不能替代备份，真实 Vault 始终保留不在同步目录中的独立备份。

## 2. 升级前必须完成

1. 在每台设备手动同步一次，确认第二次同步显示上传、下载、删除和冲突均为 0；
2. 关闭其他设备上的 Obsidian，避免升级窗口内继续写入；
3. 给测试 Vault 做一份包含 `.obsidian` 的独立备份；
4. 拉取最新代码后，为服务端创建完整一致备份：

```powershell
git pull --ff-only
.\scripts\docker-backup.ps1 -DestinationDirectory 'E:\Backups\ObsidianSync'
```

记录输出目录，并确认其中同时存在 `backup.json`、`data/sync.db`；若已有 Chunk，还应存在 `data/blobs/`。备份包含明文 Vault 内容，必须放在加密存储中，最好再复制到另一台设备。

## 3. 升级服务端

候选版本号发布后，在仓库根目录执行：

```powershell
.\scripts\docker-set-version.ps1 -Version 0.3.0-beta.1
.\scripts\docker-up.ps1
Invoke-RestMethod http://127.0.0.1:8787/healthz
```

首次启动会在事务中把 SQLite 升级到 schema 4，并在映射的数据目录中创建 `blobs/chunks`。先升级服务端，再升级插件；0.3 服务端保留 0.2 全文件 API，允许客户端逐台升级。

## 4. 通过 BRAT 安装候选插件

只在测试设备上操作：

1. 打开 BRAT 设置，添加 `qoxop/obsidian-sync-tunnel`；
2. 允许 BRAT 使用最新 prerelease，或选择标签 `0.3.0-beta.3`；
3. 重载 Obsidian；
4. 打开 Sync Tunnel 设置并点击 **Test connection**，确认服务端版本为 `0.3.0-beta.1`；
5. 先查看首次同步预览，再选择“推荐安全模式”。

如果 BRAT 没有显示候选版，先确认 BRAT 为 1.1 或更高版本，并在 GitHub Release 页面确认该标签包含 `main.js`、`manifest.json` 和 `styles.css`。

使用 Mac 作为第二台设备时，也可以通过[macOS 第二设备脚本化验收](MACOS_SECOND_DEVICE_TEST.zh-CN.md)直接从 Release 安装固定版本，并自动检查同步状态、探针哈希和服务端确认 revision。

不要安装插件 `0.3.0-beta.1`：它在 Obsidian 桌面端通过动态 `import()` 加载 Node 文件系统模块，下载校验会被 `app://` CORS 策略拦截。`0.3.0-beta.3` 还修复了 Recommended safe 未显式扫描 Obsidian 配置目录的问题。服务端 `0.3.0-beta.1` 与这些客户端修复协议兼容，不需要重建服务器容器。

## 5. 人工验收清单

依次完成，每一步后再点击一次同步，确认第二次为全 0：

实际执行结果和剩余场景记录在[0.3 Beta 人工验收记录](BETA_ACCEPTANCE_0.3.zh-CN.md)。

- [ ] 设备 A 新建 Markdown、图片和一个超过 4 MiB 的测试文件，设备 B 能完整下载且哈希一致；
- [ ] 上传大文件时临时断网，恢复后可以完成同步，服务端不重复保存已经确认的 Chunk；
- [ ] 设备 A 重命名文件，设备 B 只看到新路径且内容不变；
- [ ] 一次删除至少两个测试文件，另一台设备同步删除；
- [ ] 同步中退出并重开 Obsidian，持久化任务可以安全恢复；
- [ ] 两台设备离线修改同一路径，恢复联网后产生冲突副本，不丢失任一版本；
- [ ] `.obsidian/plugins/sync-tunnel/data.json` 没有被同步；
- [ ] 推荐安全模式没有复制其他插件的敏感 `data.json`；
- [ ] 服务端重启后再次同步仍为全 0。

记录失败步骤、两端时间、文件路径和不含 Token/正文的日志。不要在截图或 Issue 中公开 API Token、Cloudflare Secret、Vault 内容或真实域名。

## 6. 停止测试与回滚

出现重复删除、哈希不一致、无法收敛或服务端无法健康启动时，立即关闭所有测试设备上的自动同步，并保留现场日志。

0.2 服务不能打开已经升级到 schema 4 的数据库，因此不能只把镜像版本改回 `0.2.0`。安全回滚方式是：

1. 运行 `.\scripts\docker-down.ps1` 停止容器；
2. 把升级前备份中的整个 `data/` 复制到一个全新的空目录；
3. 在 `.env` 中把 `OBSIDIAN_SYNC_DATA_DIR` 改为该新目录；
4. 运行 `.\scripts\docker-set-version.ps1 -Version 0.2.0`；
5. 运行 `.\scripts\docker-up.ps1` 并验证 `/healthz`；
6. 在 BRAT 中切回稳定版插件，重载 Obsidian后再恢复自动同步。

不要覆盖或删除原 0.3 数据目录；在确认恢复结果前，它是排障证据。不要把不同备份中的 `sync.db` 和 `blobs/` 拼接使用。
