# Composition, defaults and persistence

These are proposed resolver rules. JSON Schema only checks part of this contract.

## Resolution sequence

1. Parse canonical JSON or safe YAML; reject duplicate keys, unsafe tags and unknown fields.
2. Resolve local IDs, version constraints, paths and the selected adapter/interface.
3. Validate agent, profile, role, skill, capability, template and assignment references.
4. Fill only declared defaults; validate assignment inputs against parameter definitions.
5. Compose instructions and calculate permission/limit intersections.
6. Discover environment, model settings and native-format compatibility without inference.
7. Produce a resolved manifest and AdapterReport; block required gaps and policy losses.
8. Export a reviewable native definition/config/launch plan; installation and execution are
   separate steps under existing user authorization and host policy.

## Defaults and overrides

| Setting | Initial rule |
|---|---|
| Profile | Agent default, unless assignment chooses an allowed profile |
| Model/effort | From the chosen profile; no arbitrary assignment model override |
| Missing generation setting | Host/provider default, recorded as unspecified |
| Input parameter | Assignment value, then declared agent default; otherwise missing |
| Output contract | Agent default or assignment-selected allowed contract |
| Output parameters | Agent defaults, then assignment values; validate against template declarations |
| Capabilities | Agent declared set plus resolved dependencies; permission check always applies |
| Limits | Tightest applicable project/profile/executor ceiling |
| Permissions | Intersection of platform/org/user/project, agent and assignment restrictions |
| Fallback | None unless an ordered approved fallback profile is declared |
| Delegation | Explicitly disabled in initial examples |

Output parameter declarations belong to the template registry in this draft; its
semantic validator is proposed. This proposal does not pretend the output schema alone
validates arbitrary parameter names. No implicit permission widening, fallback, plugin
installation, MCP connection, network request or credential lookup occurs on parse.

## Instruction assembly and conflicts

Preserve the host's real hierarchy and applicable platform/user/project instructions.
Within AOK-controlled content assemble identity, primary role, supporting roles,
ordered instruction files, activated skills, assignment context, output contract.
This is content assembly, not a new universal authority hierarchy. Native inherited
instructions must be discovered/reported; import must not assume only the visible file
influences a session. Do not convert untrusted repository text into privileged directives.

Union references by stable typed ID; identical entries deduplicate. Incompatible versions,
required flags, tool subsets or scalar settings fail with source locations rather than
last-write-wins. Required dependency wins over optional only when the remaining fields
agree. Distinct constraints intersect. Conflicting role responsibilities require an
explicit agent revision; prose conflicts cannot all be detected automatically.
Unknown instruction conflicts remain a documented review limitation.

## Model combination rules

- Variant effort and explicit effort must agree after adapter normalization.
- Effort plus token budget needs an explicitly verified compatibility mapping.
- Disabled/provider-default reasoning cannot carry explicit effort/budget values.
- No sampling field is silently dropped for a reasoning-only model.
- Inherited model/provider defaults must be discoverable or marked unresolved.
- A fallback is a fresh resolution with the same output, permission and capability
  requirements. Never switch providers on authentication/policy refusal automatically.
- Availability expiry in research is advisory input to discovery, not hardcoded model
  knowledge. Recheck availability; record expiry warnings and block unavailable selectors.

## Permission and path rules

Resolve roots and symlinks before checking containment. Reject destination traversal,
absolute destination escapes, unresolved variables, duplicate deliverable destinations
and overwrites outside an explicit installation plan. Substitutions are data, not code.
Network allowlists and filesystem restrictions require actual enforcement; merely putting
rules in a prompt is advisory. A broad shell can bypass narrow tool permissions unless
constrained by the executor. Stop resolution when required controls are unenforceable.

A plugin's dependency contribution cannot authorize its tools. MCP allowlists are
connection-and-tool scoped; reconnect/discovery changes invalidate the relevant manifest.
Delegated agents inherit no greater permissions, data access or budget than their parent.
Human approval roles and gates remain human even when an agent produces evidence.

## State and reproducibility

Definitions and templates are versioned desired state. Native imports are snapshots,
not a second automatically synchronized authority. Generated manifests/reports are
per-run immutable evidence; record source hashes, dirty files and host/adapter versions.
Editing a definition requires re-resolution for the next run. Changing a running model
follows host behavior and produces a recorded configuration boundary, never an invisible
manifest mutation. Host-stored model/effort preferences must not be changed by default.

Generated output installation should be idempotent, hash-tracked and atomic where
possible. Preserve user files; preview conflicts, back up intentional replacements and
record a rollback manifest. Native edits after export are detected by hashes, then
explicitly re-imported or reconciled. Runtime events, memory and usage stay in the
executor/runtime. AOK stores references or exports only when explicitly requested.

## Additional contract resolution (proposed extension)

Resolve InvocationContract, ContextPolicy, LifecyclePolicy, EvaluationContract and
MigrationContract before launch. Validate invocation input schema and mode, context
requirements, transitive registry dependencies, action-policy intersections and output
result schema. Pin all referenced versions/hashes in the snapshot. Unsupported native
mode, policy, required context or lifecycle controls block readiness.

Action policy defaults to deny. A more specific allow never defeats a higher-level deny.
Unknown native permissions are not treated as equivalent to AOK action rules. Context
limits cannot be satisfied by dropping required files. Selection triggers do not imply
automatic delegation authorization. Evaluation status not-run cannot enable automatic
profile routing. A completed AgentResult with missing/invalid outputs is rejected.
