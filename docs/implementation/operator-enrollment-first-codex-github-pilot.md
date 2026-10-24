# Operator enrollment and live qualification: first Codex + GitHub P9 pilot

Date: 2026-10-24. Completion milestones within P3–P10, not another phase.
Status: integration code and fixture wiring implemented; live qualification remains pending. No credential access, deployment, invocation or remote write was performed.

## Outcome and route

First qualify one private, operator-controlled local Codex route and one GitHub
issue-comment route. Use one exact model/configuration/profile, account/pool,
Unix platform, disposable repository, issue and deterministic standard-risk task.
Freeze their identities before enrollment. Retain one step, candidate, attempt
and concurrent run; no fallback, delegation, resume or billing change. Host
execution remains at most 60 seconds and verifier command at most 30 seconds.

Two Luna-medium agents audited host and provider inputs; a separate Luna-high
agent reviewed enrollment dependencies. The audit distinguishes component
implementation from an actually operable end-to-end enrollment. Previous fixture
acceptance does not remove the concrete integration blockers below.

The first milestone is a **separately signed calibration rehearsal** if quality
cohorts do not yet qualify. Its only quality exception is the existing calibration
path; all non-quality gates and independent acceptance remain required. The first
ordinary P4-selected P9 experiment follows sufficient quality/demand evidence.
Neither milestone activates production.

## Pre-enrollment repairs and dependencies

These are AOK/operator-integration work, not credentials you must obtain.

| Required work | Current evidence | Acceptance before live use |
| --- | --- | --- |
| Complete GitHub writer dependencies, P5/P8 | Implemented in `bootstrap.buildGitHubWriteWorker`: typed writer options include a root-signed immutable issue ID → REST issue number mapping, bounded hash-verified comment loader, and durable response evidence sink. Scope is `issue:<REST number>`; immutable ID remains the operation identity. | Positive configured-writer fixture exercises final exact POST and readback through injected HTTP transport. Wrong mapping/asset and unsafe or conflicting paths fail closed. Live broker identity, token permissions, provider conformance and independent signed readback remain pending; response loss never triggers resend. |
| Establish independent qualification entrypoint, P6/P7/P9 | Implemented as a separately root-pinned and operator-signed, purpose-bounded `qualification` action. `github-comment` fixes the body to `AOK synthetic qualification <ParentID>/<OperationID>\nNo task acceptance, completion, or production claim.`, binds the exact asset and operation, and consumes a separate one-use external anchor before provider IO. | Synthetic conformance only. It is not `authority.Enrollment`, does not satisfy the 13 live pilot gates, create proofs, or promote readiness. External proof review and the independently qualified issuer/measurement path remain required before live enrollment. |
| Connect post-output verification/acceptance/handoff, P8 | Implemented protected worker stages: `verify-candidate` checks the actual candidate and runtime lineage, fresh candidate/snapshot-bound proof, bounded verifier policy, and consumes a separate one-use post-run grant before upload/execute. `accept-candidate` attaches an independent signed receipt and current acceptance/revocation projection to the runtime ledger. `handoff-candidate` requires accepted lineage and writes the handoff record. | A proof for a future/placeholder candidate cannot authorize the actual output. Independent verifier/acceptor, fresh external proof review, protected runtime state and live service qualification remain pending. These stages do not mint acceptance or general execution authority. |
| Provide readback and evidence issuance, P8/P9 | Writer fixture exercises the configured final request/readback path and preserves returned evidence assets. Existing reconcile still consumes an independently signed report; raw provider data is not self-signing evidence. | A qualified observer must collect complete correlated provider assets and sign the exact report under an approved role. Prove ambiguous-response reconciliation without resend; keep the one-page readback bound and incomplete evidence blocked. |

The protected runner, collectors, authority, anchor, verifier, GitHub writer,
synthetic qualification action and post-run worker stages are implemented. Their
fixture tests establish wiring and fail-closed behavior, not live operability.
Handing over tokens alone does not make the pilot path ready: all independent
measurements, external proof review, current trust/revocation, provider
conformance, acceptance and recovery gates remain pending. See [enrollment
integration delivery evidence](enrollment-integration-delivery-evidence.md) for
the implementation and test boundary. Keep remaining work in the existing
phases and rerun focused/full Go 1.22, races/recovery, Python, kit, packaging,
offline and six-target checks after code changes.

