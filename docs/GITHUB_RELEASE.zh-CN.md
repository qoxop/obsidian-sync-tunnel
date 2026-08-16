# 通过 GitHub 和 BRAT 分发插件

本项目不需要进入 Obsidian 官方社区目录。GitHub 保存源码和 Release，其他电脑使用 BRAT 根据仓库地址安装并检查更新。

## Beta 候选版

正式 Obsidian 插件版本保持 `x.y.z`。项目测试版本使用 `x.y.z-beta.n` GitHub 预发布，并通过 BRAT 1.1 或更高版本安装。候选版版本号只存在于独立 release commit/tag，不写入默认分支的 `manifest.json`，避免普通 Obsidian 更新通道误把 beta 当作稳定更新。

测试者在 BRAT 中添加 `qoxop/obsidian-sync-tunnel` 并选择最新预发布或冻结到指定 beta 标签。正式版发布后，beta 测试者应通过 BRAT 主动切换；Obsidian 本身不保证从同一基础版本的 prerelease 自动升级到 stable。

## 1. 创建并关联 GitHub 仓库

在 GitHub 创建一个空的公开仓库，不要初始化 README、LICENSE 或 `.gitignore`。然后在本仓库根目录运行：

```powershell
git remote add origin https://github.com/<GitHub 用户名>/<仓库名>.git
git push -u origin main
```

仓库名可以继续使用 `obsidian-sync-tunnel`；插件内部 ID 已固定为 `sync-tunnel`，二者不要求相同。

## 2. 发布第一个版本

根目录和 `plugin` 目录的 manifest、versions 以及 npm 包版本必须一致。版本升级使用仓库脚本统一修改：

```powershell
node .\scripts\set-plugin-version.mjs 0.1.0
node .\scripts\verify-plugin-release.mjs
git add manifest.json versions.json plugin .github scripts docs README.md
git commit -m "Prepare plugin release 0.1.0"
git push origin main
git tag 0.1.0
git push origin 0.1.0
```

标签必须是纯 `x.y.z`，并与 `manifest.json` 的 `version` 完全相同，不要添加 `v` 前缀。

推送标签后，GitHub Actions 会执行类型检查和构建，并创建同名 Release。Release 中会有 BRAT 使用的：

```text
main.js
manifest.json
styles.css
```

在 GitHub 仓库的 **Actions** 和 **Releases** 页面确认工作流成功。首次推送前无需手工创建 Release。

## 3. 其他电脑通过 BRAT 安装

1. 在 Obsidian 的社区插件市场安装并启用 **BRAT**；
2. 打开 **Settings > BRAT > Add Beta plugin**；
3. 输入完整仓库地址 `https://github.com/<用户名>/<仓库名>`；
4. 安装完成后启用 **Sync Tunnel**；
5. 配置 Server URL、Vault ID、该电脑唯一的 Device ID 和密钥。

如果 BRAT 报告找不到版本，依次检查仓库是否公开、Release 是否存在、三个文件是否作为独立附件存在，以及标签是否与 manifest 版本一致。

## 4. 发布后续更新

每个新版本都先提交版本变更，再给该提交打同名标签：

```powershell
node .\scripts\set-plugin-version.mjs 0.1.1
node .\scripts\verify-plugin-release.mjs
git add manifest.json versions.json plugin
git commit -m "Release plugin 0.1.1"
git push origin main
git tag 0.1.1
git push origin 0.1.1
```

BRAT 会根据其更新设置检查新的 GitHub Release。不要复用或覆盖已发布的版本号。

## 5. 从旧的本地测试版迁移

早期测试版目录名是 `.obsidian/plugins/obsidian-sync-tunnel`，GitHub 版目录名是 `.obsidian/plugins/sync-tunnel`。迁移时：

1. 禁用旧插件并关闭 Obsidian；
2. 备份旧目录的 `data.json`，或准备重新填写插件设置；
3. 使用 BRAT 安装新版，确认配置和手工同步正常；
4. 删除旧插件目录，避免同时加载两个副本。

新版仍会排除旧目录的 `data.json`，避免迁移期间意外同步本机密钥引用；但旧目录确认无用后仍应移除。
