// Package cli 提供 starcat 命令入口。
// 命令只做参数校验与 MCP Tool 映射，写操作默认 dry-run，必须显式传 --apply。
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/starcat-app/starcat-cli/internal/config"
	"github.com/starcat-app/starcat-cli/internal/credential"
	"github.com/starcat-app/starcat-cli/internal/mcp"
	"github.com/starcat-app/starcat-cli/internal/pairing"
	"github.com/starcat-app/starcat-cli/internal/updater"
)

const (
	maxNoteBytes       = 1 << 20
	maxPairingURIBytes = 16 << 10
)

// Runner 持有可替换依赖，生产入口和测试共享完全相同的命令路径。
type Runner struct {
	Profiles    config.Store
	Credentials credential.Store
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	RunUpdate   func(context.Context, string) (updater.Result, error)

	// stdinInteractive 只控制人类可见提示；`starcat mcp` 的 stdout 仍严格保留给协议输出。
	stdinInteractive bool
}

func NewRunner(stdin io.Reader, stdout, stderr io.Writer) (*Runner, error) {
	profiles, err := config.DefaultFileStore()
	if err != nil {
		return nil, err
	}
	return &Runner{
		Profiles:         profiles,
		Credentials:      credential.KeyringStore{},
		Stdin:            stdin,
		Stdout:           stdout,
		Stderr:           stderr,
		RunUpdate:        updater.NewClient().Update,
		stdinInteractive: isInteractiveReader(stdin),
	}, nil
}

func (r *Runner) Run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return r.writeText(usage)
	}
	switch args[0] {
	case "help", "--help", "-h":
		return r.runHelp(args[1:])
	case "version", "--version":
		if len(args) != 1 {
			return errors.New("Usage: starcat version")
		}
		return r.writeText("Starcat CLI " + mcp.Version)
	case "pair":
		return r.runPair(ctx, args[1:])
	case "unpair":
		if len(args) != 1 {
			return errors.New("Usage: starcat unpair")
		}
		return r.unpair()
	case "update":
		if len(args) != 1 {
			return errors.New("Usage: starcat update")
		}
		if r.RunUpdate == nil {
			return errors.New("the update service is unavailable")
		}
		result, err := r.RunUpdate(ctx, mcp.Version)
		if err != nil {
			return err
		}
		return r.writeUpdateResult(result)
	case "doctor":
		if len(args) != 1 {
			return errors.New("Usage: starcat doctor")
		}
		return r.doctor(ctx)
	case "capabilities":
		if len(args) != 1 {
			return errors.New("Usage: starcat capabilities")
		}
		return r.call(ctx, "starcat.get_capabilities", map[string]any{})
	case "stats":
		return r.runStats(ctx, args[1:])
	case "search":
		return r.runSearch(ctx, args[1:])
	case "mcp":
		if len(args) != 1 {
			return errors.New("Usage: starcat mcp")
		}
		transport, err := r.loadTransport()
		if err != nil {
			return err
		}
		return mcp.BridgeStdio(ctx, transport, r.Stdin, r.Stdout)
	case "repo":
		return r.runRepo(ctx, args[1:])
	case "tags":
		if len(args) == 2 && args[1] == "list" {
			return r.call(ctx, "starcat.list_tags", map[string]any{})
		}
		return errors.New("Usage: starcat tags list")
	case "tag":
		return r.runTag(ctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q; run `starcat help` for usage", args[0])
	}
}

// runSearch 是 Alfred 等外部启动器使用的顶层全局搜索命令。
// 现有 `repo search` 保持纯本地/语义语义，避免破坏 Agent 已依赖的网络边界。
func (r *Runner) runSearch(ctx context.Context, args []string) error {
	positionals, flags, err := parseFlags(args, map[string]bool{
		"source": true,
		"limit":  true,
	})
	if err != nil {
		return err
	}
	if len(positionals) != 1 || strings.TrimSpace(positionals[0]) == "" {
		return errors.New("Usage: starcat search <query> [--source all|local|github] [--limit N]")
	}
	limit, err := intFlag(flags, "limit", 30, 1, 50)
	if err != nil {
		return err
	}
	source := valueFlag(flags, "source", "all")
	var sources []string
	switch source {
	case "all":
		sources = []string{"local", "github"}
	case "local", "github":
		sources = []string{source}
	default:
		return errors.New("--source must be all, local, or github")
	}
	err = r.call(ctx, "starcat.global_search_repos", map[string]any{
		"query":   positionals[0],
		"limit":   limit,
		"sources": sources,
	})
	if err != nil {
		return classifyGlobalSearchError(err)
	}
	return nil
}

