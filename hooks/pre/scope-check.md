# pre_scope_check

Verify that a GitHub Project item is ready for agent execution.

## Pass Conditions

- problem is clear
- target repo is known
- acceptance criteria are measurable
- task is issue-sized
- verification path is known
- dependencies and blockers are explicit

## Fail Action

Move or recommend moving the item to `Inbox` or `Blocked`, then comment with the missing information.

## Output

```markdown
Scope Check: PASS | FAIL
Missing:
-
Decision:
- Ready for implementation
- Needs product clarification
- Needs architecture clarification
- Blocked by dependency
```
