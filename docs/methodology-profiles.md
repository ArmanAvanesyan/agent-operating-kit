# Methodology Profiles

AOK keeps the work system, flow framework, and engineering practices separate:

```text
Work system:       GitHub or Azure DevOps
Flow:              exactly one
Practice overlays: zero or more
```

The work system maps neutral fields and states. Methodology changes planning,
WIP, role emphasis, hook expectations, and may add a project-, release-,
iteration-, or feature-level lifecycle without changing the task `phase` or
the `Inbox` through `Done` status model.

```text
Methodology lifecycle: project, release, iteration, or feature progression
Task phase:           Intake, Planning, Build, QA, Review, Release, Operate
Board status:         Inbox, Ready, In Progress, QA, Review, Blocked, Done
```

These axes are deliberately independent. A DSDM project may be in
`evolutionary-development` while individual tasks move through Build, QA, and
Review. An FDD feature may move from `design-by-feature` to
`build-by-feature` without defining new task phases.

Methodology selection is supported by the contributor Python CLI and the
opt-in Go CLI.

## Support

| Profile | Kind | Support |
|---|---|---|
| Kanban | flow | active, default |
| XP | overlay | active |
| Lean | overlay | active |
| FDD | flow | scaffolded; enforcement pending |
| DSDM | flow | scaffolded; enforcement pending |
| AUP | flow | scaffolded; enforcement pending |
| AM | overlay | active, guidance only |

Only active profiles can be selected. AM is an explicitly selected lightweight
modeling overlay; it adds no lifecycle gate or approval authority. Scaffolded
profiles expose catalog metadata and exportable templates but do not render
operational policy.

FDD, DSDM, and AUP include structured lifecycle, role, artifact, ceremony,
gate, and phase-mapping catalogs. Their enforcement status is recorded
separately and remains pending; they remain non-selectable until lifecycle
state and protected enforcement are implemented.

## Initialize a Project

Kanban with no overlays is the default:

```bash
./scripts/aok project init /path/to/project
```

Select active overlays by repeating `--overlay`:

```bash
./scripts/aok project init /path/to/project \
  --flow kanban \
  --overlay xp \
  --overlay lean
```

An existing explicit selection is preserved when init receives no methodology
flags. Providing `--overlay` without `--flow` is rejected.

## Reconfigure

```bash
./scripts/aok methodology set /path/to/project \
  --flow kanban \
  --overlay xp \
  --overlay lean

./scripts/aok methodology show /path/to/project
./scripts/aok methodology validate /path/to/project
./scripts/aok methodology list
```

`methodology set` replaces the complete selection. Omitting `--overlay` clears
all overlays. Overlay order is canonical and does not depend on flag order.

The selection is stored in `.aok/project.json`:

```json
{
  "methodology": {
    "flow": "kanban",
    "overlays": ["xp", "lean"]
  }
}
```

The rendered policy is `agents/methodology.md`. It is generated from the flow
fragment followed by overlay fragments. Do not edit it directly.

## Safety and Migration

AOK refuses to overwrite an unmarked or manually changed methodology policy.
Use `--force` only when the generated file should be replaced; AOK writes a
timestamped backup first.

Projects without methodology metadata remain valid as legacy implicit
Kanban-without-overlays projects. Run project init again or use `methodology
set` to materialize an explicit selection and generated policy.

Methodology commands preserve work-system bindings and unrelated manifest
keys. Work-system validation remains independent.

## Export a Delivery Scaffold

Export the roles, editable artifact templates, and reference documentation for
a scaffolded flow without selecting it:

```bash
./scripts/aok methodology scaffold /path/to/project --flow fdd
./scripts/aok methodology scaffold /path/to/project --flow dsdm --json
./scripts/aok methodology scaffold /path/to/project --flow aup
```

The command writes methodology-specific roles under `agents/roles/`, artifact
templates under `docs/methodology/<flow>/artifacts/`, and a flow README. It
does not change `.aok/project.json`, create lifecycle state, assign roles,
enforce gates, or change a remote work system.

Exported files carry an AOK scaffold marker. Re-running the command is
byte-for-byte idempotent while they remain unchanged. If a user edits or
replaces any exported file, AOK refuses to overwrite the scaffold as a unit.
Use `--force` to create timestamped backups and restore the catalog versions.

Human-only roles are explicit in the catalog. Agents may prepare evidence for
those roles but cannot assume sponsor, stakeholder, business-visionary, or
other human approval authority.

Methodology-aware issue and work-item templates use the separate
`./scripts/aok work-item scaffold` command. Core types are always included; active
overlays and explicitly exported scaffolded flows add their native types.
See `work-item-types.md`.

## Structured Scaffold Contract

Each scaffolded flow defines:

- lifecycle stages, scope, allowed neutral phases, and transitions;
- named roles with `agent`, `human`, or `either` assignment mode;
- milestone, evolutionary, iterative, or working artifacts;
- ceremonies with participants and outputs;
- transition gates with inputs, artifacts, roles, and human approvals;
- a one-to-one mapping from lifecycle stages to their allowed task phases.

Task packets may optionally carry `lifecycleStage`, `featureId`, `moscow`, and
`timebox`. FDD packets may additionally carry `subjectArea`,
`businessActivity`, `featureName`, structured `featureNameParts`,
`estimatedEffortDays`, and completed `featureMilestones`. These fields are
additive; `phase` and `status` remain required and unchanged.

DSDM packets may additionally carry `requirementId`, `requirementTheme`,
`requirementOutcome`, `projectIncrement`, `projectMoscow`, `incrementMoscow`,
and `timeboxMoscow`. The existing `moscow` field is retained as a legacy alias
for the current timebox priority. New DSDM material uses `timeboxMoscow`.

