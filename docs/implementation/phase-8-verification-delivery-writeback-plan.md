# Phase 8 — Verification, delivery and controlled tracker writeback

Status: implementation plan, 2026-10-24. Offline delivery results are recorded in
[Phase 8 delivery evidence](phase-8-delivery-evidence.md). This document authorizes no live
verification subprocess, repository publication or tracker mutation. Two
GPT-6 Luna workers at medium reasoning reviewed the current verification,
runtime, repository-delivery and connector seams. A separate Luna-high reviewer
checked the integrated plan; its acceptance-atomicity and revocation-race
corrections are incorporated below.

## Outcome and separate gates

Turn an immutable candidate into independently verified acceptance evidence,
handoff to existing repository delivery, then execute only specifically approved
tracker operations through a crash-safe outbox. Keep implementation completion,
real evidence qualification and end-to-end promotion separate.

| Gate | Current status / completion condition |
|---|---|
| P8-core | Planned: offline verification, acceptance, delivery translation, grant validation, outbox/replay and four fixture-backed writers |
| Codex P6 live | **Blocked**: trusted scoped control proofs and live conformance evidence absent |
| Claude P6 live | **Independently blocked** for the same requirements |
| P7 connected execution/recovery | Pending host-specific P6, usable P3 capacity, qualified P4 evidence and runtime conformance |
| P5 live reads | Pending separately for GitHub, Azure DevOps, Linear and Jira Cloud/auth mode |
| Verifier execution | Pure local validators can qualify offline; command verifiers need their own environment/control conformance |
| Repository delivery | Existing optional engine available; exact integration/publication actions require separate authority and tested receipts |
| Tracker writes | Pending per provider, operation, auth mode, tenant scope and concurrency semantics |
| Agent-to-tracker end-to-end promotion | Pending the complete selected path; overall two-host claim requires both host paths |

An imported/manual candidate can be verified without claiming that an AOK host
executed it. Fixtures can qualify the offline machinery but never qualify a
production issuer, live host, provider principal or routing-quality cohort.
A provider writer can be tested separately with an authorized manual/synthetic
item; that does not complete a P6/P7 agent workflow. Missing credentials or
consent leaves connected gates pending without blocking P8-core work.

## Scope and ownership decisions

- Use `internal/verification` for candidate evidence, verifier execution and
  acceptance; `internal/writeback` for grants, durable outbox and dispatch.
  Keep provider writers beside the P5 provider code behind the connector boundary.
- P7 owns attempts, fences, candidates and preservation; P8 owns acceptance
  evidence. P3 remains the sole capacity accountant. Verification does not refund
  uncertain model usage or create new quota observations.
- Reuse P1 `SyncOperation`, `SyncReceipt`, `ConnectorCapability`, bindings and
  canonical operation hashing. A P5 `TrackerUpdatePlan` remains a draft.
- Reuse the repository-delivery pack's preservation, isolated delivery, gate and
  review machinery. Add a translation/receipt adapter, not another shipping engine.
- Keep the existing P1–P7 contracts and historical records valid. New contracts
  are independently versioned. Do not change historical host reports or turn the
  candidate's `accepted:false` into proof of acceptance.
- GitHub first, Azure next, then Linear and Jira Cloud. Implement fixture-backed
  paths for all four; enable each live operation only after its own conformance.
  Cloud Codex tasks, Jira Data Center, provisioning/process customization,
  webhooks, background synchronization and Windows delivery certification remain
  separate. Merge, deployment and cleanup are separately authorized actions.

## Contracts and trust

Add closed `aok-verification/v1` and `aok-writeback/v1` records.

| Record | Required contents |
|---|---|
| CandidateManifest | Origin class (runtime/manual/fixture), exact repository/base/source inventory including executable bits, plan/goal/assignment/input/configuration/profile, prepared/host receipt/checkpoint and every evidence hash |
| VerificationPolicy / VerifierSpec | Approved issuer and verifier identities, executable/implementation hashes, exact argv/toolchain/environment, required criteria, independence/review rules, validity, resource/control bounds and allowed evidence |
| VerificationReceipt | Candidate and policy digests, verifier/rubric/environment identity, per-predicate results, before/after snapshot hashes, usage/time observations, fixture/live scope, signature and evidence assets |
| AcceptanceReceipt | Candidate/plan/goal/criteria revisions, required coverage and verifier/review references, derived disposition, validity and protected acceptance-issuer signature |
| DeliveryHandoff / DeliveryProof | Accepted candidate, preservation/source and destination snapshot identities, existing delivery context/gate/review receipts, resulting commit/tree/PR head and independently read facts |
| WritePolicy / WriteGrant | Protected approval issuer, principal/auth reference, provider origin and exact target, operation and payload digest, capability/mapping/binding versions, precondition, evidence dependencies, nonce, expiry and revocation generation |
| OutboxEntry / ReconciliationReport | Immutable operation/grant refs, stable logical operation ID, execution fence, attempts, durable send intent, native response identifiers, readback observations and ambiguity/conflict disposition |

