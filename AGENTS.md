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
- Go directive 1.25.0；发布 toolchain 固定为 Go 1.26.5，以包含既定 TLS 安全修复
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
go build -o bin/starcat ./cmd/starcat
git diff --check
```

修改平台相关代码时，还应验证 `CGO_ENABLED=0` 的五个目标组合；不要用一次本机构建
代替跨平台检查。

## 项目特有约束

- CLI 是 MCP 薄客户端：搜索、排序、去重、鉴权、授权与写入规则留在 Starcat App，
  禁止新增数据库直读或第二套业务实现。
- `starcat mcp` 的 stdout 必须保持纯协议流；更新提示、日志和诊断不得污染 stdout。
- 写操作默认 dry-run，只有显式 `--apply` 才能持久化；不得弱化该保护。
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
