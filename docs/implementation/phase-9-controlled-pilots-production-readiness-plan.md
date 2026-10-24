# Phase 9 — Controlled pilots and production readiness

Status: implementation plan, 2026-10-24. This plan grants no execution,
publication, credential access or tracker-write authority. It follows the
[roadmap](end-to-end-workflows.md), [P8 delivery evidence](phase-8-delivery-evidence.md)
and [operator boundaries](../verification-delivery-writeback.md).

Two GPT-6 Luna workers at medium reasoning inspected pilot/evaluation requirements
and operational dependencies. A separate Luna-high review identified restore-authority, metric-definition
and calibration-admission gaps; the corrections are incorporated below. Implementation and live qualification remain different completion gates.

## Outcome and scope

Make readiness measurable, implement the missing connected integration seams,
and run separately authorized campaigns that can establish a narrowly scoped
production capability. A green offline implementation report never constitutes
a live qualification or deployment decision.

Deliver three independent results:

1. **P9-core:** versioned campaign/readiness contracts, evidence aggregation,
   transport integration and admission guards, offline replay, operational drills,
   release packaging and runbooks.
2. **P9 connected qualification and pilots:** observed conformance and complete
   read → admit → execute → verify → accept → deliver → reconcile evidence for
   each claimed route. These gates are currently blocked or pending.
3. **P9 production promotion:** externally approved, limited, expiring canary
   enablement after statistical and operational gates pass. This is not a blanket
   certification of all models, hosts, accounts, platforms or tracker operations.

Reuse P2 catalog identity/trust, P3 accounting, P4 selection/evaluation, P5 scoped
reads, P6 execution, P7 claims/checkpoints and P8 acceptance/delivery/outbox. Add
no second executor, accountant, shipping engine, background daemon or distributed
scheduler. Broader hosts, Codex Cloud, Jira Data Center, provisioning, webhooks,
new methodology enforcement and general campaign optimization remain P10 or
separately approved work.

## Current gate ledger

These are repository-observed statuses, not fresh live tests of installed tools.
Older P6/P7 delivery documents retain their historical phase-time statements;
P8 added offline acceptance/projections without qualifying live execution.

| Gate | Current state | Required evidence to change it |
| --- | --- | --- |
| P1–P8 offline foundations | Implemented within documented fixture/injected scope | Regression and distribution checks for each change |
| Codex P6 live | **Blocked** | Externally trusted, fresh, exact scoped controls plus actual bounded native conformance |
| Claude P6 live | **Independently blocked** | Its own controls, version/auth conformance, cancellation and required native-task observations |
| Native-task variants | Fixture observations only | Claude task availability modes separately exercised; Codex local thread/turn identity separately observed |
| P3 live collectors | Pending per host/auth/account/source | Callable supported source, complete dimensions, freshness/order/attribution and live conformance |
| P4 quality/demand evidence | Insufficient/unqualified for production routing | Independently accepted, signed, matching cohorts and finite applicable bounds |
| P5 provider reads | Pending separately for GitHub, Azure, Linear, Jira | Scoped account/API/auth conformance and captured complete observations |
| Command-verifier execution | Pending; CLI currently blocks | Implemented qualified control transport, independent environment and live negative tests |
| Author, verifier, reviewer and approval identity | External production qualification absent | Protected trust custody and authenticated identity/role bindings, not fixture keys |
| P8 provider write transport | **Engineering integration missing and conformance pending** | Concrete transport/CLI routing behind the existing boundary, then per-operation live tests |
| P8 milestone-evidence verifier | CLI currently blocks evidence-dependent operations | Resolver verifies actual required acceptance/delivery evidence before grant use |
| Repository publication | Local handoff core only | Exact destination verification, explicit action authority and observed publication/readback |
| P7 connected execution/recovery | Pending the selected complete path | Qualified host + capacity + quality + verifier/identity + durable recovery |
| Production auto-dispatch | Disabled | Completed relevant shadow, experiment, operational and promotion gates |

Credentials alone cannot close an unimplemented transport, unobservable quota,
untrusted control implementation or insufficient quality evidence. Discovery,
login, listing, smoke success and fixture certificates cannot substitute for
these gates. A missing host path blocks that path; another qualified host can
advance without granting a two-host claim.

