# Workflow Model

## Phases

1. Intake: sync GitHub context, create task packet, select primary and QA roles.
2. Planning: bound ambiguous, cross-repo, architectural, user-facing, or risky work.
3. Implementation: make one scoped change, run repo-local verification, record evidence.
4. QA: validate against acceptance criteria and return `PASS` or `FAIL`.
5. Review: inspect the diff for bugs, risks, missing tests, and readiness.
6. Closeout: update GitHub fields, comments, PR links, evidence, and follow-ups.

## Retry Policy

Each task gets at most three implementation/QA attempts. Keep scope fixed on retries. After three failures, mark blocked and create an escalation report.

## Branch Discipline

- Never work directly on `main`.
- Use one branch per issue-sized task.
- Avoid cross-repo changes unless explicitly scoped.
- Do not overwrite unrelated user changes.
