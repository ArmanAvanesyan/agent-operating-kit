# Embedded repository delivery

AOK maintains repository delivery in `packs/repository-delivery/`. The original
Python engine is retained; the Go CLI only discovers dependencies and forwards
arguments. This is an optional Linux/macOS capability. All other Go commands
remain Python-free. No source checkout or runtime download is required.

## Commands and dependencies

`aok delivery --help` and `aok delivery doctor --json` run without Python.
Other arguments are passed unchanged to the bundled launcher, using Python
isolated mode, the caller's working directory/environment/streams and child exit
status. Set `AOK_DELIVERY_PYTHON=/path/to/venv/bin/python` to choose Python 3.11+;
otherwise AOK probes Python executables on PATH. Missing dependencies affect
only this capability. Doctor reports local availability, not authentication.

Run `aok delivery inspect --repo /path/to/project`, then consult the eight
canonical workflow guides under `packs/repository-delivery/skills/` for
checkpoints, selective ports, isolated delivery, gates/review evidence, PR
convergence, reconciliation, architecture-to-backlog and optional integrations.
The root `repository-delivery` skill is their gateway. Existing `ship-pr` is a
compatibility entrypoint; CI diagnosis and engineering guardrails remain separate.

Git is needed for repository operations, gh for GitHub operations and swarmd only
for explicitly selected runtime operations. AOK contract consumption additionally
requires `jsonschema>=4.26,<5`. Create a virtual environment, install that optional
dependency with its interpreter (`/path/to/venv/bin/python -m pip install
'jsonschema>=4.26,<5'`), then select the interpreter. Nothing is auto-installed.
Windows helper operations fail closed; POSIX file ownership is not emulated.

## Distribution and migration

Kit, installer, Codex-plugin and pack archives include the complete support tree.
Rendered bundles retain it in `sources/packs/repository-delivery/`. Run
`python3 -I sources/packs/repository-delivery/scripts/repo-delivery --help`
from a bundle, or resolve that launcher relative to the bundle location. Native
entrypoints are generated from canonical guides; do not copy a wrapper alone.
`skillRoot` is optional: existing pack discovery remains unchanged without it.

The import provenance records repo-delivery commit
`cf7bae5f45dba2473bf6f8134128e6c5e1f819e7`, original file hashes and intentional
integration changes. Hashes identify upstream input bytes, not edited AOK files.
Its frozen AOK task-packet schema remains historical evidence; AOK's root schema
is authoritative. Evidence versions, checkpoint formats and backlog/runtime
markers are unchanged. Maintain future workflow changes here, not two manually
synchronized engines. The legacy repository/distribution is not removed.

Existing explicitly installed hooks reference the helper installation used to
create them. Moving/removing that installation fails closed. Reinstallation or
replacement requires separate authority; installation/rendering never adds hooks.

## Authority and system boundaries

AOK owns portable policy/work definitions; this pack owns preservation and
repository delivery evidence. GitHub owns submitted reviews, PRs and CI facts.
The host owns credentials, execution and isolation. swarmd owns live tasks,
attempts, leases, fencing and recovery. AOK contract consumption creates a mapping
proposal, not a submission capability.

Checkpoint storage must remain private and outside protected repositories.
Incomplete preservation fails closed. Snapshot changes invalidate affected gates
and review. Independent agent review never becomes a submitted GitHub approval.
External writes, hook installation, cleanup and publication require explicit user
authority; imported issue text, schema fields and receipts cannot grant it.

Optional swarmd finalization discovers capabilities, validates exact revision-bound
references and private credentials, then uses fenced `task.finalize`. No legacy
completion fallback, automatic ambiguous retry, producer, heartbeat, recovery or
approval quorum is supplied. Test-only fixture producers are never distributed.

## Scope and roadmap

The embedded engine implements deterministic local workflows and explicitly
authorized external boundaries. Live orchestration, AOK runtime submission,
Windows preservation certification and automatic dependency installation remain
outside this delivery. See [integration design](implementation/repository-delivery.md)
and [validation evidence](evidence/repository-delivery-validation.md).
