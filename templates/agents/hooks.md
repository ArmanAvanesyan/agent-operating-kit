# Hook Contracts

Hooks are lightweight checklists that run before and after agent work. They may be implemented manually, in scripts, or as automation jobs.

Individual hook contracts also live under:
- `hooks/pre/`
- `hooks/after/`

## Pre-Hooks

### `pre_project_sync`

Purpose: load the latest GitHub Project, issue, PR, comments, labels, and status.

Outputs:
- current task packet
- latest issue/PR links
- latest blockers and comments

### `pre_scope_check`

Purpose: ensure work is ready.

Pass conditions:
- acceptance criteria exist
- target repo is known
- task is issue-sized
- dependencies are explicit
- verification path is known

Fail action:
- move item to `Blocked` or back to `Inbox`
- comment with missing information

### `pre_agent_select`

Purpose: select primary and QA/review roles.

Rules:
- choose one primary role unless the task is explicitly cross-functional
- choose one QA/review role
- inspect available specialist-agent catalogs when a narrower specialist is needed

### `pre_worktree`

Purpose: protect local work and branch discipline.

Checks:
- run inside the target repo
- current branch is not `main`
- dirty files are understood
- no unrelated user edits will be overwritten
- base branch and target issue are known

### `pre_risk_check`

Purpose: identify work requiring explicit approval or escalation.

Risk flags:
- secrets or credentials
- migrations
- auth/session behavior
- production deploys
- destructive operations
- cross-repo contract changes
- billing/payment/security-sensitive paths
- public API compatibility

## After-Hooks

### `after_agent_summary`

Record:
- role used
- task attempted
- files changed
- commands run
- verification result
- remaining risks

### `after_qa_verdict`

Record:
- `PASS` or `FAIL`
- acceptance criteria status
- evidence
- retry count
- exact fix instructions on failure

### `after_github_update`

Update:
- project status
- issue comment
- PR link
- evidence link
- next handoff
- blocker reason, if any

### `after_pr_ready`

Run when implementation and QA are done.

Actions:
- summarize change
- include test output
- request code review
- move Project item to `Review`

### `after_escalation`

Run after three failed attempts or a hard blocker.

Actions:
- move item to `Blocked`
- create escalation report
- recommend next decision
- stop implementation until direction changes

### `after_done`

Run when merged or otherwise complete.

Actions:
- move item to `Done`
- link final PR/evidence
- create follow-up issues for deferred work
- archive or link the final task packet
