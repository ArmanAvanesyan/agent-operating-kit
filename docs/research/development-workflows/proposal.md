# Proposed composable development workflows for AOK

Status: **proposed design**, based on the [research](research.md) and inspected AOK baseline
in [README.md](README.md). Names, fields, states and paths introduced below are design
candidates, not commands or schemas accepted by the current CLI.

A development workflow composes a task recipe, planning approach, execution strategy,
verification contract and delivery policy. Keep these independent so a project can use
the same migration recipe with different methodologies, agent tools and execution hosts.

## Independent axes

| Axis | Question | Examples |
|---|---|---|
| Methodology | How does the project organize delivery? | Kanban; XP and Lean overlays |
| Work-item type | What kind of work is requested? | Feature, Bug, Task, Spike, Incident, Risk, Change |
| Workflow group | Which engineering responsibility does it serve? | Discovery, migration, maintenance |
| Workflow type | Which repeatable procedure applies? | API migration, flaky-test repair, compatibility review |
| Planning approach | How much explicit planning is useful? | Direct execution, short plan, ExecPlan |
| Execution strategy | Who chooses the next action? | Single agent, script, orchestrator |
| Continuation policy | What starts another iteration? | Unmet objective, failed check, queued unit |
| Trigger | What starts a run? | Human request, approved event integration, schedule |
| Environment | Where do edits and commands run? | Checkout, worktree, container, remote sandbox |
| Verification | What establishes correctness? | Tests, parity, browser scenarios, benchmarks |
| Delivery | What can be published? | Report, patch, draft PR, merge, deployment |

Example: a Change under Kanban can select `api-migration`, an ExecPlan, a measurable Goal,
isolated worktrees, contract tests and one draft PR per component. None of these choices
creates a new board status or activates a scaffolded methodology.

The [catalog](catalog.md) defines 12 groups, 48 workflow types, 144 ordered activity entries
and 12 reusable approaches. The activities are templates for bounded assignments, not
pre-created issues. Groups are organizational labels; a real task selects the smallest
recipe that owns its outcome.

## Project exploration and environment readiness

Before implementation, use the proposed [exploration and readiness topology](project-exploration-and-readiness.md)
to establish project context and task-specific execution prerequisites. This refinement
spans the catalog's `discovery` and `readiness` groups; it does not replace the 48 type IDs
or add new records to `catalog.json`.

The proposed entry types are explore project, explore repository, investigate subsystem,
assess environment and prepare environment. Research, specification, breakdown, execution
and verification remain reusable activities across workflows.

| Element | Proposed definition | Existing capability to reuse |
|---|---|---|
| Exploration tasks | Inspect workspace, identify project, inventory technology, map architecture, trace execution, map commands, assess verification, assess agent setup, assess environment, produce handoff | File/Git inspection and existing repository instructions; no dedicated exploration skill claimed |
| Readiness tasks | Select profile, check tools, prepare workspace, install necessary dependencies, configure/start services, run relevant checks, record result | AOK bootstrap and diagnostics provide only part of this procedure |
| Skills | New `project-exploration` and `environment-readiness` content with references | Existing `project-bootstrap`, `agent-workflow`, `qa-verification` and `code-review` |
| Tools | Start with file access, search and Git; add runtimes, package managers, services, browser and remote integrations only when needed | Availability must be inspected per project/target; no universal installer or connector requirement |
| Environment profiles | Inspection, local development, service integration, application verification, delivery | Existing Codex local-environment helpers; these profiles are not installed AOK selectors |
| Outputs | Project map, development guide, readiness report, task handoff | Reuse canonical docs and current packet `spec`/verification fields rather than unsupported new fields |
| Exploration acceptance | Source-backed boundaries, architecture, commands, prerequisites and usable next-task handoff | Existing evidence-oriented task/QA guidance |
| Readiness acceptance | Required profile capabilities and checks actually demonstrated at the relevant revision | Explicit verification, rather than helper exit status alone |

Exploration may complete while environment readiness is blocked. Reports distinguish
observed, documented-but-untested, inferred and unknown claims. Required failed, blocked
or unchecked capabilities cannot receive a ready verdict. Preserve user changes, avoid
secret values and keep delivery authority separate from technical readiness.

