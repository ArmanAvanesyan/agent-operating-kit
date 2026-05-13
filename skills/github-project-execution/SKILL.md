---
name: github-project-execution
description: Use when creating task packets from GitHub Project items or issues, syncing project status, reading issue/PR context, or preparing GitHub-ready progress comments.
---

# GitHub Project Execution

Use GitHub Projects as the task queue and state machine. Prefer structured GitHub connector/plugin data when available. Use `gh` for CLI gaps.

## Required Context

Resolve these fields before implementation:

- Project item
- Linked issue
- Linked pull request, if any
- Target repository
- Acceptance criteria
- Current status
- Dependencies and blockers
- Verification path

## Commands

Use the kit CLI when available:

```bash
aok task sync <project-item-or-issue-url>
aok task packet <project-item-or-issue-url>
aok hooks pre <task-packet>
aok hooks after <task-packet>
```

Fallback `gh` commands:

```bash
gh issue view <issue-number> --repo <owner/repo> --comments --json title,body,labels,state,comments,assignees,milestone,projectItems,url
gh pr view <pr-number> --repo <owner/repo> --comments --json title,body,state,headRefName,baseRefName,files,commits,checks,reviews,comments,url
gh project item-list <project-number> --owner <org-or-user> --format json
```

## Update Shape

Post material updates in this format:

```markdown
## Agent Update

Status:
Agent:
Summary:
Verification:
Evidence:
Next:
Blockers:
```

Read `references/github-project-fields.md` when configuring a new GitHub Project.
