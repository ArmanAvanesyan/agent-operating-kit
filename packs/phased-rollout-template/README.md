# Phased Rollout Template Pack

Reusable project scaffolding content for phased production rollouts.

AOK stores this pack as neutral content in `pack.content.json` and preserves
its Claude Code behavior in `claude-code/` for compatibility.

## Render Targets

- `codex`: native AOK Codex plugin bundle metadata plus this pack's neutral content.
- `claude-code`: compatibility marketplace bundle metadata plus preserved plugin assets under `claude-code/`.
- `cursor`, `opencode`, `gemini`, `openclaw`, `hermes-agent`: generated import bundle metadata and JSON contracts via `aok render pack phased-rollout-template --target <target>`.
