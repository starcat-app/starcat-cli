// Package pairing 实现一次性 URI 配对。
// URI 中的 secret 短时有效且只能兑换一次，长期设备 token 不进入命令行输出。
// Starcat 设置页可生成包含该短期 URI 的完整命令；即使命令进入 shell history，
// secret 也会在五分钟后过期或首次兑换后立即失效，最终签发仍需 App 内人工确认。
package pairing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/starcat-app/starcat-cli/internal/config"
	"github.com/starcat-app/starcat-cli/internal/credential"
	"github.com/starcat-app/starcat-cli/internal/mcp"
)

// Invitation 是 Starcat 生成的短期连接资料。
type Invitation struct {
	Endpoint string
	Secret   string
}

// ParseInvitation 解析当前版本由 Starcat 签发的 HTTP 配对邀请。
func ParseInvitation(raw string) (Invitation, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "starcat-pair" || u.Host != "connect" {
		return Invitation{}, errors.New("invalid pairing URI")
	}
	query := u.Query()
	if query.Get("v") != config.CurrentProtocolVersion {
		return Invitation{}, errors.New("pairing URI protocol version is not supported")
	}
	invitation := Invitation{
		Endpoint: query.Get("endpoint"),
		Secret:   query.Get("secret"),
	}
	if len(invitation.Secret) < 32 {
		return Invitation{}, errors.New("pairing URI contains an invalid one-time secret")
	}
	probe := config.Profile{
		Endpoint:        invitation.Endpoint,
		ProtocolVersion: config.CurrentProtocolVersion,
	}
	if err := probe.Validate(); err != nil {
		return Invitation{}, err
	}
	if _, legacy := probe.HTTPVersion(); legacy {
		return Invitation{}, errors.New("update Starcat and copy a new HTTP pairing command from MCP settings")
	}
	return invitation, nil
}

type exchangeRequest struct {
	Secret       string `json:"secret"`
	DeviceName   string `json:"device_name"`
	Platform     string `json:"platform"`
	Architecture string `json:"architecture"`
	CLIVersion   string `json:"cli_version"`
}

type exchangeResponse struct {
	DeviceID        string `json:"device_id"`
	Token           string `json:"token"`
	AppVersion      string `json:"app_version"`
	ProtocolVersion string `json:"protocol_version"`
}

// Service 完成邀请兑换，并分别保存非敏感 profile 与系统安全存储中的 token。
type Service struct {
	Profiles    config.Store
	Credentials credential.Store
}

func (s Service) Pair(ctx context.Context, rawURI string) (config.Profile, error) {
	invitation, err := ParseInvitation(rawURI)
	if err != nil {
		return config.Profile{}, err
	}
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		hostname = "unknown-device"
	}
	payload, err := json.Marshal(exchangeRequest{
		Secret:       invitation.Secret,
		DeviceName:   hostname,
		Platform:     runtime.GOOS,
		Architecture: runtime.GOARCH,
		CLIVersion:   mcp.Version,
	})
	if err != nil {
		return config.Profile{}, err
	}

	exchangeURL, err := pairingEndpoint(invitation.Endpoint)
	if err != nil {
		return config.Profile{}, err
	}
	client := mcp.NewHTTPClient(2 * time.Minute)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL, bytes.NewReader(payload))
	if err != nil {
		return config.Profile{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return config.Profile{}, fmt.Errorf("connect to Starcat pairing endpoint: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return config.Profile{}, err
	}
	if response.StatusCode != http.StatusOK {
		return config.Profile{}, fmt.Errorf("Starcat rejected pairing (HTTP %d): %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	var exchanged exchangeResponse
	if err := json.Unmarshal(data, &exchanged); err != nil {
		return config.Profile{}, fmt.Errorf("decode pairing response: %w", err)
	}
	if exchanged.DeviceID == "" || exchanged.Token == "" {
		return config.Profile{}, errors.New("Starcat pairing response is missing the device credential")
	}
	profile := config.Profile{
		Endpoint:        invitation.Endpoint,
		DeviceID:        exchanged.DeviceID,
		AppVersion:      exchanged.AppVersion,
		ProtocolVersion: exchanged.ProtocolVersion,
		PairedAt:        time.Now().UTC(),
	}
	if err := profile.Validate(); err != nil {
		return config.Profile{}, err
	}
	if err := s.Credentials.Set(profile.DeviceID, exchanged.Token); err != nil {
		return config.Profile{}, err
	}
	if err := s.Profiles.Save(profile); err != nil {
		_ = s.Credentials.Delete(profile.DeviceID)
		return config.Profile{}, err
	}
	return profile, nil
}

func pairingEndpoint(endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	u.Path = "/pairing/exchange"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}
