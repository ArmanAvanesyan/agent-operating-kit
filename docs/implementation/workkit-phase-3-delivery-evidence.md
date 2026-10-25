# WorkKit P3 implementation and delivery evidence

Date: October 26, 2026. Baseline: WorkKit P2 merge
`a7f2a4d628629429a8d1e3319e57897d5bbb7605` (PR #88). Implementation remains on
`codex/workkit-p3-toolchain-auth`; this request does not commit, push or activate
anything. The unrelated existing `aok-tool-worker.exe` is excluded and untouched.

## Completion against WK-030–036

| Package | Source implementation and fixture acceptance |
|---|---|
| WK-030 | Closed access/broker/native/registry/state contracts; exact signed input/output/policy/implementation bindings; historical P1/P2 formats preserved |
| WK-031 | Bounded explicit-root discovery, byte-only inspection, authorized version probes, directory/dependency-aware runtime and extension-tree pins |
| WK-032 | Verified binary/full archive layouts, official Azure wheel mapping, side-by-side updates, staged failure preservation and emitted tool/extension pins |
| WK-033 | Concrete Keychain/Secret Service/Credential Manager backends, private broker IPC, cross-process CAS/tombstones, protected local state, Unix durable holds and Windows SQLite journal/SID/ACL implementation |
| WK-034 | Explicit native gh/az drivers, labeled acli hybrid, Jira confidential OAuth/site gateway and Linear PKCE/refresh; no native global-cache mutation or credential fallback |
| WK-035 | Shared standalone/umbrella CLI, immutable connection add/list/inspect/remove, broker-to-P2 pinned snapshots, private basic identity mapping, incomplete-read handling and scope checks |
| WK-036 | Independent review, focused/full/race/crash regressions, build/distribution checks, operator guide and sealed acceptance/source manifest |

The prior partial delivery remains immutable at
`build/workkit-p3/delivery-20261026-001/`, manifest SHA-256
`e9c1be9702c31938d54cec2291c4cf6a0fa1c1441d4a683e870bfb002068b812`.
It accurately recorded source gaps at that earlier observation; completion work
neither rewrites it nor renews its qualification dates.

The completion acceptance directory is `build/workkit-p3/acceptance-002/`.
Every attempt, including compile/fixture failures corrected during development,
remains preserved. Final check records and the independent review bind the final
source/pin; initial/interim passes are not substituted for final checks.

## Review corrections

Independent review prompted fixes for forged readiness in unsigned auth receipts,
receipt/state durability before unlock, DC profile/CA binding, broker generation
sentinels/tombstones, cross-scope namespaces, helper ownership/cleanup, bounded
runtime trees, exact native origins/credential modes, registry identity/size and
byte-preserving storage, official extension wheel layout, and broker-read input
TOCTOU/deadline/username/partial-result handling. No sandbox rule or production
policy was weakened to make a fixture pass.

## Acceptance and distribution

The final acceptance records cover Go 1.22.12 full/focused/race suites, synthetic
multiprocess/crash and nested-helper cleanup, Python regressions, kit validation,
both design validators, reconciliation/P0 preservation, schema/fixture generation
and temporary-copy generator reproduction. Credential-free empty-PATH and
network-denied smoke checks exercise built commands without live dependencies.

AOK, WorkKit and the broker are built CGO-disabled for all six OS/architecture
targets. macOS Keychain helper compilation and a compiler/source/binary manifest
are separate checks. Packaging/extracted-source builds and disposable installed
delivery verification do not publish a release or establish native certification.

Exact command exits, logs, source/artifact hashes and review receipt are in the
fresh completion evidence bundle. No actual user credential store, real provider
request, protected service, model/agent run, tracker mutation or store cutover is
used by these tests. Local TLS/callback and in-memory native-store fixtures are
synthetic. API documentation/index lookup during development is not conformance.

## Independent gate matrix

| Gate | Status |
|---|---|
| P3 source implementation | Implemented within the explicit supported formats/routes in the operator guide |
| Fixture conformance | Recorded final acceptance and independent review required; no fixture qualifies a route live |
| Native platform certification | Pending per OS/tool/store/runtime/filesystem; Windows NTFS/ACL/power-loss and Secret Service/Keychain behavior need actual enrolled evidence |
| Provider/auth live qualification | Pending independently per provider/account/origin/API/authentication/scope |
| Migration activation | **Blocked** on preserved state, quiescence, explicit transfer authority and recovery obligations |
| Existing AOK ownership | Runtime/accounting/acceptance/authority/anchor and current tracker senders retain ownership |
| Production activation | **Not authorized; unchanged** |

Supported v1 installation formats and native observation routes are deliberately
bounded. Unsupported system/package-manager formats, native global SSO/cache
mutation and absent upstream machine-readable identity are explicit capabilities,
not hidden fallback implementations. See the [operator guide](../workkit-toolchain-auth.md).