## Contracts and readiness decisions

Add independently versioned closed `aok-pilot/v1` and `aok-readiness/v1` records
in `internal/pilot` and `internal/readiness`. Preserve P1–P8 wire formats. Reuse
existing signatures and evidence rather than introduce a competing qualification
issuer or a universal certificate that bypasses native validators.

| Record | Required content |
| --- | --- |
| RouteManifest | Host/binary digest/version, OS/arch, transport/control policy, exact configuration/profile, account/auth/pool, verifier/rubric/environment, provider origin/native tenant/container/item kind/operation/API/auth and delivery milestone |
| GateRequirement / EvidenceBinding | Required native validator, exact applicability scope, artifact digests, approved issuer/role, original observation/expiry, revocation and supersession dependencies |
| ReadinessReport | Every required gate, applicability, status/reason, evidence/input digests, earliest expiry, blocking next actions and permitted claim; no execution authority |
| PilotManifest | Purpose, dataset/family split, exact routes/baselines, budgets, trial IDs/repetitions, fixed policies, mandatory numerical decision rules and denominators, stopping protocol, named operators and retention |
| TrialRecord | Stable logical ID, runtime attempt/fence/holds, exact source/input/profile/host, observations, independent outcome, complete cost/time or explicit censoring and terminal reconciliation |
| CampaignFreeze / AuditOpen | Dataset/policy/estimator/code/verifier digests, externally witnessed signing and locked-audit access event; no caller-supplied timestamp treated as chronology proof |
| OperationalDrillReceipt | Failure injection, protected state/backup identities, independent authority-witness epoch, before/after integrity, bounded stop/recovery/reconcile observations and unresolved exposure |
| PromotionAssessment / PromotionGrant | Assessment versus fixed protocol, approved exact scope/action, budget/concurrency, issuer, expiry/revocation and rollback conditions; assessment alone grants no authority |

Gate statuses are `passed`, `blocked`, `stale`, `unknown`, or explicitly
`not-applicable` under a preregistered scope rule. Required unknown/stale gates
block. No aggregate score, majority vote or green test count hides a failed gate.
Not-applicable cannot waive a mandatory capability, quality, permission or capacity
gate. Report optional failures without silently removing them from the matrix.

Readiness validates evidence assets, native signatures, category, scope, dates,
revocation and original provenance. Hashes establish integrity, not authorship.
External operator policy and protected issuer keys remain trust roots. Fixture
issuers cannot become production issuers by copying a bundle. P9 does not certify
an operator, isolation implementation or reviewer merely by verifying a signature.

An offline report is a reproducible snapshot using required `--as-of`. Execution
uses trusted current UTC and monotonic budgets and reruns the underlying P2–P8
gates at admission, launch and each publication/send boundary. A readiness report
is never a transferable launch token. Control/policy changes and proof refresh
must follow existing exact-digest invalidation; no automatic rebinding.

## Work packages, dependencies and acceptance

