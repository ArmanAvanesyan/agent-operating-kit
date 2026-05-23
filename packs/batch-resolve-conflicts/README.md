# Batch Resolve Conflicts Pack

Reusable workflow for resolving multiple conflicting pull requests in parallel
when conflicts match known shared-file patterns.

AOK stores this pack as neutral content in `pack.content.json` and preserves
its Claude Code behavior in `claude-code/` for compatibility.

## Render Targets

- `codex`, `cursor`, `opencode`, `gemini`, `openclaw`, `hermes-agent`: generated JSON contracts via `aok render pack batch-resolve-conflicts --target <target>`.
- `claude-code`: preserved plugin assets under `claude-code/` plus generated compatibility metadata.
