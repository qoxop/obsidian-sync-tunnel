# 用户必须配合的操作清单

代码、测试、构建和脚本可以自动完成；以下事项涉及你的账户、设备、管理员权限、真实数据判断或 SecretStorage，必须由你确认或操作。

## 现在：1.0 RC 人工验收

- [ ] 确认旧 0.x 环境已停止自动同步，旧数据和 `.env` 保留；
- [ ] 确认真实 Vault 有异机加密备份；
- [ ] 按[升级指引](UPGRADE_TO_1.0.zh-CN.md)为 RC 选择新的数据、备份和 Admin Token 路径；
- [ ] 启动 Docker RC 环境并把 health/doctor/stats 的脱敏结果发回；
- [ ] 创建逻辑测试 Vault，并为每台设备分别生成一次性配对码；
- [ ] 在 Obsidian 向导中输入配对码和 Cloudflare Access Secret；这些值不要发回；
- [ ] 按[人工验收](MANUAL_ACCEPTANCE_1.0.zh-CN.md)逐项操作和记录。

## Cloudflare 账户操作

- [ ] 账号和由 Cloudflare 管理 DNS 的域名；
- [ ] Tunnel Public Hostname 只指向 `http://127.0.0.1:8787`；
- [ ] 确认没有任何路由指向 `8788`；
- [ ] 推荐创建 Access Self-hosted Application、Service Auth policy 和 Service Token；
- [ ] Tunnel Token、Access Client Secret 进入密码管理器，不进入 Git、普通笔记、命令行历史或聊天；
- [ ] Windows 管理员权限安装/升级 `cloudflared` 服务并验证重启自启。

需要域名才能使用稳定的自定义 HTTPS Public Hostname；Cloudflare Registrar 不是必需，域名可在其他注册商购买后把 DNS 托管给 Cloudflare。纯局域网/本机测试不需要域名，可用 `http://127.0.0.1:8787`。

## Obsidian 与设备操作

- [ ] Windows 同机两个独立 Vault 目录先完成收敛测试；
- [ ] Mac 新建空测试 Vault并用独立配对码；
- [ ] 至少一台 Android 或 iOS 真机测试后台/前台、网络切换和内存限制；
- [ ] 每台设备使用可识别的 Device name，Device ID 由服务端分配，不能手填或复制；
- [ ] 每台设备的 Credential 自动进入 SecretStorage；Admin Token 永远不填插件；
- [ ] 默认 Recommended；Full 前审计其他插件 `data.json` 是否含敏感值；
- [ ] 首次同步和高风险操作阅读预览，不盲目确认；
- [ ] 自动同步只在两次手动同步第二次全 0后开启；
- [ ] 同一 Vault 不同时启用 Obsidian Sync、Syncthing 或网盘双向同步。

## GitHub 仓库管理员操作

- [ ] 审查本次 diff，决定 commit/push；
- [ ] 启用分支保护、CodeQL、Dependabot、Secret scanning/push protection；
- [ ] 启用 Private Vulnerability Reporting；
- [ ] 确认 Actions/GHCR 权限；
- [ ] 人工验收通过后创建 annotated RC/Stable Tag；
- [ ] 在 Release、GHCR、SBOM、checksum、attestation 和 Cosign 签名页面核验产物；
- [ ] 用 BRAT 从 GitHub Release 安装，而非把本地代码复制到其他设备。

## 备份与安全判断

- [ ] 选择不在同步服务器电脑上的加密备份目标；
- [ ] Windows 数据盘启用 BitLocker，Mac 使用 FileVault，移动设备启用系统加密/锁屏；
- [ ] 执行至少一次在线备份 → manifest 校验 → 恢复 → 客户端收敛；
- [ ] 确认服务端明文存储风险可接受；若不能接受，应停留在测试环境等待 2.0 E2EE；
- [ ] 为开源项目确认 License、Logo、公开安全联系入口和中英文文档范围。

## 发送给维护者的安全结果

可以发送：PASS/FAIL 标志、计数、HTTP 状态、插件/服务版本、脱敏 request ID、doctor/stats 汇总、无正文的 SHA-256。

不要发送：`data.json`、`.env`、Admin Token、配对码、设备 Token、Cloudflare Tunnel Token/Access Secret、真实域名、Vault ID、Device ID、文件路径或笔记正文。
