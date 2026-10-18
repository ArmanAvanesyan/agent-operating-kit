# Isolated native-loading verification

These probes inspect synthetic AOK-exported native definitions. They establish
only the observations recorded in the evidence: parsing, a loader component,
or CLI discovery. They never establish runtime permission enforcement. A
component probe does not automatically close a full CLI loading gate.

Build AOK first with `go build -o /absolute/path/aok ./cmd/aok`. The Python
Codex and Cursor probes accept explicit binary/package paths and print sanitized
JSON; they do not install those hosts. They require Python 3 and Darwin's
`/usr/bin/sandbox-exec`. Run their `--help` for arguments. Other host probes name
their pinned package/runtime requirements explicitly; do not replace pins with
`latest` when reproducing a versioned result.

Every native runtime process must use a disposable, resolved HOME/project,
an allow-listed environment without credentials, and a filesystem write boundary
that permits only the owned temporary root and terminal/null devices. Prefer an isolated copy for
self-updating executables. The OS write boundary must protect the original
installation even when a shipped loader component is inspected in place. A temporary HOME
alone does not prevent a launcher from replacing its executable or running a
global package-manager update.

For npm-based hosts, disable the host's automatic update setting and point both
runtime npm prefix spellings, cache, user configuration and global configuration
at temporary paths. Use the official registry and a temporary install prefix;
disable package lifecycle scripts where the selected host supports that mode.
The OS write boundary also applies to descendants if an updater still runs.

`native_confinement.py` enforces the Darwin write boundary, selects a working
directory under the owned root, and limits subprocess duration. Timeouts terminate
the native process group. The guard does not block network traffic; probes must
separately avoid inference, credentials and imported commands. Detached children
are not a reason to relax the write boundary or wait indefinitely on inherited
pipes. Outside-root write-denial and child-timeout regressions run on Darwin;
other platforms skip those two OS-specific tests.

Run generated-fixture, absent/empty, and malformed controls as appropriate. Save
host/package versions, integrity/source hashes, rendered-file hashes, sanitized
results and any remaining blockers. Source/render hashes bind the fixture;
temporary paths, session identifiers and request hashes may vary between runs.
Do not save raw credential stores, process arguments from unrelated programs,
or unrestricted host logs. Inspect only owned process IDs and targeted metadata.

The Codex probe points its provider at a loopback HTTP sink that always returns
HTTP 400; no model implementation or inference output exists. The Cursor probe
changes only the bundled startup entry in memory to expose the shipped loader
module. It preserves module factories and reports component discovery separately
from full CLI loading. Its additional Node API guards cover TCP connect, HTTP(S)
request and fetch; they are not a claim of a complete network sandbox.

## OpenClaw container check

OpenClaw 2026.9.4 hardcodes its coordination locks under `/tmp` on non-Windows.
A strict Darwin temporary-root write boundary therefore blocks a successful CLI
roster query. Do not grant access to the shared host lock namespace.

With an already-running Docker engine, explicitly pull
`node:26.7.0-bookworm-slim` at digest
`sha256:4db36457f406501e6f608802e5da617e5fbd0e80b75901b6a09de1ae5a667d32`,
then run `python3 scripts/native-loading/verify_openclaw_container.py --aok
/absolute/path/to/aok`. The script requires that exact local image digest and
checks the official OpenClaw package integrity. Network access and official npm
lifecycle scripts are allowed only in the disposable install container; the
package must finish its lifecycle, since `--ignore-scripts` makes the CLI reject
an incomplete installation. No package code is patched and no lifecycle marker
is forged.

Runtime probes use an unprivileged user, no network, read-only root/package,
private tmpfs home and `/tmp`, resource limits and bounded Docker calls. Only
two synthetic fixture files enter over a tar stream; no host directories,
credentials or Docker socket are mounted. Full UUID names and ownership labels
limit teardown to this run's containers and volume. The script verifies the
native roster, absent/malformed controls and exact workspace bytes, prints JSON,
and removes its owned containers/volume. The explicit base-image cache remains.
This proves the recorded Linux scope/version, not Darwin loading or runtime
permission enforcement.

## Installed Hermes profile check

