# Phase 3 — Per-CLI usage and shared capacity accounting

Delivery update (2026-10-23): **P3-core delivered**. Live collector gates remain
pending per host/auth mode; runtime admission remains pending P6/P7. See the
[delivery evidence](phase-3-delivery-evidence.md) and
[operator guide](../usage-accounting.md). The detailed design below remains the
scope and acceptance reference; deployment/live gates are not implied by it.

## 1. Outcome, boundaries and dependencies

Provide a reproducible answer to: what was consumed, what allowance remains,
which accounts/windows constrain this configuration, when might capacity return,
and how much is already held by other work? P4 must receive this evidence before
it can make capacity-aware selection. Historical token totals alone do not answer it.

Recommended scope:

- Offline normalization, validation, replay, aggregation and capacity reports.
- Read-only Codex and Claude collectors first, gated by exact host version/auth
  mode and supported interfaces; separate usage and quota support declarations.
- An accounting-store interface and private local SQLite reference implementation
  for one runtime authority across local CLI processes. Runtime callers own the
  store, reservations and recovery. AOK supplies conformance rules and accounting
  code, with no daemon, scheduler, model launch or authentication service.
- Explicit support results for all eight hosts; the other six pass independent
  discovery gates before production collection is enabled.

P1 supplies exact run/attempt/work-item references and existing strict JSON,
asset and validation conventions. P2 supplies pinned catalog/policy hashes and
configuration IDs. Unresolved identities remain representable; their usage is
reported under unresolved attribution rather than attached to a guessed model.
P2's pending live probe gate does not block accounting.

P3-core requires neither P5 trackers nor P6 invocation. Live observation of
already-running authorized sessions is allowed only through a supported read-only
interface. Attaching to invocation streams that cannot be obtained independently
waits for P6. P7 must demonstrate runtime reservation/dispatch/recovery conformance;
an accounting lease is not a P1 RunRecord lease or execution authorization.
Distributed/multi-machine reservation authority remains outside the local MVP.

## 2. Packages and contracts

Use `internal/usage` for pure domain logic, `internal/usage/collectors/<host>` for
normalizers/read adapters, `internal/usage/store` for the store contract and local
reference implementation, and `internal/cli/usage.go` for offline reports. Keep
host transport, persistence and pure arithmetic separate.

Define closed embedded schemas under independently versioned `aok-usage/v1`.
Do not change P1 record meanings or P2's catalog format. Exported accounting
records are immutable snapshots; mutable indexes and reservations live only in
the runtime-owned store. Reject unknown versions, duplicate JSON keys, unsafe
paths, symlinks, special files and changed sanitized-evidence hashes. Use exact
decimal/rational arithmetic and explicit units, never float-based money totals.
Trust policy must be supplied through the trusted operator/runtime configuration
boundary. A policy embedded in an export or its hash establishes integrity only;
it cannot approve its own collector source or authorize collection/reservations.

| Record | Required content and invariant |
|---|---|
| SourcePolicy / CollectorDescriptor | Allowed source/adapter versions, auth modes, permitted metrics, authority, freshness and collection permissions; usage, quota, streaming and identity support are separate capabilities |
| AccountPoolBinding | Opaque provider/service deployment, pseudonymous principal/organization/project, auth/billing mode, pool ID and explicit host-to-pool mappings; credential fingerprints and display labels are not identity |
| UsageEvent | Source/version and event key; observed/received times; session/turn/request/call/parent IDs where supplied; exact P1 references when available; reported provider/model/configuration/catalog digest; metric, exact value/unit, counter semantics, counter epoch, source sequence and correction relationship |
| QuotaSnapshot | Pool, provider limit ID, window identity/generation, applicability, source/revision, observation/expiry, explicit completeness, precision, remaining/used/limit as supplied, reset semantics/time, optional coverage watermark |
| ReservationSnapshot | Runtime authority/epoch, reservation/idempotency ID, owner/attempt, configuration, ledger revision, snapshot/policy refs, all window demands, per-reservation fence, lease and state; unconfirmed charges remain distinct |
| CapacitySnapshot | Immutable ledger revision, as-of instant, source/policy/catalog hashes, per-constraint arithmetic and uncertainty, reservation totals, scope coverage and candidate capacity disposition |
| CollectionReport | Per-capability status, host/source version, coverage and gap intervals, freshness, omissions, sanitized diagnostics and next supported refresh action |

