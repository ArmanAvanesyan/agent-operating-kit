# Offline model selection and evaluation

The Go CLI provides opt-in advisory selection in `internal/selection`, with signed
outcome preparation in `internal/evaluation` and trace replay in
`internal/evaluation/replay`. It never launches an agent, reserves quota, changes
an account or billing route, replenishes a window, or updates a tracker.

The contracts use `aok-selection/v1` and `aok-evaluation/v1`. They are separate
from P1 operational resources. `SelectionDecision` has one canonical definition.
The P1–P3 input formats and research exports remain unchanged.

## Commands

```text
aok models select --task task.json --candidates candidates.json \
  --catalog catalog --catalog-policy catalog-policy.json \
  --usage usage.json --usage-policy usage-policy.json \
  --policy selection-policy.json --evidence-policy evidence-policy.json \
  --as-of 2026-10-23T12:00:00Z --out new-decision --json

aok evaluation validate --manifest signed-shard.json --evidence-policy evidence-policy.json
aok evaluation estimate --manifest signed-shard.json --evidence-policy evidence-policy.json \
  --cohort cohort.json --as-of 2026-10-23T12:00:00Z --out new-estimate
aok evaluation replay --dataset replay.json --partition locked-test --out new-replay
aok evaluation compare --dataset replay.json --partition locked-test --out new-comparison
aok evaluation replay --selection-manifest selector-replay.json --partition locked-test
```

Outputs require a new directory when `--out` is used. The existing private,
non-overwriting evidence writer rejects unsafe paths and protected-input overlap.
Read-only validation and replay may print reports without writing a directory.
Exit 0 means selected/successful validation or evaluation; exit 1 means invalid,
blocked or non-selected; exit 2 means command misuse. There is no live
`evaluation run` command. Commands require neither credentials nor host CLIs.

## Preparing inputs

`TaskProfile` requires original assignment, packet and input-revision assets with
SHA256 hashes, explicit task class, language, standard/high risk, context/output
budgets, deadline, currency/spend limit and attempt/review protocol digest. Task
requirements are structured; no classifier or LLM infers them. The assignment's
packet path and execution-profile reference must match the supplied assets and
candidate document metadata. Each alternate candidate may include an optional
original `assignment` asset whose profile reference matches that candidate. Its
agent, inputs, outputs, packet bytes and assignment permission restrictions must
match the task's baseline assignment. Signed context binds that route's exact
assignment digest. This permits explicit alternatives while preserving every
original profile and assignment byte; the selector never creates or edits them.

`CandidateManifest` lists exact configuration IDs, original profile assets,
catalog digest, pseudonymous account scope, all applicable P3 binding IDs, signed
context observations, signed outcome-shard paths, and an optional prior decision.
Paths are relative to their enclosing document. Source hashes are checked before
use. Exact identities in catalog configuration records or trusted imported probe
records can identify candidates; presence alone establishes no support.

Each account context observation must attest `coverageComplete:true` and bind
`usage.ConfigurationBindingsDigest` for the entire configuration's inventory.
This digest includes pool, provider, source, stream, constraint coverage and
completeness fields. The issuer is responsible for discovering and approving the
complete inventory; AOK cannot discover omitted private account limits offline.
The signature also binds configuration/profile, assignment, packet, input
revision, account scope, dimension, values and original observation/expiry times.
Named tools, allowed data regions and effective static permissions require their
own signed exact-scope observations. This attestation is a static finding, never
an execution authorization. Context validity is capped at five minutes.

`SelectionPolicy` explicitly approves configuration/account routes and a quality,
cost, capacity or deadline priority. Set `defaultsVersion` to
`aok-selection-defaults/v1`, standard floor at least 0.80, high-risk floor at least
0.95, an empty fallback list by default, an optional pin and hysteresis waiver.
Policies cannot waive mandatory gates. At most one explicit fallback edge per
source is allowed and only one edge can be traversed in a decision.

Optional `upperBounds` in the separately trusted policy can supply missing
consumption/cost/time bounds for an exact cohort. Each bound has a captured source
asset, original observation and expiry, and is labeled **unmeasured**. Such bounds
never replace capability or quality evidence and expire within seven days.

Use `selection.Schema("task"|"candidates"|"policy"|"decision")` for the closed
selection schemas. Evaluation schemas are embedded under
`internal/evaluation/schemas`. Typed input definitions and the synthetic signed
end-to-end fixture in `internal/selection/integration_test.go` show complete
construction. Fixture keys and successful outcomes are test data, not production
issuers or qualified model evidence.

## Evidence and estimates

An externally supplied evidence policy approves canonical Base64 Ed25519 public
keys with shard, independent verifier and optional context roles. Shard and
verifier keys must differ even under aliases. Sign a canonical JSON object with
its signature member set to the empty string; object keys are sorted, original
JSON numbers preserved. The manifest pins the exact bounded outcome asset bytes.
Independent verifier receipts bind experimental identity, revisions and acceptance.
Signatures establish approved issuers, not runtime readiness or objective truth.

