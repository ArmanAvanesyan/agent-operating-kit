# Phase 8 delivery evidence

Implementation date: 2026-10-24. Scope and acceptance baseline:
[Phase 8 plan](phase-8-verification-delivery-writeback-plan.md).
Operator instructions: [verification, delivery and writeback](../verification-delivery-writeback.md).

## Independent completion gates

| Gate | Result |
| --- | --- |
| P8-core | **Delivered: offline and injected-transport scope; acceptance checks passed** |
| Command-verifier live environment | Pending: no qualified installed control transport; CLI execution fails closed |
| Acceptance identity qualification | Policies and signed fixtures implemented; real issuer/author/reviewer identity qualification remains external |
| Existing repository delivery | Receipt translation implemented; no publication, merge, deployment or cleanup performed |
| GitHub live read / comment write | Pending per API/authentication/principal/scope |
| Azure DevOps live read / revision-tested field write | Independently pending |
| Linear live read / comment write | Independently pending |
| Jira Cloud live read / ADF comment write | Independently pending |
| Other fields, transitions, creation and conditional/idempotent semantics | Each operation requires its own provider conformance; operator-facing creation remains disabled |
| Codex P6 live | **Blocked**: trusted scoped filesystem/network/ambient controls and live conformance absent |
| Claude P6 live | **Independently blocked** for the same requirements |
| P7 connected runtime and P9 pilot readiness | Pending the complete selected host/capacity/quality/verifier/delivery/provider path |

No live agent invocation or tracker mutation was performed. The implementation
registers no production provider write transport. Fixture-qualified software is
not a production principal, observed concurrency guarantee or routing cohort.

## Implemented packages

- `internal/verification`: closed independently versioned records; bounded safe
  capture; signed per-criterion coverage and verifier/review receipt digests; explicit inventory/mode/scope/evidence checks; external issuer and
  authorship policy; signed pinned verifier, independent review and acceptance
  receipts. Command execution only through separately signed scoped controls.
- `internal/runtime`: exact current candidate lineage, signed short-lived
  projection authority, explicit SQLite migration, digest-bound acceptance
  attachment, permanent receipt revocation, and transactional dependency/fence
  guards. Existing P7 wire records retain `accepted:false`.
- `internal/deliverybridge`: recomputes actual existing Python-engine seals,
  snapshot inventories, context, gate/review and optional integration bindings;
  independently authenticates review. Produces only a local context draft.
- `internal/writeback`: narrow externally signed grants, exact fresh
  draft/snapshot/mapping binding, native completion-value policy, private SQLite
  outbox, expiring pre-intent claims, fenced one-send intent, monotonic
  revocation, versioned database identity and private sidecar checks, durable clock high-water, signed operation-aware reconciliation,
  and a separately typed fixture creation boundary.
- `internal/connector`: private authorization construction after exact durable
  intent; one-use transport tokens; no declaration-to-approval conversion.
- `internal/tracker`: separate typed injected writers and readback for four
  provider slices. Existing P5 read transport remains read-only.
- `internal/cli`: opt-in verification, runtime capture/attachment, local handoff,
  approval preimages, enqueue/revocation/outbox inspection and offline replay.
  Dispatch/reconcile report blocked provider transport gates.

Creation uses a separate signed parent/type/schema/field/correlation intent,
not a fake P1 remote ID. The fixture creator receives a private single-use
permit only after durable intent. Actual provider-assigned identity and a valid
P1 binding are available only after trusted signed readback. There is no
operator-facing create command or registered production creator.

## Review and fixes

Luna workers at medium reasoning implemented bounded packages. An independent
Luna-high reviewer audited the integrated result; the primary also reviewed
provider effect reconciliation and time fencing. Findings were corrected with
regressions for:

- Candidate ID traversal, unsafe paths, unlisted/mutated bytes and modes.
- Cross-verifier criterion substitution, stale evidence renewal, claimed reviewer
  identities, same-principal cross-role review and missing author bindings.
- Authoritative plan/goal/assignment/configuration/profile/author lineage.
- Revoked receipt reattachment, policy changes under an unchanged generation,
  and state/audit reads racing across processes.
- No-op authorization guards, wrong queued operation/grant binding and direct
  creator invocation bypasses.
- Revocation omission, pre-intent crash recovery, subsecond expiry comparisons,
  backward clocks across restart and caller-asserted readback success.
- Signed readback target mismatch, marker/content correlation and incomplete
  provider responses. Ambiguous outcomes remain unknown and never auto-resend.

The send-intent CAS is the authorization-consumption point. A revocation after
that point cannot promise suppression of an in-flight effect. No database/provider
atomic transaction or universal exactly-once guarantee is claimed.

## Acceptance campaign

The final campaign covers focused package tests, the full Go 1.22 suite including
release packaging/extracted-kit checks, race and cross-process cases, Python
regressions, kit validation, syntax/whitespace checks, empty-PATH/no-credential
network-denied smoke tests and the six CGO-disabled release targets.

| Check | Recorded result |
| --- | --- |
| Focused verification, outbox, connector, delivery, tracker and runtime tests | Passed |
| `go1.22.12 test -p 2 ./...` | Passed; includes release packaging and extracted-kit validation |
| Race tests: verification, writeback, connector, deliverybridge, tracker, runtime/staging/store and usage/store | Passed |
| Multi-process claim and crash/reopen checks | Passed; runtime claim contention also repeated ten times |
| Python contributor regressions | 267 tests passed, four skipped |
| Python syntax, release shell syntax, kit validation | Passed |
| Whitespace checks, including new Phase 8 source/docs | Passed |
| Built CLI: empty PATH, no credentials, macOS `deny network*` | Passed; import/check/inspect, immutable-output refusal, replay, read-only inspection and blocked-live gates |
| Six CGO-disabled release binaries | Passed: Linux/macOS/Windows, amd64/arm64 |
| Independent Luna-high review | Closed: no remaining material findings after fixes |

The toolchain was the cached Go **1.22.12** binary with `GOTOOLCHAIN=local`,
`GOPROXY=off` and `GOSUMDB=off`. Package parallelism was limited to two for the
final full run: an initial unconstrained run hit the existing five-second fake
Python discovery timeout under build load. No product timeout was relaxed.
Strict path tests use canonical `/private/tmp`, because `/tmp` is a symlink on
macOS. The initial sandboxed Python campaign could not access its controlling
terminal or start a nested macOS sandbox; the complete unrestricted fixture
campaign subsequently passed. Neither run exercised a real agent or provider.
Compilation establishes buildability, not native platform certification.

## Fixture API and authentication scope

| Provider | Fixture operation | API label / credential mode | Live qualification |
| --- | --- | --- | --- |
| GitHub | Evidence comment and complete readback | REST 2022-11-28; gh-session reference | Pending |
| Azure DevOps | Single native field with revision test and readback | REST 7.1; bearer reference | Pending |
| Linear | UUID-scoped comment and GraphQL readback | GraphQL; bearer reference | Pending |
| Jira Cloud | ADF comment and numeric issue/self-URL readback | REST v3; bearer reference | Pending |

These are injected fixture modes. No token, authenticated request, native
conditional-write guarantee, OAuth gateway behavior or provider permission was
live-tested by this campaign. Truncated/partial readback, GraphQL errors, changed
content or duplicate markers remain unknown. HTTP errors and retries never
silently switch credentials or produce an automatic resend.
