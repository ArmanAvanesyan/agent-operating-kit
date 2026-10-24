# Unblocking the first Codex + GitHub P9 pilot

Date: 2026-10-24. This is completion work within P3–P10, with the first live
experiment in P9. It is not another phase or an authorization to use credentials,
spend quota, invoke a model or mutate a tracker during planning.

## Target and decisions

Qualify one local Codex App Server route, one exact configuration/profile and
account/pool, one disposable repository and one GitHub Issue comment operation.
The first endpoint is independently accepted code, preserved local handoff and
one reconciled evidence comment. It grants no issue-completion, publication,
merge, release or deployment claim.

Keep standard risk, one candidate, one step, concurrency one, one attempt,
empty fallback, no autonomous child agents and no pilot resume. Retain the
60-second host and 30-second verifier limits. Qualification itself has a separate
explicit synthetic test budget: the current P6 matrix allows at most ten turns
over ten minutes, with 30-second turn bounds. Its resume test does not enable
resume for the pilot.

The completion coordinators, experiment contracts and scoped readiness are
implemented. The remaining work is concrete protected-service implementation,
deployment, enrollment and actual conformance/evidence collection. The stock
binary installs none of those services. Credentials alone cannot unblock it.

Distinguish the first **calibration-purpose end-to-end rehearsal** from the first
**ordinary P4-selected P9 pilot**. The rehearsal may precede sufficient quality
evidence under its separate signed calibration enrollment. It still requires
all non-quality guards, independent acceptance and a separately authorized
calibration-evidence comment. Its success does not satisfy the ordinary pilot's
quality gate or production promotion.

## Services and deployment

Use three independently protected trust domains. Services may share a deployment
within a domain; they need not become a collection of separate daemons.

| Domain / service | Implementation to install | Qualification evidence |
| --- | --- | --- |
| Worker: enrolled AOK runner | A compiled Go entrypoint in the AOK module registering `Phase9Services` and `Phase10Services`; captured enrollment selects fixed implementations, never executable plugins from task data | Build digest, protected operator configuration, registration inventory, rejection of task-supplied keys/endpoints and stock-binary blocking regression |
| Worker: native Codex boundary | Exact approved Codex executable and account; real filesystem, network and ambient-configuration enforcement | Independently observed denial/exfiltration tests, actual process/child cleanup, signed short-lived proofs; native settings or signatures alone are insufficient |
| Worker: collector supervisor | Qualified implementation of `collectors.CodexChannelFactory`, supplying only the pinned App Server/account; serialized capture ordinals and pool identity | Read-only method allowlist, five-second read bound, account/source binding, simultaneous windows, reset/freshness/order handling, no invocation side effect |
| Worker: P7/P3/P8 local state | Existing runtime ledger, capacity ledger, outbox and preservation engine with protected paths | Claims/fences, launch-intent ordering, conservative uncertain holds, snapshot inventories and restart/recovery drills |
| Authority: policy and identity | Readiness `PolicyAuthority`/native validators, `TrialServices.CheckTrial` and `VerifyTrialLaunch`, issuer registry, current revocation and acceptance projection | Exact principal/role/route checks, account pseudonyms, expiry/revocation/service-unavailable denials, actual-launch binding |
| Authority: witness and budget | `operations.AuthorityWitness` plus independently qualified `WitnessVerifier`; external atomic trial consumption; `TrialServices.ConsumeTrialBudget` and campaign stop monitor | Single active epoch, durable consumption across restore/renewal, finite cumulative caps, concurrent claimants, revocation and incident-stop behavior |
| Authority: evidence custody | Freeze/audit-open witness, approved outcome-shard issuers, immutable retained evidence and protected audit access | Dataset/partition/protocol hashes, trusted chronology, receipt signatures, retention/access log and duplicate detection |
| Authority: GitHub broker | Opaque credential resolver, scoped `providers.NewWriter`, exact dispatch resolver, independent readback collector/signing role | Correct repository/item mapping, approved principal/auth mode/API, one-use grant/outbox dispatch, complete correlated readback and response-loss recovery |
| Verifier: isolated command service | Implement the project-defined HTTPS `/v1/execute` protocol or another qualified `verification.Controls` implementation; separate acceptance authority | Candidate/spec/rubric/image hashes, isolation and bounded execution, signed verifier and acceptance receipts, rejected/timeout/revoked cases |

Do not put witness state or its issuer keys inside worker backups. Recovery must
contact the independently held epoch and consumption authority. Local SQLite
snapshots are single-store snapshots, not atomic snapshots of all three ledgers.

