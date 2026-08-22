# Obsidian Sync Tunnel

[English](README.en.md) | 简体中文

一个面向个人使用的 Obsidian 自托管同步方案。服务运行在自己的 Windows 电脑上，通过 Cloudflare Tunnel 让其他电脑和手机安全连接。

> 当前仍是 1.0 候选版本。测试完成前，请为真实 Vault 保留独立备份。

## 主要功能

- 同步笔记、图片、附件、Canvas、主题、CSS 和其他插件；
- 支持 Windows、macOS、Android 和 iOS；
- 多 Vault、多设备、断网续传、冲突处理和文件历史；
- 默认使用安全的 Recommended 同步范围；
- 本机网页管理 Vault、设备、配对码、日志、备份和数据检查；
- 服务数据持久化在 Windows 宿主机，不依赖公共容器镜像。

## 安装服务端

准备一台长期在线的 Windows 10/11 电脑，并安装 Docker Desktop、Git 和 PowerShell。下载项目后，只需要运行一次：

```powershell
Set-ExecutionPolicy -Scope Process Bypass -Force
.\scripts\setup.ps1
```

脚本会构建并启动服务，然后打开管理页面：

<http://127.0.0.1:8788/admin/>

以后更新源码后仍运行同一个 `setup.ps1`。日常使用不需要执行其他运维命令。

## 配置 Cloudflare Tunnel

在 Cloudflare Zero Trust 中创建 Named Tunnel，并完成以下设置：

1. 按 Cloudflare 页面指引，在服务器电脑上安装 Windows connector；
2. 创建 Public Hostname，例如 `sync.example.com`；
3. Origin Service 填写 `http://127.0.0.1:8787`；
4. 打开 `https://sync.example.com/healthz`，确认返回 `status: ok`。

不要把管理端口 `8788` 添加到 Tunnel。推荐在同步域名上配置 Cloudflare Access Service Token。

稳定域名需要有一个已接入 Cloudflare 的域名；Quick Tunnel 只适合临时测试。

如果公网出现 `Error 1033`，或服务器同时运行 Clash Verge TUN，请参见 [故障排查](docs/TROUBLESHOOTING.zh-CN.md#cloudflare-返回-1033)。

## 创建 Vault 和配对设备

打开本机管理页面，进入 **Vault 与设备**：

1. 点击 **新建 Vault**；
2. 填写 Vault ID 和显示名称；
3. 点击该 Vault 的 **配对**；
4. 复制一次性配对码。

Vault ID 是服务器上的同步空间名称，不是本地文件夹路径。需要同步到一起的设备使用同一个 Vault ID，但每台设备都应生成新的配对码。

## 安装 Obsidian 插件

本插件暂不进入 Obsidian 官方市场，通过 BRAT 安装：

1. 在 Obsidian 中安装并启用 BRAT；
2. 打开 `Settings → BRAT → Add Beta plugin`；
3. 输入 `https://github.com/qoxop/obsidian-sync-tunnel`；
4. 选择最新的 prerelease；
5. 在社区插件中启用 **Sync Tunnel**。

然后打开 Sync Tunnel 设置向导，填写 Server URL、Vault ID、Device name 和刚生成的 Pairing code。

首次同步保持 **Recommended** 和 **安全合并（推荐）**。第一次同步完成后再同步一次，上传、下载、删除和冲突数量都应为 0，然后再开启自动同步。

## 日常管理

打开 <http://127.0.0.1:8788/admin/> 即可完成：

- 创建或暂停 Vault；
- 生成配对码、查看或撤销设备；
- 查看容量、运行日志和审计日志；
- 一键检查本地服务、Cloudflare Tunnel、DNS 和公网入口；
- 执行数据完整性检查；
- 创建和校验在线备份；
- 预览并执行安全垃圾回收。

Docker Desktop 默认会自动重启服务。只有灾难恢复需要停服操作，具体步骤见 [Docker 部署与恢复](docs/DOCKER_DEPLOYMENT.zh-CN.md)。

## 重要说明

- 服务器和备份可以读取明文笔记；建议开启 BitLocker，并把备份复制到另一台设备的加密存储；
- Full 模式可能同步其他插件保存的 API Key、Cookie 和本地路径，通常应使用 Recommended；
- Sync Tunnel 自身的设备凭据和运行状态不会同步；
- Cloudflare、历史版本和同机备份都不能代替独立灾难恢复备份；
- 管理页面只允许本机访问，不要修改 Compose 的 `127.0.0.1` 端口绑定。

## 更多文档

- [架构设计](docs/ARCHITECTURE.md)
- [Docker 部署与恢复](docs/DOCKER_DEPLOYMENT.zh-CN.md)
- [HTTP 协议](docs/PROTOCOL_1.0.zh-CN.md)
- [测试与质量门禁](docs/TESTING.md)
- [人工验收清单](docs/MANUAL_ACCEPTANCE_1.0.zh-CN.md)
- [GitHub 与 BRAT 发布](docs/GITHUB_RELEASE.zh-CN.md)
- [威胁模型](docs/THREAT_MODEL.zh-CN.md)
- [故障排查](docs/TROUBLESHOOTING.zh-CN.md)

## License

[MIT](LICENSE)
