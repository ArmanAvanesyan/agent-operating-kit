# Uninstall And Rollback

This guide removes a local AOK install or rolls it back to an earlier checkout.
It does not remove projects that were initialized with `aok project init`.

## What Install Adds

The release installer copies the kit to one of these roots, depending on
platform:

| Platform | Install root |
|---|---|
| macOS | `~/Library/Application Support/Agent Operating Kit/agent-operating-kit` |
| Windows | `%LOCALAPPDATA%\Agent Operating Kit\agent-operating-kit` |
| Linux user | `${XDG_DATA_HOME:-$HOME/.local/share}/agent-operating-kit/agent-operating-kit` |
| Linux system | `/opt/agent-operating-kit/agent-operating-kit` |

Each installer also writes a setup report near its install root:

| Platform | Setup report |
|---|---|
| macOS | `~/Library/Application Support/Agent Operating Kit/setup-report.md` |
| Windows | `%LOCALAPPDATA%\Agent Operating Kit\setup-report.md` |
| Linux user | `${XDG_DATA_HOME:-$HOME/.local/share}/agent-operating-kit/setup-report.md` |
| Linux system | `/opt/agent-operating-kit/setup-report.md` |

It then registers:

- `~/.agents/plugins/marketplace.json`
- `~/.agents/plugins/plugins/agent-operating-kit` as a symlink on Unix, or a
  junction/copy on Windows, pointing at the installed kit
- `~/.local/bin/aok` on Unix, or `~/.local/bin/aok.cmd` (and the installed
  `aok.exe`) on Windows — the release path installs a Go binary or shim to it,
  not a symlink to `scripts/aok`. See [cli-distribution.md](cli-distribution.md).

Claude Code setup may also add an `aok-claude-code` entry to
`~/.claude/settings.json`.

## Uninstall Local AOK

Remove the plugin link:

```bash
# Unix (macOS / Linux)
rm ~/.agents/plugins/plugins/agent-operating-kit

# Windows (Command Prompt)
rmdir "%USERPROFILE%\.agents\plugins\plugins\agent-operating-kit"
```

On Windows, if the plugin path is a copied directory rather than a junction,
delete that directory instead of using `rmdir` on a junction.

Remove the CLI binary and PATH shim (binary + shim, not a symlink-to-Python
assumption):

```bash
# Unix (macOS / Linux) — removes the Go binary or shim on PATH
rm ~/.local/bin/aok

# Windows — remove the cmd wrapper and any installed aok.exe under the kit
# or PATH bin directory used by the installer
del "%USERPROFILE%\.local\bin\aok.cmd"
del "%USERPROFILE%\.local\bin\aok.exe"
```

If `aok.exe` lives only under the install root, deleting that root (below)
removes it; still delete `aok.cmd` from `~/.local/bin` so `PATH` is clean.

Remove the installed kit and setup report for your platform:

```bash
# macOS
rm -rf "$HOME/Library/Application Support/Agent Operating Kit"

# Windows (Command Prompt)
rmdir /s /q "%LOCALAPPDATA%\Agent Operating Kit"

# Linux user
rm -rf "${XDG_DATA_HOME:-$HOME/.local/share}/agent-operating-kit"

# Linux system
sudo rm -rf /opt/agent-operating-kit
```

Edit `~/.agents/plugins/marketplace.json` and remove the plugin entry whose
`name` is `agent-operating-kit`.

If you added Claude Code compatibility, edit `~/.claude/settings.json` and
remove:

- `extraKnownMarketplaces.aok-claude-code`
- any `enabledPlugins` entries ending in `@aok-claude-code`

## Roll Back To Another Checkout

If you keep two AOK checkouts, point the plugin and CLI entrypoints back to the
checkout you want. Developer rollbacks may use `scripts/aok`; release-path
rollbacks should restore the previous release binary on `PATH`.

Unix (macOS / Linux) — developer checkout:

```bash
rm ~/.agents/plugins/plugins/agent-operating-kit
ln -s /path/to/previous/agent-operating-kit ~/.agents/plugins/plugins/agent-operating-kit
rm ~/.local/bin/aok
ln -s /path/to/previous/agent-operating-kit/scripts/aok ~/.local/bin/aok
```

Unix — release rollback: re-run the previous release installer (or copy the
previous `aok-<version>-…` binary to `~/.local/bin/aok`) instead of symlinking
`scripts/aok`.

Windows: recreate the plugin junction or copy under
`%USERPROFILE%\.agents\plugins\plugins\agent-operating-kit`. For a developer
checkout, point `%USERPROFILE%\.local\bin\aok.cmd` at
`\path\to\previous\agent-operating-kit\scripts\aok` (or re-run that checkout's
install helper). For a release rollback, restore the previous `aok.exe` and
`aok.cmd` from that release's installer.

Then verify:

```bash
aok doctor
aok validate
# or, developer checkout:
/path/to/previous/agent-operating-kit/scripts/aok doctor
/path/to/previous/agent-operating-kit/scripts/aok validate
```

## Roll Back Project Init

Project init writes files into the target project. Rollback depends on what the
init output reported:

- `created`: remove the created file if you do not want AOK in that project.
- `unchanged`: no action needed.
- `preserved AGENTS.md`: remove `AGENTS.aok-proposed.md` and `AGENTS.aok.diff`.
- `updated`: restore the previous file from `.aok-backups/<timestamp>/`.

`aok project init` also writes `.aok/project.json`. Remove `.aok/` only if the
project should no longer track AOK initialization metadata.

## Validate Removal

After uninstalling, these paths should be absent unless another local install
owns them:

```bash
# Unix
test ! -e ~/.agents/plugins/plugins/agent-operating-kit
test ! -e ~/.local/bin/aok

# Windows (PowerShell)
Test-Path "$env:USERPROFILE\.agents\plugins\plugins\agent-operating-kit"
Test-Path "$env:USERPROFILE\.local\bin\aok.cmd"
```

On Windows, both `Test-Path` checks should return `False` after a clean
uninstall.

If Claude Code still shows AOK plugins, recheck `~/.claude/settings.json` for
`aok-claude-code`.
