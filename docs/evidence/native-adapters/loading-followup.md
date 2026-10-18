# Native-loading follow-up

Checked 2026-10-18. The six original loading blockers are being evaluated using
synthetic AOK-exported fixtures. A native file parse, a component loader result,
CLI discovery, invocation and runtime enforcement are separate observations.

| Target | Follow-up result | Overall loading gate |
|---|---|---|
| Codex 0.162.1 | New explicit project registration loads the AOK-exported role; absent registration, malformed config, malformed referenced role and missing-file controls distinguish actual loading | Verified for `project-registered` loading only; standalone discovery and child-agent invocation unverified |
| Cursor 2026.04.17 / temporary 2026.10.16 | Older shipped-component discovery remains version-bound; fresh official CLI reaches login before any role inventory. Source-confirmed filename extensions are now preserved by the adapter | Open; full CLI discovery/selection not established |
| Gemini CLI 0.61.0 | Actual guarded TUI discovers generated name/description before auth; empty-project control does not | Verified for project discovery only |
| OpenClaw 2026.9.4 | Actual Linux CLI roster loads id/model; absent/malformed controls and exact workspace/missing-file loader controls pass | Verified for Linux isolated-home loading only; Darwin CLI unverified |
| Hermes Agent, direct installed entrypoint | Actual installed CLI lists exported profile/model; separate installed metadata/SOUL loaders pass expected-content and missing controls | Verified for the explicitly selected installed profile entrypoint; normal launcher selection unverified |
| agy installed 1.3.6 / temporary official 1.4.2 | Fresh help confirms listing aliases; isolated Git-root valid/absent/malformed listings remain identical empty output | Open; no native agent inventory established |

The implemented adapter commands still produce incomplete draft candidates and
report runtime enforcement as unverified. No inference or authentication was
performed. Two of the six original loading gates remain open at this checkpoint.

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
Both gates remained open at that checkpoint. The registered-scope follow-up below
supersedes the Codex loading conclusion. No credentials, model turns or remote network requests
were used in these host probes.

## Installed Hermes entrypoint and agy recheck

The [installed Hermes CLI evidence](hermes-cli-profile-list.json) and bounded
[`probe-hermes-installed.py`](../../../scripts/native-loading/probe-hermes-installed.py)
cover actual AOK import/export, CLI profile listing, empty/malformed controls,
and separate installed metadata and SOUL loaders. The reported CLI version is
`vunknown (2026.9.24)`; exact source hashes and Python 3.14.7 identify the tested
entrypoint. The probe uses the existing environment directly and does not run
the package-manager launcher. Launcher selection, complete profile readiness,
model execution and runtime policy enforcement remain unverified.

The [copied agy 1.3.6 evidence](agy-1.3.6-loading.json) records empty alias results
and the interactive manager's login barrier. Only loopback bind/inbound traffic
was permitted; a canary verified remote connections were denied. No login URL,
state, challenge or raw terminal transcript is saved. The original executable
was never launched or replaced during this recheck.

PR [67](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/67) was cleared
by independent automated Luna review at `84ff7ff227718d405479e15cd248ed8295113d27`
and passed all eight CI checks before squash merge. Agent review is not human
approval.


## Codex explicit project registration

The [registered-scope evidence](codex-project-registered-0.162.1.json) uses a
freshly built AOK binary to import/export a two-file `project-registered`
inventory. The [bounded reproducer](../../../scripts/native-loading/verify_codex_project_registered.py)
runs copied Codex CLI 0.162.1 under a network-denying, temporary-write-only OS
sandbox with a synthetic trusted project and isolated `CODEX_HOME`.

The positive `config/read` resolves `[agents.aok-probe].config_file`, and an
ephemeral read-only `thread/start` completes without configuration warnings.
A standalone role without registration produces no registered agent. Malformed
project config fails the config read; malformed referenced role and missing-file
controls emit native role-loading warnings. These controls establish that the
client reads the referenced file, beyond echoing the registration table.

This closes the target's native-loading gate for `project-registered` only.
Standalone discovery, child-agent invocation, instruction use, model
compatibility and runtime enforcement remain unverified. No turn, authentication
or inference request is sent. Import/export results remain blocked incomplete
drafts, and unknown native settings are preserved without execution or a claim
that their wider dependencies are satisfied.

During review fixes, two additional copied-host `--version` checks reached their
10-second timeout while an extra security-service Mach denial was present; the
owned process groups were terminated. The final successful probe uses explicit
file-based credential storage in an empty isolated `CODEX_HOME`, no inherited
credential variables, denied network access and temporary-root write confinement.
The cause of the earlier startup timeouts was not established.


## Cursor and agy offline controls

The [offline probe record](cursor-agy-offline-2026.10.18.json) and
[bounded reproducer](../../../scripts/native-loading/probe-cursor-agy-offline.py)
retain actual AOK-imported/exported draft bytes for both clients, hashes,
pinned installed identities, controls and sanitized protocol outcomes.
Neither loading gate is promoted. The source's pinned hashes identify the
versions; a guarded Cursor version-only check timed out, so the final reproducer
does not invoke `--version`. It rejects a different source hash.

Cursor ACP receives only initialize, initialized and session/new requests with
a synthetic invalid API key. Initialize returns a result; session creation
returns an internal error and the process reaches its 30-second timeout. The
555 captured stdout bytes contain one error record. The cause remains unresolved;
this does not prove a parser failure or native agent discovery. No authenticate
or session/prompt request is sent. Remote networking, outside-root writes and
keychain service lookup are denied.