Observed source identity and requested configuration are separate fields. A host
that reroutes models must attribute actual usage to the reported route. Changing
an alias never changes historical usage identity. Unknown effort/revision remains
unknown; reports may aggregate by known dimensions while exposing those gaps.

### Consumption and aggregation

1. Keep input, output, cached input, cache creation, reasoning, requests, credits,
   subscription percentage points and currency distinct. Record whether a detail
   is a subset of another total; cached/reasoning tokens must not be added twice.
2. One canonical billable call contributes once. Parent/subagent rollups reference
   or allocate that call; inclusive parent totals are not added to child totals.
   Ambiguous overlap is reported as partial coverage, not silently summed.
3. Source namespace + stable event ID is the preferred dedupe key. Namespace by
   service/account/source stream and relevant session/call scope so unrelated
   CLI or pool IDs cannot collide. Record adapter/protocol version as provenance;
   upgrading an adapter and replaying the same physical events must not create a
   second billable contribution. Namespace migrations require explicit equivalence.
   A documented
   scoped sequence/call tuple may substitute. If neither exists, retain aggregate
   evidence with weak dedupe coverage; do not claim exactly-once accounting.
4. Same key and same canonical payload is a no-op. Conflicting payloads require
   an explicit source correction/revision; otherwise quarantine the conflict.
5. Incremental events sum once. Cumulative samples replace the corresponding
   series contribution: 10 then 16 totals 16. Retain revisions and recompute for
   corrections. An unexplained decrease marks a discontinuity; it is not a refund
   or a reset. Older observations cannot overwrite a newer revision.
6. Provider-reported cost and price-derived estimates are separate measures.
   Estimated cost pins dated P2 pricing/configuration evidence. Currency totals
   are never merged without explicit versioned conversion evidence.

Summaries group by host, provider, actual configuration/model, account pool,
project, work item, parent/child, run and attempt. Include unbound/unattributed
usage, interval coverage, excluded usage, estimate labels and conflict counts.
Pool quota is displayed once even when several host/model rows reference it.

## 3. Remaining capacity and reset rules

A constraint key is structural: authority/service + pool + provider limit ID +
metric/unit + scope/applicability + window kind. Its window instance carries the
provider generation or reset identity. Distinguish fixed, rolling, token-bucket,
concurrency, non-resetting and unknown constraints. Preserve all simultaneous
windows, shared family buckets, project/org budgets and rate limits; none is a
substitute for another. The context window is a task-compatibility constraint,
not an account quota balance.

Explicit bindings determine which constraints apply to a candidate configuration.
Do not merge pools because CLIs use the same model name, nor split a known shared
pool into a fresh allowance for each model. Unknown applicability/identity blocks
a claim of fully assessed capacity.

For each constraint in compatible units:

```text
effective observed headroom = max(0, reported remaining
                                   - confirmed-not-covered local charges
                                   - unresolved overlap charges
                                   - unspent active reservation amounts
                                   - policy safety margin)
```

Each local debit/reservation portion belongs to exactly one subtraction category
per constraint. Settling a reservation transfers its unconfirmed consumption and
releases proven unused allowance; it never subtracts both full reservation and
full actual charge. Expose raw overdrawn amount before clipping headroom to zero.

Snapshots replace a constraint's current observation; never sum them. A source
watermark or explicit request coverage can prove which charges are included in
provider remaining. Without coverage evidence, retain unresolved debits and label
the result conservative; timestamps alone cannot prove provider ingestion.
Reconciliation is per charge and per window. This may defer work indefinitely
until refreshed evidence or an explicit audited disposition resolves ambiguity.
Unbounded external consumption means no guarantee that provider allowance is
reserved: this ledger coordinates participating local callers only.

Missing fields are not zero or unlimited. Define full snapshot versus sparse
update semantics per pinned adapter. Omitted fields in a sparse update do not
renew old observations; explicit unavailable/null values invalidate positive
support for the affected field. Conflicting equally authoritative observations
produce unknown, not last-write-wins. Use provider ordering where supplied;
otherwise preserve uncertainty rather than guessing freshness from arrival order.

