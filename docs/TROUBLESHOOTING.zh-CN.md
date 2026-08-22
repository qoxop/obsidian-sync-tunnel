# 故障排查

## Cloudflare 返回 1033

### 使用管理后台检查

优先打开本机管理页面 <http://127.0.0.1:8788/admin/>，进入 **连接诊断**，填写 Obsidian 插件使用的公网同步地址并点击 **开始检查**。页面会检查：

- 本地同步 API；
- 公网域名和 Cloudflare Tunnel 边缘 DNS；
- 可读取时的 `cloudflared /ready` 状态；
- 公网 `/healthz` 及 `1033`、Origin 错误和 Cloudflare Access 拦截。

如果 Cloudflare Access 保护了同步域名，可以临时填写 Service Token。Client ID 和 Secret 只用于本次请求，不会保存；公网同步地址仅保存在当前浏览器本机。

下面的 PowerShell 命令保留用于管理页面无法打开或需要交叉确认的场景。

### 现象

打开同步地址或 `/healthz` 时，Cloudflare 显示：

```text
Error 1033
Cloudflare Tunnel error
```

这表示 Cloudflare 边缘没有找到可用的 Tunnel 连接器。它不等于 Go 服务或 Docker 容器已经停止。

### 逐层确认

先在服务器电脑的 PowerShell 中确认本地同步服务：

```powershell
curl.exe http://127.0.0.1:8787/healthz
```

正常结果包含：

```json
{"status":"ok"}
```

再确认 Windows 连接器：

```powershell
Get-Service cloudflared
```

`Running` 只表示进程存在，不保证 Tunnel 已经注册成功。可以继续检查连接器就绪状态：

```powershell
curl.exe -i http://127.0.0.1:20241/ready
```

- `200`：连接器已就绪；
- `503`：连接器进程存在，但没有建立可用 Tunnel。

如果本地 `/healthz` 正常、`cloudflared` 为 `Running`、`/ready` 为 `503`，再检查 Cloudflare Tunnel 边缘域名的 DNS：

```powershell
Resolve-DnsName region1.v2.argotunnel.com
```

出现以下地址通常表示 Clash Verge 的 TUN/Fake-IP 接管了连接：

```text
198.18.x.x
198.19.x.x
fdfe:dcba:9876::...
```

### Clash Verge TUN 的持久解决方案

不需要长期关闭 TUN。打开 Clash Verge Rev：

```text
订阅 -> 全局扩展脚本 -> Script
```

填入以下脚本：

```javascript
function main(config) {
  const directRules = [
    "PROCESS-NAME,cloudflared.exe,DIRECT",
    "DOMAIN-SUFFIX,argotunnel.com,DIRECT",
  ];

  const oldRules = Array.isArray(config.rules) ? config.rules : [];
  config.rules = directRules.concat(
    oldRules.filter((rule) => !directRules.includes(rule)),
  );

  if (!config.dns) {
    config.dns = {};
  }

  const realIpPattern = "+.argotunnel.com";
  const oldFilter = Array.isArray(config.dns["fake-ip-filter"])
    ? config.dns["fake-ip-filter"]
    : [];

  if (!oldFilter.includes(realIpPattern)) {
    config.dns["fake-ip-filter"] = [realIpPattern].concat(oldFilter);
  }

  return config;
}
```

该脚本同时完成两件事：

1. `argotunnel.com` 不再返回 Fake-IP；
2. `cloudflared.exe` 和 Tunnel 边缘域名强制直连。

全局扩展脚本独立于订阅内容，切换或更新订阅后仍会应用。脚本机制和 DNS 规则分别参见 [Clash Verge Rev 自定义规则脚本](https://github.com/clash-verge-rev/clash-verge-rev.github.io/blob/main/docs/guide/script.md) 与 [Mihomo DNS 配置](https://wiki.metacubex.one/en/config/dns/)。

保存脚本后，点击订阅页面右上角的 **重新激活订阅**，保持 TUN 开启。然后使用管理员 PowerShell 重启连接器：

```powershell
Restart-Service cloudflared
```

### 验证修复

依次执行：

```powershell
Resolve-DnsName region1.v2.argotunnel.com
curl.exe -i http://127.0.0.1:20241/ready
curl.exe https://sync.example.com/healthz
```

通过标准：

- DNS 不再返回 `198.18.x.x`、`198.19.x.x` 或 `fdfe:dcba:9876::/64`；
- `/ready` 返回 `200`；
- 公网 `/healthz` 返回 `status: ok`。

请将最后一条命令中的 `sync.example.com` 替换为自己的同步域名。

### 仍然返回 1033

如果 DNS 已恢复真实地址但仍为 1033，按顺序检查：

1. 在 Cloudflare Zero Trust 中确认 Tunnel 的 Connector 为 Healthy；
2. 确认 Public Hostname 绑定的是当前 Tunnel，而不是已经删除或重建的旧 Tunnel；
3. 确认 Public Hostname 的 Origin Service 为 `http://127.0.0.1:8787`；
4. 确认防火墙允许 `cloudflared` 出站连接；
5. 如果重新生成过 Tunnel Token，重新安装 Windows Connector。

不要在命令输出、截图、Issue 或日志中公开 Tunnel Token。

## 本机管理页面无法打开

本机管理页面为 <http://127.0.0.1:8788/admin/>。如果无法打开，参见 [Docker 部署与恢复](DOCKER_DEPLOYMENT.zh-CN.md#无法打开管理页面)。管理端口 `8788` 不应加入 Cloudflare Tunnel。
