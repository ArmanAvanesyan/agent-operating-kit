# Native-loading follow-up

Checked 2026-10-18. The six original loading blockers are being evaluated using
synthetic AOK-exported fixtures. A native file parse, a component loader result,
CLI discovery, invocation and runtime enforcement are separate observations.

| Target | Follow-up result | Overall loading gate |
|---|---|---|
| Codex 0.162.1 / temporary 0.163.0 | Trusted positive/empty config/read controls and generated app-server schemas expose no custom-role discovery/selection interface; earlier loopback request audit also exposed no role identity | Open; no-turn loading proof unavailable |
| Cursor 2026.04.17 | Shipped loader component discovers generated file; absent/malformed controls discover none; scalar/control/blank-prompt incompatibilities fixed; new copied ACP startup fails under keychain-denying privacy boundary | Open; full CLI discovery/selection not established |
| Gemini CLI 0.61.0 | Actual guarded TUI discovers generated name/description before auth; empty-project control does not | Verified for project discovery only |
| OpenClaw 2026.9.4 | Actual Linux CLI roster loads id/model; absent/malformed controls and exact workspace/missing-file loader controls pass | Verified for Linux isolated-home loading only; Darwin CLI unverified |
| Hermes Agent | Installed source components load model, metadata and SOUL; malformed config falls back with a recorded parse failure | Open; full CLI/profile sufficiency and source-to-CLI equivalence unverified |
| agy | Copied 1.4.2 executable returns the same empty listing for positive/empty controls | Open |

The implemented adapter commands still produce incomplete draft candidates and
report runtime enforcement as unverified. No inference or authentication was
performed. Four of the six original loading gates remain open at this checkpoint.

## Evidence and reproduction

- [Codex request construction](codex-loading-0.162.1.json),
  [Cursor shipped-component discovery](cursor-component-2026.04.17.json), and
  [Cursor compatibility rules](../../../internal/nativeadapters/cursor/README.md).
- [Gemini component and actual CLI discovery](gemini-cli-0.61.0.json),
  [no-input TUI reproduction](../../../scripts/native-loading/probe-gemini-cli-discovery.sh),
  and [adapter limits](../../../internal/nativeadapters/gemini/README.md).
- [OpenClaw Linux CLI/container evidence](openclaw-container-2026.9.4.json),
  [Darwin component result and shared-lock audit](openclaw-2026.9.4.json),
  and [Hermes component/bootstrap audit](hermes-profile-loader.json).
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

PR [65](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/65) was cleared
by independent Luna review at `24983d3bcf65c9bab1063a36bea83f4e500248bb` and
passed all eight CI checks before squash merge.

The earlier Hermes launcher attempt entered dependency preparation and was
stopped. Source/install/cache auditing found one recent ignored bytecode file
with no reliable pre-attempt baseline; it was left untouched and its hash is
recorded. Earlier OpenClaw CLI attempts used the shared host lock namespace;
individual lock-file ownership could not be established, so shared files were
not removed or changed during cleanup. Final OpenClaw CLI verification uses
private container `/tmp`, read-only packages/root and no network or host mounts.
The task-owned update quarantines were removed after rechecking the restored
agy hash and absence of the task-created global Gemini launcher.

## No-turn interface recheck

The [Codex/Cursor audit](codex-cursor-discovery-2026.10.18.json) records the
copied CLI identities, fixture hashes, version-specific protocol observations,
configuration/JSON-RPC parameters and isolation boundaries. Codex 0.163.0 was
installed only in a temporary private npm prefix with package lifecycle scripts
disabled. The installed user CLI was not upgraded. Cursor's keychain-denied
startup failure is recorded separately from its documented authentication flow;
it is not evidence of a parser failure or a successful discovery attempt.
Both gates remain open. No credentials, model turns or remote network requests
were used in these host probes.
