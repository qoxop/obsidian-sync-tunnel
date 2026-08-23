# GitHub 与 BRAT 非官方插件分发

项目不进入 Obsidian 官方社区插件市场。GitHub 保存源码和插件 Release，BRAT 从 GitHub Release 在线安装或更新插件。服务端不发布预构建可执行文件或容器镜像；用户从同一版本 Tag 获取源码，并用仓库中的 `Dockerfile`/Compose 在本机构建。

## 版本与兼容

RC 使用 `1.0.0-rc.N` 并标记 Pre-release；正式版使用 `1.0.0`。Tag、根/插件 manifest、根/插件 versions、npm package 和 lock 必须完全一致。Tag 不加 `v`，不得移动、覆盖或复用。

pre-1.0 客户端/服务协议不兼容。用户更新到 1.0 必须同时更新服务和插件，使用全新的服务数据，并重新配对所有设备。RC 之间的具体升级要求以目标 Release 说明为准。

## 本地准备

```powershell
node .\scripts\set-plugin-version.mjs 1.0.0-rc.3
Set-Location .\plugin
npm ci
npm run typecheck
npm test
npm run build
Set-Location ..
node .\scripts\verify-plugin-release.mjs 1.0.0-rc.3 --artifacts
```

发布工作流只生成插件附件：`main.js`、`manifest.json`、`styles.css`、`SHA256SUMS.txt` 和 `plugin-sbom.cdx.json`。它不会发布服务端二进制、Docker/OCI 镜像、`latest` 标签或任何运行数据。

## 创建 Release

先完成[发布清单](RELEASE_CHECKLIST_1.0.zh-CN.md)和人工验收。管理员提交并推送同步后的版本文件，再创建不可移动的 annotated Tag。Tag 推送触发 `.github/workflows/release-plugin.yml`；带连字符的版本自动标记为 Pre-release。

失败的工作流应修复代码并增加新版本，不能手工拼出与 Tag 源码不一致的 Release，也不能强制移动旧 Tag。

## BRAT 安装

在另一设备：

1. 安装并启用 BRAT；
2. Settings > BRAT > Add Beta plugin；
3. 输入 `https://github.com/qoxop/obsidian-sync-tunnel`；
4. RC 测试选择或固定目标 prerelease，稳定用户跟随正式 Release；
5. 启用 Sync Tunnel，使用管理端为这台设备新生成的一次性配对码；
6. 完成首次预览并手动同步两次。

BRAT 只安装插件程序，不复制配置、凭据或客户端状态。找不到版本时检查仓库是否公开、Release 是否存在、三个插件附件是否独立上传、Tag 是否与 manifest 一致，以及 BRAT 是否允许 prerelease。

## 服务端从源码构建

在服务器电脑检出与插件完全相同的 Tag，然后本地构建：

```powershell
git clone --branch 1.0.0-rc.3 --depth 1 https://github.com/qoxop/obsidian-sync-tunnel.git
Set-Location .\obsidian-sync-tunnel
docker build --pull --build-arg VERSION=1.0.0-rc.3 -t obsidian-sync-tunnel:1.0.0-rc.3 .
docker run --rm obsidian-sync-tunnel:1.0.0-rc.3 version
```

输出版本正确后，再按 [Docker Desktop 部署指引](DOCKER_DEPLOYMENT.zh-CN.md)初始化本地数据目录、密钥和 Compose。不要使用来源不明的第三方镜像；服务端升级和回滚都应重新检出明确 Tag 并使用该 Tag 的 Dockerfile 构建。

## 插件产物验证

```powershell
Get-FileHash .\main.js -Algorithm SHA256
Get-FileHash .\manifest.json -Algorithm SHA256
Get-FileHash .\styles.css -Algorithm SHA256
```

三个哈希必须与 Release 中的 `SHA256SUMS.txt` 一致；`plugin-sbom.cdx.json` 必须是可解析的 CycloneDX JSON。插件 Release 不包含 Token、Vault 数据、SQLite、服务端配置或备份。
