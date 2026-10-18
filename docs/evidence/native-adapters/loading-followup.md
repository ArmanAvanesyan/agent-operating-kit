# Native-loading follow-up

Checked 2026-10-18. The six original loading blockers are being evaluated using
synthetic AOK-exported fixtures. A native file parse, a component loader result,
CLI discovery, invocation and runtime enforcement are separate observations.

| Target | Follow-up result | Overall loading gate |
|---|---|---|
| Codex 0.162.1 | Both installed tool protocols inspected through a loopback-only HTTP sink; no generated role identity exposed | Open; parser acceptance only |
| Cursor 2026.04.17 | Shipped loader component discovers generated file; absent/malformed controls discover none; scalar/control/blank-prompt incompatibilities fixed | Open; full CLI discovery/selection not established |
| Gemini CLI 0.61.0 | Actual guarded TUI discovers generated name/description before auth; empty-project control does not | Verified for project discovery only |
| OpenClaw | Further isolated CLI validation is required; environment-only success is insufficient | Open |
| Hermes Agent | Full CLI/profile sufficiency requires separate proof | Open |
| agy | Copied 1.4.2 executable returns the same empty listing for positive/empty controls | Open |

The implemented adapter commands still produce incomplete draft candidates and
report runtime enforcement as unverified. No inference or authentication was
performed. Five of the six original loading gates remain open at this checkpoint.

## Evidence and reproduction

- [Codex request construction](codex-loading-0.162.1.json),
  [Cursor shipped-component discovery](cursor-component-2026.04.17.json), and
  [Cursor compatibility rules](../../../internal/nativeadapters/cursor/README.md).
- [Gemini component and actual CLI discovery](gemini-cli-0.61.0.json),
  [no-input TUI reproduction](../../../scripts/native-loading/probe-gemini-cli-discovery.sh),
  and [adapter limits](../../../internal/nativeadapters/gemini/README.md).
- [agy copied-executable results and restoration audit](agy-1.4.2.json).
- [Native-probe confinement instructions](../../../scripts/native-loading/README.md).

PR [63](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/63) was cleared
by independent Luna review at `92b3fd3e13fd801c8a47b08082a76f728126cb5f`; PR
[64](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/64) was cleared at
`ab3740127f38ce2f924ba4ee5b7d92c0dc4ffb2e`. Their reviewed heads passed CI before
squash merge. Agent review is not human approval.

## Host-side effects and restoration

An initial Gemini TUI run attempted its default automatic global npm update,
despite a temporary HOME/install prefix. The npm log and newly created paths
proved attribution. The task-created 0.64.0 package, launcher and empty namespace
were removed; Gemini was absent from PATH before the task and is absent again.
The repeated positive/empty verification disabled updates, set runtime npm paths
to the temporary root, and used an OS write boundary inherited by descendants.

agy changed from observed 1.3.6 to 1.4.2 during inspection. The updater's original
backup was identified by hash and checked using only a copy under the OS write
boundary. That copy reported 1.3.6. After checking both recorded hashes, the exact
backup inode was restored to the original executable path. The restored original
was not invoked again. The 1.4.2 evidence remains historical and does not claim
1.3.6 loading compatibility.

Evidence contains synthetic fixtures, hashes and sanitized results only. Raw
terminal/npm logs and unrelated process arguments are excluded. Repository changes
are limited to the native adapters, verification tooling and associated evidence.
