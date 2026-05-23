# Migration From Claude Code Toolbox

`agent-operating-kit` is now the canonical project. `claude-code-toolbox`
content is represented inside AOK as Claude Code adapter assets and curated
workflow packs.

## What Changed

- Claude Code plugin content moved conceptually into AOK packs.
- The Claude Code marketplace shape is preserved under
  `adapters/claude-code/marketplace/` and `packs/*/claude-code/`.
- AOK pack manifests describe the neutral intent and target mappings.
- New cross-tool adapters can render the same operating content for Codex,
  Cursor, OpenCode, Gemini, OpenClaw, and Hermes Agent.

## Pack Mapping

| Old toolbox plugin | New AOK pack |
|---|---|
| `engineering-guardrails` | `packs/engineering-guardrails` |
| `ship-pipeline` | `packs/ship-pipeline` |
| `phased-rollout-template` | `packs/phased-rollout-template` |
| `batch-resolve-conflicts` | `packs/batch-resolve-conflicts` |

## Migration Path

1. Install AOK from `ArmanAvanesyan/agent-operating-kit`.
2. Enable the Claude Code adapter assets from AOK instead of cloning the old
   toolbox as the source of truth.
3. For existing Claude Code projects, keep current enabled plugin names while
   switching the marketplace path to AOK's Claude Code compatibility assets.
4. For new projects, prefer AOK pack names and neutral manifests.

## Compatibility Note

The old toolbox repository can remain as a compatibility pointer or archived
mirror, but new content should land in AOK first. If a Claude Code feature
requires target-specific files, place them under the relevant pack's
`claude-code/` directory and update `pack.json`.