Hashes prove integrity, not authorship. Protected external policies define issuer
trust. Fixture issuers cannot enter a production trust policy implicitly. Signing
keys and credentials stay outside task-controlled workspaces and captured bundles.
Use an injected signer or external key reference; introduce no credential store.

Acceptance, repository publication and tracker authorization are separate facts.
An acceptance receipt grants no tool or remote-write permission. A write grant
permits only its exact reviewed operation. Task text, model output, a schema-valid
`SyncOperation` or its `ExternalAuthorityRef` cannot mint that grant.

## Verification and acceptance

1. Import the candidate into a private create-only bundle. Validate every asset,
   source revision, scope, P7 attempt/fence lineage and origin. A stopped/quiescent
   runtime candidate is required for authoritative evaluation; an old owner can
   preserve quarantined bytes but cannot replace the current candidate.
2. Resolve required predicates from pinned AOK contracts and explicit policy.
   Cover output schema, deliverables/sections, resolvable evidence, tests, scope,
   inventory coverage, goal predicates and required independent review. Never
   infer missing criteria, test commands or completion rules with an LLM.
3. Run pure validators offline first. Run command verifiers only with pinned
   implementations/argv, controlled environment, independent filesystem/network
   enforcement, time/output bounds and no ambient hooks, plugins or credential
   exposure. Repository tests are executable code, not harmless reads. P6 model
   controls do not automatically cover verifier processes.
4. Evaluate a read-only candidate snapshot; place builds/reports in separate
   approved scratch storage. Bind and compare snapshot state before and after.
   Unexpected source mutation invalidates the receipt. Pin the relevant toolchain,
   dependencies and environment so a command name alone cannot establish identity.
5. Record `pass`, `fail`, `unknown` or policy-approved `not-applicable` for each
   criterion, with evidence. Missing consumption is unknown, not zero. Exit zero
   alone cannot establish inventory/semantic coverage. Model self-report cannot
   satisfy a required independent verifier or review.
6. Issue acceptance only when every required predicate passes and coverage and
   independence requirements hold. Not-applicable requires an explicitly allowed
   exemption and rationale; it cannot waive a mandatory criterion. No numeric
   aggregate may hide an individual required failure or unknown result.
7. Sign an immutable acceptance receipt. Re-evaluation produces a new receipt;
   revocation/supersession preserves history. Material source/input/criterion,
   verifier, policy or required environment changes invalidate affected receipts.

Proposed defaults: 30-second individual verifier timeout, five-minute command
budget, 4 MiB frame/report limit and 64 MiB capture limit; policies can narrow
these. Expensive test campaigns require an explicitly reviewed bounded verifier
profile. Offline inspection uses required `--as-of`; execution uses trusted UTC
plus monotonic elapsed time. Freshness is policy-defined and never renewed by
copying evidence. Live review identity is authenticated separately from a signed
assertion that someone reviewed; an author's own result is not independent.

Keep four milestones distinct: verified candidate, accepted work, delivered
repository revision and reconciled tracker update. Remote Done/closed is disabled
by default until an explicit completion policy specifies its required milestones.
Progress/blocker reporting can use separate authorized operations without
pretending acceptance. Exact native status IDs map to lifecycle meanings through
policy; changing an intent label cannot disguise a completion transition.

## P7 dependency and lifecycle bridge

P7 currently blocks dependent/preconditioned steps and preserves candidates as
`accepted:false`. Add a narrow, independently validated acceptance-evidence reader.
It verifies protected issuer/policy, exact run/step/input/candidate, required
predicates, validity and revocation before eligibility is recomputed.

