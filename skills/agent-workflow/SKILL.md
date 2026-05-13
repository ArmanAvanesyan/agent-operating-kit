---
name: agent-workflow
description: Use when a task needs the reusable GitHub Project driven agent workflow, status model, role handoffs, pre-hooks, after-hooks, retry policy, or task packet lifecycle.
---

# Agent Workflow

Use this skill to run work as one issue-sized slice with GitHub Projects as the external source of truth.

## Workflow

1. Sync the GitHub Project item, linked issue, linked PR, comments, labels, and current status.
2. Build or update one task packet from `agents/tasks/task-template.md`.
3. Select one primary role and one QA or review role from `agents/roles/`.
4. Run pre-hooks before edits: project sync, scope check, agent select, worktree check, risk check.
5. Execute only the scoped slice.
6. Run verification and after-hooks.
7. Update GitHub with status, evidence, PR link, blockers, and next handoff.
8. Escalate after three failed implementation/QA attempts.

## Status Model

Use these statuses unless the target project defines its own equivalent names:

- `Inbox`: untriaged request.
- `Ready`: scoped task with acceptance criteria and target repo.
- `In Progress`: implementation underway.
- `QA`: implementation complete and awaiting validation.
- `Review`: pull request ready for code review.
- `Blocked`: cannot proceed without a decision or dependency.
- `Done`: merged, verified, and updated.

## Rules

- Do not implement before acceptance criteria, target repo, and verification path are known.
- Do not work directly on `main`.
- Keep one issue-sized branch per task.
- Do not bundle unrelated repos unless the task explicitly requires it.
- Evidence is required before advancing status.
- Preserve user changes and unrelated dirty work.

For field details, read `references/workflow-model.md` only when you need a fuller handoff model.
