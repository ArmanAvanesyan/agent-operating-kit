# Phase 6 delivery evidence

Phase 6 adds opt-in Codex App Server and Claude Code execution adapters. The
core is implemented and fixture-qualified; **Phase 6 is not complete** because
both required live qualification gates remain blocked.

| Gate | Status |
|---|---|
| P6 core | Implemented; offline acceptance and independent review recorded below |
| Codex 0.162.1 installed executable/version discovery | Passed locally |
| Claude Code 2.1.303 installed executable/version and OAuth-login discovery | Passed locally |
| Codex bounded live invocation/conformance | Blocked: trusted signed control proofs absent |
| Claude bounded live invocation/conformance | Blocked: trusted signed control proofs absent |
| Claude native TaskCreate/Get/List/Update | Fixture-qualified observations; live gate pending |
| Codex local session/thread/turn identity | Fixture-qualified; separate cloud tasks unsupported |
| P2 invocation-probe publication | Implemented in qualification; live successful evidence pending |
| P3 usage export | Explicit bound observations only; no invented remaining quota |
| P7/P8 runtime admission and independent acceptance | Pending |

## Implemented surface

`aok host inspect`, `prepare`, `run`, `resume`, `replay`, `validate`,
`tasks plan` and `qualify` use the closed `aok-host-bridge/v1` records.
Preparation captures exact assignment/profile/task/instruction sources and
output schema, configuration identity, optional catalog digests and native-step
plan. Loading re-resolves the captured corpus and rejects rebinding of the
assignment, profile, task inputs, schema or task steps. Run rechecks the trusted
operator policy, complete configuration identity and fresh scoped controls.

The subprocess runner bounds frames, total output and elapsed time; handles
cancellation and process termination; and launches an exact hash-pinned
executable with an explicit environment. API-key environment routes, child
agent dispatch, unapproved ambient integrations and unsupported configuration
dimensions fail closed. Completed attempts cannot be resubmitted via resume;
failed or ambiguous outcomes require reconciliation. An initialized interrupted
session may be explicitly resumed under the same captured inputs. Local
ownership locks are not distributed runtime leases.

Codex uses stdio JSON-RPC with explicit initialization, account-mode checking,
thread start/resume, one turn, denial of approval requests and normalized event
capture. Claude uses print-mode stream JSON, exact model/effort selection,
explicit settings, strict empty MCP configuration, tool restrictions and exact
session IDs. Native task observations do not update AOK lifecycle state.

Execution produces a candidate result only after output validation. Every
report keeps `authorizationGranted`, `executionReady` and `accepted` false.
P1 bindings/run records are emitted only with a real captured work-item binding.
Explicit hash-bound usage assets are required for P3 export; missing information
stays unknown. The P2 smoke result records invocation only, never task quality.

## Evidence and trust boundaries

The external operator policy, including issuer public keys, is the trust root.
It must be protected outside task-controlled material. Signatures authenticate
proofs relative to those keys; they do not authenticate the policy owner or
certify a sandbox implementation. Scope hashes bind all exported policy fields
except the proof assets themselves. Fresh signed filesystem, network and ambient
configuration proofs are mandatory before execution or qualification launches.
No production control proof or issuer was fabricated to pass these gates.

The final connected checks recorded `enforcement-unqualified` for both hosts
before launching qualification subprocesses. Earlier development attempts did
not yield qualifying evidence and are not counted as successful invocation.
Local discovery confirms installed versions/login only. No claim is made that
both hosts passed live inference, cancellation, denied access or native tasks.

Required capability references remain blocked until native bindings can be
verified. Unenforceable cost, turn and tool-call limits block preparation.
Optional capabilities are not installed. Unknown catalog findings are not
support. General required-deliverable evidence verification remains blocked
where no supported verifier exists. These are explicit functional limits of
this core, not successful conformance results.

Qualification is limited to synthetic disposable workspaces, one attempt per
case, at most ten turns and ten minutes per host, thirty seconds for smoke,
no retry or fallback. Persisted qualification transcripts redact outputs and
payloads; the probe hashes the exact saved smoke evidence. Production commands
are opt-in and never perform tracker writes, billing changes, automatic model
selection or distributed quota reservations.

## Acceptance checks

Go 1.22.12 is used with module downloads disabled. The checked-in synthetic
fixtures cover both protocols; they contain no credentials or private responses.
Protocol reference schemas are pinned to the installed Codex 0.162.1 generator
and exact asset hashes under `internal/hostbridge/testdata/`.

| Check | Result |
|---|---|
| Full Go 1.22.12 suite | Passed, including release packaging/extracted-kit validation |
| Host bridge and evidence race suites | Passed |
| Python regressions | 267 tests passed; four skips |
| Python compilation, release-shell syntax, kit validation | Passed |
| Empty PATH, no credentials, blocked proxy | Both host replay/validate chains passed |
| Output overwrite attempt | Rejected; original receipts unchanged |
| CGO-disabled six-target builds | CLI and hostbridge/CLI test binaries compiled |
| Whitespace check | Passed |

Cross-target test binaries were compiled, not executed. Cross-test compilation
used `-vet=off` after an intermediate generated test file disappeared during
parallel work; the final complete native Go suite passed with normal vet.
Python confinement/TTY tests need host process-group access: the sandbox run
failed three such tests, and the complete authorized rerun passed. No product
checks were disabled to make those tests pass.

CI includes host/evidence race coverage, both offline replay chains and host
bridge cross-compilation. Independent review
examined prepared-input rebinding, control scope, cancellation/resume, evidence
hashes and privacy. Its actionable findings were corrected and regression tests
added. The reviewer also identified the operator-policy trust boundary, which
is explicitly documented above.

See [operator guide](../host-bridges.md) for syntax and prerequisites.