| Package | Dependencies | Deliverable | Acceptance criteria |
| --- | --- | --- | --- |
| P9.0 Scope and contracts | P1–P8 core; current gate inventory | Closed route/campaign/readiness contracts, claim vocabulary and traceable dependency graph | Reject malformed/duplicate JSON, unknown versions, unsafe paths, changed assets and fabricated scope/authority; existing commands unchanged without opt-in |
| P9.1 Readiness aggregation | P9.0; native P2–P8 validators | Offline requirement graph/report and drift evaluation | Every missing/stale/conflicting/revoked required input blocks; fixture evidence cannot pass live requirements; deterministic inputs/time produce identical report |
| P9.2 Trusted connected seams | P9.0–1; existing host/collector/verifier/connector boundaries | Concrete transport registration, externally pinned trust/identity and verifier/milestone adapters | No generated production proofs, task-chosen credential destination or uncontrolled subprocess; cancellation, redaction, budgets and exact scope enforced; CLI defaults remain blocked |
| P9.3 Live host/capacity/verifier conformance | P9.2; actual operator resources | Per-host/auth/source/control observations, identity and accounting conformance | Codex and Claude independently pass allowed/denied access, ambient isolation, interruption/cancel/stop, fresh quota and actual settlement tests; unsupported source/limit stays blocked |
| P9.4 Provider and delivery conformance | P9.2; provider-specific P5/P8 slices | GitHub first; Azure next; Linear then Jira; scoped publication path | Read/write/readback/reconcile observed per exact operation/auth; native conditionals tested where required; one-send and ambiguous-effect recovery verified; no inferred merge/Done/creation permissions |
| P9.5 Dataset, calibration and audit evidence | P9.0–3; qualified independent verifier; approved experiment enrollment | Curated/frozen trials, signed outcome shards, demand bounds and evidence lineage | No family leakage, retry/repetition inflation, censored-zero imputation or token-to-quota conversion; P4 actual cohort/floor/calibration requirements met before production eligibility |
| P9.6 Shadow and controlled comparison | P9.1, P9.3–5 | Observed shadow ledger and preregistered controlled route comparisons | At least 100 distinct eligible shadow opportunities plus all abstentions reported; no shadow dispatch; statistical targets and operational negatives resolved before promotion |
| P9.7 Operational drills and release readiness | P9.1–4; parallel with P9.5–6 | Stop/recovery/backup/restore/migration/incident rehearsals, support manifest and release evidence | Restore preserves fences/holds/revocations/one-send state; externally witnessed epochs fence stale clones or activation stays blocked; rollback closes admission without deleting uncertain obligations; exact supported platform passes |
| P9.8 Bounded promotion and expansion | P9.6–7; exact selected route fully qualified | Expiring operator-approved canary activation and per-route delivery matrix | Promotion grants cannot waive hard gates; automatic stop and guard rechecks tested; expand only after evidence/rollback review; broad claims remain blocked for unqualified tuples |
| P9.9 Independent review/distribution | Integrated P9-core; connected receipts separately | Review, operator guide, regression/package evidence and truthful gate matrix | Offline checks pass; independent audit has no material open findings; core/connected/promotion statuses published independently |

P9-core can be implemented while external gates remain blocked. P9.3/4/5 live
campaigns and P9.8 cannot be reported complete from fixtures. Collecting more
synthetic observations never resolves unavailable production controls or sources.

## Missing engineering and qualification work

P9.2 owns concrete integration, not just another layer of report JSON:

- Connect supported read-only Codex rate-limit transport and the supported Claude
  usage/quota source without scraping unsupported private state or guessing
  subscription remaining capacity. Usage events alone do not prove remaining
  windows. Preserve account/source ordering and completeness semantics.
- Supply independently controlled command-verifier execution through the existing
  `verification.Controls` boundary. Pin actual executable/toolchain/environment,
  scratch/output locations and filesystem/network/ambient enforcement. Verify
  source before/after; a policy boolean cannot qualify the implementation.
- Implement separate provider write and readback transports plus opt-in CLI
  resolution behind `connector.DispatchOnce` and the P8 opaque permit/outbox
  boundaries. Keep P5 reads read-only. Exact protected origin/auth/principal,
  redirect denial and bounded response capture remain mandatory.
- Resolve required milestone evidence from actual immutable acceptance/delivery
  bundles. Verify signed criterion coverage, verifier/review references and current
  runtime projection before a completion operation can obtain an execution token.
- Establish protected author/reviewer/approval/projection identity custody and
  revocation procedures. Imported policies must not self-select their own trust
  anchors. Production signing keys and credentials remain outside task workspaces.
- Reuse the repository-delivery engine for actual preservation/integration and
  separately authorized publication. Source acceptance cannot be transplanted to
  a changed destination, rebase or stale PR head. Local runtime and swarmd may
  not both finalize the same attempt.

Refresh official API/CLI documentation and verify the pinned installed behavior
when implementing each transport; existing fixtures are protocol evidence, not
current vendor guarantees. Record exact tested auth and platform variants. Do not
advertise a provider-wide guarantee from one scoped comment test.

