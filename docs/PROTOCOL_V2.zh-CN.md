# Sync Tunnel Protocol v2 草案

状态：设计草案。0.2 阶段允许调整；进入 0.9 后必须遵守兼容策略。

## 1. 设计目标

- 客户端本地状态完全丢失后仍能从服务端安全重建；
- 支持变更日志压缩，不要求服务端永久保留所有游标；
- 每次写入可幂等重试；
- 为分块传输、版本恢复、设备撤销和未来 E2EE 预留稳定模型；
- 协议失败必须返回机器可读错误码和可操作建议。

## 2. 核心标识

| 字段 | 含义 |
|---|---|
| `vault_id` | Vault 的稳定、不可猜测 ID；显示名称单独保存 |
| `device_id` | 已注册设备的稳定 ID |
| `revision` | 服务端提交成功后分配的单调递增序号 |
| `operation_id` | 客户端为一次逻辑写入生成的 UUID，用于幂等重试 |
| `snapshot_id` | 一次一致 Vault 快照的标识 |
| `path_key` | 1.0 为规范化路径；2.0 可替换为加密路径标识 |
| `content_id` | 完整文件或 Chunk 的内容标识 |

`modified_at` 只用于恢复文件时间和向用户展示，不参与并发胜负判断。

## 3. 能力协商

`GET /api/v2/server-info` 返回：

```json
{
  "server_version": "0.2.0",
  "protocol": { "min": 1, "max": 2 },
  "capabilities": ["snapshot-v1", "operation-id", "whole-file-v1"],
  "limits": {
    "max_upload_bytes": 67108864,
    "max_page_size": 1000
  }
}
```

插件必须在写入前检查协议兼容性。服务器过旧、插件过旧或功能不可用要分别显示，不得笼统报告“网络错误”。

## 4. 快照与增量

### 4.1 当前状态快照

客户端可以分页读取 Vault 当前文件状态：

```text
GET /api/v2/vaults/{vault}/snapshot?cursor=&limit=
```

第一页返回固定的 `snapshot_id` 和 `snapshot_revision`；后续页必须继续读取同一快照。如果服务器无法继续该快照，返回 `snapshot_expired`，客户端从第一页重试。

快照项包含路径、当前 revision、内容标识、大小、时间和删除状态。实现初期可以通过短事务确定 revision，再按 `(path)` 稳定分页；正式 GC 前必须保证分页期间语义一致。

### 4.2 增量变更

```text
GET /api/v2/vaults/{vault}/changes?after={revision}&limit=
```

如果 `after` 早于服务器保留的最小 revision，返回 HTTP 410 和 `cursor_expired`。客户端随后获取新快照并对账。

客户端只有在一页变更全部安全落盘并保存本地状态后，才能推进确认水位线。

### 4.3 排除规则

客户端保存规范化排除规则的 fingerprint。fingerprint 改变时必须执行快照对账。被排除的变更可以推进传输游标，但取消排除后必须由快照补齐，不能依赖已经越过的历史变更。

## 5. 写入与并发

写入携带：

```text
X-Device-ID
X-Operation-ID
X-Base-Revision
X-Modified-At
X-Content-SHA256
```

规则：

1. 同一个 `operation_id` 重试返回原提交结果。
2. `base_revision` 等于当前路径 revision 时允许提交。
3. revision 不同但目标内容相同，返回幂等成功。
4. revision 不同且内容不同，返回 HTTP 409 和当前服务端状态。
5. 服务端提交与操作幂等记录位于同一事务。

客户端必须先把待执行操作写入本地 outbox，再发送网络请求；收到明确提交结果并保存文件状态后才能删除 outbox 项。

## 6. 首次接入和状态重建

首次接入不得直接执行隐式双向合并。客户端应：

1. 获取服务端快照；
2. 扫描本地同步范围；
3. 生成只读计划：上传、下载、相同、潜在冲突、远端删除、本地删除；
4. 让用户选择“以远端初始化”“以本地初始化”或“确认合并”；
5. 保存远端基线后再执行写入。

如果只是客户端索引丢失而设备已注册，默认进入安全重建：远端和本地同路径同哈希视为相同；不同内容视为潜在冲突；缺少可靠基线时不能自动传播删除。

## 7. 删除、设备水位线与 GC

- 删除提交 tombstone；
- 每台设备保存最后确认的 revision 和最后在线时间；
- tombstone 只有超过保留期，且所有未 retired 设备都确认越过该 revision 后才可 GC；
- 长期不用的设备由管理员显式 retire；
- retired 设备重新接入时必须执行完整快照，不能使用旧游标；
- GC 先生成 dry-run 清单，正式执行生成审计事件和恢复窗口。

## 8. 路径规则

- 网络路径统一使用 `/`；
- 拒绝绝对路径、空段、`.`、`..`、NUL 和超长路径；
- 客户端在比较前执行 Unicode NFC；
- 同时检测规范路径冲突和大小写折叠冲突；
- 发现跨平台不兼容路径时暂停该文件并报告，不得擅自改名；
- 不跟随 Vault 外部符号链接；空目录在 1.0 仍不保证同步。

## 9. 分块扩展

0.3 引入能力 `chunk-upload-v1` 和 `chunk-download-v1`：

1. 客户端计算固定大小 Chunk 和文件 Manifest；
2. 查询服务端缺少的 Chunk；
3. 有限并发上传缺块；
4. 服务器校验 Chunk 哈希并原子落盘；
5. 客户端提交 Manifest、`base_revision` 和 `operation_id`；
6. 服务器在事务中创建文件 revision；
7. 未提交 Manifest 的临时 Chunk 按较长宽限期清理。

完整文件 API 在迁移期继续保留，能力协商决定使用哪种传输方式。

### 9.1 原子重命名

0.3 引入能力 `rename-v1`。客户端通过 `POST /api/v2/vaults/{vault}/rename` 提交源路径、目标路径、源路径 `base_revision` 和 `operation_id`。成功响应包含目标 change 和 `related_changes` 中的源路径 tombstone；两者与 operation 结果位于同一事务。

无法可靠证明本地操作是重命名时，客户端不得调用该接口。

## 10. 错误模型

统一返回：

```json
{
  "error": {
    "code": "cursor_expired",
    "message": "The saved cursor is no longer available",
    "retryable": false,
    "action": "fetch_snapshot"
  },
  "request_id": "..."
}
```

错误码至少区分鉴权、权限、协议不兼容、Vault 不存在、设备被撤销、冲突、游标过期、快照过期、配额、磁盘空间、速率限制、内容校验、路径冲突和内部错误。

## 11. 兼容策略

- 0.x 阶段允许协议字段扩展，但不得在补丁版本破坏已有字段；
- 1.0 后服务端至少兼容当前和前一个稳定协议版本；
- 数据库迁移必须可测试，破坏性迁移前必须产生已验证备份；
- E2EE 使用新的 Vault 格式能力，不在原明文 Vault 上静默切换。