// codedCommandError 让 Alfred 等外部调用方依赖稳定 code，而不是解析英文错误全文。
// 人类仍能在同一行看到原始说明，现有终端使用方式不受影响。
type codedCommandError struct {
	code    string
	message string
}

func (e codedCommandError) Error() string {
	return fmt.Sprintf("STARCAT_ERROR %s: %s", e.code, e.message)
}

func classifyGlobalSearchError(err error) error {
	code := "SEARCH_FAILED"
	var toolError *mcp.ToolError
	switch {
	case errors.Is(err, config.ErrNotPaired):
		code = "CLI_NOT_PAIRED"
	case errors.Is(err, mcp.ErrUnauthorized):
		code = "CLI_NOT_PAIRED"
	case errors.Is(err, context.DeadlineExceeded):
		code = "SEARCH_TIMEOUT"
	case errors.Is(err, mcp.ErrUnavailable):
		code = "MCP_DISABLED"
	case errors.As(err, &toolError):
		switch toolError.Code {
		case "REQUIRES_PRO":
			code = "REQUIRES_PRO"
		case "MCP_DISABLED":
			code = "MCP_DISABLED"
		case "UPGRADE_REQUIRED":
			code = "UPGRADE_REQUIRED"
		case "SEARCH_TIMEOUT":
			code = "SEARCH_TIMEOUT"
		case "SEARCH_FAILED", "INTERNAL_ERROR", "INVALID_ARGUMENTS":
			code = "SEARCH_FAILED"
		}
	}
	return codedCommandError{code: code, message: err.Error()}
}

func (r *Runner) runHelp(args []string) error {
	if len(args) == 0 {
		return r.writeText(usage)
	}
	if len(args) != 1 {
		return errors.New("Usage: starcat help [command]")
	}
	help, ok := commandHelp[args[0]]
	if !ok {
		return fmt.Errorf("unknown command %q; run `starcat help` for usage", args[0])
	}
	return r.writeText(help)
}

func (r *Runner) runPair(ctx context.Context, args []string) error {
	var rawURI string
	switch len(args) {
	case 0:
		if r.stdinInteractive && r.Stderr != nil {
			fmt.Fprintln(r.Stderr, pairingInputPrompt())
		}
		line, err := readLineContext(ctx, io.LimitReader(r.Stdin, maxPairingURIBytes+1))
		if err != nil {
			return fmt.Errorf("read pairing URI from stdin: %w", err)
		}
		rawURI = line
	case 1:
		if strings.HasPrefix(args[0], "--") {
			return fmt.Errorf("unknown flag %q; run `starcat help pair` for usage", args[0])
		}
		rawURI = args[0]
	default:
		return errors.New("Usage: starcat pair [one-time-pairing-URI]")
	}
	if len(rawURI) > maxPairingURIBytes {
		return fmt.Errorf("pairing URI exceeds the %d-byte limit", maxPairingURIBytes)
	}
	profile, err := (pairing.Service{Profiles: r.Profiles, Credentials: r.Credentials}).Pair(ctx, rawURI)
	if err != nil {
		return err
	}
	return r.writeText(fmt.Sprintf(
		"✓ Paired with Starcat successfully.\n\nEndpoint: %s\nDevice ID: %s\nProtocol version: %s\n\nNext step:\n  starcat doctor",
		profile.Endpoint,
		profile.DeviceID,
		profile.ProtocolVersion,
	))
}