Keep P8 receipts in a separate immutable evidence store. Attach only their verified
candidate/receipt/policy digests and revocation generations through new versioned
P8 projection tables in the authoritative P7 runtime database. An explicit opt-in
schema migration adds those tables/audit records; it does not change historical
P7 wire records or their `accepted:false` fields. Existing runtime behavior remains
unchanged when no verification store/policy is supplied. Never authorize a
successor from a user-supplied `completedSteps` list or a JSON acceptance boolean.

Validate receipt signatures/content before attachment, then CAS the exact current
run/step/input/candidate revision and fence while attaching that immutable digest.
Pin the protected trust/revocation generation into the same runtime transaction.
Claim and finalization read this local projection and current fence in one runtime
transaction. Revocation/supersession advances its projection generation and closes
new claims; stale generations fail the guard before launch/publication. Do not
claim an atomic commit across the separate evidence store, accounting store and
runtime database: an unattached receipt is harmless, and attachment reply loss
replays the same idempotent request. Acceptance does not cure an expired lease, uncertain
process or exhausted quota; successor dispatch still passes P2–P7 hard gates.
Runtime cancellation, input revision changes and stale candidate owners block
new acceptance promotion/outbox creation. Recovery can read old evidence without
resurrecting authority. For task outcomes used later by P4, construct the normal
signed outcome shards and preserve cohort/repetition rules; P8 acceptance alone
does not qualify model ranking or supply enough independent task families.

## Existing delivery engine integration

Translate the accepted candidate into a reviewable delivery context with explicit
file scope and action permissions. Begin with local preservation and isolated
handoff. The adapter consumes the embedded engine's existing context, snapshot,
gate/review and integration receipts; recompute their seals and snapshot bindings.
A recomputable SHA seal is not a trusted reviewer identity or write grant.

A port, rebase, cherry-pick or changed destination creates a different integration
snapshot. Preserve the source acceptance, then rerun affected verification/review
on that exact destination before claiming it accepted or ready to publish. Do not
transplant source receipts onto a new tree or stale PR head. Preserve the source
worktree and untracked bytes; no automatic cleanup on failure.

The existing helper remains optional and Python 3.11+ on supported Linux/macOS.
Other core commands remain Go-only. Missing Python or optional schema dependencies
blocks only helper execution, not receipt inspection/drafting. Do not auto-install.

Local AOK runtime finalization and the pack's optional swarmd finalization cannot
both control the same task. Disable the swarmd path for local-authority runs;
a separate backend must prove compatible ownership/fencing before adoption.

Push, opening a PR, submitting a GitHub review, merging, deployment and cleanup
each retain distinct authority. An internal independent review is not a submitted
GitHub approval. PR/CI/merge facts require their own current readback; none is
inferred from a helper's optimistic message. Completion policy decides which
milestone is required, rather than assuming every task must merge or deploy.

## Authorization, outbox and provider writes

Preserve P5's read-only transport and read policy. Add a separate write transport
whose requests are constructed from validated typed operations, never arbitrary
URLs/bodies supplied by tasks. Reuse scope checks, origin pinning, secret
references, cancellation and credential-forwarding redirect rejection.

Only a grant verifier inside the connector boundary may construct the currently
opaque `AuthorizationContext`. The grant follows a concrete fresh-read plan
review: exact target, native field/value diff, content assets, precondition,
accepted/delivery evidence where required and stable operation ID are all bound.
Default grant TTL and planning freshness are five minutes, shorten-able by policy.
Protected issuers/revocation state are operator inputs. Recheck them immediately
at the durable send-intent boundary; clock regression or unavailable revocation
state blocks writes. Extend the private authorization context/Apply seam with
expiry and the protected generation check, rather than adding a public constructor
that merely copies the declaration fields.

Outbox ordering:

1. Refresh permitted source facts and rederive the operation from the P5 draft.
   Unknown/lossy fields, missing transitions or human-owned changes without
   explicit intent remain blocked.
2. Validate exact scope, capability, expected revision and canonical operation
   digest. Obtain the narrowly signed human-approved grant; a later change
   requires a new reviewed operation/grant.
3. Atomically journal operation, evidence/grant hashes and stable logical ID in
   the local outbox. Import signed/protected revocation generations monotonically
   into that authority's ledger. Claim the operation with an expiring owner/fence.
4. Recheck grants, dependencies, revocations and provider preconditions. CAS the
   current local revocation generation, claim/fence and grant validity while
   committing send intent. This is the authorization-consumption linearization
   point; the grant's request/attempt budget is consumed here. Then perform the
   bounded request. Never hold a database transaction open across
   network I/O or claim an atomic database/provider transaction.
