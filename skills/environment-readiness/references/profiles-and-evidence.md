# Profiles and evidence

| Profile ID | Required capabilities |
|---|---|
| `inspection` | Instructions, relevant file access, search and Git inspection where applicable |
| `local-development` | Required runtime/dependencies, build and relevant local checks |
| `service-integration` | Development capabilities plus necessary services/fixtures and integration checks |
| `application-verification` | Running application plus required API/browser scenarios and test identities |
| `delivery` | Required artifacts/CI/remote prerequisites for the actual revision and authorized delivery |

Dimensions: context (commands/boundaries discoverable), verification (behavior checkable),
feedback (failures diagnosable). Inspection requires context; other profiles require all
three dimensions. Enumerate task-specific capabilities, not only one generic check per
dimension. Lower-profile evidence may be reused only while still applicable.

Outcomes are pass, fail, blocked, not-checked and not-applicable. A required check must pass
with evidence; not-applicable does not waive a required check. To make a capability optional,
state why the task does not require it and explicitly set required false. Report optional
failures too, without conflating them with required blockers.

The assessment helper accepts this **skill-local evidence format**, not a new task-packet
field or AOK schema. References must be independently inspected by the agent. Example:

```json
{
  "task": "Inspect the CLI architecture",
  "profile": "inspection",
  "revision": "actual-inspected-revision",
  "environment": "local checkout, OS and relevant tool versions",
  "checks": [
    {
      "id": "repository-context",
      "dimension": "context",
      "required": true,
      "outcome": "pass",
      "evidence": ["exploration report with source paths and instruction coverage"]
    }
  ]
}
```

```bash
python3 scripts/assess.py /path/to/report.json --expected-revision ACTUAL_REVISION
```

Exit 0 means supplied records are consistently ready; exit 1 means blocked/not-ready;
exit 2 indicates invalid input. A required failure produces not-ready, an unexecuted or
unsupported required capability produces blocked. Missing required dimensions, missing
pass evidence and a revision mismatch block readiness. The helper does not infer the
complete task inventory, validate evidence files or discover the current revision itself.
A caller must not use it to launder self-reported success into observed evidence.
