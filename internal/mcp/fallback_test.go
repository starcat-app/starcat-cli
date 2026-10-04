// 验证 DNS 失败发生在请求发送前时，CLI 能安全尝试本机，并保持证书 pin 和单次写入边界。
package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// DNS 失败后仅本机收到一次完整 body，且实际地址随 response 返回给配对存储层。
func TestHTTPClientFallsBackOnEndpointDNSError(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		body, err := io.ReadAll(request.Body)
		if err != nil || string(body) != "one-time-secret" {
			t.Errorf("fallback body = %q, error = %v", body, err)
		}
		if !strings.HasPrefix(request.Host, "127.0.0.1:") || request.URL.Path != "/pairing/exchange" {
			t.Errorf("fallback request = %s %s", request.Host, request.URL.Path)
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client, endpoint, attempts := fallbackTestClient(t, server, testFingerprint(server), endpointDNSError())
	response, err := client.Post(strings.TrimSuffix(endpoint, "/mcp")+"/pairing/exchange", "text/plain", strings.NewReader("one-time-secret"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.Request.URL.String() != server.URL+"/pairing/exchange" || requests.Load() != 1 || attempts.Load() != 2 {
		t.Fatalf("actual endpoint = %s, requests = %d, attempts = %d", response.Request.URL, requests.Load(), attempts.Load())
	}
}

// 本机兜底也必须 pin 同一张证书，TLS 拒绝时不得发送 secret/token 或丢失错误类型。
func TestHTTPTransportLocalFallbackStillPinsCertificate(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	client, endpoint, attempts := fallbackTestClient(t, server, strings.Repeat("ab", 32), endpointDNSError())
	transport := &HTTPTransport{endpoint: endpoint, token: "test-token", client: client}
	_, _, err := transport.Send(context.Background(), []byte(`{"jsonrpc":"2.0"}`))
	if !errors.Is(err, ErrCertificateMismatch) || !strings.Contains(err.Error(), "re-pair with `starcat pair`") {
		t.Fatalf("Send() error = %v, want typed certificate mismatch and re-pair hint", err)
	}
	if requests.Load() != 0 || attempts.Load() != 2 {
		t.Fatalf("requests = %d, attempts = %d", requests.Load(), attempts.Load())
	}
}

// 连接拒绝、超时、代理 DNS 和远端证书变化均不能被当成目标主机的 DNS 失败。
func TestHTTPClientDoesNotFallbackForOtherFailures(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		t.Error("a failed connection must not send a request")
	}))
	defer server.Close()
	cases := []struct {
		name        string
		primary     error
		fingerprint string
	}{
		{"connection refused", errors.New("connection refused"), testFingerprint(server)},
		{"deadline", context.DeadlineExceeded, testFingerprint(server)},
		{"proxy DNS", &net.DNSError{Name: "proxy.invalid", Err: "no such host", IsNotFound: true}, testFingerprint(server)},
		{"certificate changed", nil, strings.Repeat("ab", 32)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			client, endpoint, attempts := fallbackTestClient(t, server, testCase.fingerprint, testCase.primary)
			_, err := client.Post(endpoint, "application/json", strings.NewReader("secret"))
			if err == nil || attempts.Load() != 1 {
				t.Fatalf("error = %v, attempts = %d; want failure without fallback", err, attempts.Load())
			}
		})
	}
}

// App 已经收到请求后的 HTTP 错误不能触发兜底，以免重复兑换或重放业务写入。
func TestHTTPClientDoesNotReplaySubmittedRequest(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, request.Body)
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client, endpoint, attempts := fallbackTestClient(t, server, testFingerprint(server), nil)
	response, err := client.Post(endpoint, "application/json", strings.NewReader("secret"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable || requests.Load() != 1 || attempts.Load() != 1 {
		t.Fatalf("status = %d, requests = %d, attempts = %d", response.StatusCode, requests.Load(), attempts.Load())
	}
}

// 服务已经读到一次性 secret 后断开连接时，EOF 也不能触发第二次请求。
func TestHTTPClientDoesNotReplayAfterConnectionCloses(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		_, _ = io.Copy(io.Discard, request.Body)
		connection, _, err := writer.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	client, endpoint, attempts := fallbackTestClient(t, server, testFingerprint(server), nil)
	_, err := client.Post(endpoint, "application/json", strings.NewReader("secret"))
	if err == nil || requests.Load() != 1 || attempts.Load() != 1 {
		t.Fatalf("error = %v, requests = %d, attempts = %d", err, requests.Load(), attempts.Load())
	}
}

// 两个地址都失败时保留两次失败原因，用户能区分 DNS 故障和本机服务未启动。
func TestHTTPClientReportsBothConnectionFailures(t *testing.T) {
	client, err := NewHTTPClient("https://starcat.invalid:5555/mcp", strings.Repeat("ab", 32), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	base := client.Transport.(*loopbackFallbackTransport).transport
	base.Proxy = nil
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if strings.HasPrefix(address, "starcat.invalid:") {
			return nil, endpointDNSError()
		}
		return nil, errors.New("local service is not listening")
	}
	_, err = client.Get("https://starcat.invalid:5555/mcp")
	if err == nil || !strings.Contains(err.Error(), "no such host") || !strings.Contains(err.Error(), "local service is not listening") {
		t.Fatalf("error = %v, want both connection failures", err)
	}
}

// 用可控 DialContext 模拟 DNS，避免测试依赖公网解析器、/etc/hosts 或代理环境。
func fallbackTestClient(t *testing.T, server *httptest.Server, fingerprint string, primaryError error) (*http.Client, string, *atomic.Int64) {
	t.Helper()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	endpoint := "https://starcat.invalid:" + serverURL.Port() + "/mcp"
	client, err := NewHTTPClient(endpoint, fingerprint, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.CloseIdleConnections)
	base := client.Transport.(*loopbackFallbackTransport).transport
	base.Proxy = nil
	var attempts atomic.Int64
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		attempts.Add(1)
		if strings.HasPrefix(address, "starcat.invalid:") && primaryError != nil {
			return nil, primaryError
		}
		return (&net.Dialer{}).DialContext(ctx, network, serverURL.Host)
	}
	return client, endpoint, &attempts
}

// 将故障绑定到 endpoint 名称，使代理 DNS 失败不会被错误兜底。
func endpointDNSError() error {
	return &net.DNSError{Name: "starcat.invalid", Err: "no such host", IsNotFound: true}
}

// 使用测试 listener 的真实证书，避免通过关闭 pinning 来构造成功场景。
func testFingerprint(server *httptest.Server) string {
	digest := sha256.Sum256(server.Certificate().Raw)
	return hex.EncodeToString(digest[:])
}