func (r *Runner) runRepo(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("missing repo subcommand")
	}
	switch args[0] {
	case "search":
		positionals, flags, err := parseFlags(args[1:], map[string]bool{
			"scope": true, "limit": true, "semantic": false,
		})
		if err != nil {
			return err
		}
		if len(positionals) != 1 {
			return errors.New("Usage: starcat repo search <query> [--scope starred|knowledge|all] [--limit N] [--semantic]")
		}
		limit, err := intFlag(flags, "limit", 20, 1, 100)
		if err != nil {
			return err
		}
		scope := valueFlag(flags, "scope", "starred")
		if scope != "starred" && scope != "knowledge" && scope != "all" {
			return errors.New("--scope must be starred, knowledge, or all")
		}
		tool := "starcat.search_repos"
		if hasFlag(flags, "semantic") {
			tool = "starcat.semantic_search"
		}
		return r.call(ctx, tool, map[string]any{"query": positionals[0], "scope": scope, "limit": limit})
	case "context", "readme", "summary":
		allowedFlags := map[string]bool{}
		if args[0] == "summary" {
			allowedFlags["generate"] = false
			allowedFlags["allow-external-context"] = false
		}
		positionals, flags, err := parseFlags(args[1:], allowedFlags)
		if err != nil {
			return err
		}
		if len(positionals) != 1 {
			return fmt.Errorf("Usage: starcat repo %s <owner/name>", args[0])
		}
		selector, err := repoSelector(positionals[0])
		if err != nil {
			return err
		}
		tool := map[string]string{
			"context": "starcat.get_repo_context",
			"readme":  "starcat.get_readme",
			"summary": "starcat.get_repo_summary",
		}[args[0]]
		if args[0] == "summary" && hasFlag(flags, "allow-external-context") && !hasFlag(flags, "generate") {
			return errors.New("--allow-external-context requires --generate")
		}
		if args[0] == "summary" && hasFlag(flags, "generate") {
			tool = "starcat.generate_repo_summary"
			selector["allow_external_context"] = hasFlag(flags, "allow-external-context")
		}
		return r.call(ctx, tool, selector)
	case "note":
		return r.runRepoNote(ctx, args[1:])
	case "status":
		return r.runRepoStatus(ctx, args[1:])
	case "tags":
		return r.runRepoTags(ctx, args[1:])
	default:
		return fmt.Errorf("unknown repo subcommand %q", args[0])
	}
}

func (r *Runner) runRepoNote(ctx context.Context, args []string) error {
	positionals, flags, err := parseFlags(args, map[string]bool{"apply": false})
	if err != nil {
		return err
	}
	if len(positionals) != 2 || positionals[0] != "set" {
		return errors.New("Usage: starcat repo note set <owner/name> [--apply]")
	}
	selector, err := repoSelector(positionals[1])
	if err != nil {
		return err
	}

	// 必须在读取 stdin 前预检权限，否则交互式终端会先无限等待正文，用户永远看不到
	// Starcat 已关闭写入的明确提示。
	client, err := r.writeClient(ctx, "local_writes")
	if err != nil {
		return err
	}
	if r.stdinInteractive && r.Stderr != nil {
		fmt.Fprintln(r.Stderr, interactiveNotePrompt())
	}
	content, err := readAllContext(ctx, io.LimitReader(r.Stdin, maxNoteBytes+1))
	if err != nil {
		return fmt.Errorf("read note from stdin: %w", err)
	}
	if len(content) > maxNoteBytes {
		return errors.New("note exceeds the 1 MiB limit")
	}
	selector["content"] = string(content)
	selector["dry_run"] = !hasFlag(flags, "apply")
	return r.callClient(ctx, client, "starcat.upsert_repo_note", selector)
}

func (r *Runner) runRepoStatus(ctx context.Context, args []string) error {
	positionals, flags, err := parseFlags(args, map[string]bool{"apply": false})
	if err != nil {
		return err
	}
	if len(positionals) != 3 || positionals[0] != "set" {
		return errors.New("Usage: starcat repo status set <owner/name> <unread|read|using> [--apply]")
	}
	status := positionals[2]
	if status != "unread" && status != "read" && status != "using" {
		return errors.New("status must be unread, read, or using")
	}
	selector, err := repoSelector(positionals[1])
	if err != nil {
		return err
	}
	selector["status"] = status
	selector["dry_run"] = !hasFlag(flags, "apply")
	return r.callWrite(ctx, "starcat.set_repo_status", "local_writes", selector)
}

func (r *Runner) runRepoTags(ctx context.Context, args []string) error {
	positionals, flags, err := parseFlags(args, map[string]bool{"apply": false})
	if err != nil {
		return err
	}
	if len(positionals) < 3 {
		return errors.New("Usage: starcat repo tags <add|remove|replace> <owner/name> <tag...> [--apply]")
	}
	action := positionals[0]
	tool, ok := map[string]string{
		"add":     "starcat.add_repo_tags",
		"remove":  "starcat.remove_repo_tags",
		"replace": "starcat.set_repo_tags",
	}[action]
	if !ok {
		return errors.New("tag action must be add, remove, or replace")
	}
	selector, err := repoSelector(positionals[1])
	if err != nil {
		return err
	}
	selector["tags"] = positionals[2:]
	selector["dry_run"] = !hasFlag(flags, "apply")
	if action != "remove" {
		selector["create_missing"] = true
	}
	capability := "local_writes"
	if action == "replace" {
		capability = "destructive_writes"
	}
	return r.callWrite(ctx, tool, capability, selector)
}

