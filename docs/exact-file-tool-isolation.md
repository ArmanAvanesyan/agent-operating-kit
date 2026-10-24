# Exact-file tool worker for the Codex pilot

This adds a separate, uncredentialed tool worker and a prototype Darwin Seatbelt
launcher. Finite file-existence grants moved the synthetic macOS startup failure
from `execvp: Operation not permitted` to child SIGSEGV. No worker operation
completes, and positive/adversarial OS conformance remain unestablished; this is not a delivered operational sandbox. An operator-approved
one-run broad Unix-syscall diagnostic also failed before its positive controls;
it did not identify a safe syscall fix. Its source is archived as inert evidence,
and its broad rule is absent from the delivered profile.
This component does not connect Codex 0.162.1's built-in tools to that worker.
The pinned protocol has no verified delegated-tool interface in AOK's captured
schema. Native Codex authentication/tool isolation and the first live pilot
therefore remain blocked. Do not treat passing worker fixtures as that evidence.

The separately approved bounded follow-up was also executed once with a
finite, test-only syscall list. Child 1 reported SIGSEGV with no output; its
target was unverified, so child 2 was skipped. The fault location remains
unknown. Production policy stayed unchanged, and the disposable test source
was archived and removed. See the
[delivery evidence](implementation/tool-isolation-delivery-evidence.md) for the
recorded scope, independent review and remaining gates.

## Implementation

`internal/toolisolation` separates a protected parent-facing Session from the
worker process. The parent supplies an immutable policy and an admission checker;
untrusted messages contain only a bounded read or create-target request. They
cannot choose an executable, environment, credentials, policy or workspace.
The Session rechecks admission and consumes a single create attempt before
sending it; errors do not automatically retry. This is an in-process dispatch
limit, not a replacement for P7 durable claims, holds, fences or recovery.

The worker creates only a previously absent `pilot-output.txt`, containing
`AOK pilot fixture v1` followed by LF (21 bytes), mode 0644. Descriptor-relative
opens reject symlinks and hardlinked reads; reads require an explicit trusted
allowlist, are bounded to 4 MiB, and cannot traverse outside the workspace.
Empty read lists deny file reads. The parent receives only the requested result,
with bounded output and generic errors. No arbitrary command tool is exposed.

On Darwin the parent stages verified worker bytes in a private directory outside
the workspace and launches them through `/usr/bin/sandbox-exec` under a
deny-by-default profile. The only writable user path is the exact target;
network access is denied. Required platform runtime resources and the worker
binary remain readable, so this is not a claim that literally every outside
read is forbidden. The child receives empty environment, stdio and one explicit
workspace directory descriptor; parent authentication caches, credential values,
proxy configuration and service descriptors are not passed. The worker validates
the inherited directory identity and cannot fall back to an unconstrained CLI
operation if it is absent. Timeouts kill the worker process group and wait.

The worker binary and staging directories require protected ownership. The
boundary is against the constrained tool process, not an unconfined hostile
process running as the operator or root. Final workspace, enforcer, executable,
platform and custody measurements remain independent operator qualification.
Other operating systems fail closed for OS-backed execution; cross-compilation
does not certify them. The worker executable is an internal implementation
detail and must not be used directly as a sandbox or authorization mechanism. A supplied
directory descriptor proves access to that directory, not authentication of its
parent. A local caller with that capability can invoke the helper without the
launcher; only the protected launcher applies Seatbelt. The helper is not a
standalone user-facing security command and is not added to release installers.

## Offline inspection

```text
aok host isolation-check --policy FILE --as-of RFC3339 --out NEW --json
```

The closed `aok-tool-isolation/v1` policy contains workspace, fixed target,
worker executable/hash, timeout and explicit read paths. The command safely
reads the policy and creates an immutable inspection report. It never runs a
worker or model. Exit 1 and `executionReady:false` retain the native-transport
and live-enrollment blockers even if policy structure is valid. Existing native
host commands, profile/configuration identities and execution defaults are
unchanged. No runtime or tracker authority is granted by this command.

## Qualification boundary

Disposable worker fixtures can test exact-target creation, denied other reads
and writes, absent inherited credential/proxy environment, denied networking,
process stop and immutable input/result checks. They never touch real native
login state, the enrolled pilot workspace or provider APIs. Follow
[the external control-proof preparation guide](enrollment-preparation.md) before
any independently authorized final-root measurements. Preliminary roots cannot
qualify a different enrolled path; baseline restoration and target absence must
be independently established before pilot admission.

The [official AppServer documentation](https://learn.chatgpt.com/docs/app-server)
describes native sandbox and experimental dynamic-tool interfaces. Documentation
for newer or experimental fields does not establish a supported delegated
execution path in AOK's pinned 0.162.1 bridge. No automatic API billing fallback,
new model route, credential switching or experimental native execution is added.

An actual supported native connection still needs protocol discovery and fixtures,
exact parent-account/tool boundary qualification, new scoped evidence and P7
admission integration. Protected issuer trust, fresh signed controls, quota,
independent verification/acceptance, GitHub qualification and production gates
remain separate. No production keys or control proofs are generated here.
