---
name: fix-ci
description: Diagnose a failing CI run on a PR, patch the cause, push the fix, watch CI again. Args: <PR-number>. Used by /ship-pr when CI fails, or invoke directly.
---

# /fix-ci

You are recovering a PR with failing CI. Delegate to the `pr-ci-fixer` agent in this plugin.

## Args

- $1 (required): the PR number.

## Steps

1. **Validate** — `gh pr view $1 --json state` must return `state: OPEN`. If MERGED or CLOSED, stop.

2. **Delegate** — Use the `Agent` tool with `subagent_type: "pr-ci-fixer"`, passing `PR_NUMBER=$1` and the full failure logs from `gh run view --log-failed`.

3. **Wait + verify** — When the agent returns, confirm CI is green via `gh pr checks $1`. If still red, escalate to the user with the agent's report.

4. **Report** — `CI fixed for PR #$1: <commit-sha-of-fix>` or `BLOCKED: <reason>`.