The offline [enrollment preparation commands](../enrollment-preparation.md) now
produce typed, non-executable drafts and an editable inventory, with stage-labelled
preflight blockers. They do not create signatures, credentials, services or live
qualification. The post-run files remain absent until their actual outputs exist.

## What the operator provides

Provide decisions, access, custodians, public roots and secret references.
Keep actual tokens/private keys in the named secret store or service environment;
never paste them into chat, commit them, or place them in task/model context.

| Input | Exact material and custody | Non-secret enrollment/evidence supplied |
| --- | --- | --- |
| Operator identity and approval | Named enrollment owner and permitted signatory; separate authority for synthetic writes, calibration, ordinary experiment and later promotion | Approved purposes, owners, expiry, finite budgets, repository/account allowlists, stop/recovery contacts |
| Codex native session | Operator performs ChatGPT login in isolated approved user/HOME/config context; actual subscription/workspace/model access. No API key requested for this native-login route | Account/workspace pseudonyms, pool binding, supported auth type, independent native-account proof reference/hash, session/config measurement and access observation |
| GitHub read token | Broker-issued installation token restricted to the disposable repository, with Issues read and Metadata read; separately enrolled fine-grained PAT is an alternative | Opaque auth/env references, actual principal/installation identity, granted repositories/permissions, expiry, broker grant reference |
| GitHub write token | Same enrolled installation/principal, separately scoped Issues write and Metadata read, or an explicitly approved identity mapping | Same metadata plus exact single-issue/one-comment grant and root-signed mapping from immutable issue ID to positive REST issue number. AOK source scope is `issue:<REST number>`; immutable ID identifies the operation and is not substituted into the HTTP path. AOK scope strings are `issues:write` and `metadata:read`; these are not GitHub OAuth scope names |
| GitHub App minting credential, when chosen | App private key held only by external broker, app/installation identifiers, permission to mint restricted installation tokens | Public app/installation IDs, issuance/renewal/revocation audit and actual permission/repository response. No minting/login service is installed by AOK |
| Bootstrap signing key | Externally held Ed25519 private key; approved raw 32-byte public key file and SHA-256 compile pin | Signed exact bootstrap payload, public root digest and service binary digest. Empty-pin build cannot activate services |
| Authority enrollment signing key | Externally held approved operator key | Public key, independently signed enrollment/issuer registry, purpose/trial/route/input bindings and original dates |
| Authority and anchor response keys | Separate externally supplied Ed25519 response signing keys, private values only in protected service references | Pinned response public keys, server/stream IDs and authenticated endpoint bindings |
| Live proof issuer keys | Approved roles: `controls-issuer`, `identity-issuer`, `capability-issuer`, `capacity-issuer`, `verification-issuer`, `runtime-issuer`, `authorization-issuer`, `platform-issuer`, `provider-read-issuer`, `provider-operation-issuer`, `witness-issuer`, `budget-issuer`, `stop-monitor-issuer`; also `quality-issuer` for ordinary controlled-evidence collection | Public issuer IDs, role bindings, scope, measured evidence digest, signed original observation and expiry; author/model holds no issuer key |
| Verifier and acceptance keys | Protected controls/verifier/acceptance signing roles, independent of the candidate author | Policy/spec/rubric/executor/image identity, command receipts, candidate/snapshot-bound acceptance and current trust/revocation projection |
| Readback/outcome/custody keys | Protected readback signer; approved outcome-shard/custody issuers for P4 evidence | Signed reconciliation and retained assets; signed outcomes, partition freeze/audit-open chronology and verifier/rubric identities |
| Service bearer credentials | Separate worker→authority, authority→anchor and worker→verifier opaque references | Allowed principal/destination, rotation/expiry/revocation evidence; no shared general administrator credential |
| TLS identities | HTTPS server certificate chains and private-key files for authority, anchor and verifier; OS-trusted roots on clients | DNS/origin/path, certificate/CA provenance, trust installation and destination/redirect-denial tests |

Read capture accepts exactly the AOK permission labels `issues:read` plus
`metadata:read`; dispatch metadata uses `issues:write` plus `metadata:read`.
The current reader source uses `auth.mode=gh-session` with an injected broker
`GH_TOKEN`; this is not permission to use an ambient personal gh session. App
versus PAT provenance is not a dedicated bootstrap auth-kind field, so it needs
an independently attested broker/authentication record. Prefer separate restricted
read/write tokens from the same installation. Contents, PR, Projects, Actions and
administration write permissions are outside the first route.