The remote-verifier client sends snapshot identifiers, not the source tree. Add
an authenticated immutable candidate-transfer/materialization path: the service
must independently hash the exact tree before executing the pinned command. It
must not execute against a mutable worker directory or fetch arbitrary URLs from
task content. A zero exit code remains invocation evidence until an independent
acceptance receipt and current projection are recorded.

For capture, wire `tracker.CaptureSource`/`CaptureWithCheckpoint` to an injected
`GitHubTransport.Runner` with a pinned `gh` executable and broker-controlled
environment. The existing Phase9 CLI dispatch/reconciliation hooks do not, by
themselves, install that capture runner. Use the same explicitly enrolled App
installation principal across read/write with separately scoped tokens, or prove
an explicit approved identity mapping; do not quietly use a personal ambient
`gh` session for capture and an unrelated App for writeback. Skip GitHub Projects
discovery for this issue-only route so Projects permissions are unnecessary.

## Credential and key inventory

Secrets stay in the appropriate credential store or broker; enrollment records
contain references, identity pseudonyms, scopes and expiry, never secret values.
The agent tools receive neither GitHub credentials nor signing/witness keys.

| Credential / key | Required scope and custody | Evidence required before use |
| --- | --- | --- |
| Codex native session | Operator-created ChatGPT subscription login in an isolated approved user/configuration context; enroll the actual account/workspace and model access | `account/read` confirms the expected ChatGPT auth type; fresh supported limit observations and a bounded successful selected-model invocation |
| GitHub read identity | One disposable repository; issue and comment read access, through the existing injected `gh` reader under a protected read policy | Immutable owner/repository/issue IDs, numeric issue mapping, principal, API version, pagination, denial and privacy evidence |
| GitHub write identity | Prefer a GitHub App installation restricted to that repository with Issues write; a separately enrolled fine-grained PAT is an alternative | Actual granted permissions/repository allowlist, principal/installation binding, expiry and successful/denied scoped operations; application grants restrict it further to one comment |
| GitHub App minting key, if selected | App private key held by the broker only; app and installation IDs are non-secret enrollment data | Issuance audit, restricted token response and rotation/revocation tests; token renewal keeps operation IDs and consumed effects intact |
| Verifier service credential | Dedicated service identity or bearer reference restricted to the pinned verifier endpoint and candidate scope | Authentication, TLS/destination binding, expiry/revocation and credential-echo denial |
| Control-proof and readiness issuer keys | Protected enforcement/authority roles, public trust roots supplied independently of evidence | Actual control measurements plus signature, role, scope and freshness checks |
| Verifier, acceptance, readback, outcome and custody keys | Distinct approved signing roles; author/model cannot issue its own acceptance | Receipt binding, rubric/verifier identity, current issuer/revocation state and replay tests |
| Witness/budget/operator credentials | Runner can claim only its enrolled route/trial/budget; no authority-admin access | External durability, role denial, cumulative-budget enforcement and stop/revocation tests |

