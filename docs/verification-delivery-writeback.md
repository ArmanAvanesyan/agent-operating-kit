# Verification, delivery and controlled writeback

Phase 8 adds opt-in local verification and acceptance evidence, a fenced runtime
acceptance projection, repository-delivery receipt translation and a private
tracker outbox. None of these records grants agent execution, repository
publication or tracker permission merely by being valid JSON.

## Gate matrix

| Gate | Status |
| --- | --- |
| Offline candidate verification, signatures and acceptance | Implemented; fixture-qualified |
| Command verifier transport | Injected bounded interface; installed CLI has no qualified control transport |
| P7 acceptance projection | Opt-in signed authority state; fixture-qualified |
| Repository handoff evidence | Local receipt validation; publication remains independently authorized |
| GitHub, Azure DevOps, Linear, Jira writers | Separate injected transports and fixture coverage; no production network writer registered |
| Provider live reads and writes | Pending separately per operation, API, authentication mode, origin, principal and tenant |
| Codex P6 live | **Blocked**: missing trusted scoped controls and live conformance evidence |
| Claude P6 live | **Independently blocked** for the same requirements |
| P7 connected runtime / complete end-to-end workflow | Pending the selected host, capacity, quality, verifier, delivery and provider gates |

Installed host discovery does not qualify enforcement. Codex 0.162.1 and Claude
Code 2.1.303 discovery evidence remains distinct from live qualification. Fixture
keys and injected writers establish software behavior, not production issuer
identity, account permissions or provider concurrency guarantees.

## Candidate capture and pure checks

```sh
aok verify import --manifest manifest.json --policy verifier-policy.json \
  --source-root source --out new-candidate
aok verify check new-candidate --policy verifier-policy.json \
  --as-of 2026-10-24T12:00:00Z --json
```

The optional source root defaults to `source/` beside the manifest. It contains
only the explicitly inventoried files. Import creates exactly the new output
directory; existing directories are rejected. Paths, symlinks, hashes, executable
bits, duplicate JSON keys, unknown fields and versions are checked. The captured
manifest and immutable file bytes establish the candidate identity; checks reload
that bundle to detect changed or unlisted files.

Explicit criteria cover asset existence/hash, evidence resolution, repository
scope, complete inventory and role coverage. Missing command evidence stays
unknown. There is no LLM inference of criteria or verifier commands. Runtime
origins require P7 ownership validation and the separate capture command:

```sh
aok runtime capture-candidate STATE --manifest candidate-manifest.json \
  --source-root EXACT-ATTEMPT-WORKSPACE --out new-candidate \
  --verification-policy verifier-policy.json --verification-store signed-authority.json
```

Runtime capture binds the stopped, quiescent attempt, current fence/input,
checkpoint inventory and host/prepared receipts. Runtime `inputSha256` binds the
complete captured P7 submission manifest digest; the original git commit and
checkpoint HEAD remain separate base/source revisions. Capture copies the
verified immutable checkpoint, excluding Git worktree administration files and
subsequent workspace edits. A manual or fixture manifest
cannot impersonate a runtime origin.

## Verifier and acceptance trust

The closed `aok-verification/v1` policy pins criteria, verifier implementation,
rubric, argv, toolchain/environment and bounds. Trusted issuer keys are externally
supplied operator inputs, outside task-controlled content. Author bindings and reviewer principals
come from that policy; a signed self-asserted name cannot establish independence.

The injected command-control boundary requires independently signed, scoped
filesystem/network/credential/time/output control evidence. Default limits are
30 seconds, 4 MiB output and 64 MiB candidate inventory. Missing controls block
execution. `aok verify run` records this blocked gate; it does not fall back to an
uncontrolled subprocess. No installed verifier transport has been live-qualified.

Acceptance issuance recomputes pure predicates and consumes signed verifier and
independent-review receipts. Each result must match its pinned criterion and
verifier; one verifier cannot override a different verifier or a pure check.
Unknown/failing required criteria, stale/future evidence, changed snapshots and
self-review cannot pass. Copied evidence does not gain a new observation time.

Signing is an injected external-key boundary; the CLI never creates approval or
stores private keys. Signed acceptance includes criterion coverage plus exact
verifier and independent-review receipt digests. To validate and preserve a receipt from an approved issuer:

```sh
aok verify accept new-candidate --receipt signed-acceptance.json \
  --policy verifier-policy.json --out accepted-evidence
```

Acceptance proves only its exact candidate and supplied experimental scope. It
does not qualify a P4 cohort, replenish P3 usage or authorize publication.

## Runtime dependency projection

`aok runtime attach-acceptance` takes the signed receipt, candidate manifest and
paired `--verification-policy` / `--verification-store` inputs. The latter is a
fresh signed `aok-runtime-acceptance-authority/v1` state file with policy digest,
trust/revocation generations and at most five-minute validity. It is not an
arbitrary list of completed steps.

