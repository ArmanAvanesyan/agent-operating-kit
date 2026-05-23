---
name: ship-pr
description: Run the full PR ship pipeline — simplify, quality gates, push, create PR, watch CI, squash-merge. Optional args: "<title>" "<body>". Use this whenever the user says "ship this" or asks to PR + merge their current branch.
---

# /ship-pr

You are running the project's full PR ship pipeline. Execute these steps in order. Stop on any failure and report.

## Args

- $1 (optional): PR title. If absent, derive from the most recent commit message.
- $2 (optional): PR body. If absent, generate a short summary from the commit messages on this branch.

## Steps

1. **Sanity check** — `git status` and `git log --oneline origin/main..HEAD`. Confirm there are commits to ship. If the branch is named `main`, REFUSE — never PR main into itself.

2. **Simplify** — Invoke the `Skill` tool with `skill: "simplify"`.

3. **Quality gates** — Run in sequence, stop on any failure:
   - `uv run ruff check . --fix && uv run ruff format .` (if `pyproject.toml` exists)
   - `uv run mypy app` or equivalent (read pyproject.toml `[tool.mypy]` to find the target)
   - `uv run pytest tests/unit/ -x -q` or equivalent (read testpaths from pyproject.toml)
   - `uv run bandit -r app/ -t B603,B604,B105,B106,B101,B608` (if app/ exists)
   - If any gate fails, stop and report. Do not push.

4. **Commit any auto-fixes** — If ruff auto-fixed anything, `git add -A && git commit -m "style: ruff auto-fixes"`.

5. **Push** — `git push -u origin HEAD`.

6. **Create PR** — `gh pr create --title "<title>" --body "<body>"`. Capture the PR number from the URL.

7. **Watch CI** — `gh pr checks <number> --watch 2>&1 | tail -30`. If any check fails, invoke `/fix-ci <number>` (the companion skill in this plugin) and wait for it to complete.

8. **Merge** — Once all checks are green, `gh pr merge <number> --squash --delete-branch`. If the local delete fails because the branch is in use by a worktree, ignore — the remote merge + remote branch delete succeeded.

9. **Report** — End your response with `PR: <url>` and a one-line summary.

## Refuses / safety

- Never use `--no-verify` to bypass hooks.
- Never force-push to main.
- Never merge a PR with red CI.
- If `gh` is not authenticated, stop and tell the user to run `gh auth login`.

## Test plan

Run on a branch with a known-good change:
- All gates pass → PR created → CI green → merge succeeds → returns PR URL.
- Test failure → gates fail at pytest → no push, no PR created.
