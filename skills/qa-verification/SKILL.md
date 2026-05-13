---
name: qa-verification
description: Use when validating task completion, collecting evidence, producing QA verdicts, checking acceptance criteria, or deciding retry versus escalation.
---

# QA Verification

Validate what changed against acceptance criteria, not implied intent. Require direct evidence.

## Process

1. Read the task packet and acceptance criteria.
2. Identify the changed behavior and verification command or artifact.
3. Run or inspect the relevant evidence path.
4. Mark each acceptance criterion pass/fail.
5. Return `PASS` or `FAIL`.
6. On failure, provide exact fix instructions and affected files.

## Verdict

```markdown
Verdict: PASS | FAIL
Attempt: N of 3

Acceptance Criteria:
- [x]
- [ ]

Evidence:
-

Issues:
- Severity:
  Expected:
  Actual:
  Evidence:
  Fix instruction:
  Files:
```

After three failed implementation/QA attempts, recommend escalation instead of another retry.
