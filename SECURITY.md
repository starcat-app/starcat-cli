# Security Policy

## Reporting a vulnerability

Please report suspected vulnerabilities through [GitHub Security Advisories](https://github.com/starcat-app/starcat-cli/security/advisories/new). Do not publish credentials, pairing URIs, exploit details, or private Starcat data in a public issue.

Include the affected CLI version, operating system and architecture, reproduction steps, and the expected security impact. You should receive an acknowledgement within seven days.

## Supported versions

Security fixes are provided for the latest published stable release. Users should update with `starcat update` or, for Homebrew installations, `brew upgrade starcat`.

## Security boundaries

- The Starcat app is the authority for authentication, entitlements, MCP permissions, and audit logging.
- Pairing URIs are short-lived, single-use credentials and must be entered through stdin.
- Long-lived device tokens are stored only in the operating-system credential store.
- The MCP service listens on loopback by default. **Allow Other Devices** extends the HTTP listener to the Mac's network interfaces.
- Update archives are accepted only after SHA-256 verification against the same GitHub Release manifest.
- Update checks contact GitHub Releases at most once per day and can be disabled with `STARCAT_NO_UPDATE_CHECK=1`.

Local malware, a compromised operating-system credential store, a compromised GitHub account, and a compromised Starcat app installation are outside the CLI's protection boundary.
