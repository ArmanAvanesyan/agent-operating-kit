# Phase 7 delivery evidence

The opt-in local runtime core is implemented with offline fixtures. **Phase 7
live promotion and the complete end-to-end workflow are not delivered.** No
live model invocation or provider mutation was performed for this phase.

| Gate | Status |
|---|---|
| P7 local core | Implemented; acceptance results below |
| Codex P6 live qualification | Blocked: required trusted scoped control proofs and live conformance evidence absent |
| Claude P6 live qualification | Independently blocked for the same requirements |
| P3 live capacity evidence | Separately pending per collector/auth mode; unknown capacity blocks admission |
| P4 production task-quality evidence | Separately insufficient/unqualified; no routing-quality claim |
| P5 connected tracker reads | Separately pending per provider/auth mode |
| P7 connected admission/recovery | Pending the selected host and evidence gates; both host paths required for overall promotion |
| P8 independent acceptance, delivery and writeback | Pending |

## Implemented behavior

`aok runtime init|submit|validate|dispatch|status|explain|cancel|resume|reconcile|export|replay`
uses closed `aok-runtime/v1` envelopes and a private local SQLite ledger.
Claims, operation idempotency, audit integrity, lease renewal, authority epochs
and fences survive process restart. P1–P6 record formats remain unchanged.

The controller consumes captured plan/assignment/source inventories, binds task
input revisions to repository bytes, stages isolated workspaces and calls the
existing P4 selector against a current P3 accounting snapshot. It reuses P3's
atomic reservation/projector and launch confirmation. Runtime and accounting
transactions remain separate, with journaled intents and stable operation IDs.
Only an exact live-qualified host route with fresh trusted controls can reach
the controlled transport. Fixture qualification cannot satisfy that gate.

The P6 controlled seam accepts preallocated attempt IDs and checks guards at
actual transport start. The start hook confirms accounting after runtime launch
intent. Heartbeats, deadlines and cancellation constrain execution. Stale owners
cannot promote results or settle using expired accounting tokens. Candidate
observations are reserved before a host start and preserved privately; publication
remains fenced. Lease expiry never proves process termination or refunds quota.

Independent review found and corrected lost reservation/recovery reply handling,
checkpoint continuation and fixture/live qualification distinctions. Scoped P3
recovery touches only the exact reservation, leaving unrelated CLI holds intact.
Signed exact-attempt reconciliation can adopt a committed reservation whose
reply was lost, recognize already settled/released state and record a confirmed
unlaunched outcome without fabricating a host receipt. Usage actuals require
trusted scoped evidence; estimates never become actual consumption.

Checkpoints copy bytes into immutable private snapshots. Recovery survives old
workspace mutation/deletion and restores into a new worktree. Continuation needs
an observed stop, quiescent verified checkpoint, resolved accounting exposure,
explicit resume and a separately approved attempt route. P4 safe-checkpoint
protocol requirements still apply. Required capability bindings or controls that
P6 cannot enforce remain blocked rather than silently omitted.

## Boundaries and limitations

- One local authority, bounded foreground operations, no daemon, remote workers,
  child agents or distributed scheduling. Local staging is not a sandbox.
- External operator policy and issuer keys are trusted inputs; signatures do not
  independently certify the operator or an isolation implementation.
- Host policies bind predetermined step/attempt workspace paths. Proof renewal
  changes the exact policy digest; no automatic rewriting or relaxed P6 resume.
- Ordinary dispatch preserves an uncertain hold when complete attributable
  usage is unavailable. Signed reconciliation is required to settle; reset hints
  never mint observations or solve cross-window allocation.
- P8-gated dependencies/preconditions cannot be authorized by plain supplied
  completed-step declarations. A host result is never accepted/Done.
- No live quota guarantee: external account activity can consume capacity despite
  conservative reservations among participating local callers.
- No tracker writes, billing fallback, automatic credential switching, merge,
  deployment or swarmd authority takeover. Swarmd remains separately unqualified.
- Raw intended candidate output can contain private repository content; it stays
  in private evidence directories. Qualification transcripts are redacted.

## Acceptance

Checks are run with the cached Go 1.22.12 toolchain and module downloads disabled.
The core fixture suite covers concurrent subprocess claims, crash rollback,
fences, cancellation, blocked transitions, audit corruption, idempotent and scoped
accounting recovery, missing/expired evidence, identity drift, immutable snapshot
recovery, credential/path canaries, deterministic replay and guarded host-start
ordering. These are synthetic/local checks, not connected certification.

| Acceptance check | Result |
|---|---|
| Full Go 1.22.12 suite | Pass, including release packaging/extracted-kit checks |
| Runtime, host bridge and accounting race suites | Pass |
| Python regressions | 267 tests, four skips |
| Python compilation, release-shell syntax, kit validation | Pass |
| Six-target CGO-disabled CLI and test builds | Pass: Linux/macOS/Windows, amd64/arm64 |
| Built binary with empty PATH, no credentials, blocked network proxy | Replay, init, status, validate and blocked dispatch pass |
| Identical replay inputs | Byte-identical report and event trace |
| Existing output directory | Rejected; original report preserved |
| Whitespace validation | Pass |

Final independent review found no remaining concrete blocker in the inspected
core paths. Native-versus-fixture qualification, current route status, exact
account scope, the guarded start boundary and signed recovery remain distinct.
A default macOS temporary-directory symlink failure found by binary smoke tests
was fixed by canonicalizing only the runtime's own new temporary directory;
input symlinks remain rejected. An intermediate concurrent test run hit a
five-second existing delivery-doctor probe timeout; the final full run passed
with normal checks and no product-test bypass.

CI includes runtime replay, runtime race coverage and cross-compilation.
Platform builds establish compilation only; native execution/control conformance remains pending
on unexercised platforms. All future live qualification must retain the P6/P3/P4
hard gates rather than weakening them to obtain a passing run.

See [runtime operations](../runtime-operations.md) and the
[Phase 7 plan](phase-7-durable-execution-plan.md).
