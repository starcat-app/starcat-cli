// 浏览器原生宿主与普通 MCP 入口分离，stdout 只允许长度前缀 JSON。
package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"

	"github.com/starcat-app/starcat-cli/internal/browserhost"
)

const browserHostUsage = `Usage: starcat browser-host CHROME_EXTENSION_ORIGIN
       starcat browser-host install --extension-id ID

Chrome invokes the first form using native messaging, not MCP JSON-RPC.
Installation is explicit, macOS Chrome only, and requires a stable CLI path.
No credentials are written to CLI profiles. Safari keeps its Companion transport.
`

func (r *Runner) runBrowserHost(ctx context.Context, args []string) error {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "help") {
		return r.writeText(browserHostUsage)
	}
	if len(args) == 3 && args[0] == "install" && args[1] == "--extension-id" {
		if runtime.GOOS != "darwin" {
			return errors.New("native host installation currently supports macOS Chrome")
		}
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		path, err := browserhost.Install(filepath.Join(home, "Library/Application Support/Google/Chrome/NativeMessagingHosts"), executable, args[2])
		if err != nil {
			return err
		}
		return r.writeJSON(map[string]any{"installed": true, "manifest": path, "host": browserhost.Name})
	}
	if len(args) != 1 {
		return errors.New(browserHostUsage)
	}
	return (browserhost.Host{}).Serve(ctx, r.Stdin, r.Stdout, args[0])
}
