# Obsidian Sync Tunnel

个人自托管的 Obsidian 全 Vault 同步原型：Go + SQLite 服务运行在 Windows 本机，通过 Cloudflare Tunnel 提供 HTTPS 入口，Obsidian 插件同步笔记、附件和 `.obsidian` 中的插件数据。

> 当前版本是需要在专用测试 Vault 验证的 MVP，不应替代独立备份。

## 从这里开始

1. 阅读[完整开发方案](docs/DEVELOPMENT_PLAN.zh-CN.md)；
2. 按[用户配合操作清单](docs/USER_ACTIONS.zh-CN.md)准备测试 Vault、Cloudflare 和 Windows 服务；
3. 运行 `scripts/build.ps1`；
4. 在两个测试 Vault 完成人工验收矩阵后，再考虑真实数据。

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
- API 使用随机 Bearer Token，可叠加 Cloudflare Access Service Token；
- Obsidian 侧密钥进入 SecretStorage；
- 同步插件自己的 `data.json` 被强制排除；
- SQLite 内容未端到端加密，请使用磁盘加密、ACL 和独立备份。
