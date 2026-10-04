# Changelog

All notable changes to Starcat CLI are documented here.

The project follows Semantic Versioning. GitHub Releases are the source of published binaries and release notes.

## Unreleased

## v1.1.1 - 2026-10-04

- Security: Updated the release Go toolchain to 1.26.6 to include the standard-library fixes for GO-2026-6218, GO-2026-6090, GO-2026-5972, and GO-2026-5026.
- Fixed pairing and MCP connections when the advertised host cannot be resolved by trying the same port on local loopback while preserving HTTPS and the paired certificate fingerprint.
- Fixed saved pairing profiles to retain the reachable local endpoint after a successful DNS fallback.
- Added clear re-pairing guidance when the Starcat TLS certificate changes, retaining strict fingerprint verification and the existing `CLI_NOT_PAIRED` launcher error code.
- Added versioned global-search contract fixtures for Alfred, uTools, Raycast,
  and future launcher adapters.

## v1.1.0 - 2026-07-29

- Added `starcat search` as a stable JSON entry point for Alfred and other external launchers.
- Added combined local and GitHub repository search with source labels and constrained open URLs.
- Added stable machine-readable error codes for pairing, MCP availability, Pro access, upgrades, and search failures.
- Removed the redundant machine-readable doctor output; automation uses structured MCP tools instead.
- Removed the redundant note-input marker; `repo note set` now always reads content from stdin.

## v1.0.0 - 2026-07-20

- Added verified cross-platform self-update support.
- Added daily interactive update notifications with an opt-out.
- Added macOS/Linux and Windows one-line installers.
- Added staged installer progress, PATH guidance, pairing steps, and common command hints.
- Added GitHub Actions CI, release archives, checksums, and build provenance attestations.
- Added terminal-friendly repository, AI usage, knowledge-base, and RAG chunk statistics backed by structured MCP tools.
- Changed pairing so users can paste a complete pairing command or press Enter after entering a one-time URI.
- Changed human-facing commands to terminal-friendly text while keeping data commands machine-readable JSON.
- Rejected unknown CLI flags instead of silently accepting them.
- Standardized all command-line output in English.
