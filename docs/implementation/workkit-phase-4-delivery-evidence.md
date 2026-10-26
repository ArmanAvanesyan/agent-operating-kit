# WorkKit P4 implementation and delivery evidence

Date: October 26, 2026. Baseline: P3 merge `f87307e` (PR #89).
This delivery concerns source and fixture conformance, not live activation.

| Package | Delivered implementation |
|---|---|
| WK-040 | Closed workkit-setup/v1 operational records, generated schemas, deterministic fixtures; existing domain formats preserved |
| WK-041 | Typed setup metadata boundary, five-provider P2 projection, scoped raw assets and field/category provenance; unavailable metadata stays unknown |
| WK-042 | Deterministic narrow additive field/option drafts, domain SetupPlan and raw-evidence/recomputation validation; no sender |
| WK-043 | Catalog-bound field/lifecycle/person validation and preview; ownership, exact decimals, enum domain and Project/issue separation |
| WK-044 | Existing P2 local views plus provider-view drafts and manual guidance; native comparisons/APIs without translators remain unknown |
| WK-045 | Purpose-bound candidate verification, protected trust/revocation/consumption ports, durable guarded local selection and retained holds |
| WK-046 | Shared CLI/SDK, compatibility alias, CI/source packaging, operator guide, independent review and immutable acceptance manifest |

The [operator guide](../workkit-setup-mapping.md) is the authoritative supported
operation matrix. Five generic synthetic fixtures exercise contracts/planning;
provider-shaped transport fixtures separately exercise native parsing and preview.
Neither category establishes live behavior. Missing default P2 metadata for views,
people, transitions, contexts or process association remains explicit and blocks
any conformance check that requires it. Embeddings must supply independently
qualified, exact-scope metadata before operational binding selection.

Acceptance logs and source/artifact hashes are captured in a fresh directory
under `build/workkit-p4/`. They include failed development attempts where present;
only final source-bound successful runs count as acceptance. The independent
review receipt binds its verdict and executed checks to the final source scope.
Historical P0–P3 evidence and the unrelated `aok-tool-worker.exe` are preserved.

Required delivery checks cover full Go 1.22 tests, relevant races and synthetic
crash/abandoned-lock recovery, Python regressions, kit/design validators,
reconciliation checks, generated records and temporary-copy reproducibility,
empty-PATH/network-denied commands, six-target CGO-disabled builds, packaging and
extracted/installed-source checks. Exact outcomes belong to the acceptance
manifest rather than being inferred from this checklist.

| Gate | Status |
|---|---|
| P4 source implementation | Implemented within the operator guide's supported routes and explicit capability limits |
| Fixture conformance | Final successful command exits and independent review recorded separately |
| Default CLI binding selection | Blocked: no protected setup trust/operational enrollment installed |
| Injected local selection | Synthetic verification, conflict, revocation, interruption and retained-hold tests only |
| Provider/auth/platform live qualification | Pending independently per exact route; prior blocked/pending gates unchanged |
| Migration activation | Blocked; original stores/senders, uncertainty, fences and retention remain authoritative |
| Production activation | Unauthorized; no readiness or production promotion occurs |

No credentials, native credential stores, actual providers, protected services,
model execution, tracker mutations or store cutover are required by these checks.
No commit, push or publication is part of the implementation request.
