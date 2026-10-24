# Phase 5 delivery evidence

Phase 5 adds a versioned, offline-first tracker capture and draft-planning layer.
Its completion gates are independent:

| Gate | Status | Evidence boundary |
|---|---|---|
| P5-core | Delivered, fixture-qualified | Four adapters, strict contracts, immutable capture/import, projection, semantic diff and draft updates |
| GitHub live-read conformance | Pending | No connected account or live capture was used |
| Azure DevOps live-read conformance | Pending | Bearer and PAT modes require separate scoped account tests |
| Linear live-read conformance | Pending | Bearer and API-key modes require separate scoped account tests |
| Jira Cloud live-read conformance | Pending | Basic and bearer modes require separate site/cloud tests |
| Remote writeback | Pending P7/P8 | No writer, authorization grant, provisioning, recovery or reconciliation is enabled |

P4 remains advisory. Native agent tasks and execution bridges remain P6 work.
Fixture qualification does not establish production API conformance or workflow
completion. Every draft/report persists `authorizationGranted:false` and
`executionReady:false`.

## Implemented packages

`internal/tracker` defines closed `aok-tracker/v1` source, operator read-policy,
manifest, snapshot, checkpoint, mapping, desired-state and update-plan records.
Schemas are generated from the typed records with closed control structures;
provider field values remain raw JSON. Packet projection validates against the
unchanged task-packet schema and emits existing P1 `WorkItemBinding` records.
The P1 connector reader/mapper boundary is implemented; its write mapper returns
unsupported and directs callers to the separate `DraftPlanner`.

| Adapter | Implemented reads and semantics | Fixture API/auth coverage |
|---|---|---|
| GitHub | Repository identity, issues/PRs, paged comments/timeline, Projects/fields/items/field values; separate Project fields with item-bound provenance | REST `2022-11-28`/`2026-03-10`, fixed GraphQL queries, injected `gh-session` runner |
| Azure DevOps | Project identity, process/type/field/state discovery, 200-ID batches, project-scoped WIQL, native revisions, relations and paged comments | REST `7.1`; comments `7.1-preview.4`; injected HTTP bearer/PAT |
| Linear | Workspace/team discovery, issues, states, team/issue labels, comments and relationships with nested cursors | Fixed GraphQL queries; injected HTTP bearer/API-key |
| Jira Cloud | Project/type/project-scoped field discovery, enhanced JQL continuation, issues, paged comments, links, allowed transitions and required inputs | REST `3`; site and cloud-prefixed routes; injected HTTP basic/bearer |

The fixture API/auth matrix describes parsers and mocked transports, not tested
live authentication. Unknown provider fields and rich text survive in original
assets. Lossy derived fields cannot silently replace their originals.

## Acceptance evidence

The implementation uses two Luna-medium workers for provider/test work and a
separate Luna-high reviewer. Review findings were converted into scope,
provenance, connection-shape, resume, immutable-identity and diff regression
tests. The primary integrated GitHub, shared contracts and CLI behavior.

Automated acceptance includes:

- All four captures round-trip through import/load and yield valid packets and
  P1 bindings with explicit complete mappings.
- Exact decimal lexemes, immutable IDs, native revision/fingerprint distinctions,
  field-level provenance and fixed original observation dates are preserved.
- Duplicate keys, unknown versions, changed hashes, unsafe paths, symlinks,
  scope drift and output replacement are rejected.
- Pagination, duplicates, interruptions, scoped resumptions, GraphQL partial
  errors, null/malformed connections and budget ceilings cannot establish
  false completeness. Partial diffs do not imply deletion.
- Authentication/permission/not-found/rate-limit/conflict/cancellation/timeout
  dispositions and retry hints remain visible. Error payloads and stderr are
  excluded; credential reflection and privacy canaries are tested.
- Missing semantics, stale observations, unknown/lossy fields, transition
  prerequisites and human-owned-field changes block drafts as appropriate.
- Project V2 status remains separate from issue state; PR merge never becomes
  acceptance. Tracker text cannot grant AOK authority.

Validation performed with the cached Go **1.22.12** toolchain, module downloads
disabled:

| Check | Result |
|---|---|
| Focused tracker and CLI suites | Pass |
| Full uncached Go suite, including release packaging/extracted-kit tests | Pass |
| Tracker race suite | Pass |
| Python regressions | 267 tests pass, four skips |
| Python compilation, release shell syntax, kit validation, whitespace checks | Pass |
| Built CLI, empty PATH, no credentials, blocked network proxy | Four-provider import/validate/inspect/packet/diff/plan-update pass |
| Identical offline imports and rejected overwrite | Byte-identical bundles; originals preserved |
| CGO-disabled builds | CLI and tracker tests compile for Linux/macOS/Windows, amd64/arm64 |

The existing Python process-confinement/terminal tests need host process-group
access; their sandbox-limited run failed, and the complete authorized rerun
passed with that access. No product changes were made to bypass those tests.

CI now includes tracker race coverage and four-provider, empty-PATH offline
command chains. Synthetic source/manifests, response assets, mappings, desired
changes and read policies live in `internal/tracker/testdata/offline/`.

## Connected promotion

Each provider/auth mode needs an operator-approved scope, a pinned source and
read policy, a disposable test account, pagination and denial evidence, and a
reviewed capture before its live gate can change. A successful read still
cannot establish write permission or atomic conditional updates. P7/P8 must
add authorization, concurrency controls, recovery and readback before any
remote update can be dispatched.

See the [operator guide](../tracker-adapters.md) for commands and constraints.
