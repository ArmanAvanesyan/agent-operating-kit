# ship-pipeline

Bundles the full PR ship workflow we've run dozens of times on real projects
into two slash commands plus a specialist fixer agent.

## What it does

- `/ship-pr` — runs **simplify → quality gates → push → create PR → watch CI → squash-merge** end-to-end. Stops on any failure and reports.
- `/fix-ci <pr-number>` — diagnoses a failing PR, delegates to the `pr-ci-fixer` agent, watches CI until green.
- `pr-ci-fixer` agent — specialist that classifies the failure (lint / type / test / security / migration / NDA / other), reproduces it locally, applies a real fix (no band-aids), and re-pushes.

## Install

Ships as part of the `toolbox` marketplace. To enable, add to your project's
`.claude/settings.json` (or `~/.claude/settings.json`):

```json
{
  "enabledPlugins": {
    "ship-pipeline@toolbox": true
  }
}
```

## Invoking

```text
/ship-pr
/ship-pr "feat(billing): add stars payment"
/ship-pr "feat(billing): add stars payment" "Implements Telegram Stars (XTR) flow; closes #14."
/fix-ci 42
```

If you omit the title, it's derived from your most recent commit. If you omit
the body, it's generated from the commit messages on the branch.

## Quality gates that run

In order, in the same shell, each must pass before the next runs:

1. `uv run ruff check . --fix && uv run ruff format .`
2. `uv run mypy app` (target read from `pyproject.toml [tool.mypy]`)
3. `uv run pytest tests/unit/ -x -q` (testpaths read from `pyproject.toml`)
4. `uv run bandit -r app/ -t B603,B604,B105,B106,B101,B608`

If any gate fails, the pipeline stops before push — nothing leaks to GitHub.

## Safety rules (hard-coded)

- Never `--no-verify` to bypass hooks.
- Never force-push to `main`.
- Never merge a PR with red CI.
- Refuses to PR from `main` into itself.
- If `gh` isn't authenticated, stops and prompts you to `gh auth login`.

## Example session

```text
> /ship-pr

[ship-pr] git status — clean, 3 commits ahead of main
[ship-pr] simplify — no changes recommended
[ship-pr] ruff — 2 auto-fixes, formatting clean
[ship-pr] mypy app — Success
[ship-pr] pytest tests/unit/ — 142 passed
[ship-pr] bandit — No issues
[ship-pr] committed: "style: ruff auto-fixes"
[ship-pr] pushed feat/billing-stars → origin
[ship-pr] PR #42 created: https://github.com/org/repo/pull/42
[ship-pr] watching CI...
[ship-pr] ✗ pytest-integration failed
[ship-pr] delegating to /fix-ci 42

[fix-ci] pr-ci-fixer agent activated
[pr-ci-fixer] failure: pytest tests/integration/test_billing.py::test_credit_charge
[pr-ci-fixer] reproduced locally — missing await on async call
[pr-ci-fixer] fix applied, gates re-run green
[pr-ci-fixer] pushed fix commit abc123f
[pr-ci-fixer] CI green

[ship-pr] merging PR #42 (squash)
PR: https://github.com/org/repo/pull/42 — feat(billing): add stars payment (merged)
```

## When NOT to use

- For draft PRs or PRs that aren't ready to merge — use `gh pr create --draft` directly.
- When you need to push without creating a PR — just `git push`.
- For changes that should split into multiple PRs — split first, ship each.

## Files

```
plugins/ship-pipeline/
├── plugin.json                 manifest
├── README.md                   this file
├── skills/
│   ├── ship-pr.md              /ship-pr workflow
│   └── fix-ci.md               /fix-ci companion
└── agents/
    └── pr-ci-fixer.md          specialist invoked by fix-ci
```
