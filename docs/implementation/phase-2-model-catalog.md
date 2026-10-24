# Phase 2 delivery evidence

Status: **P2-core delivered**. **P2 invocation-probe execution gate pending P6**.
Validated locally on 2026-10-23. This is an opt-in static catalog, not a model
selector, quota collector or agent executor.

## Delivered scope

| Package | Result |
|---|---|
| P2.1 | Independently versioned closed schemas, typed records, explicit source policy, exact decimal preservation and deterministic configuration hashes |
| P2.2 | Four source-specific importers, pinned manifests, original assets, field pointers, original dates and categorized guidance |
| P2.3 | Offline import, validate, inspect, refresh and semantic/provenance/freshness diff; private immutable bundle writer |
| P2.4 | Scoped three-state compatibility checks and optional digest-bound catalog evidence in agent resolution; legacy resolution preserved |
| P2.5a | Typed redacted probe evidence, exact configuration/profile binding, six outcome fixtures and Import → Load → Check coverage |
| P2.5b | Pending P6 transport; no live probe or host execution implemented or performed |
| P2.6 | Independent review, operator documentation, CLI/distribution checks and regression gates |

The pinned four-source import produces 18 entries, 67 capability assertions and
17 guidance records. The existing research files are unchanged. Their incomplete
host/configuration scope deliberately leaves compatibility unknown; they do not
prove account access or execution readiness. Fresh exact-profile supplied probe
observations can establish only the explicitly tested dimensions. A supplied
external evidence digest does not establish verified contents or authorship.

## Acceptance evidence

- `TMPDIR=/private/tmp GOCACHE=/private/tmp/aok-p2-cache go test -p 1 ./...`
  passed, including release archive validation and extracted release-builder tests.
- Focused model-catalog, resolver, CLI and shipped executable tests passed.
- `python3 -m unittest discover -s tests`: 267 tests passed, 4 skipped. The
  successful run used permissions needed by existing controlling-terminal and
  macOS process-confinement tests; a sandboxed run cannot exercise those tests.
- `python3 -m py_compile scripts/aok`, `./scripts/aok validate`, Go build,
  `git diff --check` and whitespace checks for new files passed.
- Built import, validate, inspect, diff, refresh and check commands succeeded
  with empty `PATH` and an allow-listed environment without credentials.
  Shipped-executable tests also cover catalog-aware resolution in that environment.
- Two Luna-medium workers implemented import and validation/reporting packages;
  a separate Luna-high reviewer found no remaining blockers after integrated review.

Fixtures cover strict parsing, unsafe paths/symlinks, changed hashes, source-shape
drift, unknown identities, reproducibility, policy trust, TTL expiry, semantic
diff, conflicts, exact scope, alias/history separation and probe failure outcomes.
Capture seals prevent mutated in-memory records from bypassing validation.

## Remaining gates

P6 must provide an authorized transport that enforces the synthetic test profile,
one attempt and a 30-second timeout before live invocation acceptance can pass.
No automatic retry, fallback, repository-content probe or promotion to task quality
is provided here. P3 capacity accounting and P4 ranking/selection remain separate.

See the [operator guide](../model-catalog.md) for commands, formats and limits,
and the [workflow roadmap](end-to-end-workflows.md) for downstream dependencies.