This AOK route currently requires `native-login` and ChatGPT account type; it does
not qualify API-key or provider override fallback. OpenAI documents subscription
and API-key authentication as separate modes, with API usage separately billed.
Use the native login path for this enrollment. [Official authentication documentation](https://learn.chatgpt.com/docs/auth).

GitHub documents Issues write permission for issue-comment creation and supports
GitHub App installation tokens and fine-grained PATs. The plan excludes Contents,
Pull requests, Actions, Projects and administration write permissions. Repo
metadata access still needed for identity discovery must be captured in the read
enrollment. Issues write is broader than comment creation, so AOK's operation
grant and transport allowlist must remain narrower. [Issue-comment API](https://docs.github.com/en/rest/issues/comments).
App tokens can be restricted to selected repositories/permissions and expire
after one hour; minting/renewal belongs to the broker and cannot authorize a retry
or credential switch. [Installation token documentation](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app).

## Ordered gate closure

| Work in existing phase | Dependencies | Action and acceptance criterion |
| --- | --- | --- |
| P9 enrollment | Named operator and trust owners | Freeze exact platform/build, repository, issue IDs/number, configuration/profile, account/pool, control policy, verifier/rubric, mapping and API/auth mode. Set finite qualification, calibration and experiment budgets independently. Missing values block; no invented model selector or allowance. |
| P7/P9 service host | Enrollment and trust roots | Implement/deploy the protected entrypoint and services above. Prove services cannot be selected by a task, `.aok` configuration or arbitrary JSON. Map the complete pilot path to one P7 owner. |
| P6 native conformance | Control enforcer, native session, pinned binary | Discover without model invocation; then explicitly enrolled synthetic qualification. Verify boundaries, actual stop, version/account/model/session identity and all applicable qualification cases. Record unsupported controls as blockers. |
| P3 remaining capacity | Exact native identity and collector supervisor | Prove source ordinals, pool/window/generation binding, freshness, external consumption and reservations. Demonstrate a supported attempt-attribution/settlement mechanism; aggregate percentage deltas alone do not establish causality. |
| P5/P8 GitHub conformance | Read policy, broker identity, disposable issue | Capture exact scoped read evidence first. Independently authorize a synthetic marker-comment test, then complete readback. Prove auth/scope denial, rate limiting, duplicate markers, partial responses and ambiguous-response reconciliation without resend. |
| P8 verifier/handoff | Independent verifier, artifact transfer and current identity | Verify accepted/rejected/timeout/revoked candidates and exact receipt lineage; demonstrate dirty/untracked/manual preservation and interrupted-handoff recovery. |
| P7/P9 recovery | Qualified host, capacity, provider, verifier and witness | Drill fenced concurrent claimants, crash boundaries, revocation, lost responses, stale-clone activation, backup/restore and service loss. Zero duplicate launches/sends or released ambiguous obligations. |
| P4/P9 calibration | Qualified non-quality controls plus signed disposable grants | Run the dedicated calibration coordinator; collect signed outcomes and correctly attributable demand. Keep ordinary selection abstention intact. Establish the independent cohort criteria below. |
| P9 first controlled experiment | All applicable live gates and P4 evidence | Use the implemented experiment coordinator with ordinary P4 selection, fresh P3 admission and exact scoped readiness v2. Complete the seven-step run below. |
| P9 shadow/promotion | Qualified frozen route and controlled observations | At least 100 distinct eligible observed shadow opportunities, preregistered comparison/drill assessment and separate external promotion decision. The first trial is not a production promotion. |

GitHub conformance and verifier deployment may run alongside host/collector work,
under separate enrollments. Calibration may start after its non-quality guards
pass; requiring production quality first would recreate the bootstrap loop.

## Hard technical checks before scheduling live work

1. **Pinned client compatibility.** The current Codex bridge pins `0.162.1`.
   Record the installed version/hash without reading credentials. If it differs,
   validate and update the adapter/version fixtures first, or provision the
   approved version in the enrolled environment. Never bypass version checking.
   Current official App Server docs describe `account/read` and rate-limit RPCs;
   live qualification must test the actual pinned version's behavior.
   [Official App Server documentation](https://learn.chatgpt.com/docs/app-server).
2. **Credential isolation.** Codex must authenticate, while model-invoked tools
   must be unable to read its credential material or other services' secrets.
   Prove the actual process/tool filesystem and network boundaries with canaries;
   an allowlisted HOME or signed control assertion alone is not enforcement.
3. **Quota attribution.** The rate-limit API provides aggregate window state.
   Qualify a supported direct attribution source, or an exclusive account/window
   capture protocol with independently demonstrated absence of concurrent users
   and adequately precise measurements. If neither works, keep uncertain holds
   and the affected gate blocked. Tokens never become subscription percentages.
   An operator-approved unmeasured demand bound stays labeled as such; it does
   not supply measured attribution or release an uncertain reservation.
4. **Proof renewal and immutable identity.** Host `ControlProof` validation enforces
   a maximum five-minute interval; other receipts retain their own validity rules.
   The runtime also binds full policy/proof hashes and exact workspaces. Test
   fresh per-attempt enrollment and proof renewal under a frozen parent protocol
   and cumulative allowance. Preserve original dates and stable consumed IDs.
   If current hash contracts cannot represent safe renewal, implement a reviewed
   versioned identity/proof-envelope change inside P6/P7/P9 before the campaign;
   do not rewrite signed records or reset the budget.
5. **Bounded GitHub readback.** The current writeback slice reads one comments
   page with `per_page=100` and rejects incomplete evidence. Use a new quiet issue.
   If a continuation exists, stop or first implement/qualify bounded full readback
   pagination. Never interpret a missing marker on a partial page as non-delivery.
6. **Origin and authentication coverage.** The current writer allows public
   `api.github.com`. Enterprise hosts are separate work. P5's `gh-session` read
   transport and the HTTP broker writer must each prove the intended principal;
   App-token compatibility with the read runner is a conformance check, not an
   assumption. Use no automatic principal or credential fallback.

## P4 evidence collection before ordinary pilot admission

Preregister one narrow task class/language/risk and exact configuration/profile,
account and attempt/review protocol. Develop policy on development inputs; collect
at least 30 independent validation families for demand calibration before opening
at least 30 separate locked-test quality families. All registered repetitions
must independently pass for a quality family to succeed. Repetitions never
increase independent family count.

Use isolated repository/task-family groups assigned wholly to one partition;
the final single disposable pilot repository does not require recycling that
same repository across calibration and audit partitions. Each evidence attempt
gets its own exact input/repository binding under the frozen cohort protocol.

Require the actual one-sided 95% Wilson lower bound to clear the standard-risk
0.80 floor, and finite split-conformal demand bounds at
`ceil((n+1)*0.95)`. Keep failed/interrupted/censored attempts; missing consumption
is unknown, not zero. Freeze policy, estimates and audit access chronology; reject
family/repository/near-duplicate leakage and receipt/shard replay. Preserve the
existing quality/demand/context validity and stricter P3 freshness policies.

The old 48-packet, six-class, 24/12/12 pipeline experiment cannot satisfy these
claim-bearing per-cohort thresholds. A smaller end-to-end rehearsal can run as
separately enrolled calibration; its comment must explicitly identify experimental
evidence and must not claim normally selected work or production readiness.
Its eligible raw outcomes enter P4 only through the separately approved custody,
independent verifier and signed-shard pipeline. The ordinary route remains
blocked if sufficient evidence cannot be
collected within its enrolled budgets or valid measurement scope.

## First ordinary P4-selected P9 end-to-end run

1. Capture the enrolled issue, complete scoped assets and exact revisions. Use
   explicit mappings to produce the validated assignment/packet and task profile.
2. Recheck current authority, native controls, model access, account/pool, capacity
   and all readiness-v2 gates. Record the ordinary P4 selected decision; preserve
   any abstention without switching models or billing routes.
3. P7 claims/fences the exact run, P3 reserves bounded capacity, and the experiment
   coordinator consumes the enrolled trial/budget before native launch. Stop if
   the attempt could cross a known quota reset. No retry or fallback is implicit.
4. Execute one small deterministic Go change, retain normalized events and
   snapshot-bound output, observe actual process stop, and reconcile actual usage.
   Ambiguous stop/usage retains conservative obligations and blocks further work.
5. Independently materialize and verify the candidate, issue the scoped acceptance
   receipts and current projection, and preserve the local handoff. Rejected work
   never receives an accepted-work comment.
6. Prepare the exact evidence comment and obtain a separate P8 write grant binding
   its text digest, accepted artifacts, immutable issue/number mapping, principal,
   expiry and operation ID. Dispatch once through the external witness/outbox.
7. Read back the full scoped comment state and have the trusted readback service
   sign its observation. GitHub does not sign AOK reconciliation receipts. Require
   exactly one matching `aok-op` marker and expected content; on response loss,
   reconcile without automatic resend. Finalize local-handoff/comment milestones
   only and retain all failed/abstained/aborted attempts in their denominators.

Pass requires complete hash/identity lineage, independent acceptance, preserved
handoff, conservative reconciled accounting and the exact correlated comment.
Repeat the same logical IDs: no second model launch, allowance consumption or
comment may occur. Stop new admissions on unauthorized effects, leaks, unfenced
launches, duplicate sends, unknown quota, revoked authority, cap breaches or lost
monitor/witness. Cancellation never substitutes for observed stop.

## Evidence package, staffing and final decisions

Retain a create-only bundle referencing: enrollment and public trust roots;
service/build/platform inventory; native control/qualification evidence; P2 exact
catalog/profile evidence; P3 source/window observations, holds and settlement;
signed P4 shards/estimates and partition-custody receipts; P5 snapshot/mapping;
P7 claim/fence/intent/checkpoint journal; independent P8 candidate/verifier/
acceptance/handoff artifacts; comment draft/grant/outbox/send/readback; incident
and recovery drills; and a digest-bound verdict with expiry/revocation references.
Store only redacted evidence and secret references. Retain original observation
times and distinguish service-signed observations from vendor-authored material.

Use two Luna-medium workers in waves: one owns the pinned host/control/collector
integration and conformance harness; the other owns GitHub broker, artifact
transfer and verifier integration. The primary owns protected enrollment,
witness/budget/stop wiring, exact-route readiness and end-to-end assembly. A
separate Luna-high reviewer audits authority, attribution and recovery before any
live enrollment is activated. Agents do not become the independent credential
owner, signatory or outcome acceptor by reviewing their own fixtures.

Rerun focused and full Go 1.22 tests, affected races/multiprocess recovery,
Python/parity, kit/package/extracted-install checks, six CGO-disabled builds and
credential-free network-denied offline smoke after implementation changes. Run
connected checks only under their exact bounded enrollment. Report five separate
milestones: services deployed, route live-qualified, calibration rehearsal passed,
ordinary P4-selected P9 trial passed, and production promotion pending/authorized.
Unrelated Claude/provider/
P10 optional gates remain explicitly pending and do not acquire this route's
qualification.