Existing bounds remain binding: P6 currently rejects host attempts over **60
seconds**; P8 command verifiers cap at **30 seconds**. Fit initial trials into
those bounds and fixed-DAG steps. Longer tasks require a separately versioned,
reviewed bounded transport extension with timeout/process-tree/renewal tests and
live requalification before use. Merely increasing a manifest budget cannot
raise a transport limit. Short-lived controls need explicit renewal/new attempts,
not silent replacement of exact policy bindings.

## Evidence bootstrap without a production quality bypass

P7 production admission requires qualified P4 evidence. Initial E3 evidence does
not exist yet; sending cold-start configurations through that path would either
deadlock or silently waive a hard gate. Define two separate campaign purposes:

- **Calibration experiment:** explicitly authorized pinned research trials in
  disposable evaluation repositories/accounts. An externally signed preregistered enrollment binds
  configuration, exact disposable repository/account allowlists, inputs, attempts,
  resource upper bounds and test scope. It makes
  no production suitability or selected-model claim.
- **Production-policy pilot:** P4-qualified routes only, all normal P7 hard gates
  intact. A pin, promotion grant or pilot label never overrides those gates.

The experiment coordinator composes existing P6 transport, P7 durable attempt /
fencing/checkpoint primitives and P3 reservation/launch/reconcile accounting.
Factor shared admission code if needed rather than copy an executor/accountant.
Use a new explicit purpose record and prevent its receipts from masquerading as
production admission or automatic selection. No general `--ignore-quality` flag.

The separate calibration admission API changes only the requirement for qualified
P4 production eligibility. It does not grant a selector recommendation, waive any
other safety gate or mutate the existing P7 production guard. Closed calibration
records persist `productionEligible:false`; production admission rejects them by
kind/purpose. A dedicated signed experiment grant covers only the preregistered
trial IDs and bounds. Tests must prove a cold-start experiment can run when all
other gates pass, cannot run with any other gate missing, and cannot be relabeled
or submitted as production admission. Independent verified outcomes require the
normal separately approved P4 shard conversion; raw trial receipts are not shards.

Experiments still require qualified controls, exact capability/data/account scope,
independent verification, spend/attempt/time authority and fresh P3 observed-fit
for every applicable window. Where empirical demand is absent, only trusted
explicit upper bounds in the correct pool/window units may cover that absence;
they remain unmeasured. Unknown quota, unobservable subscription draw or unresolved
usage blocks subsequent experiment admission just as it blocks production.
Do not derive percentage points from tokens or refund an uncertain hold.

If those prerequisites cannot be supplied, remain blocked. Separately imported
external experiments may contribute P4 evidence only through normal approved
issuer/shard verification, exact identities and dataset/protocol requirements;
external execution is not qualified AOK runtime execution.

## Dataset, statistics and comparison protocol

Keep the roadmap's 48-packet pipeline pilot: eight packets in each of six classes,
repository/family-grouped **24 development / 12 validation / 12 locked audit**,
two or three configurations and three preregistered repetitions (**288–432
attempts**). Near-duplicate variants stay in one partition. This demonstrates
pipeline coverage and correct abstention, not broad per-class quality or a
winner's superiority.

Claim-bearing cohorts need additional families in their actual development,
calibration and independent audit partitions. Preserve P4 rules:

- Exact configuration/profile, class, language/risk, account and attempt/review
  protocol; no pooling across incompatible hosts/accounts/configurations.
- At least 30 independent task families and the actual one-sided 95% Wilson lower
  bound clearing **0.80 standard / 0.95 high risk**. The high-risk floor first
  becomes feasible at 52 all-success families; failures require more.
- Family success requires all preregistered repetitions independently accepted.
  Repetitions and retries do not increase independent sample count.
- At least 30 independent calibration families for split-conformal consumption
  bounds; use the existing worst-family statistic/rank. Missing finite bounds
  stay unknown. Include failed/interrupted trials and censored consumption.
- Preserve original validity periods and exact live quota generations. Estimates
  never carry historical generation IDs into a new reset window.

Freeze datasets, model/profile/host versions, policies, estimators, toolchains,
verifiers and preregistered analyses before opening the audit split. P4 currently
checks supplied partition/freeze metadata but does not prove real-world chronology
or semantic near-duplicate separation. P9 adds approved curation attestations,
protected freeze/audit-access records and independently auditable custody. A
signature alone does not prove nobody saw the audit data; operator controls must
be part of the qualified protocol.

