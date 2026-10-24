# Exact-file tool isolation implementation status

Date: 2026-10-24. Existing P6/P9 work; no phase added.

**Incomplete: this is a prototype, not a qualified operational sandbox.**
The worker, parent request interface and launcher are implemented, but the
positive and adversarial Darwin OS fixtures do not complete successfully.
Finite file-existence grants moved the synthetic startup failure from
`sandbox-exec`/`execvp: Operation not permitted` to child SIGSEGV. This is partial startup progress. The signal is observed on the sandbox-exec
command, so the faulting instruction/process and entry into Go worker code are
unverified; worker bootstrap and OS conformance remain failed. Native Codex 0.162.1 tools are not connected to this
component, and parent native authentication/tool isolation is not established.

## Implemented and reviewed

- Closed bounded request/policy/result records; fixed target bytes; exact read
  allowlist; descriptor-relative no-symlink access and hardlink rejection.
- Parent Session with immutable policy capture, required admission callback,
  one create dispatch even on ambiguous failure, finite reads and no retries.
- Verified private worker staging and protected-file checks, empty child
  environment, stdio plus one scoped directory descriptor, result validation,
  bounded output and generic/redacted errors.
- Deny-by-default Darwin launcher with finite file grants and network denial,
  absolute hash-pinned Apple loader-support import, and bounded process cleanup.
  The directory descriptor is a capability, not authentication of its parent;
  the helper is private and is not a standalone secure execution command.
- Offline `aok host isolation-check`, which always retains blocked execution
  and unestablished OS/native qualification. It does not run a worker or model.

Two Luna-medium workers implemented the worker/launcher; independent Luna-high
review found workspace-wide reads and a hash/exec race. These were corrected
with exact read paths and verified staging. The final review found no additional
concrete security issue by inspection, while retaining the private-helper trust
assumption and the failed OS acceptance gate. This is not security certification.

## Acceptance evidence

Local evidence is under
`build/enrollment/codex-github-first-live-run-tool-isolation/`.

| Check | Result |
|---|---|
| Full Go 1.22.12 suite, default fixture set | Passed; opt-in OS tests are separate |
| Relevant race suites | Passed |
| Python regressions | 268 tests passed, four existing skips |
| Kit validation, Python compilation, formatting | Passed |
| CGO-disabled builds | aok, aok-enrolled and internal worker on six targets |
| Offline inspection/preflight, empty PATH/HOME, network denial | Passed with expected blocked results |
| Actual Darwin positive worker operation | Failed: child SIGSEGV after finite exec-transition fix |
| Actual Darwin adversarial OS probes | Failed before probe results; denials are unproven |
| Native Codex delegated-tool integration | Unsupported/unverified in captured pinned protocol |
| Final-root controls, authenticated native parent, provider/pilot/production | Blocked/pending |

The Apple loader bootstrap was diagnosed from its local system profile and
synthetic worker crash metadata. The current profile keeps deny-default.
Automatic approval review previously rejected proposed broad diagnostic rules.
The operator subsequently approved one disposable-only Unix-syscall diagnostic
with synthetic data. That run was performed once: neither worker reached its
positive control. The broader rule did not resolve startup; it did not identify
a missing syscall or establish any denied capability. Its closed loopback
listener and absent parent canary also made those negative controls inadequate.
No security claim uses that diagnostic. Its exact source, rule diff, hashes and
redacted results are archived under
`build/enrollment/codex-github-first-live-run-syscall-fix/`. The broad diagnostic
is no longer an executable Go test and no production broad grant was added.
The original frozen evidence remains unchanged.

Subsequent finite-profile trials added `file-test-existence` to the staged
executable and literal ancestors, approved read paths and existing runtime
resources. Apple's local loader-support profile documents existence checks.
The synthetic failure mode advanced from exec rejection to SIGSEGV; the signal
does not locate the fault or prove worker main began. No target creation or
adversarial result completed. Tests now require a successful
approved read, an open loopback control and a parent environment canary.
Their bounded failure classifier kills the synthetic process group on timeout.
Independent Luna-high review found no additional concrete issue in the final
fixture changes and confirmed that they do not qualify native/live execution.

Automatic approval review rejected an attempted acceptance run for a proposed
expanded production syscall allowlist, citing insufficient authorization for
that expansion and its process/memory/thread/file blast radius. The unvalidated
syscall expansion was reverted, and no alternate execution path was used.
That rejection does not apply to the earlier approved one-run disposable
diagnostic; the two actions and their evidence remain separate.

## Approved bounded test-only follow-up

A separate operator approval authorized the exact finite diagnostic described in
the prior local `FOLLOW-UP-DIAGNOSTIC.md`, with production policy unchanged.
Two Luna-medium workers prepared the diagnostic and acceptance checks; an
independent Luna-high reviewer checked the source before execution and audited
the evidence afterward.

Exactly one invocation and one sandbox child ran on macOS 15.8.1 (24H32), native
amd64, with the pinned Go 1.22.12 worker. The first child terminated with SIGSEGV,
without stdout, stderr or timeout. Target verification failed, so the second
child was not run. The diagnostic does not identify the faulting instruction or
an exact denied syscall. No negative control or OS-conformance claim follows.

The explicit additional Unix syscall numbers existed only in the disposable
test profile; no production source or policy changed. Exact profiles, policies,
command/input hashes, the approved scope, signal status and source baseline are
preserved under
`build/enrollment/codex-github-first-live-run-bounded-followup/`. The synthetic
external fixture was removed. The diagnostic source was archived as inert
`.txt` evidence and removed from executable Go tests. No retry, alternate policy,
model invocation, control-proof issuance or tracker mutation occurred.

The independent audit verified all 13 baseline source assets, profile and policy
hashes, the one-child limit, cleanup, and the absence of a readiness claim.
Offline regression and preflight reports are separate from this failed OS
acceptance result. P6 host/native isolation, provider qualification, P9 admission
and production gates remain blocked or pending. This is existing P6/P9 work;
no phase is added.

## Preflight and remaining inputs

Fresh preflight is blocked: accountRef, poolId, operator and recoveryOwner;
seven opaque credential references; 23 signed/evidence inputs; 15 external
qualification/production gates. Task/profile/configuration hashes and prior
enrollment files remain unchanged. No proof, signature, real credential,
activation or provider mutation was produced. No pilot workspace was created
and no model was invoked. Initial fixture evidence from other directories
cannot qualify the exact enrolled root.

See [the component guide](../exact-file-tool-isolation.md) for its interface,
trust assumptions and unsupported native connection. The first P9 pilot cannot
start on this prototype. A passing compile or default test suite does not
resolve either the OS failure or the native transport gap.
