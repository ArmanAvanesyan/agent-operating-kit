# Antigravity CLI (AGY) native agent adapter

This adapter handles one explicitly supplied, project-scoped Markdown agent
file. AGY's official docs describe both `.agents/agents/<name>.md` and
`.agents/agents/<name>/agent.md`; the adapter accepts either and emits the
nested `agent.md` form. It does not discover files or write a project. Global
customizations under `~/.gemini/config/agents/` are outside this adapter's
scope.

The native subset is intentionally small:

| AOK field | Native value | Handling |
|---|---|---|
| `agent.metadata.id` | YAML `name` | Required non-empty string, which must match the file's agent name. |
| `agent.identity.purpose` | YAML `description` | Required non-empty string. |
| `executionProfile.model.selector` | YAML `model` | Optional; supported values are `inherit`, `flash`, and `pro`. Omission stays omitted. |
| `agent.instructions.sources` | Markdown body | One materialized `instructions.md` asset; body bytes are preserved. |

Other frontmatter fields are preserved under `Unknown` and re-emitted without
inventing AOK equivalents. This includes `tools`, `mainAgent`, `subagent`,
`commandExecutionPolicy`, `mcpServers`, `skills`, and `plugins`. Official docs
list defaults including `tools: []`, `mainAgent: true`, `subagent: true`,
`model: inherit`, and `commandExecutionPolicy: sandbox`; omitted values remain
omitted in the projection. Required AOK tool, filesystem, network, or MCP
controls are unsupported and block through the shared native adapter engine.
Unknown frontmatter syntax must be representable by the bounded YAML parser;
YAML comments and formatting are not retained.

Sources checked:

