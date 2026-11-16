# AGENTS.md — Starcat CLI

本文档是本仓库 AI 协作规则的唯一维护源。

## 独立仓库边界

- 本目录是 `starcat-app/starcat-cli` 独立 Git 仓库，拥有自己的版本、CI、Release
  和 Homebrew 联动边界，不是 Starcat macOS App 的源码子目录。
- 修改前必须确认当前分支与工作区状态；未经 dong4j 明确要求，不得切换分支、提交或处理其他仓库。
- App 端 MCP tools 才是权限、Pro entitlement、dry-run 和审计的最终边界；
  CLI 不得直接读取 Starcat SQLite，也不得复制 App 业务逻辑。

## 用途与技术栈

`starcat` 是跨平台命令行客户端，也是面向 Codex、Claude Code 等 Agent 的
stdio MCP bridge。它把逐行 JSON-RPC 从 stdio 转发到 Starcat MCP
Streamable HTTP，协议输出只能写 stdout，诊断只能写 stderr。

- Go module：`github.com/starcat-app/starcat-cli`
- Go directive 1.25.0；发布 toolchain 固定为 Go 1.26.11，以包含既定标准库安全修复
- 支持 macOS arm64/amd64、Linux arm64/amd64、Windows amd64
- 系统凭据存储：macOS Keychain、Windows Credential Manager、Linux Secret Service

## 关键目录

- `cmd/starcat/`：唯一命令入口。
- `internal/cli/`：命令解析与终端输出。
- `internal/mcp/`：HTTP 客户端和 stdio MCP bridge。
- `internal/pairing/`、`internal/config/`、`internal/credential/`：配对、profile 与凭据。
- `internal/updater/`：更新检查和脚本安装版本的更新逻辑。
- `contracts/global-search/`：launcher 共用的 schema v1、固定样例和稳定错误码契约。
- `scripts/`：安装器、跨平台构建和 Homebrew Formula 渲染。

## 开发与验证

```bash
go mod verify
go test ./...
go test -race ./...
go vet ./...
go run golang.org/x/vuln/cmd/govulncheck@v1.6.0 ./...
bash -n scripts/*.sh
make build
./bin/starcat version
git diff --check
```

修改平台相关代码时，还应验证 `CGO_ENABLED=0` 的五个目标组合；不要用一次本机构建
代替跨平台检查。

## 版本随改动持续升级（强制）

- 以后凡修改 CLI 命令、参数、输出、MCP 桥接、浏览器宿主、连接行为或交付工具链，
  AI 必须在同一任务主动检查并升级根目录 `VERSION`，不再等待 dong4j 提醒。
  仅文档、注释或不影响行为的测试调整可不升级，但交付报告须说明。
- 以一个完成并验证的功能或修复批次为单位升级；同批返工、每次编辑或编译不重复升级。
  兼容修复升 patch，兼容新增能力升 minor，不兼容的公开接口变化升 major；
  未发布开发迭代使用 `-dev.N`，同一目标版本的后续交付递增 N，不复用已交付编号。
- `VERSION` 记录当前开发/交付版本；本地验证统一使用 `make build`，通过既有
  `-ldflags` 注入 `internal/mcp.Version`。源码的 `dev` 仅保留为直接 `go build`
  的兜底，禁止在 Go 文件、Makefile 或文档命令里再维护一份硬编码版本。
- 升级后重新构建，核对 `./bin/starcat version` 与 `VERSION` 一致，并在任务
  Checklist/交付报告记录前后版本、变更范围和验证结果。CLI 版本、App MCP server
  版本与公共契约版本独立管理，不能相互替代。
- 更新版本不等于发布授权。正式或预发布前将 `VERSION` 与目标 tag 对齐，产物仍由
  tag 注入版本；Changelog 按独立授权同步，未获授权时在 Checklist 留待办。
  不自动提交、打 tag、push、更新 Homebrew 或执行发布脚本。

## 项目特有约束

- 新增或修改业务动作时主动评估公共暴露；复用 App `tools/list` 与
  `starcat://capabilities`，不在 CLI 维护另一份领域目录。遵循
  `docs/public-capabilities-contract.md`；同一工作区可查看主仓
  `../../docs/5-规范/CLI与MCP能力暴露规范.md`。
- 新能力至少可经 `tools call` 与 `mcp` 双入口使用；专用命令仅为高频便利入口。
  变更时同步真实 help、输入 Schema、预览、错误与 Skill 校验；纯 UI 或暂缓须说明原因。
- CLI 是 MCP 薄客户端：搜索、排序、去重、鉴权、授权与写入规则留在 Starcat App，
  禁止新增数据库直读或第二套业务实现。
- `starcat mcp` 的 stdout 必须保持纯协议流；更新提示、日志和诊断不得污染 stdout。
- 公共授权只有 `read` / `write`：配对后默认可读（含笔记），写默认关闭，只能在 App 设置开启；禁止增加笔记、Star、批量或破坏性细分授权。
- 专用写命令默认预览，`--apply` 执行。通用调用尊重 App Schema 和显式 `dry_run`；`--apply` 仅映射为 `false`，不能覆盖显式 `true`，也不能要求无预览工具额外预览。
- `starcat search` 是 launcher 的稳定 JSON 入口。调整字段、错误码、部分来源 warning
  或 URL 语义时，必须同步 `contracts/global-search/` schema、fixtures 和测试。
- Alfred、uTools、Raycast 只消费此 contract；不得为了单个适配器加入私有分叉格式。
- 明文 HTTP 只允许 loopback；远程连接必须保持 TLS 1.3、证书指纹绑定和独立可撤销设备 token。
- 长期 token 不得进入命令参数、stdout、日志或项目文件。
- `scripts/build-all.sh` 只生成 `dist/` 资产与 `checksums.txt`；稳定 tag 会触发
  GitHub Release，并自动渲染、提交 `homebrew-starcat-cli` Formula。

## 发布禁令

未经 dong4j 在当前任务中明确授权，禁止执行 `scripts/build-all.sh` 等打包脚本、
创建或推送 tag、执行 `git push`、发布 GitHub Release、上传二进制或安装器、
更新远端 Homebrew Formula、触发发布 workflow 或执行任何对外分发操作。
