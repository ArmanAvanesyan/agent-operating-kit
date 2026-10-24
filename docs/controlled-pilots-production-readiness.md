# Controlled pilots and production readiness

Phase 9 adds opt-in offline readiness and pilot evidence tooling. It also adds
injected transport and admission integrations. The distributed CLI has no
production service registration, issuer custody, activation witness or qualified
pilot enrollment. Host and provider live gates remain blocked/pending. No pilot,
model invocation, publication or tracker mutation was performed to implement it.

Read [the implementation plan](implementation/phase-9-controlled-pilots-production-readiness-plan.md)
and [delivery evidence](implementation/phase-9-delivery-evidence.md). The
[host](host-bridges.md), [usage](usage-accounting.md),
[runtime](runtime-operations.md), and [verification/writeback](verification-delivery-writeback.md)
boundaries still apply.

## Offline commands

Use the built Go binary. These examples use deliberately unqualified fixtures;
readiness and assessment exit 1 and write their blocking explanations.

```sh
aok readiness validate --manifest internal/readiness/testdata/route.json \
  --policy internal/readiness/testdata/policy.json --as-of 2026-10-24T12:00:00Z --json

aok readiness report --manifest internal/readiness/testdata/route.json \
  --policy internal/readiness/testdata/policy.json --as-of 2026-10-24T12:00:00Z \
  --out /absolute/new-readiness-directory --json

aok pilot validate --manifest internal/pilot/testdata/manifest.json \
  --policy internal/pilot/testdata/policy.json --as-of 2026-10-24T12:00:00Z --json

aok pilot replay --scenario internal/pilot/testdata/scenario.json \
  --as-of 2026-10-24T12:00:00Z --out /absolute/new-replay-directory --json

aok pilot assess --campaign internal/pilot/testdata \
  --policy internal/pilot/testdata/policy.json --as-of 2026-10-24T12:00:00Z \
  --out /absolute/new-assessment-directory --json
```

A campaign directory contains `manifest.json` and `bundle.json`; its policy is
supplied separately.

`pilot validate` validates structure and exact manifest-policy binding; success
says nothing about evidence qualification. An assessment is review-only and
always retains `authorizationGranted:false`, `executionReady:false` and
`productionEligible:false`. Unknown/stale/revoked/contradictory required evidence
blocks readiness. Hashes establish integrity, not authorship.

Each output directory must be new and outside protected inputs. Reads are bounded
and reject symlink ancestors, duplicate keys, unknown fields/versions and changed
asset hashes. All offline evaluation uses explicit RFC3339 `--as-of`. Real
execution, grant expiry and revocation checks use current time; `pilot execute`
accepts no `--as-of`.

## Campaign and statistical scope

`aok-pilot/v1` freezes exactly one route per campaign. That route binds the
host/version/binary, transport/control policy, platform, configuration/profile,
account/authentication, provider identity/API/operation and verifier identity.
Use separate campaigns to qualify another route; one passing cell never implies
the host/provider Cartesian product. Every measurement binds the frozen route.

Named metric rules have fixed units, direction, analysis unit and denominator.
Quality uses one-sided 95% Wilson bounds over independent families, with every
preregistered repetition accepted for family success. Minimum 30 families and the
actual .80 standard floor apply; the .95 high-risk floor first becomes feasible
at 52 all-success families. The initial runtime coordinator supports standard-risk
trials only. Repetitions do not create extra independent families.

Typed complete episodes include failed/interrupted/setup/review/recovery cost and
time, divided by independently accepted results. Missing, censored, unknown or
zero-accepted episodes cannot support a favorable bound. Paired account-window
observations separate capacity deferrals/eligible-before-capacity from provider
quota denials/provider-attempts. Quota-avoidance comparisons require the declared
paired comparison and exact account/route identity. These are fixed-method offline
statistics; they do not establish causal savings from recorded replay.

Signed freeze and audit-open receipts require separate trusted roles and a
protected external chronology witness. Supplied timestamps cannot prove the audit
was unopened. The qualification verifier must check actual native/P4/P3 evidence,
receipt acceptance, preregistration and comparison protocol. Fixture verifier
callbacks are not production qualifications.

