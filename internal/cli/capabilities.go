// 本文件承载动态能力入口。目录、Schema 和最终权限都来自 App，CLI 只校验传输参数与执行意图。
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/starcat-app/starcat-cli/internal/config"
	"github.com/starcat-app/starcat-cli/internal/mcp"
)

const maxCapabilityInputBytes = 8 << 20

// runTools 不维护工具名 allowlist；批准公开的能力由 App tools/list 决定。
func (r *Runner) runTools(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("Usage: starcat tools <list|describe|call>")
	}
	allowed := map[string]bool{"json": false}
	if args[0] == "call" {
		allowed = map[string]bool{"input": true, "apply": false}
	}
	positionals, flags, err := parseFlags(args[1:], allowed)
	if err != nil {
		return err
	}
	if (args[0] == "list" && len(positionals) != 0) ||
		(args[0] != "list" && len(positionals) != 1) {
		return errors.New("Usage: starcat tools list | describe NAME | call NAME [--input FILE|-] [--apply]")
	}
	if args[0] != "list" && args[0] != "describe" && args[0] != "call" {
		return fmt.Errorf("unknown tools subcommand %q", args[0])
	}
	client, err := r.loadClient()
	if err != nil {
		return err
	}
	tools, err := client.ListTools(ctx)
	if err != nil {
		return err
	}
	if args[0] == "list" {
		return r.writeJSON(tools)
	}
	var descriptor map[string]any
	for _, tool := range tools {
		if tool["name"] == positionals[0] {
			descriptor = tool
			break
		}
	}
	if descriptor == nil {
		return &mcp.ToolError{Code: "NOT_FOUND", Message: "App does not expose tool " + positionals[0]}
	}
	if args[0] == "describe" {
		return r.writeJSON(descriptor)
	}
	arguments, err := r.readCapabilityInput(ctx, valueFlag(flags, "input", ""))
	if err != nil {
		return err
	}
	schema, ok := descriptor["inputSchema"].(map[string]any)
	if !ok {
		return &mcp.ToolError{Code: "UPGRADE_REQUIRED", Message: "tool has no supported input schema"}
	}
	if err := prepareExecution(descriptor, schema, arguments, hasFlag(flags, "apply")); err != nil {
		return err
	}
	if err := validateSchema(schema, arguments, "input"); err != nil {
		return &mcp.ToolError{Code: "INVALID_ARGUMENTS", Message: err.Error()}
	}
	return r.callClient(ctx, client, positionals[0], arguments)
}

// readCapabilityInput 读取调用设备的文件，不把 CLI 路径传给远端 App；日志不包含正文。
func (r *Runner) readCapabilityInput(ctx context.Context, filename string) (map[string]any, error) {
	if filename == "" {
		return map[string]any{}, nil
	}
	reader := r.Stdin
	if filename != "-" {
		file, err := os.Open(filename)
		if err != nil {
			return nil, fmt.Errorf("open input file: %w", err)
		}
		defer file.Close()
		reader = file
	}
	data, err := readAllContext(ctx, io.LimitReader(reader, maxCapabilityInputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxCapabilityInputBytes {
		return nil, errors.New("tool input exceeds the 8 MiB limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var arguments map[string]any
	if err := decoder.Decode(&arguments); err != nil || arguments == nil {
		return nil, &mcp.ToolError{Code: "INVALID_ARGUMENTS", Message: "input must be one JSON object"}
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, &mcp.ToolError{Code: "INVALID_ARGUMENTS", Message: "input must contain exactly one JSON object"}
	}
	return arguments, nil
}

// prepareExecution 不覆盖调用方明确传入的参数；--apply 仅是 dry_run=false 的便利写法。
// 默认值与写权限由 App 决定，使普通 CLI 和 MCP 的同参数调用语义一致。
func prepareExecution(descriptor, schema, arguments map[string]any, apply bool) error {
	properties, _ := schema["properties"].(map[string]any)
	if _, supported := properties["dry_run"]; supported && apply {
		if preview, exists := arguments["dry_run"]; exists && preview != false {
			return &mcp.ToolError{Code: "INVALID_ARGUMENTS", Message: "--apply conflicts with dry_run; omit --apply for a preview"}
		}
		arguments["dry_run"] = false
	}
	return nil
}

func (r *Runner) runResources(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("Usage: starcat resources list [--json] | read URI")
	}
	positionals, _, err := parseFlags(args[1:], map[string]bool{"json": false})
	if err != nil {
		return err
	}
	if (args[0] == "list" && len(positionals) != 0) || (args[0] == "read" && len(positionals) != 1) {
		return errors.New("Usage: starcat resources list [--json] | read URI")
	}
	client, err := r.loadClient()
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		resources, err := client.ListResources(ctx)
		if err != nil {
			return err
		}
		return r.writeJSON(resources)
	case "read":
		value, err := client.ReadResource(ctx, positionals[0])
		if err != nil {
			return err
		}
		return r.writeJSON(value)
	default:
		return fmt.Errorf("unknown resources subcommand %q", args[0])
	}
}

// classifyCapabilityError 与全局搜索分开，保留既有 launcher 错误契约。
func classifyCapabilityError(err error) error {
	if err == nil {
		return nil
	}
	code := "CLI_FAILED"
	var toolError *mcp.ToolError
	switch {
	case errors.Is(err, config.ErrNotPaired):
		code = "CLI_NOT_PAIRED"
	case errors.Is(err, mcp.ErrUnauthorized):
		code = "UNAUTHORIZED"
	case errors.Is(err, mcp.ErrCertificateMismatch):
		code = "CERTIFICATE_MISMATCH"
	case errors.Is(err, mcp.ErrUnavailable):
		code = "MCP_UNAVAILABLE"
	case errors.Is(err, context.DeadlineExceeded):
		code = "TIMEOUT"
	case errors.Is(err, context.Canceled):
		code = "CANCELLED"
	case errors.As(err, &toolError):
		code = toolError.Code
	case strings.HasPrefix(err.Error(), "Usage:"), strings.HasPrefix(err.Error(), "unknown "):
		code = "INVALID_ARGUMENTS"
	}
	return codedCommandError{code: code, message: err.Error()}
}
