# Phased implementation plan

All phases are proposed. Coordinate with the
[agent-spec plan](../agent-spec/implementation-plan.md); saving drafts is not operational
phase completion. No new scheduler, runtime or model-provider dependency is introduced here.

| Phase | Work | Acceptance gate |
|---|---|---|
| 0 — Contract alignment | Review Goal/Plan/Decision ownership; connect agent assignment/output/evaluation and existing task axes | No competing lifecycle or implicit authority; reviewed schema/examples and baseline |
| 1 — Definitions and deterministic checks | Promote reviewed contracts; reference resolution, DAG validation, predicate/result semantics, traceability | Cycles, missing dependencies, unsupported outcomes and coverage gaps fail clearly |
| 2 — Bounded planning pilot | Fixed DAG repository onboarding; readiness gates; external executor handoff | Required goal predicates and invariants independently pass; no false success from completed steps |
| 3 — Decisions and adapter conformance | Deterministic rules first; optional System One/provider mapping with abstention/calibration labels | Native type/error mappings and action-policy separation tested; no model authorization |
| 4 — Revision and campaign behavior | Evidence-triggered plan revisions, dependency invalidation, inventory reconciliation, bounded hierarchical methods | Limits enforced; history retained; excluded/deferred units explicit; no silent scope growth |
| 5 — Offline evaluation/optimization | Frozen datasets/evaluator; optional DSPy/GEPA; protected fields and separate spend limits | Held-out tests, regression evidence, reproducible candidates and reviewed promotion |
| 6 — Evaluated routing | Choose among approved profiles/methods using versioned evaluation | Better than baseline on representative tasks; policy, quality and cost gates hold |
| 7 — RL feasibility research | Specify environment/action/reward and offline evaluation; decide whether benefits justify complexity | No training/deployment without separate design/evidence; constraints remain outside reward |

Phase 1 requires the agent resolver/registry work before execution. Phase 2 depends on
real enforcement and run evidence. Phase 3's provider work is optional and cannot block
deterministic planning. Phase 5 requires stable evaluation and usable real traces. Native
format/version tests precede support claims. Promotion and migration follow the agent
spec's version/hash and rollback contracts.

## Validation work packages

- Goal: required pass/fail/unknown/error; invariant failure; cancellation/budget limits;
  stale evidence and native objective clearing without success.
- Plan: duplicate IDs, missing/cyclic dependencies, failed prerequisites, ownership
  conflicts, exhausted replans, revision history and inventory/aggregate coverage.
- Decision: typed values, allowed choices, range/ranking validation, abstention, missing
  evidence, provider errors and confidence semantics; never grant action permissions.
- Optimization: disjoint datasets, immutable evaluator, protected ancestor/descendant
  paths, attempt and spend ceilings, held-out leakage, regression and rollback evidence.
- Traceability: every required outcome reaches verifier evidence; planned is not verified;
  no current task schema fields added without compatible implementation.
- Regression: existing kit/schema/render tests; agent proposal validation; isolated
  adapter tests when code is implemented. No live production mutation in conformance tests.

## First pilot scope

Repository exploration with three steps: inspect → report → verify. One agent profile,
concurrency one, no model routing, no optimizer, no fallback. Use real evidence from the
existing exploration/readiness skills, and preserve report-only write boundaries.
Add a deterministic depth recommendation only after the basic goal/plan works. An offline
instruction experiment is a later pilot, with synthetic examples replaced by pinned,
representative repositories and independently evaluated outcomes.