func (r *Runner) runTag(ctx context.Context, args []string) error {
	positionals, flags, err := parseFlags(args, map[string]bool{
		"color": true, "icon": true, "apply": false,
	})
	if err != nil {
		return err
	}
	if len(positionals) != 2 || positionals[0] != "create" {
		return errors.New("Usage: starcat tag create <name> [--color '#0A84FF'] [--icon tag] [--apply]")
	}
	arguments := map[string]any{"name": positionals[1], "dry_run": !hasFlag(flags, "apply")}
	if value := valueFlag(flags, "color", ""); value != "" {
		arguments["color"] = value
	}
	if value := valueFlag(flags, "icon", ""); value != "" {
		arguments["icon"] = value
	}
	return r.callWrite(ctx, "starcat.create_tag", "local_writes", arguments)
}

func (r *Runner) doctor(ctx context.Context) error {
	profile, err := r.Profiles.Load()
	if err != nil {
		return err
	}
	client, err := r.loadClient()
	if err != nil {
		return err
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return err
	}
	if _, err := client.CallTool(ctx, "starcat.get_capabilities", map[string]any{}); err != nil {
		return err
	}
	tlsStatus := "✓ Local loopback connection"
	if strings.HasPrefix(profile.Endpoint, "https://") {
		tlsStatus = "✓ TLS certificate fingerprint verified"
	}
	return r.writeText(fmt.Sprintf(
		"Starcat Doctor\n\n✓ Pairing profile found\n%s\n✓ MCP connection healthy\n✓ Available tools: %d\n✓ Capabilities loaded\n\nCLI version: %s\nApp version: %s\nProtocol version: %s\nEndpoint: %s",
		tlsStatus,
		len(tools),
		mcp.Version,
		profile.AppVersion,
		profile.ProtocolVersion,
		profile.Endpoint,
	))
}

func (r *Runner) unpair() error {
	profile, err := r.Profiles.Load()
	if err != nil && !errors.Is(err, config.ErrNotPaired) {
		return err
	}
	if err == nil {
		if err := r.Credentials.Delete(profile.DeviceID); err != nil {
			return err
		}
	}
	if err := r.Profiles.Delete(); err != nil {
		return err
	}
	return r.writeText("✓ Starcat CLI is no longer paired.")
}

func (r *Runner) call(ctx context.Context, tool string, arguments map[string]any) error {
	client, err := r.loadClient()
	if err != nil {
		return err
	}
	return r.callClient(ctx, client, tool, arguments)
}

// callWrite 在所有 CLI 写命令前统一检查 Starcat 当前能力，避免不同命令返回不一致的提示。
func (r *Runner) callWrite(
	ctx context.Context,
	tool string,
	capability string,
	arguments map[string]any,
) error {
	client, err := r.writeClient(ctx, capability)
	if err != nil {
		return err
	}
	return r.callClient(ctx, client, tool, arguments)
}

func (r *Runner) callClient(ctx context.Context, client *mcp.Client, tool string, arguments map[string]any) error {
	value, err := client.CallTool(ctx, tool, arguments)
	if err != nil {
		return err
	}
	return r.writeJSON(value)
}

// writeClient 使用公开的 capabilities Tool 作为唯一权限事实源，不在 CLI 复制 App 设置状态。
func (r *Runner) writeClient(ctx context.Context, capability string) (*mcp.Client, error) {
	client, err := r.loadClient()
	if err != nil {
		return nil, err
	}
	value, err := client.CallTool(ctx, "starcat.get_capabilities", map[string]any{})
	if err != nil {
		return nil, err
	}
	capabilities, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("Starcat returned an invalid capabilities response")
	}
	enabled, ok := capabilities[capability].(bool)
	if !ok {
		return nil, fmt.Errorf("Starcat capabilities response is missing %q", capability)
	}
	if enabled {
		return client, nil
	}
	switch capability {
	case "local_writes":
		return nil, errors.New("local writes are disabled; open Starcat > Settings > MCP Service, enable Allow Local Writes, and try again")
	case "destructive_writes":
		return nil, errors.New("replace/delete writes are disabled; open Starcat > Settings > MCP Service, enable Allow Local Writes and Allow Replace/Delete Writes, then try again")
	default:
		return nil, fmt.Errorf("Starcat write capability %q is disabled", capability)
	}
}

