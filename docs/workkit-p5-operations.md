# WorkKit P5: controlled mutations and one-way sync

P5 adds opt-in planning, protected coordinator ports and a new integration
journal. Standalone `workkit`, `aok work` and `aok workkit` call the same SDK.
Standard binaries install **no qualified mutation bootstrap**. They can plan,
validate and inspect local evidence; an enrollment JSON file cannot install a
writer, establish trust, transfer a namespace or activate production.

The baseline is P4 merge `ce86edadf31ab317fa969c328b2e2c7310bbdc05`, PR #90.
Historical P0–P4 evidence and the legacy read-only setup commands are preserved.

## Commands

All commands accept `--json` and preserve domain results within the umbrella
envelope. Exit codes remain 0 success, 1 operational failure, 2 misuse/schema,
3 blocked/unqualified, 4 conflict/stale, 5 uncertain, 6 partial.

```text
items plan-create|plan-update|plan-transition|plan-comment|plan-relate
  --snapshot BUNDLE --observation BUNDLE --desired FILE --mapping FILE
  --binding FILE [--binding-root ROOT] --capability FILE --policy FILE
  --as-of RFC3339 --out NEW
operations list|inspect --store FILE [--operation ID]
operations apply|reconcile|cancel --plan BUNDLE --policy FILE
  --enrollment FILE --out NEW
setup apply|reconcile --plan P4-BUNDLE --operation CHANGE-ID
  --policy FILE --enrollment FILE --out NEW
sync validate|preview --configuration FILE --state FILE
  --source BUNDLE --destination BUNDLE --source-observation BUNDLE
  --observation BUNDLE --mapping FILE --mapping-context FILE
  --source-binding FILE --binding FILE --query FILE --as-of RFC3339
  [--out NEW]
sync conflicts --plan BUNDLE
sync status --store FILE --route SHA256
sync run --once [--initialize] --plan BUNDLE --invocation FILE
  --policy FILE --enrollment FILE --out NEW
sync pause|resume|reconcile --plan BUNDLE [--invocation FILE]
  --policy FILE --enrollment FILE --out NEW
```

`preview` requires a fresh output. `validate` is report-only. Initialization uses
`sync run --once --initialize` and a separately signed `sync-initialize` purpose;
it cannot silently accept differing values. Each bounded run references an exact
`SyncInvocation`, containing separately prepared operation bundles, policies,
short-lived grants and fresh output paths. Reconciliation uses new read-only
purpose grants and new output directories, retaining the original immutable plan.
Effectful commands use current time, five-minute command bounds and the existing
30-second provider request bounds. No command logs in or discovers credentials
as a side effect of planning.

## Provider-operation matrix

| Route | Planning/coordinator implementation | Concrete dispatch boundary | Live gate |
|---|---|---|---|
| GitHub public cloud issues | Explicit comment drafts | Existing typed comment writer/readback | Pending per repository, principal, auth and platform |
| Azure DevOps Services | Source-owned field patches with native revision and provider CAS | Existing revision-tested field writer | Pending per organization/project/process/type/field/auth |
| Linear | Explicit comment drafts | Existing typed comment writer/readback | Pending per workspace/team/auth |
| Jira Cloud | Explicit comment drafts | Existing native-ID/body-aware comment writer/readback | Pending per cloud/project/auth |
| Jira Data Center | Inspectable blocked mutation drafts | Unsupported | Separate adapter/qualification required |
| GitHub setup | Selected P4 additive field/option handoff | Injected existing protected provisioning boundary | Pending per operation; no native setup writer installed |
| Azure setup | Selected inherited-process field handoff | Injected existing protected provisioning boundary | Pending; system processes and option/state changes blocked |
| Item creation | Typed parent/type/schema/correlation intents, existing create outbox | Injected creator port only | No concrete live creator inferred |
| Transitions/relationships/completion | Inspectable blocked drafts | Unsupported until exact controls and independent acceptance exist | Pending |
| One-way sync | Portable selector, exact correlations, paired-baseline previews and bounded coordinator | Each prepared operation independently gated | No route automatically qualified |

Native tasks remain P6. Continuous/bidirectional sync, webhooks, automatic conflict
resolution, deletion propagation and provider-native query execution remain P7
or unsupported. Unlinked items can emit creation proposals when enabled; missing
type/schema and confirmed identity keep dispatch/correlation blocked. No fuzzy
identity matching is performed.

## Protected embedding and ownership

