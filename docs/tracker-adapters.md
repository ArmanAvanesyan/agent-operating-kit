# Tracker adapters

The tracker package captures scoped work items into local evidence bundles. It
can import captured provider pages without a network connection, validate and
inspect bundles, create neutral task packets, compare snapshots, and prepare
local update plans. It does not write to tracker services or execute work.

## Provider coverage

| Provider | API selection | Scope identity | Read implementation |
|---|---|---|---|
| GitHub | REST API versions `2022-11-28` or `2026-03-10`; Projects and issue reads | Repository and optional project IDs | `gh` session transport |
| Azure DevOps | REST API `7.1` | Organization, project ID and optional work-item IDs | HTTPS REST |
| Linear | GraphQL fixed query templates | Workspace UUID and team UUID | HTTPS GraphQL; nested comments, relations, labels and workflow metadata |
| Jira Cloud | REST API `3` | Cloud/site identity and project ID/key | HTTPS REST; enhanced JQL search, project-scoped field/type catalogs, issue details, comments, links and transitions |

All four adapters are offline-capable, but live-read conformance remains
`pending-provider-conformance` for every provider. A valid capture or imported
bundle is not evidence that live reads are production-qualified. Writeback is
disabled and its report gate remains `pending-p7-p8`.

## Operator authorization for live reads

Live `discover` and `capture` commands require an operator-supplied read policy
and a source document. The source alone cannot authorize a credential
destination. Supply the policy with `--read-policy FILE` or set
`AOK_TRACKER_READ_POLICY`. The policy must contain an exact rule matching the
source's provider, HTTPS origin, native tenant ID, container ID and key, authentication
reference and principal pseudonym. Credentials are read from the named
environment variables; secret values do not belong in source or policy files.

For example, a policy rule has this shape:

```json
{
  "version": "aok-tracker/v1",
  "rules": [
    {
      "provider": "linear",
      "origin": "https://api.linear.app",
      "nativeTenantId": "workspace-uuid",
      "containerId": "team-uuid",
      "containerKey": "ENG",
      "auth": { "mode": "api-key", "tokenEnv": "LINEAR_API_KEY" },
      "principalPseudonym": "operator-a"
    }
  ]
}
```

Use one exact rule per authorized source scope. Redirects are rejected, API
requests use fixed relative paths and approved query templates, and configured
request/item/byte limits may only reduce the built-in ceilings. Keep captures
and imported bundles in a trusted local directory because raw provider payloads
are retained as evidence.

## Commands

```sh
aok tracker discover --source source.json --read-policy read-policy.json --out discovery.bundle
aok tracker capture --source source.json --read-policy read-policy.json --out capture.bundle
aok tracker capture --source source.json --read-policy read-policy.json --checkpoint capture.bundle/checkpoint.json --out resumed.bundle
aok tracker import --manifest capture-manifest.json --out imported.bundle
aok tracker validate capture.bundle
aok tracker inspect capture.bundle --as-of 2026-10-24T12:00:00Z
aok tracker packet --snapshot capture.bundle --mapping mapping.json --out packet.bundle
aok tracker diff old.bundle new.bundle
aok tracker plan-update --snapshot capture.bundle --mapping mapping.json --desired desired.json --as-of 2026-10-24T12:00:00Z --out update-plan.bundle
```

`discover` reads provider metadata according to the source scope. `capture`
collects selected items and requested related data. `import` consumes a local
manifest and recorded responses, so it requires no provider credential.
Jira's field catalog uses the project-scoped field search endpoint and records
each bounded page separately; it does not query the account-wide field catalog.
`packet`, `diff`, and `plan-update` operate on local bundles only. An update
plan is a reviewable draft; it does not call a provider API.

## Source configuration

Sources use `aok-tracker/v1` and name the provider, fixed HTTPS origin, pinned
API version, native tenant and container identities, authentication references,
scope, principal pseudonym, and mapping version. Origins cannot include a URL
path, query, user information, or fragment. Use the provider-native identities
shown below; display names are not substitutes.