func (r *Runner) loadClient() (*mcp.Client, error) {
	transport, err := r.loadTransport()
	if err != nil {
		return nil, err
	}
	return mcp.NewClient(transport), nil
}

func (r *Runner) loadTransport() (*mcp.HTTPTransport, error) {
	profile, err := r.Profiles.Load()
	if err != nil {
		return nil, err
	}
	token, err := r.Credentials.Get(profile.DeviceID)
	if err != nil {
		return nil, err
	}
	return mcp.NewHTTPTransport(profile, token)
}

func (r *Runner) writeJSON(value any) error {
	encoder := json.NewEncoder(r.Stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func (r *Runner) writeText(value string) error {
	_, err := fmt.Fprintln(r.Stdout, value)
	return err
}

func (r *Runner) writeUpdateResult(result updater.Result) error {
	if result.Updated {
		return r.writeText(fmt.Sprintf(
			"✓ Updated Starcat CLI from %s to %s.\nExecutable: %s",
			result.CurrentVersion,
			result.LatestVersion,
			result.Executable,
		))
	}
	return r.writeText(fmt.Sprintf("✓ Starcat CLI is up to date (%s).", result.CurrentVersion))
}

// readAllContext 让被 signal.NotifyContext 取消的 CLI 能立即退出。
// 终端读取本身没有跨平台的 context API，因此放到 goroutine 中；生产入口取消后会立刻结束进程，
// 缓冲 channel 则保证读取稍后结束时不会再次阻塞该 goroutine。
func readAllContext(ctx context.Context, reader io.Reader) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	results := make(chan result, 1)
	go func() {
		data, err := io.ReadAll(reader)
		results <- result{data: data, err: err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-results:
		return result.data, result.err
	}
}

// pairing URI 是单行值；读取到换行就提交，让交互终端粘贴后按 Enter 即可。
// EOF 仍视为正常结束，因此 `printf %s "$URI" | starcat pair` 等管道不需要补换行。
func readLineContext(ctx context.Context, reader io.Reader) (string, error) {
	type result struct {
		line string
		err  error
	}
	results := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(reader).ReadString('\n')
		if errors.Is(err, io.EOF) && line != "" {
			err = nil
		}
		results <- result{line: strings.TrimSpace(line), err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case result := <-results:
		return result.line, result.err
	}
}

// isInteractiveReader 区分真实终端与 Agent 常用的 pipe/file stdin，避免污染自动化 stderr。
func isInteractiveReader(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// interactiveNotePrompt 使用各平台真实的终端 EOF 快捷键，避免跨平台 CLI 给出错误操作指引。
func interactiveNotePrompt() string {
	if runtime.GOOS == "windows" {
		return "Reading note content from stdin. Press Ctrl+Z, then Enter to submit; press Ctrl+C to cancel."
	}
	return "Reading note content from stdin. Press Ctrl+D to submit or Ctrl+C to cancel."
}

func pairingInputPrompt() string {
	return "Paste the one-time pairing URI, then press Enter to pair. Press Ctrl+C to cancel."
}

func repoSelector(value string) (map[string]any, error) {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return nil, errors.New("repository must use owner/name format")
	}
	return map[string]any{"owner": parts[0], "name": parts[1]}, nil
}

// parseFlags 的 allowedFlags value 表示该 flag 是否必须带值。所有未声明 flag 都报错，
// 避免拼写错误或已经删除的参数被静默忽略。
func parseFlags(args []string, allowedFlags map[string]bool) ([]string, map[string][]string, error) {
	positionals := make([]string, 0, len(args))
	flags := make(map[string][]string)
	for index := 0; index < len(args); index++ {
		item := args[index]
		if !strings.HasPrefix(item, "--") {
			positionals = append(positionals, item)
			continue
		}
		name := strings.TrimPrefix(item, "--")
		requiresValue, allowed := allowedFlags[name]
		if !allowed {
			return nil, nil, fmt.Errorf("unknown flag %q", item)
		}
		if requiresValue {
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				return nil, nil, fmt.Errorf("--%s requires a value", name)
			}
			index++
			flags[name] = []string{args[index]}
			continue
		}
		flags[name] = nil
	}
	return positionals, flags, nil
}

func hasFlag(flags map[string][]string, name string) bool {
	_, ok := flags[name]
	return ok
}

func valueFlag(flags map[string][]string, name, fallback string) string {
	values := flags[name]
	if len(values) == 1 {
		return values[0]
	}
	return fallback
}

func intFlag(flags map[string][]string, name string, fallback, minimum, maximum int) (int, error) {
	raw := valueFlag(flags, name, "")
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("--%s must be between %d and %d", name, minimum, maximum)
	}
	return value, nil
}

