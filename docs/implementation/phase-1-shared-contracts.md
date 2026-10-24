# Phase 1: shared bindings, execution records and connector protocol

Status: static implementation delivered, 2026-10-23. This phase implements no
runtime execution. It refines P1 of [the end-to-end plan](end-to-end-workflows.md).
Two independent `gpt-6-luna` medium-reasoning reviews informed the contract and
connector design. Their work was read-only.

## Goal and fixed scope

Deliver six closed, typed operational record kinds, their cross-record validator,
a Go-only offline validation command, and an in-memory connector conformance suite.
An implementer must be able to describe a work item, its host attempts, a supplied
run snapshot, and a proposed remote update without granting authority or performing it.

Included kinds: WorkItemBinding, HostTaskBinding, RunRecord,
ConnectorCapability, SyncOperation and SyncReceipt.

Excluded: live GitHub/Azure/Linear/Jira connectors; Claude/Codex invocation;
workflow dispatch; persistent databases, leases or schedulers; event ingestion;
UsageEvent, QuotaSnapshot, Reservation, SelectionDecision; model selection;
remote provisioning. Those remain P2-P8. Tests may simulate remote updates only
inside isolated in-memory fixtures. No live writes, PRs or deployment are implied.

Existing packet/project files and Python CLI behavior remain unchanged. Record
schemas use the existing `aok/v1alpha1` envelope and exact `{kind,id,version}` pins.
Newer binaries accept the additive kinds; older binaries reject them as unsupported.
There is no new API version or silent migration in P1.

## P0 decisions resolved for this slice

- AOK owns static record validation. An external runtime later owns persistence,
  scheduling, leases, authorization decisions and recovery.
- Records are immutable exported snapshots. A changed snapshot uses a new exact
  metadata version; it is not an instruction to mutate a running task. Runtime
  identifiers and snapshot metadata versions are separate concepts.
- Authorization is an external opaque reference plus declared scope and operation
  digest. Its presence is checked for consistency, never treated as a grant.
  Workflow CLI reports include `authorizationGranted: false` and `executionReady: false`.
- P1 run disposition describes a supplied observation, not accepted goal success.
  Allowed values: `pending`, `running`, `blocked`, `cancelled`, `exhausted`,
  `failed`, `completed`. QA acceptance/delivery remain separate evidence gates.
- Strict unsupported-domain handling is mandatory. A parser accepting a new kind
  cannot cause an older validator path to silently skip its semantics.
- No live runtime version pin is required for static records; a supplied runtime
  identity/version is an observation. P7 must verify a real pinned interface.

## Contract design

All six schemas reject unknown fields and use the current parser limits, numeric
rules, duplicate-key rejection and offline embedded-schema loading. Required IDs
and references are nonempty. Hashes are lowercase 64-character SHA-256 hex.
Times use RFC3339 and are normalized for comparisons; no current wall clock is
consulted. Unknown external values are explicitly absent, not invented defaults.

Shared value types:

- `RemoteIdentity`: provider (`github`, `azure-devops`, `linear`, `jira`),
  tenant ID, project/container ID, immutable remote ID and item kind. Each component
  is opaque and compared exactly. URLs/display keys are labels. A tenant scopes
  the service deployment/workspace; project scopes the provider's repository,
  project or team. Never case-fold or normalize opaque IDs globally.
- `AssetRef`: corpus-relative path plus SHA-256. Resolve relative to the owning
  document, remain within the corpus, and reject symlinks, special files and unsafe
  paths. Packet Markdown and captured provider JSON belong under `assets/`.
- `PlanStepRef`: exact PlanDefinition reference and exact step ID.
- `ExternalAuthorityRef`: opaque external reference, RemoteIdentity scope,
  permitted operation and operation digest. It is an inert declaration.
- `EvidenceRef`: AssetRef plus purpose and observation time. No arbitrary embedded
  transcript, credentials, executable command or fetched URL content.

| Kind | Required content | Semantic checks |
|---|---|---|
| WorkItemBinding | RemoteIdentity; optional display key/URL; packet AssetRef; nonempty observed source revision; mapping version; field mappings | No conflicting bindings for the same remote identity; packet hash verified; field-map keys unique; unmapped fields are not claimed supported |
| HostTaskBinding | Exact work-item and AgentAssignment refs; host name/version; session ID; optional thread/native-task IDs; attempt ID; assignment document SHA-256; optional PlanStepRef | Refs resolve; assignment bytes match; plan step exists and selects that assignment; one scoped host attempt cannot bind different items/assignments |
| RunRecord | HostTaskBinding ref; runtime name/version/run ID; attempt ID; input-revision string; ExecutionProfile ref; assignment SHA-256; optional lease/fence pair; policy refs; checkpoint/output EvidenceRefs; observed time; disposition | Attempt, assignment and profile match binding/assignment; lease/fence are both present or both absent; fence is nonnegative bounded integer; refs/assets resolve; observation alone never establishes accepted completion |
| ConnectorCapability | Provider; adapter ID/version; supported scope; per-operation support and concurrency/idempotency declarations | No duplicate operations; unsupported operations cannot claim stronger guarantees; required conditional write support cannot be satisfied by readback-only behavior |
| SyncOperation | WorkItemBinding and ConnectorCapability refs; logical operation ID; operation kind; expected revision; typed desired change; declared ExternalAuthorityRef; canonical operation digest | Scope/provider agree; operation supported; declaration covers exact digest/action/scope; expected revision matches captured binding; ambiguous or unsupported field changes rejected |
| SyncReceipt | SyncOperation ref; logical operation ID/digest; receipt ID; observed time; disposition; dispatch/readback EvidenceRefs as applicable | Exact correlation; unique receipt identity; applied requires matching readback evidence; unknown does not become applied; receipt never authorizes or accepts work |

