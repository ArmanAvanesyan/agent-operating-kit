# Protected Codex and GitHub service operation

This completes service implementation work inside P3–P10. It adds no phase and
activates no production route. Read this with
[the pilot unblocking plan](implementation/codex-github-live-pilot-unblocking-plan.md).

## Executable and trust

`cmd/aok-enrolled` is a separate operator-built Go executable. Ordinary `aok`
remains unchanged and installs no live services. The enrolled executable also
ships with an empty compiled root pin: all side-effecting commands fail before
reading configuration or resolving credentials.

An operator builds it with a SHA-256 pin of their externally provisioned **raw
32-byte Ed25519 public key**:

```sh
go build -trimpath -ldflags '-X main.compiledRootSHA256=<approved-root-sha256>' -o aok-enrolled ./cmd/aok-enrolled
```

The placeholder is not an approved root. This workflow generates no keys, creates
no accounts, logs in nowhere and discovers no existing credentials. Operator
signing and independent measurement services remain outside task/model control.

Offline `prepare` and `preflight` commands are available even with the empty
root pin. They create drafts or inspect local preparation files only; active
services still require the compiled root. See [enrollment preparation](enrollment-preparation.md).

Commands:

```text
aok-enrolled prepare --out NEW_DIRECTORY
aok-enrolled preflight --bundle DIRECTORY --as-of RFC3339 --json
aok-enrolled inspect --config SIGNED --root PUBLIC_KEY --as-of RFC3339
aok-enrolled anchor serve --config SIGNED --root PUBLIC_KEY
aok-enrolled authority serve --config SIGNED --root PUBLIC_KEY
aok-enrolled verifier serve --config SIGNED --root PUBLIC_KEY
aok-enrolled worker --config SIGNED --root PUBLIC_KEY
aok-enrolled qualify --config SIGNED --root PUBLIC_KEY
```

The protected bootstrap also has a one-operation `qualification` service for
synthetic conformance actions and fixed post-run worker modes for
`verify-candidate`, `accept-candidate`, and `handoff-candidate`. These are
operator-signed, bounded inputs, not extensions of production authority. The
GitHub synthetic action consumes its own one-use external anchor before any
provider call; it does not obtain the 13-gate pilot authority or turn its
observation into readiness. Post-run verification likewise needs a fresh
candidate-bound proof and its own one-use execution grant. Acceptance and
handoff consume separately issued current acceptance evidence and project it
into the protected runtime ledger.

The `qualification` record uses `aok-qualification-bootstrap/v1` with purpose
`synthetic-conformance-only`, stable parent/operation IDs and at most five minutes.
Every action consumes its pre-provisioned `qualification-<structural digest>`
anchor stream before IO. A lost append response consumes the operation; renewal
cannot adopt the consumed tip. The actions are `github-read`, `codex-quota`,
`host-conformance`, `verifier-conformance` and `github-comment`. Each accepts only
its typed payload; none creates authority proofs or signs acceptance.

`codex-quota` uses a unique unapproved qualification source namespace, never a
production capture ordinal. `host-conformance` uses the existing P6 fixed matrix,
with at most ten starts, thirty seconds per turn and the shorter five-minute
bootstrap deadline. It requires existing native controls and a positive,
operator-approved upper bound for the entire matrix in P3's actual units. It
rechecks capacity before starts, refuses crossing a known reset, and retains the
whole hold as uncertain after execution. Tokens cannot supply percentage demand
or settlement. `verifier-conformance` uploads only an explicitly synthetic fixture
candidate using an externally signed candidate-bound control proof. It records
hashed invocation metadata, never acceptance or raw output.

Post-run verification requires the actual runtime candidate and `runtimeRoot`
lineage, an exact policy file, a fresh candidate/snapshot-bound control proof and
a root-signed `aok-postrun-execution/v1` grant with purpose
`post-run-verification-only`. Its separate `postrun-<structural digest>` anchor is
consumed before upload. The verifier result is invocation evidence. An independent
issuer must sign the acceptance receipt; accept/handoff stages check current
acceptance authority and runtime lineage separately. Verification installs the
current signed acceptance-authority projection before checking lineage; advancing
its trust/revocation generations intentionally fences older active projections.
It is therefore not a read-only runtime-ledger inspection. Configuration field names
and hash construction are defined in `internal/enrolled/bootstrap`, rather than
an automatically generated or self-approved enrollment file.

Inspection verifies exact signed payload bytes without resolving secrets or
opening state. With an empty compiled pin it reports `rootPinned:false`; that is
structural inspection, not authorization. Active commands use current time,
require the compiled pin, and accept no task-selected endpoints, implementations,
keys, command arguments or billing fallbacks. Exit codes are 0 success, 1 blocked
or invalid, and 2 command misuse. Inspection requires an explicit `--as-of`.

