# WorkKit P1 delivery evidence — 2026-10-25

P1 implements WK-010–015 in the existing Go module: versioned contracts,
strict parsing, offline resolution, validation, command results and a
preservation-first AOK import bridge. The [operator guide](../workkit-offline-core.md)
describes the supported commands and limits. The starting revision is
`28f62998e5a62dd34348e0e18e72d2d937da32dd`. Acceptance captured the source on
`codex/workkit-p1-offline-core` before commit and PR publication; the sealed
bundle retains that historical working-tree state. This is not a published
release or an operational activation.

## Delivered boundaries

| Area | Implementation and fixture evidence | Remaining boundary |
|---|---|---|
| Contracts | 24 typed resource kinds; 25 embedded closed schemas; 28 structural examples and 11 existing negative fixtures | Static validity grants no trust, permissions or live support |
| Parsing and identity | Strict bounded JSON/restricted YAML, duplicate and Unicode rejection, pinned RFC 8785 canonicalizer and upstream vectors, separate raw/content digests | Decimal values that cannot retain exact value are rejected; wrappers apply only where the schema permits them |
| Local resolver | Four linked workspace corpora; kind/ID/revision/hash resolution, asset confinement, duplicate detection and operation-cycle rejection | No external policy discovery, credential lookup or remote schema fetch |
| SDK and entry points | Public `kits/workkit/api/v1alpha1` and `kits/workkit/sdk`; standalone `workkit` and opt-in `aok work` call the same application service | Full proposed CLI inventory remains a roadmap; help lists only implemented commands |
| Result projection | Suite envelope retains the complete domain result, original exit code, diagnostics and detailed effects | No automatic retries or additional authority |
| AOK tracker import | Fixtures for GitHub, Azure DevOps, Linear, Jira Cloud and separate Jira Data Center deployment handling; original source assets and digests retained | Connection credentials unresolved; Data Center TLS bytes cannot be invented from a digest |
| Workflow import | WorkItemBinding, HostTaskBinding, SyncOperation and SyncReceipt in both existing AOK versions are preserved | Missing connection, mapping, original time and operation semantics remain explicit losses; records are not promoted into authorized WorkKit operations |
| Distribution | Source archives include SDK, integration, schemas, fixtures, generators and module checksums; extracted-source build and disposable installer checks | No standalone installer publication or native Windows certification |

Imports use fresh private directories, write the final manifest last and never
activate projected bindings or routes. Partial preservation returns exit 6.
Original observation time is null when absent; `--as-of` is inspection time.
All workspace reports keep `authorizationGranted:false`,
`executionReady:false` and `liveQualification:"not-established"`.

The SDK does not depend on legacy AOK internals. Only the
`integrations/legacy-aok/workkit` bootstrap adapter imports existing validators.
No runtime store, sender, protected service or credential source is opened.

## Compatibility decisions

The authored design and generated schema mirror have two explicit alpha
additions: the closed exact-decimal wrapper in `common.$defs.decimal`, and
nullable `MigrationManifest.originalObservedAt`. Historical P0 schemas,
inventory bytes and evidence dates remain unchanged. Existing AOK wire formats,
execution profiles and legacy command behavior are retained.

The suite's active source-map is a new P1 snapshot; historical baseline maps
remain present. Contract synchronization and a compiled implementation digest
are checked by developer tooling and CI. These digests establish byte integrity,
not authorship, qualification or activation permission.

## Acceptance

Local logs live under ignored `build/workkit-p1/`. Attempts `acceptance-001` and
`acceptance-002` remain preserved, including failures. The first full Go run
found a date-dependent existing runtime fixture: its fixed observation date was
checked against wall-clock time. The test now uses the explicit fixture instant
and additionally rejects expiry; production runtime code is unchanged. Another
run overlapped a test-file edit and failed compilation; a frozen-tree rerun
passed. Neither failed log is relabeled as passing.

Final-source checks use `acceptance-003`:

- Full Go suite under Go 1.22.12, serialized across packages.
- WorkKit and legacy bridge race tests, including concurrent parse/resolution.
- Python regression suite: 268 tests, four skips.
- Kit validation, both design validators and reconciliation regressions.
- Historical P0 delivery validation, without recapturing or renewing it.
- Generated contract and implementation-pin checks.
- Built standalone/suite commands with empty PATH and no inherited credentials;
  the same smoke test under a network-denied disposable process policy.
- CGO-disabled AOK and WorkKit builds for Linux, macOS and Windows, amd64/arm64.
- Release packaging, extracted-source WorkKit build and disposable macOS Intel
  installer/delivery verification. Cross-compilation is not platform certification.

The create-only delivery bundle under
`build/workkit-p1/delivery-20261025-001/` indexes the exact logs, source hashes,
commands, results and artifact hashes. It records the uncommitted source state
and excludes the unrelated pre-existing `aok-tool-worker.exe`. No credentials,
private responses or service state enter the bundle.

## Completion gates

| Gate | Status |
|---|---|
| P1 offline implementation and fixture acceptance | Delivered |
| Inactive preservation/import | Delivered, with explicit partial/loss diagnostics |
| Operational store migration and sender cutover | Blocked on state inspection, quiescence and explicit authority |
| Provider/host live qualification | Existing route-specific blocked/pending statuses retained |
| Native platform certification | Separate; six build targets do not certify controls |
| Production activation | Unchanged and not authorized |

Next implementation work can add provider-facing application services against
the public contracts. Provider authentication, active synchronization, native
task mutations and ownership transfer require their own implementation and
qualification evidence. P1 supplies no substitute for those gates.
