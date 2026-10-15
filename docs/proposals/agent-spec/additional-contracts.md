# Invocation, context, execution evidence and evolution contracts

Status: **all contracts in this document are proposed**. Existing role/skill/task,
rendering, setup and readiness-helper capabilities are listed in the
[baseline table](README.md#existing-capabilities-versus-additions). Draft JSON schemas
under this proposal are documentation assets, not operational CLI support.

## Invocation and execution mode

InvocationContract declares positive selection triggers, exclusions, automatic-selection
policy, input schema, and positive/negative selection examples. Selection is not launch
authorization. The assignment's `inputs` must satisfy the referenced schema, parameter
definitions and project policy. Never choose an agent on description similarity alone
when exclusions or required inputs fail. Examples are evaluation fixtures, not a classifier.

Execution modes are interactive assistant, primary agent, subagent, batch worker and
reviewer. A reviewer is an operating constraint, not an independent approval authority.
Adapters validate the mode against the native host: unsupported modes block or require
an explicitly revised contract, rather than silently creating a primary agent. Mode
lives in InvocationContract; CLI/app/API transport remains in ExecutionProfile.

See [invocation schema](schemas/invocation-contract.schema.json),
[example](examples/invocation-contract.json) and
[input schema](examples/invocation-input.schema.json).

## Context policy

ContextPolicy specifies repository roots, required documents, conversation inheritance,
retrieval sources, input-token ceiling, overflow behavior, redaction and retention.
Missing required documents block readiness. Optional discovery material must not be
confused with required context. `assignment-only` supplies the task and explicitly
selected assets, not arbitrary prior conversation. `selected-history` requires an
explicit selection with provenance. Tools remain limited by capability and permission
contracts regardless of what the context contains.

Untrusted source content is data, never elevated instructions. Prompt assembly must
respect the native host's actual hierarchy and disclose inherited instructions that
cannot be removed. The token limit must account for mandatory native context; if the
host cannot report or bound it, required budget enforcement is unresolved. Selective
retrieval preserves required documents or blocks; it must not silently truncate them.
Retention applies to the executor's controllable storage and is not a provider privacy
guarantee. Unknown provider retention must be reported separately.

See [context schema](schemas/context-policy.schema.json) and [example](examples/context-policy.json).

## Capability and environment registries

CapabilityRegistry binds typed IDs to native tools, trusted command executables and
argument arrays, skill files, MCP connection/tool references and plugin components.
Dependencies form an acyclic graph. Binding type must agree with entry type; dependency
IDs, versions and credentials must resolve. Command arguments are ordered and may
repeat; do not shell-join them. Executable discovery uses trusted bindings and never
runs imported arbitrary code. `observed` installation state requires current evidence,
not a declaration. Availability is not authorization. Verification entries name required validators; template entries declare source and
parameter schema. Implementations are still not installed. The example plugin is fictional
and not selected by the agent; listing its components grants no access. Existing role
schemas remain the role source, not capability bindings. Binding schemas and paths still
need a production semantic validator.

EnvironmentProfile defines symbolic roots, required/optional requirements, discovery
probes, isolation, network policy, credential references and readiness checks. Its
network/isolation settings constrain the execution profile and cannot widen platform
policy. Roots are resolved using assignment parameters before path containment checks.
Probes are trusted registry references; parsing/discovery never installs tools, starts
MCP servers, changes user preferences or sends inference prompts. Starting external
connections is a separately authorized preparation action when discovery requires it.

See [registry schema](schemas/capability-registry.schema.json),
[registry example](examples/capability-registry.json),
[environment schema](schemas/environment-profile.schema.json) and
[environment example](examples/environment-profile.json).

## Action-level permissions

The shared permissions object now has `actions.default: deny` and scoped rules:
`action`, `resource`, `decision`, `enforcementOwner`. Example actions include
repository.read, artifact.write, github.comment, git.push, github.merge and release.publish.
The first two appear in the example; the others remain denied by default.
Resources must use canonical, adapter-validated identifiers: workspace roots for local
actions, repository/PR/environment identifiers for remote actions. Credentials do not
authorize actions. Role responsibilities and task text do not broaden this policy.

A decision is allow, require-authorization or deny. Deny wins; intersection across all
policy layers is mandatory. Require-authorization needs trusted user/org evidence scoped
to the concrete action/resource/run, and cannot be satisfied by repository prose or
another agent's claim. An allow is still constrained by higher-level authorization.
Host/native or executor enforcement must be verified. A broad shell or MCP endpoint
that bypasses the rule blocks readiness unless independently constrained.

## Output result envelope and handoff

AgentResult standardizes terminal handoff: status, summary, hashed deliverables,
evidence references, unresolved items, next actions and verification result.
OutputContract's `resultSchema` selects this schema. Required artifacts remain defined
by deliverables, not by an agent's completion statement.

`completed` requires passed verification, every required deliverable present and valid,
and no unresolved acceptance/policy blockers. Nonblocking follow-up suggestions may
remain under nextActions. `partial` preserves useful evidence but does not close the task.
`blocked`, `failed` and `cancelled` explain the incomplete outcome. Empty artifacts are
valid for a blocked run; they are insufficient for completing this example assignment.
The executor validates hashes, destinations, evidence and acceptance before accepting
completion. Valid JSON or section headings do not establish factual correctness.

See [result schema](schemas/result-envelope.schema.json) and [blocked example](examples/result-envelope.json).

## Complete resolution and run records

ResolvedAgentManifest now snapshots selected canonical documents and instruction text
with content hashes, capability/registry references and enforcement gaps. Production
resolution must additionally capture every transitive source: roles, skills, templates,
schemas, policies, native settings, host/adapter versions and actual effective controls.
Secrets remain references with redacted non-secret metadata. Hashes bind exact UTF-8 file
bytes; snapshots must reconstruct those bytes or include the original bytes/content
address to support comparison. The documentation checker compares parsed content and
source-file hashes; it does not certify a production immutable artifact store.

AgentRunRecord separates requested settings, the resolved manifest hash and observed
configuration. Unknown observations/usage use null, never zero or a guessed model.
Each attempt has an ID, parent reference, ordered events, timestamps, errors, checkpoint
reference and validated terminal result. The fixture flag is mandatory in the draft;
production records use false only when backed by real executor evidence. The saved
example uses true, no observed model, no usage and a blocked outcome.

The draft run schema supports active records with a null result and terminal records
with a result envelope. An active event stream belongs to the runtime and can be exported
as a terminal record at completion; no fabricated terminal result is required while a
run is running. State/result consistency beyond that shape needs semantic validation.
Task packet status/phase remain separate from run state. A run result cannot automatically
mark a task done or grant human approval.

See [manifest example](examples/resolved-manifest.json),
[run schema](schemas/run-record.schema.json) and [synthetic run](examples/run-record.json).

## Lifecycle, recovery and cancellation

LifecyclePolicy defines states, allowed transitions, retry classes, attempt cap, backoff,
checkpoint policy and cancellation behavior. Conditions are registry-implemented checks,
not evaluated prose. The example transition table is a proposed policy fixture.
Terminal states stay terminal; a retry or resumed terminal attempt creates a new attempt
linked to the previous run and preserves evidence. An in-place suspended resume is
allowed only with a compatible checkpoint and explicit policy support.

Before retry, reconcile side effects using idempotency keys/action receipts. Do not retry
unknown-result writes, authentication failures, policy denials or unsupported settings
blindly. A timeout maps to failed terminal output with a timeout error; cancellation maps
to cancelled. Partial is reserved for accepted partial work. Cancellation stops new actions,
records in-flight outcomes and does not promise reversal of external side effects.
Compensations are explicit, scoped, authorized actions, not automatic rollback guarantees.

Checkpoints bind manifest/config hashes, repository state, host version and external
resource receipts. Incompatible or tampered checkpoints block resume. The example disables
checkpoints; declaring resume conditions does not implement persistence.
See [lifecycle schema](schemas/lifecycle-policy.schema.json) and [example](examples/lifecycle-policy.json).

## Evaluation and model/profile suitability

EvaluationContract pins agent, candidate profiles, task fixtures and dataset revisions.
It defines metrics, units, direction, acceptance thresholds, repetitions and result evidence.
Null thresholds mean reporting-only metrics. Tests must include output validity, action
policy compliance, source-grounded accuracy and tool reliability before speed/cost ranking.
Record evaluator/version, environment, model/date, failures and repetitions in results.
The current draft results only point to evidence and status; production scoring needs a
versioned detailed result schema before automated routing is supported.

No candidate is certified by being listed. Both saved profiles are not-run. Use the same
fixtures and distinguish human scoring from deterministic validators. Account for retry
costs, provider availability and variance; publish limitations. Routing and fallback policy
must reference a reviewed evaluation version.
See [evaluation schema](schemas/evaluation-contract.schema.json) and [example](examples/evaluation-contract.json).

## Versioning, deprecation and migration

MigrationContract defines readable API versions, version/hash pinning, deprecations,
allowed transformations, dry-run/backup/re-resolution requirements and rollback scope.
Unknown versions block. Initial examples have no migrations or deprecations because
no prior operational Agent schema exists. Do not fabricate a migration from existing roles.

Schema/API version and individual document version are separate. Lock transitive references
by version and content hash at resolution; native format and adapter versions are pinned
separately. Upgrade previews show field changes and semantic losses. Never auto-convert
incompatible permissions or instruction authority. Restore definitions/generated files
from recorded backups on rollback; remote side effects and provider preferences require
separate reconciled actions. Existing runs retain their old manifests and evidence.
See [migration schema](schemas/migration-contract.schema.json) and [example](examples/migration-contract.json).
