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
