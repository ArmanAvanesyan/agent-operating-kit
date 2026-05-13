# pre_risk_check

Identify work that needs approval, escalation, or extra review.

## Risk Flags

- secrets or credentials
- database migrations
- authentication or session behavior
- production deploys
- destructive commands
- cross-repo contracts
- billing, payment, security, or privacy-sensitive code
- public API compatibility

## Output

```markdown
Risk Check: LOW | MEDIUM | HIGH
Flags:
-
Required Approval:
Extra Review Role:
Rollback Needed:
```