5. Record response identifiers, then perform a separate fresh, bounded readback.
   Mark success only after operation-aware comparison of the intended effect.
6. After lost replies, timeout, crash or partial success, enter
   `unknown-awaiting-reconciliation`. Readback precedes any further mutation.

States distinguish prepared, authorized, dispatch-intent, response-observed,
readback-verified, conflict, rejected, unknown, revoked, expired and superseded.
Duplicate delivery events reuse the same logical operation; identical requests
replay their record, changed bytes under the same key conflict. Multiple requested
operations have independent receipts; partial success is not group atomicity.

Revocation committed before send-intent CAS prevents admission. Revocation racing
with or after that CAS cannot guarantee suppression, even if request bytes have
not yet reached the provider; attempt cancellation where possible and report the
in-flight/unknown result. Revocation cannot undo a remote effect. Protected
revocation input freshness and monotonic generations bound this guarantee; absence
of a current trusted generation blocks admission. Keep its audit and reconcile
reads under the separate read
policy. `Writer.Reconcile` must be read-only. Repair, retry after a changed
precondition, rollback or compensating mutation requires a new authorized outbox
operation. Cancellation closes new dispatch, but an in-flight request may still
complete. A fence prevents stale local finalization, not a remote server effect.

## Provider capability policy and rollout

P1 currently rejects `update-fields` and `transition` without conditional-write
support. Preserve that rule. Local serialization and read-before/read-after do
not establish server CAS. Where native conditional semantics are unverified,
leave those operations unsupported. Do not weaken P1 validation to make a provider
appear complete.

| Provider | First qualification slice | Additional operation gates |
|---|---|---|
| GitHub | Explicitly authorized marker-bearing evidence comment with readback | Issues versus PRs versus Project fields remain distinct; field updates/closing need independently established concurrency guarantees; merge stays separate |
| Azure DevOps | One mapped work-item field with documented and exercised native revision precondition, if available | Process/type/state identity, relations and comments qualify separately; no process customization |
| Linear | One scoped issue evidence/comment operation with exact workspace/team/issue IDs | GraphQL partial errors, mutation correlation and cursor-complete reconciliation; field/state writes remain blocked without qualified preconditions |
| Jira Cloud | One scoped issue comment/evidence operation preserving supported rich text | Native issue/project/field/type IDs; transition ID and required fields; unknown rich-text conversions and conditional guarantees block affected writes |

These are qualification targets, not current claims of API support. The first
provider work item is to verify current official documentation and implement
fixture assertions for the exact API/auth mode. Then exercise an explicitly
approved disposable scope. Record declared versus fixture-tested versus observed
live capability evidence separately. Do not infer write permission from read
permission, a timestamp/fingerprint from an atomic revision, or partial-list
absence from deletion.

For comments/links, use verified native idempotency when available. Otherwise
persist a stable operation marker and correlate exact content/native IDs through
bounded complete readback. Missing markers after a timeout or eventual-consistency
window are not proof of non-delivery: retain unknown rather than blindly resend.
If a provider lacks a safe non-duplication strategy, leave automatic retry
unsupported. No universal exactly-once guarantee is promised.

Creation requires a distinct `CreateIntent` envelope in `aok-writeback/v1`: P1's
closed operation kinds do not include create, and no immutable item ID exists
before creation. Bind approved parent/container, type/schema, requested fields,
client correlation ID, budgets and grant before send. Capture the assigned native
identity and issue a real WorkItemBinding only after verified readback. Use a
separate validated creator boundary; never invent a placeholder RemoteIdentity
or overload a P1 field update. Creation stays disabled by default and qualifies
separately per provider. No unrequested creation or provisioning is introduced.

## Packages, dependencies and acceptance

