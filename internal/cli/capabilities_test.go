// 动态工具测试使用隔离 HTTP App，不读取真实配对信息，也不产生真实业务写入。
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/starcat-app/starcat-cli/internal/config"
)

func TestDynamicToolPreservesExplicitExecution(t *testing.T) {
	for _, apply := range []bool{false, true} {
		t.Run(map[bool]string{false: "preview", true: "apply"}[apply], func(t *testing.T) {
			runner, captured, cleanup := capabilityTestRunner(t, true, false)
			defer cleanup()
			runner.Stdin = strings.NewReader(`{"content":"你好\n\"Starcat\"","dry_run":false}`)
			args := []string{"tools", "call", "starcat.future_write", "--input", "-"}
			if apply {
				args = append(args, "--apply")
			}
			if err := runner.Run(context.Background(), args); err != nil {
				t.Fatal(err)
			}
			captured.Lock()
			defer captured.Unlock()
			if len(captured.arguments) != 1 || captured.arguments[0]["dry_run"] != false {
				t.Fatalf("unexpected execution arguments: %#v", captured.arguments)
			}
			if captured.arguments[0]["content"] != "你好\n\"Starcat\"" {
				t.Fatal("input text was not preserved")
			}
			if !json.Valid(runner.Stdout.(*bytes.Buffer).Bytes()) {
				t.Fatal("stdout is not pure JSON")
			}
		})
	}
}

