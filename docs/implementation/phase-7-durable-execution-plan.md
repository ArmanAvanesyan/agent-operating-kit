# Phase 7 — Durable execution and quota reservations

Status: implementation plan, 2026-10-24. No runtime capability or live
qualification is established by this document. Two GPT-6 Luna workers at medium
reasoning reviewed the existing accounting, selection, planning and host seams.
A separate Luna-high reviewer checked the resulting plan against the APIs; its
permission-drift and exact-policy-resume acceptance findings are incorporated.

## Outcome and delivery gates

Implement an opt-in, bounded, local execution controller for a fixed AOK plan:
validated captured inputs → eligible step → fenced claim → isolated staging →
revalidated selection → atomic local capacity reservation → durable launch
intent → bounded host attempt → preserved candidate output → reconciliation.

Publish separate statuses:

| Gate | Current status and completion requirement |
|---|---|
| P7-core | Planned. Deterministic local state machine, accounting integration, fake-host execution and crash/recovery acceptance must pass. |
| Codex P6 live | **Blocked**. Installed 0.162.1 discovery passed; fresh trusted signed filesystem/network/ambient-control proofs and successful live qualification are absent. |
| Claude P6 live | **Blocked**. Installed 2.1.303/OAuth discovery passed; the same control-proof and live-conformance requirements remain unmet. |
| P7 connected Codex path | Blocked on Codex P6, current usable P3 observations, qualified P4 task evidence and scoped runtime authorization. |
| P7 connected Claude path | Independently blocked on Claude P6 and the same evidence/admission requirements. |
| P8 verification/delivery/writeback | Pending; host completion remains a candidate result. |

A passing host path may be reported individually; overall P7 live promotion
requires both planned host paths. Fixture replay, version discovery, compilation,
manual model pins and reservation tokens cannot satisfy those gates. P5 live-read
conformance also remains pending per provider. Offline P7 consumes captured P5
snapshots; any claim of connected tracker-to-host operation additionally requires
the relevant P5 live-read gate.

## Fixed scope and architecture

1. One local operator/runtime authority on one machine. Use a private local
   SQLite runtime ledger with explicit migrations, audit history and CAS state
   transitions. No network filesystem, distributed scheduler, daemon, remote
   worker, child-agent dispatch or automatic background loop in v1.
2. Reuse `internal/usage/store` as the sole local capacity accountant. Its
   `Reserve`, `ConfirmLaunch`, `Renew`, `ReleaseUnlaunched`, `Settle`, `Reconcile`
   and `Recover` already implement accounting transactions and fencing. Expose
   its concrete `MarkUncertain` through the existing interface. Do not create a
   second quota allocator or change P1–P6 wire formats.
3. Implement `internal/runtime` for lifecycle/admission and
   `internal/runtime/store` for durable task state. Runtime and accounting stores
   are separate transaction domains: use a durable, idempotent handoff protocol,
   not a claim that they commit atomically together.
4. Add narrow P6 integration interfaces for a caller-allocated attempt ID,
   admission guard, actual host-start hook and observed process/session events.
   Preserve existing P6 interfaces via wrappers. Runtime-controlled execution
   requires these hooks; pre/post `AttemptGuard.Check` alone is insufficient.
5. Use explicit isolated worktrees/staging and existing preservation helpers.
   Never dispatch into the user's current checkout. Staging is not a sandbox;
   scoped external enforcement proofs remain mandatory. Preserve user files and
   every attempt's dirty/untracked changes before cleanup or takeover.
6. Keep swarmd separate. Its repository mapping is unpinned and does not prove
   compatible leases, fences or cancellation. The local reference backend is
   the P7 implementation target. An optional swarmd adapter needs its own pinned
   capability/conformance gate and must replace, rather than compete with, task
   authority. This intentionally extends AOK with an opt-in local backend; it
   does not claim the existing documentation is a working swarmd integration.
7. Consume immutable tracker bindings/revisions. No tracker mutation, automatic
   source refresh, billing change, subscription reset redemption, API-billing
   fallback, merge/deploy or accepted/Done transition.

## Contracts and identity

Introduce independently versioned closed `aok-runtime/v1` envelopes, plus a
versioned lifecycle translation. Reuse P1 `RunRecord`, `HostTaskBinding`, existing
`SelectionDecision`, reservation records and host receipts rather than duplicate
their definitions.

