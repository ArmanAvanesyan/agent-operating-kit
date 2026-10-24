# Phase 10 delivery evidence

Phase 10 supplies opt-in offline implementation and fixture conformance. It does
not complete broader production support. Qualification is tracked independently
for each host/version/platform/authentication/configuration/account route.

## Delivery matrix

| Package | Implementation | Fixture conformance | Live route qualification | Platform certification | Production activation |
|---|---|---|---|---|---|
| Compiled registry, v2 evidence/migration | Implemented | Closed parsing, exact identity, replay and preservation checks | No qualification from registration | Pending by route | Disabled |
| P7 v2 launch integration | Implemented through existing claims/fences/accounting/launch controls | Scope mutation and durable-intent ordering checked | Codex and Claude remain independently blocked | Pending | Disabled |
| OpenCode | Bounded v2 preparation/transport/replay | Synthetic protocol fixture | Blocked; independent controls/account/quota/conformance required | Pending | Disabled |
| agy | Descriptor and blocked discovery reporting | Unsupported transport disposition | Blocked; machine transport not verified | Pending | Disabled |
| Cursor | Bounded v2 preparation/transport/replay | Synthetic protocol fixture | Blocked; independent controls/account/quota/conformance required | Pending | Disabled |
| Gemini | Bounded v2 preparation/transport/replay | Synthetic protocol fixture | Blocked; independent controls/account/quota/conformance required | Pending | Disabled |
| OpenClaw | Bounded v2 preparation/transport/replay | Synthetic protocol fixture | Blocked; independent controls/account/quota/conformance required | Pending | Disabled |
| Hermes Agent | Descriptor and blocked discovery reporting | Unsupported transport disposition | Blocked; machine transport not verified | Pending | Disabled |
| Go methodology/work-item/work-system parity | Implemented scoped commands, no Python launcher | Python oracle byte/semantic comparisons and conflict tests | Provider discovery requires injected read-only reader | Go build matrix only | Not runtime authorization |
| FDD/DSDM/AUP lifecycle | Signed catalog transition checks; P7 revision journal and admission/execution/acceptance hooks | Role, replay, revision and evidence fixtures | Protected role/evidence services pending; profiles remain scaffolded | Pending | Disabled |
| AM | Explicitly selected lightweight guidance content | Optional/default selection tests | Not an execution capability | Content only | No universal modeling gate |
| Jira Data Center | Separate tracker v2/API 2 capture, normalization, packet/draft projection; pinned TLS root boundary | API scope, pagination, rich-text, transition and provenance fixtures | Read and operation-specific write qualification pending | Pending | Writes blocked |
| GitHub/Azure provisioning | Deterministic additive plans, dedicated signed grants, P7 one-send journal, guarded injected apply/reconcile and external activation boundary | Drift, system process, authority, lost response, readback and duplicate-send tests | Qualified provider provisioning writers/authority enrollment pending | Pending | Default CLI apply blocked |
| Windows preservation | Native probe protocol and strict report evaluator | Report/negative fixtures; script parser check | Not applicable | Native Win11 amd64 local NTFS delivery qualification pending | Not certified |
| Codex Cloud | Separate descriptor and discovery blocker | Unsupported route disposition | Blocked pending approved bounded remote transport and independent P2/P3/P6/P9 evidence | Pending | Disabled |
| Campaign proposals/evaluation/comparison | Advisory static DAG; signed P4 outcomes; frozen bounded soft-policy replay | Cycle/leakage/hard-gate and immutable output checks | Qualified fixed-DAG prerequisite pending | Not applicable | No dispatch or automatic adoption |

“Fixture conformance” means protocol or policy handling under supplied synthetic
inputs. It is not evidence of account access, live controls, subscription quota,
quality or acceptance. New host quality/demand cohorts are never pooled with
Codex/Claude or another account. A passing schema or fixture cannot clear a live
gate. The first existing Codex or Claude route must qualify under P9 before
broader production activation.

## Retained blockers

The [P9 matrix](phase-9-delivery-evidence.md) remains in force: Codex and Claude
P6 live gates are independently blocked; live collectors, protected verifier and
identity services, current revocation/custody/witness projections, real pilot
outcomes and production promotion remain pending. GitHub, Azure DevOps, Linear,
Jira Cloud and Jira Data Center retain independent live read/write gates per
operation and authentication mode. P3 cross-window settlement remains
fail-closed. No credit purchase, billing fallback, credential switching, reset
redemption, resume or autonomous delegation was enabled.

