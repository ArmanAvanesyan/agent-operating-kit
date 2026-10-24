# Usage and shared capacity accounting

Phase 3 supplies an opt-in offline accounting core and a runtime-owned local
reference store. Live collector gates are **pending per host/auth mode**;
runtime admission enforcement remains **pending P6/P7**. Accounting reports never
grant account access, authorize execution, or reserve allowance at a provider.

## First offline report

The fixture uses synthetic account/configuration identities, not a live account.

```sh
go build -o build/aok ./cmd/aok
build/aok usage collectors --json
build/aok usage validate internal/usage/testdata/export.json \
  --policy internal/usage/testdata/policy.json --json
build/aok usage summary internal/usage/testdata/export.json \
  --policy internal/usage/testdata/policy.json \
  --as-of 2026-10-23T12:00:30Z --group-by host,model,pool --json
```

The independently versioned export, source policy, binding and demand schemas use
`aok-usage/v1`, separate from the P1 corpus and P2 catalog. Pure arithmetic uses
exact decimals, including currency and subscription percentage points. Summaries
show billed observations, separately labeled estimated costs, attribution gaps,
coverage and exclusions. They do not count context snapshots as session consumption.

Sources, pools and configurations are distinct. Bindings explicitly connect a
host/version/source/auth mode to a pseudonymous pool and exact configuration IDs.
Several CLIs may share a pool. Display/model names never create account identity,
and changing an alias does not rewrite historical attribution. Unknown model or
revision remains unknown. P1 work-item/run references are pinned when supplied;
validation does not fabricate missing runtime bindings or accepted completion.

## Import and replay

```sh
build/aok usage import --source codex --input captured-frames.jsonl \
  --binding binding.json --policy policy.json \
  --received-at 2026-10-23T12:00:00Z --out build/usage-import --json
build/aok usage replay --manifest replay-manifest.json --policy policy.json \
  --as-of 2026-10-23T12:00:30Z --out build/usage-replay --json
```

`--received-at` is the **original capture instant**, supplied explicitly. It is
not an import-time clock: copying old frames must preserve that original instant.
No command invents an observation timestamp from the current wall clock.
`--stream-revision <positive-integer>` may carry an original serialized capture
ordinal for the exact bound source stream. It is local capture order, not a vendor
revision; do not assign ordinals to out-of-order requests merely by arrival time.
Without trustworthy ordering, conflicting quota observations remain unknown.
One quota source identity must resolve to one bound capture stream; independent
streams need distinct source IDs, and their ordinals are never compared globally.

Replay manifests are closed JSON with this shape:

```json
{
  "version": "aok-usage/v1",
  "inputs": [{"path": "first/usage.json", "sha256": "<exact SHA-256>"}]
}
```

Inputs are confined relative files with verified hashes. Replay accepts
self-contained normalized exports, and rejects conflicting identities or auxiliary
assets rather than silently losing provenance. Reports are deterministic for the
same inputs and as-of instant. Outputs use the existing private non-overwriting
evidence writer; parent directories must exist and have no symlink ancestors.
Import preserves only allow-listed normalized fields, not raw transcripts,
prompts, credentials, private paths or whole status-line payloads.

Source policy must be loaded separately through trusted operator/runtime
configuration. A captured policy/hash establishes integrity, not authorship or
permission. Policy pins source ID, host/protocol version, adapter version, auth
mode, metric and quota authority. `collectionAllowed` controls **live I/O**;
offline imports can normalize explicitly supplied snapshots without enabling it.
Reformatting supplied policy bytes changes its evidence hash. A policy change
cannot silently discard existing commitments.

## Capacity and the model-selection boundary

```sh
build/aok usage capacity usage-bundle --policy policy.json \
  --catalog model-catalog --catalog-policy catalog-policy.json \
  --profile execution-profile.json --as-of 2026-10-23T12:00:30Z --json
```

Add `--demand demand.json --require-fit` to require observed fit. The demand pins
the exact configuration ID, profile bytes, validity and an amount/unit/generation
for every applicable constraint. Catalog/profile contradictions are rejected;
unknown identity or uninterpreted version constraints cannot establish exact fit.
The final report digest binds catalog/policy/profile, capacity and demand evidence.

Every relevant window is evaluated separately. Shared pools are not duplicated
per host/model. Tokens, requests, credits, currency and percentage points are never
converted implicitly. Remaining percentage is `100 - used`, preserving over-limit
values. A percentage balance alone cannot predict whether a token-sized task fits.
Task demand estimation/calibration and ranking belong to P4.

Headroom subtracts uncovered charges, unresolved overlap, unspent active holds and
the policy margin from reported remaining. Each charge/hold portion contributes
once per constraint. Provider snapshots replace observations; they are not added.
Only explicit per-charge/per-window coverage can absorb local debits into a
provider balance. Without coverage evidence, subtraction stays conservative.
External activity can consume allowance after a read, so this is observed capacity
for participating local callers, not guaranteed provider capacity.