| Envelope | Required binding |
|---|---|
| RuntimePolicy | Authority identity, approved repositories/roots and routes, concurrency/lease/time/attempt bounds, trusted policy/control/qualification references, retry/checkpoint rules, verifier evidence accepted for dependency gates |
| RunManifest | Exact plan/goal, assignments, packet/input revisions, P5 binding/revision, repository snapshot and P2–P4 inputs/policies |
| StepClaim / AttemptEnvelope | Preallocated run/step/attempt ID, runtime epoch/owner/fence, accounting token/epoch/fence, configuration/profile, decision/demand and all captured hashes |
| AdmissionReceipt | Every gate, evidence validity, reservation/ledger revision and narrow authorization scope; never blanket execution readiness |
| RuntimeEvent | Unique event ID, expected revision, authority/fence, operation key, occurrence/recording time, event payload digest and previous audit hash |
| Checkpoint | Attempt/input/plan/repository/environment identity, tracked patch and allowed untracked assets, artifact hashes, native IDs, quiescence and resume disposition |
| RecoveryReport | Known/unknown launch, stop, usage and effect observations; outstanding holds; safe next action and required evidence |

A launch permit is created only by the trusted admission path for one exact
attempt/configuration/profile/workspace, owner/fence and validity interval. It
cannot be manufactured from task text, a JSON boolean or a P4 recommendation.
P2–P6 reports keep their existing authority/readiness flags false. Runtime
admission does not imply independent acceptance; candidate results remain
`accepted:false`.

Runtime phases distinguish pending, eligible, claimed, staged, reserved,
launch-intent, running, awaiting-verification, blocked, exhausted, cancelled and
failed. Keep the richer phase in the P7 envelope and map to existing P1
vocabulary explicitly. `blocked` is recoverable; a host's completed turn cannot
satisfy an accepted-dependency predicate. P8 will produce new verification and
acceptance evidence. In P7 tests, any supplied verifier evidence is explicitly
synthetic; production dependency gates require approved revision-bound evidence.

## Admission and crash-safe ordering

1. Validate the pinned corpus and evaluate readiness through `planning.Ready`.
   Atomically claim the exact run/step/input revision in the runtime ledger.
2. Preserve the repository baseline, stage an isolated workspace, and persist its
   immutable identity. Prepare the exact assignment through P6. Proofs must match
   the actual staged workspace; they cannot be copied from another path.
3. Read the current accounting snapshot. Re-evaluate P4 using that snapshot,
   current trusted time and exact task/candidate/context evidence. Materialize
   demand against current P3 constraint generations. Pins/preferences never
   waive quality, capability, data-policy or capacity gates.
4. Persist reservation intent and a stable operation key. Call existing atomic
   `Reserve` with its expected ledger revision. Store the returned accounting
   token together with the claim and all selection/demand hashes. On a ledger
   conflict, reread/recompute; never reuse a key with changed request bytes.
5. Revalidate claim, policy, host qualification, controls, evidence freshness and
   permit before launch. Persist runtime launch intent, then commit accounting
   `ConfirmLaunch`. The latter rechecks capacity excluding its own hold.
6. Only after both durable intents and all guards succeed may the inference
   subprocess start. Record native/process identities as observed. Keep
   non-inference preflight subprocesses distinct from an inference launch.
7. Renew both ownership domains and monitor cancellation, deadlines and controls
   while the attempt is active. At every checkpoint/result/state promotion,
   atomically compare current runtime fence and verify accounting ownership.
   Expired/stale owners cannot publish authoritative results.
8. Preserve output/patches, verify hashes and write an attempt observation.
   Settle only with complete attributable actuals for every reserved constraint.
   Missing percentage usage cannot become token-derived percentages or zero.
   Otherwise retain conservative uncertain exposure and request reconciliation.

The runtime journal records accounting intents/results with stable IDs so a
restart can resolve a commit whose reply was lost. A crash between stores can
leave a claim or conservative hold; it must never permit an unfunded launch.
A confirmed launch intent is potentially launched even if no native ID was
recorded. Release a held reservation only with durable proof that launch was
never possible. Missing launch acknowledgement is not such proof.

Fences stop stale state/artifact promotion, not arbitrary process writes.
Heartbeat loss triggers cancellation and quarantines outputs. Do not start a
replacement writer until the previous process is observed stopped or a proven
isolation boundary makes it unable to affect the new attempt. Unknown stop
remains blocked. Lease expiry never proves process termination or refunds quota.

## Capacity, defaults and continuation

- Default max active host attempt: one; default model retries after possible
  launch: zero. No fallback edge is invented. Administrative DB-contention
  retries are bounded to three within five seconds and cannot relaunch a host.
- Initial claim lease: 30 seconds; heartbeat target: five seconds. Renewal is
  bounded by demand and control validity and policy limits. Clock regression,
  missed renewals or unavailable accounting authority fail closed. Use trusted
  UTC for persisted validity and monotonic elapsed time for in-process budgets.
- Retain P6 invocation limits: at most 60 seconds per normal invocation and
  30 seconds for smoke. A higher P7 budget cannot extend those transport limits.