| Package | Dependencies | Deliverable and acceptance criteria |
|---|---|---|
| P8.0 Contracts/authority ADR | P1, P5–P7 core | Closed verification/writeback records, origin classes, milestone policy and ownership. Reject malformed/duplicate JSON, unknown versions and fabricated authority; preserve old schemas/receipts. |
| P8.1 Candidate capture/pure checks | P8.0, P7 snapshots/P6 output contracts | Immutable import, source inventory/schema/deliverable/evidence checks. Tampered bytes, executable bits, scope drift and missing coverage cannot pass; identical offline inputs reproduce reports. |
| P8.2 Verifier runner/review trust | P8.1 | Pinned bounded verifier execution, exact environment and authenticated independent review. Mutation, timeout, cancellation, missing tool/control or self-review yields fail/unknown, never acceptance. |
| P8.3 Acceptance and P7 bridge | P8.1–2, P7 ledger | Signed immutable acceptance and revocation projection. Only exact valid receipts unlock corresponding dependencies; stale fences, transplanted receipts and plain completed-step declarations cannot. |
| P8.4 Delivery handoff | P8.3, repository-delivery pack | Translate context, preserve bytes, validate existing receipts and exact destination tree. Port/rebase or stale PR head invalidates affected gates; no second shipping engine or implicit publication authority. |
| P8.5 Grant verifier/outbox | P8.0, P7 transactional patterns, P5 drafts | Protected signer policy, opaque auth construction, durable send intent/fences, expiry/revocation and recovery. Forged/changed/expired grants start zero requests; lost success replies remain reconcilable. |
| P8.6 Four provider writers | P8.5, provider API capability review, P5 adapters | Typed operations behind separate write transport; fixture success/denial/conflict/ambiguity/readback for all four. Unqualified operations remain unsupported, read transport remains read-only. |
| P8.7 Explicit creation extension | P8.5–6, separate creator schemas | Parent/type-scoped grant, stable correlation and assigned-ID readback. No fake pre-creation identity; replay/crash cannot trigger an unapproved duplicate creation. |
| P8.8 Integrated review/distribution | P8.1–7 | Deterministic/crash/security tests and full regression/distribution checks; operator approval, recovery and retention guides; truthful per-gate matrix. |
| P8.9 Connected campaigns | Relevant P5 read/write, P6 host, P7, verifier and delivery gates | Disposable authorized paths prove exact model→verification→delivery→tracker readback and negative/recovery cases. Currently blocked/pending; qualify each combination independently. |

P8.5 outbox foundations can proceed alongside P8.1–3 after contracts lock.
Completion-intent operations require P8.3/4 evidence according to milestone
policy; progress/blocker operations have explicit narrower prerequisite sets.
P8.6/7 fixtures do not depend on live hosts. Connected provider writer conformance
can use manual candidates; complete agent-workflow claims additionally require
all host/runtime/capacity/quality gates.

## Proposed interfaces

```text
aok verify import --manifest FILE --policy FILE --out NEW
aok verify check BUNDLE --policy FILE --as-of RFC3339 [--json]
aok verify run BUNDLE --policy FILE --out NEW [--json]
aok verify accept BUNDLE --policy FILE --out NEW [--json]
aok verify inspect BUNDLE --policy FILE --as-of RFC3339 [--json]
aok delivery handoff --acceptance BUNDLE --policy FILE --out NEW
aok delivery validate-handoff BUNDLE --policy FILE --as-of RFC3339 [--json]
aok tracker authorize-plan --draft BUNDLE --policy FILE --out NEW [--json]
aok tracker enqueue --operation FILE --grant FILE --policy FILE --outbox STATE
aok tracker dispatch --outbox STATE --operation ID --once [--json]
aok tracker reconcile --outbox STATE --operation ID [--json]
aok tracker outbox STATE [--json]
aok tracker revoke --outbox STATE --grant ID --revocation FILE [--json]
aok writeback replay --scenario FILE --as-of RFC3339 --out NEW [--json]
```

`authorize-plan` prepares the approval preimage; it does not mint human consent.
The signer/approval boundary returns a separately verifiable grant. Handoff and
validation remain local; publication uses the existing engine's explicit action
interfaces. Preserve its argument forwarding: use a uniquely namespaced new bridge
entrypoint if `handoff` conflicts with an existing subcommand. Runtime dependency
integration adds an explicit verification-store/policy pair; without them retain
P7's current fail-closed behavior. Writes/verifier processes use trusted time;
`--as-of` belongs only to inspection, validation and replay. Queries perform no
mutation, and replay never contacts providers or launches real agent/verifier CLIs.

Use existing private bounded readers/writers, immutable output directories and
exit conventions (0 success, 1 invalid/blocked/conflict, 2 misuse). Default network
bounds follow P5: 30 seconds/request, five minutes/command, 100 requests,
4 MiB/response and 64 MiB capture. Bounds produce incomplete/unknown evidence,
not implied success. No automatic credential switching or hidden daemon.

## Acceptance and delivery campaign