Current clients use Go system TLS trust, not a configurable custom CA/mTLS
transport. Supply certificate chains trusted by the enrolled clients or complete
a reviewed trust-transport extension first; never disable certificate validation.

Key encodings differ by contract. Bootstrap root file is raw 32-byte Ed25519;
bootstrap/authority use base64 signature/key fields where defined, whereas host
`aok-control-proof/v1` uses hex public-key/signature fields. Service secret key
references resolve base64-encoded 64-byte Ed25519 private-key material. Generate
and encode through the contract-specific signer; do not reuse one generic JSON
signing routine or key across all roles.

## Services and protected storage to provision

| Component | Exact configuration | Qualification evidence |
| --- | --- | --- |
| Unix worker | Exact OS/arch/filesystem, pinned Codex 0.162.1 binary/hash, pinned gh version/hash, protected HOME/PATH/config census, worktree/repository base, model selector and P2 config/profile | Native identity/access, binary/config immutability, ambient credential isolation, filesystem/network denial, real process/child stop. Windows collector constructors currently refuse |
| Native enforcer/observer and issuer | Independently managed enforcement and account/config observation implementation | Measured canary tests and signed scoped observations; AOK's string/hash comparisons do not implement this service |
| Authority HTTPS `/v1/authority` | Private canonical SQLite path, server ID, approved enrollments/tokens/issuer registry, response key, pinned anchor endpoint/key/stream | Single current epoch, finite cumulative budgets, one-use effects, durable stop, nonce replay rejection, parallel claimants and recovery/service-loss refusal |
| Anchor HTTPS `/v1/anchor` | Existing private retained stream directory, pre-provisioned canonical genesis `STREAM.00000000000000000000.json`, stream ID matching authority server ID, initial tip, separate token/response key | CAS one-winner, signed contiguous appends, corruption/missing history refusal, actual retention/WORM and restore resistance. Reference cap is 10,000 appends; prove enough allowance before starting |
| Verifier HTTPS `/v1/candidates` and `/v1/execute` | Separate UID/artifact root, pinned policy/spec/rubric, measured Docker client, immutable image digest, fixed Unix daemon socket, empty protected client config, CPU/memory/PID/output bounds | Upload/hash/proof validation; network-none/read-only/credential isolation; accepted/rejected/timeout/cancellation cases, observed container removal; exact signed result lineage |
| GitHub broker and independent readback observer | One approved installation/PAT route, explicit token resolution, principal mapping, exact issue scope and current credentials | Actual read/write permission and denial tests, token renewal without new effect allowance, complete marker/body readback and signer identity |
| P7/P3/P8 state and handoff | Initialized runtime ledger, accounting/reservation store, outbox, preserved base/dirty/untracked snapshot and protected acceptance projection | Claims/fences, launch/send intent ordering, conservative holds, interrupted handoff, stale-clone and backup/restore drills |
| Evidence custody and monitoring | Immutable redacted retained assets, clock synchronization, proof renewal signer, stop monitor and incident/recovery owner | Original chronology/hashes, audit access, expiry/revocation/service loss and stop behavior; no model-issued acceptance |

At least worker, authority and verifier need independently protected custody and
execution boundaries. Anchor history must also survive authority-database restore;
use a separate retained storage/custody domain or a qualified compatible endpoint.
It must not be copied into worker/authority backups. Merely running four processes
under one task-writable UID does not establish these boundaries.

## Frozen non-secret enrollment package

Supply a versioned inventory containing:

- Disposable repository URL/key, native tenant/owner ID, immutable repository ID,
  immutable issue ID, positive REST issue number and exact mapping, API origin
  `https://api.github.com`, pinned supported API version, principal and auth provenance.
- Exact native executable versions/hashes, host driver/protocol, OS/architecture,
  workspace/base revision, execution profile bytes/hash, P2 configuration ID,
  exact selector/reasoning/tier, tool/data/region requirements and input/packet hashes.
- Account/workspace/pool/source/stream bindings; every applicable current quota
  window, generation, observed ordinal/time, remaining/headroom/reset and demand
  unit. Unknown values remain explicit.
