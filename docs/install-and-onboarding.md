# Install And Onboarding

This guide gets a new user to a first successful AOK install, then proves that
Codex and Claude Code can consume the same neutral pack.

## Requirements

### End-user (release installers)

- macOS, Windows, or Linux (user or system install).
- No Python runtime and no Go toolchain on the device for the release path.
  Installers place a thin Go `aok` binary on `PATH`. See
  [cli-distribution.md](cli-distribution.md).
- Optional but useful: `git`, `gh`, `codex`, and Claude Code.

### Developer (git checkout)

- Python 3 for `./scripts/aok` (stdlib only; no pip install for kit validation).
- Optional: Go toolchain when building the CLI locally from `cmd/aok` (when
  present). See [CONTRIBUTING.md](../CONTRIBUTING.md).

The installer treats `codex` as optional. `aok doctor` reports optional command
availability separately from required AOK files and local registration.

## Release Installers

Use this route when you do not want to use Git clone flows.

1. Open the latest GitHub release for AOK.
2. Download your platform installer:
   - macOS Apple Silicon:
     `agent-operating-kit-<version>-macos-apple-silicon-installer.zip`
   - macOS Intel: `agent-operating-kit-<version>-macos-intel-installer.zip`
   - Windows: `agent-operating-kit-<version>-windows-installer.zip`
   - Linux user install:
     `agent-operating-kit-<version>-linux-user-installer.tar.gz`
   - Linux system install:
     `agent-operating-kit-<version>-linux-system-installer.tar.gz`
3. Extract and run:
   - macOS: `install.command`
   - Windows: `install.cmd` (or `install.ps1`)
   - Linux user: `./install.sh`
   - Linux system: `sudo ./install.sh`
4. Open Codex Desktop and check the local plugin marketplace for Agent
   Operating Kit.

Install paths:

- macOS: `~/Library/Application Support/Agent Operating Kit/agent-operating-kit`
- Windows: `%LOCALAPPDATA%\Agent Operating Kit\agent-operating-kit`
- Linux user:
  `${XDG_DATA_HOME:-$HOME/.local/share}/agent-operating-kit/agent-operating-kit`
- Linux system: `/opt/agent-operating-kit/agent-operating-kit`

Each installer writes a setup report near its install root.

On Windows, the installer writes `~/.local/bin/aok.cmd` as the CLI wrapper that
launches the release `aok.exe`. The plugin directory uses a junction when
Windows allows it and falls back to a copy when junction creation is
unavailable.

No Git checkout is required. The release path puts the Go `aok` binary on
`PATH` (not a symlink to `scripts/aok`) and installs kit content under the
roots above. Marketplace registration and setup reports are produced by the
installer / binary path.

## Optional Manual Verification

Prefer the `aok` binary on `PATH` after a release install:

```bash
aok doctor
aok validate
```

On Windows, use `aok.cmd` from `~/.local/bin` when that wrapper is on `PATH`.

Until the Go binary ships with full command parity, you can still invoke the
Python kit CLI from an install root or git checkout (developer path):

```bash
# macOS install root (developer / transitional)
"$HOME/Library/Application Support/Agent Operating Kit/agent-operating-kit/scripts/aok" doctor

# Linux user
"${XDG_DATA_HOME:-$HOME/.local/share}/agent-operating-kit/agent-operating-kit/scripts/aok" doctor

# Linux system
/opt/agent-operating-kit/agent-operating-kit/scripts/aok doctor
```

## Git Install

```bash
git clone https://github.com/ArmanAvanesyan/agent-operating-kit.git
cd agent-operating-kit
./scripts/aok codex setup
./scripts/aok validate
```

Use `./scripts/aok codex setup --claude` when you want Codex and Claude Code
configured in one pass.

Use `./scripts/aok codex setup --project /path/to/project` only when you want
to initialize a separate downstream project with AOK files and Codex local
environment assets. Do not point `--project` at the AOK repo checkout itself.

The installer / setup helpers write:

- `~/.agents/plugins/marketplace.json`
- `~/.agents/plugins/plugins/agent-operating-kit` as a symlink on Unix, or a
  junction/copy on Windows
- `~/.local/bin/aok` on Unix (release: Go binary or shim to it; developer
  checkout may still link `scripts/aok`), or `~/.local/bin/aok.cmd` on Windows

## Codex First Success

Render `engineering-guardrails` for Codex:

```bash
./scripts/aok render pack engineering-guardrails --target codex
```

Expected result:

```text
Wrote render: /path/to/agent-operating-kit/build/renders/engineering-guardrails/codex.json
```

That file is the first proof that the neutral AOK pack can produce a Codex
contract.

## Codex App Local Environment

For a project that should use AOK inside Codex App worktrees, initialize the
project files:

```bash
aok project init /path/to/project
aok codex local-env validate /path/to/project
aok codex local-env actions /path/to/project
```

Codex can read `.codex/environments/environment.toml` for the setup script and
action definitions. The setup script writes `.codex/aok-setup-report.md`; the
actions expose doctor, validation, bundle rendering, project init, Claude setup,
and release build commands.

The AOK manifest at `.codex/aok-local-environment.json` is validation metadata
that records the same setup script and action commands. The TOML shape is based
on a locally observed Codex plugin example because the public docs page does not
publish the full generated schema.

## Claude Code First Success

Register AOK's Claude Code compatibility marketplace. This updates
`~/.claude/settings.json` after writing a timestamped backup:

```bash
aok claude setup --enable agent-operating-kit engineering-guardrails
```

The helper uses the canonical marketplace key `aok-claude-code` and writes this
shape:

```json
{
  "extraKnownMarketplaces": {
    "aok-claude-code": {
      "source": {
        "source": "directory",
        "path": "/path/to/agent-operating-kit/adapters/claude-code/marketplace"
      }
    }
  },
  "enabledPlugins": {
    "agent-operating-kit@aok-claude-code": true,
    "engineering-guardrails@aok-claude-code": true
  }
}
```

Then render the pack:

```bash
./scripts/aok render pack engineering-guardrails --target claude-code
```

Expected result:

```text
Wrote render: /path/to/agent-operating-kit/build/renders/engineering-guardrails/claude-code.json
```

## Choose A Pack

Start with one pack:

| Need | Pack |
|---|---|
| Safer everyday agent sessions and guardrail hooks | `engineering-guardrails` |
| PR shipping and CI recovery | `ship-pipeline` |
| A new phased project scaffold | `phased-rollout-template` |
| Coordinated branch or PR conflict resolution | `batch-resolve-conflicts` |

Render all packs for all targets when you want a complete generated snapshot:

```bash
./scripts/aok render bundles --target all
```

## Troubleshooting

- If `doctor` reports a missing CLI shim, ensure `~/.local/bin` exists and rerun
  `./scripts/install.sh`.
- If `doctor` reports the plugin path as failed, check whether
  `~/.agents/plugins/plugins/agent-operating-kit` already exists and points
  somewhere else.
- If Claude Code cannot see a plugin, verify that `~/.claude/settings.json` uses
  `aok-claude-code` and that the path points to
  `adapters/claude-code/marketplace`.