`OperationBackend`, `SyncBackend` and `SetupOperationBackend` in the legacy
integration are opt-in compiled plumbing. The operator must supply protected
bootstrap callbacks; task data cannot select a plugin or install those callbacks.

For item operations, bootstrap supplies the existing AOK grant, revocation,
connector capability, exact native operation and typed writer factory. The
factory must install its received admission callback on `providers.Route.Admission`:
the callback runs before credential resolution and immediately before HTTP I/O.
The callback must not cache a prior approval across credential resolution. Injected creation and setup writer factories receive the same final-admission requirement. Creation rechecks current native grant expiry/revocation and uses a 30-second request context; setup rechecks its protected provisioning authority at the final guard.
`Current` independently validates protected policy custody, P4 operational binding
proof, provider/operation/authentication/platform qualification, namespace owner,
implementation and current revocation. Plain `verified` labels or fixture flags
are insufficient. `Observe` revalidates exact current identities and preconditions.
Writable fields additionally require an explicit independently checked ordinary-field role and supported canonical semantics; opaque IDs or labels cannot prove that a field is unrelated to completion. Completion/state changes currently remain blocked rather than inferring acceptance.

A fresh `workkit-operations/v1` journal has a distinct SQLite application ID and
binds its random identity, canonical path, namespace and owner. Creation is an
operator embedding action, not an ordinary CLI command. It reuses AOK outbox,
CAS, authorization-consumption, revocation, clock and create-intent code. Unknown,
legacy, copied, damaged or nonempty unversioned stores are rejected without
repair. External anti-rollback witness consumption precedes effects. Existing
AOK stores retain their authority; provisioning delegates to its existing
journal without copying operations into a second active sender.

The protected service supplies opaque references for scoped credentials, current
trust/revocation services, an independently controlled anti-rollback witness,
operational binding proof, route-specific live qualification and signed readback.
Secrets remain behind the provider resolver. There is no environment-variable
fallback or ordinary CLI signing service.

## Recovery and bounded synchronization

Prepared operations bind exact bytes/assets, configuration, authentication mode,
transport, principal, capability, mapping, binding, policy and implementation.
Item-operation bundles also retain the existing domain `OperationPlan` and `OperationReceipt` as hashed assets. The receipt references exact domain-plan bytes and preserves the complete journal observation in `workkit/observation`; this projection adds no authority. Logical IDs are stable across repeated requests; changed bytes under an admitted
ID conflict. One-use short-lived authority binds store identity and output path.
The journal records send intent before external I/O. Lost responses, timeout,
unconfirmed cancellation and crashes remain uncertain; a missing effect on a
read does not authorize resending. Reconciliation does not require the expired
original creation send grant and cannot create a new send permit.

A sync route pins native endpoints, mapping/binding revisions and a portable
query. Observation changes do not rename the route. Missing and null remain
distinct; numbers retain their exact JSON representation. Destination changes,
loss, incomplete coverage and unknown values produce conflicts/blockers. Only
source-owned, compatible values can produce proposals.

Before any run operation, a fenced, externally witnessed journal transaction
records the complete pending set and plan digest. Pause/revision checks occur
inside the existing send-intent transaction. Already admitted effects remain
obligations. A crash after admission cannot make the route appear clean.
Read-only reconciliation remains possible while paused. Field baselines advance
only for authoritative confirmed readback, and completed watermarks advance only
with complete coverage and no unresolved operations. Resume preserves lineage
and requires reconciliation first. Target-field ownership is unique within the
journal; external protected enrollment must additionally ensure disjoint ownership
across all enrolled stores and existing AOK namespaces.

## Qualification preparation and activation gates

The [non-executable qualification template](examples/workkit-p5-qualification.json) intentionally contains unresolved operator fields and an invalid implementation pin. Replace these with reviewed values and opaque evidence references; never credential values. `OperationQualificationTemplate` and `ValidateOperationQualification` prepare and
inspect required evidence. File presence and hashes do not qualify a route.
The first intended live route is one disposable GitHub issue comment with an
operator-selected repository/issue, separately scoped write credential, fresh
read identity, independent operation capability, P4 verified binding, protected
authority/witness, one-use grant, provider receipt and independent readback.
No live operation is part of offline acceptance.

Migration activation remains **blocked** until store preservation, quiescence,
uncertain effects, external retention and namespace transfer are verified.
Provider/auth/platform live qualification remains **pending separately**.
Production activation remains **unauthorized**, including after a disposable
qualification. P5 does not activate native tasks, execution, accounting,
acceptance or production authority.
