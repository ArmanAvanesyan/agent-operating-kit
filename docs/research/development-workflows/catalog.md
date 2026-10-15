# Proposed development workflow catalog

Status: proposed. This catalog is a repository synthesis, not a vendor taxonomy or an installed AOK feature.

The structured companion is [catalog.json](catalog.json). Each group contains four workflow types. Each type has a concrete example task, three ordered activities, completion evidence, recommended approaches, and suggested existing work-item types. There are 12 groups, 48 types, and 144 activity entries. Sources indicate design inputs, not vendor endorsement of this exact catalog.

## Execution approaches

| ID | Choose when | Sequence | Constraint |
|---|---|---|---|
| `direct-execution` | Small clear low-impact change | Inspect → edit → relevant check → report | Avoid extra planning artifacts unless uncertainty emerges. |
| `explore-plan-implement` | Uncertain solution or impact | Explore → compare → plan → implement → verify | Resolve consequential unknowns before expensive implementation. |
| `execplan-delivery` | Complex milestones dependencies or handoffs | Plan → deliver increment → record evidence → update plan | Keep the plan resumable and acceptance observable. |
| `goal-driven-iteration` | Measurable finish line with adaptive path | Act → evaluate evidence → select next action → repeat | Honor native lifecycle permissions and configured stop limits. |
| `reproduce-repair-verify` | Defect correction | Failing behavior → cause → repair → regression check | Do not weaken the judge to obtain a green result. |
| `characterize-transform-compare` | Behavior-sensitive existing code | Baseline → transform → compare → reconcile exceptions | Baseline behavior can include defects; approve intentional semantic changes separately. |
| `scripted-pipeline` | Predictable stages and transitions | Execute explicit steps with checked intermediate outputs | Script success must depend on real check outcomes. |
| `parallel-workstreams` | Independent ownership and integration boundaries | Partition → execute → aggregate → integration check | Separate contexts do not isolate files ports databases or external systems. |
| `generator-evaluator` | Independent assessment materially helps | Generate → evaluate → remediate within limits | Calibrate the evaluator and test whether it earns its overhead. |
| `pilot-and-expand` | Repeated change across a large surface | Representative pilot → refine → bounded batches → reconcile | Pilot exceptional cases as well as easy ones. |
| `evidence-led-audit` | Many candidate findings or uncertain claims | Inventory → evidence → confidence → prioritize | Insufficient evidence is an outcome; do not invent certainty. |
| `scheduled-maintenance` | Recurring or event-triggered bounded work | Trigger → inspect → report or propose change | Scheduling is distinct from continuation toward an active Goal. |

## Workflow groups and types

### Discovery and understanding

