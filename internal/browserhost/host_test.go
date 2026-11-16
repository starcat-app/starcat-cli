// 原生 framing 与 loopback 路由边界测试；不安装宿主，不接触用户浏览器或凭据。
package browserhost

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const testOrigin = "chrome-extension://aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/"

// discoveryTransport 在进程内记录探测，避免测试触达用户正在运行的本地服务。
type discoveryTransport func(*http.Request) (*http.Response, error)

func (transport discoveryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func invoke(t *testing.T, host Host, request Request) map[string]any {
	t.Helper()
	var input, output bytes.Buffer
	if err := writeMessage(&input, request); err != nil {
		t.Fatal(err)
	}
	if err := host.Serve(context.Background(), &input, &output, testOrigin); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := readMessage(&output, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestProxyPreservesBodyOriginAndCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Origin") != strings.TrimSuffix(testOrigin, "/") ||
			request.Header.Get("Authorization") != "Bearer fixture-credential" ||
			request.URL.Path != "/plugin/v1/notes" || request.Method != "PATCH" {
			t.Error("request semantics changed")
		}
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"content":"你好"}` {
			t.Errorf("body changed: %s", body)
		}
		_, _ = writer.Write([]byte(`{"saved":true}`))
	}))
	defer server.Close()
	result := invoke(t, Host{}, Request{Action: "request", ServiceURL: server.URL, Path: "/plugin/v1/notes", Method: "PATCH", Body: json.RawMessage(`{"content":"你好"}`), Token: strings.Join([]string{"fixture", "credential"}, "-")})
	if result["ok"] != true || result["body"].(map[string]any)["saved"] != true {
		t.Fatal(result)
	}
}

// 默认配置只探测一个端口；保存的自定义地址保留，且与默认地址去重。
func TestDiscoveryOnlyProbesSavedAddressAndDefault(t *testing.T) {
	const defaultURL = "http://127.0.0.1:5051"
	for _, test := range []struct {
		name       string
		serviceURL string
		expected   []string
	}{
		{name: "default", expected: []string{defaultURL}},
		{name: "saved default", serviceURL: defaultURL, expected: []string{defaultURL}},
		{name: "saved custom", serviceURL: "http://127.0.0.1:5444", expected: []string{"http://127.0.0.1:5444", defaultURL}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var probed []string
			host := Host{Client: &http.Client{Transport: discoveryTransport(func(request *http.Request) (*http.Response, error) {
				probed = append(probed, request.URL.Scheme+"://"+request.URL.Host)
				if request.Method != http.MethodGet || request.URL.Path != "/plugin/v1/discover" ||
					request.Header.Get("Authorization") != "" ||
					request.Header.Get("Origin") != strings.TrimSuffix(testOrigin, "/") {
					t.Fatal("discovery request semantics changed")
				}
				body, err := json.Marshal(map[string]string{
					"product": "starcat", "protocol_version": "1", "pairing": "numeric_comparison",
					"instance_id": request.URL.Host, "channel": "appstore",
				})
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
			})}}
			result := invoke(t, host, Request{Action: "discover", ServiceURL: test.serviceURL})
			if !reflect.DeepEqual(probed, test.expected) {
				t.Fatalf("probed %v, want %v", probed, test.expected)
			}
			candidates := result["body"].(map[string]any)["candidates"].([]any)
			if result["ok"] != true || len(candidates) != len(test.expected) {
				t.Fatal(result)
			}
		})
	}
}

func TestRejectsArbitraryNetworkAndRoutes(t *testing.T) {
	for _, request := range []Request{
		{ServiceURL: "https://example.com:443", Path: "/plugin/v1/ping", Method: "GET"},
		{ServiceURL: "http://user@127.0.0.1:5051", Path: "/plugin/v1/ping", Method: "GET"},
		{ServiceURL: "http://127.0.0.1:5051", Path: "//example.com/plugin/v1/ping", Method: "GET"},
		{ServiceURL: "http://127.0.0.1:5051", Path: "/mcp", Method: "POST"},
	} {
		if _, err := (Host{}).request(context.Background(), request, strings.TrimSuffix(testOrigin, "/")); err == nil {
			t.Fatalf("accepted %+v", request)
		}
	}
	var framed bytes.Buffer
	_ = writeMessage(&framed, Request{})
	if err := (Host{}).Serve(context.Background(), &framed, io.Discard, "https://github.com"); err == nil {
		t.Fatal("accepted webpage origin")
	}
}

func TestFramingBoundsAndEvents(t *testing.T) {
	for _, length := range []uint32{0, maxMessageBytes + 1} {
		var frame bytes.Buffer
		_ = binary.Write(&frame, binary.NativeEndian, length)
		if err := readMessage(&frame, new(Request)); err == nil {
			t.Fatal("accepted invalid frame")
		}
	}
	var output bytes.Buffer
	if err := forwardEvents(strings.NewReader(": heartbeat\r\n\r\nevent: notes\r\ndata: {\"repo_id\":7}\r\n\r\n"), &output); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := readMessage(&output, &result); err != nil {
		t.Fatal(err)
	}
	event := result["event"].(map[string]any)
	if event["type"] != "notes" || event["data"].(map[string]any)["repo_id"] != float64(7) {
		t.Fatal(result)
	}
}

func TestInstallOnlyGrantsRequestedExtension(t *testing.T) {
	directory := t.TempDir()
	path, err := Install(directory, "/tmp/Starcat CLI's/starcat", strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]any
	if err := json.Unmarshal(body, &manifest); err != nil {
		t.Fatal(err)
	}
	origins := manifest["allowed_origins"].([]any)
	if len(origins) != 1 || origins[0] != testOrigin {
		t.Fatal(manifest)
	}
	script, _ := os.ReadFile(filepath.Join(directory, Name+".sh"))
	if !strings.Contains(string(script), "'\\''") || !strings.Contains(string(script), `browser-host "$@"`) {
		t.Fatal("executable path is not shell quoted")
	}
	if _, err := Install(directory, "/tmp/starcat", "*"); err == nil {
		t.Fatal("wildcard extension accepted")
	}
}
