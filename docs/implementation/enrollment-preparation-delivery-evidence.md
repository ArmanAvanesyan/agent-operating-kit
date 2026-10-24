# Enrollment preparation delivery evidence

Date: 2026-10-24. Existing P3–P10 completion work; no phase added.

Implemented `aok-enrolled prepare --out NEW_DIRECTORY` and
`preflight --bundle DIRECTORY --as-of RFC3339 [--json]`. Preparation creates
40 deterministic, typed, non-executable drafts, an immutable manifest/README
and an editable operator inventory. Preflight reports safe-read/integrity,
missing fields, supplied-key envelope signatures and explicit live gate blockers.
It does not resolve credential values, issue signatures, execute native tools,
contact services or authorize a run. Supplied signatures remain signature-only;
other evidence remains unverified. Operational admission uses the existing
protected validators and independently pinned trust.

The generated handoff is at `build/enrollment/codex-github-first-live-run/`,
with its report at `build/enrollment/codex-github-first-live-run-preflight.json`.
These are local generated artifacts, reproducible using the commands in the
[operator guide](../enrollment-preparation.md). Signed inputs are deliberately
absent. The saved report has 103 findings including 16 missing route/operator
fields, seven missing credential references, 23 missing external inputs/configs,
42 valid immutable manifest/template checks and 15 pending live/quality/production
gates. Exit 1 is the expected blocked preflight result.

Checks passed: full Go 1.22.12 suite, new enrollment/CLI race tests, Python
268 tests with four skips, Python entrypoint compilation, kit validation,
empty-PATH/credential-free smoke with and without OS network denial, formatting
and whitespace, and six CGO-disabled aok-enrolled cross-builds for Darwin/Linux/
Windows amd64/arm64. Cross-building is not native platform certification.
Two Luna-medium workers and an independent Luna-high reviewer were used.

The [evidence manifest](evidence/enrollment-preparation/manifest.json) preserves
source/output hashes and acceptance logs. No live Codex, GitHub, Docker, credential
lookup, key generation, signing, service activation or remote write was performed.
Live controls/identity/capacity, provider read/write/readback, verifier isolation,
retained anchor custody, acceptance/recovery, P4 cohorts, first P9 trial and
production promotion all remain pending. Missing post-run artifacts must be
created only after the corresponding actual candidate or provider effect exists.
