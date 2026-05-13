---
name: code-review
description: Use when reviewing changes for correctness, regressions, security, maintainability, performance, missing tests, or PR readiness.
---

# Code Review

Review against task scope and user-visible behavior. Findings come first, ordered by severity.

## Priorities

- Correctness bugs and behavioral regressions
- Security, privacy, auth, and secret-handling risks
- Missing verification for changed behavior
- Maintainability risks that affect future changes
- Performance risks when the task touches hot paths

## Output

Use this shape:

```markdown
Findings
- Severity: Blocker | Suggestion | Nit
  File:
  Line:
  Issue:
  Fix:

Open Questions
- 

Summary
- 

Verification Gaps
- 
```

Avoid style-only blocking comments. Tie findings to concrete files and lines when possible.
