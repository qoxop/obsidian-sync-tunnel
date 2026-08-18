# 0.3 Beta 人工验收记录

本文件记录专用测试 Vault 的已验证结果，不包含 Server URL、Vault ID、Device ID、Token、Cloudflare Secret、真实笔记路径或正文。同步不是备份；未完成的项目不能视为稳定版发布结论。

## 2026-08-18：Windows 与 macOS 双向基础验收

### 环境

- 服务端：Windows Docker Desktop，`0.3.0-beta.1`，健康检查通过；
- 客户端 A：Windows，Obsidian `1.13.7`，Sync Tunnel `0.3.0-beta.2`；
- 客户端 B：macOS，空本地测试 Vault，Sync Tunnel `0.3.0-beta.2`；
- 同步模式：Recommended safe；
- macOS 验收工具：`scripts/macos-device-test.sh`。

### 已通过

- [x] macOS 空 Vault 从同一服务端 Vault ID 完成首次下载；
- [x] macOS 首次同步完成后第二次同步收敛，cursor `135`、跟踪记录 `78`，pending paths、outbox、inbox 和 pending renames 均为 `0`；
- [x] macOS 创建 Markdown、Canvas、PNG、Unicode/空格路径、嵌套路径和 5 MiB 二进制探针；
- [x] macOS 上传探针后 cursor 从 `135` 推进到 `141`，跟踪记录从 `78` 增加到 `84`；
- [x] macOS 对 5 个内容文件完成 SHA-256 本地复验，并确认每个文件取得服务端 revision；
- [x] macOS `.DS_Store` 未进入同步状态；
- [x] Windows 下载同一探针目录，5 个文件的 SHA-256 全部与 macOS `SHA256SUMS` 一致；
- [x] Windows 下载的二进制文件大小为 `5,242,880` 字节；
- [x] Windows 未出现 `.DS_Store`；
- [x] Windows 第二次同步后客户端 cursor 与服务端 revision 均为 `141`，跟踪记录 `84`，所有持久化队列为 `0`，`needsFullScan=false`。

### 结论

Windows 与 macOS 之间的空设备初始化、双向传输、跨平台路径、5 MiB 分块文件、内容哈希和空闲收敛通过。本轮没有发现文件损坏、遗漏下载或排除规则失效。

## 仍待验证

- [ ] 上传大文件时中断网络，恢复后不重复上传已确认 Chunk；
- [ ] Windows 重命名文件后，macOS 只保留新路径且哈希不变；
- [ ] 一次删除至少两个测试文件并在另一端收敛；
- [ ] 同步中退出并重开 Obsidian，持久化任务可以恢复；
- [ ] 两台设备离线修改同一路径，恢复后产生冲突副本且不丢版本；
- [ ] 重启服务端容器后，两端再次同步仍为全 `0`；
- [ ] 明确验证其他插件的敏感 `data.json` 未被 Recommended safe 模式复制；
- [ ] 验证 Sync Tunnel 自身 `data.json` 始终保持设备本地化。

下一轮从网络中断和重命名场景开始。任一步出现重复删除、哈希不一致或无法收敛时，立即关闭自动同步并保留现场。
