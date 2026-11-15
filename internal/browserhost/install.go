// Chrome 用户级宿主登记显式执行，不在更新、配对或 App 启动时暗中安装。
package browserhost

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Install 为指定扩展 ID 生成 macOS Chrome 宿主；可执行文件必须是稳定安装路径。
// 不复制二进制，因此 CLI 原地升级会沿用同一登记。卸载路径由调用方明确展示。
func Install(directory, executable, id string) (string, error) {
	if !extensionID.MatchString(id) {
		return "", errors.New("extension ID must contain 32 letters from a to p")
	}
	if !filepath.IsAbs(executable) || strings.ContainsAny(executable, "\r\n") {
		return "", errors.New("a stable absolute CLI executable path is required")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", err
	}
	script := filepath.Join(directory, Name+".sh")
	quoted := "'" + strings.ReplaceAll(executable, "'", "'\\''") + "'"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec "+quoted+" browser-host \"$@\"\n"), 0700); err != nil {
		return "", err
	}
	manifest, err := json.MarshalIndent(map[string]any{
		"name": Name, "description": "Starcat CLI browser bridge", "path": script,
		"type": "stdio", "allowed_origins": []string{"chrome-extension://" + id + "/"},
	}, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(directory, Name+".json")
	return path, os.WriteFile(path, append(manifest, '\n'), 0600)
}
