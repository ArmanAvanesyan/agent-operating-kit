# Changelog

## Unreleased

## 0.5.0

- Ship the thin Go CLI (`cmd/aok`) for end-user install without Python on the
  device: `version`, `doctor`, `validate`, `install`, `claude setup`,
  `codex setup` / `local-env`, `project init|validate`, and
  `render pack|all|bundle|bundles`.
- Release publish cross-compiles five platform binaries
  (`aok-<version>-{darwin,linux}-{arm64,amd64}` / `windows-amd64.exe`), lists
  them in `SHA256SUMS` and `index.json`, and bundles `bin/aok` into installers
  that invoke the binary for `install` / `doctor`.
- PATH install copies the Go binary to `~/.local/bin` (Unix) or `aok.exe` +
  `aok.cmd` (Windows); `install --replace` upgrades old Python shims.
- Python `scripts/aok` remains the contributor entrypoint; `task` / `hooks` /
  `import` stay Python-only. See `docs/cli-distribution.md`.

## 0.4.0

- Add Codex App local-environment setup and action helper templates.
- Add `aok codex local-env` init, validate, and action discovery commands.
- Add a conservative `.codex/environments/environment.toml` based on a locally
  observed Codex plugin example, plus AOK validation metadata.
- Add release installers for macOS Apple Silicon, macOS Intel, Windows, Linux
  user, and Linux system installation flows.

## 0.3.0

- Add a root Claude Code `agent-operating-kit` control plugin for setup,
  diagnostics, rendering, migration, and pack discovery.
- Register the root plugin in the `aok-claude-code` marketplace alongside the
  existing workflow packs.
- Update `aok claude setup --enable all` to enable the root plugin plus all
  migrated packs.

## 0.2.0

- Make AOK the canonical cross-tool agent operating kit.
- Represent Claude Code Toolbox content as curated AOK packs and Claude Code
  compatibility assets.
- Add neutral pack content, pack render contracts, and bundle metadata for
  Codex, Claude Code, Cursor, OpenCode, Gemini, OpenClaw, and Hermes Agent.
- Add non-CLI onboarding docs, release artifact generation, and macOS
  installer-equivalent packaging.
