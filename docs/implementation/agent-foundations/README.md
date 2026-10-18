# Agent foundations delivery contract

Status: static foundations and all eight native adapter projections are implemented
in the Go CLI. The native-only release remains incomplete: three host-loading gates
are open and runtime enforcement is unverified. See the
[operator guide](../../native-adapter-operations.md) for commands and limits.
The historical [agent specification](../../proposals/agent-spec/README.md) and
[planning proposal](../../proposals/planning-decisions-optimization/README.md)
remain design assets. Their illustrative unresolved references do not form a
runnable operational corpus. This contract governs the new Go implementation;
existing Python commands and existing task/role schemas remain compatible.

## Contract reconciliation

- Canonical authoring is UTF-8 JSON, without BOM, duplicate keys, trailing values,
  non-finite numbers or unknown fields. Limit each document to 4 MiB and nesting
  to 64 levels. Native parsers also reject duplicate keys; YAML rejects tags,
  aliases and merge keys. Native unknown fields are retained as inert provenance.
- Use the existing envelope `apiVersion: aok/v1alpha1`, `kind`, and `metadata`.
  API versions select a schema, independently of definition versions. Unknown
  versions fail closed. Operational schemas live under
  `internal/contracts/schemas/` and are embedded in the binary, with no network
  resolver. Proposal schemas are not silently promoted or rewritten.
- Operational metadata IDs match `^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`.
  Definition versions are exact three-component numeric versions (no ranges).
  References use `{kind, id, version}` and resolve locally; there is no implicit
  latest selection. Duplicate kind/ID/version tuples fail. Nested identifiers
  must be unique within their owner. References cannot change expected kinds.
  Content hashes are separate from version strings and detect mutable inputs.
- Typed Go contracts and closed operational schemas must agree. Schemas enforce
  shape; semantic checks enforce references, cycles, defaults, profile selection,
  input/output compatibility and policy. Required unresolved references fail;
  explicitly optional unresolved dependencies are reported without being used.
- File references resolve relative to their owning document within the explicit
  corpus root. Output destinations are relative POSIX paths under the explicitly
  selected output root. Reject absolute paths, parent traversal, symlink escape,
  collisions, and implicit overwrites. Inspect existing parent components before
  writes. Do not follow imported commands or expand shell/environment expressions.
- Resolution is deterministic for identical inputs: stable ordering, canonical
  serialization, source SHA-256 hashes including transitive files, no timestamps
  in hashed configuration. Permissions intersect across assignment, profile and
  agent; no layer can grant a denied capability. Unsupported required enforcement
  blocks readiness. Syntax acceptance never establishes runtime enforcement.
- Validation stages are parsing, schema, semantic references, compatibility and
  readiness. Diagnostics contain stable `code`, `source`, `field`, `severity`
  (`error`, `warning`, `info`) and `message`; reports separately expose status
  (`valid`, `invalid`, `blocked`). Validation never grants authorization.
- Exit codes: 0 successful validation/resolution, 1 invalid or blocked, 2 command
  misuse. A requested output directory receives a blocked resolution report when
  the directory itself passes safety checks. An unsafe output path is not written.
- Planning computes DAG eligibility only from supplied, current evidence. Failed,
  unknown or stale prerequisites, failed invariants, missing inventory coverage
  and exhausted replan limits block eligibility. Revisions must reference the
  exact predecessor. Decision results enforce boolean/choice/score/ranking and
  abstention semantics and required evidence; results never authorize actions.
- Optimization validation checks dataset separation and protected-field boundaries
  only. No optimization, inference or workflow dispatch is included.

## Public interface

New functionality initially requires the built Go binary, not `scripts/aok`:

```text
aok agent validate <directory>
aok agent resolve <assignment> --out <directory>
aok agent native inspect|import|export|compare --target <target> --scope <scope> --input <path>
aok planning validate <directory>
aok planning ready <plan>
aok decision validate-result <contract> <result>
```

Native import/export additionally require `--out`; compare requires a second
explicit input. Native operations never write installed/global configuration.
Every command supports machine-readable diagnostics. Exact flags and output
filenames must be covered by CLI tests when implemented.

## Delivery sequence and gates

Each row is a separate focused PR from current main. Workers use gpt-6-luna at
medium reasoning; an independent Luna reviewer uses high reasoning. The primary
agent integrates, inspects the exact diff, commits, pushes, opens/attaches PRs,
checks feedback, and squash-merges only the reviewed head with green required
checks. Agent review is not human approval. Use at most two implementation
workers and one reviewer, with isolated worktrees for concurrent edits.

