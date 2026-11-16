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

通用工具保留输入 JSON 的 `dry_run`，省略时由 App 决定默认值。`--apply` 是 `dry_run=false` 的便利写法，与明确的 `true` 冲突时拒绝调用。没有预览的工具直接遵循 App 的调用契约。专用写命令仍默认预览。只读工具由实际 `readOnlyHint` 判定，不能靠工具名猜测。

App server `0.7.0` / `contract_version=3` 统一 `read` / `write`。读默认开放，包括笔记；写默认关闭，在 App 设置 → 扩展与连接 → MCP 服务中开启“允许写入”。刷新、生成、删除、批量、本地与 GitHub 修改均使用这一个写权限。App 保持含 `dry_run` 操作默认预览；旧细分权限字段已移除，请升级 App 与 CLI 后使用 v3。业务契约版本与 MCP 协议协商版本分开。

公共任务由 App 持有，断开客户端后按 `task_id` 查询，不能超时后自动重放。完成结果保留一小时；App 重启、切号、过期后句柄失效。`supports_cancellation=false` 时取消明确拒绝，`cancelling` 不代表已退出。文件属于运行 CLI 的设备，App 不解释客户端路径；导出内容可由调用方重定向到本地文件。

通用命令成功写结构化 JSON 到 stdout，错误写 stderr 并使用既有 runner 非零退出码。新增业务错误使用 `STARCAT_ERROR CODE: message`；现有 `search` / launcher schema v1 和错误行为保持独立。`mcp` 的 stdout 保持纯协议流，更新提示和诊断不混入。

新增 App 能力时更新 App 目录及共享业务执行器，通用 CLI 自动发现；专用命令按频率选择。同步 Skill、覆盖文档与测试，记录“已有覆盖 / 本次开放 / 暂缓 / 不适用”。已有 App 操作默认开放，不能把下载、事务或限流作为暂缓理由。账号、Pro、渠道、系统授权及文件冲突继续由 App 原服务检查，不增加公共细分权限。

健康/安全/洞察刷新、Release 订阅、标签与合集管理、RAG 会话/问答，以及模型、Skill、缓存和诊断均可从 `tools list` 发现。耗时动作返回任务，调用 `starcat.get_task_status` 和 `starcat.get_task_result`；模型安装会等待下载及加载检查，不能把排队当成功。诊断导出返回 artifact ID，通过 `starcat.read_export_chunk` 获取 base64 分块，文件属于 App 设备且只保留一小时。
