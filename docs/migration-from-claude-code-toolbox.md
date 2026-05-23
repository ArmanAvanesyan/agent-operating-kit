# Migration From Claude Code Toolbox

`agent-operating-kit` is now the canonical project. `claude-code-toolbox`
content is represented inside AOK as Claude Code adapter assets and curated
workflow packs.

## What Changed

- Claude Code plugin content moved conceptually into AOK packs.
- The Claude Code marketplace shape is preserved under
  `adapters/claude-code/marketplace/` and `packs/*/claude-code/`.
- AOK pack manifests now carry canonical intent and cross-target render policy.
- Each pack now has `pack.content.json` as the neutral source.

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
   switching the marketplace path to AOK's Claude Code compatibility assets. The
   canonical marketplace name is `aok-claude-code`.
4. For new targets and future tools, generate target contracts with:
   `./scripts/aok render pack <pack-id> --target <target>`.

## Compatibility Note

The old toolbox repository can remain as a compatibility pointer, but new
content and integration decisions should land in AOK first. If a Claude Code
feature remains target-specific, keep it under the relevant pack's `claude-code/`
and update both `pack.content.json` and `pack.json`.

## Deprecation and Compatibility

- Existing `claude-code-toolbox` usage is still supported via compatible plugin
  names in `adapters/claude-code/marketplace/.claude-plugin/marketplace.json`.
- Non-Claude targets currently consume generated JSON contracts; native package
  installers are planned where not yet available.
