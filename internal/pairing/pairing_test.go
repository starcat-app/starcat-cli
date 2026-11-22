package pairing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/starcat-app/starcat-cli/internal/config"
)

func TestParseInvitation(t *testing.T) {
	endpoint := "http://starcat-mac.local:5551/mcp"
	secret := strings.Repeat("s", 32)
	raw := "starcat-pair://connect?v=1&endpoint=" + url.QueryEscape(endpoint) + "&secret=" + secret

	got, err := ParseInvitation(raw)
	if err != nil {
		t.Fatalf("ParseInvitation() error = %v", err)
	}
	if got.Endpoint != endpoint || got.Secret != secret {
		t.Fatalf("ParseInvitation() = %#v", got)
	}
}

func TestParseInvitationRequiresNewCommandForLegacyHTTPS(t *testing.T) {
	raw := "starcat-pair://connect?v=1&endpoint=" + url.QueryEscape("https://starcat-mac.local:5551/mcp") +
		"&secret=" + strings.Repeat("s", 32)
	if _, err := ParseInvitation(raw); err == nil || !strings.Contains(err.Error(), "copy a new HTTP pairing command") {
		t.Fatalf("ParseInvitation() error = %v, want upgrade guidance", err)
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
