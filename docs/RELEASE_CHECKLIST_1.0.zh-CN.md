# Sync Tunnel 1.0 RC 与正式发布清单

本清单区分“仓库代码准备”和“需要仓库管理员在 GitHub/设备上完成的发布”。项目只发布非官方 Obsidian 插件；任何 Tag/Release 推送都是外部写操作，必须由仓库管理员明确执行。服务端由用户从同名 Tag 的源码本地构建。

## A. RC 前代码门禁

- [ ] `git status --short` 只包含本次 1.0 变更；
- [ ] 搜索不到运行时代码中的 `/api/v2`、`X-Device-ID`、旧 capability 和全局同步 Token；
- [ ] `go test ./... -count=1`、`go vet ./...`；
- [ ] `scripts/smoke-selftest.ps1`；
- [ ] 插件 `npm ci`、typecheck、test、build；
- [ ] 10,000 文件显式测试；
- [ ] Docker build 和 Compose config；
- [ ] `node scripts/verify-plugin-release.mjs 1.0.0-rc.1 --artifacts`；
- [ ] CI、Ubuntu race、CodeQL 和 Dependabot 无阻塞项；
- [ ] Changelog、README、协议、升级、威胁模型和人工验收文档与代码一致。

## B. GitHub 仓库设置（管理员人工）

- [ ] Settings > Actions > General 允许 Actions 使用 `GITHUB_TOKEN` 创建 Release；
- [ ] main 分支保护：必须通过 CI/CodeQL、禁止 force push、至少一次 review（个人仓库可保留管理员 bypass 但发布时不用）；
- [ ] Settings > Code security 启用 Dependabot alerts、secret scanning、push protection 和 Private vulnerability reporting；
- [ ] 仓库 Topics、License、简介和安全联系入口已完善；
- [ ] 确认 Actions 不保存 Cloudflare、Admin Token、设备凭据或真实 Vault 数据。

## C. 创建 RC（管理员人工）

先检查差异，不复用或覆盖 Tag：

```powershell
node .\scripts\set-plugin-version.mjs 1.0.0-rc.1
node .\scripts\verify-plugin-release.mjs 1.0.0-rc.1 --artifacts
git diff --check
git status --short
# 精确暂存上面审查过的文件，不使用整仓库暂存命令
git commit -m "Prepare Sync Tunnel 1.0.0-rc.1"
git push origin main
git tag -a 1.0.0-rc.1 -m "Sync Tunnel 1.0.0-rc.1"
git push origin 1.0.0-rc.1
```

推 Tag 会自动：

- 构建和测试插件；
- 发布 `main.js`、`manifest.json`、`styles.css`、`SHA256SUMS.txt` 和 CycloneDX SBOM；
- 创建 GitHub Prerelease；
- 不发布服务端二进制或容器镜像。

## D. RC 发布后验证

- [ ] Release 标记为 Pre-release，Tag/manifest/package/versions 完全一致；
- [ ] 下载三个插件附件，与 `SHA256SUMS.txt` 比对；
- [ ] BRAT 使用 `qoxop/obsidian-sync-tunnel` 能安装/更新到 RC；
- [ ] 从 Release 安装的插件而不是本地构建产物执行完整人工验收；
- [ ] 从同名 Tag 源码使用 Dockerfile 本地构建服务端，并执行版本、健康检查和备份恢复演练。

## E. 1.0.0 Stable 门禁

- [ ] [人工验收](MANUAL_ACCEPTANCE_1.0.zh-CN.md)全部通过；
- [ ] 至少 7 天多设备 RC 使用无 P0/P1；
- [ ] Windows、macOS、Android/iOS 的实际覆盖在 Release Notes 中如实列出；
- [ ] 无未处理的高危依赖/CodeQL/Secret scanning 结果；
- [ ] 异机加密备份恢复成功；
- [ ] 所有 RC 修复已经重新走自动化和受影响人工场景；
- [ ] 从干净目录执行一次全新安装文档；
- [ ] 公开已知限制：1.0 服务端明文、不是备份、不支持 0.x 协议、不允许其他实时同步器共用 Vault。

正式版重新运行版本脚本为 `1.0.0`，提交后打同名 annotated Tag。工作流只创建插件 Stable Release；服务端继续从同名 Tag 源码本地构建。绝不移动 `1.0.0-rc.1` Tag，也不在失败工作流上手工拼接不一致附件。

## F. 发布后

- [ ] 新设备严格通过一次性配对加入；
- [ ] 监控 GitHub Issues/Private Vulnerability Reporting，但不收集客户端遥测；
- [ ] 定期处理 Dependabot、CodeQL，并在本地执行规模测试和备份恢复演练；
- [ ] 安全修复使用新 SemVer，不覆盖 Release；
- [ ] 1.0.x 只做兼容修复；任何未来协议变更先写设计和迁移策略。这里的“不兼容自由”只适用于 1.0 发布前，正式 1.0 的兼容承诺在发布说明中确定。
