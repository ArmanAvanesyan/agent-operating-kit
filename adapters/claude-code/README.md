# Claude Code Adapter

Status: active compatibility adapter.

The Claude Code adapter preserves useful `claude-code-toolbox` plugin assets while
making AOK the source of truth.

## Render Shape

- Marketplace metadata lives under `adapters/claude-code/marketplace/.claude-plugin/`.
- Pack-specific Claude Code assets live under `packs/*/claude-code/`.
- `aok render pack <pack> --target claude-code` emits a compatibility JSON contract
  that points at preserved plugin markdown and command files.

## Compatibility

Existing toolbox plugin names are preserved in the pack assets:

- `engineering-guardrails`
- `ship-pipeline`
- `phased-rollout-template`
- `batch-resolve-conflicts`

New content should add or update the neutral `pack.json` and `pack.content.json`
first, then update Claude Code assets where target-specific behavior is needed.
