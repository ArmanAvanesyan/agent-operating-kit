# Phase 3 delivery evidence

Status: **P3-core delivered**, 2026-10-23. **Live collector gates pending per
host/auth mode**. **Runtime admission gate pending P6/P7**. No live account read,
model invocation, quota reset or provider mutation was performed.

## Implemented scope

| Package | Delivered |
|---|---|
| P3.0–1 | Source-policy and account-pool contracts; closed embedded `aok-usage/v1` schemas; exact decimal arithmetic; safe bounded loading and immutable sanitized exports |
| P3.2 | Scoped event replay, explicit corrections, cumulative-counter discontinuities, parent/child overlap handling, cross-CLI provider-request dedupe and separate estimated costs |
| P3.3 | Shared-pool multi-window capacity, fresh/scoped source checks, demand bounds, explicit reset uncertainty and conservative charge/hold reconciliation |
| P3.4 | Runtime store interface and private local SQLite WAL reference implementation; transactions, idempotency, per-reservation fencing, lease recovery, settlement and audit integrity |
| P3.5a | Codex App Server v2 and Claude status-line fixture normalizers; injected bounded Codex read transport; explicit ordered-capture API |
| P3.5b | Live conformance pending; the CLI does not configure transports or install a status-line collector |
| P3.6 | All eight hosts have explicit support descriptors; the other six remain unknown pending verified source discovery, with no invented adapters |
| P3.7 | `usage validate/import/replay/summary/capacity/collectors/collect`; exact P2 catalog/profile/policy evidence in capacity reports; no ranking, selection or dispatch |
| P3.8 | Independent Luna-high review, documentation, offline executable tests, regression and portability checks |

Two Luna-medium workers owned collectors and accounting; another Luna-medium
worker completed schema/validation and CLI acceptance work. The primary owned
store/CLI integration. The independent reviewer found no remaining blockers
after fixes for policy hashes, receipt dedupe, exact profile binding, source
ordering and idempotency-cache integrity.

## Acceptance results

- Full Go suite passed under **Go 1.22.12** with `go test -p 1 ./...`, including
  release archive validation and extracted release-builder tests.
- `go test -race ./internal/usage/...` passed.
- The multi-process conformance test raced **100 processes against ten units**:
  ten reservations were admitted, no allowance was over-admitted, and the remaining
  callers were blocked or conflicted.
- Focused tests cover all-window rollback; same-key request conflicts; independent
  fences; exact launch profile; demand validity; source/policy changes; old-window
  holds; restart after launch intent; in-flight uncertainty; stale owners; repeated
  settlement; overruns; cancellation; uncommitted process crash; corrupt audit and
  operation rows; private storage and symlink rejection.
- Accounting/collector tests cover reordered/duplicate/corrected observations,
  adapter upgrades, incremental fragments, cumulative totals, cache subsets,
  shared pools across CLIs, independent pools, ambiguous parent totals, conflicts,
  expired/reset/missing evidence, unknown freshness and ordered/unordered captures.
- Privacy canaries from source credentials/prompts/paths are absent from normalized
  exports and persisted database/WAL/SHM files.
- Closed schemas are tested directly with the JSON Schema compiler before typed
  decoding, including nested records and reservation evidence.
- Python regressions: **267 passed, four skipped**. The run used existing required
  terminal/process-confinement permissions. Python compile and kit validation passed.
- Go 1.22 CGO-disabled **CLI and accounting-store test binaries** cross-compiled for
  Linux amd64/arm64, macOS amd64/arm64 and Windows amd64/arm64.
- Shipped executable tests run offline commands with empty PATH, an allow-listed
  environment and no credentials; import/replay preserve deterministic bytes and
  refuse existing output directories. CI includes offline usage smoke commands
  and accounting-store cross-builds.

The local gates use fake accounts, clocks and provider observations. They establish
accounting behavior and portability, not live availability or execution enforcement.

## Remaining gates and bounded reference limits

Codex needs pinned binary/auth-mode tests of the actual authorized read channel,
source ordering, account switching and coverage of existing sessions. Claude needs
opt-in composition/rollback of the existing status line and a supported fresh
quota signal; redraws alone stay unknown. The other six hosts require independent
supported-source discovery and conformance before their descriptors can advance.

P6/P7 must enforce the accounting reservation token and demonstrate real
launch/cancel/recovery reconciliation. Native Windows ACL behavior is implemented
and cross-compiled, but requires native runtime verification. Local reference
transactions do not reserve provider quota or coordinate external/multi-machine
callers. Task consumption calibration and automatic selection remain P4.

The reference store is single-authority/local-filesystem only, with a 4 MiB state
limit and full normalized snapshots per audit operation. It has no automatic
compaction, migration, retention, backup service or daemon. Policy/binding changes
fail closed pending explicit migration of existing commitments. These limits are
documented in the [operator guide](../usage-accounting.md).
Cross-window consumption allocation is not provided by the local reference API;
settlement after a generation change retains the uncertain hold and requires the
later verified runtime reconciliation/migration path.

See the [Phase 3 plan](phase-3-usage-capacity.md) and
[end-to-end roadmap](end-to-end-workflows.md).
