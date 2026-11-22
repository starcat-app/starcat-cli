// Package mcp 实现 Starcat CLI 的 MCP Streamable HTTP client 与 stdio bridge。
// 所有业务命令都通过该 client 调用 Starcat MCP Tools，不复制 App 内部业务逻辑。
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/starcat-app/starcat-cli/internal/config"
)

const MCPProtocolVersion = "2025-03-26"

var (
	// ErrUnavailable 表示 CLI 无法连接 MCP transport。调用方依赖这个类型分类，
	// 不能再从底层英文网络错误中猜测 Service 状态。
	ErrUnavailable = errors.New("Starcat MCP service is unavailable")
	// ErrUnauthorized 表示已保存的设备凭据失效，需要重新配对。
	ErrUnauthorized = errors.New("Starcat MCP device credential is unauthorized")
)

// ToolError 是 MCP `tools/call` 业务失败的机器可读结果。
//
// Code 来自 App 的 structuredContent，Message 仅供人类诊断；Launcher 必须只按
// Code 分支，避免 App 文案调整破坏 CLI 行为。
type ToolError struct {
	Code    string
	Message string
}

func (e *ToolError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Message
}

// HTTPTransport 负责认证和 MCP HTTP 状态映射。
type HTTPTransport struct {
	endpoint string
	token    string
	client   *http.Client
}

// NewHTTPClient 直接访问配对端点，避免本机或局域网流量被系统代理或重定向改道。
func NewHTTPClient(timeout time.Duration) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func NewHTTPTransport(profile config.Profile, token string) (*HTTPTransport, error) {
	if err := profile.Validate(); err != nil {
		return nil, err
	}
	if _, legacy := profile.HTTPVersion(); legacy {
		return nil, errors.New("Starcat MCP transport requires an HTTP endpoint")
	}
	return &HTTPTransport{endpoint: profile.Endpoint, token: token, client: NewHTTPClient(30 * time.Second)}, nil
}

// Send 原样转发一个 JSON-RPC 消息。它同时服务 MCP stdio bridge 和高层 tool client。
func (t *HTTPTransport) Send(ctx context.Context, body []byte) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Authorization", "Bearer "+t.token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("MCP-Protocol-Version", MCPProtocolVersion)

	response, err := t.client.Do(request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return 0, nil, fmt.Errorf("connect to Starcat: %w", err)
		}
		return 0, nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return response.StatusCode, nil, fmt.Errorf("read Starcat response: %w", err)
	}
	if response.StatusCode == http.StatusUnauthorized {
		return response.StatusCode, nil, ErrUnauthorized
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.StatusCode, nil, fmt.Errorf("Starcat returned HTTP %d: %s", response.StatusCode, serverMessage(data))
	}
	return response.StatusCode, data, nil
}

// Client 是供 CLI 子命令使用的最小 JSON-RPC client。
type Client struct {
	transport   *HTTPTransport
	mu          sync.Mutex
	nextID      int64
	initialized bool
}

func NewClient(transport *HTTPTransport) *Client {
	return &Client{transport: transport, nextID: 1}
}

func (c *Client) Initialize(ctx context.Context) error {
	c.mu.Lock()
	if c.initialized {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	_, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": MCPProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo": map[string]any{
			"name":    "starcat-cli",
			"version": Version,
		},
	})
	if err != nil {
		return err
	}
	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		return err
	}
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

func (c *Client) ListTools(ctx context.Context) ([]map[string]any, error) {
	return c.list(ctx, "tools/list", "tools")
}

// ListResources 保留 App 的 URI 与 MIME 元数据，不在 CLI 枚举本机文件。
func (c *Client) ListResources(ctx context.Context) ([]map[string]any, error) {
	return c.list(ctx, "resources/list", "resources")
}

// ReadResource 返回协议原始 contents，保留 text/blob、URI 和内容类型。
func (c *Client) ReadResource(ctx context.Context, uri string) (map[string]any, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	return c.request(ctx, "resources/read", map[string]any{"uri": uri})
}

// list 消费协议 cursor；目录增长时不能只返回第一页或无限重复同一页。
func (c *Client) list(ctx context.Context, method, field string) ([]map[string]any, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0)
	params := map[string]any{}
	seen := make(map[string]bool)
	for {
		result, err := c.request(ctx, method, params)
		if err != nil {
			return nil, err
		}
		items, ok := result[field].([]any)
		if !ok {
			return nil, fmt.Errorf("MCP %s response is missing %s", method, field)
		}
		for _, item := range items {
			value, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("MCP %s returned an invalid descriptor", method)
			}
			out = append(out, value)
		}
		cursor, _ := result["nextCursor"].(string)
		if cursor == "" {
			return out, nil
		}
		if seen[cursor] {
			return nil, fmt.Errorf("MCP %s repeated a pagination cursor", method)
		}
		seen[cursor] = true
		params["cursor"] = cursor
	}
}

func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (any, error) {
	if err := c.Initialize(ctx); err != nil {
		return nil, err
	}
	result, err := c.request(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return nil, err
	}
	if isError, _ := result["isError"].(bool); isError {
		return nil, toolError(result)
	}
	if structured, ok := result["structuredContent"]; ok {
		return structured, nil
	}
	return result, nil
}

func (c *Client) request(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.mu.Unlock()
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	_, data, err := c.transport.Send(ctx, payload)
	if err != nil {
		return nil, err
	}
	var response struct {
		Result map[string]any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode JSON-RPC response: %w", err)
	}
	if response.Error != nil {
		return nil, fmt.Errorf("MCP %d: %s", response.Error.Code, response.Error.Message)
	}
	if response.Result == nil {
		return nil, errors.New("JSON-RPC response is missing result")
	}
	return response.Result, nil
}

func (c *Client) notify(ctx context.Context, method string, params map[string]any) error {
	payload := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		payload["params"] = params
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, _, err = c.transport.Send(ctx, data)
	return err
}

func serverMessage(data []byte) string {
	var body map[string]any
	if json.Unmarshal(data, &body) == nil {
		if message, ok := body["error"].(string); ok {
			return message
		}
		if object, ok := body["error"].(map[string]any); ok {
			if message, ok := object["message"].(string); ok {
				return message
			}
		}
	}
	return strings.TrimSpace(string(data))
}

func toolError(result map[string]any) error {
	if structured, ok := result["structuredContent"].(map[string]any); ok {
		code, _ := structured["code"].(string)
		message, _ := structured["message"].(string)
		if code != "" {
			return &ToolError{Code: code, Message: message}
		}
	}
	return &ToolError{Code: "MCP_TOOL_ERROR", Message: toolErrorMessage(result)}
}

func toolErrorMessage(result map[string]any) string {
	content, _ := result["content"].([]any)
	for _, item := range content {
		value, _ := item.(map[string]any)
		if value["type"] == "text" {
			if text, ok := value["text"].(string); ok {
				return text
			}
		}
	}
	return "Starcat MCP tool call failed"
}

// Version 在构建发布产物时由 -ldflags 覆盖。
var Version = "dev"
