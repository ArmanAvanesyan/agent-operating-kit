# GitHub Project Contract

GitHub Project items are the task queue and state machine for agent work.

## Required Project Fields

Recommended fields:
- `Status`: `Inbox`, `Ready`, `In Progress`, `QA`, `Review`, `Blocked`, `Done`
- `Phase`: `Intake`, `Planning`, `Build`, `QA`, `Review`, `Release`, `Operate`
- `Priority`: `Critical`, `High`, `Medium`, `Low`
- `Primary Agent`
- `QA Agent`
- `Target Repo`
- `Attempt`
- `PR URL`
- `Evidence URL`
- `Blocker Reason`
- `Next Handoff`

If GitHub Project fields do not exist yet, keep the same values in the issue body or task packet until the fields are added.

## Item Intake Checklist

Before moving an item to `Ready`, confirm:
- the problem is clear
- target repo is known
- acceptance criteria are measurable
- non-goals are explicit when scope could expand
- dependencies are known
- risk flags are listed
- verification command or evidence type is known

## GitHub CLI Patterns

List project items:

```bash
gh project item-list <project-number> --owner <org-or-user> --format json
```

View an issue:

```bash
gh issue view <issue-number> --repo <owner/repo> --comments --json title,body,labels,state,comments,assignees,milestone,projectItems,url
```

View a PR:

```bash
gh pr view <pr-number> --repo <owner/repo> --comments --json title,body,state,headRefName,baseRefName,files,commits,checks,reviews,comments,url
```

Discover current branch PR:

```bash
gh pr status --json currentBranch
```

Inspect CI:

```bash
gh run list --branch <branch> --repo <owner/repo>
gh run view <run-id> --repo <owner/repo> --log-failed
```

## Issue Body Template

```markdown
## Problem

## Scope

## Acceptance Criteria
- [ ]

## Target Repo

## Suggested Agent Roles
- Primary:
- QA:

## Verification

## Risks / Blockers

## Links
- Project item:
- PR:
- Evidence:
```

## Project Update Rules

After every material step, update:
- item status
- attempt count
- PR URL
- evidence URL or comment
- blocker reason, if blocked
- next handoff

Never leave a task in `In Progress` when the active blocker is known. Move it to `Blocked` and state the decision needed.
