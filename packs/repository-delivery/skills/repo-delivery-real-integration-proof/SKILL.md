---
name: repo-delivery-real-integration-proof
description: Run and record source-bound integration evidence with explicit real, mock, or mixed service coverage and environment blockers.
---

# Establish integration evidence

Read the [integration guide](../../docs/integrations.md) and the target repository's gate definitions. Use the bundled [helper](../../scripts/repo-delivery) to retain evidence outside the tested source snapshot.

Identify the required service boundary and the observable behavior that proves it. Record service versions, relevant environment, exact commands, and whether each boundary is real, mocked, or mixed. Never relabel mocks as real because a surrounding test process or container ran. A user-supplied service description is an assertion; connect it to captured test output and the actual environment.

Run required tests against the intended services, then bind results to the final source snapshot and environment. Record unavailable Docker, credentials, network, or service versions as concrete blockers when they prevent the required proof. Preserve useful mock results while reporting the missing real coverage. Fixes invalidate affected prior results and require rerunning the relevant tests.

Do not provision services, publish telemetry, install hooks, or mutate production merely to improve a proof label without user authority. Report the command outcomes, covered boundaries, version evidence, blockers, and remaining uncertainty. A local receipt does not certify remote CI or authenticate an external service.

For managed swarmd handoffs, test the actual `task.finalize` capability with a fixture-created recorded leased attempt and a fresh database. Cover accepted outcomes and expired, revoked or mismatched credentials and evidence. Exported references alone do not establish live ownership. Never reuse a user's active queue or print lease credentials. The adapter must fail closed without a legacy completion fallback or automatic retry after an uncertain response. See [runtime ownership](../../docs/integrations.md#swarmd-runtime-references).
