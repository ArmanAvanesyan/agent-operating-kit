# Agent Operating Kit Claude Code Plugin

This is the root Claude Code control plugin for AOK. It gives Claude Code users
an obvious entrypoint for setup, diagnostics, rendering, migration, and pack
discovery.

The workflow packs remain separate plugins in the same `aok-claude-code`
marketplace:

- `engineering-guardrails`
- `ship-pipeline`
- `batch-resolve-conflicts`
- `phased-rollout-template`

Enable this root plugin first, then enable whichever workflow packs match the
project.