- [Custom subagents](https://www.antigravity.google/docs/subagents/)
- [CLI `/agents` command](https://www.antigravity.google/docs/cli/commands/agents)
- [CLI subagent guide](https://antigravity.google/docs/cli-subagents)

`Descriptor().HostLoading.Status` remains `not-run`. A bounded probe observed
the installed executable change from 1.3.6 to 1.4.2 after non-isolated
help/list invocations. The exact pre-probe 1.3.6 backup was later version-checked
as a copy under a write guard and atomically restored to the original path; the
updated executable was quarantined. Neither original executable was invoked
during that restoration audit. The restored backup SHA-256 is recorded in
[`agy-1.4.2.json`](../../../docs/evidence/native-adapters/agy-1.4.2.json).

The historical 1.3.6 `agent`/`agents` no-TTY calls returned empty output; the
captured help text was not retained, so those runs do not prove that these were
recognized listing aliases. Official headless `-p` is a prompt path and was
not used. Native loading remains unverified. See
[`agy-1.3.6-loading.json`](../../../docs/evidence/native-adapters/agy-1.3.6-loading.json)
for the prior isolated results, [the retained offline probe record](../../../docs/evidence/native-adapters/cursor-agy-offline-2026.10.18.json)
for the positive/absent/malformed controls and exact rendered bytes, and
[`agy-1.4.2.json`](../../../docs/evidence/native-adapters/agy-1.4.2.json) for
the historical version/restoration audit.

The official download page advertises CLI 1.4.0, while its official install
script resolves the darwin-x64 manifest to 1.4.2. The manifest-pinned archive
was checksum-verified in task-owned temporary storage; the binary was extracted
without running the installer or touching the restored 1.3.6 executable. In
1.4.2, `agent --help` and `agents --help` print `Usage: antigravity agent
[flags]` and `List available agents`. Direct no-TTY calls returned empty
output for positive, absent, and malformed workspaces. A PTY attempt failed
process-group signaling with EPERM; a later retry also occurred but its
cleanup path was not recorded. Both PTY observations are excluded. A targeted
audit later found no process matching the known executable paths or temporary
prefix, but full descendant cleanup remains unconfirmed because the first
attempt's PID/PGID was lost. No authentication or model invocation was
performed. See [the fresh 1.4.2 evidence](../../../docs/evidence/native-adapters/agy-1.4.2-fresh-loading-2026.10.18.json).

The earlier Darwin host reproducer draft was withdrawn after process-group
cleanup and audit gaps were found. It was not rerun. The retained historical
observations above remain separate and do not inherit later guard settings.

A fresh official Linux x64 1.4.2 package was checksum-verified as data and
tested in a disposable Docker Desktop container with a read-only root, no
network, no host mounts, closed stdin, and task-owned tmpfs. AOK produced the
expected blocked/incomplete import and export draft. The native CLI emitted no
agent identity or description for positive, absent, or malformed project
controls. Container and derived-image cleanup were confirmed. The probe status
remains `not-verified`; parsed output never auto-promotes this gate. See the
[Linux container evidence](../../../docs/evidence/native-adapters/agy-linux-container-2026-10-18.json)
and [bounded reproducer](../../../scripts/native-loading/probe-agy-linux-container.py).
Runtime enforcement remains unverified.

The adapter tests validate parsing, preservation, and semantic round-tripping;
they do not establish host discovery, selection, invocation, or enforcement.
No model or imported/custom command was run. Historical probe results retain
the isolation limits recorded with each observation; the strengthened guard
in the current reproducer does not apply retroactively.

The interactive manager check stopped at AGY's authentication barrier in all
three isolated no-network cases, so it did not send `/agents`. Official
authentication uses a local OS keyring profile or browser OAuth; documented
headless alternatives require user-provided ADC or a Gemini API key. No
portable credential file is documented. Completing the isolated check needs
the user to authenticate that test profile. No existing keychain value was
read or copied. See the [interactive manager evidence](../../../docs/evidence/native-adapters/agy-interactive-manager-2026-10-18.json)
and [official authentication guide](https://www.antigravity.google/docs/install/).


## Interactive manager follow-up (2026-10-18)

The no-network Linux TTY run above is separate from the
[Darwin existing-keyring attempt](../../../docs/evidence/native-adapters/agy-macos-manager-diagnostic-2026-10-18.json).
The latter starts the pinned copied 1.4.2 CLI in private homes/projects, permits
only the securityd Mach lookup needed for native Keychain access, allows network
for native initialization, and blocks browser launch and original-home reads.
The parent never reads or copies AGY credentials. Native Keychain access is
permitted, so the filesystem guard does not establish read-only Keychain use.

All three Darwin controls stopped with a 107-byte generic startup error before
showing a local prompt. Backend authentication was not verified, and `/agents`
was not sent. Fixed error categories are retained; raw output is discarded to
avoid retaining account data. The exact cause has not been established, so
this is neither evidence of a role parser failure nor proof that a fresh login
alone will fix it. Both native loading and runtime enforcement remain unverified.

The reproducer now assigns a controlling terminal, closes the parent slave
immediately, and closes the master before group cleanup. It saves sanitized
state before cleanup, checks only recorded group PIDs/UIDs, and confirms group
absence. The earlier failed Darwin attempt is explicitly excluded in the
[attempt ledger](../../../docs/evidence/native-adapters/agy-interactive-attempt-ledger-2026-10-18.json).
All final controls passed cleanup. Exact executed sources and startup process
identities accompany the evidence; historical snapshots are not the active helper.

```sh
python3 -B scripts/native-loading/probe-agy-macos-interactive-manager.py \
  --aok /path/to/aok --binary /path/to/pinned/antigravity \
  --evidence-out docs/evidence/native-adapters/agy-new-observation.json \
  --audit-out docs/evidence/native-adapters/agy-new-process-audit.jsonl
```

The Linux helper is `scripts/native-loading/probe-agy-interactive-manager.py`.
Its true interactive startup stopped at login in all controls, with no `/agents`
input. It uses a pinned cached base image, no network or host credential mounts,
a read-only container root and disposable writable state. Neither helper invokes
an agent or submits an ordinary prompt; only a recognized local ready prompt
permits the documented manager command. Reports never auto-promote the loading gate.

## Resolved launcher and isolated OAuth follow-up (2026-10-18)

The [startup investigation](../../../docs/native-startup-diagnostics.md)
supersedes the earlier unexplained Darwin startup error: the Apple Python
launcher shim failed before native execution. Resolving the running Python
interpreter lets all three controls reach native authentication.

The approved private-profile OAuth follow-up submitted a fresh code once and
advanced to welcome/onboarding labels with a generic error. It did not establish
a ready prompt, backend authentication, or `/agents` discovery. The private HOME
was removed. The error's cause remains unresolved, so the native-loading gate
stays open. Current helper fixes and tests do not retroactively establish native
loading or runtime enforcement.