The explicit opt-in migration adds versioned projection tables to P7's SQLite
ledger. Attachment CAS binds current run/step/input/candidate/attempt/fence;
claims, launch guards and finalization check the same projection/generations
inside runtime transactions. Historical `accepted:false` candidate records remain
unchanged. The pair is also required on dependency-aware dispatch. Without it,
P7 keeps dependent work blocked. Uncovered precondition predicates stay blocked.

Revocation closes new claims. An attachment reply can be replayed idempotently;
an unattached immutable receipt grants no runtime eligibility. There is no atomic
transaction across the runtime, evidence and accounting stores.

## Existing repository delivery

`aok delivery handoff --acceptance HANDOFF-MANIFEST --policy FILE --out NEW`
validates an explicit local handoff evidence manifest. `validate-handoff BUNDLE`
adds required `--as-of` for inspection. The separately supplied policy overrides
embedded trust data. Raw existing engine context, source/destination snapshots,
gate, review and optional integration receipts retain their original bytes and
have their seals recomputed. Candidate inventory, modes, source/destination,
commands and environment are bound together. A changed destination requires its
own verification and acceptance.

The output is a local context draft with only preservation/isolated preparation
actions, `authorizationGranted:false` and `executionReady:false`. A SHA seal is
integrity evidence, not authenticated reviewer identity; a separate trusted
signed review is required. Existing repository-delivery commands still own
shipping and retain Python 3.11+ and supported platform requirements. Pure receipt
inspection needs only Go. No helper is installed automatically.

Push, open PR, submitted GitHub approval, merge, deployment, cleanup and tracker
completion remain distinct permissions. Local P7 authority must not also invoke
the pack's optional swarmd finalizer. No automatic cleanup or publication is added.

## Tracker grants and outbox

P5 commands remain read-only. `tracker authorize-plan` prepares a review preimage
from a valid fresh draft; it does not sign a grant. The issuer approves the exact
operation ID, payload digest, target, origin, principal, credential reference,
binding/capability, native revision, nonce, policy/evidence and expiry. Unknown
native field semantics and unqualified conditional updates stay blocked. Remote
completion defaults off and needs explicitly mapped native values/transitions
and independently validated milestone evidence.

```sh
aok tracker enqueue --operation operation.json --grant signed-grant.json \
  --policy write-policy.json --capability capability.json \
  --revocation signed-revocations.json --draft update-draft \
  --snapshot fresh-capture --mapping mapping.json --outbox private/outbox.sqlite
aok tracker outbox private/outbox.sqlite --json
aok tracker revoke --outbox private/outbox.sqlite --grant GRANT-ID \
  --revocation signed-revocations.json --policy write-policy.json
```

The outbox stores immutable operation bytes and grant/policy digests. Claims have
expiring ownership and fences. The durable send-intent transaction checks exact
operation/grant bindings, fresh monotonic revocation state, expiry and one-send
budget before transport. A crash before intent can recover after lease expiry;
intent or ambiguous outcomes can never become a fresh automatic send. Revoked
IDs/nonces remain revoked when later snapshots omit them.

Send intent is the authorization-consumption boundary. Revocation committed
before it blocks admission; revocation racing after it cannot guarantee that an
in-flight remote effect is suppressed. No database transaction spans network I/O.
Only separate operation-aware readback can establish success. Signed readback
reports bind exact operation/evidence; arbitrary status strings cannot finalize
`readback-verified`. Reconciliation is read-only, even after grant expiry. A new
grant alone does not make an ambiguous retry safe.

`tracker dispatch --once` and `tracker reconcile` currently report the pending
provider transport gate. The injected connector bridge and provider writers are
fixture-tested without registering a production credential/network transport.
There is no hidden fallback to P5's read transport.

Provider slices are GitHub marker comments with immutable-ID/issue-number mapping;
Azure one-field revision-tested patch; Linear UUID-scoped comment; Jira Cloud ADF
comment. Readback checks completeness, exact body and marker, numeric/native target identity
and scoped response URLs. A bounded evidence sink must preserve response bytes
and return matching hash-bound evidence before readback can establish success. Native idempotency,
fields/transitions and creation each require their own qualification. Creation
uses a separate typed intent/grant boundary; no invented pre-creation remote ID.
Default operator-facing creation remains disabled.

## Recovery, retention and offline replay

Keep immutable candidates, receipts, externally protected policies and outbox
files together with their digests. Back up quiescent SQLite databases using a
SQLite-aware procedure; do not copy just a live WAL database file. Preserve
ambiguous operation evidence until read-only reconciliation resolves it. Do not
delete rows to retry or repair; use a separately reviewed supported operation.

```sh
aok writeback replay --scenario internal/writeback/testdata/replay.json \
  --as-of 2026-10-24T12:00:00Z --out new-replay --json
```

Replay performs no provider contact, host launch or credential lookup. Reports
retain fixture scope. Exit codes follow AOK: 0 valid, 1 blocked/invalid/conflict,
2 command misuse. Inspection and replay do not renew evidence freshness.
