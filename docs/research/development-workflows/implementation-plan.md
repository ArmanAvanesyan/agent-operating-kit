# Phased AOK implementation plan

Status: **proposed**. This plan describes future changes to AOK, not work executed by
saving the research. The [baseline capability table](README.md#existing-capabilities-and-proposed-additions)
is authoritative for what was found at commit
`a34fecc7d9cd54ffe29ad47a428f348e394c93b1`.

The intended result is portable, validated workflow intent with explicit target and runtime
boundaries. Existing task packets, methodology selection, status/phase enums and user-owned
files remain compatible. No phase turns AOK into a daemon or an execution-host provider.

## Dependency order

```text
Baseline and design decisions
          |
Phase 1  Catalog and reusable templates
          |
Phase 2  Validated composition and references
          |
Phase 3  Target projections and compatibility tests
          |
Phase 4  External runtime bridge and outcome evaluation
```

Work on independent recipe content can overlap after naming and ownership are agreed.
Do not ship runtime continuation before its evidence, cancellation and retry semantics
are defined. No duration estimate is implied by the ordering.

## Baseline and design decisions

These are prerequisites for implementation, not blockers to using the research as guidance.

1. Record the actual implementation branch/revision and refresh relevant baseline claims.
2. Decide whether general workflow artifacts use a separate schema or a compatible
   extension of methodology artifacts. The current artifact schema is methodology-specific.
3. Decide where workflow definitions and run/campaign specifications live. Keep mutable
   runtime state outside the reusable definition catalog.
4. Decide whether phase 2 adds task reference fields or retains document references first.
   The current packet schema rejects unknown fields.
5. Define target capability states: unavailable, documented, rendered, structurally
   validated and runtime-tested. Do not equate those with overall adapter support.
6. Decide ownership of new command groups across Python and Go distribution surfaces.
   Document any intentionally contributor-only phase; do not advertise unavailable commands.
7. Agree evidence sensitivity, retention and storage policy before collecting live traces.

Deliverable: an ADR with compatibility decisions, runtime boundary and unresolved choices.
Acceptance: every new concept has one owner and a defined relationship to existing objects.

## Phase 1 Catalog and reusable templates

### Goal

Make six useful workflows adoptable as content without introducing an execution engine
or requiring new task-packet fields.

### Work packages

| ID | Task | Existing owner to inspect first | Output |
|---|---|---|---|
| P1-01 | Stabilize group/type/approach identifiers and selection guidance | Core model and this research catalog | Documented taxonomy and terminology |
| P1-02 | Create focused-change and verified-bug recipes | Agent workflow and engineering guardrails | Reusable procedures with proportional verification |
| P1-03 | Create ExecPlan delivery template | Phased rollout template | Resumable plan with milestones, decisions and acceptance |
| P1-04 | Create Goal intent and execution-evidence templates | Task packet and existing verification guidance | Outcome/constraints/stop contract and observed-result format |
| P1-05 | Create migration campaign template | Phased rollout and conflict-resolution content | Inventory, pilot, transformation rules, parity and recovery guidance |
| P1-06 | Create evidence-led maintenance template | Engineering guardrails | Candidate evidence and confidence dispositions |
| P1-07 | Reconcile review-and-ship recipe with existing shipping content | Ship pipeline | One owning procedure for CI/review/delivery |
| P1-08 | Add source provenance, examples and unsupported-feature notes | Documentation and pack manifests | Discoverable content with honest support boundaries |

### Exploration and readiness foundation

Before developing the six delivery recipes, add content for the proposed
[exploration and readiness procedures](project-exploration-and-readiness.md):

- **P1-09:** implement `project-exploration` instructions and references for E01–E10,
  preserving source-backed discovery and a separate environment outcome.
- **P1-10:** implement `environment-readiness` instructions and references for R01–R07,
  with five conditional profiles and actual check evidence.
- Reuse `project-bootstrap` for AOK initialization and `qa-verification` for acceptance;
  do not duplicate their procedures or claim they install application dependencies.
- Forward-test an unfamiliar repository, a multi-repo/subsystem investigation, an
  inspection-only task, a missing dependency, a failing application check and a setup
  helper that exits zero despite a diagnostic failure.
- Acceptance requires honest observed/untested/unknown distinctions, preserved user
  changes, valid handoff references, and no ready verdict for unmet required capabilities.

These are new work packages, not implemented skills. Phase 2 must decide whether profile
and readiness records need additional schemas; until then, keep them in linked documents
or supported packet fields. Target projections and runtime execution remain later phases.

### Initial six recipes

| Recipe | Required result | Minimum evidence |
|---|---|---|
| Focused change | Bounded intended behavior | Relevant check or observed artifact |
| Verified bug repair | Cause corrected without weakening the judge | Before/after reproducer and justified regression coverage |
| ExecPlan delivery | Complete verifiable milestones | Current plan, acceptance evidence and unresolved dispositions |
| Migration campaign | All in-scope units reconciled | Baseline, unit checks, parity/integration and recovery evidence |
| Evidence-led maintenance | Justified focused cleanup | Usage/consumer evidence and preserved operational purpose |
| Review and ship | Reviewable authorized delivery | Resolved findings and revision-matched required checks |

### Acceptance

- Content can be referenced by existing `spec`, role, verification and pack mechanisms.
- Examples do not insert unrecognized fields into current task packets.
- Each artifact has one owner; no mandatory duplicate goals, plans or logs.
- Small changes can use the lightweight recipe without unnecessary planning or delegation.
- A complex example can be resumed from repository artifacts and current source state.
- Existing packs are extended where they own the responsibility; new packs have distinct scope.

### Validation

Run kit validation, check source/relative links, verify examples against existing schemas
where they claim current compatibility, and render affected packs for supported targets.
Review a small-change example and a migration example to detect unnecessary process or
missing recovery information. Content adoption is not proof of native lifecycle execution.

### Rollout and recovery

Ship opt-in content. Preserve user-owned files and existing generated-marker/hash/backup
conventions. Removing the optional recipe must not invalidate existing packets or project
methodology configuration.

## Phase 2 Validated composition

### Goal

Represent reusable workflow definitions and campaign intent with machine-checkable contracts,
while preserving existing project and task compatibility.

### Work packages

| ID | Task | Dependency | Output |
|---|---|---|---|
| P2-01 | Specify a workflow-definition schema | Phase 1 terminology | Inputs, steps, dependencies, outputs, evidence, roles and capabilities |
| P2-02 | Specify campaign intent and unit inventory | P2-01 | Bounded units, dependencies, ownership and aggregate acceptance |
| P2-03 | Specify evidence and review-rule records | P1 templates | Revision/environment provenance, dispositions and evaluation cases |
| P2-04 | Design task/workflow references | Compatibility ADR | Versioned references with explicit migration strategy |
| P2-05 | Validate cross-references and composition | P2-01 through P2-04 | Unknown-ID, cycle, missing-output and incompatible-target diagnostics |
| P2-06 | Export definitions and templates offline | P2-05 | Deterministic output and explicit supported entrypoints |
| P2-07 | Document read/write ownership and versioning | All contracts | Clear distinction among definition, assignment, run and campaign |

Proposed storage candidates include a future workflow catalog directory and new schemas
under `schemas/`; exact paths are a design decision. The files in this research directory
are not those installed contracts. Do not add illustrative commands to operational docs
until the commands exist in the claimed CLI.

### Composition rules to enforce

- Stable unique IDs and explicit versions for referenced definitions.
- Acyclic declared dependencies, or a separately modeled bounded repair loop.
- Required inputs available before a step; required outputs/evidence accounted for.
- Explicit owner for every mutable unit or shared resource.
- Verification exceptions require rationale and cannot silently satisfy a mandatory gate.
- Target capabilities distinguished from native features merely described in vendor docs.
- A native-only requirement cannot quietly degrade into a checklist or text prompt.
- Methodology-specific work-item suggestions respect compatible active selection.
- No changes to existing neutral status or phase enums merely to represent execution state.

### Acceptance and meaningful tests

Positive fixtures cover the six initial recipes and their compatible combinations.
Negative fixtures cover duplicate IDs, dangling references, dependency cycles, missing
evidence, unsupported native capability, conflicting ownership and incompatible methodology
suggestions. Old packet fixtures must still validate. New reference fields must reject
malformed values without making the fields mandatory for legacy projects.

Export tests should establish deterministic bytes, preservation of user edits, refusal of
unsafe path writes and absence of remote side effects. These tests verify behavior and
compatibility rather than merely repeating schema implementation details.

### Distribution decision

If the Python contributor CLI implements export first, state that limitation explicitly.
Before advertising the same commands through the installed Go binary, implement and test
its equivalent surface or share a well-defined contract. Release archives carrying a catalog
do not establish that the binary executes it.

### Rollout and recovery

Keep definitions opt-in and versioned. Provide migration notes for new reference fields.
Existing projects without workflow metadata remain valid. Reverting an export must not
delete unrelated source, evidence or runtime records.

## Phase 3 Target projections and compatibility

### Goal

Render useful procedures for Codex and Claude while preserving differences in instructions,
Goal lifecycle, hooks, orchestration, permissions and persistence.

### Work packages

| ID | Task | Output |
|---|---|---|
| P3-01 | Define per-target capability declarations | Documented/rendered/tested capability matrix with versions |
| P3-02 | Render instructions, skills, roles and plan links | Native content where supported, explicit guidance elsewhere |
| P3-03 | Map portable Goal intent | Native activation and lifecycle mapping only where tested |
| P3-04 | Project evidence gates to supported hook/CI surfaces | Event/handler/output/permission mapping and honest limitations |
| P3-05 | Map scripted orchestration or external-runner handoff | Explicit owner of script state and resumption |
| P3-06 | Test target-specific restart and termination | Lifecycle fixtures and recorded live compatibility results |
| P3-07 | Update support docs and distribution assets | Support claims limited to exercised combinations |

### Required distinctions

| Concern | Mapping requirement |
|---|---|
| Goal persistence | Codex thread state and Claude session state are not interchangeable |
| Native clearing | Determine cause/evidence before advancing AOK task status |
| Permissions | Goal activation does not grant tools, credentials or deployment authority |
| Hook envelope | Validate actual events, fields, outputs, environment and trust; no verbatim-portability assumption |
| Scripted orchestration | A reusable skill does not supply a durable workflow runtime |
| Isolation | Context separation and worktrees do not establish OS sandboxing |
| Capability fallback | Guidance-only rendering is labeled and cannot satisfy a required native control |

### Acceptance and validation

Test applicable instruction discovery, rendered references and target syntax. For live
compatibility tests, record tool version/date, adapter version and the behaviors exercised:
completion, interruption, cancellation, resumption, budget stop, unavailable verifier and
failed gate. Verify no false Done transition when evidence is absent or native state clears.

Use small fixtures and bounded model-backed tests only when those executions are authorized
and needed to validate the adapter. Keep deterministic tests separate from variable
model outcomes. Do not expose credentials or sensitive traces in committed evidence.

### Rollout and recovery

Start with opt-in Codex and Claude projections. Other adapters can receive documented
content only at their declared support level. Keep native Goal/lifecycle integration
experimental until the compatibility matrix is backed by observed runs. Users can disable
one projection without rewriting neutral workflow definitions.

## Phase 4 External runtime bridge and evaluation

### Goal

Connect validated intent to an external coordination runtime for campaigns and durable
execution while leaving AOK responsible for contracts, projections and validation.

### Work packages

| ID | Task | Output |
|---|---|---|
| P4-01 | Choose and pin a tested runtime interface | Versioned compatibility record and capability inventory |
| P4-02 | Map assignments, roles, dependencies and lifecycle | Explicit translation policy and unsupported-state handling |
| P4-03 | Implement claims, attempt scopes and checkpoints | Durable ownership and restart protocol |
| P4-04 | Validate workspace staging and returned artifacts | Base-revision, path, evidence and output-integrity checks |
| P4-05 | Implement cancellation, budgets and bounded retries | Tested stop behavior without false completion |
| P4-06 | Reconcile external side effects and partial execution | Idempotency/recovery policy with audit events |
| P4-07 | Pilot a real migration and maintenance campaign | Unit coverage and accepted outcomes with measured overhead |
| P4-08 | Evaluate and simplify the harness | Component ablations and outcome-based recommendations |

The documented AOK-to-swarmd mapping is a starting design input, not a tested integration.
If swarmd is selected, verify the actual version, config names and MCP methods before
claiming compatibility. Explicitly translate AOK review/QA/blocked states rather than
collapsing them into terminal failure. Do not copy unverified deployment examples into a
new adapter without checking the selected runtime.

### Recovery and failure tests

- Two workers cannot own the same mutable unit concurrently under the chosen claim model.
- A crash after producing a patch does not lose the patch or cause blind replay.
- A crash after a remote write reconciles the existing result before retry.
- Cancellation prevents new work and preserves the current evidence/checkpoint.
- Budget exhaustion records incomplete progress rather than completion.
- Stale evidence cannot satisfy acceptance for a changed revision or environment.
- A conflicting patch or unsafe artifact path is rejected with a useful disposition.
- Failed local verification cannot be overwritten by aggregate campaign progress.
- A temporarily blocked dependency remains recoverable without resetting accepted units.
- Campaign coverage includes every unit and every exclusion/blocker disposition.

### Evaluation design

Use a bounded, representative task set with fixed acceptance and recorded versions.
Include small edits, bug repair, complex delivery, migration parity, maintenance evidence,
review-rule positive/negative cases and recovery failures. Compare a simple baseline to
added planning, evaluator and parallelization components one change at a time.

| Measure | Interpretation |
|---|---|
| Accepted completion | Required behavior demonstrated, not worker self-report |
| Consequential misses | Unmet requirements or defects discovered after claimed completion |
| Review restraint | Noise on safe exceptions and unrelated changes |
| Human intervention | Scope/decision corrections and manual recovery needed |
| Recovery success | Progress safely resumed after interruption/failure |
| Cost per accepted result | Total measured run cost including retries and evaluation |
| Time to accepted result | Wall-clock time including integration and review |
| Coverage reconciliation | All campaign units and dispositions accounted for |

Set project-specific acceptance thresholds before trials. Do not invent universal cost or
latency targets from vendor case studies. Keep structural validity separate from demonstrated
workflow improvement. Retest on meaningful model/tool changes and remove overhead that
no longer contributes to quality or recovery.

### Rollout and recovery

Begin with report/patch-producing runs and small inventories. Expand only after recovery
and integration results justify it. Publishing PRs, merging and deployments remain explicit
delivery capabilities governed by existing authorization. A failed pilot can revert to
content-only workflows without losing portable definitions or evidence.

## External comparison work packages

These proposed work packages implement the [comparison backlog](external-workflow-comparison.md#recommended-backlog).
Their ordering refines the four phases without changing current schemas, catalog counts,
methodology support or runtime capabilities.

| ID | Phase and dependency | Deliverable | Acceptance and validation |
|---|---|---|---|
| X01 | Phase 1, P1-09 | Bounded inventory procedure/tool design | Fixtures exercise depth limits, generated/vendor exclusions and relevant hidden agent/CI directories; source evidence remains scoped |
| X02 | Phase 1, P1-10 | Command matrix and context/verification/feedback readiness rubric | Missing command, unavailable verifier and insufficient diagnostics cannot be reported as ready; preserve existing profile definitions |
| X03 | Phase 1 linked template; Phase 2 contract | Review scope and remediation records | Compare dirty-workspace, single-commit and merge-base branch cases; findings remain tied to reviewed/corrected revisions |
| X04 | Phases 1–2, planning templates and P2 validators | Specification/architecture/breakdown acceptance | Positive and negative fixtures cover untestable acceptance, missing references, cycles and premature transition |
| X05 | Phase 2, P2-03/P2-04 | Requirement/task/change/evidence references | Detect dangling/stale references and incomplete acceptance; old packets remain valid; authoritative state ownership documented |
| X06 | Phase 1 documentation recipe; Phase 2 validation where mechanical | Drift findings and decision preservation | Test stale docs, missing docs and intentionally superseded decisions; do not overwrite rationale silently |
| X07 | Phase 3 projection; Phase 4 live pilot | Optional CI diagnosis adapter | Pin compiler/runtime; inspect generated jobs and configured/default outputs; prove artifact-only mode cannot publish, then evaluate diagnosis/deduplication on bounded cases |
| X08 | Phase 2 review policy; Phase 4 outcome evaluation | Governed exceptions | Test narrow accepted exception, expired/stale exception and materially changed code; ensure genuine defects remain detectable |

### Pilot constraints

The GitHub automation projection is separate from work-system bootstrap/provider state.
AOK supplies intent and validation; GitHub Agentic Workflows supplies compilation/execution.
The runtime's safe-output mechanism can publish configured actions automatically. Do not
infer a human approval requirement or absence of writes from read-only agent permissions.
Artifact-only tests must inspect actual compiled jobs and outputs for the selected version.

Start with a failed-CI fixture and a revision-linked report. Evaluate root-cause evidence,
confidence calibration and duplicate consolidation. Publishing issues/comments is a later,
explicitly authorized capability; broad autonomous repository assistance is not the pilot.

No external framework dependency or fixed agent count is required for X01–X06. If importing
source later, review pinned implementation, dependencies and license/notice obligations.
Source inspection is not runtime validation; all live claims need observed evidence.

## Risks and decisions to revisit

| Risk | Mitigation |
|---|---|
| Workflow/skill duplication | One primary procedural owner and references |
| Too many artifacts for small work | Lightweight path and conditional templates |
| False target parity | Per-capability support and runtime compatibility records |
| Overbuilt multi-agent execution | Single-agent baseline and measured component ablations |
| Divergent Python/Go command support | Explicit distribution decision and surface tests |
| Methodology/task-state conflation | Independent axes and unchanged neutral enums |
| Stale evidence or silent native termination | Revision-bound checks and explicit lifecycle translation |
| Sensitive logs and credentials in artifacts | Data minimization and retention/access policy |
| Endless repair loops | Defined attempt scopes, limits and escalation |
| Accidental remote writes during export | Offline defaults and tested side-effect boundaries |

## Completion of the proposed program

The program is complete only when the initial recipes have validated contracts, honest
target support, compatible existing projects, observed runtime recovery where claimed,
and representative outcome evaluation. Saving this research completes none of those
implementation acceptance criteria by itself.
