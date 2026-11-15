// Package browserhost 实现 Chrome Native Messaging 的长度前缀协议。
// 它只转发 Companion 既有 loopback 路由，不连接 MCP、不扩大浏览器权益，
// 也不把浏览器设备凭据写入 CLI profile 或人类可见输出。
package browserhost

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const Name = "com.starcat.browser"
const maxMessageBytes = 1 << 20

var extensionID = regexp.MustCompile(`^[a-p]{32}$`)

// Request 的 token 仅来自扩展后台；路径、方法与地址在宿主再次校验。
type Request struct {
	Action     string          `json:"action"`
	ServiceURL string          `json:"service_url"`
	Path       string          `json:"path"`
	Method     string          `json:"method"`
	Body       json.RawMessage `json:"body"`
	Token      string          `json:"token"`
}

// Host 使用可替换的网络客户端，使协议测试无需启动用户的 Starcat。
type Host struct {
	Client *http.Client
}

// Serve 每次处理一个原生请求；事件订阅持续输出，浏览器关闭 stdin 即取消网络请求。
// 此 framing 与 MCP 逐行 JSON-RPC 不兼容，不能把两者串接到同一个 stdout。
func (h Host) Serve(ctx context.Context, input io.Reader, output io.Writer, origin string) error {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "chrome-extension" || !extensionID.MatchString(parsed.Host) ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
		return errors.New("a Chrome extension origin is required")
	}
	var request Request
	if err := readMessage(input, &request); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if request.Action == "events" {
		go func() {
			_, _ = io.Copy(io.Discard, input)
			cancel()
		}()
	}
	origin = "chrome-extension://" + parsed.Host
	switch request.Action {
	case "discover":
		candidates := []map[string]any{}
		seen := map[string]bool{}
		for _, base := range []string{request.ServiceURL, "http://127.0.0.1:5051", "http://127.0.0.1:5001"} {
			if base == "" || seen[base] {
				continue
			}
			seen[base] = true
			probe, finish := context.WithTimeout(ctx, 1300*time.Millisecond)
			response, requestErr := h.request(probe, Request{ServiceURL: base, Path: "/plugin/v1/discover", Method: "GET"}, origin)
			if requestErr == nil {
				var data map[string]any
				decodeErr := json.NewDecoder(io.LimitReader(response.Body, maxMessageBytes)).Decode(&data)
				_ = response.Body.Close()
				instance, _ := data["instance_id"].(string)
				if decodeErr == nil && response.StatusCode == 200 && data["product"] == "starcat" &&
					data["protocol_version"] == "1" && data["pairing"] == "numeric_comparison" &&
					instance != "" && !seen["instance:"+instance] {
					seen["instance:"+instance] = true
					candidates = append(candidates, map[string]any{
						"serviceURL": base, "instanceID": instance, "channel": data["channel"], "transport": "native",
					})
				}
			}
			finish()
		}
		return writeMessage(output, map[string]any{"ok": true, "body": map[string]any{"candidates": candidates}})
	case "request", "events":
		response, err := h.request(ctx, request, origin)
		if err != nil {
			return writeMessage(output, map[string]any{"ok": false, "error": "companion_unavailable"})
		}
		defer response.Body.Close()
		if request.Action == "events" && response.StatusCode == http.StatusOK {
			return forwardEvents(response.Body, output)
		}
		var body any
		if err := json.NewDecoder(io.LimitReader(response.Body, maxMessageBytes)).Decode(&body); err != nil {
			return writeMessage(output, map[string]any{"ok": false, "error": "invalid_companion_response"})
		}
		return writeMessage(output, map[string]any{"ok": response.StatusCode >= 200 && response.StatusCode < 300, "status": response.StatusCode, "body": body})
	default:
		return writeMessage(output, map[string]any{"ok": false, "error": "unsupported_native_action"})
	}
}

