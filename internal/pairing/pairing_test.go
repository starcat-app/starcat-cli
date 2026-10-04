package pairing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/starcat-app/starcat-cli/internal/config"
	"github.com/starcat-app/starcat-cli/internal/mcp"
)

func TestParseInvitation(t *testing.T) {
	endpoint := "https://starcat-mac.local:5551/mcp"
	fingerprint := strings.Repeat("ab", 32)
	secret := strings.Repeat("s", 32)
	raw := "starcat-pair://connect?v=1&endpoint=" + url.QueryEscape(endpoint) +
		"&fingerprint=" + fingerprint + "&secret=" + secret

	got, err := ParseInvitation(raw)
	if err != nil {
		t.Fatalf("ParseInvitation() error = %v", err)
	}
	if got.Endpoint != endpoint || got.Fingerprint != fingerprint || got.Secret != secret {
		t.Fatalf("ParseInvitation() = %#v", got)
	}
}

func TestParseInvitationRejectsShortSecret(t *testing.T) {
	raw := "starcat-pair://connect?v=1&endpoint=http%3A%2F%2F127.0.0.1%3A5551%2Fmcp&secret=short"
	if _, err := ParseInvitation(raw); err == nil {
		t.Fatal("ParseInvitation() should reject a short one-time secret")
	}
}

func TestServicePairExchangesAndStoresDeviceCredential(t *testing.T) {
	secret := strings.Repeat("s", 32)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/pairing/exchange" {
			http.NotFound(writer, request)
			return
		}
		var body exchangeRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Secret != secret || body.Platform == "" || body.Architecture == "" {
			t.Fatalf("unexpected exchange request: %#v", body)
		}
		_ = json.NewEncoder(writer).Encode(exchangeResponse{
			DeviceID:        "device-1",
			Token:           "device-token",
			AppVersion:      "1.0.0",
			ProtocolVersion: config.CurrentProtocolVersion,
		})
	}))
	defer server.Close()

	profiles := &memoryProfileStore{}
	credentials := &memoryCredentialStore{values: map[string]string{}}
	invitation := "starcat-pair://connect?v=1&endpoint=" + url.QueryEscape(server.URL+"/mcp") + "&secret=" + secret
	profile, err := (Service{Profiles: profiles, Credentials: credentials}).Pair(context.Background(), invitation)
	if err != nil {
		t.Fatalf("Pair() error = %v", err)
	}
	if profile.DeviceID != "device-1" || profiles.profile.DeviceID != "device-1" {
		t.Fatalf("stored profile = %#v", profiles.profile)
	}
	if credentials.values["device-1"] != "device-token" {
		t.Fatalf("stored token = %q", credentials.values["device-1"])
	}
}

// 覆盖用户的 DNS 故障链：本机配对成功必须保存 loopback；证书不同则不发 secret、不存凭据。
func TestServicePairLocalFallbackStoresReachableEndpoint(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "same certificate", true: "different certificate"}[mismatch], func(t *testing.T) {
			var exchanges atomic.Int64
			server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/pairing/exchange" {
					exchanges.Add(1)
					var body exchangeRequest
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil || body.Secret != strings.Repeat("s", 32) {
						t.Errorf("invalid exchange body: %v", err)
					}
					_ = json.NewEncoder(writer).Encode(exchangeResponse{
						DeviceID: "device-1", Token: "device-token", AppVersion: "1.8.0", ProtocolVersion: config.CurrentProtocolVersion,
					})
					return
				}
				if request.URL.Path != "/mcp" || request.Header.Get("Authorization") != "Bearer device-token" {
					t.Errorf("invalid MCP request: %s", request.URL.Path)
				}
				var message map[string]any
				_ = json.NewDecoder(request.Body).Decode(&message)
				result := map[string]any{}
				if message["method"] == "tools/list" {
					result["tools"] = []map[string]any{{"name": "starcat.get_capabilities"}}
				}
				_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": message["id"], "result": result})
			}))
			defer server.Close()
			serverURL, _ := url.Parse(server.URL)
			originalTransport := http.DefaultTransport
			base := originalTransport.(*http.Transport).Clone()
			base.Proxy = nil
			base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
				if strings.HasPrefix(address, "starcat.invalid:") {
					return nil, &net.DNSError{Name: "starcat.invalid", Err: "no such host", IsNotFound: true}
				}
				return (&net.Dialer{}).DialContext(ctx, network, address)
			}
			// Service 使用生产 HTTP client；仅替换默认 dialer，并在子测试结束时立即恢复。
			http.DefaultTransport = base
			t.Cleanup(func() { http.DefaultTransport = originalTransport })
			digest := sha256.Sum256(server.Certificate().Raw)
			fingerprint := hex.EncodeToString(digest[:])
			if mismatch {
				fingerprint = strings.Repeat("ab", 32)
			}
			endpoint := "https://starcat.invalid:" + serverURL.Port() + "/mcp"
			invitation := "starcat-pair://connect?v=1&endpoint=" + url.QueryEscape(endpoint) + "&fingerprint=" + fingerprint + "&secret=" + strings.Repeat("s", 32)
			profiles := &memoryProfileStore{}
			credentials := &memoryCredentialStore{values: map[string]string{}}
			profile, err := (Service{Profiles: profiles, Credentials: credentials}).Pair(context.Background(), invitation)
			if mismatch {
				if !errors.Is(err, mcp.ErrCertificateMismatch) || exchanges.Load() != 0 || profiles.profile.DeviceID != "" || len(credentials.values) != 0 {
					t.Fatalf("mismatch must not exchange or store credentials: error=%v, exchanges=%d", err, exchanges.Load())
				}
				return
			}
			if err != nil || profile.Endpoint != server.URL+"/mcp" || profiles.profile.Endpoint != profile.Endpoint || exchanges.Load() != 1 {
				t.Fatalf("Pair() endpoint=%s, error=%v, exchanges=%d", profile.Endpoint, err, exchanges.Load())
			}
			transport, err := mcp.NewHTTPTransport(profiles.profile, credentials.values[profile.DeviceID])
			if err != nil {
				t.Fatal(err)
			}
			tools, err := mcp.NewClient(transport).ListTools(context.Background())
			if err != nil || len(tools) != 1 {
				t.Fatalf("MCP after fallback pairing: tools=%d, error=%v", len(tools), err)
			}
		})
	}
}

type memoryProfileStore struct {
	profile config.Profile
}

func (s *memoryProfileStore) Load() (config.Profile, error) { return s.profile, nil }
func (s *memoryProfileStore) Save(profile config.Profile) error {
	s.profile = profile
	return nil
}
func (s *memoryProfileStore) Delete() error { return nil }

type memoryCredentialStore struct {
	values map[string]string
}

func (s *memoryCredentialStore) Get(deviceID string) (string, error) { return s.values[deviceID], nil }
func (s *memoryCredentialStore) Set(deviceID, token string) error {
	s.values[deviceID] = token
	return nil
}
func (s *memoryCredentialStore) Delete(deviceID string) error {
	delete(s.values, deviceID)
	return nil
}
