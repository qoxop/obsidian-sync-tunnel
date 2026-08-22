# 测试与质量门禁

本文只记录可长期重复执行的测试入口。具体某次 RC 的人工结果不保存在主分支文档中，应记录在 GitHub Issue 或 Release 说明里。

## 1. 本地自动测试

在仓库根目录执行：

```powershell
go test ./...
go vet ./...
.\scripts\smoke-selftest.ps1

Set-Location .\plugin
npm ci
npm run typecheck
npm test
npm run build
Set-Location ..

node .\scripts\verify-plugin-release.mjs
docker compose config --quiet
```

`smoke-selftest.ps1` 会编译并启动一个使用临时目录和随机 Admin Token 的服务，执行公共 API 与管理 API 核心流程，结束后清理临时进程和文件。它不会使用正式运行数据。

## 2. 覆盖范围

### Go Store

- SQLite schema 初始化与迁移；
- 路径规范化、revision、tombstone 与乐观冲突；
- operation UUID 幂等与错误复用；
- 固定 revision 快照和游标分页；
- Chunk 缺块查询、校验、Manifest 与跨 Vault 隔离；
- 原子重命名和批量删除；
- Vault、设备、配对码、scope、轮换与撤销；
- 历史查询、删除版本和恢复；
- 配额、磁盘余量、设备 ACK、GC 计划与执行；
- SQLite doctor、备份、校验和恢复。

### HTTP API

- Public/Admin 双入口；
- Bearer 鉴权、Vault 约束、scope 与撤销；
- 请求体限制、限速和结构化错误；
- 文件、Chunk、重命名、批量删除、ACK、历史与恢复；
- Vault 与设备管理、审计、统计、doctor、GC 和备份。

### Obsidian 插件

- 当前数据 schema 和不兼容状态重置；
- 路径、glob、跨平台碰撞和同步范围；
- 首次同步预览与三种初始化策略；
- API 请求、Cloudflare Access 头、重试和错误解析；
- 文件事件工作集、快照对账、outbox/inbox 恢复；
- whole-file/Chunk 传输、冲突副本、重命名和批量删除；
- 桌面流式哈希/下载边界与移动端大小限制。

## 3. 可选规模测试

10,000 文件测试默认跳过，避免拖慢日常开发：

```powershell
$env:OBSIDIAN_SYNC_SCALE_TEST='1'
go test .\internal\store -run '^TestScaleTenThousandFiles$' -count=1 -v
Remove-Item Env:\OBSIDIAN_SYNC_SCALE_TEST
```

## 4. Docker 验证

静态验证：

```powershell
docker compose config --quiet
docker build --build-arg VERSION=local-test --tag obsidian-sync-tunnel:local-test .
```

正式本机环境的只读健康检查：

```powershell
docker compose ps
Invoke-RestMethod http://127.0.0.1:8787/healthz
Invoke-RestMethod http://127.0.0.1:8788/healthz
.\scripts\admin.ps1 -Doctor
.\scripts\admin.ps1 -Stats
```

不要让自动测试指向正式 Vault。需要接口级写入测试时，使用 `smoke-selftest.ps1` 的临时服务，或显式创建只用于测试的逻辑 Vault。

## 5. 人工测试

自动测试无法替代以下场景：

- Windows 与 macOS 双向同步和第二次同步全零；
- Unicode、空格、大小写碰撞、Canvas、图片与二进制附件；
- 上传和下载过程中断网，恢复后哈希一致；
- Obsidian 退出、服务容器重启以及设备重启后的队列恢复；
- 跨设备同时修改、重命名和批量删除；
- `.obsidian` 配置与插件文件同步后的重启提示；
- Android 或 iOS 前后台切换、网络切换和大文件内存限制；
- Cloudflare Tunnel、Access Service Token 和设备凭据组合；
- 在线备份校验、全新目录恢复和旧数据回滚路径。

完整步骤见[1.0 人工验收清单](MANUAL_ACCEPTANCE_1.0.zh-CN.md)。人工报告不得包含 Token、Access Secret、完整 Server URL、笔记正文或真实路径。

## 6. CI 门禁

`.github/workflows/ci.yml` 在 push 和 pull request 上执行：

- Windows：Go test/vet、插件安装/typecheck/test/build、发布元数据验证；
- Linux：`go test -race ./...`；
- Linux：从当前 Dockerfile 构建服务镜像，但不推送；
- CodeQL：Go 与 JavaScript/TypeScript 静态分析。

Tag 发布工作流会再次执行插件 typecheck、测试和构建，然后验证版本元数据，生成 SHA-256 与 CycloneDX SBOM，只发布非官方 Obsidian 插件资产。

## 7. 提交前检查

```powershell
git diff --check
git status --short
```

提交前确认没有暂存 `.env`、`secrets/`、`runtime-data/`、`runtime-backups/`、SQLite、日志、Vault 内容、插件 `data.json` 或任何真实凭据。