The closed `aok-protected-bootstrap/v1` envelope carries issue/expiry times,
exact JSON `payload`, and base64 Ed25519 signature. Signing bytes are exposed by
`bootstrap.SigningBytes`; payload version is `aok-enrolled-service-config/v1`.
Unknown fields, duplicate keys, invalid signatures, stale envelopes, symlinks and
unbounded files fail closed. Refer to the typed contracts in
`internal/enrolled/bootstrap` rather than editing signed bytes after approval.
`secretEnv` maps opaque service references to explicitly approved environment
variable names. Only active, verified service startup resolves them; inspection
outputs their count, never names or values. Run each service with a minimal
externally managed environment. Environment references are not a credential
store or an isolation mechanism.

## Deployment domains and inputs

| Service | Protected inputs | Deployment prerequisite |
| --- | --- | --- |
| Anchor | Existing private stream directory, canonical genesis, initial tip, signing key reference, bearer reference, TLS | Independently retained append history, separate custody and demonstrated retention/anti-rollback storage |
| Authority | Signed enrollments, independent live-proof issuer keys, response key/token references, private SQLite location, pinned HTTPS anchor | No worker write/backup access; exclusive active service identity; qualified external anchor and recovery process |
| Verifier | Policy/spec/rubric, pinned Docker binary/image/socket/config/resource limits, protected artifact store, TLS and bearer reference | Separate UID, worker-denied store/socket access, actual engine/kernel/network/mount/cancellation conformance |
| Worker | Exact signed purpose, route/profile/configuration/account/repository/operation, preexisting runtime/outbox where applicable, public trust and token references | Independently enforced native controls, actual native-account mapping and current scoped service qualification |

TLS listeners require a configured certificate/private-key file and use TLS 1.2
or later. TLS secrets must be private files; all referenced paths reject symlink
ancestors. HTTPS clients pin origin/path, reject redirects, authenticate through
opaque references, and verify response signatures and freshness where applicable.
No executable plugins load from records.

The anchor is a reference implementation of `aok-authority-anchor/v1`:
`POST /v1/anchor` exposes only `current` and compare-and-append `advance`.
Provision the canonical genesis outside AOK, before startup:

```json
{"version":"aok-authority-anchor/v1","streamId":"APPROVED_STREAM","sequence":0,"tip":"APPROVED_64_HEX_INITIAL_TIP"}
```

The file name is `STREAM.00000000000000000000.json`. The reference server accepts
no create/reset/truncate operation. Appends use create-only numbered files,
signed links, file fsync and directory fsync; corrupt or partial chains block.
History reads are bounded. Record-count exhaustion requires an explicitly
reviewed continuation/recovery plan; automatic truncation is absent. Application
append-only behavior does not prevent an administrator rolling back its entire
volume. Independently qualified retention/WORM storage and custody remain a
live gate, or use a separately qualified compatible append service.

## Authority and recovery

Enrollment is `aok-enrolled-authority/v1`, signed by an externally configured
operator key. Required independent live gate proofs include identity, controls,
capability, capacity, verification, runtime, authorization, platform, provider
read/operation, witness, budget and stop-monitor. Ordinary evidence experiments
also require quality. Proofs bind the exact trial scope, measured evidence,
issuer role and original observation time; maximum validity is five minutes.
Fixture-origin proofs never pass production enrollment.

Every check sends the complete enrolled binding map. Runtime adapters separately
match actual prepared/policy/launch hashes, route/readiness/proof hashes and exact
grant/trial identity. Listing, registration, credentials or a successful smoke
probe cannot supply task quality or enforcement.

Budgets and one-use boundaries retain stable parent/trial/operation IDs across
enrollment renewal and epoch replacement. They are cumulative, not refunds.
Capture leases serialize account/source observations and allocate globally
ordered ordinals; completed capture-result hashes are retained. A permanent
parent stop blocks new authority actions across renewal/reopen. Existing P7
heartbeat revalidation observes denial and requests cancellation; actual process
stop is a separate observed runtime fact. Worker signal cancellation propagates
into execution and tracker dispatch/reconciliation contexts.

SQLite state, including schema, budget, stops, consumed IDs, epochs, captures,
results and request nonces, is committed to the external anchor. Every operation
checks that commitment. A restored or edited database, repeated nonce, lost
append response or uncertain database commit fails closed. There is no automatic
resend, tip reset, refund, truncate or repair. Preserve both domains and reconcile
through an independently approved recovery procedure. Copying the anchor into an
authority/worker backup defeats its purpose and is not a qualified deployment.

## Fixed worker modes

The signed configuration chooses one operation. The command line cannot replace
it, and no production `PilotExecution` service is installed.