const usage = `Starcat CLI

Usage:
  starcat <command> [options]

Pairing and diagnostics:
  pair [one-time-pairing-URI]  Pair with Starcat; without an argument, paste the URI and press Enter
  unpair                        Remove the paired device credential
  doctor                       Check pairing, connection, tools, and capabilities
  update                        Update a standalone Starcat CLI installation

Information:
  capabilities                  Print Starcat capabilities as JSON
  stats                         Show Starcat repository, AI usage, and RAG statistics
  stats ai [filters]            Show detailed AI token and call statistics
  stats knowledge               Show detailed knowledge-base and chunk statistics
  version                       Print the CLI version

MCP server:
  mcp                           Start the stdio MCP bridge

Read commands:
  search <query> [--source all|local|github] [--limit N]
  repo search <query> [--scope starred|knowledge|all] [--limit N] [--semantic]
  repo context <owner/name>
  repo readme <owner/name>
  repo summary <owner/name> [--generate] [--allow-external-context]
  tags list

Write commands (dry-run by default; --apply persists changes):
  repo note set <owner/name> [--apply]
  repo status set <owner/name> <unread|read|using> [--apply]
  repo tags <add|remove|replace> <owner/name> <tag...> [--apply]
  tag create <name> [--color HEX] [--icon SYMBOL] [--apply]

Run "starcat help <command>" for command-specific help.`

var commandHelp = map[string]string{
	"pair": `Pair a device with Starcat

Usage:
  starcat pair [one-time-pairing-URI]

Without an argument, paste the one-time URI and press Enter. Starcat pairing
commands expire after five minutes, work only once, and still require approval
inside the Starcat app.`,
	"doctor": `Check the Starcat CLI connection

Usage:
  starcat doctor`,
	"capabilities": `Print Starcat capabilities

Usage:
  starcat capabilities

Capabilities are always written as JSON.`,
	"stats": `Show Starcat statistics

Usage:
  starcat stats
  starcat stats ai [--range today|7d|30d|all] [--feature NAME] [--provider ID] [--model NAME]
  starcat stats knowledge

Statistics commands call Starcat MCP tools and render the structured result in
a terminal-friendly format. Agents should use the same tools through starcat mcp.`,
	"search": `Search Starcat and GitHub repositories

Usage:
  starcat search <query> [--source all|local|github] [--limit N]

This command writes the stable global repository search contract as JSON.
Local results open in Starcat; GitHub-only results open on GitHub.`,
	"repo": `Read or update Starcat repositories

Usage:
  starcat repo search <query> [--scope starred|knowledge|all] [--limit N] [--semantic]
  starcat repo context <owner/name>
  starcat repo readme <owner/name>
  starcat repo summary <owner/name> [--generate] [--allow-external-context]
  starcat repo note set <owner/name> [--apply]
  starcat repo status set <owner/name> <unread|read|using> [--apply]
  starcat repo tags <add|remove|replace> <owner/name> <tag...> [--apply]

Note content is always read from stdin.`,
	"tags": `List Starcat tags

Usage:
  starcat tags list`,
	"tag": `Create a Starcat tag

Usage:
  starcat tag create <name> [--color HEX] [--icon SYMBOL] [--apply]`,
	"mcp": `Start the Starcat stdio MCP bridge

Usage:
  starcat mcp`,
	"unpair": `Remove the paired device credential

Usage:
  starcat unpair`,
	"update": `Update a standalone Starcat CLI installation

Usage:
  starcat update`,
	"version": `Print the Starcat CLI version

Usage:
  starcat version`,
}
