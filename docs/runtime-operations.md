# Local runtime operations

The Go CLI exposes an opt-in local runtime for admitting a captured workflow,
tracking attempt state, and producing reviewable evidence. It does not accept
completed work or apply tracker updates. Local foreground execution is the
only execution mode.

## Commands

```text
aok runtime init --policy FILE --state NEW-DIRECTORY [--json]
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

`init` validates the versioned policy and creates a new private state directory.
The policy must name absolute repository and staging roots, a local accounting
database, and a hash-bound usage-policy asset. Bounds are enforced for
concurrency, leases, heartbeats, and attempts. Dispatch is disabled unless the
operator explicitly sets `allowDispatch`; the default should remain `false`.
Unknown and repeated flags fail with usage exit code 2. Dispatch always requires
`--once` and performs one bounded foreground pass.

`submit` captures the manifest and its hash-bound inputs under the configured
repository scope. `validate`, `status`, and `explain` report local state and
readiness without authorizing acceptance. `cancel` records a cancellation;
`resume` requires a specific verified checkpoint. `export` writes a new,
immutable evidence directory and refuses to overwrite existing output.

`replay` accepts a strict versioned synthetic scenario and a required RFC3339
`--as-of` time. It runs deterministic fixture events against temporary private
runtime and accounting databases. It returns `report.json` and
`eventtrace.json`; it does not use host credentials, invoke a host, or return
database files. The fixture policy cannot enable dispatch. Output creation is
new-directory only, so an existing result remains unchanged.

## Host routes and live gates

Each `HostRoute` binds a host adapter to a hash-bound operator policy and may
bind a qualification asset. Routes can be scoped to a step and attempt; an
unscoped route defaults to attempt 1. Runtime dispatch selects the matching
route for the current step/attempt and stages the repository in an isolated
workspace. The selected operator policy must apply to that workspace. A
qualification must bind the route's host, policy hash, and required controls;
a fixture qualification cannot authorize a live run.

`allowDispatch` is an operator opt-in, not evidence that execution is safe or
qualified. Reports keep `accepted` false. P6 live control proofs and native
host conformance remain blocked, and P8 independent acceptance evidence is
still required before dependent or preconditioned steps can proceed. A plain
completed-step declaration in planning evidence does not satisfy that gate.
Tracker writeback and runtime token enforcement remain separate unverified
capabilities.

## Signed recovery

`reconcile` requires current signed `RecoveryEvidence` whose run, manifest
hash, step, and attempt match the ledger. The issuer's Ed25519 public key must
be configured in `reconciliationIssuers`. Evidence must be no more than five
minutes old and cannot be future-dated. If launch may have occurred, recovery
also requires captured host receipt and transcript assets bound to the same
attempt, prepared session, configuration, and transcript hash. A stop
observation alone does not resolve uncertain usage: actual usage must be
reconciled, or the durable accounting hold remains unresolved. Confirmed
pre-launch recovery requires a durably released hold. Recovery creates a new
checkpoint and requires an explicit `resume`; it never automatically relaunches
work.

Runtime reports distinguish the local offline core from the blocked Codex and
Claude live gates and pending connected qualification. These labels describe
the currently implemented evidence boundary; they do not claim live
qualification, workflow acceptance, or production readiness.

## Evidence identity and retention

Runtime admission requires a `RepositoryRevision` input-revision asset with
`version: aok-runtime/v1`, the exact `commit` and `inventorySha256` of the complete
sorted source asset inventory. The manifest's `repositoryAssets` pins every
staged path/hash; dirty tracked bytes and explicitly allowed untracked files are
part of that inventory, including executable bits. An altered source tree cannot reuse the old task input
identity. Checkpoint continuation retains the original input identity and adds
its immutable checkpoint lineage; it requires the P4 task protocol to declare
`midRun` and `safeCheckpoint` and an approved next-attempt route.

Each route can pin a `stepId` and `attempt` (default 1). Its host policy must name
an exact, unoccupied workspace directly below `stagingRoot`. The attempt ID is
that workspace's basename, enabling the external enforcement owner to bind the
actual path in advance. Routes, host policies and proof hashes are operator
inputs, never generated from tracker text. Reusing an occupied workspace is
blocked. Updated proof/policy bytes require explicit new operator configuration;
P6 exact-policy native-session resume is not weakened. Host qualification itself
requires the full live synthetic-workspace conformance matrix, not a listing or
fixture replay. Native qualification records `qualified-live-synthetic`;
injected fixture runners record `fixture-qualified` and cannot admit dispatch.
Current delivered evidence has neither host live-qualified.

Candidate output and normalized host events are retained privately for review;
the runtime does not claim to scrub arbitrary private source text out of intended
outputs. Qualification payloads/outputs are redacted. Hashes protect integrity,
not authorship. Reconciliation issuer keys and runtime/host policies must remain
outside task-controlled workspaces. No background collector is installed, and
current P3/P4 evidence can still block every candidate.

Captured task/evidence inputs and operator policies are immutable. A material
requirement, input revision, route or expired proof change requires an explicitly
new captured submission/operator configuration; reconciliation does not edit old
policy hashes or manufacture missing evidence. An unresolved pre-admission
failure remains inspectable and blocked. Resume applies to preserved checkpoints
whose process and accounting outcomes satisfy the documented checks.

`status`/`explain` project current local accounting balances where available,
including remaining allowance, held exposure, headroom and reset hints. Missing
or invalid observations stay unknown. These queries do not collect new provider
usage or promise fit after a reset.
