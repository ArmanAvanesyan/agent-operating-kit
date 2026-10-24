# Offline workflow contract validation

The Go CLI validates captured work-item bindings, host attempts, run observations
and proposed connector operations. Build a binary from the checkout, then inspect
the synthetic example corpus:

```sh
go build -o build/aok ./cmd/aok
build/aok workflow validate internal/workflowcheck/testdata/corpus --json
```

This command reads explicit local files only. It needs no Python, provider CLI,
credentials or network. It writes its report to stdout without changing input
files. The contributor Python CLI does not provide this command.

## Records

| Kind | Describes |
|---|---|
| WorkItemBinding | Immutable provider identity, captured source revision and hashed packet |
| HostTaskBinding | Exact work item/assignment, host session and attempt identity |
| RunRecord | A supplied runtime observation with checkpoints/output references |
| ConnectorCapability | Declared adapter operations and concurrency guarantees |
| SyncOperation | A proposed typed field update, transition, comment or link |
| SyncReceipt | A supplied observation of dispatch/readback or an unresolved result |

Use `aok/v1alpha1` and exact `{kind,id,version}` resource references, as for agent
and planning contracts. Updated snapshots require a new metadata version. Older
binaries reject unknown kinds; there is no automatic migration. Existing task
packets, project configuration and Python task commands are unchanged.

Place supporting Markdown/JSON under `assets/`; paths are relative to the owning
document and must stay inside the corpus. Symlinks, unsafe path components and
special files are rejected. Asset and assignment hashes identify exact file bytes.
Provider identifiers are opaque and scoped by provider, tenant and container;
display keys and URLs do not establish identity. Multiple attempts for the same
item are permitted, while rebinding an existing attempt to another item fails.

The example has one item, two attempts, a proposed field update and an applied
receipt. It contains synthetic data and does not record a real external write.
Readback evidence is JSON with `operationDigest`, `identity`, `revision` and the
observed operation payload (`fields`, `transitionId`, `commentDigest`, or relation
fields). Applied claims require that captured payload to match the proposed change.
External truth and authorship are not established by supplying a matching hash.

## Reports and boundaries

JSON reports contain `status`, resource counts, structured diagnostics,
`authorizationGranted: false` and `executionReady: false`. Text reports express
the same results. Exit 0 means static validation passed; 1 means invalid/unreadable
input; 2 means invalid command arguments. A workflow corpus must contain at least
one of the six workflow kinds. Validation also runs existing agent/planning checks;
it does not replace separate decision-result or experiment validation.

The same workflow semantic checks run when `agent validate` or `planning validate`
accept these records. Missing pins, changed hashes, conflicting identities,
unsupported operations, mismatched authority declarations and invalid receipts
therefore cannot bypass validation through another command.

An external authority reference is an inert declaration, never permission to
perform an operation. A completed RunRecord is not accepted goal completion.
There is no live connector, credential resolver, execution engine, runtime store,
model selector or usage collector in this phase. Connector interfaces and their
in-memory conformance fixtures establish a future integration boundary only.

See the [Phase 1 contract and acceptance plan](implementation/phase-1-shared-contracts.md)
and the [later integration roadmap](implementation/end-to-end-workflows.md).
