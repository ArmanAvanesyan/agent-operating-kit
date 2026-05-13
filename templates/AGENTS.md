# AGENTS.md

Workspace-level instructions for AI agents working in this project.

## Source of Truth

Use GitHub Project items as the execution source of truth.

For each task, resolve:
- GitHub Project item
- linked issue
- linked pull request, if any
- target repository
- acceptance criteria
- current status
- dependencies and blockers

Prefer structured GitHub connector/plugin data. Use local `git` and `gh` when connector coverage is incomplete.

## Operating Model

Use the dedicated operating model under `agents/`:
- `agents/workflow.md`: phase flow and quality gates.
- `agents/github-project.md`: project fields, task contract, and CLI patterns.
- `agents/hooks.md`: pre-hook and after-hook contracts.
- `agents/tasks/task-template.md`: task packet template.
- `agents/roles/`: role cards for orchestration, planning, implementation, QA, review, and operations.

## Default Agent Loop

1. Sync the GitHub Project item and linked issue.
2. Build a task packet from `agents/tasks/task-template.md`.
3. Select one primary role and one QA/review role.
4. Run pre-hooks from `agents/hooks.md`.
5. Execute exactly one issue-sized slice.
6. Run verification and after-hooks.
7. Update GitHub Project status, comments, PR links, and evidence.
8. Escalate after three failed implementation/QA attempts.

## Branch and Change Discipline

- Never work directly on `main`.
- Keep one issue-sized slice per branch.
- Do not bundle unrelated repositories in one branch unless the GitHub task explicitly requires a cross-repo change.
- Do not mix architecture refactors with unrelated bug fixes.
- Do not overwrite user changes or revert unrelated dirty work.

## Quality Gates

Every task must have:
- acceptance criteria
- target repository and files/areas
- implementation evidence
- verification evidence
- GitHub update
- next handoff or closure note

Frontend/UI work needs screenshot or browser evidence. Backend/API work needs tests or request/response evidence. Infrastructure work needs validation or dry-run output where possible.

## Escalation

Escalate instead of continuing when:
- acceptance criteria are missing or contradictory
- target repository is unclear
- secrets or production credentials are needed
- the change is destructive or production-impacting
- QA fails three times
- implementation requires architecture changes beyond the issue scope
