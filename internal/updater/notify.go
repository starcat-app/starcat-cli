package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

const checkInterval = 24 * time.Hour

type checkState struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

// MaybeNotify checks at most once per day and only writes to an interactive stderr.
// MCP stdio and automated pipelines are intentionally excluded from update chatter.
func MaybeNotify(ctx context.Context, currentVersion string, args []string, stderr *os.File) {
	if os.Getenv("STARCAT_NO_UPDATE_CHECK") != "" || currentVersion == "dev" || !semver.IsValid(currentVersion) {
		return
	}
	if stderr == nil || !isInteractiveFile(stderr) || skipNotification(args) {
		return
	}
	cachePath, err := defaultCheckPath()
	if err != nil {
		return
	}
	state, _ := loadCheckState(cachePath)
	if time.Since(state.CheckedAt) >= checkInterval {
		checkCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		release, checkErr := NewClient().Latest(checkCtx)
		cancel()
		state.CheckedAt = time.Now().UTC()
		if checkErr == nil {
			state.Latest = release.TagName
		}
		_ = saveCheckState(cachePath, state)
	}
	if !semver.IsValid(state.Latest) || semver.Compare(state.Latest, currentVersion) <= 0 {
		return
	}
	command := "starcat update"
	if executable, err := os.Executable(); err == nil {
		if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil && IsHomebrewPath(resolved) {
			command = "brew update && brew upgrade starcat"
		}
	}
	fmt.Fprintf(stderr, "A new Starcat CLI version is available: %s (current: %s). Run `%s`.\n", state.Latest, currentVersion, command)
}

func skipNotification(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "mcp", "browser-host", "update", "pair":
		return true
	default:
		return false
	}
}

func isInteractiveFile(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func defaultCheckPath() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "starcat", "update-check.json"), nil
}

func loadCheckState(path string) (checkState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return checkState{}, err
	}
	var state checkState
	if err := json.Unmarshal(data, &state); err != nil {
		return checkState{}, err
	}
	return state, nil
}

func saveCheckState(path string, state checkState) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// WriteNotification is a small test seam that keeps stdout free of update messages.
func WriteNotification(writer io.Writer, latest, current, command string) {
	fmt.Fprintf(writer, "A new Starcat CLI version is available: %s (current: %s). Run `%s`.\n", strings.TrimSpace(latest), strings.TrimSpace(current), command)
}
