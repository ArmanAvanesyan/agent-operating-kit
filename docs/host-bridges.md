# Host execution bridges

This guide describes the current shared AOK lifecycle for prepared host
execution. The Go CLI exposes the commands below, but this does not establish
live host qualification, provider authentication, or runtime enforcement.

## Operator lifecycle

The shared interface is:

```text
aok host inspect --host codex|claude-code --policy FILE --out NEW [--json]
aok host prepare --assignment FILE --root DIR --policy FILE --out NEW [--json]
aok host run --prepared BUNDLE --policy FILE --out NEW [--json]
aok host resume --attempt BUNDLE --prepared BUNDLE --policy FILE --out NEW [--json]
aok host replay --manifest FILE --out NEW [--json]
aok host validate BUNDLE [--json]
aok host tasks plan --attempt BUNDLE --plan FILE --out NEW [--json]
aok host qualify --host codex|claude-code --policy FILE --out NEW [--json]
```

`inspect` reports the locally detected executable and version along with
its declared capability surface. It is discovery, not permission to execute.
`prepare` binds an assignment to an immutable corpus snapshot, selected
model evidence, host policy, and output contract. Review the prepared artifact
and its diagnostics before authorizing a run.

`run` is an explicit, policy-gated host invocation. It requires signed,
unexpired operator control evidence binding the exact resolved permissions,
workspace, host version, executable identity, tools, environment, settings,
configuration, and asset hashes. Without that evidence the attempt is blocked
before the model runner starts. A host response is recorded as an observation
and validated against the prepared output schema. It does
not accept the assignment, alter AOK lifecycle state, or grant additional
permissions. `resume` continues only a recorded interrupted native session and
checks its identity against the previous receipt. Failed or ambiguous attempts
require reconciliation and cannot be resumed automatically. `replay` and
`validate` operate on captured local evidence; they do not contact a model
provider.

`tasks plan` projects AOK task steps into a reviewable plan. When policy enables
native task tools, the adapter may record tool calls and native IDs returned by
those calls. These observations do not bind or update AOK task state. TaskList
or TaskGet results must remain bounded to the IDs needed for the evidence
record; local task files are not emulated or used as a substitute for host
tools.

`qualify` runs adapter compatibility checks. Fixture tests do not establish
provider login, billing, live model behavior, host-version compatibility, or
runtime policy enforcement. Record each of those gates separately from fixture
results.

## Codex write scope

The pinned Codex 0.162.1 bridge keeps `read-only` / `readOnly` for policies
without filesystem writes. For supported, concrete write paths inside the
canonical approved workspace, it can request `workspace-write` at thread start
or resume and `workspaceWrite` at turn start. The turn names only that workspace
as a writable root, disables network access, and excludes `/tmp` and `$TMPDIR`
from additional writable roots. Approval policy remains `never`. This change
neither permits automatic retries/resume nor changes runtime admission.

Native workspace-write can write more than the individual file requested by
pilot issue #1. It is not single-file enforcement. Before a write transport
starts, the bridge requires the existing fresh, trusted filesystem, network
and ambient-configuration proofs bound to the exact policy. The filesystem
proof must additionally include the check
`codex-workspace-write-exact-permissions/v1`. That check attests externally
measured enforcement of the exact resolved permissions with this write
transport, including denial of other writes; it is not established by adding a
string to a policy. Operators must qualify actual OS enforcement and credential
isolation independently. Generic or read-only-era proofs cannot qualify this
write route. Controls are checked again before sending a turn.

The bridge rejects unsupported write expressions and paths outside the
workspace before starting the inference transport. Canonical path checks are
not a sandbox and do not solve hostile concurrent filesystem replacement.
Protected workspace ownership and the qualified external enforcement boundary
remain prerequisites. Task text cannot choose a sandbox or authorize writes.

The [official AppServer documentation](https://learn.chatgpt.com/docs/app-server)
describes thread and turn sandbox controls. AOK uses only fields present in its
checked-in 0.162.1 schemas; newer documentation does not extend that pinned
protocol. Fixture conformance establishes request construction and rejection
behavior only. Codex/Claude live qualification, provider read/write qualification,
capacity, independent verification/acceptance, the first P9 pilot and production
activation remain separate blocked or pending gates.

## Claude Code bridge

The current Claude adapter implementation targets Claude Code **2.1.303**. It
uses print mode and newline-delimited `stream-json`, selects the model exactly
as prepared, supplies a generated UUID for a new session, and resumes only a
receipt's validated session UUID. It sends one user message over stdin and
requests structured output using the prepared JSON Schema.

The launch uses an empty setting-source list, explicit settings with hooks and
credential helpers removed, an empty strict MCP configuration, a tool
allowlist, and `default` permission mode. API credential environment variables
are rejected when the policy selects native Claude login. The adapter records
only allowlisted event fields, validated structured output, observed tool
names, usage counters, and native task IDs returned by successful Task tool
results. Error text and unrelated session content are not persisted in event
data.

Before inference, host preflight must inspect `claude auth status --json` and
require an OAuth login method. A Claude initialization event that identifies
an API key or API key helper as its authentication source is rejected. This
check qualifies the local login method; it does not validate billing or live
model behavior.

Claude only accepts an unknown or Anthropic provider route. It rejects
configuration overrides for reasoning budgets, service tiers, deployments,
model revisions, generation parameters, and other reasoning modes. When the
prepared configuration explicitly selects effort reasoning, the bridge passes
the exact prepared effort value with `--effort`.

Policy settings cannot install hooks or credential helpers. A Claude sandbox
setting is accepted only with exactly `enabled: true` and
`allowUnsandboxedCommands: false`; the setting itself does not prove that the
host enforced filesystem or network controls. Those controls still require
separate, scoped evidence.

Claude task calls are observations. A TaskCreate result may provide a native
ID; TaskGet, TaskList, and TaskUpdate results contribute only exact IDs that
the host returned. When a result can be tied to one prepared step by a unique
subject or exact previously observed native ID, the event records that binding
as evidence only. Missing or unrelated readbacks cannot update an AOK step. A
delete or clear result does not imply that an AOK task was
deleted, cleared, completed, or accepted. A successful Claude turn is still
only a host outcome, and missing terminal output remains `unknown`.

Required capability references currently fail closed because no verified
native binding is available. Optional references are not installed or
emulated. The current operator has not supplied the signed control proofs
needed to pass the run gate. The live host and runtime-enforcement gates are
blocked, and no qualified status is claimed. Fixture tests and locally
observed executable versions are not substitutes for those proofs.

## Remaining qualification gates

Before using a bridge for live work, operators still need evidence for the
installed executable identity and version, login mode and billing path,
allowed environment, workspace containment, host-version behavior, structured
output compatibility, cancellation and process-exit handling, and runtime
policy enforcement. Keep those checks distinct from offline fixture
qualification. No live model invocation is implied by this guide.

## Operator trust boundary

The external policy file and its issuer public keys are trusted operator inputs,
like catalog and usage policies. Protect them outside task-controlled content.
A signature authenticates a proof relative to those configured keys; AOK does
not authenticate the policy owner or independently certify the enforcement
implementation. Replacing the policy is a change of authority, not a supported
way to qualify a missing control. Neither this adapter nor its test fixtures
issues production enforcement proofs.

Catalog-aware preparation binds the loaded catalog and external policy digests;
a changed catalog blocks execution. Unknown catalog compatibility does not
become support. Required capability references currently remain blocked until
a native binding can be verified. Cost, turn-count and tool-call limits that
the transport cannot enforce also block preparation. Profile timeouts narrow
the operator timeout; neither can extend the signed policy scope.