The existing setup helper attempts AOK doctor/project validation and tolerates failures;
it does not install application dependencies. Therefore AOK initialization and a zero
helper exit code are not proof that the target application can build or run. Detailed
task IDs, tool/profile requirements, artifact ownership and acceptance checklists are in
the linked topology document. Its existing-capability inspection was refreshed at
`c2cbc3e4f29bb08b1c2064a411f6e8cc24a015c5` on 2026-10-15.

## Architecture and responsibilities

| Layer | Owns | Boundary |
|---|---|---|
| AOK | Neutral definitions, policy, templates, validation, target projections | Does not become a daemon, model loop or infrastructure provider |
| Coding-agent harness | Reasoning, context, tool use and native continuation | Native lifecycle belongs to the selected tool |
| Coordination runtime | Durable assignments, claims, execution state, events and recovery | Requires a tested external bridge; not established by documented MCP payloads |
| Executor | Commands, edits and artifact production | Uses the selected workspace and permissions |
| Sandbox | Enforced execution and access isolation | Worktree separation alone is not a security boundary |
| Infrastructure | Compute, storage, networking and environments | Provides hosting, not workflow acceptance |

The existing [architecture](../../architecture.md) and [swarmd mapping](../../swarmd-integration.md)
provide the starting boundary. No swarmd binary or MCP integration was exercised in this
research. Its current compatibility version is untracked in AOK documentation; an example
payload is not a capability test.

## Proposed definition model

### Workflow definition

A versioned workflow definition would describe:

- Identity, version, owner, support status and source provenance.
- Group/type classification and compatible work-item suggestions.
- Inputs, outputs and prerequisites.
- Steps with stable IDs, dependencies and artifact responsibilities.
- Recommended approaches and permitted execution strategies.
- Required evidence, gate conditions and justified exceptions.
- Retry/escalation policy and stopping conditions.
- Target capabilities and explicit unsupported projections.
- References to skills, roles, hooks and templates owned elsewhere.

A step should reference an existing skill when that skill owns the procedure. Avoid
copying the procedure into a second executable definition and letting the two drift.
A workflow definition is reusable desired intent; a run records a specific execution.

### Workflow run

A run record would reference definition version, assignment, source revision, environment,
checkpoint, attempts, evidence, delivery references and final disposition. The runtime
owns mutable run state. Repository artifacts can provide durable handoff context, but
must not compete with a queue's authoritative claims and cancellation state.

### Campaign definition

A campaign would add a complete unit inventory, dependency graph, ownership boundaries,
coverage reconciliation, concurrency limits and aggregate acceptance. It groups related
workflows without making every unit share one context or one large patch.

Campaign progress distinguishes:

1. Inventoried units.
2. Units transformed or investigated.
3. Units locally verified.
4. Units integrated and accepted.
5. Excluded, blocked or deferred units with dispositions.

Do not use output-file existence, lines changed or worker completion as a proxy for
campaign acceptance. Distinguish a campaign specification from its mutable runtime manifest.

### Existing packet compatibility

The current [task schema](../../../schemas/task-packet.schema.json) rejects unknown fields.
Until an additive schema change is implemented, retain workflow intent in the existing
`spec` and verification fields or in linked design documents. Do not place proposed
`workflowRef`, `goalRef`, `campaignRef`, `runId` or similar keys into current packets.
These names are illustrative; field naming and ownership must be decided in phase 2.

## Composition and selection rules

1. Select one primary workflow type for each bounded assignment. Link secondary workflows
   when they own a separate outcome, such as a migration followed by release verification.
2. Choose direct execution for clear small changes. Add a short plan for uncertainty and
   an ExecPlan for material milestones, dependencies or handoffs.
3. Add a Goal only when completion is observable and the next useful action may change.
4. Use a script when transitions are known. Use adaptive orchestration when discovery
   genuinely controls the next step.
5. Parallelize only separable work. Declare file/resource ownership, prerequisites and
   integration checks. One component changing a shared interface may block other units.
