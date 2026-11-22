package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProfileValidateAllowsLoopbackHTTP(t *testing.T) {
	profile := Profile{
		Endpoint:        "http://127.0.0.1:5551/mcp",
		DeviceID:        "device-1",
		ProtocolVersion: CurrentProtocolVersion,
	}
	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestProfileValidateAllowsRemoteHTTP(t *testing.T) {
	profile := Profile{
		Endpoint:        "http://192.168.1.10:5551/mcp",
		DeviceID:        "device-1",
		ProtocolVersion: CurrentProtocolVersion,
	}
	if err := profile.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestLegacyProfileDropsCertificateAfterHTTPSchemeMigration(t *testing.T) {
	store := FileStore{Path: filepath.Join(t.TempDir(), "profile.json")}
	payload := `{"endpoint":"https://mac.local:5555/mcp","certificate_sha256":"old","device_id":"device-1","app_version":"2.0.0","protocol_version":"1","paired_at":"2026-11-22T00:00:00Z"}`
	if err := os.WriteFile(store.Path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	original, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	migrated, changed := original.HTTPVersion()
	if want := "http://mac.local:5555/mcp"; !changed || migrated.Endpoint != want {
		t.Fatalf("HTTPVersion() = %#v, %v, want %s", migrated, changed, want)
	}
	original.Endpoint = migrated.Endpoint
	if original != migrated {
		t.Fatalf("HTTPVersion() changed pairing data: %#v", migrated)
	}
	if err := store.Save(migrated); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(saved), "certificate_sha256") {
		t.Fatal("saved profile still contains a certificate fingerprint")
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	store := FileStore{Path: filepath.Join(t.TempDir(), "nested", "profile.json")}
	want := Profile{
		Endpoint:        "http://localhost:5551/mcp",
		DeviceID:        "device-1",
		AppVersion:      "1.0.0",
		ProtocolVersion: CurrentProtocolVersion,
		PairedAt:        time.Unix(1_700_000_000, 0).UTC(),
	}
	if err := store.Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !got.PairedAt.Equal(want.PairedAt) || got.Endpoint != want.Endpoint || got.DeviceID != want.DeviceID {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}