Compare pinned, quality-only, cost-aware, quota-aware and reservation-aware
policies with the same capability, quality, permission and applicable capacity
gates. Do not drop shared holds to make a baseline appear different. Label unsafe
capacity ablations counterfactual and exclude them from safety denominators.
Recorded-state replay has no causal savings claim. Synthetic closed-loop replay
requires explicit consumption/reservation/external-activity/replenishment events;
clock passage never replenishes quota.

Live comparisons preregister workload-specific non-inferiority margin, accepted-
result cost/time objective, quota-block reduction target, minimum independent
sample count, confidence method, stopping/maximum budget and missing-data rules.
These are project decisions, not vendor facts. Control order and external quota
activity; shared accounts create interference, so randomize/block at an appropriate
account-window or campaign unit rather than claim task-level independence.
Confidence intervals and power must match that unit. Audit data is not tuned on;
inconclusive results do not promote. Repeated interim looks require a fixed
sequential protocol or independent new audit campaign.

## Mandatory metrics and promotion decision rules

Every campaign manifest must specify finite numerical targets, units, direction,
minimum independent sample count, analysis unit, confidence method/level,
comparison baseline, missing/censoring rule and stop rule **before execution**.
Reject missing values, NaN/infinity, incompatible units, ambiguous denominators
and post-audit changes. Promotion is `insufficient-evidence` if a required bound
cannot be computed or does not resolve the preregistered criterion. There is no
implicit numerical default for business improvement or statistical power.

| Metric | Required definition / acceptance |
| --- | --- |
| Quality | Preregistered family-level independently accepted result; P4 family counts and Wilson floors remain mandatory; comparison non-inferiority margin and interval rule must also be explicit |
| Consequential miss | Blind rubric/severity definition, audited misses / assessed independent families, unknown/unassessed counts and explicit stop threshold |
| Accepted-result cost/time | Complete preregistered episode cost/elapsed time including failed/interrupted/setup/review/recovery effort, divided by independently accepted results; zero accepted results or censored totals cannot become a finite positive savings claim |
| Capacity outcomes | Separate pre-start capacity deferrals / pre-capacity-qualified opportunities from provider quota denials / admitted starts; stratify and analyze by account-window cluster |
| Quota avoidance | Benefit claims require a registered controlled comparison; unknown external consumption and observational counterfactuals do not prove avoidable blocks |
| Observation coverage | Complete and fresh required dimensions / all required observations; unknown, stale, missing and rejected samples reported separately with explicit allowed bounds |
| Prediction coverage | Actual bounded consumption within preregistered upper estimate / independently observable trials, with censored trials separately retained |
| Operational safety | Counts of unauthorized requests, hard-gate bypasses, false acceptance, local over-admission and automatic ambiguous resends; deterministic acceptance requires zero |
| Recovery / service bounds | Measured stop, reconciliation and restore latencies against explicit finite limits; unresolved processes/holds/effects mean recovery is incomplete, not success |
| Stability | Switches, interventions, deadline misses, orphan processes, uncertain holds and unresolved write intents, with declared denominator/time window |

A pre-capacity-qualified opportunity passes non-capacity hard gates; it may be
quota blocked. A fully eligible opportunity passes **all** P4/P3 gates and is the
unit counted toward the 100-shadow threshold. Record both rather than improve
rates by discarding capacity failures. Prevent zero-denominator ratios from
becoming free cost, perfect coverage or success.

Keep consumption units and dated pricing evidence separate from actual charges.
Subscription percentages cannot be converted to cash savings without a registered
allocation method and adequate evidence; unobserved charges remain unknown.
Quality/cost/time ranking indices are not measured accepted-result guarantees.
Multiple endpoints and interim looks need a preregistered error-control procedure;
do not select whichever metric or confidence interval happens to pass.

The recommended initial live scope is standard-risk, non-sensitive disposable
engineering work. High-risk production tasks remain excluded from the initial
canary; later admission requires their existing stricter floors plus separately
approved workload-specific safety protocol. Supplying campaign target values is
an operator approval dependency, not a request to loosen quality floors.

## Pilot ladder and operational limits