6. Add a separate evaluator when representative trials show useful defect detection or
   assessment quality. It is not automatically necessary for every task.
7. Treat scheduling as a trigger, not a Goal lifecycle or another work-item type.
8. Preserve methodology selection and neutral task statuses/phases. Do not infer native
   target support from a pack's general active status.
9. Apply the project's existing authorization. A workflow gate does not create permission
   for a remote write, but it also does not require reapproval of already-authorized work.
10. Prefer the smallest sufficient artifact set. References are better than duplicated
    objectives, checklists or trackers.

## Goal contracts and native behavior

A portable Goal is an intent contract, distinct from native thread/session state.

| Field | Purpose |
|---|---|
| Outcome | Desired end state |
| Verification surface | Command, scenario, artifact or measurement proving success |
| Invariants | Behavior and constraints that must remain intact |
| Scope | Allowed repositories, files, tools, data and resources |
| Iteration policy | How to select the next useful action |
| Limits | Configured time, cost, attempts, concurrency and resources |
| Stop conditions | Completion, user cancellation, limits, blockers or no defensible next step |
| Checkpoint policy | State and evidence needed for safe continuation |

Illustrative objective:

```text
Migrate the billing client to the new API. Complete when every inventoried
production call site uses the new client, contract tests pass, and the old
client has no remaining supported consumers. Preserve response and retry
semantics. After each batch, record call sites changed, checks, exceptions
and remaining units. If compatibility cannot be preserved, stop with evidence
and the decision needed to proceed.
```

| Mechanism | Documented scope | Proposed mapping obligation |
|---|---|---|
| Codex Goal | Persistent thread objective and native lifecycle/budget controls | Bind objective and evidence to the thread; retain native stop and continuation rules |
| Claude `/goal` | Session objective evaluated after turns, with documented automatic clearing | Treat clearing as a native event; verify its cause and evidence before task completion |
| Repository goals document | Versioned desired outcomes | Link portable intent; do not claim it activates native continuation |
| Schedule | Time/event trigger for a run | Define deduplication and overlap behavior independently |
| Verification gate | Check of supplied evidence or observed behavior | Preserve failure/unknown states and any accepted exception |
| Workflow script | Explicit next-step control | Declare script-owned intermediate state and resume behavior |

