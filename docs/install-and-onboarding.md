# Install And Onboarding

This guide gets a new user to a first successful AOK install, then proves that
Codex and Claude Code can consume the same neutral pack.

## Requirements

- macOS or another Unix-like shell environment.
- Python 3 for `scripts/aok`.
- Optional but useful: `git`, `gh`, `codex`, and Claude Code.

The installer treats `codex` as optional. `aok doctor` reports optional command
availability separately from required AOK files and local registration.

## Release Installer On macOS

Use this route when you do not want to use Git or terminal commands.

1. Open the latest GitHub release for AOK.
2. Download `agent-operating-kit-<version>-macos-installer.zip`.
3. Double-click the ZIP in Finder.
4. Double-click `install.command`.
5. Open Codex Desktop and check the local plugin marketplace for Agent
   Operating Kit.

The installer writes AOK into:

```text
~/Library/Application Support/Agent Operating Kit/agent-operating-kit
```

It also writes a plain setup report:

```text
~/Library/Application Support/Agent Operating Kit/setup-report.md
```

No Git checkout is required. The installer still uses the bundled Python CLI
internally so it can preserve existing marketplace entries and generate the
setup report.

## Optional Manual Verification

Technical users can verify the installed copy:

```bash
"$HOME/Library/Application Support/Agent Operating Kit/agent-operating-kit/scripts/aok" doctor
"$HOME/Library/Application Support/Agent Operating Kit/agent-operating-kit/scripts/aok" validate
```

## Git Install

```bash
git clone https://github.com/ArmanAvanesyan/agent-operating-kit.git
cd agent-operating-kit
./scripts/install.sh
./scripts/aok doctor
./scripts/aok validate
```

The installer writes:

- `~/.agents/plugins/marketplace.json`
- `~/.agents/plugins/plugins/agent-operating-kit` as a symlink to this checkout
- `~/.local/bin/aok` as a symlink to `scripts/aok`

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

## Claude Code First Success

Register AOK's Claude Code compatibility marketplace. This updates
`~/.claude/settings.json` after writing a timestamped backup:

```bash
aok claude setup --enable engineering-guardrails
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
- If `doctor` reports the plugin symlink as failed, check whether
  `~/.agents/plugins/plugins/agent-operating-kit` already exists and points
  somewhere else.
- If Claude Code cannot see a plugin, verify that `~/.claude/settings.json` uses
  `aok-claude-code` and that the path points to
  `adapters/claude-code/marketplace`.