| Stage | Scope | Exit criteria |
| --- | --- | --- |
| A — Offline rehearsal | Fixtures, frozen replay and injected failure states | All hard-gate negatives, privacy, accounting and crash/recovery cases pass; no live calls |
| B — Disposable conformance | One exact host route or provider operation at a time | Actual positive/negative/stop/recovery evidence; budgets and credentials separately authorized |
| C — Calibration and shadow | Pinned experiments plus read-only production-like decision observation | P4 cohorts/bounds qualified; at least 100 distinct eligible shadow decisions, with all blocked/abstaining opportunities retained |
| D — Controlled comparison | GitHub + Codex first; fixed qualified routes, advisory decisions and explicit starts | Preregistered quality/cost/capacity targets resolved, mandatory failure/recovery scenarios pass |
| E — Bounded canary | Same exact tuple, limited automatic model choice/dispatch under explicit promotion grant | Operational soak and statistical gates pass; stop/rollback exercised; no open critical incident |
| F — Expansion | GitHub + Claude, then Azure, Linear and Jira using a proven host | Full delta qualification per host/provider/auth/operation/platform; no inherited blanket pass |

A shadow decision is a real, current, signed/observed input opportunity. Duplicate
replays and repeated polling of one unchanged task do not count toward 100.
Shadow cannot dispatch, reserve execution capacity, publish, write trackers or
claim task success. The 100 threshold tests observation/decision operations; it
cannot substitute for P4 family counts or an E4 causal comparison.

Initial canary defaults are AOK engineering limits, not statistical proof:
one repository/container, one approved account route, concurrency one, ten task
starts in the first tranche, one attempt per trial, no automatic fallback, no
billing/account switch and one send per reviewed outbox operation. Three planned
repetitions are separate trial intents, never automatic retries. Use the stricter
existing timeout limits. Start and monetary/consumption caps must be explicit,
finite and externally approved; there is no safe invented universal dollar cap.
Minimum soak is seven days including the applicable observed reset transitions;
extend if required windows/events are not observed. Counts/soak do not override
statistical or recovery failures. Increasing limits requires a new reviewed grant.

All automatic dispatch requires exact P4 eligibility and fresh P3 accounting;
shared external activity can still consume quota. Revalidate before launch, keep
holds/fences and report provider quota blocks truthfully. Reset times are hints:
wait, obtain fresh observations and rerun rather than assume future fit.

## Host and provider qualification matrix

First conformance on one disposable, specifically authorized scope; no production
account exhaustion or deliberate unsafe write race against human-owned work.

| Path | Initial slice | Mandatory negative/recovery cases |
| --- | --- | --- |
| Codex | Exact local executable/account/model/profile; local thread/turn | Denied file/network/ambient access, tool/limit refusals, malformed frames, cancellation/process stop, lost observations, restart/fencing |
| Claude | Exact executable/auth/model/effort; native task modes separate | Same controls; TaskCreate/Get/List/Update available/unavailable variants without inventing unsupported native IDs |
| GitHub | Scoped read + marker evidence comment + complete readback | Wrong immutable-ID/issue-number mapping, partial pages, altered/duplicate marker, lost success reply, permissions/rate limit |
| Azure DevOps | Scoped work-item read + one mapped field with native revision test | Stale revision/human edit, wrong org/project/process/field, inaccessible item, response loss and converged-state versus causal-effect distinction |
| Linear | Scoped issue read + UUID comment | HTTP-200 GraphQL errors, nested pagination, wrong workspace/team/issue, duplicate/corrupted marker and eventual visibility |
| Jira Cloud | Scoped numeric issue read + ADF comment | Site/gateway auth variants separately, wrong cloud/self URL/issue, rich-text loss, incomplete pages, duplicate content and ambiguous response |
| Verifier/delivery | Read-only candidate, isolated reports, independent authenticated review, exact destination | Source mutation, toolchain/environment drift, self-review, changed destination/rebase/PR head, failed gate, missing helper or action authority |

