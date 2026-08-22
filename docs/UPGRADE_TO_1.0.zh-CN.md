# 从任何 0.x 升级到 Sync Tunnel 1.0

## 结论

这是有意设计的不兼容升级，不是就地 migration：

- 0.x 公共协议、capability 和全局 API Token 不受支持；
- 1.0 使用管理端、逻辑 Vault、一次性配对和每设备范围凭据；
- 0.x 插件 `data.json` 的 cursor、文件索引、outbox/inbox 和 Token 引用不会续跑；
- 插件 1.0 检测到非最终 schema 会保留无害的 URL/Vault/同步配置，但清空身份和同步状态，要求重新配对和首次预览；
- 旧服务数据不得交给 1.0 继续写，也不能把 1.0 SQLite 降级给 0.x。

这样做是为了让 1.0 只有一套清晰架构，不引入长期兼容分支或双鉴权代码。

## 安全迁移策略

把旧环境冻结为回滚材料，在新目录建立 1.0：

1. 所有旧客户端同步两次并确认第二次全 0；
2. 停止自动同步；
3. 对每个本地 Vault 做独立完整备份，包含 `.obsidian`；
4. 保留旧 `.env`、数据目录和 Token 文件，不删除、不覆盖；
5. 停止旧容器；
6. 用新的 1.0 数据/备份/Admin Token 路径运行 `docker-init.ps1 -ForceConfig`；
7. 启动 1.0，创建逻辑 Vault；
8. 每台设备使用独立一次性配对码重新加入；
9. 首台设备用有完整数据的测试副本，查看首次预览后选择安全合并；
10. 其余设备从空 Vault 或已备份副本加入，逐台确认收敛。

不要同时运行 0.x 和 1.0 指向同一 Cloudflare 主机名，也不要让两个版本写同一 SQLite/Chunk 目录。

## 当前测试环境推荐命令

```powershell
.\scripts\docker-down.ps1
Copy-Item -LiteralPath .\.env -Destination .\.env.pre-1.0 -ErrorAction SilentlyContinue

.\scripts\docker-init.ps1 `
  -DataDirectory (Join-Path $PWD 'runtime-data-1.0-rc1') `
  -BackupDirectory (Join-Path $PWD 'runtime-backups-1.0-rc1') `
  -AdminTokenFile (Join-Path $PWD 'secrets\admin-token-1.0-rc1.txt') `
  -Version '1.0.0-rc.1' `
  -ForceConfig
.\scripts\docker-up.ps1
```

`Copy-Item` 只是保留旧 Compose 参数；旧 live SQLite 不能直接复制作为一致备份。旧数据若需要归档，使用旧版本对应备份方式，并把整个目录保持只读。

## 插件重新配对

1. 更新插件程序到 1.0 RC；
2. 打开设置，确认旧 API Token 配置已消失；
3. 管理端为这台设备生成一个新配对码；
4. 向导填写 Server URL、逻辑 Vault ID、设备名、配对码和同步配置；
5. 连接测试必须显示协议 1；
6. 检查首次预览，明确选择安全合并/远端/本地；
7. 手动同步两次，第二次全 0后再打开自动同步。

配对码一次性且短期有效；每台设备必须重新生成。Admin Token 永远不填插件。Cloudflare Access Client Secret 仍通过 SecretStorage 配置。

## 回滚

人工验收失败时：

1. 所有 1.0 客户端关闭自动同步并退出 Obsidian；
2. 停止 1.0 容器；
3. 保存 1.0 测试数据用于问题复现，不与旧目录合并；
4. 恢复旧 `.env`，启动旧镜像和旧数据目录；
5. 恢复客户端升级前的完整 Vault/插件目录备份；
6. 确认 Cloudflare 仍只指向旧公共端口。

回滚是“整个服务数据集 + 整个客户端状态”一起回滚，不支持把 1.0 产生的 revision 写回 0.x 数据库。人工需要保留的 1.0 新笔记应先导出为普通文件，在旧环境恢复完成后再人工复制。
