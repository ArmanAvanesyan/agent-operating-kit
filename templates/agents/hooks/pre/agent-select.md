# pre_agent_select

Select the primary and QA/review roles for the task.

## Steps

1. Identify the dominant deliverable.
2. Inspect the relevant role card under `agents/roles/`.
3. Inspect available specialist catalogs when a narrower specialist is needed.
4. Choose one primary role and one QA/review role.
5. Record rationale in the task packet.

## Defaults

- implementation: `implementer`
- validation: `qa-reality-checker`
- code review: `code-reviewer`
- CI/deploy: `devops-sre`
- ambiguity: `product-manager` or `software-architect`

## Output

```markdown
Primary Agent:
QA/Review Agent:
Specialist Source:
Rationale:
```