Read support is not write permission. Comments do not qualify fields, transitions,
creation, merge or deployment. Azure conditional behavior must be observed, not
inferred from fixture JSON Patch. For other state updates preserve P1's conditional-
write gate. Creation remains default-off and needs a separate provider-qualified
creator, P8 one-use permit, assigned-ID readback and real binding. Native agent
tasks are observations, not authority to mark AOK accepted or tracker Done.

## Stop, recovery and release readiness

Stop admission on any expired/revoked/mismatched proof, unknown mandatory input,
source/version/scope drift, insufficient headroom, unbounded cost/time, absent
verifier/identity, failed acceptance, stale destination or missing write grant.
Rollback to an explicit qualified pin/advisory/defer; a pin is not a gate bypass.

Immediately suspend the affected route/campaign on any unauthorized request,
control escape, false acceptance, quality-floor bypass, local over-admission,
wrong account attribution, duplicated automatic send, unexpected billing/credential
switch, exposed secret, stale-fence promotion or unrecoverable loss of state.
Consequential misses, quality/cost targets, unknown measurement coverage, deadline
misses and error rates follow stricter preregistered stop rules where applicable.
Classify incident scope: stop one route or the whole authority if shared state/
trust is affected. Every unresolved severe incident blocks promotion.

Stopping closes new claims, attempts cancellation and preserves durable evidence.
It does not prove process termination, undo a provider effect or release uncertain
usage. P8 send-intent remains the authorization-consumption boundary: revocation
before it blocks; racing after it cannot guarantee suppression. Reconcile read-only
before further mutation; a new grant alone does not make an ambiguous resend safe.

Operational acceptance requires rehearsed procedures for:

- Host death/ambiguous stop, quota collector outage/correction, clock regression,
  concurrent external use, reserve/launch/settle crashes and hold recovery.
- Checkpoint continuation after owner loss, cancellation/revocation and changed
  input. Keep P8 acceptance projections bound to the complete P7 manifest digest;
  original git base and checkpoint source HEAD are separate identities.
- Tracker crash before/after intent, response loss, delayed readback, conflicting
  human edits and partial multi-operation results. Preserve original grant scope,
  nonce use, admitted generation, asset hashes and native identity.
- SQLite-aware backup/restore of runtime, usage and outbox authorities with evidence
  stores and protected trust/revocation state. Record a quiescence protocol and
  per-store consistent snapshots; do not claim a cross-database atomic backup.
- Restore into a quarantined non-dispatch state; verify WAL/audit integrity,
  revocation/clock high-water and all held/in-flight/uncertain obligations. Prove
  old authority is stopped/fenced before activating a replacement. Never run a
  stale backup beside the original or regain send budgets by rolling back a DB.
- Explicit migration rehearsal, interrupted-migration recovery and upgrade/version
  drift detection. Binary rollback must not silently downgrade newer store formats;
  retain a compatible recovery binary and forward repair path where necessary.
- Key compromise/rotation, credential revocation, privacy/retention, evidence export,
  incident triage and recovery-owner handoff. Separate task content from authority.

**Restore fencing mechanism:** local SQLite epoch, nonce, revocation and clock
markers are not an anti-rollback anchor when they are restored with the backup.
Add an injected, independently protected `AuthorityWitness` contract, outside the
restored/cloneable authority domain, that supplies a current monotonic activation
epoch and enforces single-writer activation. Qualify its rollback resistance and
availability; a second file copied with the same backup is insufficient. P9 does
not create a new accountant or distributed scheduler for this purpose.

Bind activation epoch to host admission and provider grant consumption. The witness
must remember consumed logical operation/trial IDs across replacement epochs and
reject superseded authority tokens at each new admission/send-intent boundary.
Close the old epoch before activating a replacement; if it cannot be fenced,
restore stays inspection-only. Evidence of process stop alone cannot establish
that no stale clone or previously admitted network request exists. The witness
must itself be externally qualified; a signed caller-chosen epoch is not proof.

Already consumed/in-flight requests remain ambiguous across epoch changes. Drain
or reconcile them read-only before allowing any overlapping operation, preserve
all spent logical IDs and verify native non-duplication conditions. A freshness
lease does not retroactively cancel a request authorized before revocation. If
no qualified external witness or equally strong operation-specific fencing exists,
P9 can deliver quarantined restore/replay tooling but **cannot pass production
replacement/restore activation**. Missing this anchor explicitly blocks the
corresponding operational-readiness and promotion claim.

