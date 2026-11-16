# Starcat CLI

<!-- starcat-promo:start -->
<div align="center">
<a href="https://starcat.ink"><img src="https://raw.githubusercontent.com/starcat-app/starcat-site/main/assets/banner.webp" width="100%" alt="Starcat" /></a>

<p><strong>Cross-platform Starcat CLI and MCP bridge for AI agents.</strong></p>
<p>Starcat is a native macOS app that turns GitHub Stars into a searchable, organized and AI-assisted local knowledge base, with a broader ecosystem of desktop clients, plugins, CLI tools, and self-hostable services.</p>

<a href="https://github.com/starcat-app/homebrew-starcat"><img src="https://img.shields.io/badge/Install%20with-Homebrew-FBBF24?style=for-the-badge&logo=homebrew&logoColor=white" width="220" alt="Install with Homebrew"/></a>
<br/>
<sub><a href="./README-ZH.md">中文说明</a></sub>
</div>

<div align="center">
<a href="https://starcat.ink"><img src="https://img.shields.io/badge/website-starcat.ink-38BDF8?style=flat&color=blue" alt="website"/></a>
<a href="https://github.com/starcat-app/Starcat/issues"><img src="https://img.shields.io/github/issues/starcat-app/Starcat?style=flat&color=blue" alt="issues"/></a>
<a href="https://github.com/starcat-app/homebrew-starcat"><img src="https://img.shields.io/badge/install-homebrew-lightgrey.svg?style=flat&color=blue" alt="homebrew"/></a>
<a href="https://github.com/starcat-app/starcat-localization"><img src="https://img.shields.io/badge/localization-open-lightgrey.svg?style=flat&color=blue" alt="localization"/></a>
</div>

<div align="center">
<img width="900" src="https://raw.githubusercontent.com/starcat-app/starcat-site/main/assets/main.webp" alt="Starcat main window"/>
</div>

**Preferred install method:**

```bash
brew tap starcat-app/starcat
brew trust starcat-app/starcat
brew install --cask starcat
```

**Useful links:**

