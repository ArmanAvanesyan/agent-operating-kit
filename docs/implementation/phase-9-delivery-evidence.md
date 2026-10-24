# Phase 9 delivery evidence

Date: 2026-10-24. Scope: offline/injected implementation in the current working
checkout. The entire Phase 9 live-pilot/production objective is **not complete**.
No live host invocation, credential access, provider write, publication or
production promotion was performed. No release was published.

See the [implementation plan](phase-9-controlled-pilots-production-readiness-plan.md)
and [operator guide](../controlled-pilots-production-readiness.md).

## Delivery matrix

| Work package | Delivered implementation | Separate remaining gate |
| --- | --- | --- |
| P9.0 Contracts | Closed versioned route/readiness/campaign records; exact hashes/scopes; bounded strict readers | Protected external trust custody |
| P9.1 Readiness | Fixed mandatory gate inventory, freshness/revocation interfaces, deterministic reports, exact applicability; native control/acceptance/handoff bridges | A control proof alone never passes P6 conformance; absent native validators remain unknown |
| P9.2 Connected seams | Four typed provider HTTPS writer/readback slices; Codex rate-limit channel; remote verifier protocol; operation-bound milestone resolver; protected CLI service registration | Production service registration/qualification; supported Claude remaining-quota source; no default credential discovery |
| P9.3 Hosts/capacity/verifier | Fixture protocols, guard boundaries and failure tests | Codex live **blocked**; Claude live **independently blocked**; per-account/source collectors and verifier service pending |
| P9.4 Providers/delivery | GitHub comment, Azure revision-tested field, Linear UUID comment, Jira ADF comment; signed readback integration | GitHub, Azure, Linear and Jira live read/write/auth qualification each pending; publication separately pending |
| P9.5 Campaign evidence | Signed curation/audit custody receipts; exact single-route families/repetitions; typed complete episodes and paired account windows; conservative quality bounds | Actual P4-qualified cohort/demand evidence and protected chronology/acceptance verification insufficient/unavailable |
| P9.6 Shadow/comparison | Dedupe, eligible/abstention accounting, no-shadow-dispatch replay, fixed metric semantics/denominators/stop rules | No 100-decision observed eligible shadow corpus or live controlled comparison collected |
| P9.7 Operations | Single-store WAL-consistent quarantined snapshotting; external epoch/consumption boundary; signed bounded drill records; crash/concurrency fixtures and runbook | No qualified anti-rollback witness, cumulative campaign guard, live incident monitor, multi-authority recovery/migration or platform drill certification |
| P9.8 Promotion | Narrow signed principal/role/TTL/revocation-bound grants; singleton pinned standard-risk production enrollment composed with ordinary P7 dispatch and repeated guards | No production grant/enrollment installed; calibration execution remains a separately qualified purpose-specific integration; no deployment authorization |
| P9.9 Review/distribution | CLI/help, guide, fixtures, CI smoke/race additions, independent review and acceptance checks below | Broader support claims require actual per-combination evidence |

The library guard and transport integrations are callable through explicit protected
embedding APIs. The standard binary registers no such production services. Its
readiness/assessment fixtures intentionally return blocking results; execute,
collection, verifier commands and provider mutation do not become live-qualified
by presenting a JSON file or obtaining credentials.

`Controller.DispatchPilotOnce` retains normal P4 selection, P3 reservations and P7
fences/checkpoints. It verifies complete captured input, frozen singleton route,
current native policy/binary/account/configuration/profile/platform, signed current
promotion, cumulative budget authority, activation witness and campaign monitor.
Actual launch rechecks the pinned bindings; monitor/native/grant checks recur in
guarded execution. It cannot run a calibration purpose. Calibration admission has
a distinct signed, bounded, disposable enrollment and atomic external consumption
API, with all non-quality guards mandatory; it grants no production eligibility.

## Independent review

Implementation used GPT-6 Luna medium workers for readiness/evidence, pilot metrics
and transport integration. A separate Luna high reviewer inspected the integrated
result independently. Review found and resolved:

- Invalid not-applicable exemptions, missing/extra scope dimensions, insufficient
  freshness and control-proof versus native-conformance conflation.
- Revoked/superseded acceptance and publication evidence lacking exact target scope.
- Cross-family receipt reuse, incomplete/censored survivor bias and cross-route
  pooling; incorrect averaging of family cost ratios instead of campaign totals.
- Noncanonical metric direction/denominators, fabricated safety rows and unenforced
  stopping thresholds; missing live stop-monitor dependency.
- Calibration scope/budget widening; promotion approver/key attribution, expiry and
  current revocation; runtime route drift between enrollment and launch.
- Credentialed raw transport access, JSON numeric rounding, redirect/privacy
  handling and remote verifier proof/spec/input mutation.

The reviewer reported no remaining material issue in the reviewed closures after
those corrections. This is code review, not independent production authorization
or certification of the external witness, isolation or provider services.

## Acceptance checks

Final acceptance is performed with Go **1.22.12**, `GOTOOLCHAIN=local`, a private Go
cache and canonical `TMPDIR=/private/tmp` on the local macOS host. macOS's default
`/var` temporary alias is a symlink; strict production readers intentionally reject
symlink ancestors. Canonical test roots avoid weakening that invariant.

- Focused readiness/pilot/operations/runtime/connector/collector/verifier/CLI suites.
- Full Go suite, including existing multiprocess accounting, claim/fence and crash
  tests; relevant race suites across P3–P9.
- A new subprocess writer-crash snapshot test retains unresolved obligations from
  committed WAL state and never incorporates an uncommitted attempted release.
- Python regression suite: **267 tests, four existing skips**. Its terminal and
  sandbox-confinement tests require access outside the outer development sandbox;
  that rerun passed. No application gate was relaxed for those tests.
- Kit validation, Python compilation, whitespace/link checks and release packaging.
- Six release targets with CGO disabled: Linux/macOS/Windows, amd64/arm64. These
  builds do not certify execution on untested native platforms.
- Built offline Phase 9 commands with empty PATH, empty credential environment and
  OS-denied network; expected blocked statuses and create-only outputs checked.
- Installer extraction and installed delivery regression checks in a disposable
  home; no change to the operator's installed CLI or configuration.

Commands and retained local logs are recorded during this run under
`/private/tmp/aok-p9-*`. Distribution artifacts remain local test outputs with the
existing unsigned release policy. They are not a public release or deployment.

## Evidence limits and next qualification work

Fixture callbacks and keys are never production roots. `PolicyAuthorityVerifier`,
`QualificationVerifier`, current projection/revocation, protected campaign custody,
`AuthorityWitness`, witness verification, cumulative campaign budget and live
campaign monitor implementations must come from independently controlled operator
infrastructure. Missing or uncertain services block admission. The project-defined
remote-verifier protocol has fixture coverage only.

First connected qualification remains GitHub + Codex after its host/collector/
verifier/identity path is proven. GitHub + Claude requires its independent host and
native-task-mode evidence. Azure, Linear and Jira require their own scoped accounts,
API/authentication conformance and operation-specific readback/recovery evidence.
Comments never qualify state transitions, creation, merge, deployment or completion.
P3 cross-window settlement remains fail-closed. No pilot bypasses unknown quota,
P4 quality floors, native controls, independent acceptance or current authorization.

Publish three separate statuses: **offline/injected P9-core delivered**;
**connected qualification and actual pilots blocked/pending**;
**production promotion pending**. Keep those statuses separate in subsequent
work; no complete-workflow or broad production-readiness claim follows this core.
