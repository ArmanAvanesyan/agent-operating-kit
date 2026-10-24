# Codex pilot write sandbox fix

Date: 2026-10-24. This closes a transport implementation blocker within the
existing P6/P9 work. It adds no phase and does not qualify a live route.

## Change

Codex 0.162.1 now derives its native sandbox request from the exact operator
filesystem permissions. No write intent retains read-only behavior. Supported
concrete paths inside the canonical approved workspace request workspace-write
at thread start/resume and workspaceWrite at turn start. The turn specifies
only the workspace root, denies network and excludes additional temporary
writable roots. Approval requests cannot escalate write permissions through a
broker. Unsupported globs, off-workspace paths, noncanonical paths and symlinks
fail closed. The direct entrypoint also binds prepared host, protocol version,
installed-version claim and model allowlist to the operator policy.

A native workspace-write request is broader than the pilot's single-file
permission. Before starting that transport, AOK requires execution authority,
a valid operator policy, approved workspace, fresh trusted signed filesystem,
network and ambient-configuration proofs, and the filesystem check
`codex-workspace-write-exact-permissions/v1`. The issuer must measure the actual
external exact-permission boundary under this transport, including denial of
other writes and protected credential access. The marker itself is not an
enforcer or proof of those measurements. Checks repeat before turn/start.
Existing P7 ownership, reservations, launch hooks and recovery remain in place;
P8 remains the acceptance authority. Synthetic signatures occur only in tests.

## Acceptance

The focused fixtures cover read-only/write/start/resume request construction,
validation against the checked-in 0.162.1 schemas, exact policy binding, missing
and actually expired signed proofs, tampered scopes, generic filesystem proofs,
unauthorized execution, unsafe paths, workspace drift, denial of broker
escalation and changed proof assets during the handshake. They assert no start
or no turn when blocked. They invoke no installed Codex process.

Final checks passed under Go 1.22.12: full `go test -p 2 ./...` (including
packaging regressions), relevant bridge/runtime/enrollment race tests, all 268
Python regressions with four existing skips, kit validation, Python entrypoint
compilation, formatting and whitespace. Terminal, nested-sandbox and local TLS
fixture tests were run outside the restrictive outer development sandbox.
The final suite used bounded package parallelism after an earlier concurrent
run hit fixture failures; the final frozen-tree suite and race run both passed.

Both `aok` and `aok-enrolled` built with CGO disabled on Darwin/Linux/Windows
amd64/arm64 (12 builds). These are build checks, not native certifications.
The rebuilt stock enrolled binary passed empty-PATH/isolated-HOME/network-denied
smoke checks, preserved disabled active services, and reproduced the same
blocked populated-enrollment preflight without modifying its inputs. This fix
was not installed or released, and no live qualification was attempted.

Two Luna-medium workers handled implementation and boundary audit; an
independent Luna-high reviewer identified the direct host-policy binding gap.
That finding was fixed and re-reviewed with no unresolved findings.

Final acceptance logs, source hashes, new binary hashes and the new preflight
report are preserved under
`build/enrollment/codex-github-first-live-run-sandbox-fix/`. The earlier task
packet/profile/hash bundle and its historical blocker report remain unchanged.
The new report supersedes only the historical hardcoded read-only implementation
finding. It does not renew any observation or qualification evidence.

## Remaining gates

The current enrollment still lacks four account/pool/operator/recovery identity
fields, seven scoped opaque credential references, 23 signed/evidence inputs,
and 15 external qualification/production gates. The exact-file external write
control and protected isolated workspace have not been measured or enrolled.
Codex/Claude live conformance, per-account capacity/attribution, protected
identity/anchor/verifier services, independent candidate verification and
acceptance, provider read/write/readback, P4 cohorts, the first P9 trial and
production activation remain blocked or pending. Existing personal native
login does not replace these inputs. Profile/configuration hashes identify
intent; they do not grant admission. No live model invocation, provider change,
service activation or pilot execution was performed for this fix.
