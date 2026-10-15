# Planning and decision foundations

These commands require the Go binary. They validate declarations and supplied
local evidence. They never execute a plan, call a model, run an optimizer, or
grant authorization.

```text
aok planning validate /path/to/corpus --json
aok planning ready /path/to/corpus/plan.json --evidence /path/to/corpus/evidence.json --as-of 2026-10-16T10:30:00Z --json
aok decision validate-result /path/to/decision.json /path/to/result.json --json
```

For nested plans, `ready` accepts `--root <corpus>`. Otherwise the plan's parent
is the corpus root. The supplied `--as-of` instant makes freshness evaluation
reproducible; it is not a clock attestation. JSON reports contain stable diagnostic
codes, sources, fields, severity and explanations. Exit codes are 0 for valid
results, 1 for invalid or blocked results, and 2 for command misuse.

## Operational contracts

The operational kinds are GoalContract, PlanDefinition, PlanRevision,
DecisionContract, DecisionResult, OptimizationExperiment, SDLCTraceability and
PlanningEvidence. They use `aok/v1alpha1`, required operational metadata and exact
`{kind,id,version}` references. Historical proposal examples remain unchanged;
they are not automatically operational corpora.

A plan pins its goal and assignments. Predicate and step verifiers pin registry
capabilities of type `verification`. Workflow and method names remain descriptive.
A goal may optionally pin a plan; if present, the reference must agree. Revision
records pin before/after plans and the exact predecessor revision. The first
revision has ordinal 1 and an explicit null predecessor. Version strings on the
pinned plans are the source of truth. Step additions, removals and modifications
must match the actual before/after definitions, with no unlisted changed steps.
Preserved and invalidated evidence cannot overlap. Revision evidence references
must resolve locally; accepted records require evidence and an authorization
reference, which is checked as a declaration, not as a grant of authority.
All supplied revision records count conservatively against limits, including
proposed/rejected records; the command does not select or execute a branch.
Plan revision limits cannot exceed the goal's limit.

PlanningEvidence pins the plan's exact bytes and the sorted raw-byte hashes of
all canonical corpus resources except PlanningEvidence. The latter context hash
detects same-version changes to goals, verifiers and assignments; it is a
conservative corpus-wide definition hash, not the all-file snapshot produced by
agent resolution. `planning validate` reports the context hash and per-plan hashes
for authoring evidence before adding it to the corpus. Evidence separately records predicate
outcomes, artifact paths/hashes, completed steps, covered inventory units, used
budgets, cancellation and a validity interval. It is current only when
`evaluatedAt <= asOf < expiresAt`. Supplied evidence is a claim being checked;
its presence does not establish who produced it or authorize an action.

DAG eligibility requires current passing prerequisite/invariant evidence and
verified dependency completion. Missing, unknown, failed, expired or mismatched
evidence blocks eligibility. Completing all steps alone does not establish goal
success: required goal predicates and aggregate verification checks need their own
passing evidence. Inventory
coverage and bounded retry/replanning budgets are checked separately. Integer
counters and limits are bounded to 2,147,483,647 and compared without floating
point coercion. Replan limits constrain additional revisions; using the last
allowed revision does not invalidate that existing plan. Attempts and elapsed
time at their limit block new work. Eligible
steps are reported deterministically; the command does not dispatch them.

## Decisions

Boolean results require JSON booleans, choices must name a declared alternative,
scores must lie within the declared inclusive range, and rankings contain every
alternative exactly once. Abstained/failed results carry null values and no
confidence. Missing required evidence cannot produce a decided result.

Confidence remains unavailable, provider-reported and uncalibrated, or declared
calibrated with a calibration artifact reference. This checks the declaration
and reference; it does not independently establish statistical calibration.
Results always carry `authorizationGranted: false`. A valid failed/abstained
result means its format and semantics conform, not that a decision succeeded.
Supporting schema and evidence references must resolve locally and safely.

## Optimization boundaries

Experiments are inert. Dataset references use `ID@<content-sha256>` and train,
validation and test splits must have disjoint IDs and hashes. This checks supplied
identifiers, not hidden overlap inside external datasets. Mutable dotted paths
cannot overlap declared protected paths or the minimum protected boundaries:
permissions, verification, outputs, identity boundaries, evaluator and test data.
The evaluator is immutable and promotion is not automatic. No optimizer or
promotion action is implemented.

## Synthetic walkthrough

After building `build/aok`, run:

```sh
build/aok planning validate internal/planning/testdata/corpus --json
build/aok planning ready internal/planning/testdata/corpus/plan.json \
  --evidence internal/planning/testdata/corpus/evidence.json \
  --as-of 2026-10-16T12:00:00Z --json
```

The fully pinned fixture reports only `inspect` eligible and the goal in progress.
At `2026-10-17T00:00:00Z`, its evidence has expired and eligibility is blocked.
These timestamps and artifacts are synthetic fixtures, not claims about the host
or repository. The agent resolver still reports unavailable native/runtime
readiness evidence separately. Planning eligibility does not override that gate.
