# Agent Operating Kit

Agent Operating Kit (AOK) is the cross-tool operating system for agent workflows.
It defines one neutral model for tasks, roles, hooks, skills, packs, and tool
adapters, then renders that model into the agent tools a project actually uses.

AOK is the source of truth. Tool-specific repositories and plugin formats are
adapters or content packs, not separate operating models.

## Supported Targets

- Codex
- Claude Code
- Cursor
- OpenCode
- Gemini
- OpenClaw
- Hermes Agent

## Repository Relationships

- `ArmanAvanesyan/agent-operating-kit` is the canonical public project.
- `ArmanAvanesyan/claude-code-toolbox` is represented here as Claude Code
  adapter assets and curated packs.
- `ArmanAvanesyan/swarmd` remains separate. AOK integrates with it through MCP
  by generating config and mapping task packets, roles, decisions, and events onto
  the `swarmd` MCP surface.

## Install

Clone the kit:

```bash
git clone https://github.com/ArmanAvanesyan/agent-operating-kit.git
cd agent-operating-kit
```

Install the local Codex plugin and CLI shim:

```bash
./scripts/install.sh
```

Validate the kit:

```bash
./scripts/validate.sh
```

Initialize a project with AOK operating files:

```bash
./scripts/aok project init /path/to/project
./scripts/aok project validate /path/to/project
```

## Render Contracts

AOK can render each pack into parseable target contracts:

```bash
./scripts/aok render pack <pack-id> --target <target> [--out PATH]
./scripts/aok render all --target all [--out DIR]
```

`target` is one of `codex`, `claude-code`, `cursor`, `opencode`, `gemini`,
`openclaw`, `hermes-agent`.

## Usage Model

1. Create or sync a task packet.
2. Select a role for implementation and one role for QA or review.
3. Run pre-hooks to check project context, scope, worktree safety, and risk.
4. Execute one issue-sized change.
5. Run verification and after-hooks.
6. Record GitHub, swarmd, or local lifecycle updates.

The neutral schema lives in `schemas/`. Tool-specific target behavior lives in
`adapters/`. Reusable workflow content lives in `packs/`.

## Claude Code Compatibility

The Claude Code adapter exposes the former toolbox plugins through AOK. Register
the compatibility marketplace path in `~/.claude/settings.json`:

```json
{
  "extraKnownMarketplaces": {
    "aok-claude-code": {
      "source": {
        "source": "directory",
        "path": "/path/to/agent-operating-kit/adapters/claude-code/marketplace"
      }
    }
  }
}
```

Then enable a pack by its preserved plugin name:

```json
{
  "enabledPlugins": {
    "engineering-guardrails@aok-claude-code": true
  }
}
```

## Claude Code Toolbox Migration

The former Claude Code Toolbox content is now represented as AOK packs:

- `packs/engineering-guardrails`
- `packs/ship-pipeline`
- `packs/phased-rollout-template`
- `packs/batch-resolve-conflicts`

Claude Code compatibility assets are preserved under each pack's
`claude-code/` directory. See
`docs/migration-from-claude-code-toolbox.md` for migration guidance.

## swarmd Integration

AOK does not absorb `swarmd`. AOK can generate MCP configuration and payloads
for `swarmd`:

- task packets map to `task.create`
- roles map to `assignee`
- decisions map to `memory.put`
- lifecycle updates map to `event.append`

See `docs/swarmd-integration.md` for example payloads.

## Validation

Run:

```bash
./scripts/aok validate
python3 -m py_compile scripts/aok
```

Validation is dependency-free and checks required docs, schemas, adapter manifests,
pack manifests, pack content, JSON syntax, and supported target coverage.
