# Enrollment integration delivery evidence

Date: 2026-10-24. This closes implementation work within existing P3–P10;
it introduces no phase and activates no route.

## Delivery status

| Gate | Status |
| --- | --- |
| GitHub writer integration | Implemented; fake HTTP POST and complete readback fixtures pass |
| Qualification bootstrap | Implemented; root-pinned signed synthetic operations and one-use anchor fixtures pass |
| Post-run verification/acceptance/handoff | Implemented; genuine runtime-ledger candidate and injected verifier fixtures pass |
| Independent review | Luna-high review completed; identified lineage and freshness issues corrected and re-reviewed |
| Live Codex controls, account measurement and collector attribution | Pending; no native live invocation or credential inspection performed |
| GitHub live read/write and independent readback | Pending; no remote mutation performed |
| Verifier engine/platform controls, identity services and retained anchor custody | Pending; no Docker or service deployment performed |
| P4 quality/demand cohorts, P9 first end-to-end pilot and production activation | Pending; fixture success grants none of these |

The operator guide is [protected services](../enrolled-live-services.md).
The enrollment dependency plan is [operator enrollment](operator-enrollment-first-codex-github-pilot.md).

## Implemented changes

The configured GitHub writer now supplies an explicit immutable issue ID to REST
issue-number mapping, bounded/hash-verified comment loading and durable,
create-only response evidence. Source scope is `issue:<REST number>` while
operation identity retains the immutable provider ID. Authorization, POST and
GET agree on the same issue number. A lost POST reply receives one reconciliation
read and never an automatic resend. Provider responses are observations; an
independent signed readback remains required for P8 completion.

`aok-enrolled qualify --config SIGNED --root PUBLIC_KEY` is distinct from
production `authority.Enrollment`. Its closed record has purpose
`synthetic-conformance-only` and at most five minutes. It supports the fixed
`github-read`, `codex-quota`, `host-conformance`, `verifier-conformance` and
`github-comment` actions. The normal authority still requires its independent
13 live gate proofs and quality evidence where applicable. The bootstrap does
not relax that contract or generate proofs to approve itself.

Every bootstrap operation uses a separately pre-provisioned external anchor
stream structurally derived from stable parent and operation IDs. Compare-and-
append consumes the operation before action IO. A consumed tip cannot become a
renewed starting tip. Lost responses remain consumed/uncertain. Output bundles
are private, non-overwriting, observation-only and explicitly non-authorizing.
The stock executable remains disabled without an operator-compiled root pin.

Codex quota observations occupy a unique, unapproved qualification namespace;
they cannot impersonate production capture ordinals. Host conformance uses the
existing P6 synthetic matrix and NativeRunner, existing measured control proofs,
at most ten starts/thirty seconds per turn and the shorter five-minute deadline.
A positive trusted operator upper bound must cover the entire matrix in the
actual P3 units. The guard checks the exact synthetic profile/configuration,
reservation owner/epoch/fence/lease and current observed fit before each start;
known reset crossings block. The full hold remains uncertain afterward. No
subscription percentage is derived from tokens or aggregate quota deltas.
These are conservative accounting controls, not qualified causal settlement.

Verifier conformance accepts only synthetic fixture candidates with an externally
signed exact candidate/snapshot-bound control proof. It reuses the existing
bounded upload and verifier interfaces, reports output hash/length and outcome,
and never records acceptance or raw private output.

New fixed worker stages `verify-candidate`, `accept-candidate` and
`handoff-candidate` operate on the actual post-run candidate. Verification checks
the protected runtime ledger, current signed acceptance-authority projection,
stopped attempt, checkpoint, captured inputs, configuration/profile and author
lineage. A fresh candidate-bound control proof and separate one-use
`post-run-verification-only` execution grant precede upload. An old proof for a
placeholder candidate fails. A zero exit reports invocation only. Acceptance
requires an independently signed receipt; handoff requires accepted exact
lineage. Updating the signed authority projection intentionally fences earlier
trust/revocation generations; this is not a read-only runtime operation.

## Acceptance evidence

Checks use Go 1.22.12 and local fixtures only. The source-hash manifest and
redacted logs are retained under [enrollment integration evidence](evidence/enrollment-integration/manifest.json).

| Check | Result |
| --- | --- |
| Full Go suite, `go test -p 2 ./...` | Passed |
| Race suites: enrolled, usage/store, runtime/store/staging, writeback, connectors and verification | Passed; includes local crash/recovery and concurrent fixtures |
| Final changed bootstrap package under race detector | Passed |
| Python regressions | 268 tests passed; four skipped |
| Python entrypoint compilation and kit validation | Passed |
| Empty-PATH, credential-free completion/P9/P10/enrolled smoke | Passed |
| Same smoke with OS-level network denial | Passed |
| CGO-disabled builds: aok and aok-enrolled, Darwin/Linux/Windows × amd64/arm64 | All twelve passed; cross-builds do not certify those platforms |
| Release package builder and extracted macOS Intel installer/delivery tests | Passed; local artifacts only, no release published |
| Formatting and whitespace checks | Passed |

The initial restricted Python run could not obtain a controlling terminal or
nest native sandbox tests. Rerunning those fixture tests with the required local
OS facilities passed. A race build overlapped a worker import edit; the suite
was rerun successfully after source freeze. These failed preliminary attempts
are not represented as live service failures or hidden acceptance passes.

Focused coverage includes invalid/tampered signed envelopes, expired grants,
wrong scope/configuration/profile, unsafe/symlink assets, changed hashes,
immutable outputs, renewal replay, parallel anchor contention, lost anchor
responses, cancellation without IO, positive upload-before-execute, actual
runtime-ledger lineage, forged acceptance rejection, explicit inspection-time
freshness and positive GitHub POST/readback after a lost POST response.

The primary integrated the changes. Luna-medium workers implemented GitHub and
post-run fixtures; a separate Luna-high reviewer reviewed authority, provenance,
freshness and replay boundaries. Review found two issues: runtime-origin labels
did not prove ledger lineage, and GitHub dispatch reused an earlier timestamp.
The final implementation checks actual ledger lineage and revalidates grant
freshness at dispatch and provider send with an expiry-bounded context. Re-review
found no remaining direct bypass in those paths. Review itself was static and
never supplies live qualification.

## Remaining enrollment requirements

The operator must still provision separate root/proof issuer custody, TLS and
service principals, independently retained anti-rollback anchor storage, native
account/HOME/environment measurements, exact approved executables, disposable
workspace, protected P3 ledger with fresh observations and explicit matrix
bounds, GitHub broker token/grant and one test issue, verifier engine/image and
measured isolation, current acceptance/revocation signing, and stop/witness
services. Credentials remain external environment references; none was read or
created by these acceptance checks.

Each live qualification action needs its own explicit enrollment and evidence
review. The synthetic observation cannot approve its issuer, grant readiness,
qualify another route, prove task quality or settle unknown cross-window usage.
The first ordinary P9 pilot still depends on qualified P4 cohorts and all live
host/provider/runtime gates. Production promotion remains separately blocked.
