# WorkKit P7 delivery evidence

Baseline: P6 merge `28cfa6527bcb4ca3dda87f36db27777b86204aba` (PR #92).
Observation date: October 26, 2026. Historical P0–P6 records and qualification dates
are preserved. This document concerns the P7 offline implementation only.

## WK-070–077 checklist

| Work package | Implemented boundary | Acceptance evidence |
|---|---|---|
| WK-070 | Closed sync/v2 and integration/v1 records; separate journal/v2; v1 unchanged | Generated schema checks, strict parser/version tests, pin and regressions |
| WK-071 | Explicit forward/reverse three-way ordinary-field reconciliation | Two distinct Azure scopes; unilateral/equal/divergent/no-op tests |
| WK-072 | Stable conflicts, explicit observed-side resolution, fresh proposal | Changed-input/ownership/tampering tests; no implicit authority |
| WK-073 | Bounded operational read port, separate read evidence, GitHub raw-envelope ingestion | HMAC/signature/substitution/coverage/checkpoint/budget fixtures |
| WK-074 | Fresh v2 journal, fences, inbox, latest conflicts, notification outbox | Real SQLite multiprocess races, recovery/ack/candidate/send-intent guards |
| WK-075 | Serial review-first cycles, idle renewal, current external ownership and approved batches | No implicit sends, revocation, scope, expiry and uncertainty tests |
| WK-076 | Shared SDK/three CLI entry points, private macOS/Linux IPC | Domain/suite parity, peer/fence/lost-reply/cleanup and empty-PATH smoke |
| WK-077 | Operator guide, matrix, recovery guidance, review and distribution | Source-bound acceptance bundle and independent receipts |

All supported production boundaries retain protected injected dependencies. No
standard CLI enrollment bootstrap is installed. Generic or injected protocol
conformance is distinct from an independently qualified provider route.

## Authority and journal decision

The WorkKit coordinator remains the sole integration sender per enrolled
namespace. P7 adds a genuine v2 adapter into the existing P5 operation session,
native write-grant verifier, CAS, durable outbox and one-send dispatch/readback.
It does not copy v1 operations, rewrite signed grants or upgrade a store. Both
endpoint cell ownership and global v1/v2 ownership require protected external
proof. Quiescence, external retention and unresolved effects block migration.

The initial directional fixture uses ordinary `System.Title` fields in two
separate synthetic Azure project/tenant scopes. It proves deterministic mapping
and adapter/coordinator guards only. GitHub comments are a separate P5 route;
they do not establish GitHub bidirectional fields. Persons, relationships and
completion/lifecycle dispatch remain explicitly blocked/manual where their
semantics cannot be established.

## Acceptance artifacts

The fresh delivery manifest under `build/workkit-p7/` indexes exact source hashes,
executed commands, logs, generator comparisons, review receipts and packaging
outputs. Failed development checks are retained separately and never count as a
passing gate. Final acceptance must have no unresolved blocking review finding.
Cross-builds establish buildability only; Unix IPC fixture execution establishes
only the tested platform behavior, not production enrollment.

## Independent gates

| Gate | Status |
|---|---|
| P7 core implementation and fixture acceptance | See sealed acceptance manifest; delivery requires all checks passing |
| Bidirectional provider routes | Pending separately per direction, operation, authentication, scope and platform |
| GitHub webhook ingress | Fixture conformance; live ingress qualification pending |
| Native mutation | Blocked; P6 observation boundaries unchanged |
| P5 live operations | Pending separately per provider/operation/authentication/scope/platform |
| Migration activation | Blocked; no namespace or sender transfer |
| Production activation | Unauthorized |

No credential, real provider request, public webhook registration, model call,
native task mutation, service enrollment or production activation is performed
by this offline acceptance. See the [operator guide](../workkit-bidirectional-integration.md)
for interfaces, exact provider scope and recovery procedures.
