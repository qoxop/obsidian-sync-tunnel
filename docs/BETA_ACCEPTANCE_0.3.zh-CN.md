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

## 2026-08-18：大文件断网续传验收

### 已通过

- [x] macOS 上传单个 64 MiB 文件，在服务端收到 `3` 个 4 MiB Chunk（共 12 MiB）、Vault revision 仍为 `147` 时断开网络；
- [x] 断网后插件明确报告 `ERR_INTERNET_DISCONNECTED`，未提前提交不完整文件；
- [x] 恢复网络后只补传剩余 `13` 个 Chunk，最终清单恰好包含 `16` 个 4 MiB Chunk；
- [x] 断网前已确认的 `3` 个 Chunk 在恢复后仍存在，大小和修改时间均未变化，证明没有重复写入；
- [x] 64 MiB 文件只产生一次最终文件提交，Vault revision 从 `147` 推进到 `148`；
- [x] macOS 恢复后第二次同步全部计数为 `0`，脚本返回 `INTERRUPT_PROBE_LOCAL_HASH_PASS`；
- [x] Windows 从 revision `147` 拉取到 `148`，下载文件大小为 `67,108,864` 字节，整文件 SHA-256 与服务端一致；
- [x] Windows 再次同步全部计数为 `0`，跟踪记录 `91`，pending paths、outbox、inbox 和 pending renames 均为 `0`，`needsFullScan=false`。

### 结论

分块上传在真实断网后能够复用服务端已经确认的 Chunk，只补传缺失部分；文件在 Chunk 齐全且整文件哈希校验通过后才提交。macOS 上传端与 Windows 下载端均完成整文件哈希复验并收敛为空闲状态。

## 2026-08-18：跨设备重命名验收

### 已通过

- [x] Windows 在 Obsidian 内将 macOS 探针中的 Markdown 文件重命名，首次同步计数为“重命名 `1`”，其余为 `0`；
- [x] 服务端以相邻 revision `149` 和 `150` 记录旧路径 tombstone 与新路径，内容哈希保持不变；
- [x] Windows 客户端 cursor 推进到 `150`，pending paths、pending renames、outbox 和 inbox 均为 `0`，`needsFullScan=false`；
- [x] macOS 同步后旧路径消失、新路径存在，整文件 SHA-256 与重命名前一致，终端复验返回 `RENAME_PROBE_PASS`。

### 结论

跨设备重命名能够保留文件内容并让另一设备收敛到唯一的新路径，没有残留旧文件或产生内容变化。

## 2026-08-18：跨设备批量删除验收

### 已通过

- [x] Windows 将两个已同步测试文件移入被排除且可恢复的 `.trash`，插件在一次同步中报告“远端删除 `2`”，其余为 `0`；
- [x] 服务端为两个不同路径生成 revision `151` 和 `152` 的 tombstone；
- [x] Windows 第二次同步全部计数为 `0`，cursor 为 `152`，pending paths、pending renames、outbox 和 inbox 均为 `0`，`needsFullScan=false`；
- [x] macOS 同步后两个目标文件均不存在，未被删除的重命名 Markdown 文件仍存在，终端复验返回 `BATCH_DELETE_PROBE_PASS`。

### 结论

一次同步可以原子地提交多个删除意图，另一设备能够应用全部 tombstone，且不会误删未包含在操作中的相邻测试文件。

## 2026-08-18：服务端容器重启验收

### 已通过

- [x] Windows 对运行中的 Docker Compose `sync-server` 执行重启，容器重新进入 `healthy`；
- [x] 重启前后的 `149` 条历史变更元数据指纹一致，服务端 latest revision 保持为 `152`；
- [x] 重启后仍可查询 revision `151` 和 `152` 的两个最新 tombstone；
- [x] Windows 重启后同步全部计数为 `0`，cursor 为 `152`，pending paths、pending renames、outbox 和 inbox 均为 `0`，`needsFullScan=false`；
- [x] macOS 通过 Cloudflare Tunnel 重新连接后看到预期 revision `152`，状态脚本返回 `Client state PASS`。

### 结论

服务端进程重启不会丢失 SQLite 中的 Vault revision、历史变更或 tombstone；Tunnel 后的 Windows 与 macOS 客户端均可继续使用原有游标收敛。

## 仍待验证

- [x] 上传大文件时中断网络，恢复后不重复上传已确认 Chunk；
- [x] Windows 重命名文件后，macOS 只保留新路径且哈希不变；
- [x] 一次删除至少两个测试文件并在另一端收敛；
- [ ] 同步中退出并重开 Obsidian，持久化任务可以恢复；
- [ ] 两台设备离线修改同一路径，恢复后产生冲突副本且不丢版本；
- [x] 重启服务端容器后，两端再次同步仍为全 `0`；
- [ ] 明确验证其他插件的敏感 `data.json` 未被 Recommended safe 模式复制；
- [ ] 验证 Sync Tunnel 自身 `data.json` 始终保持设备本地化。

下一轮从客户端持久化任务恢复场景开始。任一步出现重复删除、哈希不一致或无法收敛时，立即关闭自动同步并保留现场。
