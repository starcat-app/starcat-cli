// Package config 管理 Starcat CLI 的非敏感连接配置。
// 长期访问凭据必须交给 credential.Store，禁止写入 profile.json。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const CurrentProtocolVersion = "1"

var ErrNotPaired = errors.New("Starcat CLI is not paired; run `starcat pair`")

// Profile 是 CLI 与一台 Starcat App 建立连接所需的非敏感资料。
type Profile struct {
	Endpoint        string    `json:"endpoint"`
	DeviceID        string    `json:"device_id"`
	AppVersion      string    `json:"app_version"`
	ProtocolVersion string    `json:"protocol_version"`
	PairedAt        time.Time `json:"paired_at"`
}

// Validate 允许读取旧 HTTPS profile，以便在连接成功后迁移。
func (p Profile) Validate() error {
	if p.ProtocolVersion != CurrentProtocolVersion {
		return fmt.Errorf("CLI/App protocol mismatch: CLI=%s, Starcat=%s", CurrentProtocolVersion, p.ProtocolVersion)
	}
	u, err := url.Parse(p.Endpoint)
	if err != nil || u.Hostname() == "" || u.EscapedPath() != "/mcp" ||
		u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return errors.New("invalid Starcat endpoint: path must be /mcp")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return errors.New("Starcat endpoint must use http or https")
	}
	return nil
}

// HTTPVersion 保留已配对的设备身份与连接端口，只迁移 URL scheme。
func (p Profile) HTTPVersion() (Profile, bool) {
	endpoint, err := url.Parse(p.Endpoint)
	if err != nil || endpoint.Scheme != "https" {
		return p, false
	}
	if endpoint.Port() == "" {
		endpoint.Host = net.JoinHostPort(endpoint.Hostname(), "443")
	}
	endpoint.Scheme = "http"
	p.Endpoint = endpoint.String()
	return p, true
}

// Store 抽象 profile 持久化，便于命令测试替换为内存实现。
type Store interface {
	Load() (Profile, error)
	Save(Profile) error
	Delete() error
}

// FileStore 将非敏感资料保存到用户配置目录，并在 Unix 上限制为当前用户可读写。
type FileStore struct {
	Path string
}

func DefaultFileStore() (FileStore, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return FileStore{}, fmt.Errorf("locate user configuration directory: %w", err)
	}
	return FileStore{Path: filepath.Join(root, "starcat", "profile.json")}, nil
}

func (s FileStore) Load() (Profile, error) {
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return Profile{}, ErrNotPaired
	}
	if err != nil {
		return Profile{}, fmt.Errorf("read connection profile: %w", err)
	}
	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return Profile{}, fmt.Errorf("decode connection profile: %w", err)
	}
	if err := profile.Validate(); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

func (s FileStore) Save(profile Profile) error {
	if err := profile.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create configuration directory: %w", err)
	}
	data, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return fmt.Errorf("encode connection profile: %w", err)
	}
	// CLI 与 MCP 可以同时迁移同一 profile，各自使用独立的临时文件再原子替换。
	temporary, err := os.CreateTemp(filepath.Dir(s.Path), ".profile-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary connection profile: %w", err)
	}
	defer os.Remove(temporary.Name())
	_, writeErr := temporary.Write(append(data, '\n'))
	closeErr := temporary.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return fmt.Errorf("write temporary connection profile: %w", err)
	}
	if err := os.Rename(temporary.Name(), s.Path); err != nil {
		return fmt.Errorf("save connection profile: %w", err)
	}
	return nil
}
func (s FileStore) Delete() error {
	err := os.Remove(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
