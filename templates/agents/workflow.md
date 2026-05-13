# Agent Workflow

## Status Flow

Use this status model for GitHub Project items:

1. `Inbox`: untriaged request.
2. `Ready`: scoped task with acceptance criteria and target repo.
3. `In Progress`: implementation underway.
4. `QA`: implementation complete, awaiting evidence-based validation.
5. `Review`: PR ready for code review.
6. `Blocked`: cannot proceed without a decision or dependency.
7. `Done`: merged, verified, and project item updated.

## Phase Flow

### 1. Intake

Owner: `orchestrator`

Done when:
- task packet exists
- primary role and QA role are selected
- acceptance criteria are testable
- target repo is known

### 2. Planning

Owners: `product-manager`, `project-manager`, `software-architect`

Use this phase when the task is ambiguous, cross-repo, architectural, user-facing, or risky.

Done when:
- scope is issue-sized
- non-goals are explicit
- technical approach is bounded
- dependencies and rollback are documented where needed

### 3. Implementation

Owner: `implementer`

Rules:
- work in one target repo at a time unless the task packet says otherwise
- preserve behavior outside the task scope
- run repo-local verification
- record changed files and commands

Done when:
- acceptance criteria appear satisfied
- tests/build/typecheck relevant to the change have run or are explicitly blocked
- implementation summary is ready for QA

### 4. QA

Owner: `qa-reality-checker`

Rules:
- validate against acceptance criteria, not intent guesses
- require evidence
- return `PASS` or `FAIL`
- on `FAIL`, provide exact fix instructions and affected files

Done when:
- task passes, or
- retry count reaches 3 and an escalation packet is created

### 5. Review

Owner: `code-reviewer`

Rules:
- prioritize bugs, security, regressions, missing tests, and maintainability risks
- avoid style-only blocking
- tie findings to files and lines when possible

Done when:
- PR is ready to merge, or
- findings are returned to implementation

### 6. Closeout

Owner: `orchestrator`

Done when:
- GitHub Project fields are current
- PR/issue comments include evidence and final summary
- follow-up issues exist for deferred work
- task packet is archived or linked

## Retry Policy

Each task gets at most three implementation/QA attempts.

On each failure:
1. Increment `Attempt`.
2. Keep scope fixed.
3. Return only the QA failure details to implementation.

After three failures:
- set status to `Blocked`
- create escalation report
- recommend one of: decompose, reassign, revise approach, defer, or accept with limitations