- Policy, verifier/rubric/command/image hashes, trusted public issuer registries,
  current revocation generations, service URLs, public response keys, certificates,
  storage identities and opaque secret references.
- Stable parent, grant, trial, run, attempt, operation and epoch/holder identities;
  exact complete binding map; approved spend currency/maximum, consumption unit/
  maximum, trial count, host/verifier limits, qualification budget and stop rules.
- For ordinary selection: qualified quality/demand estimates and signed source
  shards, partition/cohort/protocol identity, and the exact selected P4 decision.
- For provider send: final immutable comment asset/text digest, accepted candidate
  and handoff evidence, capability/identity/binding, exact P8 grant, current revision,
  operation digest and independent reconciliation evidence when available.

Do not pick a symbolic model label as a CLI selector or substitute an inexpensive
unqualified model. Cheap planning subagents do not alter pilot model requirements.

## Proof freshness and renewal

Authority requires these exact live gates before startup and again on requests:
`controls`, `identity`, `capability`, `capacity`, `verification`, `runtime`,
`authorization`, `platform`, `provider-read`, `provider-operation`, `witness`,
`budget`, `stop-monitor`; `quality` is added for `controlled-evidence-collection`.
Each issuer role is `<gate>-issuer`, distinct from the enrolled worker principal.
Proofs bind `usage.Digest(TrialScope)`, a measured evidence digest, issuer, origin
`live`, original observation time and expiry, with validity at most five minutes.
Additional readiness/driver/completion requirements come from the protected route
policy; the 13 authority gates do not replace them.

Native filesystem/network/ambient-configuration control proofs also have at most
five-minute validity and bind the exact host, executable, workspace and control
scope. Bootstrap envelopes/enrollments have at most 24 hours, which does not
extend these shorter proofs. Capacity retains its own stricter P3 freshness.

There is no hot-renewal endpoint. Issue fresh exact-scope proof/enrollment/envelope
records through the protected signer and perform quiescent, controlled restarts
between attempts, preserving databases, parent/protocol/trial/operation IDs and
consumed allowance. Renewing credentials or grants never retries an effect,
refunds a budget or clears a permanent parent stop. The bootstrap matrix has a five-minute total deadline, ten native starts and
thirty seconds per turn. If it cannot complete within that bound, the result is
incomplete; extending the transport or restarting under a new operation needs
separate explicit approval and a new conservative capacity bound.

## Dependency order and acceptance

| Milestone within existing phases | Dependencies and operation | Pass criterion |
| --- | --- | --- |
| Repair/prepare | Close the pre-enrollment gaps; name custodians and freeze route/input/limits | Positive installed-path fixtures and independent review; stock build still blocked; all required services/observers identified |
| Provision protected domains | Operator keys, TLS, storage, native login, broker, pinned executables and verifier image | Isolation/access/storage inventory measured; no secrets in task/evidence; system TLS trust works |
| Independent synthetic qualification | Root-pinned fixed qualification action with an independently approved one-use anchor; no pilot admission | Synthetic fixtures may exercise one bounded provider action, but do not establish live control/identity/provider/authority/verifier/capacity/monitor evidence. Review external measurements and proof issuers independently before signing live proofs; the live gate remains pending |
| P6 host + P3 capacity | Scoped synthetic enrollment; at most ten starts/five minutes and 30 seconds per qualification turn | Exact pinned-version/account/model protocol, denial/exfiltration tests, actual stop, source ordering/windows/external consumption/reservations, and valid causal attribution |
| P5/P8 GitHub | Exact read identity, implemented mapped writer, separate synthetic comment authorization | Fixture proves numeric scope and POST/readback agree for a root-signed immutable-ID→REST-number mapping; live scoped reads, broker permission/identity, independent complete readback, provider conformance, lost-reply reconciliation and duplicate-effect denial remain pending |
| P8 verifier/handoff + P7 recovery | Implemented post-run stages; qualified controls, independent verifier/acceptor and retained anchor still required | Fresh proof binds the actual candidate; one-use grant precedes upload/execute; acceptance projection and handoff attach to runtime lineage. Live verification, acceptance, recovery, expiry/revocation, and service-loss drills remain pending |
| Sign/start short-lived enrollment | All prerequisite live evidence, finite budgets and frozen IDs | Pin build root; inspect signed config with explicit as-of; startup and every action validate exact current scope; no model-selected key/endpoint or fixture promotion |
| Calibration rehearsal | All non-quality gates plus separate calibration grant and explicit upper bounds | One bounded trial, conservative accounting, independently accepted output, preserved local handoff and separately authorized experimental-evidence comment/readback |
| P4 cohort qualification | Frozen protocol/custody, sufficient independent evidence | At least 30 validation families for demand and 30 distinct locked-test quality families; actual one-sided 95% Wilson lower bound clears 0.80 standard-risk floor; finite 95% conformal bound; no repeat inflation, leakage or censored-zero imputation |
| Ordinary P4-selected P9 trial | Qualified cohorts, exact live readiness, fresh capacity and all services | Selected advisory decision plus P7 admission/reservation, one native launch, accepted handoff, exact separately granted comment, exactly one complete correlated readback; stable-ID replay has zero additional effects |
| Production decision, still separate | Qualified fixed route, at least 100 eligible observed shadow decisions and preregistered assessment | Independent external promotion grant; first rehearsal/trial alone never qualifies |