func (h Host) request(ctx context.Context, request Request, origin string) (*http.Response, error) {
	base, err := url.Parse(request.ServiceURL)
	if err != nil || base.Scheme != "http" || (base.Hostname() != "127.0.0.1" && base.Hostname() != "localhost") ||
		base.Port() == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("invalid_service_url")
	}
	path, err := url.Parse(request.Path)
	if err != nil || path.IsAbs() || path.Host != "" || path.Fragment != "" || !allowed(request.Method, path.Path, request.Action) {
		return nil, errors.New("unsupported_local_request")
	}
	body := io.Reader(nil)
	if len(request.Body) != 0 && string(request.Body) != "null" {
		body = strings.NewReader(string(request.Body))
	}
	base.Path, base.RawQuery = path.Path, path.RawQuery
	httpRequest, err := http.NewRequestWithContext(ctx, request.Method, base.String(), body)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Origin", origin)
	if request.Token != "" {
		httpRequest.Header.Set("Authorization", "Bearer "+request.Token)
	}
	if request.Action == "events" {
		httpRequest.Header.Set("Accept", "text/event-stream")
	} else {
		var finish context.CancelFunc
		ctx, finish = context.WithTimeout(ctx, 5*time.Second)
		httpRequest = httpRequest.WithContext(ctx)
		response, requestErr := h.client().Do(httpRequest)
		if requestErr != nil {
			finish()
			return nil, requestErr
		}
		response.Body = &cancelBody{ReadCloser: response.Body, cancel: finish}
		return response, nil
	}
	return h.client().Do(httpRequest)
}

func (h Host) client() *http.Client {
	client := &http.Client{}
	if h.Client != nil {
		*client = *h.Client
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return errors.New("redirect_forbidden") }
	return client
}

type cancelBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (body *cancelBody) Close() error {
	defer body.cancel()
	return body.ReadCloser.Close()
}

func allowed(method, path, action string) bool {
	switch method {
	case "GET":
		return path == "/plugin/v1/discover" || path == "/plugin/v1/ping" || path == "/plugin/v1/repo-context" ||
			(path == "/plugin/v1/events" && action == "events")
	case "POST":
		return path == "/plugin/v1/pairing/start" || path == "/plugin/v1/pairing/poll" ||
			path == "/plugin/v1/pairing/cancel" || path == "/plugin/v1/devices/revoke-current" ||
			path == "/plugin/v1/stars/state" || path == "/plugin/v1/recommendations/more" || path == "/plugin/v1/actions/open"
	case "PATCH":
		return path == "/plugin/v1/notes" || path == "/plugin/v1/tags" || path == "/plugin/v1/library-state"
	}
	return false
}

func readMessage(input io.Reader, value any) error {
	var size uint32
	if err := binary.Read(input, binary.NativeEndian, &size); err != nil {
		return err
	}
	if size == 0 || size > maxMessageBytes {
		return errors.New("invalid_native_message_size")
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(input, body); err != nil {
		return err
	}
	return json.Unmarshal(body, value)
}

func writeMessage(output io.Writer, value any) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(body) > maxMessageBytes {
		return errors.New("native_message_too_large")
	}
	if err := binary.Write(output, binary.NativeEndian, uint32(len(body))); err != nil {
		return err
	}
	_, err = output.Write(body)
	return err
}

// forwardEvents 保留 Companion 的事件名与数据，订阅撤销使 HTTP EOF 立即结束原生进程。
func forwardEvents(input io.Reader, output io.Writer) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), maxMessageBytes)
	event := "message"
	var data []string
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		switch {
		case line == "":
			if len(data) != 0 {
				body := json.RawMessage(strings.Join(data, "\n"))
				if !json.Valid(body) {
					return errors.New("invalid_event_data")
				}
				if err := writeMessage(output, map[string]any{"ok": true, "event": map[string]any{"type": event, "data": body}}); err != nil {
					return err
				}
			}
			event, data = "message", nil
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = append(data, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("event_stream_failed: %w", err)
	}
	return nil
}