Native Windows testing was unavailable on this macOS host. The probe reports
actual delivery preservation, interruption and restore recovery as pending;
filesystem demonstrations are not substituted for those scenarios. Cross-builds
and extracted macOS installer checks establish no Windows certification.

## Contracts and authority

P2/P3/P4 wire formats are unchanged. V1 signed bytes and readers remain intact;
new host routes use opt-in v2. Connector deployment distinction is limited to
six workflow record kinds under `aok/v1alpha2`; `jira` stays Cloud-only. Migration
is evidence preservation with activation disabled, not a store activation tool.
Outstanding holds, fences, consumed send IDs and revocations are not released by
copying evidence. P9 SQLite snapshots include all journal tables; activation
still needs quiescence and an independently protected anti-rollback authority.

P7 owns local execution, capacity consumption boundaries and recovery. P8 owns
acceptance. The standalone v2 runner explicitly refuses execution. A native
session/task/result never becomes an AOK acceptance or tracker completion.
Provisioning consumes its stable logical ID before sending and verifies typed
readback. Ambiguous responses require reconciliation and cannot be resent.
Plain methodology snapshots are integrity projections; approval consumers use
`VerifiedMethodologySnapshot` with current protected role/evidence verification.

## Acceptance and independent review

Two GPT-6 Luna medium workers implemented host/runtime and parity/platform slices.
A separate GPT-6 Luna high reviewer examined the integration. Review fixes cover
actual input/launch binding at P7's start boundary, universal readiness gates,
exact methodology binding, duplicate JSON, failed terminal events, credential
field rejection, pinned Data Center TLS roots, typed provisioning readback,
ambiguous resource names, signed governance history verification and aggregate
campaign source/capture bounds.

Observed acceptance on this checkout (Go 1.22.12):

| Check | Result |
|---|---|
| `go test -p 2 ./...` | Passed, including release packaging tests |
| Relevant `go test -race -p 2` packages | Passed across hosts, runtime/store, readiness, pilot, usage, tracker, provisioning, campaigns and methodologies |
| Python `unittest discover -s tests` | 268 run; 264 passed and four skipped |
| Go/Python parity oracles | Passed generated-byte and canonical-report cases |
| `./scripts/aok validate`, Python compilation, `git diff --check` | Passed |
| PowerShell 7.6.3 `Parser.ParseFile` | Syntax passed on macOS; no Windows execution |
| Empty-PATH/no-credential P9 and P10 smokes with OS network denied | Passed |
| CGO-disabled cross-builds | Darwin/Linux/Windows amd64 and arm64 passed; not platform certification |
| Release bundles/installers and extracted-install checks | Passed release construction and macOS Intel disposable install, installed validation without Python, delivery flow and stale-evidence checks; no publication |

The initial Python run inside the outer development sandbox could not exercise
`/dev/tty` or nested OS confinement. Repeating the same suite outside that outer
sandbox passed all tests above; it used disposable test roots, not real host
credentials. A race run overlapped source edits and encountered a compiler import
error; the frozen-source rerun passed. No failing acceptance check is hidden by a
qualified live or production status.

Reproduce the extended smoke with
`python3 scripts/testing/verify-phase10-offline.py /absolute/path/to/aok --deny-network`
on macOS, outside an enclosing sandbox that forbids creating the inner sandbox.
Release construction uses `go run ./cmd/aok-release --dist NEW-TEMP-DIRECTORY`.
The release constructor emits five installer-target binaries; the sixth
Windows/arm64 target is separately compiled with `CGO_ENABLED=0`, as in CI.
Extracted installer validation uses
`python3 scripts/testing/verify-delivery-release.py --release-dir DIRECTORY --out REPORT`.
Native platform and provider qualification are not performed by those checks.
The operator interface is documented in
[broader hosts and optional workflows](../broader-hosts-optional-workflows.md).


Local acceptance artifacts are under `/private/tmp/aok-p10-release-verified` and
`/private/tmp/aok-p10-installer-report.json`. They are test outputs, not a
published release. Windows ARM64 is a separate CGO-disabled compile artifact.
Descriptor identity covers registry/argument-building code and the normalizer;
it is not a digest of every AOK dependency. Protected runtime/controls enrollment
must independently bind the complete AOK bridge build. Matching a descriptor
alone never qualifies a route or permits carrying qualification across builds.
