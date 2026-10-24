# Operational model catalog

P2-core provides an opt-in, offline Go catalog for the four retained model research
exports. The separate invocation-probe execution gate is **pending P6**. This
feature does not rank models, select defaults, collect quota, or execute agents.

## Build and import the retained snapshots

```sh
go build -o build/aok ./cmd/aok
build/aok models import \
  --manifest docs/research/model-catalog-manifest.json \
  --policy docs/research/model-catalog-policy.json \
  --out build/model-catalog --json
build/aok models validate build/model-catalog \
  --policy docs/research/model-catalog-policy.json --json
build/aok models inspect build/model-catalog \
  --as-of 2026-10-23T12:00:00Z --json
```

The manifest and policy pin the existing source bytes. The original exports stay
unchanged. Their dates are historical observations, not claims that the listed
models, offers or account entitlements are available now. Ordinary commands never
fetch URLs, inspect credentials, contact providers or invoke a host CLI.

Output directories must be new and outside the protected inputs, with an existing
real parent and no symlink ancestors. Use a canonical path where a system temp
directory is itself an alias. Completed bundles are never overwritten. Read-only
commands write only to stdout/stderr; input files are not modified.

## Bundle and evidence model

The independently versioned format is `aok-model-catalog/v1`, with companion
`aok-model-manifest/v1`, `aok-model-policy/v1` and `aok-model-probe/v1` schemas.
It is separate from the `aok/v1alpha1` agent/workflow corpus. Keep catalog bundles
outside that corpus, or under its supporting `assets/` subtree.

Each bundle contains normalized entries/configurations, capability assertions,
guidance, lifecycle notices, an import report, captured policy/manifests and the
exact source/evidence assets. Validation replays the source-specific importers
against the captured bytes so editing a normalized claim alone fails validation.
Policy and manifest hashes are integrity inputs, not proof of trusted authorship.

Configuration identity separates host/version, provider, selector/revision,
reasoning mode/effort/budget, generation settings, tier, auth mode, deployment and
tool profile. Unknown dimensions are explicit nulls, never wildcards. Display
labels and aliases cannot silently become executable model IDs. Optional manifest
selector mappings must name the source entity and its exact JSON pointer; the
mapping is a declared correspondence, not observed availability.

| Category | Meaning | Default validity |
|---|---|---|
| `published-declaration` | A captured published statement | 30 days |
| `observed-cli-listing` | A configuration appeared in a retained listing | 7 days |
| `repository-guidance` | Editorial or repository-derived advice | 90 days |
| `successful-invocation` | A supplied, scoped successful probe observation | 24 hours |
| `reproduced-evaluation` | A separately supplied evaluation category; research benchmark references do not become this | 90 days |
| Pricing/offer fields | Dated financial/offer statements | 1 day |

Policy `validityHours` explicitly overrides these defaults. Freshness is evaluated
as `observedAt <= asOf < expiresAt`; no command supplies an implicit wall clock.
Inspection retains expired, future and untrusted facts. Current external policy
may shorten a captured validity interval, but cannot renew an expired captured
claim. Reimport preserves source dates rather than replacing them with import time.

Codex symbolic model keys and benchmark identities remain separate. Claude official
facts and derived mappings remain separate. OpenCode and agy requested subsets do
not imply exhaustive inventories. A listing does not demonstrate invocation,
reasoning support, tool use, a permanent free offer or task quality.

## Inspect, compare and refresh

```sh
build/aok models diff build/model-catalog build/model-catalog-next \
  --as-of 2026-10-23T12:00:00Z --json
build/aok models refresh build/model-catalog \
  --manifest /absolute/path/to/replacement-inputs/manifest.json \
  --policy /absolute/path/to/replacement-policy.json \
  --out build/model-catalog-next --json
```

Refresh rebuilds from explicitly supplied files; it is not a network updater. It
writes a new bundle plus a structural comparison report. Use `diff --as-of` for
an explicit freshness comparison. Diffs distinguish configuration/claim/guidance,
source-provenance and freshness changes. A source key-order change affects raw
provenance, not semantic model meaning. A model absent from a newer partial export
is reported as absent from that capture, not retired. Removal requires an explicit
dated notice from the relevant approved source.

## Check an execution profile

```sh
build/aok models check --catalog build/model-catalog \
  --policy docs/research/model-catalog-policy.json \
  --profile internal/planning/testdata/corpus/execution-profile.json \
  --as-of 2026-10-23T12:00:00Z --json
```

An explicit external policy is required for compatibility checks. A policy bundled
with captured evidence is sufficient for descriptive inspection, not independent
approval to use those facts. Missing pins/categories preserve the evidence as
untrusted and prevent it from establishing support.

Checks report `supported`, `unsupported` or `unknown` for each requested field.
They require applicable, approved, fresh evidence, exact selector/provider and
known host-version scope. Unsupported version expressions stay unknown. Listing
and editorial assertions cannot establish capability support. Conflicting applicable
claims stay unknown rather than selecting the last one. The retained research is
deliberately incomplete, so many profile checks correctly remain unknown.
The retained research assertions lack the full host/configuration scope required
by the checker. Positive findings currently require fresh, exact-profile supplied
probe observations; importing research alone does not establish support.

Numeric generation limits use exact arithmetic. Static compatibility never grants
permission, proves current account access, or demonstrates sandbox enforcement.
Reports keep `authorizationGranted: false`, `executionReady: false` and the P6
probe gate pending. Unknown is an informational result, not an invented support
score; an explicit applicable incompatibility blocks the check.

## Catalog-aware resolution

```sh
build/aok agent resolve internal/planning/testdata/corpus/assignment.json \
  --root internal/planning/testdata/corpus --out build/catalog-resolution \
  --catalog build/model-catalog \
  --catalog-policy docs/research/model-catalog-policy.json \
  --as-of 2026-10-23T12:00:00Z --json
```

The three catalog options must be supplied together. Resolution captures catalog
and policy once, binds their digests and the exact profile-byte digest into
`catalogEvidence`, and computes the final snapshot digest afterward. Without the
options, the existing resolution behavior and digest format remain unchanged.
Runtime readiness remains blocked and native exports remain incomplete drafts.

## Supplied invocation evidence

A manifest may include a `probe` source containing typed, redacted evidence with
configuration, host version, exact test-profile hash, observation time,
account-scope pseudonym, outcome, explicitly tested observations and an evidence
digest. It must not include raw private responses or credential values. Separate
probe evidence attachments are not accepted. Failure outcomes retain the observed
failure; authentication errors or timeouts do not establish model incompatibility.
The evidence digest is a declared external reference: P2 does not fetch or verify
the referenced contents. Trust is bound to the exact policy-pinned typed probe
artifact, not to an assertion of authorship or to that external digest.

A successful smoke invocation is not task evaluation. Only specifically tested,
fresh observations that match the profile and approved probe source can inform a
compatibility finding. The observation is account scoped, not proof of current
entitlement. All live transport, bounded request enforcement and actual probe
execution remain P6 acceptance gates; P2 introduces no probe-execution command.

Exit codes are 0 for a valid command/check, 1 for invalid or blocked results, and
2 for command misuse. Reported diff changes are normal successful output.
`--json` emits machine-readable reports; without it a text heading precedes the
same evidence report. The Python contributor entrypoint has no `models` group.
