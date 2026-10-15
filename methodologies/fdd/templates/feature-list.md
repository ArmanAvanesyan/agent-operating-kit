# Prioritized Feature List

Hierarchy: **Subject Area → Business Activity → Feature**. “Business Activity” is the canonical AOK term for an FDD feature set.

Preferred naming:

```text
<action> <result> <by|for|of|to> <object>
```

Example: `Generate PDF for Invoice`

The classic form is also accepted:

```text
<action> the <result> <by|for|of|to> a|an <object>
```

Validate a proposed name with:

```bash
aok methodology validate-feature --name "Generate PDF for Invoice"
```

## Features

| Subject Area | Business Activity | Feature ID | Feature Name | Action | Result | Prep. | Object | Effort Days | Priority | Status |
|---|---|---|---|---|---|---|---|---:|---|---|
| Billing Management | Invoice Generation | BILL-001 | Generate PDF for Invoice | Generate | PDF | for | Invoice | | | |

Each feature must be client-valued and estimated at no more than 10 working days. Split larger work before approval.

## Approval

- Feature-list owners:
- Domain review:
- Chief architect review:
- Project manager approval:
- Approved date:
