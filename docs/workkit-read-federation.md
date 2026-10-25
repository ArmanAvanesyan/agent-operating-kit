# WorkKit P2 read extraction and federation

WorkKit P2 adds immutable read captures, catalog inspection, portable queries and
local view projections. The existing AOK module owns the implementation;
`workkit` and `aok work` call the same SDK. Provider mutation, authentication
lifecycle, store cutover and production activation remain separate gates.

## Build and offline first success

```sh
go build -o /absolute/path/workkit ./kits/workkit/cmd/workkit
go build -o /absolute/path/aok ./cmd/aok
workkit providers list --json
workkit capture import \
  --connection-file kits/workkit/testdata/read/connections/github.json \
  --manifest internal/tracker/testdata/offline/github/manifest.json \
  --as-of 2026-10-24T12:00:30Z --out /absolute/new-github-capture --json
workkit capture validate --capture /absolute/new-github-capture --json
workkit items list --capture /absolute/new-github-capture \
  --as-of 2026-10-24T12:00:30Z --json
```

These examples use synthetic fixtures. Output parents must exist; outputs must
be fresh directories. No network, installed provider CLI or credential is needed
for import, validation, inspection, query or diff.

`kits/workkit/api/readv1` defines `workkit-read/v1`, an operational bundle format
alongside the unchanged `workkit/v1alpha1` domain resources. Generated structural
schemas live beneath that API directory. SDK validation additionally enforces
identity, provenance, bounds, signatures and inactive defaults. A schema-valid
record is not a trusted capability or qualification.

## Explicit connection configuration

A read configuration contains `version`, `connectionId`, the exact P1
ConnectionProfile resource, and `source`, the existing AOK read-source encoding.
The source encoding supplies explicit native lookup keys, principal pseudonym,
authentication references, requested scope and detailed transport limits. A
WorkKit display label cannot supply missing native IDs. Profile origin, API,
provider, container, Data Center deployment/TLS pins and limits must agree.

`--connection-file` is the P2 operational configuration input. Scope is embedded
in that configuration, not changed by an unpinned CLI override. P1 workspace
validation and the complete proposed command inventory remain available as
previously documented; P2 does not implement connection management or login.

The legacy source provider spelling `jira` remains Jira Cloud; the public
registry uses `jira-cloud`. `jira-datacenter` is a separate identity including
origin, base path, native tenant/project and configured server/TLS versions.

## Capture format, continuation and budgets

Bundles contain a final `bundle.json`, exact configuration/source assets,
normalized item batches, catalog batches and, for observed sessions, original
read policy/enrollment and bounded prior-proof lineage. All assets are hashed;
files and directories are private and create-only. Unindexed files, unsafe
paths, symlinks, duplicate identities, unknown fields/versions and changed
assets block validation. JSON null cannot silently replace a required false
boolean or string.

Raw provider data uses exact-number decoding, not whole-response JCS rounding.
Original AOK digest algorithms and bytes retain their original identities.
Normalized item batches are assets, independent of P1's 1,024 configuration
resource index. Catalog pointers identify actual source nodes only when uniquely
resolved; otherwise `provenanceKnown:false` is explicit.

Transport ceilings remain 30 seconds/request, five minutes/command, 100 provider
requests, 10,000 items, 4 MiB/response and 64 MiB total response bytes. Identity
probes share the request/response budget. Source/profile/enrollment limits can
shorten these bounds. Independently, serialized bundle assets, normalized data
and manifests together must fit 64 MiB and each document 4 MiB. An oversized
normalization fails closed with bounded unsealed failure evidence and source
hashes; it does not publish a falsely complete or silently truncated bundle.

Live output is atomically reserved before credential resolution or provider IO.
Interruption may produce an explicitly partial bundle. Evidence preservation has
an independent two-second bound after transport cancellation; it performs no
further provider IO. A failure before a valid capture leaves a private failure
report, not a resumable completed bundle.

Resume requires the complete prior bundle and a fresh signed enrollment binding
its exact digest. Configuration, policy, implementation, transport, principal
and mode changes block continuation before credential access. Outputs cannot be
inside prior evidence. Original successful pages retain their observation dates
and originating enrollment proofs; failed attempts remain in prior immutable
bundles. Lineage is limited to 16 prior hops. A complete source cannot be used as
an implicit request for a new capture. Reads across time are not transactional
provider snapshots.

Import records inspection `--as-of` in the command report and retains original
source observations. It never renews freshness or claims authorship from hashes.

## Catalogs, queries and local views

```sh
workkit catalog containers --capture /absolute/new-github-capture \
  --as-of 2026-10-24T12:00:30Z --json
workkit providers capabilities --capture /absolute/new-github-capture \
  --as-of 2026-10-24T12:00:30Z --json
workkit browse --captures /absolute/capture-a,/absolute/capture-b \
  --query /absolute/query.json --as-of 2026-10-24T12:00:30Z --json
workkit views evaluate --captures /absolute/capture-a \
  --query /absolute/query.json --view /absolute/view.json \
  --as-of 2026-10-24T12:00:30Z --json
```