Only independently approved locked-test family outcomes supply selection quality.
Resource bounds are fitted exclusively on separate validation families, whose
latest observation must precede the earliest audit observation. At least 30
validation families are required; audit-only data leaves demand unknown. The
artifact records calibration partition, family count, observation and provenance.
Development and validation data are for fitting and policy choices; audit evidence
must remain independent. Cross-partition repository, family and near-duplicate
leakage is rejected. Cohorts match exact configuration/profile, task class,
language/risk, account and attempt/review protocol. Failed and interrupted attempts
are included. Every preregistered repetition must be independently accepted for a
family to succeed. Repetitions never add independent samples.

At least 30 families are required. Quality is a one-sided 95% Wilson lower bound,
rounded downward to 12 decimal places. High-risk 0.95 first clears at 52 all-success
families. Resource estimates use the conservative bundled sum of registered
attempts per family and the split-conformal rank `ceil((n+1)*0.95)` with at least
30 families. Censored measurements yield unknown bounds, not zero. Resource
arithmetic preserves exact decimal measurements. Cost/time ranking indices divide
upper estimates by the quality lower bound and round upward to 18 decimal places;
they are ranking indices, not accepted-result guarantees.

Percentage demand requires attributable `percentage` / `percentage-points`
measurements in the same pool and `usage.WindowFamily(constraint)`. This structural,
generation-neutral family includes the particular limit and scope; `fixed` or
`rolling` alone is insufficient. Tokens never become subscription percentage.
Quality expires within 30 days and demand within seven days, anchored to the
original observations. Refreshing or estimating never renews observation dates.

## Decisions and capacity

Every candidate is checked independently before ranking: exact identities,
allowed route and scoped account/data/permission context, catalog capabilities,
quality, cost/deadline, and fresh P3 `observed-fit` for every constraint. Missing or
conflicting evidence cannot pass. Guidance, listings and smoke invocation do not
supply task quality. An output-setting probe proves only its explicit accepted
profile setting, not a guarantee about generated output length.

`usage.CurrentConstraints` delegates to the existing capacity projector. Demand
is materialized against the currently observed generation and then submitted to
`usage.Capacity`. Source ordering, holds, pending charges and freshness stay in
P3. No mutable reservation store is used by selection.

Eligible candidates are Pareto filtered and ranked deterministically; final ties
use configuration ID. Scarcity uses the worst demand/headroom ratio weighted by
time to an observed reset over 24 hours. Missing reset gets full weight. Scarcity
never predicts replenishment. An eligible incumbent is retained for five minutes;
subsequent switches need 0.02 quality improvement or 10% on the first differing
minimizing metric. Zero cannot improve. Retention requires matching task/policy
and profile hashes. A hard-gate failure defeats retention, and mid-run selection
requires an explicit safe checkpoint.

Dispositions are `selected`, `refresh-needed`, `wait-until`, `defer`, `abstain` and
`no-qualified-candidate`. Waiting uses the latest deficient-window reset per route,
then the earliest permissible route hint within the deadline. A reset is only a
hint: new observations and a complete rerun remain required. Every decision
captures input/evidence hashes, candidate gates, remaining/headroom/reset values,
estimates, rank, explanations, next evidence and validity, with
`authorizationGranted:false` and `executionReady:false`.

## Offline evaluation and promotion

The pilot remains 48 packets over six classes, grouped 24 development / 12
validation / 12 locked-test, two or three configurations and three repetitions:
288–432 attempts. Repository/family/near-duplicate variants cannot cross splits.
This tests the pipeline and abstention; it cannot qualify broad per-class quality.

Recorded-state replay compares supplied outcomes under the same quota states;
it makes no causal capacity-saving claim. Synthetic closed-loop replay interleaves
attempts with explicit consumption, reservation, external activity and replenishment
events. Clock passage alone adds nothing. Comparisons cover pinned, quality-only,
cost-aware, quota-aware and reservation-aware policies with common hard gates.
Unsafe capacity ablations are counterfactual and excluded from safety denominators.
Reports retain unknown measurements and distinguish decision opportunities from
admitted starts. Replay labels are supplied observations/simulation labels, not
new independent verifier evidence.

Freeze policy, configuration and estimate assets before opening the audit split;
do not tune using audit outcomes or reuse winner-selection evidence as an
independent superiority claim. Live benchmark execution, controlled pilots,
100 observed shadow decisions and P6/P7 transport/enforcement gates remain separate.

`--selection-manifest` reruns the actual selector for five frozen, hash-verified
policies per case and checks identical task, candidate, catalog, usage and evidence
inputs. Named policy semantics are validated; common mandatory constraints must
remain fixed. Quality-only and cost-aware preferences still retain every hard gate,
including P3 holds. Quota-aware and reservation-aware both use the P3 projector;
this mode does not silently remove holds to make a baseline unsafe. Results report
conditional dispositions, with no fabricated completion labels. Manifest partition
labels are caller-supplied (`partitionMetadataVerified:false`); their consistency
is checked but semantic near-duplicate detection needs an approved external dataset
curation process. Freeze times are supplied attestations, not trusted timestamps.

`--dataset` loads frozen policy/configuration/estimate assets and checks their
byte hashes. Its decision projections and outcome labels remain explicitly
unverified supplied inputs even when those artifact hashes are valid. Synthetic
virtual time orders explicit events; it cannot establish wall-clock audit chronology.
No supplied trace or successful fixture qualifies a production model.