| Mode | Actual wiring | Boundaries |
| --- | --- | --- |
| `codex-quota` | Pinned App Server read-only factory, authority capture lease, P3 normalizer and hashed metadata | No raw frames exported; no model invocation; aggregate percentages do not establish attempt attribution |
| `github-capture` | Pinned `gh` runner, broker token, explicit read policy, existing tracker capture/writer | Scoped issue/repository/comments only; no Projects discovery or mutation |
| `calibration` | Existing P7 calibration coordinator and external trial services | Non-quality gates stay mandatory; one-shot experimental rehearsal, not ordinary quality qualification |
| `experiment` | Existing P7 experiment coordinator, P4 selection and readiness v2 | Ordinary selection/quality/capacity constraints remain; evidence collection is not production promotion |
| `github-dispatch` | Existing activated P8 outbox dispatch plus pinned GitHub writer and exact evidence attestation | Separate signed one-comment grant; no issue closure, merge, publish or deploy |
| `github-reconcile` | Existing P8 signed-readback reconciliation | Requires independent readback evidence; never automatically resends |
| `qualification` / `github-comment` | Root-pinned, operator-signed, fixed synthetic qualification action; exact immutable issue ID to REST issue-number map; bounded hash-verified comment asset; enrolled broker principal and token-reference resolution; one-use qualification anchor; response assets durably retained; one complete readback attempt | Exact fixed body carries a synthetic-qualification label and explicitly disclaims task acceptance/completion/production; observation only, no 13-gate enrollment, pilot grant, readiness or production promotion |
| `verify-candidate` | Root-signed post-run records bind the actual stopped-run candidate, input and policy; fresh candidate/snapshot proof; one bounded command; single-use post-run anchor grant before upload/execute; runtime lineage rechecked immediately before consumption | Does not accept the task; uncertain anchor consumption or remote result is not retried |
| `accept-candidate` | Independently signed acceptance receipt and current acceptance/revocation projection attach to the exact candidate lineage in the runtime ledger | Acceptance is evidence supplied by the separate authority; worker does not mint it |
| `handoff-candidate` | Requires accepted receipt, current acceptance projection and destination snapshot; creates an immutable handoff record tied to candidate and receipt in protected storage | No handoff without acceptance; no new execution or provider authority |

Codex currently pins the bridge's exact supported version and native-login route.
Account/HOME/environment measurement references must come from an independent
native observer and match enrollment. The collector checks those bindings; it
does not itself measure or authenticate a native account from a label. Actual
identity/configuration stability, credential isolation and executable filesystem
immutability are deployment qualification gates. Process groups provide Unix
cleanup supervision; Windows collector constructors explicitly refuse execution.
Cross-compilation is not platform certification.

GitHub read and write use explicitly enrolled broker principal metadata and
token references. Each exact HTTP/CLI authorization is separately hash-bound.
For issue comments, the root-signed records bind an immutable provider issue ID
to the positive REST issue number. The immutable ID identifies the enrolled
operation; the scoped source is `issue:<REST number>`, and both authorization
and HTTP dispatch/readback must resolve to that same numeric path. The comment
asset is loaded by safe relative path from an enrolled root, bounded, and
verified against its signed digest. Response assets are retained through a
create-only durable evidence sink. These fixtures exercise the configured
writer against injected HTTP transport; they do not call GitHub or resolve live
operator credentials.
App installation-token minting/rotation is supplied by the external broker; a
separately enrolled fine-grained PAT is an alternative. Ambient personal `gh`
authentication, automatic principal changes and broader provider operations are
excluded. Provider API receipts remain distinct from AOK-signed readback receipts.
The current first-route readback limit remains one complete comments page;
continuation means incomplete evidence, never permission to resend.

Verifier uploads use authenticated, bounded immutable candidate archives and an
independent candidate/snapshot/policy/spec-bound control proof. The store captures
its trust policy; an upload cannot choose another issuer registry. Production
handlers require the measured executor identity. Docker lifecycle is explicit:
create, start/attach, wait, stop/wait/remove and daemon-checked absence. Unknown
cleanup rejects the invocation result. Separate acceptance receipts and current
projection remain necessary. Same-UID store mutation, mutable binary paths and
actual daemon isolation are not solved by hashes or a read-only container mount.

## Live and production gates

No credential lookup, Codex invocation, Docker run, GitHub request/mutation,
live qualification, pilot or deployment is performed by these fixture checks.
Operators still supply the native session, actual identity/control observations,
scoped GitHub broker credentials, TLS/service identities, distinct trusted issuer
roles and independently retained anchor storage. Missing any required value or
measurement blocks the relevant operation.

P3 causal attribution and cross-window settlement remain fail-closed. P4 quality
cohorts and demand bounds must qualify independently. P6 host controls, P5/P8
provider conformance, P8 acceptance/handoff, P7 recovery, and the first P9 trial
remain live gates. The synthetic qualification route is implemented but does
not satisfy or sign those independent gates. At least 100 eligible shadow
decisions plus assessment and an external promotion grant are still required
for production activation. Other hosts, providers and platforms retain their
own gates. See [enrollment integration delivery evidence](implementation/enrollment-integration-delivery-evidence.md)
for the implemented wiring and fixture boundary.