Field mapping entries declare canonical field, provider field identifier and owner
(`tracker`, `aok`, `shared`). They contain no executable transformation expressions.
P1 validates their structure/uniqueness; provider-specific mapping truth is P5.

Operation kinds are `update-fields`, `transition`, `comment` and `link`.
Each has a closed payload: a list of named field/value changes; transition ID;
comment AssetRef; or relation type plus RemoteIdentity. Values are JSON scalar or
arrays of scalars in P1; complex provider rich text remains in captured assets.
Creation/deletion/provisioning are intentionally unsupported. No arbitrary JSON
Patch, shell command or provider request body is accepted as an execution plan.

Digest calculation uses a typed canonical serialization of binding reference,
remote identity, capability reference, operation kind, expected revision and desired
change. Sort field changes by provider field ID; reject duplicates. Preserve array
value order. Exclude authority declaration, receipt and timestamps from the digest.
Hash the serialized bytes with SHA-256; test fixtures pin those bytes and digests.

Receipt dispositions: `applied`, `not-applied`, `conflict`, `rejected`, `unknown`.
`applied` requires dispatch plus matching readback; `not-applied` requires readback;
`conflict` and `rejected` require diagnostic evidence; `unknown` may retain dispatch
only. These are checked claims, not independent certification of external state.

Identity rules: different tenants may reuse remote/display IDs without collision.
Within one identity, a corpus may contain multiple versions of the same binding
metadata ID; distinct binding IDs for the same identity are rejected. Each reference
selects an exact version. One work item may have many attempts. Host attempt identity
is `(host, sessionID, attemptID)`; identical snapshots may evolve by version, while
conflicting item/assignment bindings are invalid. Operation IDs are scoped to a
connector and tenant: replay with the same digest is the same logical operation;
the same scoped ID with a different digest is invalid. Exact duplicate resources
and receipt IDs are invalid in a corpus; transport replay deduplication is later work.

## Packages, validation and public interface

Keep types and schemas in `internal/contracts`. Introduce a pure semantic package
`internal/workflowcheck` accepting parsed resources, their source paths and a
safe asset resolver callback. It imports contracts only, performs no network or
subprocess calls, and returns existing structured diagnostics.

Wire that checker into `agentvalidate` after its normal index/reference checks.
This prevents cycles: workflowcheck does not import agentvalidate, planning, CLI,
connectors or evidence writers. Existing agent and planning entrypoints then
validate P1 semantics whenever they accept the new kinds. Keep planning-specific
graph checks in planning; workflowcheck checks its own pins and step/assignment
relationships, not goal eligibility. Do not extract or rewrite the entire loader.

Add this Go CLI surface:

```text
aok workflow validate <corpus-directory> [--json]
```

Its implementation composes the existing planning validation path (which already
includes agent/schema validation and now shared P1 checks). Require at least one
P1 record for this command; otherwise report `workflow.corpus.empty`. Do not
claim full decision-result/optimization evaluation from this command.

JSON output: status (`valid` or `invalid`), resource counts by kind,
`authorizationGranted: false`, `executionReady: false`, and sorted diagnostics.
Text output summarizes counts and diagnostics with the same meaning. Exit codes:
0 valid, 1 invalid or unreadable corpus, 2 command misuse. Unknown options,
extra positional arguments and duplicate flags are misuse. Help performs no load.
P1 has no `apply`, `start`, `fetch`, `--out`, or fixture execution CLI command.
The validator does not write files or create `.aok` state.

Reuse existing corpus asset/file checks and hash exact bytes before semantic
comparison. Do not silently accept path/checksum declarations. No endpoint is
contacted when validating remote labels or external authorization references.

## Connector protocol and test-only implementation

Create `internal/connector` with small interfaces, using `context.Context` for
future I/O and cancellation. Core methods return typed results plus structured
diagnostics; errors represent transport/program failures. Domain results have
explicit supported/unsupported/conflict/unknown dispositions.

```text
Reader:   Describe(), Discover(ctx, scope), Read(ctx, identity)
Mapper:   Normalize(capturedItem), PlanUpdate(current, desired, capability)
Writer:   Apply(ctx, validatedOperation, authorizationContext)
          Reconcile(ctx, operation, receipt)
```

