---
name: environment-readiness
description: Assess or prepare the development environment needed for a selected task, verify required tools/services/checks, and report ready or blocked with evidence. Use for project setup failures, missing prerequisites, or determining whether build, integration, application, or delivery work can run.
---

# Environment Readiness

Establish readiness for the selected task, not every possible project activity.

1. Reuse project exploration findings or discover missing prerequisites. Select the minimum
   profile from [profiles and evidence](references/profiles-and-evidence.md).
2. Inspect declared runtimes, lockfiles, setup commands, configuration names and services.
   Distinguish installed tools from compatible, usable tools.
3. Perform necessary setup within the user's existing authorization and repository policy.
   Preserve changes, inspect unfamiliar scripts, use project-local tooling and avoid exposing
   secrets. Do not switch package managers or install global tools merely for convenience.
4. Run relevant smoke/build/scenario checks and record actual outcomes at the current revision.
   A successful bootstrap helper exit alone does not establish application readiness.
5. Deliver readiness separately from exploration completion, including blockers and cleanup.
   Use [the report template](assets/readiness-report.md) if no canonical report exists.

Use existing `project-bootstrap` for AOK operating-file initialization when available; it
is not application dependency setup. Use existing QA guidance for consequential acceptance.
These are optional integrations, not prerequisites to assess another repository.

`scripts/assess.py` checks a supplied JSON evidence record against consistency rules; see
[profiles and evidence](references/profiles-and-evidence.md) for its local format. It does
not execute commands, inspect referenced evidence, install dependencies or certify truth.
Read [setup and recovery](references/setup-and-recovery.md) when making environment changes.