P3 cross-window settlement/migration is currently fail-closed. Either implement
and qualify exact allocation/reconciliation under a separately versioned change,
or keep affected routes blocked across that boundary; a pilot cannot waive it.

Release readiness binds source revision, executable and dependency hashes,
configuration/schema migration versions, supported exact tuples, test/drill/pilot
receipts, known gaps, retention and recovery instructions. Existing unsigned
release checksums are integrity evidence; they do not authenticate a publisher.
Use externally protected deployment allowlists/attestations for production trust
and never fabricate a signed-release claim. No automatic install, release publish,
PR review submission, merge or deployment follows a green readiness report.

## Proposed CLI and implementation shape

New commands are proposed, not currently available:

```text
aok readiness validate --manifest FILE --policy FILE --as-of RFC3339 [--json]
aok readiness report --manifest FILE --policy FILE --as-of RFC3339 --out NEW [--json]
aok pilot validate --manifest FILE --policy FILE --as-of RFC3339 [--json]
aok pilot replay --scenario FILE --as-of RFC3339 --out NEW [--json]
aok pilot assess --campaign BUNDLE --policy FILE --as-of RFC3339 --out NEW [--json]
aok pilot execute --campaign FILE --authority FILE --once --out NEW [--json]
```

`execute` is opt-in, bounded and unavailable until P9.2/3/4 dependencies for its
purpose are qualified. Execution time cannot be backdated with `--as-of`. A
campaign manifest or passing assessment is not execution consent. Promotion grant
verification integrates existing admission and outbox boundaries; do not implement
a second generic `apply` authority or revive read-only work-system provisioning.
Connected collector/verifier/provider commands gain explicit protected route and
qualification arguments rather than unconditional stubs or automatic discovery.
Reconciliation uses read authority separately from unexpired mutation grants.

Use current bounded safe reads, private create-only evidence output, stable IDs,
exact decimals and exit conventions (0 valid/success, 1 blocked/invalid/conflict,
2 misuse). Validation/report/replay perform no model launch, credential lookup,
tracker mutation or network activity. Side-effecting operations use real time,
protected revocation and immutable recorded evidence.

## Acceptance, staffing and publication

After contracts lock, use two Luna-medium workers in waves: one owns
readiness/campaign evidence/replay; the other owns transport/operations fixtures
and drill harnesses. Primary owns cross-phase integration, statistical protocol,
CLI and live-gate decisions. Adapter work proceeds GitHub, Azure, Linear, Jira;
each requires separate review. A Luna-high reviewer audits the integrated result
independently, including bootstrap separation, grant/fence races and statistical
claims. Do not delegate production approval to a model vote.

Core tests cover stale/revoked/contradictory proof, fixture-to-live laundering,
version/auth/principal drift, cold-start abstention, repeat/family leakage,
conflicting acceptance receipts, reset hints, shared windows, missing measurements,
privacy canaries, duplicate delivery and restoration of ambiguous holds/intents.
Require zero unauthorized starts/requests, false acceptance, hard-gate bypasses,
local over-admission and automatic ambiguous resends in deterministic fixtures.
Unknown outcomes remain unknown; absence of observed incidents is not proof of
zero production risk.

Run focused tests, full Go 1.22, relevant races and multiprocess crash/restore
checks, Python regressions, kit validation, release/extracted-kit checks,
empty-PATH/no-credentials/network-denied offline smoke and six-target CGO-disabled
builds. Test actual native platforms only within claimed scopes; cross-compilation
is not certification. Optional repository-delivery helpers retain their explicit
Python/platform requirements; core offline commands remain Go-only.

Publish separate P9-core, host/collector/verifier, identity, provider-operation,
delivery, dataset/quality, shadow, controlled-pilot, operational-drill and
production-promotion statuses. M2 can claim one fully evidenced route; broader
M3/all-requested-host/provider support requires every stated combination. Do not
turn one passing matrix cell into the Cartesian product of hosts and providers.
If external gates remain absent, deliver the core and truthful blocked reports;
mark connected pilots and promotion pending rather than claim Phase 9 complete.
