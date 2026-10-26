# WorkKit P6 observation-core delivery evidence

Baseline: P5 merge `af456c4731673568de0396d197e81841148c463a`, PR #91.
Delivery work date: October 26, 2026. This is source implementation and offline
conformance, not a native mutation release or live-route qualification.

## WK-060–066 checklist

| Package | Implemented deliverable | Acceptance evidence |
|---|---|---|
| WK-060 | Closed `workkit-native/v1` records, generated schemas, official API-source inventory and adapter/implementation pins | Strict-decoding, generation checks and contract review |
| WK-061 | Five-host deterministic assignment projections; blocked operation intents | Projection/identity/dependency/unsupported-operation fixtures |
| WK-062 | Exact native-only/managed candidate bindings and historical validation | Cross-session/owner/principal/workspace/input tests; absent/stale runtime-fence callbacks block current validation |
| WK-063 | Owner-scoped Codex/Claude imports; protected OpenCode GET port | Correlation, duplicate, identity, privacy, schema, timeout and incomplete-capture tests |
| WK-064 | Blocked native apply; read-only reconciliation; existing create-intent ambiguity primitives reused without native enrollment | P6 blocked-command fixtures and P5 outbox crash/lost-response tests; protocol-only |
| WK-065 | Explicit progress links and delegation to P5 comment/update planning | Full provider/work identity, state/value mapping, ownership and deterministic replay checks |
| WK-066 | CLI/SDK parity, operator guide, CI checks and distribution integration | Immutable acceptance logs, independent hash-bound reviews, extracted-install smoke and source manifest |

## Reproducible checks

```text
go run ./scripts/workkit/native-schemas -check
go run ./scripts/workkit/native-fixtures -check
go test ./kits/workkit/sdk ./kits/workkit/sdk/command -run Native -count=1
python3 scripts/testing/verify-workkit-native-offline.py AOK WORKKIT
```

The final run under `build/workkit-p6/acceptance-003` records exact commands,
exit codes, logs and before/after source identities. The final create-only bundle
is `build/workkit-p6/delivery-20261026-001/manifest.json`; its asset hashes bind
checks and review receipts. Consult those results for checks actually executed;
this document does not renew historical CI, observations or qualifications.
Historical P0–P5 evidence and prior attempts remain unchanged. Acceptance attempt
001 was stopped after review found CLI blocked-draft status propagation; it is
retained as interrupted evidence and is not a passing delivery check. Attempt
002 passed full Go/race/crash tests but found a pre-existing unkeyed literal in
the additional vet check. The behavior-preserving keyed-field correction is
included in the final source and review; prior results remain preserved.

The acceptance run covers Go 1.22 full/focused/race/crash tests, Python regressions,
kit/design validators, temporary-copy design generators, native schema/fixture
reproduction, empty-PATH/no-credential/no-network smoke, six-target CGO-disabled
builds, packaging and extracted installation. Buildability is separate from native
platform certification. The unrelated local `aok-tool-worker.exe` is excluded.

## Independent gates

| Gate | Status |
|---|---|
| P6 observation implementation | Implemented; delivery requires all source-bound acceptance checks and final independent review |
| Native task mutation | Blocked: no proved direct API, native admission or writer enrollment |
| OpenCode live observation | Pending per exact version/protocol/auth/session/platform |
| Codex/Claude evidence imports | Parser conformance; no live-session or owner qualification transferred |
| P5 provider live operations | Pending separately per provider/operation/auth/scope/platform |
| Migration activation | Blocked; no state/sender ownership cutover |
| Production activation | Unauthorized |

The standard binaries install no protected native read bootstrap or native
writer. Fixture callbacks, API documentation, native completion and registry
entries cannot qualify a route. No live calls, model invocations, task mutations,
credential provisioning, services or activation operations form part of offline
acceptance. See the [operator guide](../workkit-native-observation.md) and
[API source inventory](../research/workkit-p6-native-api-evidence.json).
