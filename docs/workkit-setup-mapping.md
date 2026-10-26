# WorkKit P4: setup, mapping and verified bindings

WorkKit P4 implements WK-040–046 on the merged P3 baseline `f87307e` (PR #89).
The shared Go services are available through standalone `workkit`, existing
`aok work`, and the explicit `aok workkit` alias. Go 1.22 remains the floor.

P4 performs scoped discovery, offline planning, mapping previews, view drafts and
local binding verification. It does not dispatch provider changes or establish a
new integration sender. Existing `aok work-system setup` remains read-only;
project initialization and P1/P2/P3 wire interpretation are unchanged.

## Commands and input records

```text
workkit setup discover --capture BUNDLE --out NEW
workkit setup discover --connection-file FILE --policy FILE --enrollment FILE --capture-out NEW --out NEW
workkit setup plan --observation BUNDLE --desired FILE --as-of RFC3339 --out NEW
workkit setup validate-plan --plan BUNDLE --as-of RFC3339
workkit mappings validate|inspect --mapping FILE --mapping-context FILE --source-observation BUNDLE --observation BUNDLE --as-of RFC3339
workkit mappings preview --mapping FILE --mapping-context FILE --source-observation BUNDLE --observation BUNDLE --snapshot P2-BUNDLE --as-of RFC3339 --out NEW
workkit setup verify --observation BUNDLE --desired FILE --mapping FILE --mapping-context FILE --as-of RFC3339 --out NEW
workkit views inspect --view FILE --observation BUNDLE --id NATIVE-ID --as-of RFC3339
workkit views plan-create|plan-update --observation BUNDLE --view FILE --parent-id ID --as-of RFC3339 --out NEW
```

All commands accept `--json`. View updates additionally require `--id` naming the
existing native view. P2 local view list/inspect/evaluate commands are preserved. The optional observation-backed
inspection returns only captured HTTPS view URLs passing the configured origin/base-path
policy (or the public GitHub display host); missing, credential-bearing, encoded-path
or query-bearing URLs remain unresolved. No browser is opened.
`setup apply`, remote reconciliation, provider-view execution and `views open`
remain unavailable. No read or plan command performs login, token refresh,
installation, browser launch or provider mutation.

Domain records remain `workkit/v1alpha1`. Closed `workkit-setup/v1` operational
records describe observations, mapping context, evaluations, view drafts, policy,
purpose-bound verification and binding receipts. Strict JSON decoding rejects
unknown/missing fields, duplicate keys and unsupported versions. Domain inputs
also accept the existing restricted YAML encoding. Original bytes/hashes are
retained; freshness evaluation never renews observation dates.

`MappingContext` pins exact mapping and source/destination observation hashes,
required canonical fields and optional selected native container IDs. Empty
selected IDs mean the connection's captured container. A selected GitHub Project
must be an observed direct child of the scoped repository. Cross-provider
mapping never merges their identities or principals.

Output bundles are private, bounded and create-only. Reports retain pinned inputs
and original native assets; `setup validate-plan` rechecks hashes, provenance and
recomputes the report at its original time before evaluating current freshness.
A domain SetupPlan contains no executable operation references: the supplementary
changes are drafts for the later controlled coordinator.

## Provider and operation matrix

| Provider | Source projection | Setup planning | Unavailable metadata |
|---|---|---|---|
| GitHub | Repository and captured Project fields/options; issue/PR, Project membership and field values stay distinct | Additive fields/options on exact existing containers; single-select types use native typename evidence | Uncaptured views, people and workflow transitions remain unknown |
| Azure DevOps Services | Project, captured process/type/field metadata | Additive field/option drafts require the exact associated inherited process; system processes block customization | Uncaptured process association, team/board/state details remain unknown |
| Jira Cloud | Project/type/field metadata; field schema type remains distinct from display label | Existing-field conformance; administrative changes give manual guidance | Missing field contexts/options/transition prerequisites remain unknown |
| Jira Data Center | Separate API/base-path/version/TLS-pinned project and captured metadata | Existing-field conformance; no inherited Cloud administrative APIs | Current P2 discovery does not provide a full field/context catalog |
| Linear | Captured team/states/labels, including paginated metadata | Existing-field conformance; administrative changes give manual guidance | Current P2 metadata does not provide a full setup field/person/view catalog |

All five providers have synthetic observation/planning fixtures. Real provider
response shapes are separately tested through injected P2 transports. These are
protocol fixtures, not live-read or platform certification. A complete item
capture does not imply every setup category exists. Each category records its own
coverage and evidence; missing required metadata blocks conformance.

Protected embeddings may install `SetupMetadataReader` for additional typed
metadata, with exact scope, source assets and provenance validation. Task data
cannot select plugins, arbitrary URLs or shell commands. Connected discovery
uses existing P2 read sessions and P3 credential injection; their signed
enrollment remains qualification-only. Directly captured bytes prove integrity,
not independent operational qualification.

Plans never create projects, processes, workflows or destructive replacements.
Existing resources require native IDs; a same-name collision blocks automatic
reuse. Text/number/single-select setup is supported within captured constraints;
date/iteration planning and unknown types are explicit unsupported findings.
Azure customization requires a proven inherited process association; merely
listing an inherited process elsewhere does not satisfy it. No process-state
creation is introduced by P4.

## Mapping and views

Identity and explicit value maps are non-executable. Enum domains must map to
observed native option IDs. People require explicit IDs and membership in both
selected scopes; no name/email matching occurs. Lifecycle maps require native
states and applicable transition IDs. Missing transitions remain unknown rather
than receiving fabricated identifiers.

Only source-owned fields yield proposed destination values. Human, runtime,
destination and shared ownership requires later explicit coordination. Missing
values remain unresolved; null is distinct and requires known target nullability.
Unknown enums, unsupported transforms and incompatible values block preview.
`preserve-raw` retains evidence but does not repair a required lossy conversion.
Provider numeric lexemes remain exact. Unrepresentable domain numbers are rejected
by the existing P1 contract instead of rounded; no token/numeric coercion is added.

GitHub Project values are resolved using their structural Project/field key and
known native value shapes. They cannot overwrite issue state by label similarity.
Preview retains source pointers and original snapshot evidence. Lifecycle preview
requires current item-specific prerequisites and independent acceptance; it
reports the unresolved requirement without emitting an authorized transition.
Native completion is never business acceptance.

Local table/board views reuse P2's bounded portable query engine and retain
coverage/unknowns. Timeline execution and provider-native query execution remain
unsupported. Provider-view drafts retain the requested definition, exact parent,
known native identity, capability findings and manual guidance. Without a native
definition translator their comparison remains explicitly unknown; they do not
claim a provider administrative API exists.

## Binding verification and local selection

Offline `setup verify` always produces a draft candidate and reports missing
independent operational verification. Schema validity or a qualification-only
capture cannot establish a verified binding.

Connected verification omits `--as-of` and adds `--policy FILE --verification FILE`.
It uses current time and requires externally supplied operational observations,
all conformance checks, and a separate Ed25519 `setup-verification` purpose.
The signature binds exact inputs, scope/principal, implementation, policy,
deterministic candidate digest, output path, exact configuration-root/workspace paths and expected workspace hash.
Policies may shorten the five-minute observation/proof limit. Externally supplied
issuer keys are insufficient alone: protected `SetupTrust.Current` must validate
the independently pinned policy, current issuer/revocation and qualified route.
The ordinary binaries install no such trust callback.

Explicit `--write-binding --workspace FILE --root DIR --expected-workspace-sha256 HASH`
selects a local binding only after protected one-use consumption. It uses a
create-only process-safe lock, durable evidence, exact existing connection/mapping
bytes, retained backup and atomic workspace replacement. Only the relevant binding
reference changes; unrelated configuration is preserved. Outputs cannot overlap
immutable inputs or the domain resources tree. Configuration, referenced assets,
new evidence, proof validity and freshness are rechecked before replacement.

A crash or uncertain effect retains its hold and evidence; no PID/age heuristic,
automatic retry or rollback releases it. Inspect the workspace and retained backup
under independently authorized recovery before any later action. Windows/native
filesystem durability remains separately certified; unsupported semantics fail
closed. Selecting a local binding does not transfer a sender namespace, migrate
stores, release old obligations or activate production.

## Results and delivery gates

Exit codes remain: 0 complete success, 1 operational failure, 2 misuse/schema,
3 blocked/unsupported/unqualified, 4 conflict/staleness, 5 uncertain effects,
6 partial capture/planning. JSON uses the domain CommandResult; the suite preserves
that result and exit code without lossy conversion or added authority.

Implementation/fixture results are recorded separately from native certification,
provider/auth live qualification, local selection evidence, migration and
production. Migration activation remains **blocked**, live qualification remains
**pending independently per route**, and production activation is **unauthorized**.
See [P4 delivery evidence](implementation/workkit-phase-4-delivery-evidence.md).
