# AOK Doctor

Use this skill when the user asks whether AOK is installed, healthy, or visible
to local agent tools.

## Commands

Human-readable check:

```bash
aok doctor
```

Machine-readable check:

```bash
aok doctor --json
```

Write a setup report:

```bash
aok doctor --report "$HOME/Library/Application Support/Agent Operating Kit/setup-report.md"
```

## Interpretation

- Required file failures mean the installed AOK copy is incomplete.
- Marketplace or plugin symlink failures mean Codex cannot discover AOK.
- `git`, `gh`, and `codex` are optional capability checks, not core install
  blockers.

## Output

Report the failing field, the path involved, and the exact command to re-run.
Do not imply GitHub or live service validation unless it actually ran.
