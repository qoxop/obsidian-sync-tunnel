# Obsidian Sync Tunnel

个人自托管的 Obsidian 全 Vault 同步项目：Go + SQLite 服务运行在 Windows Docker Desktop 中，通过 bind mount 把数据持久化到宿主机，并由 Cloudflare Tunnel 提供 HTTPS 入口。Obsidian 插件同步笔记、附件和 `.obsidian` 中的插件数据。

> 当前版本是需要在专用测试 Vault 验证的 MVP，不应替代独立备份。

项目正在从 0.1 MVP 向可日常使用的 1.0 演进。已确认的产品边界、版本计划和用户配合检查点见[产品路线图](docs/PRODUCT_ROADMAP.zh-CN.md)。

## 从这里开始

1. 阅读[产品路线图](docs/PRODUCT_ROADMAP.zh-CN.md)和[当前 MVP 开发方案](docs/DEVELOPMENT_PLAN.zh-CN.md)；
2. 阅读[Docker 部署说明](docs/DOCKER_DEPLOYMENT.zh-CN.md)；
3. 按[用户配合操作清单](docs/USER_ACTIONS.zh-CN.md)准备测试 Vault、Docker Desktop 和 Cloudflare；
4. 运行 `scripts/docker-init.ps1` 和 `scripts/docker-up.ps1`；
5. 如需在其他电脑在线安装插件，阅读[GitHub + BRAT 发布指引](docs/GITHUB_RELEASE.zh-CN.md)；
6. 使用 Mac 作为第二台测试设备时，可运行[macOS 第二设备脚本化验收](docs/MACOS_SECOND_DEVICE_TEST.zh-CN.md)；
7. 在两个测试 Vault 完成人工验收矩阵后，再考虑真实数据。

## 设计与质量文档

- [产品路线图](docs/PRODUCT_ROADMAP.zh-CN.md)
- [Protocol v2 草案](docs/PROTOCOL_V2.zh-CN.md)
- [威胁模型](docs/THREAT_MODEL.zh-CN.md)
- [测试策略](docs/TEST_STRATEGY.zh-CN.md)
- [从 0.1 升级到 0.2](docs/UPGRADE_0.2.zh-CN.md)
- [升级到 0.3.0 Beta](docs/UPGRADE_0.3-BETA.zh-CN.md)
- [macOS 第二设备脚本化验收](docs/MACOS_SECOND_DEVICE_TEST.zh-CN.md)
- [0.3 Beta 人工验收记录](docs/BETA_ACCEPTANCE_0.3.zh-CN.md)

## 通过 GitHub 安装插件

插件不必提交到 Obsidian 官方市场。仓库推送 `x.y.z` 标签后，GitHub Actions 会自动构建 Release；其他电脑安装 BRAT 后添加本仓库地址即可安装和更新。Release 发布、版本升级和旧测试版迁移步骤见[GitHub + BRAT 发布指引](docs/GITHUB_RELEASE.zh-CN.md)。

## Docker 部署速览

```powershell
.\scripts\docker-init.ps1
.\scripts\docker-up.ps1
Invoke-RestMethod http://127.0.0.1:8787/healthz
```

默认宿主机数据位于 `runtime-data`，Token 位于 `secrets/api-token.txt`；两者都不会提交 Git。关闭容器不会删除这些 bind-mounted 文件。

## 本地开发速览

```powershell
go test ./...
go run .\cmd\obsidian-sync-server token
Set-Content -NoNewline .\dev-token.txt '<生成的 token>'
go run .\cmd\obsidian-sync-server serve --token-file .\dev-token.txt --database .\data\sync.db
```

另一个终端：

```powershell
Set-Location .\plugin
npm install
npm run typecheck
npm run build
```

## 安全摘要

- 服务默认监听 `127.0.0.1:8787`；
- 容器内监听非回环地址需要显式安全开关，宿主机端口固定绑定 `127.0.0.1`；
- API 使用随机 Bearer Token，可叠加 Cloudflare Access Service Token；
- Obsidian 侧密钥进入 SecretStorage；
- 同步插件自己的 `data.json` 被强制排除；
- SQLite 内容未端到端加密，请使用磁盘加密、ACL 和独立备份。
