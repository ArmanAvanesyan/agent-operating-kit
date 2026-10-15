# Phased implementation plan

All phases are **proposed**. Draft schemas/examples are design assets and do not complete
any operational phase. This plan refines the existing
[workflow implementation plan](../../research/development-workflows/implementation-plan.md).
AOK owns desired state/adaptation; the external executor/runtime owns live runs.

| Phase | Contracts and implementation work | Acceptance criteria |
|---|---|---|
| 0 — Design review | Agree naming/versioning, ownership, invocation modes, instruction trust, permissions, registries, output/run/lifecycle boundaries and adoption rules | Field ownership/defaults documented; valid and invalid fixtures; existing/proposed distinction reviewed |
| 1 — Operational definitions | Promote reviewed Agent/Profile/Assignment/Output, Invocation/Input, Context, Registry/Environment, Result/Run, Lifecycle, Conformance, Evaluation and Migration drafts into supported contracts | Dependency-free AOK validation extended deliberately; existing role/task/pack/adapter behavior remains compatible; unknown versions/fields rejected |
| 2 — Registries and composition | Implement typed bindings, dependency graph, reference pinning, parameter/input validation, role composition, context assembly, action permission intersections and output paths | Missing dependencies/cycles/conflicts block; selection does not authorize launch; no arbitrary commands/secrets from imports; no widening by override |
| 3 — Complete resolution and run evidence | Produce immutable complete snapshots with transitive hashes; define requested/resolved/observed values, nullable unknown usage, action receipts and terminal envelope validation | Reproducible manifests; no false completed status; blocked fixture cannot launch; runtime state kept outside AOK |
| 4 — Native adapter conformance | Detect/parse/normalize/adopt/render/compare/verify with source locations, sidecars, version/scope matrices; OpenCode and Claude project scopes first | Native/executor-backed/guidance-only/unsupported per field; semantic round trips, denial tests, drift and credential redaction evidenced; required unenforced fields block |
| 5 — Environment readiness and safe installation | Discover host/model/tool/MCP/skill/plugin state; enforce root/network/action policies; dry-run install/launch, isolated application and rollback | Discovery sends no inference and performs no install; authorization checked separately; native inherited permissions inspected; user files preserved; idempotent installation |
| 6 — Target expansion | Verify Codex/Cursor/Gemini/OpenClaw/Hermes native agent surfaces; add agy adapter and target plumbing independently | Evidence for each format/scope/version; no support inferred from model inventories or existing generic bundle support |
| 7 — Lifecycle and executor integration | Implement suspend/resume, checkpoint integrity, timeout/cancellation, bounded retries, idempotency/reconciliation and external executor handoff | Unknown-result writes never blindly retried; terminal attempts immutable; cancellation records in-flight effects; budgets have metering/enforcement owner; explicit compensations |
| 8 — Evaluation and controlled routing | Run pinned task fixtures repeatedly; score outputs, factual grounding, permissions, tool reliability, latency/cost; publish versioned results and thresholds | No routing from not-run results; reviewed measured recommendations; explicit provider/fallback transitions; variance and evaluation limits documented |
| 9 — Evolution and migration rollout | Implement version compatibility, deprecation notices, pinned transformations, dry-run diff/backups/re-resolution and rollback | Unknown versions block; loss cannot silently weaken controls; old runs retain manifests; rollback restores config without claiming remote side-effect reversal |

## Dependency order and release gates

Contract agreement precedes promotion. Registry/input/action semantics and complete
snapshots are priorities before native launch. Adapter fixture work can proceed after
agreement, but syntax alone does not authorize execution. Phase 5 requires a conformant
enforcement path. Target expansion is incremental; unsupported targets remain explicitly
unsupported. Recovery follows evidenced run records. Routing follows real evaluation.
Migration design is required from phase 0; automated transformations ship in phase 9
only once two actual contract versions exist. No fictional initial migration is needed.

Initial experimental operations: validate, resolve, adapter import/inspect/export/compare,
install-plan and evaluate. These names describe intended operations, not existing CLI
commands or approved final syntax. Parsing and inspection are read-only. Installation,
connection preparation, inference and remote mutations are separately authorized actions.

## Validation matrix

- Shape/input: missing/unknown fields, unsafe/duplicate YAML keys, invocation exclusions,
  invalid input schema, mismatched parameter defaults and execution modes.
- Registry/context: dependency cycles, wrong binding types, unavailable required document,
  unknown credentials, token overflow, inherited instructions, context redaction/retention.
- Policy: scoped git/GitHub/release actions, shell/MCP bypass, path traversal and symlinks,
  network/delegation widening, conflicting layers, expired/missing authorization evidence.
- Model: unavailable/expired selector, inherited defaults, effort/variant/budget mismatch,
  ignored sampling settings and unsupported provider switches.
- Resolution/run: all transitive hashes pinned, snapshots/manifest hashes match, observed
  unknowns remain null, ordered events/timestamps, state/result consistency and required outputs.
- Adapter: host syntax/discovery, both round-trip directions, unknown extensions/comments,
  version/scope changes, sidecar loss/conflicts, actual denial enforcement and user-file drift.
- Recovery: transient failure versus denied/auth errors, unknown external action outcome,
  duplicate writes, tampered checkpoint, cancelled in-flight operation and timeout.
- Evaluation/migration: pinned task data, reporting-only metrics, missing repetitions,
  not-run evidence, incompatible versions, lossy transform, rollback provenance and stale runs.
- Regression: current kit checks and Python/Go suites where implementation changes warrant
  them; adapter renders, downstream setup and isolated native discovery tests.

## Pilot and rollout

Pilot repository-explorer on a scratch project with one profile, no delegation, no
fallback and checkpointing disabled. Use the existing exploration/readiness skills,
required Markdown/JSON deliverables and the standard result envelope. An external executor
must enforce the report subtree before writes. Read-only native projection alone cannot
complete the report-writing assignment. Save real run evidence and compare it with the
blocked synthetic fixture. Do not install globally or change persistent model preferences
for the pilot. Expand only after conformance and evaluation gates pass.
