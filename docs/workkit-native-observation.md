# WorkKit P6: native projections, bindings and scoped observation

P6 is an observation-first implementation from P5 merge `af456c4` (PR #91).
Standalone `workkit`, `aok work` and `aok workkit` use the same SDK. These commands
never launch models, create/resume sessions, delegate agents, modify native task
files or send provider mutations. `native apply` is blocked before any credential
resolution, transport or journal admission.

## Commands and evidence

```text
native hosts
native plan --assignment FILE --profile FILE --mapping FILE --handle FILE --as-of RFC3339 --out NEW
native plan ... --operation-request FILE
native bind --assignment FILE --profile FILE --mapping FILE --handle FILE --as-of RFC3339 --out NEW
native validate-binding --assignment FILE --profile FILE --mapping FILE --handle FILE --as-of RFC3339
native observe|reconcile --assignment FILE --profile FILE --mapping FILE --handle FILE --evidence FILE --as-of RFC3339 --out NEW
native inspect --bundle BUNDLE --as-of RFC3339
native observe --assignment FILE --profile FILE --mapping FILE --handle FILE --policy FILE --enrollment FILE --out NEW
native apply
```

All accept `--json`, `--timeout` (at most five minutes) and `--non-interactive`.
Exit codes remain 0 success, 1 operation failure, 2 misuse/schema, 3 blocked,
4 conflict/stale, 5 uncertain effects and 6 partial. These read-only adapters do
not send effects; a failed admitted read preserves an incomplete bundle. A
read or response-schema failure may produce partial evidence, never an inferred
empty native task list. Output reuse is rejected. The umbrella preserves the
complete domain result and original code. Authority, execution, migration and
production flags remain false.

`workkit-native/v1` records are closed operational wrappers. Existing
`workkit/v1alpha1` records, profile modes and legacy command effects are unchanged.
An Assignment contains explicit step keys, descriptions and dependencies. A
SessionHandle binds exact assignment/profile/mapping bytes, host/version/protocol,
origin/deployment, workspace, principal, owner and existing session. Native-only
handles contain no runtime attempt/fence; managed handles require both. Handles
have an explicit observation date and at most five-minute validity. No title,
email, display key or model output is used for identity matching.

`native plan` emits a non-executing projection. Optional NativeOperationRequest
records emit stable, blocked operation drafts; creation contains no invented task
ID. These are protocol intent fixtures, with no native sender or admission purpose.
`bind` emits a candidate only and changes no selected workspace binding. Offline
validation establishes historical consistency, never current authority. Reports
bind the normalizer's adapter version, implementation digest, input bytes and
captured assets. Inspection reproduces normalized content from its assets.

## API and operation matrix

| Host | Implemented behavior | Mutation/live boundary |
|---|---|---|
| Codex | Projection; import owner-scoped `turn/plan/updated` events | No direct task CRUD established; no App Server or thread is started |
| Claude Code | Projection; correlate captured task tool calls with successful results | Task tools are model-facing; no inference is used to invoke them |
| OpenCode | Projection; imported todos; injected existing-session GET reader | No todo write/task CRUD or session lifecycle calls |
| Cursor | Projection | Observation and mutation unsupported |
| Antigravity | Projection | Observation and mutation unsupported |

Official references: [Codex App Server](https://learn.chatgpt.com/docs/app-server),
[Claude task tracking](https://code.claude.com/docs/en/agent-sdk/todo-tracking),
and [OpenCode server](https://opencode.ai/docs/server/). The
[API inventory](research/workkit-p6-native-api-evidence.json) records discovery,
source pins and limitations. Installation/version discovery does not establish
protocol compatibility, task availability, account access or live qualification.

Imported evidence must retain original owner/session/principal/workspace/assignment
scope and dates on every event. Duplicate identical events are idempotent;
conflicting duplicates and ambiguous ordering are invalid. Missing, failed or
unmatched Claude results remain partial. Create IDs come only from successful
paired results. Unknown states remain unknown. Codex plan steps, OpenCode todos,
background completion and native tasks are different categories. Ordinal todos
never become synthetic legacy task IDs. `complete` records supplied coverage,
not independent trust or current authorization.

## Protected OpenCode reader

The standard binaries install no qualifying native reader. Enrollment JSON cannot
install trust, transport, credentials or a runtime callback. A compiled embedding
uses `Service.WithNativeReadBackend` and `OpenCodeReadTransport` with protected
Trust, Credentials and, for managed sessions, Attempt callbacks. Trust verifies
independent signature custody/revocation, purpose, exact route, current scope and
session qualification. Attempt verifies the current runtime attempt and fence;
its absence blocks managed reads. No fixture callback qualifies a live route.

Policy, handle and enrollment bind exact bytes, implementation, schema, principal,
version, session and five-minute windows. Credential references are opaque; no
login, environment fallback or credential switching occurs. Authority is rechecked
after credential resolution and before each request. The reader performs only:

1. `GET /session/{encoded-id}?directory={workspace}` with exact ID/directory readback.
2. `GET /session/{encoded-id}/todo?directory={workspace}`.

The session already exists. No enumeration, health check, config/provider access,
prompt, shell, MCP command or task mutation endpoint is called. Origins are fixed;
redirects and URL credentials are rejected. HTTP is limited to loopback; remote
origins require HTTPS. Each request is bounded to 30 seconds, each response to
4 MiB; two requests are within the 100-request/64-MiB command ceiling. Response
credential echoes are rejected, including bare bearer tokens and decoded Basic
components. Raw errors and rejected private responses are not persisted.

Safe scoped session and todo bytes are retained as evidence events. Schema/identity
failures retain sanitized dispositions and partial coverage. Cancellation uses a
separate two-second cleanup context solely to preserve private evidence. It does
not retry transport or call authority again. A schema pin describes the supported
adapter shape; independent installed-protocol conformance remains necessary.

## Progress drafts and recovery

```text
native plan --progress-from NATIVE-BUNDLE --link FILE \
  --snapshot P2-BUNDLE --observation P4-BUNDLE --desired FILE --mapping FILE \
  --binding FILE [--binding-root ROOT] --capability FILE --policy FILE \
  --as-of RFC3339 --out NEW
```

NativeProgressLink pins the complete fresh observation, structural session scope,
exact P5 mapping/binding bytes and full provider work-item identity. Explicit
entry-index/state/value rules must reproduce desired fields exactly. Rules refer
to ordinals only within that immutable observation. No fuzzy native/work-item
correlation is performed. Only comment/update drafts are delegated to the existing
P5 planner; its ownership, ordinary-field, binding, capability, CAS and acceptance
restrictions remain intact. Original native evidence is retained in the P5 draft
and revalidated by PrepareOperation. Native completion never supplies acceptance,
workflow readiness or permission to close work.

No native journal or sender namespace is activated. P5's authoritative create
intent, one-send and ambiguity fixtures provide protocol-only recovery coverage.
Lost create responses cannot authorize retries; `native reconcile` only reads or
imports observations. Neither absence nor native completion settles a remote
write or a runtime obligation.

## Delivery gates

- P6 observation core: claim delivered only with passing source-bound acceptance.
- Native mutation capability: blocked pending a proved inference-free API,
  protected admission and operation-specific qualification; remains within P6.
- OpenCode live observation: pending per version/protocol/auth/session/platform.
- Codex/Claude imports: parser conformance only; owner evidence qualified separately.
- P5 provider operations: live qualification pending separately per exact route.
- Migration activation: blocked pending preservation, quiescence, unresolved effects,
  external retention and explicit namespace transfer.
- Production activation: unauthorized.

Fixture samples are under `kits/workkit/testdata/native`; they are synthetic and
cannot qualify any session. Rebuild/check contracts with
`go run ./scripts/workkit/native-schemas [-check]` and fixtures with
`go run ./scripts/workkit/native-fixtures [-check]`. Historical P0–P5 evidence and
all legacy setup behavior remain unchanged.

The [WK-060–066 delivery checklist](implementation/workkit-phase-6-delivery-evidence.md)
identifies the source-bound acceptance artifacts and independent gate statuses.
