# Claude Code Adapter

Status: active compatibility adapter.  
Support level: `compatibility-asset`.  
Host version: untracked (see `docs/host-compatibility.md`).

The Claude Code adapter maintains the plugin assets migrated from the retired
`claude-code-toolbox`. AOK is the source of truth and contains all required assets;
the old repository and local checkout are not required. On kit **0.4.0**, Claude Code remains a
**marketplace / compatibility** path alongside Codex’s multi-platform native
installers — not a second operating model.

## Render Shape

- Marketplace metadata lives under `adapters/claude-code/marketplace/.claude-plugin/`.
- The root AOK control plugin lives under `adapters/claude-code/aok-plugin/`
  (setup, doctor, render, migrate, pack discovery skills).
- Pack-specific Claude Code assets live under `packs/*/claude-code/`.
- Bundle metadata is expected from the compatibility marketplace and each
  preserved `packs/*/claude-code/plugin.json` asset.
- `aok render bundle <pack> --target claude-code` emits a compatibility bundle
  that includes preserved plugin markdown and command files under
  `build/bundles/claude-code/<pack>/`.

## Marketplace and import (0.4.0)

Register the `aok-claude-code` marketplace from this repo (or a release checkout),
then enable the preserved plugin names. Root README documents the registration
flow; pack `importInstructions` point at the same marketplace bundle.

Existing toolbox plugin names are preserved in the pack assets:

- `agent-operating-kit` for AOK setup, doctor, render, migration, and pack
  discovery workflows
- `engineering-guardrails`
- `ship-pipeline`
- `phased-rollout-template`
- `batch-resolve-conflicts`

New content should add or update the neutral `pack.json` and `pack.content.json`
first, then update Claude Code assets where target-specific behavior is needed.

## Known Gaps

- Claude Code plugin hooks can depend on `CLAUDE_PLUGIN_ROOT` and remain
  adapter-specific.
- Marketplace plugin version fields may lag the kit version (`0.4.0`); treat
  pack/`plugin.json` versions as independent unless a release syncs them.
