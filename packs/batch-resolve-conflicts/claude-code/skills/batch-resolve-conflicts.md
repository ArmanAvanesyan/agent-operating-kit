---
name: batch-resolve-conflicts
description: Given a comma-separated list of open PR numbers, spawn one parallel rebase agent per PR. Each agent rebases its PR onto origin/main, resolves common conflict patterns (pyproject.toml merges, repositories/__init__.py = take main, settings updates = additive merge), runs quality gates, force-pushes, and watches CI. Use when several PRs collide with main after a batch of merges. Args: PR-numbers as comma-separated integers, e.g. "4,5,9,11".
---

# /batch-resolve-conflicts

You are coordinating parallel conflict resolution across N open GitHub PRs that have diverged from `main`.

## Args

- `$1` (required): comma-separated PR numbers, e.g. `"4,5,9,11"`.

## Steps

### 1. Validate

For each PR number in `$1`:
- `gh pr view <N> --json state,mergeable,headRefName` must return:
  - `state: OPEN` (skip otherwise)
  - `mergeable: CONFLICTING` (note any that aren't — they may not need rescue)
- Capture the `headRefName` for each.

If none are CONFLICTING, report `No conflicting PRs in the list` and stop.

### 2. Pre-fetch shared context

Run these once, save the output for sub-agent prompts:
- `git fetch origin`
- `git show origin/main:pyproject.toml > /tmp/canonical-pyproject.toml 2>/dev/null || true`
- `git show origin/main:app/db/repositories/__init__.py > /tmp/canonical-repos-init.py 2>/dev/null || true`
- For each conflicting PR, identify the conflicting files:
  ```bash
  git merge-tree $(git merge-base origin/main origin/<headRefName>) origin/main origin/<headRefName> 2>/dev/null | grep -A2 "^changed in both" | head -20
  ```

### 3. Spawn one sub-agent per conflicting PR — in PARALLEL (single Agent tool block with multiple invocations)

Each sub-agent's prompt must include:

- The PR number and its head branch name
- The list of files that conflict with main
- The canonical versions of well-known shared files (paste the contents)
- The resolution rules:
  - `pyproject.toml` → start from origin/main, then port the PR's unique additions (new dev deps, new mypy overrides, new coverage omits — never duplicate existing entries)
  - `app/db/repositories/__init__.py` → always take origin/main (it has the canonical re-export list)
  - `app/db/session.py` → always take origin/main
  - `app/pipeline/<stage>/__init__.py` → take origin/main if the PR predates that stage's real implementation
  - `app/storage/__init__.py` → take origin/main
  - PR-specific files (the unit's own new code) → keep the PR's version
- The quality gates: `uv run ruff check . --fix && uv run ruff format . && uv run mypy app/ && uv run pytest tests/unit/ -x -q`
- The push command: `git push --force-with-lease origin <headRefName>`
- The merge command (only after CI green): `gh pr checks <N> --watch && gh pr merge <N> --squash --delete-branch`
- Final report line: `PR: <url>` or `BLOCKED: <reason>`

Use `subagent_type: "general-purpose"` for each. Use `isolation: "worktree"` only if the PRs touch overlapping files in your local checkout — otherwise direct cloning into per-PR temp dirs is fine.

### 4. Render initial status table

```
| # | PR | Branch | Status |
|---|----|--------|--------|
| 1 | #4 | <branch> | running |
| 2 | #5 | <branch> | running |
| ... |
```

### 5. As each sub-agent reports, update the table

Parse the `PR: <url>` or `BLOCKED: <reason>` line. Mark each as `merged` or `blocked`.

### 6. Final report

Once all sub-agents have reported, render the final table and a one-line summary like:
`N/N PRs rebased and merged.` or `M/N rebased and merged; <list> blocked: <reasons>`.

If any are blocked, recommend next steps (e.g., "spawn a code-reviewer agent on PR #X", "user intervention required for PR #Y").

## Safety / refuses

- Never use `git push --force` without `--force-with-lease`.
- Never skip hooks (`--no-verify`) when committing the rebase resolution.
- Never merge a PR whose CI is red — wait or block.
- Never modify the conflict-resolution rules on the fly without informing the user; if a PR has a conflicting file not in the canonical list, escalate rather than guess.

## When to use this skill

- After merging a batch of PRs to main, several remaining open PRs show "Conflicting" in the GitHub UI
- You explicitly know the PRs share common conflict patterns (pyproject.toml, shared __init__.py files)
- You want parallel work to finish faster than serial rebasing

## When NOT to use this skill

- A single PR with complex, app-specific conflicts (use a code-reviewer subagent directly)
- PRs whose conflicts are in business-logic files (the canonical-version rules don't apply)
- Conflicts that require user judgment on which side to keep