Describe returns ConnectorCapability. Read returns identity, opaque revision and
captured JSON bytes; Normalize returns portable fields with explicit unmapped-field
diagnostics. PlanUpdate is pure. An engine validates operation/capability/scope/
digest before Apply; a Writer also checks the independent authorization context.
A declaration inside SyncOperation cannot construct an authorization context.

P1 includes no production Writer, authorization resolver or provider registry.
Only `_test.go` in-memory implementations supply a test authorization context and
simulate reads, conditional updates, conflicts, readback and ambiguity. Each test
owns its state; no mutable package globals or `gh` substitution. Keep existing
GitHub task commands unchanged until P5. No API compatibility claims follow from
passing the fake connector suite.

## Implementation sequence and cheaper-agent work allocation

Use `gpt-6-luna` medium for bounded implementation tasks and a separate Luna high
reviewer. The primary agent integrates and validates the exact combined result.
Do not let workers edit the same registry/CLI files concurrently.

| Step | Work package | Dependency | Acceptance |
|---|---|---|---|
| 1 | Record the slice ADR and six example documents; lock names, digest format, authority and identity rules | None | Examples express one item, two attempts and a proposed update without runtime execution |
| 2 | Schemas, typed decoding, embedded registration and exact fixture coverage | Step 1 | All six parse/decode losslessly; closed-schema and version negatives pass |
| 3 | Pure workflow semantic checker and safe asset integration | Step 2 | Cross-record identity/ref/hash/conflict tests pass; no silent skips in existing validators |
| 4 | Connector interfaces and in-memory conformance tests | Step 2; finish against Step 3 | Read/normalize/plan/apply/reconcile simulation proves gates and ambiguity handling |
| 5 | Offline workflow CLI, operator examples and compatibility documentation | Steps 3-4 | Built binary validates fixtures without Python, gh, credentials or network |
| 6 | Independent review and integrated regression | Steps 1-5 | Reviewer findings resolved; exact final change passes required checks |

After Step 2, one worker owns workflowcheck/tests and another connector/interfaces
and conformance tests. Primary owns shared agentvalidate/CLI wiring. The reviewer
checks authority boundaries, validation entrypoints and identity/digest behavior
after integration. No release publication or external tracker operations in P1.

## Test and acceptance matrix

1. Parsing: all six positive fixtures, unknown fields/kinds/versions, duplicate
   JSON keys, malformed IDs/hashes, exact numeric bounds and typed round trips.
2. References: missing/wrong-kind/wrong-version refs, assignment hash mismatch,
   nonexistent plan step, mismatched step assignment, packet/evidence hash drift.
3. Identity: same display key across tenants is valid; conflicting same-identity
   binding is invalid; one item/two attempts valid; one host attempt/two items invalid.
4. Operations: unsupported action, foreign scope, changed expected revision,
   authorization declaration mismatch and modified digest fail. Same logical ID
   and digest is a replay; same ID/different digest conflicts.
5. Receipts: applied without matching readback fails; ambiguous dispatch remains
   unknown; duplicate receipt IDs fail; mismatched operation/digest fails.
6. Files: absolute/traversal/symlink/special-file assets fail; no process/network
   access; validator leaves input tree unchanged and writes only stdout/stderr.
7. Entry points: semantically invalid P1 documents cannot produce valid reports
   through agent, planning or workflow validation. Valid old corpora retain prior
   results; workflow command clearly rejects an entirely non-P1 corpus.
8. Connector conformance: read creates nothing; cancelled context prevents fake
   dispatch; stale precondition conflicts; duplicate apply has one logical effect;
   successful write with lost response reconciles from readback without blind retry.
9. CLI: help/misuse/exit codes, deterministic diagnostic ordering, JSON/text
   equivalence; compiled executable with empty PATH and no credentials.

Run focused contracts/workflowcheck/connector/CLI tests during each step. Final
gate: `go test ./...`, Go build, kit validation, Python compilation and existing
task/project/work-system regression checks, plus affected distribution tests and
`git diff --check`. No new live provider test is needed for this offline slice.

P1 is done only when the six kinds, shared semantics, offline command, examples
and conformance suite all pass independent review. Report precisely: static
workflow contract validation is implemented; execution, authoritative permission
checks, usage accounting and live connectors remain later phases.

## Delivery evidence

Implemented with three `gpt-6-luna` medium workers (contracts, semantics and
connector conformance), primary integration and a separate `gpt-6-luna` high
reviewer. Review findings were resolved in the working tree, including strict
operation payloads, defensive copies, exact receipt references, stable identity
tuples, policy pins and readback validation. The operator guide is
[workflow contracts](../workflow-contracts.md).

Acceptance checks: full Go package suite and build; all 267 Python regression
tests (four skips); kit validation; Python entrypoint compilation; release-build
tests; and whitespace/link checks. The shipped-binary test uses an allow-listed
environment and empty PATH, checks all three validation entrypoints, and proves
a workflow semantic defect cannot pass through agent/planning validation. CI
also exercises the positive workflow corpus with no Python on PATH.

The sandbox initially blocked the standard Go cache and three pre-existing
Python terminal/confinement checks. The authorized runs outside that restriction
passed; these failures were not treated as passing or patched out.
