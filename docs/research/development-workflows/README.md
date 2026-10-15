# Development workflow research and AOK proposal

AOK should compose development workflows from reusable procedures, planning artifacts,
execution strategies and evidence requirements. Keep these choices independent of
methodology, work-item classification, board status and task phase. Preserve AOK as the
neutral contract and adapter layer while agent harnesses and external runtimes own execution.

Research date: **2026-10-15**. AOK baseline:
`a34fecc7d9cd54ffe29ad47a428f348e394c93b1`.

This directory saves the OpenAI/Anthropic research and the full workflow proposal.
It adds documentation and structured research data. It does not install new schemas,
commands, skills, packs, adapters, workflows, schedules or runtime integrations.

## Reading order

1. [Research and source findings](research.md): the six requested OpenAI articles,
   additional Goal guidance, and nine Anthropic references; findings and limitations.
2. [Workflow proposal](proposal.md): independent axes, architecture, composition,
   Goal semantics, artifact ownership, recipes, roles, gates and verification.
3. [Workflow catalog](catalog.md): 12 groups, 48 types, 144 task activities and 12 approaches.
4. [Phased AOK implementation plan](implementation-plan.md): dependencies, work packages,
   acceptance criteria, validation and rollout.
5. [Structured catalog](catalog.json) and [source register](sources.json): machine-readable
   research companions. They are not operational AOK configuration formats.

## Exploration and readiness refinement

[Project exploration and environment readiness](project-exploration-and-readiness.md)
adds a proposed entry topology, ten exploration tasks, seven preparation tasks, skill
ownership, conditional tools, five environment profiles, outputs and acceptance criteria.
It refines the existing `discovery`/`readiness` catalog groups without changing catalog
counts. Its implementation inspection uses commit
`c2cbc3e4f29bb08b1c2064a411f6e8cc24a015c5` on 2026-10-15; the original source-research
baseline above remains unchanged.

Existing bootstrap skills and Codex setup helpers are separated from proposed exploration
and readiness skills. Application readiness requires observed checks, not merely AOK setup.

## External workflow comparison

The [comparison](external-workflow-comparison.md) reviews seven additional references and
records selective recommendations X01–X08. Its [source register](external-comparison-sources.json)
contains the requested URLs, pinned revisions and inspected files for three repositories,
plus GitHub primary documentation on safe outputs. The AOK comparison baseline is
`ea04d30ed5b27ab302f2f42a596adec9d9b68a83` on 2026-10-15.

The proposal and phased plan incorporate bounded inventory, verified readiness dimensions,
review scope, planning checks, traceability, documentation drift, a GitHub CI-diagnosis
pilot and governed review exceptions. These remain proposals; no external framework,
new skill, contract or automation is installed or activated.

## Existing capabilities and proposed additions

The following statements describe the inspected baseline, rather than general vendor capability.

| Area | Existing at this baseline | Proposed addition |
|---|---|---|
| Workflow guidance | [Agent workflow skill](../../../skills/agent-workflow/SKILL.md) and [project workflow template](../../../templates/agents/workflow.md) describe issue-sized work, roles, QA/review, evidence and retry/escalation policy | First-class versioned workflow definitions with declared steps, inputs, outputs and gates |
| Task contracts | [Task-packet schema](../../../schemas/task-packet.schema.json) supports work-item type, specification, roles, acceptance, verification, links, status and phase | Explicit workflow/run references after a compatible schema change |
| Methodology | [Profiles](../../methodology-profiles.md): Kanban active/default, XP and Lean active overlays; FDD/DSDM/AUP scaffolded; AM planned | Workflow selection as an independent axis; no automatic promotion of scaffolded methods |
| Work items | [Work-item catalog and exports](../../work-item-types.md) provide neutral and methodology-specific classifications | Workflow recipe suggestions for existing types, rather than a second competing type system |
| Artifacts | [Artifact schema](../../../schemas/artifact.schema.json) models methodology deliverables and constrained template locations | General workflow execution evidence, plans and campaign artifacts with explicit contracts |
| Hooks and skills | [Hook](../../../schemas/hook.schema.json) and [skill](../../../schemas/skill.schema.json) contracts and pack content exist | Capability-aware projections of gates, Goal instructions and workflow procedures |
| Reusable packs | Engineering guardrails, ship pipeline, phased rollout and batch conflict resolution exist in [packs](../../../packs) | Extend owning packs first; add migration/maintenance content when responsibility is distinct |
| Target output | [Support matrix](../../targets-support-matrix.md) distinguishes native, compatibility and generated output; Cursor has native rules/skills with checklist hooks | Tested workflow-specific capability declarations and mappings, not inferred feature parity |
| Native Goals | No dedicated neutral Goal schema or native Goal lifecycle adapter in the inspected schemas/CLI | Portable Goal intent with explicit Codex/Claude lifecycle mappings |
| Campaigns | No first-class campaign schema, claim/checkpoint protocol or campaign command found in inspected AOK contracts | Inventory, dependencies, unit coverage, retry policy and external-runtime handoff |
| Work-system setup | [GitHub/Azure bootstrap](../../work-system-bootstrap.md) is scaffolded/read-only; verified local bindings are supported | Any remote provisioning remains a separate provider feature, not implied by workflow work |
| Durable runtime | [Architecture](../../architecture.md) assigns queue/runtime state outside AOK; [swarmd integration](../../swarmd-integration.md) documents mappings with version untracked | Tested runtime bridge for state, claims, evidence, events and recovery |
| Distribution | Contributor Python CLI owns methodology/work-item/work-system groups; Go release CLI has a narrower command surface | Explicit decisions about new command availability in contributor and distributed CLIs |

A documented procedure is existing content, but it is not proof of automatic runtime
or policy enforcement. The workflow template's three-attempt escalation rule is guidance;
a generic durable attempt counter and scheduler are not established by that file.

## Evidence and interpretation

- Official vendor pages support the findings in `research.md`; catalog names and
  implementation choices are AOK proposals.
- The ExecPlan recipe is archived. Retain its planning pattern while treating its
  model/API recommendations as historical.
- Native Goal, dynamic-workflow, sandbox and migration runtime behavior was not tested
  in this documentation task. Product documentation is not an AOK compatibility test.
- Vendor case-study metrics and model comparisons were not independently reproduced.
- The earlier [tool configuration comparison](../agent-tool-config-comparison.md) and
  [model exports](../codex-model-selector.md) are related snapshots, not dependencies
  that this proposal activates or updates.

## Scope of this save

All additions live in this directory. Existing runtime code, schemas, adapters, packs,
methodology selections and work-item contracts remain unchanged. Commit, push and PR
publication are outside this save request.

## Validation of the original research documentation

Checked on 2026-10-15:

- `./scripts/aok validate` passed.
- Both JSON companions parsed successfully.
- Catalog checks confirmed 12 groups, 48 unique workflow types, 144 activities and
  12 approaches; all source and approach references resolve.
- Suggested work-item names match the existing task-packet enum; methodology-specific
  suggestions remain conditional on compatible selection.
- Every workflow ID occurs once in the Markdown catalog; local links and heading
  anchors resolve; all seven added files pass whitespace and credential-pattern checks.

These checks establish documentation consistency. They do not validate the proposed
schemas, runtime integrations or workflows, which have not been implemented here.

## Validation of the exploration refinement

Checked on 2026-10-15: local links/anchors, JSON parsing, unchanged catalog counts,
ten exploration/seven preparation task entries, absence of the two proposed skill
directories, whitespace and `./scripts/aok validate` all passed. These checks validate
documentation structure; no application setup, skill installation or readiness runtime
was exercised.