For a valid percentage observation, report `100 - usedPercent` in percentage
points. Preserve provider over-limit values and flag exhausted/overdrawn rather
than silently normalizing them. Do not convert subscription percentage to tokens,
requests or currency. Admission requires a same-unit task demand/bound for every
applicable constraint. P3 accepts explicitly sourced caller demands; empirical
task estimates and their calibration belong to P4. If no bound exists, remaining
percentage is reportable but fit is unknown. An operator budget/override is
separate, audited and explicitly unverified, never provider evidence.

Offline reports that evaluate freshness or capacity require `--as-of`; structural
validation and import do not evaluate freshness. Replay never consults wall-clock time.
Live runtime operations use an injected trusted clock and record the evaluated
instant. Initial AOK freshness defaults: capacity observations 60 seconds, manual
observations five minutes with no automatic authoritative promotion; source policy
may shorten these. Old timestamps never renew on import, export or cache access.
Capacity TTL is independent of P2 research validity. Backward clock jumps, future
observations and excessive skew invalidate admission until refreshed.
Repeated delivery of cached fields does not renew their observation time. When a
source supplies no observation timestamp or update marker, record receipt time
separately and declare freshness unknown unless pinned source semantics establish
that the value was refreshed. A status-line redraw alone is not a quota refresh.

At/after `resetsAt`, mark the old window `reset-unconfirmed` and refresh; do not
replenish allowance from the clock alone. Preserve whether reset is exact or
estimated; rolling windows need source-specific semantics. A reset hint predicts
when to recheck, not when launch is guaranteed. Retry-after, reset and refresh
hints are distinct; observed exhaustion in one window cannot be fixed by resetting
another. Refunds, plan changes and account switches require fresh scoped evidence.

## 4. Reservation and recovery protocol

The reference store is private local SQLite WAL with real transactions, schema
versioning, foreign keys, restart recovery and an append-only normalized audit
log. The directory is 0700 and data/WAL/SHM/backups 0600, or equivalent Windows
ACLs. No network filesystems or concurrent writers outside the one authority.
Database contention uses a context-bounded busy deadline of at most five seconds;
then return a typed retryable conflict with no partial mutation. Retry the same
logical request/key explicitly; do not spin indefinitely or silently drop events.
A pure-Go driver must pass the existing Go 1.22 and supported release cross-build
matrix before selection; do not quietly raise Go versions or introduce CGO.

Expose library operations `Ingest`, `ReadSnapshot`, `Reserve`, `Renew`,
`ConfirmLaunch`, `Settle`, `Reconcile`, `ReleaseUnlaunched` and `Recover`, all with
context cancellation, expected revision/owner and stable idempotency keys.
Bind each operation key to its canonical request digest. The same key/request
returns its original result; the same key with a changed demand, scope or payload
is a conflict. A new attempt needs a new key rather than silently altering a hold.

```text
held -> in-flight -> settled
held -> released                 only with proof launch never occurred
in-flight -> uncertain           crash, timeout, lost stream or cancellation
uncertain -> settled            scoped reconciliation or audited disposition
```

- Reserve every applicable window in one transaction, or none. Read current
  snapshots, freshness, pending debits, holds and policy inside that transaction.
  Insufficient capacity is blocked; missing evidence is unknown/refresh-needed.
- Use a monotonic fence per reservation plus an authority epoch. The ledger
  revision is separate: creating another reservation must not invalidate existing
  reservations. Old owners cannot renew, launch, settle or release after takeover.
- `ConfirmLaunch` compares owner/fence/lease, snapshot generation and policy,
  reevaluates capacity and commits in-flight intent before dispatch. This closes
  the accounting race; P6/P7 must enforce the token at the execution boundary.
  A crash after intent but before actual launch remains uncertain until reconciled.
- Expired never-launched holds release. In-flight/uncertain holds remain on expiry;
  cancelling a client or expiring a lease does not prove provider consumption ended.
- Idempotent settlement may report usage above the estimate; record the overrun
  and block further admission as needed. Never discard actual usage to preserve a
  budget invariant. Remove holds only with covered usage or proven unused capacity.
- Source updates can reduce capacity below existing commitments. Mark overdrawn,
  prevent new reservations and notify the runtime; do not silently cancel running
  work, replenish balances or invoke fallback models.
