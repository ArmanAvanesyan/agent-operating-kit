# WorkKit P2 delivery evidence — 2026-10-25

The implementation extends WorkKit P1 (`ff99ba2`) with the shared read engine,
operational captures, catalog inspection, portable federation and local view
projections. See the [operator guide](../workkit-read-federation.md) and
[ownership decisions](workkit-phase-2-extraction-decisions.md).

## Implementation and fixture matrix

| Component | Delivered behavior | Separate limitation/gate |
|---|---|---|
| Shared providers | GitHub, Azure, Linear, Jira Cloud and separate Data Center read extraction | Each live provider/auth/transport/platform route remains unqualified |
| Read transports | Explicit pinned gh-token runner and injected fixed-origin HTTP; scope checks before secrets, principal probes, shared budgets | Native gh credential-store route blocked; auth/install lifecycle remains P3 |
| Operational records | Closed workkit-read/v1 API and generated schemas; exact source assets, item/catalog shards, private immutable outputs | Hashes establish integrity, not authorship or qualification |
| Continuation | Signed action/output/implementation/prior binding and bounded originating-proof lineage | No transactional snapshot or credential/policy/route fallback |
| Catalogs | Existing scoped metadata and per-item transitions; actual/unresolved source pointers distinguished | Iterations, unavailable team/global catalogs and provider views are explicit implementation limits |
| Queries/views | Exact typed portable predicates, unknown handling, deterministic federation, local sort/group/limit and unavailable columns | No provider-native query execution, timeline rendering or remote view operations |
| Compatibility | Existing tracker APIs delegate to one engine; legacy wire/digest behavior retained | No sender/store migration or qualification transfer |
| CLI/distribution | Standalone/suite result parity, offline smoke, source-package rebuild and six build targets | Cross-builds do not certify native controls or live support |

All reports keep workflow execution, migration and production activation false.
Native completion, provider content, a schema-valid capability or a successful
read cannot create acceptance or write authority.

## Review and acceptance evidence

A separate reviewing agent performed iterative source review. Findings covered
conflict-aware predicates, real view sorting/grouping, deployment identity,
credential reflection, identity budgets, timeout preservation, output overlap,
proof lineage, null/default normalization and exact profile pins/limits. Fixes
and regression fixtures were added before final acceptance. Review is source/
fixture review; it is not independently issued live qualification.

Fresh local acceptance logs and a create-only delivery manifest are published
under ignored `build/workkit-p2/`. Earlier failed compile/test attempts remain
preserved. No old P0/P1 acceptance result is relabeled as a newly executed check.

Delivery checks cover focused Go tests, full Go 1.22 suite, relevant races,
legacy capture byte parity, Python regressions, kit/design validation,
contract/schema/fixture generation, empty-PATH and network-denied offline smoke,
six-target CGO-disabled builds, source packaging/extracted rebuild and disposable
installer verification. The sealed delivery bundle records exact command exits,
source/artifact hashes, the final independent review and any remaining limits.

## Explicit gate status

| Gate | Status and completion condition |
|---|---|
| P2 core implementation and offline fixture conformance | Delivered; final acceptance checks passed |
| GitHub gh-token live-read qualification | Pending exact operator enrollment, credential and independent route evidence |
| Azure, Linear, Jira Cloud, Jira Data Center live reads | Independently pending by provider/API/auth/platform |
| Native gh-session authentication | Blocked; protected secret/redaction and authentication work remains |
| Provider writes and native-task operations | Outside P2; no mutation interfaces exposed |
| Migration activation | Blocked on state inspection, preserved obligations, quiescence and explicit authority |
| Native platform certification | Separate from build/installer checks |
| Production activation | Unchanged, not authorized |

No real provider requests, credentials, service startups, store cutover, model
execution or production activation are performed during this implementation.
P3 can begin from the offline public ports while live qualifications stay gated.

## Recorded final acceptance

All command exits in `build/workkit-p2/acceptance-001` are successful: the full
Go 1.22.12 suite, relevant primary and independent race suites, 268 Python tests
(four skips), kit validation, both design validators, reconciliation and
historical P0 validation. P1 capture files match byte-for-byte for the four
checked-in GitHub/Azure/Linear/Jira fixtures. P1/P2 empty-PATH smoke and
network-denied P2 smoke pass. Twelve CGO-disabled AOK/WorkKit builds cover six
targets. Release source packaging, extracted-source Go 1.22 rebuild and a
disposable macOS Intel installer/delivery check pass. Both design generators
reproduce in temporary copies without modifying historical artifacts.

The final independent review reports no blocking findings, review-scope SHA-256
`81638ba36855f51d4079235c5ac8ebe4fafe7a92f1a737af9dc3d838e024ccf2`, and
implementation SHA-256
`936214f5c6474861b9a32510e58ee862bf3ce37bdeb5dab76db3b4a2f0e97265`.
The create-only local delivery bundle is
`build/workkit-p2/delivery-20261025-001/`. It binds exact source/artifact hashes,
command exits, logs, review and separate gate statuses. It records the
working tree on `codex/workkit-p2-read-federation` before commit and PR
publication. The original sealed snapshot remains historical evidence; this
delivery does not activate a route or publish a release.
The unrelated pre-existing `aok-tool-worker.exe` is excluded and untouched.