`probe-hermes-installed.py --environment /absolute/path/to/installed-environment
--aok /absolute/path/to/aok` selects an existing environment containing `venv/`
and `workspace/`. It invokes the installed Python CLI entrypoint directly,
without the package-manager launcher, and records that selection limit.
Workspaces with `.git` or `install-stamp.json` are refused to avoid unplanned
bootstrap work. The covered workspace has neither; no bootstrap marker or
installed source is changed to make the probe pass.

The probe imports/exports the committed native fixture through AOK, requires
blocked draft status, then checks native profile listing and empty/malformed
controls. Malformed config falls back to defaults; it is not reported as native
rejection. Metadata and SOUL loading use separate installed components. The SOUL loader
returns a body with surrounding whitespace stripped; equality and its loaded
hash cover that normalized body. A separate raw-file SHA-256 check verifies
that CLI startup leaves the exported SOUL file unchanged. All host processes use
an explicit environment, disposable home, process-group timeouts, denied
network access and outside-root write denial. No credentials or profile-provided
commands run. This verifies the explicitly selected entrypoint's profile scope;
normal launcher selection, complete profile readiness and runtime enforcement
remain unverified.

## Codex registered project check

`verify_codex_project_registered.py --aok /absolute/path/to/newly-built/aok
--codex-copy /absolute/path/to/copied/codex` requires Codex CLI 0.162.1 and
Darwin `sandbox-exec`. It copies the supplied host again, generates both native
files through AOK's `project-registered` import/export, and records their bytes
and hashes. Use a fresh binary built from the adapter version being tested.

The host receives only initialize, config/read and ephemeral read-only
thread/start requests. No turn is sent. Disposable trusted projects and isolated
`CODEX_HOME` directories distinguish the positive, absent-registration,
malformed-config, malformed-role and missing-role cases. Network access,
writes outside the temporary root are denied. Credential storage is explicitly
set to `file` under the isolated `CODEX_HOME`, with no credentials supplied.
Requests have bounded reads and owned process groups are terminated on exit.
This establishes referenced-role loading only; invocation, model compatibility
and runtime enforcement remain unverified.

## Cursor and agy offline recheck

### Fresh Cursor archive check

`probe_cursor_fresh_cli.py --aok /absolute/path/to/newly-built/aok --package
/absolute/path/to/agent-cli-package.tar.gz` checks the pinned official
Darwin x64 archive for Cursor Agent `2026.10.16-e3cb207`. Download that archive
separately using the URL in `cursor-loading-2026.10.16.json`; the probe neither
downloads nor installs it. The archive is verified and safely extracted into
the private runtime root. AOK is copied there too, with its actual hash recorded.

The probe denies networking, real-user-home reads and keychain service lookup.
A harmless process-group signaling preflight runs under the same guard before
any AOK or Cursor command. Each child PID/PGID is audited before communication;
failures retain that audit outside the disposable runtime. A no-input startup
is bounded and sends no keys or prompts. The observed login screen does not
verify agent discovery. Source inspection and adapter filename tests are
reported separately from native loading, which remains open.

`probe-cursor-agy-offline.py --aok /absolute/path/to/aok --cursor-host-dir
/absolute/path/to/cursor-agent/version --agy-binary /absolute/path/to/agy`
requires the exact installed source hashes recorded in the saved evidence.
It copies both hosts before invocation and ties the version labels to those
pins; it does not launch the original executables or run `--version`.

AOK imports/exports committed synthetic inputs. Rendered bytes are retained in
the corresponding adapter's `testdata/probe-aok-rendered.md`; an existing file
with different bytes is refused. Optional `--retain-cursor-render` and
`--retain-agy-render` destinations must remain inside the repository. The parent
script writes only these explicit research fixtures; native child processes
have write access only to the disposable root.

Cursor receives a synthetic invalid API key and initialize/initialized/session-new
requests, with keychain service lookup and remote networking denied. It sends
no authentication or prompt request. agy receives only help/list commands;
listing permits localhost bind/inbound while remote networking remains denied.
The guarded source is unchanged; no protocol response or authentication state
is forged. Timeouts terminate the owned process group and preserve cumulative
output once. The helper's TERM and KILL capture regressions run in standard
Python test discovery without launching a native host.

The generated report records current UTC time and this script's hash. The
historical help-only incident audit is separate manually recorded evidence,
not a fresh output of this reproducer. These controls did not establish native
loading for either client. Do not run headless print or authenticate to make
the no-inference probe pass.
