# Obsidian Sync Tunnel

[English](README.en.md) | 简体中文

面向个人自托管的 Obsidian 全 Vault 同步：Go + SQLite 服务运行在 Windows Docker Desktop，数据通过 bind mount 持久化到 Windows 宿主机，Cloudflare Tunnel 提供 HTTPS 入口，Obsidian 插件覆盖 Windows、macOS、Android 和 iOS。

当前代码版本为 `1.0.0-rc.1`。实现和自动化测试已经完成，进入人工验收阶段；在[人工验收清单](docs/MANUAL_ACCEPTANCE_1.0.zh-CN.md)全部通过前，不应用于唯一一份真实 Vault，也不应替代独立备份。

## 1.0 产品边界

- 单用户、多 Vault、多设备；
- 默认“推荐安全模式”，可主动选择 Notes、Full Vault 或自定义范围；
- 同步笔记、附件、图片、Canvas、主题、CSS 和其他插件程序；Full Vault 还会同步其他插件的 `data.json`；
- Sync Tunnel 自身目录、凭据引用、工作区状态、缓存和临时文件始终保持设备本地；
- 设备以一次性配对码加入，每台设备获得独立、可轮换、可撤销的范围凭据；
- 支持分块断点续传、幂等操作、冲突副本、历史浏览/恢复、ACK 水位线、GC、在线备份/校验/恢复、配额和审计；
- 1.0 服务端明文保存内容和路径，E2EE 留到 2.0。

## 最重要的协议规则

项目只实现一套最终 `/api/v1` 协议。`1.0.0-rc.1` 与所有 0.x 客户端、全局 API Token 和旧客户端状态不兼容；升级必须同时更换服务和插件、重新初始化服务数据并重新配对。项目不会为 pre-1.0 协议保留兼容分支。

详见[1.0 升级指引](docs/UPGRADE_TO_1.0.zh-CN.md)和[1.0 协议](docs/PROTOCOL_1.0.zh-CN.md)。

## 文档入口

- [1.0 架构与代码边界](docs/ARCHITECTURE_1.0.zh-CN.md)
- [1.0 最终协议](docs/PROTOCOL_1.0.zh-CN.md)
- [Docker Desktop 部署与运维](docs/DOCKER_DEPLOYMENT.zh-CN.md)
- [用户配合操作清单](docs/USER_ACTIONS.zh-CN.md)
- [自动化测试与复现命令](docs/AUTOMATED_TESTS_1.0.zh-CN.md)
- [1.0 人工验收清单](docs/MANUAL_ACCEPTANCE_1.0.zh-CN.md)
- [1.0 RC 发布清单](docs/RELEASE_CHECKLIST_1.0.zh-CN.md)
- [GitHub、GHCR 与 BRAT 发布](docs/GITHUB_RELEASE.zh-CN.md)
- [威胁模型](docs/THREAT_MODEL.zh-CN.md)

旧 0.x 设计和验收文档只作为历史记录，不能用于配置 1.0。

## 开发者快速验证

```powershell
go test ./...
go vet ./...
.\scripts\smoke-selftest.ps1

Set-Location .\plugin
npm ci
npm run typecheck
npm test
npm run build
```

10,000 文件规模测试需要显式开启：

```powershell
$env:OBSIDIAN_SYNC_SCALE_TEST='1'
go test .\internal\store -run '^TestScaleTenThousandFiles$' -count=1 -v
```

## Docker 快速启动

以下命令只适用于完成[不兼容升级准备](docs/UPGRADE_TO_1.0.zh-CN.md)后的 1.0 测试环境：

```powershell
.\scripts\docker-init.ps1 -ForceConfig
.\scripts\docker-up.ps1
Invoke-RestMethod http://127.0.0.1:8787/healthz
Invoke-RestMethod http://127.0.0.1:8788/healthz
```

默认映射为：

- 公共同步端口 `127.0.0.1:8787`，Cloudflare Tunnel 只连接此端口；
- 本地管理端口 `127.0.0.1:8788`，禁止配置为 Cloudflare Public Hostname；
- 数据 `runtime-data/`；
- 备份 `runtime-backups/`；
- 本地管理员密钥 `secrets/admin-token.txt`，只用于 Windows 管理脚本，不填入 Obsidian。

创建逻辑 Vault 和一次性配对码：

```powershell
.\scripts\admin.ps1 -CreateVault -VaultId personal-notes -DisplayName 'Personal notes'
.\scripts\admin.ps1 -CreatePairing -VaultId personal-notes
```

把第二条命令临时显示的配对码填入插件设置向导；不要把 Admin Token 或配对后的设备凭据复制到普通笔记。

## 安全摘要

- 宿主机公共端口和管理端口都固定绑定回环地址；管理端口不穿透；
- Cloudflare Access 是推荐的第二层保护，但不能取代设备凭据；
- 插件凭据只进入 Obsidian SecretStorage；服务端只保存哈希；
- 日志和诊断不记录 Token、Secret、正文或完整路径；
- SQLite、Chunk、历史和备份含明文 Vault 数据，必须使用 BitLocker/FileVault、严格 ACL 和异机加密备份；
- GC 只有“生成计划 + 校验计划哈希 + 执行”两阶段，不把历史恢复当作备份。
