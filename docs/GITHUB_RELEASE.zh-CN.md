# GitHub、GHCR 与 BRAT 分发

项目无需进入 Obsidian 官方市场。GitHub 保存源码和 Release，GHCR 保存多架构服务镜像，BRAT 从 GitHub Release 在线安装插件。

## 版本与兼容

RC 使用 `1.0.0-rc.N` 并标记 Pre-release；正式版使用 `1.0.0`。Tag、根/插件 manifest、根/插件 versions、npm package 和 lock 必须完全一致。Tag 不加 `v`，不得移动、覆盖或复用。

pre-1.0 客户端/服务协议不兼容。用户更新到 1.0 必须同时更新服务和插件，并按[升级指引](UPGRADE_TO_1.0.zh-CN.md)重新配对。

## 本地准备

```powershell
node .\scripts\set-plugin-version.mjs 1.0.0-rc.1
Set-Location .\plugin
npm ci
npm run typecheck
npm test
npm run build
Set-Location ..
node .\scripts\verify-plugin-release.mjs 1.0.0-rc.1 --artifacts
```

发布工作流附件：`main.js`、`manifest.json`、`styles.css`、`SHA256SUMS.txt`、`plugin-sbom.cdx.json`。同时构建 `linux/amd64`/`linux/arm64` GHCR image，附 provenance/SBOM 并对 digest 做 Cosign keyless 签名。

## 创建 Release

先完成[发布清单](RELEASE_CHECKLIST_1.0.zh-CN.md)和人工验收。管理员执行 commit/push/tag；Tag 推送触发 `.github/workflows/release-plugin.yml`。RC 不更新 GHCR `latest`，stable 才更新。

失败的工作流应修复代码并增加新版本，不能手工拼出与 Tag 源码不一致的 Release，也不要强制移动旧 Tag。

## BRAT 安装

在另一设备：

1. 安装并启用 BRAT；
2. Settings > BRAT > Add Beta plugin；
3. 输入 `https://github.com/qoxop/obsidian-sync-tunnel`；
4. RC 测试选择/固定目标 prerelease，Stable 用户跟随正式 Release；
5. 启用 Sync Tunnel，使用管理端为这台设备新生成的一次性配对码；
6. 完成首次预览并手动同步两次。

BRAT 只安装插件程序，不复制配置、凭据或客户端状态。找不到版本时检查仓库是否公开、Release 是否存在、三个附件是否独立上传、Tag 是否与 manifest 一致，以及 BRAT 是否允许 prerelease。

## GHCR 部署

RC：

```powershell
docker pull ghcr.io/qoxop/obsidian-sync-tunnel:1.0.0-rc.1
```

正式版可固定 `1.0.0`；生产环境不建议只写 `latest`，因为固定版本更容易审计和回滚。Compose 的默认本地 build 保留，若改用 GHCR image，应删除/禁用 build 并明确 image Tag，同时保持宿主机 bind mount 和两个回环端口不变。

## 产物验证

```powershell
Get-FileHash .\main.js -Algorithm SHA256
Get-FileHash .\manifest.json -Algorithm SHA256
Get-FileHash .\styles.css -Algorithm SHA256
docker buildx imagetools inspect ghcr.io/qoxop/obsidian-sync-tunnel:1.0.0-rc.1
```

checksum 应与 Release `SHA256SUMS.txt` 一致。GitHub Attestations 页面验证构建来源；Cosign 验证时限定 GitHub Actions issuer 和仓库 workflow identity，不能只看到“有签名”就信任任意身份。
