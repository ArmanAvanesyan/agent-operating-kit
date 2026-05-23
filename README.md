# Agent Operating Kit

Agent Operating Kit (AOK) is the cross-tool operating system for agent
workflows. It defines one neutral model for tasks, roles, hooks, skills, packs,
and tool adapters, then renders that model into the agent tools a project
actually uses.

AOK is the source of truth. Tool-specific repositories and plugin formats are
adapters or content packs, not separate operating models.

## No-Terminal Install

Use this path when you want the shortest route to a working install without
cloning the repository.

1. Open the latest GitHub release.
2. Download `agent-operating-kit-<version>-macos-installer.zip`.
3. Double-click the ZIP in Finder.
4. Double-click `install.command`.
5. Open Codex Desktop and confirm Agent Operating Kit is available in the local
   plugin marketplace.

The installer copies AOK into
`~/Library/Application Support/Agent Operating Kit/agent-operating-kit`, updates
the local Codex marketplace registration, links `~/.local/bin/aok`, and writes a
plain setup report at
`~/Library/Application Support/Agent Operating Kit/setup-report.md`.

## Developer First Success Path

Use this path when you are comfortable with the terminal and want a validated
checkout.

```bash
git clone https://github.com/ArmanAvanesyan/agent-operating-kit.git
cd agent-operating-kit
./scripts/install.sh
./scripts/aok doctor
./scripts/aok validate
./scripts/aok render pack engineering-guardrails --target codex
```

Success means:

- `doctor` reports required files, marketplace registration, plugin symlink, and
  CLI shim as `OK`.
- `validate` prints `Kit validation passed.`
- the render command writes
  `build/renders/engineering-guardrails/codex.json`.

## Downloadable Bundles

Every release also includes `agent-operating-kit-<version>-bundles.zip`. It
contains self-contained, parseable bundles for every pack and every target:

- Codex: native plugin-oriented bundle.
- Claude Code: compatibility marketplace assets.
- Cursor, OpenCode, Gemini, OpenClaw, Hermes Agent: generated import bundles.

Technical users can regenerate the same bundles:

```bash
./scripts/aok render bundles --target all
```

## Claude Code First Success Path

Register AOK's Claude Code compatibility marketplace with the canonical key
`aok-claude-code`:

```bash
aok claude setup --enable engineering-guardrails
```

The setup helper preserves a timestamped backup of existing settings before it
updates `~/.claude/settings.json`. The resulting settings entry is:

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

Then render the same pack for Claude Code:

```bash
./scripts/aok render pack engineering-guardrails --target claude-code
```

Success means Claude Code can see the `engineering-guardrails` plugin through
`aok-claude-code`, and AOK writes
`build/renders/engineering-guardrails/claude-code.json`.

## Pack Chooser

| Pack | Choose it when you need | Main concepts |
|---|---|---|
| `engineering-guardrails` | Session reminders, pre-commit freshness checks, migration conventions, and assert/future-annotation guardrails. | hooks, role, task packet |
| `ship-pipeline` | Pull request shipping flow, CI failure recovery, and a CI fixer role. | skills, roles, task packet |
| `phased-rollout-template` | A starter scaffold for phased projects with CI, deploy, infra, and process docs. | templates, skill, role |
| `batch-resolve-conflicts` | Coordinated conflict resolution across multiple PRs or branches. | skill, role, task packet |

Render any pack with:

```bash
./scripts/aok render pack <pack-id> --target <target>
```

## Supported Targets

All current packs declare active render support for:

| Target | Support | Output style |
|---|---|---|
| `codex` | Active | Generated Codex JSON contract |
| `claude-code` | Active compatibility adapter | Preserved native Claude Code assets plus compatibility render |
| `cursor` | Active | Generated structured contract |
| `opencode` | Active | Generated structured contract |
| `gemini` | Active | Generated structured contract |
| `openclaw` | Active | Generated structured contract |
| `hermes-agent` | Active | Generated structured contract |

See `docs/targets-support-matrix.md` for the detailed matrix.

## Project Init

Initialize AOK operating files in another project:

```bash
./scripts/aok project init /path/to/project
./scripts/aok project validate /path/to/project
```

If the project already has `AGENTS.md`, AOK preserves it by default and writes
`AGENTS.aok-proposed.md` plus `AGENTS.aok.diff`. Use `--force` only when you
intentionally want AOK to replace `AGENTS.md` after writing a backup.

See `docs/project-init-guide.md` for the full guide.

## Repository Relationships

- `ArmanAvanesyan/agent-operating-kit` is the canonical public project.
- `ArmanAvanesyan/claude-code-toolbox` is represented here as Claude Code
  adapter assets and curated packs.
- `ArmanAvanesyan/swarmd` remains separate. AOK integrates with it through MCP
  by generating config and mapping task packets, roles, decisions, and events
  onto the `swarmd` MCP surface.

## Render Contracts

AOK can render each pack into parseable target contracts:

```bash
./scripts/aok render pack <pack-id> --target <target> [--out PATH]
./scripts/aok render all --target all [--out DIR]
./scripts/aok render bundle <pack-id> --target <target> [--out DIR]
./scripts/aok render bundles --target all [--out DIR]
```

`target` is one of `codex`, `claude-code`, `cursor`, `opencode`, `gemini`,
`openclaw`, `hermes-agent`, or `all` where supported by the command.

## Usage Model

1. Create or sync a task packet.
2. Select a role for implementation and one role for QA or review.
3. Run pre-hooks to check project context, scope, worktree safety, and risk.
4. Execute one issue-sized change.
5. Run verification and after-hooks.
6. Record GitHub, swarmd, or local lifecycle updates.

The neutral schema lives in `schemas/`. Tool-specific target behavior lives in
`adapters/`. Reusable workflow content lives in `packs/`.

## Claude Code Toolbox Migration

The former Claude Code Toolbox content is now represented as AOK packs:

- `packs/engineering-guardrails`
- `packs/ship-pipeline`
- `packs/phased-rollout-template`
- `packs/batch-resolve-conflicts`

Claude Code compatibility assets are preserved under each pack's
`claude-code/` directory. See
`docs/migration-from-claude-code-toolbox.md` for before/after settings and
migration guidance.

## Operations

- Install and first-run onboarding: `docs/install-and-onboarding.md`
- Project initialization: `docs/project-init-guide.md`
- Target support: `docs/targets-support-matrix.md`
- Release checklist: `docs/release-checklist.md`
- Uninstall and rollback: `docs/uninstall-and-rollback.md`
- `swarmd` integration: `docs/swarmd-integration.md`

## Validation

Run:

```bash
./scripts/aok validate
python3 -m py_compile scripts/aok
```

Validation is dependency-free and checks required docs, schemas, adapter
manifests, pack manifests, pack content, JSON syntax, Claude Code marketplace
metadata, and supported target coverage.