At least 100 distinct eligible shadow opportunities are required; abstentions
remain separately reported. Replays, duplicate input records and polling cannot
inflate that count. Shadow dispatch is rejected. Preserve the roadmap's 48-packet,
24/12/12 grouped split and 288–432-attempt pipeline pilot separately from the larger
claim-bearing cohorts required by P4. No qualified production dataset is shipped.

## Protected integration and remaining gates

`cli.RunWithPhase9Services` supports explicit programmatic registration from a
protected embedding application. Task data and CLI flags cannot install services,
select issuer keys, activate a witness or choose credentials. The standard `aok`
binary installs no services; `pilot execute --once`, provider dispatch/readback,
verifier execution and live collection remain blocked when their service is absent.

| Integration | Implemented boundary | Qualification still required |
| --- | --- | --- |
| Readiness | Fixed mandatory gates; exact scopes/freshness; native host control, acceptance and local handoff validators | Protected policy authority, current revocation/projection and actual per-gate live conformance |
| Codex quota | Bounded injected App Server initialize/read channel; allowlisted rate-limit payload | Exact executable/account/auth/source and capture ordering; no process launcher or credential discovery |
| Claude quota | Existing supported observation normalizer | Supported fresh remaining-window source; usage totals alone remain insufficient |
| Provider writes | Private HTTPS transport behind opaque connector permits; existing typed GitHub comment, Azure revision-tested field, Linear comment and Jira ADF comment builders | Every operation/auth/origin/principal separately; discovery/read support is insufficient |
| Command verifier | Pinned proof/spec HTTPS service protocol, bounded/cancellable and screened output | Independently qualified service/isolation, actual proof issuer and current environment; project-defined protocol is fixture-tested only |
| Milestone dependencies | Revalidation of signed acceptance, candidate lineage/current projection and local delivery artifacts | Exact operation-bound target plus qualified publication/readback verifier for delivered claims |
| Pilot production admission | `Controller.DispatchPilotOnce` composes ordinary P7 dispatch with sealed assessment, signed promotion, pinned enrollment, current activation and cumulative budget guard | Actual native qualification/P4/P3/issuer/custody/budget/witness implementations |
| Calibration | Separately signed disposable-trial grant; purpose-specific P7 coordinator; non-quality guard evidence and atomic external consumption | Qualified P6 transport, enrolled upper bounds, protected independent services and connected conformance; no ordinary CLI quality-bypass switch |

## Completing the existing P3–P10 gates

The [completion plan](implementation/remaining-gates-and-first-pilot-plan.md) stays
within the ten-phase roadmap. Its
[implementation evidence](implementation/completion-delivery-evidence.md) separates
offline/injected code from connected qualification and production activation.

Inspect the deterministic blocked-gate inventory with a built binary:

```sh
aok pilot gates --as-of 2026-10-24T12:00:00Z \
  --out /absolute/new-gate-directory --json
aok pilot experiment validate \
  --bundle internal/pilot/experiment/testdata/bundle.json \
  --as-of 2026-10-24T12:00:00Z --json
```

`pilot gates` exits 1 while writing its inspection-only ledger. The digest binds
the dependency inventory, explicit blockers and five separate implementation,
fixture, live, platform and activation columns. This inventory cannot accept its
own evidence or grant an operation. `experiment validate` checks structural
contracts only; the included fixture has no trusted signature or credentials.

Protected embeddings can install `Phase9Services.ExperimentExecution` and
`CalibrationExecution`. The corresponding commands require an exact enrolled
record, a new output directory, `--once` and current-time checks:

```sh
aok pilot experiment execute --bundle /absolute/enrolled-bundle.json \
  --once --out /absolute/new-experiment-observation --json
aok pilot calibration execute --campaign /absolute/calibration-manifest.json \
  --authority /absolute/calibration-grant.json \
  --once --out /absolute/new-calibration-observation --json
```

The ordinary binary installs neither service. In their absence these commands
return a blocked report before reading enrollment files, creating output, taking
claims or invoking a model. File contents cannot install services. A protected
embedding must independently qualify its custody, current authority, budget,
stop monitor, verifier, host and accounting services.