AUP packets reuse `requirementId`, `requirementOutcome`, `acceptanceCriteria`,
and `verification`, and may additionally carry `requirementType`,
`iterationId`, `iterationObjective`, and a complete `disciplineEvidence`
checklist.

## FDD Feature Contract

FDD uses the hierarchy:

```text
Subject Area → Business Activity → Feature
```

`Business Activity` is AOK's canonical name for an FDD feature set. The
preferred feature-name form is:

```text
<action> <result> <by|for|of|to> <object>
```

For example, `Generate PDF for Invoice`. The classic article form,
`<action> the <result> <by|for|of|to> a|an <object>`, is also accepted.

Validate names without selecting or activating FDD:

```bash
./scripts/aok methodology validate-feature --name "Generate PDF for Invoice"
./scripts/aok methodology validate-feature \
  --name "Calculate the total amount of a Sale" \
  --json
```

Validation is read-only. It normalizes whitespace, identifies the naming
style, returns the parsed action/result/preposition/object, and reports the
preferred short rendering.

FDD progress uses six sequential milestones:

| Milestone | Weight | Cumulative |
|---|---:|---:|
| Domain Walkthrough | 1% | 1% |
| Design | 40% | 41% |
| Design Inspection | 3% | 44% |
| Code | 45% | 89% |
| Code Inspection | 10% | 99% |
| Promote to Build | 1% | 100% |

Store completed milestone identifiers, not a manually entered percentage.
The percentage is derived from the last sequentially completed milestone.
FDD scaffold gates document required hierarchy, inspections, verification,
and build promotion, but lifecycle transition enforcement remains deferred.

## DSDM Requirement and Timebox Contract

DSDM uses an optional requirement hierarchy:

```text
Theme → Requirement → Acceptance Criterion
```

Requirements use an uppercase namespace plus numeric sequence, such as
`REQ-014`, `BILL-14`, or `REG2-003`. A requirement has a clear outcome and at
least one acceptance criterion; it does not use FDD feature-name grammar.

MoSCoW priority may be recorded independently for the project, a project
increment, and a timebox. The values are Must Have, Should Have, Could Have,
and Won't Have this time. The commonly used 60% Must Have maximum and 20%
Could Have target are advisory capacity guidance, not validation failures.

Validate a requirement definition without selecting or activating DSDM:

```bash
./scripts/aok methodology validate-requirement \
  --id REQ-014 \
  --theme Billing \
  --outcome "Produce customer-ready invoice PDF" \
  --project-moscow must \
  --acceptance "The PDF contains the approved invoice fields"

./scripts/aok methodology validate-requirement \
  --id REQ-014 \
  --outcome "Produce customer-ready invoice PDF" \
  --project-moscow must \
  --acceptance "The PDF contains the approved invoice fields" \
  --increment INC-1 \
  --increment-moscow must \
  --timebox TB-03 \
  --timebox-moscow must \
  --json
```

The validator normalizes whitespace and checks identifiers, priorities,
acceptance criteria, and increment/timebox pairings. It validates structure,
not the semantic quality of the outcome, and never writes files.

The DSDM scaffold exports curated bodies for all 15 catalogued products:
governance and benefits records, the Prioritised Requirements List, the
Solution Architecture Definition, development and management approaches,
delivery and timebox plans, the Evolving Solution, review records, and the
Deployed Solution. Timeboxes use Kick-off, Mid-timebox, and Close-out
checkpoints. Delivery is tracked by requirement disposition, verification, and
Business Ambassador acceptance; no FDD milestone percentages are used.

The DSDM gates and human checkpoints describe future enforcement only.
`validate-requirement` is the only operational DSDM validation in this slice.

## AUP Iteration and Milestone Contract

AUP selects typed work into an iteration inside one of four lifecycle stages:

```text
Inception → LCO
Elaboration → LCA
Construction → IOC
Transition → PR
```

Selected work may be a use case, user story, non-functional requirement,
business rule, defect, or technical item. IDs use the same uppercase namespace
and numeric sequence as other neutral requirements, for example `UC-03`.

Every iteration consciously addresses seven disciplines: Model,
Implementation, Test, Deployment, Configuration Management, Project
Management, and Environment. Each discipline supplies evidence or a non-empty
reason that it does not apply.

Validate an iteration without selecting or activating AUP:

```bash
./scripts/aok methodology validate-iteration \
  --id I-12 \
  --stage construction \
  --objective "Deliver invoice generation increment" \
  --work-item use-case=UC-03 \
  --discipline model=docs/architecture.md \
  --discipline implementation=PR-142 \
  --discipline test=reports/test-results.xml \
  --discipline deployment="not-applicable:No release activity" \
  --discipline configuration-management=build-142 \
  --discipline project-management=docs/iteration-plan.md \
  --discipline environment=ci/run-912 \
  --json
```

The validator normalizes whitespace, checks the iteration and selected-work
identifiers, requires every discipline exactly once, and reports the milestone
target derived from the lifecycle stage. It never records milestone state or
writes files.

The AUP scaffold exports curated bodies for all 11 catalogued products:
Vision, Requirements and Architecture Models, Iteration Plan and Assessment,
Implementation, Test Results, Deployment Plan and Package, Risk List, and
Configuration Record. Iteration and milestone gates remain descriptive future
enforcement; only `validate-iteration` is operational.

The DSDM catalog follows the terminology in the DSDM Agile Project Framework.
FDD follows Jeff De Luca's five-process model and named project roles. AUP
uses its four lifecycle stages and discipline-oriented delivery roles. All
generated prose and templates are original AOK adaptations.
