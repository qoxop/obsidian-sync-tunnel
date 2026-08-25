# Sync Tunnel 1.0.0-rc.3 验收记录

本文记录 2026-08-23 至 2026-08-25 在专用测试 Vault 上完成的 RC3 验收。记录不包含配对码、设备凭据、Access Secret、Admin Token、真实 Vault 正文或完整本机路径。

## 环境

- Windows 11、Docker Desktop、Obsidian 1.13.7；
- 插件与服务端版本：`1.0.0-rc.3`；
- 两个 Windows Obsidian 测试目录配对同一个逻辑 Vault；
- 服务端使用 Docker Compose，SQLite、Chunk 和备份目录映射到 Windows 宿主机；
- Cloudflare Tunnel 只代理同步端口，管理端口仅发布到 `127.0.0.1`。

## 已通过项目

### 双设备与文件语义

- 设备 A、B 使用独立一次性配对码完成 Recommended + 安全合并；
- Markdown、Canvas、Unicode/空格路径、附件、5 MiB 二进制及测试插件程序文件双向收敛；
- `.DS_Store`、`Thumbs.db` 和其他插件的 `data.json` 未进入 Recommended 同步集合；
- 新增、修改、重命名、单个删除、20 文件批量删除和删除后历史恢复通过；
- 恢复操作创建了更高 revision；第二次手动同步上传、下载、重命名、远端删除、本地删除和冲突均为 0。

### 冲突与故障恢复

- 两台设备离线修改同一路径后，原始本地字节被保存为冲突副本，远端版本正确落盘；
- 冲突中心能够显示并解决记录，“保留两份”完成后两份内容均可继续同步；
- 64 MiB 分块上传在断网后恢复，只补齐未完成数据并最终收敛；
- 上传中退出并重开客户端后，持久化 outbox 能够续传；
- 服务容器重启后 revision 保持一致，客户端继续同步；
- 20 文件批量删除、历史恢复、服务端重启及客户端重启探针均通过。

### 身份与管理控制

- 设备凭据轮换后新凭据可继续同步；
- 管理后台撤销设备后，旧凭据下一次同步立即被拒绝；
- 逻辑 Vault 暂停后活动设备被拒绝，恢复为 active 后同步继续；
- 被撤销设备可用新的一次性配对码重新配对，安全合并后第二次同步全 0；
- 审计日志记录了凭据轮换、设备撤销、Vault 暂停/恢复、配对、备份和 GC 计划；
- 运行日志未发现 Authorization、Bearer、Client Secret、配对码或 API Token 字段。

### 管理后台与运维

- 概览统计和 doctor 正常，SQLite `integrity_check` 为 `ok`，缺失、损坏、孤立 Chunk 均为 0；
- 连接诊断中的本地服务、公网域名、Tunnel 边缘 DNS、cloudflared `/ready` 和公网 `/healthz` 全部通过；
- 在线备份创建完成，文件集、SHA-256 和 SQLite 完整性校验通过；随后使用该备份执行受控灾难恢复，旧数据目录被保留为带时间戳的 rollback 目录；
- 恢复后容器 healthy，统计保持 5 个 Vault、205 个当前文件和 revision 265，doctor 仍为 `ok`；Windows A、B 的 cursor 均为 265，outbox、inbox、待处理路径、重命名和六项同步计数均为 0；
- 两阶段 GC 预览能够生成，本次计划预计回收 0 B，未执行删除；
- 公网同步入口和 Cloudflare 域名上的 `/admin/`、`/admin/v1/session` 均返回 404；管理后台仍只可通过本机管理端口访问；
- Docker 重新构建部署后容器为 healthy，本机与公网健康接口均报告 `1.0.0-rc.3`。
- 2026-08-25 完成 Windows 关机后开机恢复验收。该电脑启用了 Windows 快速启动，因此内核启动时间未刷新，但系统事件确认发生关机，Docker 容器在本次开机后重新启动，cloudflared 自动服务保持 Running；
- 开机后的服务统计仍为 5 个 Vault、205 个当前文件、revision 265，doctor 为 `ok`，缺失、损坏、孤立 Chunk 均为 0；本机同步端、本机管理端和公网健康检查返回 200，公网管理接口继续返回 404；
- 两个 Windows 测试客户端开机后再次同步，cursor 和 acknowledged revision 均为 265，各跟踪 128 个文件；pending paths、outbox、inbox、pending renames 均为 0，上传、下载、重命名、远端删除、本地删除和冲突六项计数均为 0。

## 验收中发现并修复

1. 乐观上传收到 `revision_conflict` 时只保存了副本，没有写入冲突中心。现统一通过冲突记录函数保存副本和元数据，并增加单元测试。
2. 从 Obsidian 设置页点击“重新配对”时，设置窗口关闭但向导未显示。现延迟到设置模态框稳定后打开向导，并在真实界面完成撤销、重新配对和安全合并复验。
3. doctor 在无异常时把空切片编码为 `null`，维护页读取 `.length` 后白屏。现服务端保证输出空数组，前端同时增加空值容错，并增加 Go JSON 契约测试。

## 自动化门禁

- `go test ./...`：通过；
- 插件 Vitest：8 个测试文件、34 个测试通过；
- 插件 TypeScript 类型检查与生产构建：通过；
- 管理后台 Vitest：1 个测试文件、5 个测试通过；
- 管理后台 TypeScript 类型检查与生产构建：通过；
- `git diff --check`：通过。

## 正式版前仍需人工完成

- 把备份复制到不在服务器电脑上的加密介质并再次校验；
- Mac 安装 RC3 后复跑双向同步和中断恢复；
- 至少一台 Android 或 iOS 真机完成首次配对、前后台、网络切换、附件、冲突、删除恢复和重启收敛；
- GitHub CI、CodeQL 和依赖扫描对包含上述修复的提交全部通过。

这些未完成项不影响继续修复 RC，但在宣称 `1.0.0` 全平台稳定前必须有明确结果或在 Release 中标出未验证平台。
