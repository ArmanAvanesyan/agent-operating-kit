# WorkKit P7: selective bidirectional sync and review-first worker

P7 extends the P6 merge `28cfa65` (PR #92). The opt-in implementation uses
`workkit-sync/v2`, `workkit-integration/v1` and a genuinely distinct
`workkit-journal/v2`. Historical v1 parsing, grants and command behavior remain
separate. Implementation and fixtures do not qualify a connected route.

## Review-first workflow

1. Capture both explicitly scoped endpoints through protected operational reads.
   P2 read enrollment remains qualification-only. `CaptureOperational` requires
   its own signed purpose, connection/principal/authentication/origin/query,
   permitted typed endpoints, route revision, expiry and current external checks.
   Its `capture/` parser bundle remains unqualified; `operational-read.json` is a
   separate receipt. A protected poller validates this evidence and current
   bindings before returning a prepared proposal.
2. Supply independent forward and reverse mappings, both bindings, selectors,
   native-ID correlations, current observations and a paired baseline. Preview
   never infers the inverse mapping or creates correlations by names.
3. Review the immutable result. Neither-side changes are no-ops; unilateral
   changes yield only allowed ordinary-field proposals; equivalent concurrent
   changes yield a baseline-advancement proposal; divergent changes conflict.
   Missing, partial, stale or lossy inputs remain blocked. Exact JSON decimal
   comparisons preserve numeric meaning; missing and null remain distinct.
4. Resolve conflicts explicitly against their exact original plan. Choosing a
   side generates a new proposal and rechecks current inputs. It does not grant
   authority or silently adopt a baseline. Unresolved conflicts block the route.
5. Submit an exact approved batch to its current worker. Every operation still
   needs a separate P5-compatible prepared operation and provider-send grant.
   The shared P5 coordinator consumes grants and send intent durably before I/O.
   Native completion, webhook payloads and source status provide no acceptance.
6. Confirm readback before advancing field baselines. Interrupted or ambiguous
   sends retain pending obligations; reconciliation reads never authorize resend.

Person, relationship and completion/lifecycle mutations remain blocked/manual in
v2 rather than bypassing immutable identity, ownership, CAS or acceptance checks.
No automatic create, delete, archive, comment mirroring or conflict resolution is
installed. Provider automation producing different readback creates divergence.

## Commands and SDK

Standalone `workkit`, `aok work` and `aok workkit` share the same services. Use
`--v2` to select new sync behavior; existing v1 commands are unchanged.

```text
sync validate|preview --v2 --configuration FILE --state FILE \
  --source BUNDLE --destination BUNDLE \
  --source-observation BUNDLE --observation BUNDLE \
  --forward-mapping FILE --reverse-mapping FILE \
  --forward-context FILE --reverse-context FILE \
  --source-binding FILE --binding FILE \
  --source-query FILE --destination-query FILE --as-of RFC3339 [--out NEW]
sync resolve --plan BUNDLE --resolution FILE --as-of RFC3339 --out NEW
sync ingest --event-envelope FILE --policy FILE --enrollment FILE --out NEW
sync run|pause|resume|reconcile --v2 --plan BUNDLE --policy FILE \
  --enrollment FILE --out NEW [--invocation FILE] [--once] [--initialize]
sync status|run|pause|resume|reconcile --v2 --command FILE --listen SOCKET
serve --workspace FILE --configuration FILE --listen SOCKET \
  --policy FILE --enrollment FILE
```

Offline evaluation requires explicit `--as-of`. Effectful controls use current
time. Outputs are create-only private bundles. Exit codes remain 0 success,
1 operational failure, 2 misuse/schema, 3 blocked/unsupported/unqualified,
4 conflict/staleness, 5 uncertain effects, 6 partial capture. Domain results,
operation IDs, diagnostics and detailed effects are preserved by suite routing.
`sync status` cannot forward submit, acknowledgement or ingestion commands.
Notification reads and acknowledgements use typed SDK/private-IPC methods
`IntegrationCommand`/`ForwardIntegration` (`events` and `ack`), not provider sends.

Protected embeddings install `IntegrationBackend`, `OperationalReadBackend` and
the explicit `IntegrationOperationBackend` adapter. Standard binaries install no
qualified operational bootstrap. There is no CLI flag that grants trust. P5
`OperationSession`, `DispatchOnceActivated`, native grant verification, CAS,
outbox, current-writer checks and readback remain the single send boundary.

## Journal ownership and recovery

Create v2 stores only for enrolled new disjoint namespaces. The actual SQLite
application identity and exact schema distinguish v1, v2 and legacy stores;
unknown, copied, damaged or nonempty unversioned stores fail closed. No upgrade,
import, namespace transfer or migration activation is performed.

External protected ownership/anti-rollback callbacks must cover relevant v1/v2
stores and both endpoint cells. SQLite supplies local exclusion only. The driver
also rechecks exact store path/ID, current worker incarnation/fence, policy route,
prepared plan, source/target evidence and per-operation authority at admission.

Only one reconciliation cycle runs at once. Defaults: polling 60 s plus up to 10 s
stable jitter, lifetime 15 min, cycle 5 min, request 30 s, 100 requests, 10,000 items,
4 MiB responses, 64MiB capture, 100 batch operations. Signed policy and worker
configuration may shorten limits. Idle heartbeat renews the 30 s local lease;
expiry/revocation stops admission, never proves an escaped request stopped.

Recovery runbook:

1. Pause admission and preserve the database, referenced assets, grants,
   notification cursors, pending intents and external witness obligations.
2. Inspect route state and provider readback without sends. Retain uncertainty
   after loss of response, cancellation without stop or readback disagreement.
3. Obtain independent quiescence/actual-stop evidence and current global ownership.
4. Use signed `WorkerRecovery` with exact store, prior fence, route revision,
   replacement identity and quiescence digest via `RecoverIntegration`.
5. Issue a new worker enrollment with the returned `attachedFences`; an expired
   lease or reused worker ID alone cannot attach or take over.
6. Restart review-first. No sends occur until a new exact batch and all operation
   grants are submitted. Reconciliation must resolve prior obligations first.

Notifications are committed with state changes, delivered at least once and
acknowledged per consumer/route. Acknowledgement never proves a provider effect.
Queue limits apply backpressure without discarding obligations. Read coverage
can drain only covered connection hints; checkpoint configuration/account/query/
API drift fails. Completed reconciliation watermarks advance separately.

## Private service and GitHub ingress

Initial IPC is macOS/Linux private Unix sockets: 0700 parent, 0600 socket, owner and
peer credentials, pinned worker PID/start plus separately signed caller PID/start,
4 MiB messages and action-scoped signed commands. Status cannot bypass ownership.
An unreachable worker does not authorize takeover. Submission response loss or
invalid response after transmission returns uncertain/5, without automatic retry.
Windows `serve` remains unsupported; offline Windows binaries are cross-built.

An operator-managed HTTPS ingress forwards original raw body bytes (base64 in
`EventEnvelope.body`) and selected headers to private IPC. P7 creates neither an
ingress nor webhook registration. Verify
[GitHub HMAC signatures](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries)
over exact bytes using constant-time comparison, plus an independently signed
receipt binding headers, route, repository/connection, nonce and receive time.
The receipt's freshness establishes ingress freshness only. GitHub body signatures
do not authenticate forwarded header values or delivery age.

Only issue/issue-comment hints are supported; pull requests/Projects remain
separate. Delivery-ID/body collisions quarantine the route's sends. Duplicate
pending bodies coalesce; tombstones retain collision identity after read coverage.
Hints never update baselines. Periodic bounded polling repairs gaps. GitHub uses
[the same delivery ID for redelivery](https://docs.github.com/en/webhooks/using-webhooks/best-practices-for-using-webhooks)
and [does not automatically redeliver failed deliveries](https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries).
Other provider webhook adapters remain unsupported.

## Provider/direction/operation matrix

| Provider | Read/poll boundary | P7 field dispatch | Existing P5 dispatch | Qualification |
|---|---|---|---|---|
| Azure DevOps Services | Existing scoped injected reads | Explicit ordinary native-revision/CAS patches in either approved direction | Revision-tested patches | Separate route/direction/auth/platform pending |
| GitHub public cloud | Existing scoped reads; issue/comment webhook hints | Unsupported/manual | Explicit issue comments | P5 live comment and ingress pending separately |
| Linear | Existing scoped injected reads | Unsupported/manual | Explicit comments | Pending |
| Jira Cloud | Existing scoped injected reads | Unsupported/manual | Explicit comments | Pending |
| Jira Data Center | Distinct scoped injected reads | Unsupported/manual | Unsupported | Pending |

Two distinct Azure scope fixtures exercise directional planning. They establish
neither live route conformance nor cross-provider field support. Models, agents,
native mutations, session creation/resume and business acceptance are external.

## Delivery gates

See [P7 delivery evidence](implementation/workkit-p7-delivery-evidence.md).
P7 implementation/fixture acceptance is separate from every live gate. Native
mutations remain blocked; P5 qualification pending per exact operation; migration
activation blocked; production activation unauthorized. Legacy `aok work-system
setup` remains read-only. No prior evidence dates are renewed by regeneration.
