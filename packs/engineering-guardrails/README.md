# Engineering Guardrails Pack

Reusable hooks and checks that block or warn on load-bearing engineering convention violations.

AOK stores this pack as neutral content in `pack.content.json` and preserves its
Claude Code behavior in `claude-code/` for compatibility.

## Render Targets

- `codex`, `cursor`, `opencode`, `gemini`, `openclaw`, `hermes-agent`: generated JSON contracts via `aok render pack engineering-guardrails --target <target>`.
- `claude-code`: preserved plugin assets under `claude-code/` plus generated compatibility metadata.