func TestDynamicSideEffectUsesAppContract(t *testing.T) {
	runner, captured, cleanup := capabilityTestRunner(t, false, false)
	defer cleanup()
	err := runner.Run(context.Background(), []string{"tools", "call", "starcat.future_write"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	captured.Lock()
	defer captured.Unlock()
	if len(captured.arguments) != 1 {
		t.Fatal("explicit operation was not sent to the App")
	}
}

// 显式预览不能被快捷标志覆盖；省略参数时也不替 App 写入默认值。
func TestDynamicApplyDoesNotOverrideExplicitPreview(t *testing.T) {
	schema := map[string]any{"properties": map[string]any{"dry_run": map[string]any{"type": "boolean"}}}
	arguments := map[string]any{"dry_run": true}
	if err := prepareExecution(nil, schema, arguments, true); err == nil {
		t.Fatal("conflicting --apply was accepted")
	}
	if arguments["dry_run"] != true {
		t.Fatal("explicit preview was overwritten")
	}
	arguments = map[string]any{}
	if err := prepareExecution(nil, schema, arguments, false); err != nil {
		t.Fatal(err)
	}
	if _, exists := arguments["dry_run"]; exists {
		t.Fatal("App default was overridden")
	}
}

func TestDynamicToolRejectsInvalidOrUnknownInputBeforeExecution(t *testing.T) {
	for _, input := range []string{`null`, `[]`, `{} {}`, `{"content":42}`, `{"content":"x","limit":0}`} {
		t.Run(input, func(t *testing.T) {
			runner, captured, cleanup := capabilityTestRunner(t, true, false)
			defer cleanup()
			runner.Stdin = strings.NewReader(input)
			if err := runner.Run(context.Background(), []string{"tools", "call", "starcat.future_write", "--input", "-"}); err == nil {
				t.Fatal("invalid input was accepted")
			}
			captured.Lock()
			defer captured.Unlock()
			if len(captured.arguments) != 0 {
				t.Fatal("invalid input reached the executor")
			}
		})
	}
	runner, _, cleanup := capabilityTestRunner(t, true, false)
	defer cleanup()
	err := runner.Run(context.Background(), []string{"tools", "call", "starcat.missing"})
	if err == nil || !strings.Contains(err.Error(), "NOT_FOUND") {
		t.Fatalf("unknown capability error = %v", err)
	}
}

func TestCapabilityDiscoveryPaginationAndResourceRead(t *testing.T) {
	for _, args := range [][]string{
		{"tools", "list", "--json"},
		{"tools", "describe", "starcat.future_write", "--json"},
		{"resources", "list", "--json"},
		{"resources", "read", "starcat://tags"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			runner, _, cleanup := capabilityTestRunner(t, true, false)
			defer cleanup()
			if err := runner.Run(context.Background(), args); err != nil {
				t.Fatal(err)
			}
			output := runner.Stdout.(*bytes.Buffer).Bytes()
			if !json.Valid(output) {
				t.Fatalf("invalid JSON: %s", output)
			}
			if args[1] == "list" {
				var items []any
				if err := json.Unmarshal(output, &items); err != nil || len(items) != 2 {
					t.Fatalf("pagination lost descriptors: %s", output)
				}
			}
		})
	}
	runner, _, cleanup := capabilityTestRunner(t, true, true)
	defer cleanup()
	if err := runner.Run(context.Background(), []string{"tools", "list"}); err == nil || !strings.Contains(err.Error(), "pagination cursor") {
		t.Fatalf("repeated cursor error = %v", err)
	}
}

func TestDynamicInputFileAndStatisticsJSON(t *testing.T) {
	runner, captured, cleanup := capabilityTestRunner(t, true, false)
	defer cleanup()
	filename := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(filename, []byte(`{"content":"from file"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runner.Run(context.Background(), []string{"tools", "call", "starcat.future_write", "--input", filename}); err != nil {
		t.Fatal(err)
	}
	captured.Lock()
	if captured.arguments[0]["content"] != "from file" {
		t.Fatal("file input lost")
	}
	captured.Unlock()
	for _, args := range [][]string{{"stats", "--json"}, {"stats", "ai", "--range", "7d", "--json"}, {"stats", "knowledge", "--json"}} {
		statsRunner, closeServer := newStatisticsRunner(t)
		var stdout bytes.Buffer
		statsRunner.Stdout = &stdout
		if err := statsRunner.Run(context.Background(), args); err != nil {
			closeServer()
			t.Fatal(err)
		}
		closeServer()
		if !json.Valid(stdout.Bytes()) {
			t.Fatalf("statistics is not JSON: %s", stdout.String())
		}
	}
}

type capabilityCapturedCalls struct {
	sync.Mutex
	arguments []map[string]any
}

// capabilityTestRunner 宣告测试专用未来工具，证明 CLI 不依赖编译期工具映射。
func capabilityTestRunner(t *testing.T, preview, repeatedCursor bool) (*Runner, *capabilityCapturedCalls, func()) {
	t.Helper()
	captured := &capabilityCapturedCalls{}
	properties := map[string]any{
		"content": map[string]any{"type": "string"},
		"limit":   map[string]any{"type": "integer", "minimum": 1, "maximum": 100},
	}
	if preview {
		properties["dry_run"] = map[string]any{"type": "boolean"}
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var message map[string]any
		if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
			t.Error(err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		params, _ := message["params"].(map[string]any)
		switch message["method"] {
		case "initialize":
			writeMCPResult(writer, message["id"], map[string]any{"protocolVersion": "2025-03-26"})
		case "notifications/initialized":
			writer.WriteHeader(http.StatusAccepted)
		case "tools/list", "resources/list":
			field := "tools"
			descriptor := map[string]any{
				"name": "starcat.future_write", "inputSchema": map[string]any{"type": "object", "properties": properties},
				"annotations": map[string]any{"readOnlyHint": false},
			}
			if message["method"] == "resources/list" {
				field = "resources"
				descriptor = map[string]any{"name": "tags", "uri": "starcat://tags", "mimeType": "application/json"}
			}
			result := map[string]any{field: []any{descriptor}}
			if params["cursor"] == nil || repeatedCursor {
				result["nextCursor"] = "page-two"
			}
			writeMCPResult(writer, message["id"], result)
		case "tools/call":
			arguments, _ := params["arguments"].(map[string]any)
			captured.Lock()
			captured.arguments = append(captured.arguments, arguments)
			captured.Unlock()
			writeMCPToolResult(writer, message["id"], map[string]any{"ok": true})
		case "resources/read":
			writeMCPResult(writer, message["id"], map[string]any{"contents": []any{
				map[string]any{"uri": params["uri"], "mimeType": "application/json", "text": "[]"},
			}})
		default:
			t.Errorf("unexpected method: %v", message["method"])
			writer.WriteHeader(http.StatusBadRequest)
		}
	}))
	runner := &Runner{
		Profiles: staticProfileStore{profile: config.Profile{
			Endpoint: server.URL + "/mcp", DeviceID: "fixture", ProtocolVersion: config.CurrentProtocolVersion,
		}},
		Credentials: staticCredentialStore{token: "fixture-token"},
		Stdin:       strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
	}
	return runner, captured, server.Close
}