- Freshness/reset logic comes from P3, not duplicated scheduler arithmetic.
  Known future reset hints produce a wait/refresh action, never future capacity.
  Time passage alone cannot make new quota observations or advance generations.
- Recovery retains uncertain holds across resets. Existing P3 settlement blocks
  unresolved cross-generation allocation. V1 keeps that blocker until explicit
  attributable evidence exists; it does not distribute usage heuristically.
- Unknown live capacity or insufficient P4 quality remains a blocker even if P6
  is later qualified. Fixture credentials/issuers cannot qualify production data.
- Reselection occurs only at an explicit quiescent checkpoint under policy. A
  configuration/profile/input change creates a new attempt and fresh preparation,
  selection and reservation. It does not mutate the old native session identity.
- Exact P6 interrupted-session resume is allowed only after reconciliation and
  matching captured inputs. Completed/failed/ambiguous attempts are not blindly
  resubmitted. Checkpoint continuation with a changed workspace/control policy
  uses a new session unless separately proven compatible. P6 currently also binds
  the exact policy digest: replacing expired proof assets changes that digest,
  so an old receipt cannot simply resume under a renewed policy. V1 must block
  or create a preserved checkpoint continuation in a new session; loosening
  that invariant requires a separate conformance-tested P6 change.
- Retain every cancellation, failed attempt and unresolved hold in the audit.
  `cancel` durably closes new admission before asking the host to stop; stop
  confirmation and final usage remain independently known or unknown.

## Work packages and dependencies

| Package | Dependencies | Deliverable and acceptance criteria |
|---|---|---|
| P7.0 Contracts/ownership ADR | P1, P4, P6 core | Closed runtime schemas, lifecycle table, stable attempt allocation, backend and two-store protocol. Reject unknown versions/transitions and forged authority; no P1–P6 format changes. |
| P7.1 Durable ledger | P7.0 | Private SQLite schema, explicit migrations, CAS claims/events, epochs/fences, idempotent command keys and audit integrity. Competing controllers cannot claim the same step; changed-key payloads conflict; corrupt state/clock regression blocks. |
| P7.2 Staging/checkpoints | P7.0–1, preservation helpers | Immutable repository baseline, isolated attempt workspace, hashed checkpoints. Dirty/untracked source bytes survive failure/cancel; symlink/path escapes and premature cleanup fail closed. |
| P7.3 Admission/accounting seam | P7.1, P2–4 | Revalidate exact selection against current ledger; materialize current-generation demand; journal atomic P3 reservation and launch confirmation. Shared simultaneous windows admit all-or-none; stale decisions/revisions cannot launch. |
| P7.4 Host/fence seam | P7.1–3, P6 core | Caller-owned attempt IDs, mandatory runtime guard at actual inference start, heartbeat/cancel and fenced result promotion. Missing/expired/stale permit yields zero inference starts; stale controller cannot promote output or settle. |
| P7.5 Reconciliation/recovery | P7.3–4 | Crash replay, launch ambiguity handling, process/session reconciliation and conservative usage settlement. Crash at each boundary preserves patches and uncertainty; no automatic duplicate launch or release on expiry. |
| P7.6 Operator lifecycle | P7.2–5, planning | Bounded dispatch, status/explain/resume/cancel/reconcile/export. Blocked remains recoverable; cancellation closes admission immediately; every decision explains references, gates and next evidence. |
| P7.7 Review/distribution | Integrated core | Independent review, fault-injection suites, offline commands, all regression/distribution checks and operator recovery guide. Core and connected results reported separately. |
| P7.8-CX Connected Codex | P7.7, Codex P6 live, usable P3/P4 evidence | Authorized disposable live launch/cancel/resume/recovery; actual model, workspace controls and accounting bindings verified. Currently blocked. |
| P7.8-CL Connected Claude | P7.7, Claude P6 live, usable P3/P4 evidence | Separate equivalent qualification, including native-task capability variants if claimed. Currently blocked. |

P5 provider conformance is an additional dependency only for a claimed connected
tracker path; a fixture import is sufficient for offline core integration. No
P7 package may silently promote a P5/P6 pending gate.

## Proposed Go CLI

```text
aok runtime init --policy FILE --state NEW-DIRECTORY
aok runtime submit STATE --manifest FILE [--json]
aok runtime validate STATE [--json]
aok runtime dispatch STATE --run ID --once [--json]
aok runtime status STATE [--run ID] [--json]
aok runtime explain STATE --run ID [--json]
aok runtime cancel STATE --run ID --reason TEXT [--json]
aok runtime resume STATE --run ID --checkpoint ID [--json]
aok runtime reconcile STATE --run ID --evidence FILE [--json]
aok runtime export STATE --run ID --out NEW-DIRECTORY [--json]
aok runtime replay --scenario FILE --as-of RFC3339 --out NEW-DIRECTORY [--json]
```

