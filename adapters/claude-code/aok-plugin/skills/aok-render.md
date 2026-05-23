# AOK Render

Use this skill when the user wants AOK content rendered for Codex, Claude Code,
Cursor, OpenCode, Gemini, OpenClaw, or Hermes Agent.

## Targets

Supported targets:

- `codex`
- `claude-code`
- `cursor`
- `opencode`
- `gemini`
- `openclaw`
- `hermes-agent`

## Commands

Render one pack contract:

```bash
aok render pack <pack-id> --target <target>
```

Render all pack contracts:

```bash
aok render all --target all
```

Render one self-contained bundle:

```bash
aok render bundle <pack-id> --target <target>
```

Render every bundle:

```bash
aok render bundles --target all
```

## Output

- Contracts write under `build/renders/` by default.
- Bundles write under `build/bundles/` by default.
- Every bundle includes `aok-bundle.json`, Markdown summaries, concept JSON
  files, and copied source assets where applicable.