Queries explicitly declare canonical field types and per-connection native field
IDs. Supported operators are `==`, `!=`, `contains`, `in`, `and`, `or`, `not` and
parentheses. Strings are case-sensitive; numbers compare exactly. Null is a
known JSON value. Missing, unproven, lossy and conflicted/incomplete item fields
are unknown; negating unknown does not produce true. Objects/arrays are not
silently coerced into scalar values. No code, functions, shell interpolation or
arbitrary provider queries execute. Provider-native query execution is not
implemented in P2.

Federation preserves connection/principal context and the complete structural
native identity. It does not merge equal display labels or create copies/routes.
Conflicting snapshots for one connection require explicit resolution. Ordering
is deterministic. Counts mean observed matches; incomplete/stale inputs,
unknown predicates and result limits prevent exhaustive claims. Invalid/tampered
inputs reject the operation; valid partial captures remain queryable with
per-connection coverage. Query freshness requires the supplied `maxAgeSeconds`
and explicit `--as-of`; inspection alone does not assume a freshness policy.

List/table/board views produce data projections, ascending field sort and
explicit grouping before result limits. Unavailable columns remain identified.
Board views require grouping. Timeline rendering and provider view creation are
unsupported. Provider-view discovery is not inferred from Projects fields.

## Implemented read coverage and honest limits

| Provider | Existing read coverage extracted and fixture-tested | Limits |
|---|---|---|
| GitHub | Repository, issue/PR, comments, relationships and Projects field/membership pages | Projects and issues remain separate; provider views and native issue-type catalogs are not implemented |
| Azure DevOps Services | Project/process metadata, work items/revisions/relations, types/fields/states | Existing metadata surfaces only; team/iteration catalog endpoints are not implemented |
| Linear | Explicit team/workspace issues, states/labels, nested connections and relationships | No schema-introspection field catalog or provider views |
| Jira Cloud | Enhanced search, scoped issues, project/type/field metadata, links and per-item allowed transitions | No arbitrary native JQL or global workflow catalog |
| Jira Data Center | Separate API 2 instance/project reads, wiki text and per-item transitions | Server/version/TLS route qualification remains separate; no Cloud assumptions |

`catalog` accepts containers, teams, types, fields, states, transitions and
iterations. It returns captured scoped metadata with provenance, or explicit
unknown/unsupported output. Iterations and other unavailable surfaces are
implementation limits, not merely missing credentials. No empty success implies
an exhaustive native catalog. Provider capabilities distinguish fixture-tested
implementation from observed access and independently live-qualified support.

## Qualification-only live bootstrap

```sh
workkit capture run --connection-file /protected/connection.json \
  --policy /protected/read-access-policy.json \
  --enrollment /protected/signed-read-enrollment.json \
  --out /private/approved-new-attempt --json
```

`discover` uses the same inputs with an enrollment signed for `action:discover`.
`capture resume` additionally requires `--capture PRIOR`. `items capture` requires
explicit item IDs in the configuration.

The operator must provide an external access policy containing exact legacy
read scope and approved Ed25519 issuer keys, plus a signed enrollment binding
configuration/policy/implementation digests, action, absolute output,
principal/native account ID, exact transport, expiry and limits. The signature
preimage is exposed as `sdk.EnrollmentSigningBytes`; no product command generates
keys, signs a real enrollment or starts protected services. Validity is at most
five minutes. A prior bundle digest is mandatory for continuation. The current
compiled implementation digest is reported in provider descriptors.

GitHub P2 bootstrap permits only explicit token-reference injection into a pinned
absolute `gh` executable with an empty inherited environment and fixed argv.
Its actual auth route is `gh-token`; legacy source `gh-session` is a compatibility
encoding, not evidence of native-session qualification. Native `hosts.yml`/
keychain routes remain blocked pending a protected redactor/auth implementation.
The operator supplies exact version output and executable SHA. Other providers
use fixed-origin HTTP with external environment references; Data Center requires
pinned TLS roots. No account/credential/transport fallback, login or refresh is
performed. Identity probes verify the current native principal before item reads.

This path gathers qualification-candidate evidence only. Policy keys are trusted
external operator inputs; signatures identify the approved issuer, not reviewer
independence or runtime readiness. Captured policy bytes cannot self-qualify.
Platform controls, operational revocation/anti-rollback services, actual provider
conformance and independent live qualification remain separate gates. Standard
builds contain no enrolled credentials or qualified production route.

## Delivery and activation gates

The domain/suite envelopes, detailed effects and exits remain unchanged:
0 complete, 1 operational, 2 misuse/invalid input, 3 blocked/unsupported,
4 conflict, 5 uncertain effects, 6 partial. P2 provider/native mutation effects
remain `none`; local evidence writes are reported separately.

Migration imports stay inactive. Existing AOK senders/outboxes and external
runtime, accounting, identity, anchor and acceptance owners remain authoritative.
P2 does not open operational stores, activate bindings, dispatch models, start
services or authorize production. Cross-buildability is not native certification.

See [P2 delivery evidence](implementation/workkit-phase-2-delivery-evidence.md)
and [extraction decisions](implementation/workkit-phase-2-extraction-decisions.md).

P3's [toolchain/authentication boundary](workkit-toolchain-auth.md) adds explicit
access observations and guarded tool actions. It preserves this P2 read bootstrap;
broker-backed reads require their own exact access grant and P2 enrollment.
Native global credential-cache mutation remains unsupported.