- Reopen/recovery reconstructs projections from records and checks integrity.
  Corruption or unknown DB versions fails closed. Migrations require recoverable
  backups; exporting a snapshot never copies mutable database files to a project.

The conformance invariant is no *new* admission beyond the current effective
same-unit capacity under the configured demand bounds. Provider enforcement,
external callers and underestimated task usage remain distinct limitations.

## 5. Collector rollout and supported-source gates

Documentation checked 2026-10-23 is discovery input, not live conformance evidence.
Pin tested binary/protocol versions and sanitized fixtures during implementation.
Every host reports usage and capacity support independently, using `supported`,
`unsupported`, `unknown`, `auth-required`, `stale`, `conflict` or `error` states.

| Host | First implementation target | Capacity and completion gate |
|---|---|---|
| Codex | App Server `thread/tokenUsage/updated`, account rate-limit reads/updates; exact event counter semantics verified from pinned protocol | Prefer `rateLimitsByLimitId`, preserve each primary/secondary window and legacy fallback; account/auth changes invalidate binding. API billing and subscription pools stay separate. No automatic reset-credit redemption. Live authorized read required |
| Claude Code | Opt-in status-line stdin normalizer plus structured run-usage import where source semantics are verified | Handle documented `rate_limits` five-hour/seven-day fields and distinct gateway spend fields when present; version/auth-mode gates and absent-field fixtures. No synthetic model call to populate fields |
| OpenCode | Discovery of structured session/server usage; documented statistics as aggregate cross-check | Token/cost history is usage only. No invented JSON flag for statistics; no quota claim without supported provider source |
| Gemini CLI | Import documented structured headless output from existing authorized runs | Auth-mode-specific discovery; interactive stats are a manual observation until a stable machine-readable capacity surface passes conformance |
| agy | Supported CLI/event source discovery against pinned version | Model listing proves no usage/quota; explicit unknown/unsupported until verified |
| Cursor | Supported run/account interface discovery against pinned version | No dashboard scraping or guessed account endpoints; unsupported source is a valid discovery outcome |
| OpenClaw | Supported session/model usage discovery | Routed provider and child-call attribution must demonstrate no duplicate billing; underlying provider pool mappings explicit |
| Hermes Agent | Supported run/session usage discovery | Actual provider/configuration and source semantics verified before support; quota otherwise unknown/manual |