For causal quota attribution, provide a supported direct measurement source or an
independently demonstrated exclusive account/window protocol with sufficiently
precise observations and no concurrent consumers. Aggregate percentage changes
and token counts cannot establish subscription demand. If this cannot be proven,
keep uncertain holds and the affected live/pilot gate blocked. Do not cross a
known quota reset during an attempt; P3 cross-window settlement stays fail-closed.

The GitHub first-route readback is one complete page of at most 100 comments.
Use a new quiet issue; a continuation, duplicate marker or ambiguous body blocks
completion and never authorizes resend. Qualify bounded full pagination before
using a busier issue.

## Run ceremony and retained evidence

After readiness, operate the existing stages sequentially: scoped issue capture
and packet mapping; fresh quota/readiness/selection; one fenced execution;
post-output candidate proof and independent verification/acceptance; preservation
and handoff; separately approved comment asset and P8 grant; single dispatch and
signed complete readback. New candidate and provider proofs are issued after their
actual artifacts exist, not fabricated in a pre-run config.

Preserve a create-only redacted bundle with public roots and build inventory,
original signed measurements, source artifacts, catalog/profile/packet hashes,
P3 observations/holds/attribution, P4 shards/estimates where required, P7 journals,
P8 candidate/verifier/acceptance/handoff lineage, broker identity/permissions,
comment/grant/outbox/send/readback, incident/recovery results, and an independent
scope-specific pass/block verdict. Private keys, token values and auth-cache bytes
are excluded. Any unknown stop/accounting/effect remains unresolved.

The immediate operator handoff is the frozen inventory, named custodians, access
and opaque secret references above. No secrets are needed in chat. There is no
scheduled live run until implementation dependencies and every required measured
gate have passed.

## Sources

Current implementation: `internal/enrolled/bootstrap/{bootstrap,worker,github_write,qualification,qualification_github,postrun}.go`,
`internal/enrolled/authority`, `internal/enrolled/codex`,
`internal/enrolled/github`, `internal/hostbridge/controls.go`,
`internal/connector/providers/http.go`, `internal/tracker/write.go`,
`internal/verification`, `internal/pilot/experiment`, and `internal/runtime/trials.go`.
See [service operation](../enrolled-live-services.md),
[enrollment integration delivery evidence](enrollment-integration-delivery-evidence.md),
and [prior delivery evidence](codex-github-live-unblocking-delivery-evidence.md).

Official OpenAI authentication distinguishes ChatGPT subscription login from API
billing, and warns that cached credentials must stay private. The initial AOK
route deliberately retains native login. Current App Server documentation is not
a guarantee of compatibility with AOK's pinned client; capture that version's
actual schema/protocol evidence. [Authentication](https://learn.chatgpt.com/docs/auth),
[App Server](https://learn.chatgpt.com/docs/app-server).

GitHub supports installation tokens and fine-grained PATs for issue comments,
with Issues write permission for comment creation. Restrict repository/permission
selection through the broker and prove actual grants; the AOK one-comment boundary
is narrower than that provider permission.
[Issue comments](https://docs.github.com/en/rest/issues/comments),
[installation tokens](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app).
