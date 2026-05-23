# Ship Pipeline Pack

Reusable PR shipping and CI recovery workflow content.

AOK stores this pack as neutral content in `pack.content.json` and preserves
its Claude Code behavior in `claude-code/` for compatibility.

## Render Targets

- `codex`, `cursor`, `opencode`, `gemini`, `openclaw`, `hermes-agent`: generated JSON contracts via `aok render pack ship-pipeline --target <target>`.
- `claude-code`: preserved plugin assets under `claude-code/` plus generated compatibility metadata.
