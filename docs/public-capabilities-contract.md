# 公共能力与双入口契约

`starcat` 普通命令与 `starcat mcp` 复用连接模块，App 负责实际业务及最终授权。CLI 不访问 App 数据库，不执行嵌套 CLI，不缓存一份固定工具映射。

```bash
starcat tools list --json
starcat tools describe starcat.get_repo --json
starcat tools call starcat.get_repo --input request.json
starcat resources list --json
starcat resources read starcat://capabilities
starcat stats --json
starcat stats ai --json
starcat stats knowledge --json
```

`tools call` 无 `--input` 时使用空对象；`--input -` 从 stdin 读取一个 JSON 对象。文件和 stdin 的上限为 8 MiB；拒绝 `null`、多段 JSON 和结构无效的输入。只实现文档列出的 Schema 结构预检，最终业务约束由 App 检查。工具和资源列表消费 `nextCursor`，重复 cursor 明确报错。

含 `dry_run` 的工具默认强制预览，原始 JSON 的 `dry_run=false` 不能绕过 CLI 的 `--apply`。显式 `--apply` 才传 `dry_run=false`。没有预览的副作用或未知副作用工具也要求 `--apply`，不能伪造成功预览。只读工具由实际 `readOnlyHint` 判定，不能靠工具名猜测。

App server `0.6.0` / `contract_version=2` 将写入省略 `dry_run` 的默认值统一为 `true`。旧 MCP 工作流依赖省略即执行时，需在预览与明确授权后传 `false`。CLI 主动发送该参数，可安全连接仍使用旧默认行为的 App；不把业务契约版本和 MCP 协议协商版本混为一谈。

公共任务由 App 持有，断开客户端后按 `task_id` 查询，不能超时后自动重放。完成结果保留一小时；App 重启、切号、过期后句柄失效。`supports_cancellation=false` 时取消明确拒绝，`cancelling` 不代表已退出。文件属于运行 CLI 的设备，App 不解释客户端路径；导出内容可由调用方重定向到本地文件。

通用命令成功写结构化 JSON 到 stdout，错误写 stderr 并使用既有 runner 非零退出码。新增业务错误使用 `STARCAT_ERROR CODE: message`；现有 `search` / launcher schema v1 和错误行为保持独立。`mcp` 的 stdout 保持纯协议流，更新提示和诊断不混入。

新增 App 能力时先更新 App 目录及执行器，通用 CLI 自动发现；专用命令按频率选择。同步 Skill、覆盖文档与测试，记录“已有覆盖 / 本次开放 / 暂缓 / 不适用”。App 的账号、Pro、渠道、私有笔记、Runtime allowlist、设备与写入权限不因 CLI 新入口而放宽。