Missing, stale, conflicting and unavailable observations never imply unlimited
or zero allowance. The initial capacity freshness cap is 60 seconds, shortened
by source policy and captured expiry. At the reset boundary, the old observation
becomes reset-unconfirmed; the clock does not replenish it. Ongoing holds from a
different window generation make capacity unknown until reconciled. Reset and
retry hints indicate when to refresh, not when execution is guaranteed.

Without demand, fit remains unknown while balances remain inspectable. Normal
descriptive unknown reports exit 0; invalid/conflicting imports or required-fit
failures exit 1; misuse exits 2. Partial accounting summaries/replays exit 1 so
automation cannot mistake excluded ambiguous consumption for a complete total.
No report selects, reserves or launches a model.

## Collector support and live gates

| Host | Delivered offline capability | Live gate |
|---|---|---|
| Codex | App Server v2 usage and quota frame normalizers; injected bounded read-only transport | Pending exact binary/auth-mode/account conformance |
| Claude Code | Status-line observation normalizer; estimated cumulative cost separated from context and billed usage | Pending opt-in installation and fresh supported quota evidence |
| Cursor, OpenCode, Gemini, agy, OpenClaw, Hermes Agent | Explicit discovery/unknown descriptors | Pending supported-source discovery and individual conformance |

Codex cumulative thread totals are recorded once; cached input/reasoning subsets
are not added twice. Complete documented read-window fields can form a full
observation; sparse notifications do not renew omitted fields. The multi-bucket
view is preferred. Subscription allowance stays separate from API billing.
[Official Codex App Server documentation](https://learn.chatgpt.com/docs/app-server).

Claude status-line redraws do not prove quota freshness. Those quota observations
remain inspectable with freshness unknown; estimated gateway spend is separate
from subscription percentages and provider-reported billing. No automatic
status-line replacement or live installation is performed.
[Official Claude status-line documentation](https://code.claude.com/docs/en/statusline).

`usage collect` reports that no CLI transport is configured and exits 1 without
writing a bundle or making a request. Runtime code may inject an already-authorized
`ReadOnlyTransport` for the sole Codex account-rate-limit read, with a five-second
context. Ordered transports may supply a serialized capture ordinal. This is an
integration seam, not evidence of passed live conformance. There is no host process
launch, login, dashboard scraping, private credential-store access, automatic
reset-credit redemption or model invocation.

The sanitized fixtures and source/protocol pins are documented in
[the collector guide](../internal/usage/collectors/README.md).

## Runtime-owned local reference store

`internal/usage/store.AccountingStore` defines ingest/read, reserve, renew,
confirm-launch, settle, reconcile, unlaunched release and recovery operations.
The SQLite WAL implementation uses `modernc.org/sqlite v1.34.5` without CGO;
Go 1.22 compatibility and six release-target cross-builds are delivery gates.
It accepts a dedicated real local directory owned privately by the runtime.
Unix permissions are 0700/0600; Windows applies a protected current-user DACL.
No network filesystem or multi-machine coordination is supported. Windows ACL
code is cross-compiled; native Windows enforcement requires its runtime gate.

All-window reservations are transactional. A request key binds its payload; changed
payloads conflict. Per-reservation fences survive unrelated reservations. Launch
confirmation requires the saved profile digest and valid lease/policy/demand;
the runtime must subsequently enforce that token at the P6/P7 execution boundary.
Lease renewal cannot exceed the original demand validity.

Expired unlaunched holds release. In-flight crashes/cancellation/expired leases
retain uncertain holds; takeover fences the previous owner. Settlement transfers
actual consumption into pending charges and releases proven unused capacity.
Overruns remain recorded and may block further admission. Audit records bind the
state chain and idempotency request/result hashes; corruption blocks reads/writes.
Clock regression, unknown DB versions and policy/binding changes fail closed.

This bounded reference implementation retains a full normalized snapshot per
audit operation and caps a state snapshot at 4 MiB. Audit storage grows with the
operation count and snapshot size. It has no automatic retention, compaction,
database migration or backup scheduler. Operators must retain private recoverable
backups before a future migration; copying an active DB is not an immutable export.
Existing commitments must be reconciled before any explicit store/policy migration.
Settlement across a newly observed window generation also fails closed: the
reference API does not yet accept a cross-window consumption allocation. Such
holds remain uncertain until a verified runtime reconciliation/migration path is
available; an old-window settlement cannot silently release new-window exposure.

## Completion status

**P3-core** covers offline contracts, normalization, summaries, capacity projection,
the reference store and conformance tests. **Live collection** is pending per
host/auth mode. **Runtime admission** is pending P6/P7. No live account was read
and no provider allowance was consumed during offline implementation.
See [the Phase 3 plan](implementation/phase-3-usage-capacity.md) and
[delivery evidence](implementation/phase-3-delivery-evidence.md).
