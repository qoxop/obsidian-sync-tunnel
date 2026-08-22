# Sync Tunnel 1.0 自动化测试

## 本地一键门禁

在仓库根目录执行：

```powershell
go test ./... -count=1
go vet ./...
.\scripts\smoke-selftest.ps1

Set-Location .\plugin
npm ci
npm run typecheck
npm test
npm run build
Set-Location ..

docker compose --env-file .env.example config --quiet
docker build --build-arg VERSION=1.0.0-rc.1 -t obsidian-sync-tunnel:selftest .
node .\scripts\verify-plugin-release.mjs --artifacts
```

`smoke-selftest.ps1` 会在系统临时目录构建服务、随机生成只存在于进程内的 Admin Token、启动隔离端口、执行完整公共/管理 API 流程，然后停止进程并删除临时数据。它不读取当前 `.env`、现有 Docker 数据或生产 Token。

## 覆盖清单

### Go Store

- schema 1 到 schema 7 migration 和未来版本拒绝；
- Vault、一次性配对、scope、设备凭据轮换/撤销/retired；
- 新建、修改、删除、revision 冲突、相同内容幂等；
- operation UUID 结果恢复和错误复用；
- 稳定快照、变更分页、Vault 内容隔离；
- Chunk 缺失查询、校验、Manifest、组装和跨 Vault 隔离；
- 原子 rename、batch delete；
- 历史浏览、普通恢复、删除恢复；
- ACK 单调水位线、GC 阻塞、计划哈希和执行；
- 单文件、Vault 字节/文件数、磁盘余量限制；
- SQLite doctor、Chunk 丢失/损坏；
- 在线备份、manifest 校验、篡改拒绝和恢复。

### Go HTTP

- 公共健康检查和最终 server-info；
- 配对、无鉴权、错误 Vault、scope 不足和撤销；
- 完整文件/Chunk/快照/变更/历史/恢复/ACK 全生命周期；
- 限速、`Retry-After`、请求大小和结构化错误；
- 独立 Admin Token、Vault 管理和统计；
- 不接受全局同步 Token 或客户端声明的 Device ID。

### 插件

- 新安装和 pre-1.0 状态强制重置；
- 最终 schema 状态恢复；
- `/api/v1` URL、设备 Bearer、Cloudflare Access 头和结构化错误；
- 旧 protocol/capability 明确拒绝；
- Notes/Recommended/Full/Custom 路径过滤及自身目录保护；
- 首次同步三种模式、快照对账、路径冲突；
- 事件队列、扫描缓存、删除传播安全边界；
- outbox/inbox 在请求中断和重启后的恢复；
- Chunk 缺块上传、Manifest 下载、SHA-256 校验；
- 桌面大文件流式下载不调用 `DataAdapter.readBinary`；
- rename、batch delete、ACK 和第二次同步收敛。

## 状态机与规模测试

固定种子状态机使用 5 个设备、24 个路径运行 2500 步随机写入、删除和陈旧 revision 冲突；每 100 步把 SQLite 快照/Blob 与独立内存模型逐项比对。固定种子使失败可复现。

10,000 文件测试默认跳过，显式执行：

```powershell
$env:OBSIDIAN_SYNC_SCALE_TEST='1'
go test .\internal\store -run '^TestScaleTenThousandFiles$' -count=1 -v
```

它验证 10,000 次提交、317 项非整页分页和末尾 250 revision 增量。更大 50k/100k 和真实移动端性能属于人工基准，不放入每次 PR。

## CI

- Windows CI：Go tests/vet、插件测试/类型/构建、Release metadata；
- Ubuntu race job：`go test -race ./... -count=1`。本地 Windows 若为 `CGO_ENABLED=0`，race 命令不可用；
- Docker job：macOS 脚本 `bash -n` 和镜像构建；
- Nightly：插件构建、GHCR nightly 镜像和 10,000 文件测试；
- CodeQL：Go 与 JavaScript/TypeScript；
- Dependabot：Go、npm、GitHub Actions 和 Docker 依赖更新。

## 2026-08-19 本机基线

- Go 全量测试：PASS；
- `go vet ./...`：PASS；
- 插件：8 个测试文件、33 项测试 PASS；
- 插件 typecheck/production bundle：PASS；
- 2500 步状态机：PASS；
- 10,000 文件规模测试：PASS，约 42 秒；
- 隔离 API 冒烟：`FINAL_PROTOCOL_SMOKE_PASS`；
- Docker image/Compose：PASS；
- PowerShell 解析、GitHub Actions YAML lint：PASS；
- race：本机因无 CGO 未执行，由 Ubuntu CI 门禁执行。

自动测试不能替代 Obsidian UI、SecretStorage、Cloudflare、真实移动端后台限制和 Docker Desktop 重启验证，必须继续执行人工验收。
