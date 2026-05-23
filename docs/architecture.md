# Architecture

Agent Operating Kit is the canonical source of truth for cross-tool agent
workflow content.

## Layers

1. `schemas/`: JSON Schema contracts for neutral AOK objects.
2. `core/`: conceptual model and rendering rules.
3. `packs/`: reusable workflow content and curated adapter assets.
4. `adapters/`: target-specific mappings for Codex, Claude Code, Cursor,
   OpenCode, Gemini, OpenClaw, and Hermes Agent.
5. `scripts/`: local CLI, install, sync, and validation commands.

## Source Of Truth

AOK owns the neutral model:

- task packet
- role
- hook
- skill
- pack
- tool adapter

Tool-specific files are projections. They can be hand-curated when a target has
features that do not map cleanly, but the AOK manifest remains the canonical
index of intent and compatibility.

## Claude Code Toolbox

`claude-code-toolbox` is represented as curated packs:

- `engineering-guardrails`
- `ship-pipeline`
- `phased-rollout-template`
- `batch-resolve-conflicts`

Their Claude Code plugin files are preserved under `packs/*/claude-code/`.
Those files are compatibility assets, not the new root model.

## Runtime Boundary

AOK is not a daemon and does not own persistent queue state. `swarmd` remains
the runtime/state layer. AOK can generate MCP configuration and payloads that
call `swarmd`, but `swarmd` stays a separate repository and lifecycle.

## Compatibility Policy

- Additive schema fields are allowed.
- Removing or renaming manifest fields requires a migration note.
- Adapter README files must state support level and known gaps.
- Pack manifests must list adapter assets explicitly.