Codex's documented account reads/notifications and token event are the planned
source boundary, with multi-bucket quota preferred over legacy data. This does
not imply coverage of every running desktop/CLI session; test that coverage and
expose gaps. [Official Codex App Server documentation](https://learn.chatgpt.com/docs/app-server).

Claude documents status-line JSON including optional quota and version-dependent
gateway spend fields. Implement a fast allow-list normalizer, never persist raw
stdin, and compose collection with the existing status-line command while preserving
its output. Installation is explicit opt-in with backup and rollback; no setting
replacement merely to inspect usage. Missing quota before a response remains unknown.
[Official Claude status-line documentation](https://code.claude.com/docs/en/statusline).

OpenCode documents token/cost statistics and session/export commands; verify a
structured accounting surface before enabling automated collection.
[Official OpenCode CLI documentation](https://opencode.ai/docs/cli/).
Gemini documents structured headless output and distinguishes authentication/quota
modes, but these alone do not establish an automated remaining-capacity endpoint.
[Official headless reference](https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/headless.md),
[official quota reference](https://github.com/google-gemini/gemini-cli/blob/main/docs/resources/quota-and-pricing.md).

No collector accesses private credential databases, undocumented OAuth endpoints,
transcript directories or browser dashboards. Use existing authorized host channels;
the collector neither logs in nor starts inference. Stream parsing is allow-listed
and transient; persist only normalized accounting evidence and sanitized hashes.
Source fields that may contain prompts, responses, environment values or paths
are discarded before persistence, diagnostics and hashing of exported evidence.
Untrusted strings cannot become executable commands or terminal control output.

Use event-triggered refresh first, one concurrent refresh per pool/source, and a
TTL cache. Read requests have a five-second timeout and no immediate automatic
retry. An explicitly running runtime may schedule bounded exponential backoff
starting at 30 seconds, capped at five minutes, honoring longer Retry-After; stop
on auth-required/unsupported. Reset boundaries trigger refresh eligibility, not
background wakeups or a new AOK daemon. Status-line collection does no network I/O.

## 6. Proposed interfaces and selection handoff

These are new proposed commands, not current CLI behavior:

```text
aok usage validate <export> --policy <file> [--json]
aok usage import --source <host> --input <file> --binding <file> --policy <file> --out <new-directory>
aok usage replay --manifest <file> --policy <file> --as-of <RFC3339> --out <new-directory>
aok usage summary <export> --as-of <RFC3339> [--group-by host,model,pool,project,work-item,run] [--json]
aok usage capacity <export> --catalog <bundle> --catalog-policy <file> --profile <file> --policy <file> --as-of <RFC3339> [--demand <file>] [--require-fit] [--json]
aok usage collectors [--host <host>] [--json]
aok usage collect --host <host> --binding <file> --policy <file> --out <new-directory>
```

`collect` performs only a supported bounded read and requires configured opt-in;
stream attachment belongs to the owning runtime adapter. No generic arbitrary
command execution is accepted. Offline commands work with empty PATH, no credentials
and blocked network. Output directories use the existing private non-overwriting
writer. Exit codes: 0 valid report (including explicit unknown/unsupported), 1
invalid or blocked required-capacity result, 2 misuse. No summary reserves capacity.
The optional demand file pins configuration and per-constraint unit/amount, bound
source and validity; its digest is included in the capacity result. Without demand,
report balances but leave fit unknown. `--require-fit` explicitly makes insufficient,
unknown or refresh-needed capacity exit 1; descriptive unknown reports still exit 0.

P4 receives exact catalog/policy/configuration/profile hashes plus an immutable
CapacitySnapshot digest, ledger revision, pool/constraint refs, as-of/freshness,
all remaining/reset values, pending holds, confidence and unknown/conflict reasons.
Return capacity dispositions `observed-fit`, `insufficient`, `unknown` or
`refresh-needed`; timing hints do not imply permission to launch. P4 supplies
task-specific demand bounds, suitability filters, scoring and selected/wait/defer
decisions. P7 rechecks and reserves atomically after selection. Do not implement
ranking or SelectionDecision in P3, and do not alter P2 static resolver behavior.

Example: a configuration has 42 percentage points left in its short window but
only two in its weekly pool. A supplied three-point weekly demand fails that
constraint regardless of the short balance. Another CLI using the same pool
cannot obtain a separate allowance. The report offers a refresh/reset hint;
P4 may defer or consider an independently eligible pool. Tokens never substitute
for the missing percentage demand, and changing model alone does not evade a
shared exhausted constraint.

## 7. Work packages and dependency order

| Step | Deliverable | Dependencies | Acceptance |
|---|---|---|---|
| P3.0 | Pin source/version/auth support matrix, sanitized source fixtures, accounting ADR and SQLite portability decision | Current P1/P2; official discovery | Exact counter/window meanings recorded; no guessed endpoints; driver builds on supported Go/release targets |
| P3.1 | Closed records/policy schemas, structural identities, bindings, exact arithmetic | P3.0 | Strict parser/security cases; unknowns stay unknown; no collision across pools/configurations; existing P1/P2 behavior unchanged |
| P3.2 | Event normalizers, dedupe, revisions, cumulative and parent/child accounting | P3.1 | Replay invariant, corrections and reordered/interrupted streams pass; sums do not double count |
| P3.3 | Multi-window capacity projector, TTL/reset semantics, uncertainty and snapshot reconciliation | P3.1–2 | Every applicable window checked; source overlap tracked; stale/reset/unavailable never becomes fresh allowance |
| P3.4 | Runtime store interface and private local transactional reference store; reservation conformance | P3.2–3 | Multi-process atomicity, crash recovery, fencing, all-window rollback and idempotency pass |
| P3.5a | Codex and Claude normalizers, collector descriptors and bounded reads | P3.1–3; P3.0 source pins | Fixtures/auth-mode/missing-field coverage; no inference launch or credential persistence; gaps visible |
| P3.5b | Authorized live Codex/Claude observation gates | P3.5a; supported read channel; P6 only where needed | One scoped fresh quota observation per supported auth/source, account-switch behavior and sanitized capture demonstrated |
| P3.6 | Six remaining discovery reports/adapters | P3.0–3 | Every host has tested support status; enabled adapters meet the same fixture/live gates; usage-only explicitly allowed |
| P3.7 | Offline CLI, summaries, immutable export/replay and P4 capacity API | P3.2–4; descriptors from P3.5–6 | Exact numbers, coverage and reset hints shown; deterministic snapshot/hash; no ranking/dispatch side effects |
| P3.8 | Independent review, operator guide, migration/distribution/security checks | Integrated packages | All core checks below pass; separate live/runtime gates reported without claiming completion |

After P3.1 is locked, use two Luna-medium implementation workers: one owns
collectors/fixtures/descriptors; one owns pure accounting/projectors and their tests.
Primary owns store/transactions, CLI and P1/P2 integration. Use a separate Luna-high
reviewer after integration. Do not parallelize edits to shared record/schema files.

## 8. Acceptance suite and release gates

Required automated cases:

- Exact replay/no-op, conflicting duplicate IDs, missing IDs, out-of-order samples,
  cumulative 10→16=16, explicit correction to 13, explicit epoch rollover,
  unexplained decrease, adapter upgrades replaying old events, nested/inclusive
  child totals and actual model rerouting.
- Independent pools with identical labels; shared pool across two different CLIs;
  model-family and org/project overlaps; every simultaneous window; mismatched
  units/currencies; cache and reasoning subset semantics; missing attribution.
- Stale/null/sparse/full snapshots, equal-authority conflicts, out-of-order provider
  revisions, snapshot replacement, source outages/auth changes, expired observations,
  exact reset boundary, rolling resets, skew/DST/UTC offsets and original-date retention.
- Known/unknown source watermarks, delayed provider ingestion, external usage,
  partial coverage across windows, snapshot decrease/increase/refund, and settlement
  transitions that neither double debit nor restore unproven capacity.
- At least 100 competing local processes race reservations against a fixed fake
  capacity: no over-admission, all-window atomicity and bounded conflict handling.
  New reservations do not invalidate other valid fences. Old owner cannot operate
  after takeover. Restart/crash injection at every transaction/launch-intent boundary,
  idempotent reserve/settle/reconcile, unlaunched expiry, in-flight expiry/cancel,
  conflicting idempotency-key payloads, overrun and source depletion all preserve
  the specified states.
- Exact P2 configuration/catalog/policy and P1 run refs survive exports; changed
  pool policy/source snapshots/ledger revision alter capacity digest. Identical
  inputs/as-of produce identical exports. Capacity does not imply static support,
  account entitlement, sandbox enforcement or workflow acceptance.
- Privacy canary inputs contain fake credentials/prompts/private paths: none appear
  in DB, WAL, SHM, exports, diagnostics, backups or logs. Symlink/ACL/path, corrupt
  DB/unknown version and malicious terminal-output cases fail safely. Hook collector
  failure preserves the configured status-line output and does not block the host.

Delivery checks: focused tests, race detector for accounting/store, full Go suite,
Go build, Python regressions, kit validation, CLI empty-PATH/no-network smoke tests,
release archive checks and release-target cross-builds. A SQLite dependency must
pass CGO-disabled builds for each supported release target. It must
not break dependency-free Python validation or supported installer platforms.
Use fake clocks/transports and deterministic seeds; live gates are separate bounded
read-only checks, not part of ordinary CI or a request to consume model allowance.

Publish three statuses independently:

1. **P3-core delivered**: offline records, accounting, reference store conformance,
   reports and all eight explicit collector support states pass.
2. **P3 live collector gate completed/pending per host/auth mode**: Codex/Claude
   fresh scoped evidence required first; unsupported hosts remain explicit.
3. **P3 runtime admission gate pending P6/P7** until a pinned external runtime
   enforces reservation tokens and demonstrates launch/recovery reconciliation.

Never claim the whole phase complete while a required live or runtime gate is
pending. Capacity accounting is opt-in; existing model defaults and research stay
unchanged. See [P2](phase-2-model-catalog.md), [P1](phase-1-shared-contracts.md)
and the [end-to-end roadmap](end-to-end-workflows.md).