Group ID: `discovery`. Design inputs: [O2](research.md#o2), [A2](research.md#a2), [A6](research.md#a6).

#### Repository onboarding

ID: `repository-onboarding`. Suggested work-item types: `spike`, `task`.

Example task: Map the services and startup path of an unfamiliar repository.

1. Inspect entrypoints and ownership.
2. Trace major dependencies.
3. Record navigation and open questions.

Completion evidence: Code-linked architecture map.

Recommended approaches: `evidence-led-audit`.

#### Execution path tracing

ID: `execution-path-tracing`. Suggested work-item types: `spike`, `task`.

Example task: Trace a request from HTTP boundary to persistence.

1. Locate entrypoint.
2. Follow calls and data transformations.
3. Check error and retry branches.

Completion evidence: File/function references and an observed trace where available.

Recommended approaches: `evidence-led-audit`.

#### Dependency and consumer mapping

ID: `consumer-mapping`. Suggested work-item types: `spike`, `change`.

Example task: Identify consumers of a response field before changing it.

1. Inventory direct references.
2. Investigate indirect and external consumers.
3. Record ownership and compatibility constraints.

Completion evidence: Consumer inventory with confidence and provenance.

Recommended approaches: `evidence-led-audit`.

#### Technical research

ID: `technical-research`. Suggested work-item types: `spike`.

Example task: Compare two approaches to a background job system.

1. Define questions and evidence standard.
2. Review primary sources and local constraints.
3. Compare options and unresolved assumptions.

Completion evidence: Source-backed findings and decision inputs.

Recommended approaches: `evidence-led-audit`.

### Requirements and solution design

Group ID: `design`. Design inputs: [O1](research.md#o1), [O6](research.md#o6), [A1](research.md#a1).

#### Requirements clarification

ID: `requirements-clarification`. Suggested work-item types: `feature`, `spike`.

Example task: Turn a feature request into bounded acceptance criteria.

1. Identify intended user outcome.
2. Resolve consequential ambiguities.
3. Record scope and constraints.

Completion evidence: Testable requirements with explicit assumptions.

Recommended approaches: `explore-plan-implement`.

#### Acceptance scenario design

ID: `acceptance-design`. Suggested work-item types: `feature`, `task`.

Example task: Define observable success for invoice export.

1. Enumerate user and failure scenarios.
2. Select verification surfaces.
3. Define expected observations.

Completion evidence: Acceptance scenarios including relevant edge cases.

Recommended approaches: `explore-plan-implement`.

#### Architecture decision

ID: `architecture-decision`. Suggested work-item types: `spike`, `change`.

Example task: Choose synchronous or queued report generation.

1. Inspect system constraints.
2. Compare alternatives and recovery implications.
3. Record decision and consequences.

Completion evidence: ADR with evidence and rejected alternatives.

Recommended approaches: `explore-plan-implement`.

#### Feasibility spike

ID: `feasibility-spike`. Suggested work-item types: `spike`.

Example task: Test whether a library supports the required streaming behavior.

1. Define bounded hypothesis.
2. Build disposable experiment.
3. Measure and report limitations.

Completion evidence: Reproducible experiment and go/no-go inputs.

Recommended approaches: `explore-plan-implement`.

### Repository and harness readiness

Group ID: `readiness`. Design inputs: [O2](research.md#o2), [O6](research.md#o6), [A3](research.md#a3).

#### Environment bootstrap

ID: `environment-bootstrap`. Suggested work-item types: `task`.

Example task: Make a fresh checkout runnable.

1. Document dependencies and configuration inputs.
2. Provide repeatable setup.
3. Run fresh-environment smoke checks.

Completion evidence: Successful setup and startup evidence.

Recommended approaches: `scripted-pipeline`.

#### Agent instruction setup

ID: `agent-instructions`. Suggested work-item types: `task`.

Example task: Create concise repository navigation for agents.

1. Inspect existing instructions.
2. Link canonical commands and boundaries.
3. Check scope and conflicting guidance.

Completion evidence: Navigable instructions with valid links.

Recommended approaches: `direct-execution`.

#### Validation command setup

ID: `validation-setup`. Suggested work-item types: `task`.

Example task: Provide one reliable command for relevant checks.

1. Inventory checks.
2. Define deterministic command outputs.
3. Test success and deliberate failure.

Completion evidence: Runnable checks that expose failures correctly.

Recommended approaches: `scripted-pipeline`.

#### Observability enablement

ID: `observability-enablement`. Suggested work-item types: `task`, `change`.

Example task: Make a local workflow diagnosable by an agent.

1. Identify missing feedback.
2. Expose useful logs or test diagnostics.
3. Exercise a representative failure.

Completion evidence: Failure is observable without sensitive data exposure.

Recommended approaches: `explore-plan-implement`.

### Feature delivery

Group ID: `delivery`. Design inputs: [O1](research.md#o1), [A1](research.md#a1), [A2](research.md#a2).

#### Small enhancement

ID: `small-enhancement`. Suggested work-item types: `task`, `feature`.

Example task: Add a bounded CLI option.

1. Inspect existing behavior.
2. Implement minimal change.
3. Run targeted checks.

Completion evidence: Expected option behavior and unchanged existing paths.

Recommended approaches: `direct-execution`.

#### Vertical product slice

ID: `vertical-slice`. Suggested work-item types: `feature`.

Example task: Deliver an end-to-end invoice preview.

1. Define user scenario.
2. Implement across required layers.
3. Exercise the full path.

Completion evidence: Working scenario rather than isolated component stubs.

Recommended approaches: `explore-plan-implement`.

#### API or integration feature

ID: `integration-feature`. Suggested work-item types: `feature`.

Example task: Add a webhook endpoint.

1. Specify external contract.
2. Implement boundary and failure behavior.
3. Run contract and integration tests.

Completion evidence: Contract, error and retry behavior verified.

Recommended approaches: `explore-plan-implement`.

#### Complex multi-phase feature

ID: `complex-feature`. Suggested work-item types: `feature`, `epic`.

Example task: Implement a multi-component export system.

1. Create milestones and dependencies.
2. Deliver verifiable increments.
3. Reconcile final acceptance and recovery.

Completion evidence: Milestone evidence and complete acceptance inventory.

Recommended approaches: `execplan-delivery`, `goal-driven-iteration`.

### Debugging and defect repair

Group ID: `debugging`. Design inputs: [A1](research.md#a1), [A2](research.md#a2), [O7](research.md#o7).

#### Deterministic bug repair

ID: `deterministic-bug`. Suggested work-item types: `bug`.

Example task: Fix a reproducible invalid-state crash.

1. Capture failing input.
2. Find and repair cause.
3. Verify regression and adjacent behavior.

Completion evidence: Reproducer fails before and passes after.

Recommended approaches: `reproduce-repair-verify`.

#### Intermittent failure investigation

ID: `intermittent-failure`. Suggested work-item types: `bug`, `spike`.

Example task: Diagnose a nondeterministic checkout failure.

1. Collect occurrences and environment details.
2. Test hypotheses with controlled repetitions.
3. Fix cause or document evidence-backed blocker.

Completion evidence: Failure-rate evidence with execution conditions.

Recommended approaches: `goal-driven-iteration`, `reproduce-repair-verify`.

#### Regression isolation

ID: `regression-isolation`. Suggested work-item types: `bug`.

Example task: Find the change that broke response compatibility.

1. Establish good and bad states.
2. Bisect or compare relevant changes.
3. Verify causal fix.

Completion evidence: Causal revision evidence and behavior test.

Recommended approaches: `reproduce-repair-verify`.

#### Concurrency investigation

ID: `concurrency-investigation`. Suggested work-item types: `bug`, `spike`.

Example task: Repair duplicate processing under simultaneous requests.

1. Construct concurrent reproducer.
2. Inspect ordering and resource ownership.
3. Test the synchronization or idempotency fix.

Completion evidence: Repeatable concurrent scenario and invariant checks.

Recommended approaches: `reproduce-repair-verify`, `goal-driven-iteration`.

### Refactoring and modernization

Group ID: `modernization`. Design inputs: [O1](research.md#o1), [A2](research.md#a2), [A9](research.md#a9).

#### Module extraction

ID: `module-extraction`. Suggested work-item types: `task`, `change`.

Example task: Split a large parser without changing its API.

1. Characterize current interface.
2. Extract focused modules.
3. Compare behavior and imports.

Completion evidence: Behavioral tests and stable public interface.

Recommended approaches: `characterize-transform-compare`.

#### Architectural boundary repair

ID: `boundary-repair`. Suggested work-item types: `change`.

Example task: Remove a forbidden dependency across layers.

1. Identify dependency violation.
2. Move responsibility behind an interface.
3. Run architecture and integration checks.

Completion evidence: Dependency rule and behavior both hold.

Recommended approaches: `execplan-delivery`, `characterize-transform-compare`.

#### Dependency or framework upgrade

ID: `framework-upgrade`. Suggested work-item types: `change`.

Example task: Upgrade a framework across a service.

1. Review changed APIs and defaults.
2. Update code and configuration.
3. Verify build and runtime behavior.

Completion evidence: Compatibility inventory and passing affected checks.

Recommended approaches: `pilot-and-expand`, `characterize-transform-compare`.

#### Obsolete pattern replacement

ID: `obsolete-pattern`. Suggested work-item types: `task`, `change`.

Example task: Replace a deprecated internal helper.

1. Inventory usage.
2. Define semantic replacement.
3. Transform and verify callers.

Completion evidence: No unsupported callers and equivalent behavior.

Recommended approaches: `characterize-transform-compare`.

### Migration campaigns

Group ID: `migration`. Design inputs: [O5](research.md#o5), [A9](research.md#a9).

#### API or SDK migration

ID: `api-migration`. Suggested work-item types: `change`, `epic`.

Example task: Move every inventoried client call to a new API.

1. Inventory calls and contract differences.
2. Pilot semantic mappings.
3. Migrate units and reconcile coverage.

Completion evidence: Contract parity and complete call-site inventory.

Recommended approaches: `pilot-and-expand`, `characterize-transform-compare`, `goal-driven-iteration`.

#### Language or framework port

ID: `language-port`. Suggested work-item types: `change`, `epic`.

Example task: Port a service while preserving supported behavior.

1. Create shared behavioral judge.
2. Stress-test translation rules.
3. Translate units then compile and compare.

Completion evidence: Old/new parity, integration and nonfunctional checks.

Recommended approaches: `pilot-and-expand`, `parallel-workstreams`.

#### Schema or data migration

ID: `data-migration`. Suggested work-item types: `change`.

Example task: Backfill a new schema while versions coexist.

1. Define integrity and mixed-version requirements.
2. Implement checkpointed migration.
3. Reconcile and rehearse recovery.

Completion evidence: Data reconciliation and tested restore or roll-forward.

Recommended approaches: `execplan-delivery`, `scripted-pipeline`.

#### Cross-service contract transition

ID: `contract-transition`. Suggested work-item types: `change`, `epic`.

Example task: Retire an old event only after consumers transition.

1. Map consumers and deployment ordering.
2. Introduce compatibility bridge.
3. Verify adoption before retirement.

Completion evidence: Consumer coverage and compatibility evidence.

Recommended approaches: `execplan-delivery`, `pilot-and-expand`.

### Maintenance and simplification

Group ID: `maintenance`. Design inputs: [O4](research.md#o4), [O2](research.md#o2).

#### Dead-code removal

ID: `dead-code`. Suggested work-item types: `task`, `change`.

Example task: Remove an unused subsystem with evidence.

1. Collect direct indirect and runtime usage evidence.
2. Check operational and recovery purpose.
3. Propose small removals and verify.

Completion evidence: Removal justification and supported-consumer checks.

Recommended approaches: `evidence-led-audit`, `pilot-and-expand`.

#### Stale experiment cleanup

ID: `stale-experiment`. Suggested work-item types: `task`, `change`.

Example task: Retire a fully shipped feature experiment.

1. Read live or authorized configuration evidence.
2. Check dependencies and intentional controls.
3. Remove eligible code with owner dispositions.

Completion evidence: Production-value provenance and preserved operational safeguards.

Recommended approaches: `evidence-led-audit`.

#### Dependency and configuration cleanup

ID: `dependency-cleanup`. Suggested work-item types: `task`.

Example task: Remove an obsolete package or configuration key.

1. Inventory consumers and generated uses.
2. Check supported deployment environments.
3. Remove and verify packaging and startup.

Completion evidence: No required consumer lost; build and startup evidence.

Recommended approaches: `evidence-led-audit`, `characterize-transform-compare`.

#### Documentation gardening

ID: `documentation-gardening`. Suggested work-item types: `task`.

Example task: Reconcile setup docs with current behavior.

1. Identify stale claims.
2. Verify authoritative implementation.
3. Correct links examples and claims.

Completion evidence: Source-linked corrections and usable examples.

Recommended approaches: `evidence-led-audit`, `direct-execution`.

### Testing and quality improvement

Group ID: `quality`. Design inputs: [A1](research.md#a1), [A4](research.md#a4), [O4](research.md#o4).

#### Missing behavior coverage

ID: `behavior-coverage`. Suggested work-item types: `task`, `bug`.

Example task: Add a regression scenario for invalid signatures.

1. Identify consequential uncovered behavior.
2. Write independent expected outcomes.
3. Verify the test detects the defect.

Completion evidence: Meaningful behavioral failure detection.

Recommended approaches: `reproduce-repair-verify`.

#### Flaky-test repair

ID: `flaky-test`. Suggested work-item types: `bug`, `task`.

Example task: Fix a timing-dependent CI test.

1. Establish failure history and rate.
2. Replace nondeterministic dependency or fix product cause.
3. Repeat relevant tests under comparable conditions.

Completion evidence: Stable repeated outcomes without weaker assertions.

Recommended approaches: `goal-driven-iteration`, `reproduce-repair-verify`.

#### Browser and accessibility verification

ID: `browser-quality`. Suggested work-item types: `task`, `bug`.

Example task: Verify keyboard completion of a form.

1. Define user interactions and expected states.
2. Exercise live UI and accessibility semantics.
3. Capture failures and retest corrections.

Completion evidence: Scenario results and relevant screenshots or traces.

Recommended approaches: `generator-evaluator`.

#### Test-suite improvement

ID: `suite-improvement`. Suggested work-item types: `task`.

Example task: Reduce test time while preserving useful coverage.

1. Measure baseline and bottlenecks.
2. Optimize expensive setup or redundant work.
3. Compare failures detected and run time.

Completion evidence: Comparable coverage and measured improvement.

Recommended approaches: `goal-driven-iteration`.

### Review and assurance

Group ID: `assurance`. Design inputs: [O3](research.md#o3), [A4](research.md#a4).

#### Change review

ID: `change-review`. Suggested work-item types: `task`.

Example task: Review a PR for consequential defects.

1. Inspect diff and affected context.
2. Validate suspected defects.
3. Record prioritized actionable findings.

Completion evidence: Findings with location rationale and dispositions.

Recommended approaches: `evidence-led-audit`.

#### Compatibility review

ID: `compatibility-review`. Suggested work-item types: `task`, `risk`.

Example task: Review a public wire-name change.

1. Locate consumers and contracts.
2. Evaluate safe transitions and exceptions.
3. Verify supported consumer behavior.

Completion evidence: Compatibility evidence and explicit exception decisions.

Recommended approaches: `evidence-led-audit`.

#### Security and privacy review

ID: `security-privacy-review`. Suggested work-item types: `task`, `risk`.

Example task: Review a new data boundary.

1. Identify relevant assets and trust boundaries.
2. Trace inputs authorization and outputs.
3. Verify mitigations against concrete scenarios.

Completion evidence: Validated findings and residual-risk decisions.

Recommended approaches: `evidence-led-audit`, `generator-evaluator`.

#### Repository-rule evaluation

ID: `review-rule-evaluation`. Suggested work-item types: `task`.

Example task: Check whether a rule catches breaks without noise.

1. Build violation safe-exception and unrelated cases.
2. Run blinded representative reviews.
3. Measure detection restraint and actionability.

Completion evidence: Evaluation outcomes including false positives.

Recommended approaches: `generator-evaluator`, `scripted-pipeline`.

### Release and operations

Group ID: `operations`. Design inputs: [A2](research.md#a2), [O6](research.md#o6).

#### PR shipping and CI repair

ID: `pr-shipping`. Suggested work-item types: `task`.

Example task: Get a focused PR through checks and review.

1. Inspect CI and review feedback.
2. Repair issues and rerun affected checks.
3. Deliver only within authorized policy.

Completion evidence: Required checks and resolved review dispositions.

Recommended approaches: `scripted-pipeline`, `goal-driven-iteration`.

#### Release preparation

ID: `release-preparation`. Suggested work-item types: `change`, `task`.

Example task: Prepare versioned installers and release notes.

1. Verify release contents and version consistency.
2. Build and smoke-test artifacts.
3. Record publication readiness.

Completion evidence: Artifact checks and release checklist evidence.

Recommended approaches: `scripted-pipeline`.

#### Staged rollout and recovery

ID: `staged-rollout`. Suggested work-item types: `change`.

Example task: Roll out a compatible service change gradually.

1. Define indicators and recovery triggers.
2. Deploy approved stages.
3. Observe and advance or recover.

Completion evidence: Stage observations and verified recovery path.

Recommended approaches: `execplan-delivery`, `scripted-pipeline`.

#### Incident response

ID: `incident-response`. Suggested work-item types: `incident`.

Example task: Restore service after a deployment regression.

1. Triage scope and preserve evidence.
2. Apply authorized containment or recovery.
3. Verify restoration and assign follow-ups.

Completion evidence: Restoration evidence and incident timeline.

Recommended approaches: `goal-driven-iteration`.

### Workflow and harness improvement

Group ID: `improvement`. Design inputs: [O6](research.md#o6), [A4](research.md#a4), [A5](research.md#a5).

#### Execution retrospective

ID: `retrospective`. Suggested work-item types: `lean-improvement`, `task`.

Example task: Explain why a workflow required repeated correction.

1. Reconstruct intent and observed events.
2. Classify material friction and causes.
3. Assign bounded improvements to owners.

Completion evidence: Evidence-backed findings and prioritized backlog.

Recommended approaches: `evidence-led-audit`.

#### Skill and prompt improvement

ID: `procedure-improvement`. Suggested work-item types: `lean-improvement`, `task`.

Example task: Improve an existing migration procedure.

1. Identify recurring failure.
2. Change the owning instruction reference or validator.
3. Forward-test representative positive and negative cases.

Completion evidence: Improved outcomes on held-out realistic tasks.

Recommended approaches: `generator-evaluator`, `pilot-and-expand`.

#### Model and effort evaluation

ID: `model-effort-evaluation`. Suggested work-item types: `lean-experiment`, `spike`.

Example task: Choose a cost-effective configuration for a workflow.

1. Define representative task set and outcomes.
2. Compare supported configurations.
3. Analyze accepted results cost and failure patterns.

Completion evidence: Comparable evaluation records and selection rationale.

Recommended approaches: `scripted-pipeline`.

#### Orchestration simplification

ID: `orchestration-simplification`. Suggested work-item types: `lean-experiment`, `task`.

Example task: Remove an unnecessary evaluator stage.

1. Identify the assumption behind each component.
2. Ablate one component at a time.
3. Compare quality recovery latency and cost.

Completion evidence: No material regression on representative tasks.

Recommended approaches: `pilot-and-expand`, `generator-evaluator`.

## Composition rules

- Select one primary workflow type for a bounded assignment; link additional types when they own a distinct outcome.
- Combine approaches only when their responsibilities differ: a migration may use an ExecPlan, a Goal and a pilot, without three competing task inventories.
- Apply `scheduled-maintenance` as a trigger strategy to any appropriate recurring workflow; it is not a separate work-item type.
- A group or approach does not add phases, statuses, permissions, mandatory agents or methodology profiles.
- Use XP/Lean-specific work-item types only when their active overlays are selected; neutral Task or Spike remains available otherwise.
- Shared outputs need one owner; dependency ordering and integration verification remain required after parallel work.
- These activities are reusable task templates, not live issues or assignments. Runtime execution and remote writes require separate implementation and authorization.