Dispatch is foreground and bounded; it does not install a background service.
Resume admits at most one policy-authorized continuation, not a hidden daemon.
Read-only commands do not contact hosts or providers. Synthetic replay never
launches a real CLI. Only replay accepts an arbitrary evaluation clock; live
admission uses the trusted clock. Mutating commands use durable operation IDs,
new output directories use existing private non-overwriting evidence writers,
and unsafe paths/symlinks/duplicate JSON keys fail closed. Preserve exit codes
0 success, 1 invalid/blocked/conflict and 2 command misuse; query commands can
successfully report an existing blocked run.

## Acceptance campaign

1. Contracts/identity: unsupported versions, duplicate keys, unsafe paths, changed
   assets, task/input/plan/profile/catalog/policy/qualification drift, fabricated
   permit, mismatched host version, untrusted control owner and expired proof.
   Compare pinned assignment permissions with effective host permissions,
   including route/model/tool access; any escalation or drift starts zero inference.
2. Concurrent claims/accounting: multiple processes, shared pools, overlapping
   windows, selection→reservation race, reservation→launch race, policy migration,
   two-store commit/reply loss, identical/changed idempotency requests and
   source-ordinal incomparability. Zero guarded synthetic over-admission.
3. Fault injection before/after claim, staging, reserve commit, intent commit,
   ConfirmLaunch, process creation, native acknowledgement, checkpoint, result
   promotion and settlement. Restart yields visible safe state without lost
   patches, speculative refunds or duplicate automatic launches.
4. Fencing/process control: expire/take over during active work; old owner loses
   checkpoint/finalize/settle rights. Cancel-before-launch starts zero inference;
   cancel-in-flight reports stopped or unknown. Test process descendants and
   platform-specific stop behavior; compilation alone is not OS conformance.
5. Capacity: stale/missing readings, exhausted premium-only task, missing actuals,
   exact decimals, reset during work, old generation settlement and clock
   regression. Reset hints never mint capacity; absent usage never becomes zero.
6. Lifecycle: unmet accepted-dependency, blocked versus failed, completed receipt
   resume rejection, interrupted exact-input continuation, changed configuration
   requiring a new attempt/session, invalid checkpoint, deadline and retry bounds.
   Renewed proof assets must reject exact native resume under a different P6
   policy digest; test preserved-checkpoint/new-session continuation separately.
7. Privacy/ownership: credential canaries, hostile tracker instructions, unrelated
   files, untracked work, sandbox-control expiry and isolated stale writers.
   No tracker writer, paid-route switch or P8 acceptance call occurs.
8. Delivery: focused tests, full Go 1.22 suite, runtime/accounting/host race tests,
   Python regressions, kit validation, packaging, empty-PATH/no-credentials/
   blocked-network simulation and six-target CGO-disabled builds. Execute live
   platform claims only on platforms actually exercised.

Run synthetic fixtures with fake clocks/processes and crash hooks; tag their
issuer/evidence namespace as fixtures. Connected acceptance uses independently
scoped real evidence and cannot disable common hard gates to get a green run.

## Staffing and completion report

Primary owns contract lock, admission/host integration, CLI and review fixes.
After P7.0, use two Luna-medium workers: worker A owns runtime ledger/fencing and
crash tests; worker B owns staging/checkpoints/recovery fixtures and operator
reporting. Keep shared type edits with the primary. A separate Luna-high reviewer
checks the integrated reservation ordering, stale-writer containment, privacy,
reconciliation and acceptance boundaries. Use focused targeted reviews before
expanding tests; retain bounded host attempts and no retry/fallback by default.

Deliver operator/recovery documentation and a matrix listing P7-core,
Codex/Claude P6 live, P3 live-capacity evidence, P4 quality qualification,
provider live reads, P7 connected paths and P8 promotion separately. Do not
claim a complete workflow from an offline controller or passing host fixture.

## Verified implementation anchors

- `internal/usage/store/store.go`: existing atomic accountant, tokens and launch/settlement transitions.
- `internal/usage/accounting.go`: shared capacity projection.
- `internal/selection/select.go`: offline recommendations without reservation or execution authority.
- `internal/planning/ready.go`: supplied-evidence DAG readiness.
- `internal/hostbridge/execution.go`, `types.go`: current host invocation/guard seam and internal attempt allocation.
- `internal/cli/host.go`: current direct host path has no runtime guard.
- `internal/contracts/workflow_types.go`: existing immutable P1 binding/run observations.
- `internal/evidence/reserve.go`: create-only evidence intent, not a runtime ledger.
- `docs/implementation/phase-5-delivery-evidence.md`, `phase-6-delivery-evidence.md`: pending connected gates.
- `docs/swarmd-integration.md`: untracked runtime mapping, not qualified execution.