- Home and downloads: https://starcat.ink
- Mac App Store: search for Starcat for GitHub
- Public support: https://github.com/starcat-app/Starcat/issues · Release notes: https://starcat.ink/changelog.html
- Starcat App Homebrew tap: https://github.com/starcat-app/homebrew-starcat
- CLI / MCP: [starcat-cli](https://github.com/starcat-app/starcat-cli) / [Homebrew tap](https://github.com/starcat-app/homebrew-starcat-cli)
- AI Agent Skill: https://github.com/starcat-app/starcat-skill
- Browser plugins: [Chrome](https://github.com/starcat-app/starcat-chrome-plugin) / [Safari](https://github.com/starcat-app/starcat-safari-plugin)
- Launcher integrations: [Alfred](https://github.com/starcat-app/starcat-alfred-workflow) / [uTools](https://github.com/starcat-app/starcat-utools-plugin) / [Raycast](https://github.com/starcat-app/starcat-raycast-extension)
- Documentation: https://github.com/starcat-app/starcat-docs
- Website source: https://github.com/starcat-app/starcat-site
- Localization: https://github.com/starcat-app/starcat-localization

**Self-hostable support APIs:**

- [starcat-sharing-api](https://github.com/starcat-app/starcat-sharing-api)
- [starcat-trending-api](https://github.com/starcat-app/starcat-trending-api)
- [starcat-weekly-api](https://github.com/starcat-app/starcat-weekly-api)
- [starcat-wiki-api](https://github.com/starcat-app/starcat-wiki-api)
- [starcat-recommend-api](https://github.com/starcat-app/starcat-recommend-api)
- [starcat-discovery-api](https://github.com/starcat-app/starcat-discovery-api)
<!-- starcat-promo:end -->

[![CI](https://github.com/starcat-app/starcat-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/starcat-app/starcat-cli/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](./LICENSE)

`starcat` is the cross-platform command-line client for Starcat and a stdio MCP server for AI agents such as Codex and Claude Code.

The CLI never reads the Starcat database directly and does not duplicate application business logic. Every read and write goes through Starcat MCP tools, so permissions, dry-run behavior, Pro entitlement checks, and audit logging remain enforced by the Starcat app.

[中文说明](./README-ZH.md)

## Supported platforms

- macOS: `arm64`, `amd64`
- Linux: `arm64`, `amd64`
- Windows: `amd64`

The Starcat app still runs on macOS. The CLI may run on the same Mac or connect from a trusted LAN, Tailscale, or WireGuard device.

## Install

### Homebrew

```bash
brew tap starcat-app/starcat-cli
brew install starcat
```

The tap repository is `starcat-app/homebrew-starcat-cli`; the installed command is `starcat`.

### macOS and Linux install script

```bash
curl -fsSL https://github.com/starcat-app/starcat-cli/releases/latest/download/install.sh | sh
```

The default destination is `~/.local/bin/starcat`. Override it when needed:

```bash
curl -fsSL https://github.com/starcat-app/starcat-cli/releases/latest/download/install.sh \
  | STARCAT_INSTALL_DIR=/custom/bin sh
```

### Windows PowerShell

```powershell
irm https://github.com/starcat-app/starcat-cli/releases/latest/download/install.ps1 | iex
```

The default destination is `$HOME\.local\bin\starcat.exe`.

Every installer reports platform detection, download, checksum verification, and installation progress. Assets come from GitHub Releases and are verified against `checksums.txt` before installation. The completion message includes PATH guidance, pairing steps, and common commands.

## Pair with Starcat

In Starcat, open **Settings > MCP Service**, start the service, and click **Copy Pairing Command**. Paste the complete command into the target device's terminal and press Enter, then approve the device in Starcat:

```bash
starcat pair "starcat-pair://connect?..."
starcat doctor
```

For manual entry, run `starcat pair`, paste the URI, and press Enter. Pairing commands expire after five minutes, can only be redeemed once, and still require approval inside Starcat. Long-lived device credentials are stored in macOS Keychain, Windows Credential Manager, or Linux Secret Service.

## Configure an MCP client

After pairing, configure the AI agent with a user-level MCP server:

```json
{
  "command": "/absolute/path/to/starcat",
  "args": ["mcp"]
}
```

`starcat mcp` bridges line-delimited JSON-RPC between stdio and Starcat MCP Streamable HTTP. Protocol messages are written only to stdout; diagnostics are written to stderr.

## Commands

```bash
starcat help
starcat capabilities
starcat stats
starcat stats ai --range 30d
starcat stats knowledge
starcat search "local RAG" --source all --limit 30
starcat repo search "local RAG" --scope starred --limit 20
starcat repo context owner/repo
starcat repo readme owner/repo
starcat repo summary owner/repo
starcat tags list
```

`starcat search` is the stable JSON entry point for external launchers such as
Alfred. It calls `starcat.global_search_repos`, merges Starcat local FTS and
GitHub Search results, preserves source labels, and returns a safe `open_url`.
The existing `starcat repo search` command remains local-only unless semantic
search is explicitly requested.

Launcher adapters should validate against the shared fixtures in
[`contracts/global-search`](./contracts/global-search/README.md). This directory
is the v1 contract source for schema compatibility, partial-provider warnings,
empty results, and stable error codes.

`help`, `version`, `pair`, `unpair`, `doctor`, and `update` use terminal-friendly output. Statistics also support `--json`; `capabilities`, `repo`, `tags`, `tools call`, and `resources` provide structured output for shell-based agents.

Statistics are read-only local aggregates. `starcat stats` shows the common Star, knowledge-base, AI token, and RAG chunk counts; `stats ai` supports `--range`, `--feature`, `--provider`, and `--model`; `stats knowledge` shows source coverage and index health.

Write operations are dry-run by default and require `--apply` to persist changes:

```bash
printf '%s' 'A private note' | starcat repo note set owner/repo
printf '%s' 'A private note' | starcat repo note set owner/repo --apply
starcat repo tags add owner/repo Swift macOS --apply
starcat repo status set owner/repo using --apply
```

## Dynamic tools and resources

Both ordinary CLI commands and `starcat mcp` use the app's public registry. Discover the actual connected app's tools, schemas, permissions, and contract version before calling them:

```bash
starcat tools list --json
starcat tools describe starcat.get_repo
printf '%s' '{"owner":"owner","name":"repo"}' | starcat tools call starcat.get_repo --input -
starcat resources list
starcat resources read starcat://capabilities
starcat stats --json
```

`--input` accepts one JSON object from a file or stdin, up to 8 MiB. For tools supporting preview, the CLI enforces `dry_run=true` unless `--apply` is present, even if the input contains `false`. Side-effecting tools without preview also require `--apply`. The app remains responsible for final authorization.

The expanded registry includes cached health, security, releases, notifications, discovery sources, scoped knowledge evidence, exports, and explicit AI/background tasks. Follow returned task IDs instead of replaying timed-out writes. Task results expire after one hour and do not survive app restarts or account changes. Exported content belongs to the CLI caller and can be redirected to a local file; the app does not open caller-provided paths.

App server `0.6.0`, public contract version `2`, defaults omitted `dry_run` to preview. MCP clients previously relying on omission to execute must now preview and explicitly send `false` after authorization. See [the public capability contract](docs/public-capabilities-contract.md) for migration and error semantics.

## Chrome native bridge on macOS

Install the user-level host for the exact installed extension ID:

```bash
starcat browser-host install --extension-id <chrome-extension-id>
```

The installer writes a Chrome host manifest and launcher under your user profile. Re-run it after moving or upgrading the CLI executable. To uninstall, remove `com.starcat.browser.json` and `com.starcat.browser.sh` from `~/Library/Application Support/Google/Chrome/NativeMessagingHosts/`.

Chrome can then proxy Companion requests and event streams through the CLI. Browser authorization remains an independent Companion device token; this does not reuse MCP credentials. The host uses length-prefixed native JSON, separately from MCP's line-delimited stdio. It probes the saved address and the single default loopback port `5051`; arbitrary custom ports still need an address. Safari currently retains its background HTTP transport. Installing, signing, and accepting the extensions in real browsers remain separate verification steps.

## Updates

The CLI checks GitHub Releases at most once every 24 hours and displays an English notice only in an interactive terminal. It never prints update notices in `starcat mcp`, `browser-host`, or automated pipelines.

Disable automatic checks:

```bash
export STARCAT_NO_UPDATE_CHECK=1
```

Update a script-installed binary:

```bash
starcat update
```

Homebrew installations remain managed by Homebrew:

```bash
brew update
brew upgrade starcat
```

## Security model

- Plaintext HTTP is restricted to loopback addresses.
- Remote connections require TLS 1.3 and the paired SHA-256 certificate fingerprint.
- Each device receives an independent, revocable token.
- Long-lived tokens are never written to command arguments, stdout, logs, or project files.
- Downloaded update archives must match the SHA-256 release manifest.
- Starcat MCP permissions remain the final authorization boundary.

See [SECURITY.md](./SECURITY.md) for vulnerability reporting and threat-model details.

## Development

Requires Go 1.25 or newer. The module pins the release toolchain to Go 1.26.11 or newer so published binaries include the current standard-library security fixes.

```bash
go mod verify
go test ./...
go test -race ./...
go vet ./...
go build -o bin/starcat ./cmd/starcat
```

Release builds inject a semantic version:

```bash
go build -ldflags "-X github.com/starcat-app/starcat-cli/internal/mcp.Version=v0.1.0" ./cmd/starcat
```

`scripts/build-all.sh v0.1.0` creates the five platform archives, installers, and `checksums.txt` under `dist/`. It does not create tags or publish a release.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) and [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md).

## License

MIT. Binary distributions also include [THIRD_PARTY_NOTICES.md](./THIRD_PARTY_NOTICES.md).
