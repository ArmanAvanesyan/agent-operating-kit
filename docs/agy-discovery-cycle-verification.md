# agy project discovery gate review

This investigation uses `agy-project-discovery/v2`, a separately versioned
observable-behavior gate for pinned Antigravity CLI 1.4.2 on Darwin x64,
project scope. It does not alter the historical comparator or its evidence.
The descriptor now marks this limited project discovery scope verified from
the nine observations below. Explicit native parse-error reporting is unverified.

The foundation acceptance contract requires native loading evidence, semantic
round trips, loss/drift reporting, and explicit scope/version limits. Its
native-loading requirement does not require the client to display a parser
error. The earlier agy probe imposed that extra requirement. Prior real-account
observations showed valid files listed and malformed files omitted, but did not
show a parser error. Those results alone did not satisfy that earlier gate.

The new gate asks a narrower, directly observable question: does the manager
consistently discover this valid project definition, omit the same definition
when its frontmatter is corrupted, and rediscover its exact original bytes after
repair? Silent omission qualifies only for this discovery contract. Explicit
native YAML-error reporting remains unverified.

## Acceptance controls

- Pin the native binary hash, host version, platform, project scope, and 160×80
  terminal geometry. Use only an approved fresh temporary OAuth seed.
- Keep the same Git root, native path, and private HOME throughout each cycle.
  Start a fresh native process per phase; this tests startup discovery, not hot
  reload. Preserve native HOME state between phases.
- Run valid → malformed → repaired → absent. Replicate from a fresh private
  copy of the original seed, starting malformed before repeating that sequence.
  This cold malformed control avoids relying on a previously warmed target.
- Insert exactly one `[` byte inside the original description frontmatter.
  Bind original/malformed bytes and hashes. AOK's strict inspector independently
  reports `native.decode`; this is AOK validation, not a native diagnostic.
  Repair must restore the exact original hash.
- A second valid guard definition remains unchanged and visible in every phase.
  Accept only a complete, framed manager list containing the exact controlled
  rows. Unknown, duplicate, wrapped or clipped rows block qualification.
- Require at least five seconds of stable manager display after `/agents`.
  Auth barriers, error screens, ambiguity, missing guard, identity drift, or
  process/profile cleanup failure block qualification.
- Never select an agent or send model input. Retain booleans and fixture/source
  hashes; never retain account rows, credentials, or native log content.
- Native processes cannot write fixtures, Git metadata, the session marker,
  original HOME, or source seed. Owned process groups and all credential-bearing
  copies and the consumed seed must be removed.

A passing report is only a review candidate. Descriptor promotion requires
independent review and green CI. It proves only the documented discovery
boundary. It does not prove execution, backend authentication, policy
enforcement, all YAML schema validation, or a user-visible parse error.

## Sources

- [Foundation native acceptance contract](implementation/agent-foundations/README.md)
- [Historical malformed-file investigation](agy-malformed-verification.md)
- [agy CLI agents documentation](https://www.antigravity.google/docs/cli/commands/agents)
- [Native subagent definitions](https://antigravity.google/docs/subagents?tab=cli)

The official documentation describes discovery paths and frontmatter; it does
not promise a visible malformed-file error. Native syntax alone is insufficient
for this gate; the actual controlled manager observations are required.

## Observed result and closure rationale

The [saved native report](evidence/native-adapters/agy-discovery-cycle-2026-10-18/cycles.json)
passes the v2 classifier with no diagnostic codes. Each settled display lasted
5.05–5.07 seconds. The guard appeared in all nine complete framed lists.

| Cycle | Phase | Target | AOK fixture check |
|---|---|---|---|
| Same workspace/HOME | valid | listed | valid |
| Same workspace/HOME | malformed | omitted | `native.decode` |
| Same workspace/HOME | repaired | listed | valid; exact original bytes |
| Same workspace/HOME | absent | omitted | absent |
| Fresh seed copy | cold-malformed | omitted | `native.decode` |
| Fresh seed copy | valid | listed | valid |
| Fresh seed copy | malformed | omitted | `native.decode` |
| Fresh seed copy | repaired | listed | valid; exact original bytes |
| Fresh seed copy | absent | omitted | absent |

Every phase records fresh-runner/native-exec attestations, successful owned
process-group cleanup, unchanged fixture/guard hashes and path-free workspace
and HOME identities. These identities remain stable within a cycle and differ
between cycles. Both copies and the consumed seed were removed. No agent was
selected and no model prompt was sent. The original installed agy executable
was not invoked or changed.

This supports closing the **project startup-discovery gate** for the pinned
Darwin x64 1.4.2 client. It supports silent omission and exact repair recovery,
not a claim that agy emits malformed-YAML errors. All native `parseErrorObserved`
values remain false. The old explicit-error comparator remains unchanged and
unqualified; its historical results are not reclassified as passes.

The [evidence index](evidence/native-adapters/agy-discovery-cycle-2026-10-18/index.json)
binds report, sanitized audit and exact helper-source hashes, including the
failed preflight attempt. That attempt passed `/dev/null` to a helper requiring
`/dev/tty`, failed before native launch, and removed the temporary credentials.
After correction, a credential-free sandbox preflight passed before a fresh
sign-in and the successful run. It contributes no positive loading evidence.
The shared capture source has a supplemental frozen-source attestation in the
index because the legacy dependency archiver does not include that module.
Account rows, OAuth codes, native log content and credentials are not retained.

Remaining limits: no in-process reload test, agent invocation, backend-auth
proof, general YAML-schema rejection guarantee, global/plugin scope proof, or
runtime permission enforcement. Required unmapped or unenforceable controls
still block; native imports/exports remain incomplete drafts. The eight targets
now have at least one bounded versioned native-loading scope; this does not
establish runtime release completion or authorize execution.

## Validation

Local checks passed: kit validation, Python compilation, the full Python suite
(259 tests, two platform/environment skips before the final evidence replay test
was added), all eight final focused gate tests, `go test ./...`, a fresh Go CLI
build, both proposal checkers, and Codex/Claude pack render smoke checks. The
built binary also validated agent and planning fixtures with no Python on PATH.
The initial restricted-shell Python run could not access the controlling terminal
or nested macOS sandbox; the full suite passed when run with those host facilities.
The conformance fixture was updated to expect the evidence-backed descriptor
status; required unsupported controls still block and runtime stays unverified.
Go 1.22 and cross-platform build results are checked on the reviewed PR head.
