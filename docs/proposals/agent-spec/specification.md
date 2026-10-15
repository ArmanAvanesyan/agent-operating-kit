# Agent contract and field semantics

All behavior here is proposed. [Schemas](schemas/) use JSON Schema 2020-12 and reject
unknown top-level/typed-object fields. They check shape, not executable readiness.
`aok.example` schema IDs identify draft resources and are not network endpoints.

## Objects and ownership

| Object | Purpose | Owner / persistence |
|---|---|---|
| Agent | Reusable identity and operating contract | Versioned project/pack content |
| Role | Responsibilities, evidence and escalation | Existing AOK role catalog |
| ExecutionProfile | Client, provider, model, reasoning, limits, environment | Versioned project/pack content |
| OutputContract | Deliverables, templates, schema checks, destinations | Versioned project/pack content |
| AgentAssignment | Task binding, approved profile, input parameters and narrowed permissions | Work contract referencing an existing task packet |
| ResolvedAgentManifest | Frozen effective configuration, source hashes and compatibility result | Generated evidence, immutable for a run |
| AdapterReport | Native mapping, scope, losses, enforcement and diagnostics | Generated alongside import/export |
| Run record | Actual model/configuration, events, usage, results and deviations | External executor/runtime; proposed run-record schema; no live execution implementation |

Draft run records do not claim that the model provider actually obeyed a requested setting.
Requested, resolved, and observed values must remain separate.

## Agent fields

- `apiVersion`, `kind`, `metadata`: document identity. Metadata includes stable slug ID,
  display name, version and description, with optional tags. `identity.callsign` is
  optional presentation metadata; it does not replace task assignee semantics.
- `identity`: purpose, optional communication style/evidence style, and boundaries.
  Persona prose never grants authority or replaces responsibilities.
- `roles`: exactly one primary role; optional supporting roles. All references must
  resolve. Human-only roles cannot be assigned to an automated executor.
- `instructions.sources`: ordered local instruction files, with typed scalar parameters.
  Supported parameter types initially: string, number, boolean. Defaults must match
  declared type and enum. No arbitrary expression evaluation or shell interpolation.
- `execution`: default profile, allowed profiles, explicit-only fallback policy and
  ordered fallback profile references. All selected/default/fallback IDs must be allowed.
- `capabilities`: typed arrays for tools, skills, MCPs and plugins. Each entry has a
  reference, required flag, optional version constraint and optional MCP tool subset.
  `tools` on non-MCP references is a semantic error. Empty arrays mean none requested.
- `permissions`: explicit filesystem read/write roots, network mode/hosts,
  credential-reference-only policy and external-mutation policy. Permission declaration
  does not prove enforcement. `explicit-authorization` requires trusted run authorization.
- `environment`: named environment profile and readiness gate. No environment is installed
  merely by declaring it. Secret values are excluded from committed definitions.
- `outputs`: default and allowed contract IDs, with scalar template parameters.
- `verification.required`: named checks resolved from a future registry.
- `delegation`: enabled flag, allowed agent IDs, depth limit. Disabled means no child
  execution; enabled still requires assignment/platform authorization and budget checks.

## Execution profile fields

`client.adapter` selects the target adapter, independently of CLI executable location.
`interface` is cli/app/api; an optional version constraint is resolved against discovery.
No executable or command fragment is accepted from untrusted agent identity content.

`model.selector` is the exact host-recognized identifier. Optional `provider` selects a
connection only where the client permits this independently. Adapters must resolve
inherit/alias selectors against the host and record the actual resolution if available.
Unresolved aliases cannot be represented as pinned reproducible model versions.

`model.reasoning.mode` is provider-default, effort, budget, model-variant, or disabled.
`effort` and `budgetTokens` are optional except in their corresponding modes. Their
compatibility is semantic: provider-default/disabled allow neither; effort or budget
allows its corresponding field and a second setting only when verified jointly supported;
model-variant allows a confirming effort only when it agrees with the selected variant.
No global effort enum assumes that every host supports every tier. Thinking is not an effort.

`model.generation` holds optional max output tokens, temperature and top-p. Numeric
shape validation is insufficient: provider/model limits must be checked by the adapter.
`limits` may declare timeout, turns, tool calls and USD cost; each requires an enforcement
owner or the resolution blocks. Cost ceilings need metering/cancellation behavior and may
have an unavoidable in-flight overrun, reported explicitly. They are not guarantees today.

`compatibility.unsupportedSettings` is fixed to error in the initial proposal.
`extensions` is a namespaced map of adapter-owned objects. Unknown extension namespaces
are retained for interchange but cannot influence execution without validation.

## Outputs, assignment and resolved manifest

OutputContract deliverables have unique IDs, format, required flag and destination.
Optional template/schema paths are local assets. Markdown sections are requirements,
not proof of factual correctness; JSON outputs need a validating schema. A schema is
required for JSON deliverables under the proposed semantic validator. External binary
artifacts and methodology links can be designed later without overloading these fields.

Assignment identifies an agent, task packet, allowed profile, typed inputs and allowed
output contract. Optional `permissionRestrictions` can only narrow effective permissions.
Scope, acceptance criteria and lifecycle stay in the existing task packet.

Resolved manifests include repository revision/dirty state, selected configuration,
readiness, missing optional capabilities, provenance, and adapter report location.
The draft manifest includes a configuration snapshot; production must freeze every
transitive input and enforcement binding before claiming reproducibility.
A ready production manifest requires provenance entries for all resolved inputs; provenance in documentation fixtures is not executable evidence.

A capability registry now has a draft proposal schema, not an operational implementation. It must resolve
IDs to trusted tool bindings (argument arrays, never constructed shell strings), skill
paths/version hashes, MCP connection/tool catalogs, plugin components and installation
state. Credentials reference an environment/secret store. Registry installation and
authorization are separate from discovery. No example grants ambient MCP access.

## Extended contracts

[Additional contracts](additional-contracts.md) define invocation and modes, context,
registries/environment, scoped actions, result handoff, run records, lifecycle recovery,
evaluation and migration. Agent now requires references to those policy contracts.
OutputContract requires a result schema; permissions include a default-deny action policy.