- Verification: fixture/manual/runtime origins; missing/failed/unknown predicates;
  banned exemptions; stale signatures/policy; insufficient review; contaminated
  verifier environment; source mutation; missing dependencies; timeout/cancel;
  attempted credential/network access; semantic inventory coverage.
- Identity/lifecycle: changed packet, task, plan, goal, input, configuration, host
  receipt, file bytes/modes, repository tree or criteria invalidates affected
  receipts. No acceptance of stale-fence candidates or wrong-run/step evidence.
  A cancellation or newly unmet dependency cannot be bypassed by a receipt race.
- Delivery: preserve dirty/untracked/deleted/executable files; source and
  destination identities; rebase/port conflicts; gate/review snapshot mismatch;
  old PR head; missing optional helper; no implicit GitHub approval, push or merge.
- Authority: wrong issuer/principal/tenant/item/field/value/digest/precondition,
  reused nonce, changed payload, expired/revoked grant or unknown revocation state
  causes zero mutation requests. Hostile tracker text and model outputs remain data.
- Outbox: competing controllers, stale owner, duplicate delivery, crash before
  intent, after intent/before send, after server success/before reply, before/after
  readback and before local finalize. Each case remains auditable and cannot
  blindly replay an ambiguous mutation or finalize under an old fence.
- Providers: auth/permission denial, rate limits, failed native precondition,
  human edits before/during/after send, partial mutation, truncated pages,
  GraphQL HTTP-200 errors, delayed visibility, duplicate/lost markers, unsupported
  transition fields/rich text, redirected credentials and changed origin.
- Safety assertions: zero false acceptances, zero hard-gate bypasses, zero
  unauthorized requests and zero automatic ambiguous resends in deterministic
  fixtures. One logical outbox update for duplicate events; report unresolved
  remote duplication/race risk explicitly rather than claim exactly-once effects.
- Delivery gates: focused tests; full Go 1.22 suite; verification/outbox/runtime
  race and multi-process crash tests; Python regressions; kit and packaging;
  empty-PATH/no-credentials/blocked-network pure commands; six-target CGO-disabled
  builds. Command-verifier/helper checks use declared dependencies and actual
  supported platforms; compilation is not live platform certification.

Default mutation budget is one send per reviewed operation; an ambiguous send is
never automatically retried. Reconciliation is read-only, including after grant
expiry/revocation. A newly authorized retry still needs provider-qualified
idempotency or a proven non-send disposition; simply issuing a new grant does
not make a potentially duplicated effect safe.

No actual live writes are part of this planning request. Later implementation
can finish fixtures without credentials. Connected writes require explicit
provider/item/action authorization and least-privilege test scopes; missing
consent cannot be filled in by a generated grant or elapsed waiting time.

## Staffing and rollout

Primary owns contract lock, authority/acceptance integration, P7 bridge, CLI and
review fixes. Use two Luna-medium workers after P8.0:

1. Foundation wave: verifier/candidate tests on one worker; outbox persistence and
   crash tests on the other.
2. Adapter wave: Azure on one worker; Linear then Jira on the other; primary owns
   GitHub and existing delivery-engine translation. Creation is a distinct final
   schema/qualification slice, not an implicit operation extension.
3. A separate Luna-high reviewer audits authority construction, independence,
   snapshot/fence races, conditional-write claims, privacy and ambiguous retries.

Publish P8-core, verifier execution, acceptance origin/issuer qualification,
repository delivery, every provider/operation/auth read-write gate, both blocked
P6 gates, P7 connected state and P9 pilot readiness separately. Do not claim the
complete workflow from an offline acceptance receipt or fixture-backed writer.

## Repository anchors inspected

- `internal/runtime/{controller,execution,records/types,store/store}.go`: candidate, dependency and fence boundaries.
- `internal/contracts/workflow_types.go` and `schemas/syncoperation.json`: existing identities, four operation kinds and canonical hashes.
- `internal/connector/connector.go`: validated operation, opaque authorization context and conditional-write gates.
- `internal/tracker/{planning,connector,read_policy,transport,request_scope}.go`: drafts, ownership and read-only transport.
- `internal/decision/check.go`: evidence-bearing decisions do not grant authority.
- `packs/repository-delivery/src/repo_delivery/{evidence,integration,workspace,runtime_finalize}.py`: existing snapshot/gate/review and optional runtime-finalize machinery.
- `docs/implementation/phase-{5,6,7}-delivery-evidence.md`: independently pending connected gates.
