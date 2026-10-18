# Architecture

Agent Operating Kit is the canonical source of truth for cross-tool agent
workflow content.

## Layers

1. `schemas/`: JSON Schema contracts for neutral AOK objects.
2. `core/`: conceptual model and rendering rules.
3. `methodologies/`: selectable flow and practice-overlay profiles.
4. `work-items/`: methodology-aware intake contracts and provider exports.
5. `packs/`: reusable workflow content and curated adapter assets.
6. `adapters/`: target-specific mappings for Codex, Claude Code, Cursor,
   OpenCode, Gemini, OpenClaw, and Hermes Agent.
7. `scripts/`: Python kit CLI (`scripts/aok`), install helpers, sync, and
   validation — the maintained source for contributors and kit logic.
8. Distribution layer: Go `aok` binary (`cmd/aok`, module
   `github.com/ArmanAvanesyan/agent-operating-kit`) built in release CI and
   published on GitHub Releases so end-user `PATH` installs need neither
   Python nor Go on the device. See [cli-distribution.md](cli-distribution.md).

## Source Of Truth

AOK owns the neutral model:

- task packet
- role
- hook
- skill
- pack
- methodology profile
- methodology artifact
- work-item type
- tool adapter

Tool-specific files are projections. They can be hand-curated when a target has
features that do not map cleanly, but the AOK manifest remains the canonical
index of intent and compatibility.

## Claude Code Toolbox

`claude-code-toolbox` is retired. AOK owns the maintained replacements as curated packs:

- `engineering-guardrails`
- `ship-pipeline`
- `phased-rollout-template`
- `batch-resolve-conflicts`

Their Claude Code plugin files are preserved under each `packs/*/claude-code/`.
A neutral `packs/*/pack.content.json` entry is the canonical definition.
All required assets are stored in AOK; the retired repository is not a build,
installation, rendering, or runtime dependency.

## Methodology Boundary

Methodology profiles compose one flow and optional overlays into project policy.
They do not fork task-packet fields, lifecycle statuses, or work-system provider
mappings. General-purpose packs remain independently installable workflow
content.

The `methodology`, `work-item`, and `work-system` command groups currently
use the contributor Python entrypoint (`./scripts/aok`). They are not yet
implemented by the distributed Go `aok` binary. Their catalogs and templates
ship in release archives so local exports from the kit remain available.

## Runtime Boundary

AOK is not a daemon and does not own persistent queue state. `swarmd` remains
the runtime/state layer. AOK can generate MCP configuration and payloads that
call `swarmd`, but `swarmd` stays a separate repository and lifecycle.

## Render Model

- `packs/*/pack.json` defines supported targets and per-target render mapping.
- `packs/*/pack.content.json` stores neutral content (roles, hooks, skills,
  templates, task packets).
- The release Go binary (`cmd/aok`) implements `render` for end-user installs.
- Contributors and CI packaging may still call Python
  `scripts/aok render pack <pack-id> --target <target>` (and
  `scripts/aok render all --target all`) when building release bundles.

## Compatibility Policy

- Additive schema fields are allowed.
- Removing or renaming manifest fields requires a migration note.
- Adapter README files must state support level and known gaps.
- Pack manifests must list adapter assets explicitly.
