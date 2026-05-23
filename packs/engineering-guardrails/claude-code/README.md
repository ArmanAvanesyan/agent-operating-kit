# engineering-guardrails

Hooks that enforce load-bearing engineering conventions automatically.
PreToolUse hooks block writes/edits violating conventions; SessionStart
emits a brief reminder of active rules; a non-blocking pre-commit hook
nudges when tests look stale.

## What it enforces (v0.2)

| Rule | Hook event | Tools | Action |
|---|---|---|---|
| `.py` files must include `from __future__ import annotations` | PreToolUse | Write | block |
| No `assert` in production code (bandit B101) | PreToolUse | Write, Edit | block |
| Alembic CONCURRENTLY ops require `op.get_context().autocommit_block()` | PreToolUse | Write, Edit | block |
| Alembic CONCURRENTLY ops must not mix with `op.create_table` / `op.alter_column` etc. | PreToolUse | Write, Edit | block |
| `git commit ...` with stale pytest cache → nudge to re-run tests | PreToolUse | Bash | warn (additionalContext) |
| New session → emit reminder of active rules + path to CLAUDE.md | SessionStart | — | inform |

**Exemptions** (path-based, applied to every rule that touches `.py`):
`tests/`, `conftest.py`, `test_*.py`, `alembic/versions/`, `scripts/`, `eval/`.

When a blocking rule fires, Claude sees a structured deny response
explaining exactly which line broke which rule and how to fix it. Claude
self-corrects and re-tries — you do nothing.

## Requirements

- Python 3 on `PATH` (the hooks invoke `python` directly).
- No other dependencies — uses only the standard library.

## Install

This plugin ships as part of the `toolbox` marketplace. See the toolbox
repo README for full install instructions.

Per-project enablement (in your project's `.claude/settings.json` or
`~/.claude/settings.json`):

```json
{
  "enabledPlugins": {
    "engineering-guardrails@toolbox": true
  }
}
```

## How it works

Each hook is a Python script that:

1. Reads the tool-call payload from stdin (JSON).
2. Decides if the write/edit violates a rule.
3. On violation: emits a `permissionDecision: "deny"` JSON object on stdout,
   which Claude Code surfaces to the model as a hard block.
4. On pass: exits 0 silently.

Scripts live in `scripts/`. Add new rules by:

1. Writing `scripts/check-<rule>.py` (use existing ones as templates).
2. Adding an entry to `plugin.json` under `hooks.PreToolUse`.

## Skipping a check intentionally

There is no escape hatch by design — if you need to commit a violation
(e.g., a legitimate `assert` for type narrowing in a hot path), disable the
plugin temporarily by setting `"engineering-guardrails@toolbox": false` in
local settings for that session.

## Testing

```bash
SCRIPTS=plugins/engineering-guardrails/scripts

# Should DENY
echo '{"tool_name":"Write","tool_input":{"file_path":"/x/app/y.py","content":"def f(): return 1"}}' \
  | python "$SCRIPTS/check-future-annotations.py"

# Should pass (exit 0, no stdout)
echo '{"tool_name":"Write","tool_input":{"file_path":"/x/app/y.py","content":"from __future__ import annotations\ndef f(): return 1"}}' \
  | python "$SCRIPTS/check-future-annotations.py" ; echo "exit=$?"
```

## Roadmap

Possible additions (open an issue or PR):

- `compare_digest` discipline on `==` between secret/token/signature vars
- `SecretStr.get_secret_value()` discipline — only at SDK call sites
- Block direct `print()` in production code (use `logger`)
- Block `from logging import ...` in production code (use `structlog` or
  the project's configured logger)
