# Contracts and algorithmic behavior

Everything here is proposed. Shape schemas reject unknown fields, but production
semantic validation, planning, evaluation and enforcement remain unimplemented.

## Object boundaries

| Contract | Owns | Does not own |
|---|---|---|
| GoalContract | Outcome, success predicates, invariants, scope, limits, stop conditions | Native session continuation or task status |
| PlanDefinition | Versioned step DAG, dependencies, methods, inventory and aggregate verification | Mutable runtime scheduling/claims |
| PlanRevision | Evidence-triggered change proposal and evidence invalidation | Silent edits to running work |
| DecisionContract | Typed question, mechanism, uncertainty and allowed recommendations | Authorization or execution |
| DecisionResult | Value/abstention, evidence, confidence semantics and observed provider | Proof that a recommended action is permitted |
| OptimizationExperiment | Baseline, mutable fields, protected fields, data splits, budgets and promotion | Unreviewed production self-modification |
| SDLCTraceability | Links from requirement/outcome to verification and runs | A competing task/board lifecycle |

Each schema has an example under `examples/`. References use stable IDs that a future
registry resolves; strings do not make referenced verifiers, assignments or adapters real.
A production resolver must pin every transitive version/content hash using the proposed
agent manifest. Current task packets reject unknown fields: do not insert these keys
into operational packets until a compatible extension is implemented.

## Goal evaluation

Predicates reference trusted verifiers; descriptions are explanatory, not executable code.
Evaluation returns pass, fail, unknown or error with evidence and evaluator version.
All required success predicates must pass, all required invariants must pass, and no
required evaluation may be unknown/error before verified-success. Completing steps or
producing files is insufficient. Optional findings remain visible. Scope and permissions
are intersected with the assigned agent and higher-level policy.

Cancellation, budgets, blockers and no defensible next step are distinct stop outcomes.
A stopped native Goal cannot become success without verification. Evidence must refer
to the current repository/resource version; a failed invariant blocks completion even
when outputs look correct. Production will need a versioned goal-evaluation result
record; this draft leaves it as verifier evidence rather than fabricating an operational format.

## Planning strategies

1. **Fixed DAG:** validate unique step IDs, known dependencies and acyclicity. A step
   becomes eligible only after dependencies and preconditions pass. Failed prerequisites
   block descendants. Select eligible steps subject to resource ownership and concurrency.
2. **Hierarchical:** select a registered method whose applicability checks pass, expand
   into a bounded DAG, then validate again. Method definitions and expansion evidence are
   future implementation contracts; a language model may propose a method, not waive checks.
3. **Iterative:** execute a bounded step, evaluate new evidence, propose a PlanRevision,
   validate changed scope/dependencies/permissions, then adopt an authorized new version.
4. **Inventory campaign:** enumerate units, assign units to steps, explicitly classify
   included/excluded/deferred units with reasons, then reconcile coverage at goal verification.

Start implementation with fixed DAGs. The strategy enum identifies planned capabilities;
it does not imply four planners already exist. Stop on unresolved dependencies rather
than treating unknown as ready. Worktree separation does not isolate external resources.

Illustrative executor algorithm:

```text
resolve goal + plan + assignments + policy
validate graph, references, limits and enforcement
while not stopped:
    evaluate required goal predicates and invariants against current evidence
    if verified success: return accepted result
    if cancellation, failed invariant or exhausted budget: stop with evidence
    choose eligible steps within ownership and concurrency constraints
    if no eligible step: propose bounded replan or return blocked
    dispatch authorized assignments through an external executor
    record actual run outcomes, evidence and resource versions
    invalidate stale dependent evidence when inputs or plan change
```

Retries follow the agent LifecyclePolicy and share the goal's total attempt/cost/time
budgets. Plan maxRevisions cannot exceed the goal's maxReplans. Revision acceptance pins
old/new versions, reasons and affected evidence. Completed evidence is retained in history;
its validity may be revoked. Never delete failure history to make a plan look complete.

## Typed decisions and uncertainty

Boolean asks a yes/no question; choice selects one declared option; score uses an explicit
numeric range/rubric; ranking orders declared alternatives. Abstention is an outcome for
every type, not a forced fifth answer type. The draft schema is broad; semantic validation
must enforce value type/range, choices, unique ranking entries and decision/result linkage.

Prefer deterministic checks for objective facts and policy. Models can interpret ambiguous
inputs or suggest routes; humans handle decisions requiring their authority. Uncalibrated
provider confidence is labeled as such. It must not be treated as a calibrated probability
or compared across providers without evidence. Missing required evidence means abstain;
provider/parsing failures mean failed. Neither outcome authorizes actions.

Decision outcomes can recommend a workflow, exploration depth or already permitted profile.
The consumer must independently validate action permissions, scope, model compatibility
and budgets. The boolean fixture policy cannot bypass human acceptance or approve merges.

## SDLC traceability

Reuse the existing workflow catalog rather than create a competing list. For each
requirement/outcome record: goal predicates → workflow type → plan step → agent assignment
→ output contract → verifier → actual run/artifact evidence. Planned coverage is not verified
coverage. Cross-stage flows include discovery, specification, implementation, verification,
release, operation and maintenance using existing catalog types and task status/phase axes.

The first example targets repository onboarding. For a migration, inventory every consumer,
map batches to workflow steps, preserve compatibility invariants, and reconcile all consumers
before completion. Excluded/deferred consumers require explicit disposition and cannot be
silently counted as migrated. Requirements with no verifier remain an actionable gap.

## Controlled optimization

An experiment starts from a pinned agent/profile baseline and a working immutable evaluator.
Allow edits only to explicitly selected instruction text or other approved components.
Protect permissions, invariants, output/verification requirements, evaluator, held-out data
and promotion rules. Reject ancestor/descendant field overlap and dynamic field injection.
A candidate changing protected content is invalid before scoring.

Training data guides proposals; validation selects candidates; held-out test data is used
for final assessment and is not supplied to the optimizer. Repeatedly selecting on test
results turns it into validation: rotate/pin new held-out data and disclose that change.
Split by repository/task family where needed to avoid near-duplicate leakage. The saved
fixture IDs are synthetic placeholders; no datasets or measured gains are claimed.

Track proposer and evaluation costs separately, plus evaluator calls, retries and elapsed
time. Hard limits require enforcement owners; unknown spend cannot be reported as zero.
Evaluate task correctness, evidence, policy compliance, regressions, latency and cost per
accepted result. A faster invalid result is not an improvement. Promotion requires review,
held-out gates, a candidate diff and a new version; keep rollback to the prior definition.

DSPy and GEPA are optional optimization/program backends, separate from client adapters.
Start with deterministic baselines and offline prompt experiments. RL remains research:
it needs a defined environment, permitted action space, reward design, trustworthy traces
and evaluation. A possible later target is selection among allowed profiles/methods;
reward must never override constraints. No online training or autonomous policy promotion
is enabled by this proposal.
