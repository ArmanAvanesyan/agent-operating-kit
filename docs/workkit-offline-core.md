# WorkKit P1 offline core

WorkKit P1 provides static contract validation, read-only workspace resolution and
inactive AOK import. It has no provider transport, login, native-session operation,
store migration or send permit. Existing AOK commands retain their original
formats and effects; `aok work` is a new opt-in namespace.

## Build and first success

```sh
go build -o /absolute/path/workkit ./kits/workkit/cmd/workkit
go build -o /absolute/path/aok ./cmd/aok
workkit workspace validate --workspace kits/workkit/testdata/corpora/one-provider/workspace.json --json
aok work workspace inspect --workspace kits/workkit/testdata/corpora/native-only/workspace.json --json
```

Public resources live in `kits/workkit/api/v1alpha1`; application services are in
`kits/workkit/sdk`. Consumers can parse exact bytes with `sdk.Parse`, decode a
validated resource into its typed API, or resolve a workspace with `sdk.Service`.
Raw bytes and canonical bytes are defensive copies. Raw SHA-256 identifies exact
source bytes; canonical content SHA-256 uses RFC 8785. A YAML and JSON rendering
may share content identity while retaining different raw-byte reference pins.

## Workspace layout and limits

The workspace is an explicit JSON/YAML file beneath its configuration root; by
default the root is its parent. Other resources are indexed only beneath
`resources/`. Assets are read only from explicit typed references beneath
`assets/`. References carry kind, ID, author revision and exact file-byte hash.
Provider maps and namespaced extensions are opaque; ref-shaped data there is not
traversed. No environment expansion, remote schema fetching or credential access
occurs. Duplicate resource/native identities, missing references, changed hashes,
unsafe paths, symlinks and operation dependency cycles block resolution.

Documents are limited to 4 MiB and 64 nesting levels, configurations to 1,024
resources and 64 MiB total. JSON preserves numeric lexemes and rejects duplicate
keys, trailing values, invalid Unicode and nonfinite numbers. Restricted YAML
rejects tags, anchors, aliases, merges, duplicate/non-string keys, multiple
documents, implicit dates and non-JSON numeric spellings. Use explicit `null`.
Author revisions are positive safe integers; `1`, `1.0` and `1e0` identify the same
author revision while raw byte hashes remain different.

Numbers that would change decimal value during canonicalization reject. In a
provider-value or extension position, use the closed exact-decimal wrapper:
`{"decimal":"0.123456789012345678901"}`. Number-only structural fields do not
accept this wrapper. The serializer is pinned to Cyberphone JCS revision
`19d51d7fe467`; upstream serialization vectors and the stricter WorkKit admission
policy have separate tests.

Static validity never establishes trusted capability evidence. User-supplied
verified bindings and capability reports remain supplied data; workspace reports
always return `authorizationGranted:false`, `executionReady:false` and
`liveQualification:"not-established"`. Missing externally owned policies cannot
be replaced with a similarly shaped local Mapping resource.

## Inactive AOK imports

```sh
workkit migrate inspect-aok --source internal/tracker/testdata/offline/github --format aok-tracker/v1 --as-of 2026-10-25T12:00:00Z --json
workkit migrate plan-aok --source internal/tracker/testdata/offline/github --format aok-tracker/v1 --as-of 2026-10-25T12:00:00Z --json
workkit migrate import-aok --source internal/tracker/testdata/offline/github --format aok-tracker/v1 --as-of 2026-10-25T12:00:00Z --out /absolute/new-directory --json
workkit migrate verify /absolute/new-directory --json
```

Supported sources are tracker manifests in `aok-tracker/v1` or `/v2`, and one
explicit WorkItemBinding, HostTaskBinding, SyncOperation or SyncReceipt record in
`aok/v1alpha1` or `/v1alpha2`. Tracker imports use the existing provider-specific
validators and captured pages without a live transport. Workflow imports read
only typed evidence assets, not sibling authority, runtime or credential files.
All original asset bytes, native IDs, revision kinds, timestamps and original
digest fields are retained. An original AOK digest is never relabelled a WorkKit
canonical digest. No database is opened.

Exact tracker fields become ConnectionProfile and WorkItem projections where
representable. Credential mapping remains unresolved. Data Center connections
require original TLS trust bytes; a trust digest alone does not fabricate an
asset. Relationships, project fields, comments, rich text and unsupported numbers
remain in original assets with explicit losses. Jira Cloud and Data Center
identities remain distinct. Native sessions and task IDs in workflow bindings
are preserved without launching or attaching to a host.

Workflow records generally lack connection origins, profile/mapping references,
revision semantics or original observation times required by the new model.
Their import is preservation-only with partial diagnostics, not a manufactured
WorkBinding, OperationPlan or receipt. SyncOperation and SyncReceipt IDs remain
unresolved; a successful old receipt is not reissued as WorkKit acceptance.
Missing original observation time is `null`, never the supplied inspection time.

The import writes a private, fresh directory containing `assets/original/`,
optional `resources/`, and a final `bundle.json` MigrationManifest. The manifest
binds all files, the inspection time and the compiled conversion implementation
pin. Failed/partial writes do not receive a completed final seal. Verification
requires the exact indexed files and hashes and rejects active projected
bindings/routes. `activate` remains false; validation cannot activate a sender.
Hashes establish integrity, not authorship or independence.

## Result and compatibility contract

Standalone JSON emits the domain `workkit/v1alpha1` CommandResult. `aok work`
emits the suite CommandResult with the entire domain result in `spec.data`, its
original `domainExitCode`, diagnostics and a supplemental effect summary. Both
call the same service and return the same exit code. Effects distinguish local
writes from provider/native effects; provider and native effects remain `none`.

Exits: 0 complete; 1 operational failure; 2 misuse/invalid input; 3 blocked or
unsupported; 4 conflicting identity/hash; 5 uncertain effects; 6 partial import.
A useful preservation import can therefore return 6. `--as-of` is mandatory for
migration inspection/planning/import; it does not backdate or renew authority.
Help advertises only implemented P1 commands. The other proposed command groups
are not placeholder live operations.

## Schema compatibility and delivery

The design schemas were synchronized with two documented alpha additions:
`common.$defs.decimal` is a closed exact-decimal value wrapper, and
MigrationManifest.originalObservedAt accepts null for absent source observations.
Historical P0 snapshots remain unchanged. Existing AOK wire formats and execution
profiles are unchanged.

`python3 scripts/workkit/sync_contracts.py --check` checks the adjacent authored
design when available. CI uses `--design kits/workkit` to verify generated types
against the repository's committed schema mirror. `pin_implementation.py --check`
checks the compiled conversion source pin. No generator runs in a product read.

P1 is offline implementation/fixture coverage. Store activation, live provider
and host qualification, native platform certification, and production activation
remain independent blocked or pending gates. Go 1.22 and six-target builds are
reported with acceptance evidence; cross-compilation does not certify Windows
or other native controls. This phase adds no installer or package publication.

See the [P1 delivery evidence](implementation/workkit-phase-1-delivery-evidence.md)
for acceptance checks, compatibility decisions and remaining activation gates.

P2 read commands and their separate qualification/activation boundaries are documented in [read capture and federation](workkit-read-federation.md). P1 command semantics remain unchanged.
