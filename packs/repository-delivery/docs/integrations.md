# Planning and optional integrations

Updated 2026-10-18. These helpers extend the [delivery workflows](delivery.md); the [design](design.md#ownership) defines ownership. The plugin remains usable without AOK, swarmd, GitHub, or a hook installation.

## Command entrypoints

From the project directory, inspect the current arguments with these commands:

```bash
python3 scripts/repo-delivery backlog plan --help
python3 scripts/repo-delivery backlog apply --help
python3 scripts/repo-delivery integration run --help
python3 scripts/repo-delivery aok consume --help
python3 scripts/repo-delivery runtime task --help
python3 scripts/repo-delivery runtime reference --help
python3 scripts/repo-delivery runtime finish --help
python3 scripts/repo-delivery hook install --help
```

Metadata outputs use new files outside the protected source and preservation locations. The [schemas](../config/schemas/) document emitted records. Backlog findings and contract mappings remain host decisions; the helpers validate structure and identity rather than infer product intent.

## Architecture to backlog

The [architecture-to-backlog skill](../skills/repo-delivery-architecture-to-backlog/SKILL.md) compares current implementation, ADRs, research, and roadmap evidence before proposing work. Research findings describe possibilities; the current source establishes what is implemented. The host supplies findings and the intended issue matches; the helper validates and binds them to source bytes rather than inferring architecture semantics. A backlog plan requires implementation, research, ADR, and roadmap source roles with content hashes, a pinned baseline, dependencies without cycles, and action/expected-observation acceptance criteria. An explicit issue number or stable owned marker identifies reuse; title similarity alone is not automatic matching.

GitHub owns the resulting issue state. A saved local plan is distinct from a successfully applied remote change. Publication requires the explicit hosted repository binding and `update-backlog` authority. It reconciles paginated remote issues and replaces only its marked section, retaining surrounding human text. Closed matched issues are retained without reopening them. Inspect remote state after an uncertain write before retrying; a local digest does not authenticate plan authorship. Each live issue must resolve to a distinct work item, and edits are checked again after writing. GitHub body edits have no compare-and-swap guard: re-fetching detects observed concurrent edits but cannot exclude the final remote write race atomically.

## Real integration evidence

The [real-integration-proof skill](../skills/repo-delivery-real-integration-proof/SKILL.md) records commands against an explicit service boundary. Real means the required boundary used the intended service; mock means it was substituted; mixed means only part of the boundary was exercised against real services. Running a container does not establish that a test used it.

The helper probes each declared real service at an HTTP(S) JSON version endpoint before and after the context's gate commands. It requires an exact version match, bounds responses, and refuses credential-bearing URLs, queries, fragments, and redirects. Mock versions remain declared labels. A successful version probe establishes availability and version at that endpoint; the host must still ensure the gate exercises the intended boundary.

Proof belongs to the recorded source snapshot and selected environment. Record service versions and output evidence alongside command outcomes. An unavailable required service produces a blocker, not a passing real result. Local receipts describe observed execution and supplied environment assertions; they do not authenticate services, establish remote CI success, or replace a repository's required gate.

## Reproduce explicitly mocked evidence

This disposable example demonstrates a mock label, not application integration coverage. Run it from the project directory. The fixture uses no remote service or live runtime.

```bash
proof_example=$(mktemp -d)
git init -q "$proof_example/source"
git -C "$proof_example/source" config user.name 'Repo Delivery Example'
git -C "$proof_example/source" config user.email 'example@localhost'
printf 'fixture\n' > "$proof_example/source/example.txt"
git -C "$proof_example/source" add example.txt
git -C "$proof_example/source" commit -qm 'Create example baseline'
cat > "$proof_example/context-spec.json" <<'JSON'
{"objective":"Label mock fixture honestly","files_in_scope":["example.txt"],"gate_commands":[["git","diff","--exit-code"]],"review_policy":"none","authorized_actions":[],"actor_id":"example","actor_context":"example-session","integration_required_mode":"mock"}
JSON
cat > "$proof_example/integration-spec.json" <<'JSON'
{"required_mode":"mock","services":[{"name":"fixture","mode":"mock","version":"mock-1"}]}
JSON
python3 scripts/repo-delivery delivery init --repo "$proof_example/source" --baseline HEAD --spec "$proof_example/context-spec.json" --output "$proof_example/context.json"
python3 scripts/repo-delivery integration run --context "$proof_example/context.json" --spec "$proof_example/integration-spec.json" --output "$proof_example/proof.json"
python3 scripts/repo-delivery integration verify --context "$proof_example/context.json" --proof "$proof_example/proof.json"
python3 scripts/repo-delivery delivery status --context "$proof_example/context.json" --gates "$proof_example/proof.json"
```

The proof can pass this explicitly mocked policy. A context with `integration_required_mode` set to `real` rejects mock or mixed proof even if the declared gate passes. When this policy field is set, delivery, PR, and hook readiness require the integration proof in place of a plain gate receipt. Real fixtures in the [integration tests](../tests/test_integration.py) exercise a temporary HTTP service; those tests do not claim production sidecar coverage.

## AOK contract consumption

AOK owns portable policy and work definitions. An adapter may inspect and reference those definitions, but cannot rewrite their meaning or infer that documentation mappings establish an active runtime connection. The consumer validates the packet and optional policy against caller-supplied offline JSON Schema documents pinned by SHA256. External schema references are rejected. The packet schema identity and digest establish the version boundary; no packet version field is invented. The result preserves original documents and proposes objective, acceptance criteria, and verification strings without executing them. Shell-like verification strings require host-reviewed argument mapping. Upstream status, including `blocked`, remains desired state; no runtime status is inferred.

Install `jsonschema>=4.26,<5` in a Python 3.11+ virtual environment, then set `AOK_DELIVERY_PYTHON` to that environment's interpreter. AOK never installs dependencies automatically. Other delivery commands do not require jsonschema; no sibling AOK or repo-delivery checkout is required.

Repo delivery owns local workflow evidence and invocation of repository gates. No AOK setup, provider customization, credential binding, or migration is implied by loading an adapter.

## swarmd runtime references

swarmd owns live tasks, claims, durable execution attempts, and fencing. Repo delivery may record references to supported runtime contracts, but it does not keep a parallel authoritative task queue or substitute its local delivery status for a runtime transition.

The adapter supports MCP initialization and read-only `task.tree` through a caller-supplied stdio command. It can consume an exported `execution_attempts` NDJSON dump without opening the runtime database, accepting only a durable recorded/complete/failed revision checkpoint. References require exact attempt, task, worker, revision, tree, and environment identity. They are observations rather than a live lease check.

Managed finalization uses swarmd's `task.finalize` capability. The adapter discovers the tool and validates its required input schema before supplying task, attempt, worker, revision, tree, environment, expected recorded outcome and an opaque lease credential. `success` asserts the recorded outcome; it cannot replace recorded failure with success. The service verifies immutable evidence and current lease ownership, then commits task, attempt and configured review handoffs atomically. Older servers without this capability fail closed; there is no fallback to legacy `task.complete` or `task.fail`.

The producer still starts, heartbeats and records the attempt. This adapter grants no claim, lease issuance, checkpoint recording or recovery authority. Exported references remain observations until the service accepts a finalization request. Keep the lease credential in the explicitly supplied private credential file, outside source and preservation outputs; receipts and ordinary output never contain it. An uncertain response requires inspection and explicit reconciliation, not automatic retry. Completion of a task does not establish independent review approval or GitHub merge readiness.

`runtime finish` takes `--command` (a JSON argv file for the stdio server), `--reference` and `--expected` (the same exact six-field attempt identity), `--credentials`, `--outcome complete|failed`, and `--authorize finalize-attempt`. `--output` optionally writes a new receipt; `--timeout` bounds transport waiting. Authorization must reflect the user's existing task scope; the flag cannot create authority by itself. A receipt contains the observed task identity and terminal outcome, not the token.

The credential file must be an absolute, owned mode-0600 regular file of at most 16 KiB, with no symlink path components. Its exact JSON keys are `task_id`, `attempt_id`, `worker` and `lease_token`; the first three must match the expected reference. The reference files contain only `attempt_id`, `task_id`, `worker`, `revision`, `tree` and `environment`. Keep the credential file separate from those observational exports and do not pass its contents as shell arguments. Offline reference validation reports managed-finalization support as unknown; live discovery establishes capability and only the service transaction establishes current ownership.

An adapter must preserve attempt identity and fencing requirements when preparing handoffs. Contract inspection does not claim a live task was created, claimed, completed, or independently reviewed. This project's validation uses fixtures rather than mutating a user's active swarmd runtime.

## Optional pre-push companion

The Git pre-push companion is separate from the portable plugin and requires an explicit installation action and configured checks. Installing or invoking the portable skills does not install a hook. Hook execution can enforce local configured checks; it cannot grant publication authority, establish remote review approval, or supply operating-system isolation.

The POSIX companion validates existing current gate/review receipts and recorded push authority; it never reruns gate commands. Configuration and evidence must be private, owned by the invoking user, at absolute paths outside source without symlink components. Missing or stale evidence fails closed. Tracked, staged, and untracked source must be clean so the tested bytes match the pushed commit; only updates of the evidenced current HEAD are accepted. Installation into linked worktrees is refused because Git shares hooks across worktrees. The companion refuses an existing pre-push hook and a configured `core.hooksPath`. A hook failure blocks its invocation and reports the failed check. The host retains credential and process ownership. Existing hooks must remain intact; do not silently replace another tool's hook or claim that a hook protects pushes made through every possible client.

## Developer integration gate

The normal quality gate exercises temporary Git repositories, an actual loopback HTTP version service, simulated protocol peers and mocked GitHub boundaries. Two runtime tests use a caller-supplied swarmd binary and fresh temporary databases. Six additional managed-finalization tests require the test-only attempt fixture binary from swarmd's `tests/fixtures/attempt-finalize`; that helper creates recorded attempts through the real service and never supplies a production mutation interface.

Set `REPO_DELIVERY_SWARMD_BINARY` to the absolute swarmd binary path and `REPO_DELIVERY_SWARMD_FIXTURE_BINARY` to the fixture binary path, then run `make test-runtime-integration`. Set both variables for `make quality` to include all eight real runtime tests in the full suite. The original two tests are explicitly skipped without the runtime binary; the six finalization tests are skipped unless both binaries are configured. The fixtures never select a live queue. Record the binaries' source revision and any uncommitted source snapshot when reporting results.

The fenced-finalization integration is validated against swarmd commit `f7a515320f4e939e608b13c15dca8b7f4aa25b88`, proposed in [swarmd PR #50](https://github.com/ArmanAvanesyan/swarmd/pull/50). The companion CI job pins that exact commit so an older main cannot silently substitute a server without `task.finalize`. This pin records a tested dependency; hosted CI status and merge readiness must still be checked on the pull requests. See the [local validation and independent review](reviews/fenced-finalization.md).