| Stage | Deliverable | Status |
|---|---|---|
| 1 | This reconciliation and [native inventory](native-inventory.md) | Merged: [PR #48](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/48) |
| 2 | Offline schemas, typed envelope/references, strict parsers, positive/negative fixtures | Merged: [PR #49](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/49) |
| 3 | Typed models, registries, invocation/context/assignment and reference semantics | Merged: [PR #50](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/50) |
| 4 | Deterministic resolution, immutable evidence, permission/path protections | Merged: [PR #51](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/51) |
| 5 | Planning DAG, revisions, traceability, decision results | Merged: [PR #52](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/52) |
| 6 | Shared native interface, provenance/sidecars, semantic comparison | Merged: [PR #53](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/53) |
| 7 | OpenCode native adapter | Merged: [PR #54](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/54); project loading verified on 1.18.35 |
| 8 | Claude Code native adapter | Merged: [PR #55](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/55); project discovery verified |
| 9 | Codex native adapter | Merged: [PR #56](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/56); full loading gate open |
| 10 | Cursor native adapter | Merged: [PR #57](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/57); loading gate open |
| 11 | Gemini native adapter | Merged: [PR #58](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/58); project discovery verified in the follow-up evidence |
| 12 | OpenClaw native adapter | Merged: [PR #59](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/59); isolated Linux loading verified in follow-up evidence |
| 13 | Hermes native adapter | Merged: [PR #60](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/60); direct installed CLI profile loading verified; normal launcher selection unverified |
| 14 | agy native adapter and target registration | Merged: [PR #61](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/61); loading gate open |
| 15 | End-to-end, distribution and operator documentation | Implemented in this change; release completion blocked on native gates |

For every implementation PR: relevant targeted tests; kit validation; Python
regressions and compilation; Go tests/build; relevant render smoke tests; both
proposal checkers. Distribution acceptance additionally requires Go 1.22 CI,
cross-platform builds and Go binary execution without Python. Negative fixtures
must test actual behavior rather than mirror implementation structure.

The operational corpus must include a fully pinned Goal → Plan → Assignment →
Agent/Profile/Output/Verifier chain, an unresolved blocked case, and fixtures for
syntax, shape, references, cycles, versions, paths and policies. Adapter gates
require isolated native loading evidence, supported-field semantic round trips,
sidecar loss/drift detection, unknown-field preservation, scope/version differences,
and blocked unsupported required controls. Portable Markdown or neutral render
bundles do not satisfy native completion. Missing hosts or suitable formats remain
explicit blockers while independent PRs continue. The overall release remains
incomplete until all eight native gates pass.

## Delivery-stage boundaries (historical)

Stage 2 adds [operational contract parsing](../../../internal/contracts/README.md)
and [inert native parsing](../../../internal/nativeparse/README.md). It does not
expose the planned commands or resolve references. Schema-valid fixtures can
still be unresolved and blocked. Standalone planning schemas remain subsequent stages; proposal checkers continue to check only
historical proposal assets.

Stage 3 adds [local agent validation](../../agent-validation.md) and typed
decoding for the initial eleven resource kinds. Static validation does not imply
readiness or authorization. Resolution, planning and native adapters remain open.

Stage 4 adds [static resolution](../../agent-resolution.md), captured source hashes,
permission intersection and non-overwriting report output. It does not dispatch
work or establish native/runtime enforcement.

Stage 5 adds [planning and decision validation](../../planning-foundations.md),
evidence-based DAG eligibility, revision/traceability checks and inert experiment
boundary checks. It does not run workflows, optimizers or model inference.

Stage 6 adds the [shared native adapter interface](../../native-adapter-interface.md),
explicit file capture and private non-overwriting outputs. Client adapters and
native loading gates remain separate work.

## Remaining native-loading gates

OpenCode 1.18.35, Claude Code 2.1.281 and Gemini CLI 0.61.0 have isolated project discovery evidence.
Codex has parser-startup evidence but no verified generated-agent loading;
Cursor has shipped-component discovery but lacks verified full CLI discovery; agy probes returned no
discovery evidence. OpenClaw 2026.9.4 has isolated Linux roster/workspace evidence, with Darwin CLI
loading unverified. Hermes has direct installed CLI profile-list evidence plus separate metadata/SOUL
component evidence; package-manager launcher selection remains unverified.
The three open target gates require versioned isolated native
loading evidence for generated fixtures before this release can be called complete.
No runtime enforcement claim follows from either parsing or discovery. See the
[compatibility inventory](native-inventory.md) and each adapter README for sources.