Experiments use distinct `aok-experiment-manifest/v1`, grant and bundle records,
signature domains and issuer role. They do not use a production promotion grant
and cannot enter production dispatch by relabeling. Ordinary P4 selection and
fresh P3 reservations still run for each attempt. Protected readiness v2 requires
all universal gates plus independent acceptance, local handoff, and the selected
provider operation. A delivered claim also requires publication; unrelated
providers are outside the exact route. V1 interpretation is unchanged.

Calibration retains its original quality abstention. Explicit protected bounds
are bound to the complete task cohort, captured provenance, signed input manifest,
spend/currency/deadline, account, exact profile and current capacity generations.
Its purpose-specific admission never converts that abstention into a selected or
quality-qualified result. Both paths reuse P7 staging/claims/fences, P3 holds, the
P6 native runner and P8 acceptance boundaries. They consume external campaign and
trial allowances before launch and never refund after ambiguous failures.

The initial route is singleton, standard-risk, one attempt, at most 60 seconds,
without resume/delegation/fallback. Known quota reset boundaries block attempts
that could cross them; renewed observations never clear unresolved holds.
Cross-window settlement remains unsupported and fail-closed.

Trial observations are not independently accepted outcomes, tracker grants or
production eligibility. The P8 verifier, preserved local handoff, separately
authorized comment outbox and signed readback remain separate required steps.
No controlled pilot or connected operation was performed by adding these APIs.

Provider transports forbid redirects, retries, task-supplied request paths and
credential forwarding. Response capture preserves exact numbers, removes
credential-shaped properties and rejects credential echoes. Their construction
never mints a connector permit. P5 read transports and work-system setup remain
read-only. Remote readback is separate from a successful mutation response.

The production coordinator initially permits a singleton pinned candidate and
single fixed-DAG step with runtime concurrency one and no automatic retry. It
checks captured complete input, account, platform, binary/control policy,
configuration/profile and protocol, then rechecks at launch. It retains normal
P4 selection and P3 reservation/launch/reconciliation. Calibration purpose is
rejected on this path. External grant revocation and native evidence are rechecked
while the attempt is guarded. A passing assessment alone never dispatches.

Promotion is a separately signed, principal/role-bound, narrow expiring grant with
current external revocation verification. Cumulative experiment/campaign budgets
need an atomic protected guard; no in-memory counter is offered as durable budget
authority. P6's 60-second and verifier's 30-second transport limits remain binding.
Longer tasks need separately reviewed transport support and live requalification.

## Backup, restore and incident operations

```sh
aok operations snapshot --database /absolute/current-store.sqlite \
  --store-id runtime --out /absolute/new-quarantine --json
aok operations inspect-snapshot /absolute/new-quarantine --json
```

Snapshotting uses SQLite `VACUUM INTO`, captures committed WAL state, checks source
integrity and bounds size/time. It is a consistent **single-store** snapshot, not
an atomic runtime/usage/outbox backup. The output is private and inspection-only;
there is no CLI restore/activate command. Source stores are not migrated or reset.
Quiesce and coordinate all authorities before an operational backup, retain evidence
and protected trust state, and rehearse migration/recovery on disposable copies.

`operations.Activation` requires an externally protected monotonic single-writer
witness and an independently supplied witness verifier. At each new host launch or
provider-send boundary, the witness must atomically consume the stable logical ID
and retain consumption across replacement epochs. A copied local epoch/nonce file
is insufficient. Missing witness qualification leaves replacement inspection-only.
Already consumed/in-flight effects remain uncertain; epoch changes do not undo them.

On stale/revoked/drifting evidence or insufficient capacity, close admission and
retain obligations. On unauthorized access, false acceptance, quota over-admission,
secret exposure, duplicate send or stale-fence promotion, suspend the affected route
(or shared authority) and retain incident evidence. Attempt cancellation, establish
process stop separately, reconcile read-only and preserve uncertain reservations.
Never refund estimates, delete outbox intents, resend an ambiguous operation or
restore spent budgets from a backup. A new grant cannot make an ambiguous resend safe.

Signed operational drill receipts bind campaign/route, scenario, issuer, observation
and measured recovery time. They are records for a protected verifier; signing
alone does not prove a drill happened. Production backup/restore, migration,
compromise/rotation, retention, upgrade and incident-handoff exercises remain live
operational gates. Release hashes authenticate bytes only; external publisher trust
and deployment allowlists remain separate. No automatic publication/installation,
merge, deployment or tracker completion follows readiness.