agy's copied 1.3.6 binary returns exit 0 with empty output for positive, absent
and malformed files. Empty output is not discovery evidence. The retained
[native draft](../../../internal/nativeadapters/agy/testdata/probe-aok-rendered.md)
closes the prior fixture-retention gap. Remote networking and outside-root
writes are denied, with only localhost bind/inbound allowed for the listing
probe. No login or model invocation is performed. The official
[Cursor ACP flow](https://cursor.com/docs/cli/acp) and
[agy headless documentation](https://www.antigravity.google/docs/cli/headless/)
inform the probe: documented headless print sends a prompt, so it is excluded.
No documented local-only headless slash-command path was identified.

One Cursor help-only command ran before the intended inner guard. The separate
manual audit records its zero shell exit, the unchanged original bundle hash,
wrapper/Node hashes and no compile-cache files changed during the audit window.
No authentication or model prompt was sent. The reproducer does not emit this
historical audit as a fresh observation. Subsequent probes use copied hosts and
the recorded guards.

Review also corrected timeout accounting: Python communicate returns cumulative
output after a timeout; appending the earlier prefix duplicated a response.
Two CI-discovered mocked-process tests now exercise TERM and KILL capture paths
through the real confinement helper. The saved final evidence counts the
response once; raw protocol messages, state and unrelated process arguments
are excluded.

PR [69](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/69) was cleared
by independent gpt-6-luna review at
`8ccec2127d0fb19042e7e83fd0d7f6fd7be83a45` and passed all eight CI checks before
squash merge. Agent review is not human approval.


## Fresh Cursor package and filename compatibility

The [fresh Cursor CLI audit](cursor-loading-2026.10.16.json) uses the official
2026.10.16-e3cb207 darwin-x64 package, obtained from the URL in the official
[installer source](https://cursor.com/install). The installer was inspected as
data, not executed. No user installation was upgraded or launched.

The new package's scanner recognizes `.md`, `.mdc`, and `.markdown` without
case sensitivity. AOK's `cursor-subagents-v3` mapping retains these file paths,
rejects empty filename stems, and requires reimporting older sidecars. Native
input still requires an explicit inventory with `format: markdown`; these
filename checks do not establish host loading or permission enforcement.

The copied CLI's help/version and no-input startup were inspected in isolation.
Startup stopped at the login banner before a role inventory appeared. Source
inspection of its ACP handler also identifies an authentication check before
session creation. No login, prompt or model turn was sent. Absent/malformed
host controls were not claimed to pass after this blocked positive attempt.
The [bounded reproducer](../../../scripts/native-loading/probe_cursor_fresh_cli.py)
and exact fixture/package/source hashes accompany the evidence.

The older April shipped-component result is not attributed to October's full
CLI. Both Cursor and agy loading gates remain open; no runtime-enforcement
claim follows from the extension compatibility fix.

PR [71](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/71) was cleared
by independent gpt-6-luna review at
`a292d1727679ad39e6b2db4badec23cc523c85e6` after cleanup and portable-test
fixes. All eight CI checks passed before squash merge, and the local 74-test
Python suite passed with two platform skips. Agent review is not human approval.

## Fresh agy package and evidence correction

The [fresh Darwin package audit](agy-1.4.2-fresh-loading-2026.10.18.json)
records the official installer manifest resolving to 1.4.2 while the download
page advertised 1.4.0. The installer was read as data; its checksum-verified
package ran only as a temporary copy. The user's installed 1.3.6 executable
was not launched or replaced.

Fresh 1.4.2 help explicitly describes `agent`/`agents` as listing aliases.
The earlier statement that 1.3.6 help established those aliases was unsupported
by the retained byte-count-only observation and is retracted. The earlier
commands' empty output remains historical evidence, not proof of discovery.
Fresh non-terminal positive, absent, and malformed controls used isolated Git
repositories and still produced identical zero-byte results. The official
[`/agents` documentation](https://www.antigravity.google/docs/cli/commands/agents)
describes a TUI manager; an empty listing subcommand does not establish that
the documented manager loaded a role.

Two Darwin terminal observations are excluded. The first encountered EPERM
during group termination without retaining its PID/PGID; the later attempt
did not establish whether its fallback cleanup was used. Targeted checks of
the known executable paths and unique temporary prefix identified no matching
process, but complete descendant cleanup remains unconfirmed because the
first process-group identity was lost. The manual audit is retained separately;
neither terminal attempt is native-loading evidence. The replacement Darwin
probe draft was withdrawn before another native run; it is not shipped as a
reproducer. Further verification uses an isolated container instead.

The [Linux container evidence](agy-linux-container-2026-10-18.json) records
official 1.4.2 under a local digest-pinned, COPY-only image, with read-only
root/package, an unprivileged user, no mounts or credentials, closed stdin,
resource limits and no network. Private loopback remains available inside the
container namespace without reaching host services. Each control started from
a real synthetic Git repository and a separate disposable home/project.

Version/help succeeded, but both non-terminal and terminal `agents` commands
returned no agent inventory in every control. Non-terminal output was zero
bytes; terminal capture contained four bytes of probe separator whitespace.
All three containers and the UUID-tagged derived image were removed with
ownership and absence checks. The cached public base image and Docker build
cache are not globally pruned.

This does not identify the upstream reason for the empty command or verify
the interactive `/agents` manager. The reproducer records candidate
observations only and cannot automatically promote the native-loading gate.
Its post-run qualification change is explicitly separated from the hash of
the script that generated these observations. Both loading gates remain open;
the overall native-only completion gate remains incomplete.
