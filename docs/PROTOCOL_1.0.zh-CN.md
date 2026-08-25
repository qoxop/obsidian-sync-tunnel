# Sync Tunnel 1.0 最终协议

## 版本规则

唯一协议基址为 `/api/v1`，协议版本为整数 `1`。1.0 发布前的 `/api/v2` 草案、旧 capability 名、`X-Device-ID` 和全局同步 Token 均已删除，不提供兼容或协商降级。客户端若发现 `protocol.version != 1` 或缺少必需 capability，必须停止写入并要求客户端与服务端同时升级。

服务端数据库 schema 当前为 `7`，插件数据 schema 当前为 `9`。这两个数字是内部持久化版本，不等于 HTTP 协议版本。

## 身份与入口

### 管理端

- 监听：默认 `127.0.0.1:8788`；
- 鉴权：默认 `none`，依赖回环监听、Host 与 Origin 校验；可选 `token` 模式使用 `Authorization: Bearer <Admin Token>`；
- Token 模式的 Admin Token 只保存在宿主机 secret 文件，不能进入 Obsidian 或 Cloudflare；
- 管理端不得通过 Tunnel 暴露。

### 公共端

- 监听：默认 `127.0.0.1:8787`；
- 配对：一次性 code，不带设备 Bearer；
- 配对成功后服务端分配 Device ID 并返回独立设备凭据；
- 其余接口使用 `Authorization: Bearer <device credential>`；
- Device ID 从凭据主体解析，客户端不能通过 Header 冒充；
- 可选 Cloudflare Access Service Token 使用 `CF-Access-Client-Id` 与 `CF-Access-Client-Secret`。

默认 scope：`sync:read,sync:write,history:read,restore:write`。设备被 retired/revoked、Vault suspended、凭据轮换后，旧凭据立即失效。

## Capability

`GET /api/v1/server-info` 返回：

```json
{
  "server_version": "1.0.0",
  "protocol": { "version": 1 },
  "capabilities": [
    "snapshot", "idempotent-operations", "whole-file", "chunk-transfer",
    "rename", "batch-delete", "device-ack", "history", "restore",
    "scoped-credentials"
  ],
  "database": { "schema_version": 7 },
  "limits": {
    "max_file_bytes": 67108864,
    "max_page_size": 1000,
    "chunk_size": 4194304,
    "max_chunk_query": 1000,
    "chunk_concurrency": 3
  }
}
```

插件 1.0 必需：`snapshot`、`idempotent-operations`、`chunk-transfer`、`rename`、`batch-delete`、`device-ack`。

## 公共接口

| 方法与路径 | Scope | 用途 |
|---|---|---|
| `POST /api/v1/pair` | 配对码 | 注册设备并返回设备凭据 |
| `GET /api/v1/server-info` | `sync:read` | 协议、能力和限制 |
| `GET /api/v1/vaults/{vault}` | `sync:read` | Vault 元数据 |
| `GET .../status` | `sync:read` | 最新 revision 和文件上限 |
| `GET .../snapshot` | `sync:read` | 固定 revision、按 path 分页快照 |
| `GET .../changes` | `sync:read` | revision 游标增量变更 |
| `GET .../operations/{uuid}` | `sync:read` | 恢复幂等操作结果 |
| `PUT/DELETE .../files/content?path=` | `sync:write` | 小文件写入/删除 |
| `POST .../chunks/missing` | `sync:write` | 查询缺失 Chunk |
| `PUT/GET .../chunks/{sha256}` | write/read | 上传/下载 Chunk |
| `POST .../files/commit?path=` | `sync:write` | 组装 Manifest 并提交 revision |
| `GET .../manifests/{sha256}` | `sync:read` | 下载分块清单 |
| `GET .../blobs/{sha256}` | `sync:read` | 下载完整内容 |
| `POST .../rename` | `sync:write` | 原子创建目标并 tombstone 源 |
| `POST .../batch/delete` | `sync:write` | 最多 100 路径原子删除 |
| `POST .../ack` | `sync:read` | 持久化设备水位线 |
| `GET .../history` | `history:read` | 历史/删除版本分页 |
| `POST .../restore` | `restore:write` | 历史恢复为新 revision |
| `POST .../credential/rotate` | `sync:read` | 返回新凭据并撤销旧凭据 |

写操作使用：

- `X-Operation-ID`：UUID；同设备同 Vault 内幂等；不同请求复用同 UUID 会报错；
- `X-Base-Revision`：路径当前 revision，首次创建为 `0`；
- `X-Modified-At`：Unix 毫秒；
- `X-Content-SHA256`：写文件和 Manifest 提交必需。

## 管理接口

`/admin/v1` 由本机管理 Web 使用，提供会话模式、Vault 创建/更新、配对码、设备状态、审计、运行日志、统计、doctor、受限公网连接诊断、在线备份和两阶段 GC。连接诊断只接受公开 HTTPS 域名并拒绝重定向、私网/Fake-IP 和 DNS 重绑定。灾难恢复因需要停服，使用 `scripts/docker-restore.ps1`。

## 错误与重试

错误 JSON 统一包含：

```json
{ "error": { "code": "revision_conflict", "message": "..." }, "current": {} }
```

- `400` 参数/路径/哈希错误，不重试；
- `401` 凭据无效、过期或设备撤销，不重试并要求重新配对；
- `403` Vault/scope 隔离，不重试；
- `404` 资源不存在；
- `409` revision 冲突或 operation ID 复用，交给冲突流程；
- `413` 文件/请求超限；
- `429` 限速，遵循 `Retry-After`；
- `5xx` 或网络断开按指数退避重试，持久化 outbox/inbox 保证重启后继续。

服务端响应和日志包含脱敏 request ID；不得记录 Bearer、Access Secret、正文或完整路径。