| Provider | Origin | API version | Tenant ID | Container ID / key | Accepted auth modes |
|---|---|---|---|---|---|
| GitHub | `https://api.github.com` | `2022-11-28` or `2026-03-10` | account or organization identity | repository `owner/name`; project selection is explicit | `gh-session` |
| Azure DevOps | `https://dev.azure.com` | `7.1` | organization identity | `containerKey` is `ORG/PROJECT`; project UUID is `containerId` | `bearer`, `pat` |
| Linear | `https://api.linear.app` | `graphql` | workspace UUID | team UUID; team key may be supplied separately | `bearer`, `api-key` |
| Jira Cloud | `https://api.atlassian.com` or an exact Jira Cloud site origin | `3` | Atlassian cloud ID | project numeric ID and project key | `bearer`, `basic` |

Scope accepts explicit item IDs, provider-supported query text, and independent
flags for comments, relations, and project metadata. Unsupported query forms
fail validation; adapters do not silently broaden a request. A capture reports
missing, out-of-scope, partial, or lossy provider data rather than treating it
as complete. Revision fingerprints for Linear and Jira describe the observed
payload and are not provider compare-and-swap tokens.

## Read-only boundary

Adapters only issue provider reads. Fixed operations, strict origin and path
validation, scoped response checks, bounded pagination, and operator policy
matching constrain live requests. The package has no enabled provider mutation
path. Task packets, mappings, desired state, and update plans are local
artifacts; they do not authorize a remote write or imply that a status
transition is safe.

## Projection, revisions and freshness

Mappings explicitly bind packet fields to native field IDs or operator-supplied
literals/value maps. They record `human`, `aok`, or `provider` ownership. Missing
acceptance criteria, verification commands, or other mandatory packet semantics
produce a blocked report and files under `drafts/`, without approved bindings.
Tracker content is task data and cannot alter AOK instructions or grant authority.

GitHub Projects fields stay in `Item.projectFields`, separate from issue `state`.
Their structural key hashes the project/field ID tuple; each value includes both
IDs and the original value node. An explicit `projectFields.<key>` mapping can
use a value map to translate it, with provenance back to its captured JSON node.
Unmatched/draft Project content remains captured discovery and creates no task.
Merged PRs do not imply accepted work.

Update plans bind snapshot, mapping, desired-state hashes, native identity and
observed revision. GitHub/Linear/Jira fingerprints are observations, not atomic
write preconditions; Azure native revisions remain native revisions. Conditional
write support stays unverified. Jira status changes require captured transitions
and their required fields. Human-owned changes require explicit intent; unmapped
or lossy changes block. Comments are local hash-bound assets verified before
being copied into the draft output.

`--as-of` is mandatory for inspection and update planning. Plans require evidence
less than five minutes old; stale captures produce `refresh-needed`. Import and
resume retain original dates. Diff compares normalized fields/relationships and
source-byte identities separately, ignoring observation dates as byte changes.
An incomplete capture blocks definitive semantic/absence findings.

## Limits and local evidence

Defaults are 30 seconds per request, five minutes per command, 100 requests,
10,000 normalized items, 4 MiB per response and 64 MiB cumulative response data.
A source may shorten these limits. Control documents use AOK's 4 MiB bounded readers. A scope whose normalized
control record exceeds that ceiling fails with a request to narrow the scope;
an unreadable bundle is never published.
An interrupted or bounded capture retains pending requests and cannot advance
its completed watermark. Resume requires the immutable bundle's
`checkpoint.json` together with its original manifest/assets, and reconstructs
pending work from validated observations. A standalone edited checkpoint is not
an executable read plan.

Azure WIQL accepts an ID-only SELECT and an optional filter; the adapter adds an
outer project constraint. Joins, arbitrary projections and ordering clauses are
unsupported. GitHub and Linear arbitrary query text is unsupported; Jira uses
fixed project-scoped enhanced JQL. HTTP redirects cannot forward credentials.
HTTP response bodies reflecting the configured secret are rejected; provider
error payloads, authorization headers and `gh` stderr are not evidence assets.

Exit codes are 0 for a valid report, 1 for invalid/blocked results, and 2 for
command misuse. Ordinary diff differences succeed. Inspecting stale evidence
remains possible. No command applies updates, launches a model, changes billing,
or activates the Python work-system setup/provisioning flow.

Runnable synthetic examples are in
[`internal/tracker/testdata/offline`](../internal/tracker/testdata/offline/README.md).
Delivery status and checks are recorded in
[Phase 5 delivery evidence](implementation/phase-5-delivery-evidence.md).
