# Codex + GitHub live-gate service delivery evidence

Date: 2026-10-24. Scope: completion work inside P3–P10; no additional phase.

## Delivery status

| Gate | Status |
| --- | --- |
| Protected service and worker implementation | Implemented, opt-in |
| Fixture conformance and independent code review | Passed |
| Credential/key/TLS/operator enrollment | External, not supplied or activated |
| Native Codex identity and control qualification | Blocked pending independently measured live evidence |
| P3 direct attribution and settlement | Blocked where attribution/reset uncertainty remains |
| GitHub read/write route conformance | Pending scoped live qualification |
| Verifier isolation and external anchor retention | Pending independent platform/custody qualification |
| P4 quality and demand cohorts | Must independently qualify; fixtures supply no claim-bearing outcomes |
| First P9 rehearsal / ordinary experiment | Not run |
| Production activation | Disabled; promotion requirements unchanged |

The implementation removes the missing concrete service-assembly seam; it does
not clear any live gate through registration, keys, fixtures or cross-compilation.
No live Codex, Docker or GitHub invocation was performed. No actual credentials,
keys, service deployment, tracker mutation, pilot or production grant was created.

## Implemented components

- `cmd/aok-enrolled` and `internal/enrolled/bootstrap`: root-pinned, signed,
  closed configuration; explicit offline inspection; TLS authority, anchor and
  verifier listeners; fixed one-operation workers. Empty compiled root blocks
  activation before configuration access or credential resolution.
- `internal/enrolled/codex`: pinned read-only App Server supervision, clean
  environment, bounded frame/byte/time limits, external native-identity bindings,
  authority capture leases and ordinals, normalized P3 exports without raw frames.
- `internal/enrolled/github`: pinned `gh` capture with explicit broker identity,
  clean environment, conservative repository/argv scope and process supervision;
  exact-operation activated P8 service assembly and existing HTTP writer reuse.
- `internal/enrolled/authority`: signed role/scope/time-bound live attestations;
  exact complete bindings; cumulative parent budgets; trial and operation
  consumption; current epochs; permanent parent stop; capture leases/results;
  durable nonce rejection; runtime/readiness/guard/witness/collector adapters.
- `internal/enrolled/anchorserver`: separately retained authenticated CAS append
  protocol, pre-provisioned genesis, signed sequence-bound create-only appends,
  bounded history checks and corruption refusal. No reset/truncate/provision API.
- `internal/enrolled/artifacts` and `verifier`: authenticated proof-bound immutable
  candidate transfer, policy-bound custody, measured Docker adapter, exact pinned
  command and controls, explicit daemon lifecycle and cleanup confirmation.
- CLI service contexts preserve worker shutdown cancellation. Existing P7
  heartbeat checks revalidate the external authority during execution.

The worker supports `codex-quota`, `github-capture`, `calibration`, `experiment`,
`github-dispatch`, and `github-reconcile`. It reuses existing P3/P7/P8 controllers,
not a second executor/accountant/acceptance system. `PilotExecution` remains
uninstalled. An ordinary P4-selected evidence experiment is distinct from
production promotion. The distributed stock `aok` installs no live services.

## Independent review and corrections

Implementation used Luna-medium workers; a separate Luna-high reviewer reviewed
integration and authority boundaries. Review resulted in corrections for:

1. Mandatory measured executor identity, and proof authorization before upload.
2. Explicit container create/start/wait/stop/remove/absence observation.
3. Full authority binding-map checks, Unix process groups, Windows collector
   refusal and repository-scope validation.
4. External anchor protection against restored SQLite state, complete logical
   state/schema commitments against tampering, and persistent request nonces.
5. Worker cancellation propagation into trial and tracker operations.
6. Exact readiness-report reconstruction through the protected policy/validators;
   forged gate/time/expiry/status inputs fail, while shorter validity is allowed.
7. Cross-instance anchor CAS uses the sequence observed with the prior tip;
   deterministic barrier tests prove one winner and valid chain reopen.

The final review found no additional concrete bypass. The reviewer ran focused
checks; primary acceptance also ran the full authority TLS fixtures with local
loopback permission. Test fixture permissions do not authorize real services.

## Acceptance results

All following checks passed on the final implementation:

| Check | Result |
| --- | --- |
| Full `go test -p 2 ./...`, Go 1.22.12 | Passed |
| Broad relevant `-race` suite including enrolled services, P3–P10 runtime/accounting/recovery and CLI | Passed |
| Python regressions | 268 tests, four skips, passed |
| Python entrypoint/smoke compilation and kit validation | Passed |
| Stock and enrolled offline smoke, empty PATH/disposable HOME/no credentials | Passed |
| All four smoke harnesses with macOS network denied | Passed |
| CGO-disabled builds: both binaries × Darwin/Linux/Windows × amd64/arm64 | Twelve builds passed |
| Go release builder, checksums/bundles/installers | Passed |
| Extracted macOS Intel installer and installed delivery regressions | Passed, two installed-flow tests |
| Whitespace validation | Passed |

The fixture suite covers signature/tamper/expiry/origin failures, exact identity
and binding mismatches, unsafe paths, immutable outputs, privacy canaries,
concurrent budget admission, epoch and clock changes, replay, stopped state across
renewal/reopen, database restoration/tampering, lost anchor replies, cross-instance
CAS, redacted quota exports, scoped GitHub transport and verifier cleanup failures.

The reference anchor's application append-only behavior cannot defend against
an administrator rolling back its entire storage domain. Retention/WORM,
independent custody, recovery, mutable-executable/store TOCTOU resistance and
native identity measurement remain actual deployment gates. The collector
verifies external native-account/HOME/environment references; it does not itself
measure identity from an account label. Windows cross-builds are not native
collector/platform certification. Aggregate quota state is not causal attempt
attribution, and unknown obligations remain held.

The operator guide is [enrolled service operation](../enrolled-live-services.md).

Retained acceptance logs and source digests are indexed in
[the delivery manifest](evidence/codex-github-live-services/manifest.json).
These are development acceptance records, not live qualification evidence.
