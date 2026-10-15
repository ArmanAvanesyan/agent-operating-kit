# Agent specification proposal

Status: **proposed, not implemented**. Reviewed 2026-10-15 against AOK
`bbe8c264a6a2658b37bc40c479eec6bc555e01cd`. Draft schemas and fixtures in this
directory are documentation assets, not configuration accepted by `scripts/aok`.
They install no agents, register no plugins, change no defaults, and launch no models.

An Agent Definition binds stable identity, reusable roles, instruction sources,
approved execution profiles, capabilities, permissions, and output contracts.
An assignment binds this reusable definition to a task packet. A resolved manifest
records the effective configuration; an external executor owns execution and run state.

## Existing capabilities versus additions

| Concern | Existing at the stated revision | Proposed addition |
|---|---|---|
| Identity and roles | [Role schema](../../../schemas/role.schema.json): names, callsigns, responsibilities, capabilities as strings, escalation rules and target hints | Independent Agent identity, typed capability references, role composition |
| Work assignment | [Task packet](../../../schemas/task-packet.schema.json), assignee/QA fields and verification | AgentAssignment referencing existing packet without replacing status/phase |
| Instructions | [Skills](../../../schemas/skill.schema.json), roles, project guidance and packs | Deterministic assembly, parameters and provenance |
| CLI/model selection | [Research inventories](../../research/model-selection.md), target adapters | ExecutionProfile, model discovery/compatibility checks, explicit fallback |
| Tools/MCP/plugins | Adapter and pack content, setup/render paths | Capability registry, required/optional dependencies, permission intersection |
| Outputs | [Methodology artifact](../../../schemas/artifact.schema.json) templates | General OutputContract with destinations and validation; optional artifact linkage |
| Portability | [Adapter schema](../../../schemas/tool-adapter.schema.json), generated contracts/bundles, curated native assets | Agent-format import/export, loss reports, sidecars and scope-aware enforcement checks |
| Readiness | Exploration/readiness skills and helpers | Agent-specific resolved readiness report |
| Runtime | Generated integration contracts; runtime remains external | Executor handoff and observed run records, never a new AOK daemon |

## Reading order

1. [Specification and field semantics](specification.md).
2. [Composition and defaults](composition.md).
3. [Native adapter parsing and rendering](adapters.md).
4. [Phased implementation plan](implementation-plan.md).
5. [Draft schemas](schemas/) and [example inventory](examples/README.md).
6. [Additional contracts](additional-contracts.md): invocation, context, registries, runs,
   lifecycle, handoff, evaluation and migration.
7. [Validation and evidence limits](validation.md).

## Design decisions

- Identity is stable across clients. A role is a reusable posture; a model is execution configuration.
- Use JSON as the canonical interchange. YAML authoring is optional and must parse into the same data model.
- Native definitions are valid entry points: parse them through the matching adapter before adoption.
- AOK becomes authoritative only after an explicit adoption step; imported files remain user-owned.
- Native format and scope are versioned adapter concerns, not universal neutral field names.
- Research model IDs are candidates, not automatic defaults or a measured ranking.
- Every example is illustrative. Role IDs, environment profiles and capability IDs require a future registry.

No operational schema or command was added by this proposal. Relative paths in examples
resolve from this proposal root, except task destinations, which resolve within the
assignment workspace after policy checks. Symbolic roots such as `workspace` and
`reports` require explicit resolution; `reports` means the selected output subtree.

## Extended proposal coverage

The draft now includes InvocationContract, ContextPolicy, CapabilityRegistry,
EnvironmentProfile, AgentResult, AgentRunRecord, LifecyclePolicy, AdapterConformance,
EvaluationContract and MigrationContract. All are proposed additions; existing AOK
capabilities in the baseline table are unchanged. Registry examples illustrate bindings
but leave unresolved external/tool/role/check references explicitly blocked. Full snapshots
and synthetic blocked run records are design fixtures, not observed execution evidence.