Native facts are sourced in [O7](research.md#o7), [A7](research.md#a7) and
[A8](research.md#a8). These are documented vendor behaviors, not tested AOK projections.
A cleared, interrupted or budget-limited native objective must not silently become Done.
A proposed runtime adapter should keep `unknown`, `blocked`, `cancelled` and `failed`
dispositions distinct where the underlying runtime can represent them; those are run-level
design candidates, not new values for the existing task status enum.

## Artifacts and persistence

| Logical artifact | Owns | Storage principle |
|---|---|---|
| Repository instructions | Durable conventions, navigation, boundaries, commands | Version with the repository and link canonical details |
| Goal/specification | Outcomes, acceptance and scope | Stable reference; track authorized changes |
| ExecPlan | Route, milestones, dependencies, recovery and progress | Living versioned artifact for substantial work |
| Task packet | Bounded assignment, roles, status and verification | Preserve the existing neutral contract |
| Context/decision record | Material discoveries, assumptions and downstream implications | Capture rationale and provenance, not a full transcript |
| Execution evidence | Actual commands/scenarios, observations and artifacts | Bind to source revision and environment |
| Review record | Findings, responses and accepted exceptions | Keep dispositions linked to changed code/evidence |
| Campaign manifest | Units, dependencies, attempts and coverage | Separate immutable inventory from mutable execution records |
| Runtime state | Claims, leases, active work, cancellation and budgets | One authoritative owner; avoid parallel writable trackers |
| Retrospective | Material events and bounded improvement proposals | Link observed evidence and the owning procedure |

An evidence entry should identify source revision, relevant workspace or environment,
command/scenario, observed result, timestamp, artifact path and producing responsibility.
Separate pass, fail, blocked, skipped and not-applicable; not-applicable requires rationale.
Do not store secrets or unnecessary raw sensitive logs in repository evidence.

A plan mentioning a check is not evidence that it ran. A stale green result from an earlier
revision cannot close current acceptance if the intervening change invalidated it.

The existing methodology artifact schema is not a generic home for these execution
artifacts. Its methodology fields and constrained paths require a deliberate extension
or separate schema. Native hook events also exceed the current neutral hook enum;
a gate projection needs capability negotiation and tests.

### Restart protocol

1. Recover objective, instructions, definition version and last valid checkpoint.
2. Inspect current revision, workspace changes, ownership and pending unit.
3. Check cancellation and limits in the authoritative runtime before new work.
4. Revalidate evidence invalidated by changed source, environment or dependencies.
5. Continue the next useful permitted action; do not replay a completed external write.
6. Save a checkpoint with pending actions, dispositions and evidence at meaningful boundaries.

Crash recovery must consider the interval between an external side effect and recording
its result. Idempotency keys, observation/reconciliation and unit fingerprints belong in
the future runtime bridge rather than a promise that arbitrary commands are safe to retry.

## Workflow recipes

### Focused change

Use `small-enhancement` or another narrowly bounded type. Inspect existing behavior,
make the minimal change, run relevant verification and report observed results. Commit
or delivery follows the existing authorization. No campaign, additional agent or long
plan is required merely to fit the catalog.

### Verified bug repair

Use `deterministic-bug`, `regression-isolation` or `concurrency-investigation`. Establish
a failing case, identify cause, fix it and demonstrate the behavior now passes. Add
meaningful regression protection when justified. Preserve adjacent behavior and distinguish
pre-existing failures. A green suite produced by skipping or weakening the failed case
is not acceptance.

### ExecPlan delivery

Create user-visible outcomes, scope, orientation, milestones, concrete steps, acceptance,
recovery and decision ownership. Implement verifiable increments and update discoveries
and remaining work. Link authoritative evidence instead of duplicating entire logs.
Require an explicit disposition for remaining criteria before completion.

### Migration campaign

#### Migration categories

| Category | Correctness concern |
|---|---|
| API/SDK | Requests, responses, errors, retries, streaming and pagination |
| Dependency/framework | Removed APIs, defaults, configuration, packaging and runtime compatibility |
| Language port | Numeric/string semantics, concurrency, resources and behavioral parity |
| Architecture | Responsibility, dependency direction, interfaces and operations |
| Schema/data | Integrity, coexistence, checkpoints, reconciliation and recovery |
| Cross-service | Consumers, rollout ordering, compatibility and deprecation coverage |

The catalog places small upgrades under modernization; a large framework upgrade can
instantiate the campaign structure when inventory and integration require it.

#### Campaign sequence

1. **Inventory:** code, indirect consumers, generated artifacts, configuration, owners and dependencies.
2. **Establish the judge:** scenarios executable against both old and new behavior; define intentional differences.
3. **Baseline:** record existing failures, environment and limitations separately.
4. **Transformation rules:** semantic mappings, exceptions and prohibited shortcuts.
5. **Pilot:** ordinary, difficult and exceptional units; for redesign, test the architecture as well as individual translations.
6. **Partition:** bounded workspaces, ownership, dependency order and shared-resource controls.
7. **Execute:** each unit produces a patch, check results, assumptions and unresolved items.
8. **Repair:** group failures by cause and improve repeated transformations at their owner.
9. **Integrate:** run cross-unit and nonfunctional checks, not only each unit's local tests.
10. **Roll out and reconcile:** validate consumer coverage and retire compatibility deliberately.

Database/data changes additionally need reconciliation counts, resumability, backup/restore
or tested roll-forward, and mixed-version checks. Do not promise rollback when the change
is irreversible. Record the actual recovery strategy and its validation.

For sandbox execution, declare the stage-in manifest, command surface, network/data access,
patch/report contract, output validation and teardown. Validate patch paths and expected
base revision before applying returned changes. A worktree separates Git changes but does
not supply OS isolation. Trusted host orchestration remains subject to permission and
credential boundaries.

### Evidence-led maintenance

Discovery produces a candidate inventory before a deletion campaign. Each candidate owns
its evidence, owner, expected benefit, confidence, verification and recovery strategy.
Investigate direct, indirect, generated and external references; use authorized runtime or
configuration evidence where necessary. No observed use alone does not establish removability.
Check emergency paths, permanent controls, seasonal behavior, reflection and killswitches.

Classify candidates as verified, needs-owner-decision, insufficient-evidence or retain-
intentionally. These are audit dispositions, not task statuses. Prioritize meaningful
benefit and create focused changes for eligible candidates. A benchmark or telemetry
claim should retain its measurement conditions rather than becoming an assumed saving.

### Review and ship

Inspect changed behavior, contextual contracts and consequential review rules. Findings
need location, impact, evidence and a suggested safe path. Resolve or explicitly accept
material findings, repair CI and rerun affected checks. Merge or release only under the
existing authorization and delivery policy, with revision-matched evidence. A successful
automated review does not waive required human approvals.

## Review rule contract

Proposed fields: stable identity, scope, invariant, rationale, unsafe example, safe path,
exceptions, owner and evaluation cases. Suggested format is a neutral record with
adapter-rendered repository guidance; current schemas do not define this object.

Example: preserve a supported public event identifier because consumers deploy
independently. An unsafe change renames it without a transition; a safe change preserves
it or introduces a compatible replacement with consumer evidence.

Evaluate an actual violation, a safe exception and an unrelated change. Include ordinary
bugs outside the rule to detect regression in general review quality. Measure precision,
consequential misses, actionability and restraint; keep mechanical checks in CI.

## Responsibilities and gates

| Responsibility | Owns |
|---|---|
| Request owner | Intent, priority and consequential tradeoffs |
| Planner | Decomposition, dependencies and approach |
| Implementer | Changes within the unit boundary |
| Verifier | Acceptance observations and regression checks |
| Reviewer | Defects, compatibility, policy and maintainability |
| Integrator | Combined changes and cross-unit correctness |
| Release/operator | Rollout, observation and recovery |
| Workflow maintainer | Procedure quality and evaluation |

These are responsibilities, not eight required agent processes. A small assignment can
combine roles; high-impact work can separate assessment. A fresh evaluator is useful only
when it improves the task's evidence and judgment.

| Gate | Evidence needed |
|---|---|
| Ready | Outcome, scope, prerequisites and verification are clear |
| Implementation complete | Intended changes exist; relevant local checks observed |
| Acceptance verified | Required behavior exercised; unavailable evidence explicit |
| Review complete | Material findings resolved or accepted by the responsible owner |
| Delivery ready | Revision-matched required CI and release conditions |
| Operationally complete | Applicable rollout and recovery obligations satisfied |

These gates compose within current task phases/statuses; they do not add enum values.
The existing workflow template describes a three-attempt implementation/QA escalation
policy. Future runtime counting must define attempt scope and distinguish a tool retry,
a unit attempt and a whole campaign retry. Do not silently override selected project policy.

## Evaluation and continuous improvement

Use representative tasks with success criteria fixed before comparing configurations.
Measure accepted outcomes, missed requirements, meaningful defects, false-positive review
findings, human interventions, recovery success, elapsed time and cost per accepted result.
Retain tool/model versions and conditions; do not infer universal routing from one case study.

After material friction, reconstruct intent and actual evidence, classify the cause and
assign improvement to one primary owner: repository instruction, project artifact, existing
skill/reference, validator, regression test, or new reusable procedure. No process change
is a valid outcome for one-off noise.

Prefer existing owners over skill sprawl. Forward-test realistic positive and negative
cases without revealing the expected answer. When models or tools change, remove one
scaffolding component at a time to see whether its benefit remains. Structural validation
alone does not establish a better workflow.

## Phased adoption

Start with the six recipes above. Extend existing owning packs before adding new packages.
Implement in four increments: catalog/templates, validated composition, target adapters,
and external-runtime integration/evaluation. The full work breakdown, dependencies,
validation, rollout and acceptance are in [implementation-plan.md](implementation-plan.md).
