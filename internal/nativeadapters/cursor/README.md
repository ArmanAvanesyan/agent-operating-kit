# Cursor project subagent adapter

This adapter handles one explicitly supplied Markdown subagent under
`.cursor/agents/`. It accepts `.md`, `.mdc`, and `.markdown`, the three
extensions included by the 2026.10.16 Cursor Agent workspace scanner, with
case-insensitive extension matching as in the shipped scanner. It does
not scan a project, run Cursor, or prove that Cursor
loads a given file. `HostLoading` is verified for bounded project command-picker discovery on the pinned 2026.10.16 CLI; the shared engine reports
runtime enforcement as unverified.

The mapping is intentionally limited:

- `name` maps to `agent.metadata.id`. Cursor documents it as optional and
  derives the effective identifier from the file name when absent; the adapter
  follows that rule for the projection.
- `description` maps to `agent.identity.purpose` when supplied.
- A non-empty string `model` maps to `executionProfile.model.selector`.
- The Markdown body is copied byte-for-byte to the `instructions.md` asset,
  referenced by `agent.instructions.sources`.
- Other frontmatter, including `readonly` and `is_background`, remains inert
  under `Unknown` as `frontmatter.<key>`. The input path is retained as
  `file.name` when it differs from `.cursor/agents/<name>.md`.

No native tool, permission, or execution behavior is converted to a canonical
capability reference. Required controls without an explicit supported mapping
remain unsupported. Export writes YAML frontmatter and the saved body, keeps a
retained source path, and rejects unsupported fields/assets or unsafe paths
instead of dropping them. Shared native parsing rejects duplicate YAML keys,
tags, anchors, aliases, merge keys, invalid dates, invalid UTF-8/BOM,
non-finite numbers, and inputs over 4 MiB or 64 levels deep.

## Source basis

Format and scope facts were checked against Cursor's official documentation
on 2026-10-16:

- [Subagents](https://cursor.com/docs/subagents)
- [Cursor TypeScript SDK: configuration sources and subagents](https://cursor.com/docs/sdk/typescript)

Cursor's docs list `name`, `description`, `model`, `readonly`, and
`is_background` as native frontmatter fields. Only the first three receive
projection treatment here; `readonly` is not treated as an AOK permission
enforcement guarantee. The local `adapterVersion` tracks this mapping and is
not the installed Cursor Agent version.

## Native loading blocker

The installed Cursor Agent help/version [evidence](../../../docs/evidence/native-adapters/cursor-loading-blocker.json)
does not expose a read-only subagent discovery operation. No prompt was sent.
AOK tests verify native-format parsing, supported-field round trips and unknown
preservation, but the native gate remains open until a host actually loads the
generated file. Native input uses an explicit inventory with `.cursor/agents/`
paths; a standalone basename does not establish that scope.

## Earlier component verification and scalar compatibility

The [2026.04.17 component probe](../../../docs/evidence/native-adapters/cursor-component-2026.04.17.json)
invokes the shipped `LocalSubagentsService` against an AOK-generated file.
Only the bundle startup entry is replaced in memory to expose its existing
module export; the loader module/factories remain unchanged. HOME/project are
temporary; Node TCP connect, HTTP(S) request and fetch are guarded. The positive fixture is discovered
with its name, description, model and prompt; absent and malformed controls return
no agents. This is component discovery, not full CLI startup, selection or
inference. `HostLoading` stays `not-run` for the overall CLI gate.

That installed loader split frontmatter lines at their first colon and did
not decode YAML quoting, block scalars or nesting. A quoted YAML description
is returned with literal quotation marks. AOK now rejects mapped name,
description or model scalars, plus recognized native `readonly`, `background`,
`is_background` and `tools` controls, when the strict YAML parser and installed host
would interpret them differently, including shadowing by nested/case-folded
keys. Empty or whitespace-only prompts are rejected because this loader silently
drops them. Export also rejects values requiring incompatible YAML quoting rather
than silently changing their host meaning. This is a conservative subset pinned
to the inspected CLI version; it does not assert that every Cursor version uses
this parser. Other unknown frontmatter is retained without execution guarantees.
The host trims the prompt body for its own descriptor; AOK retains original body
bytes in its sidecar.

Repeat with a built binary and an explicit installed host directory:

```sh
python3 scripts/native-loading/verify_cursor.py --aok /absolute/path/to/aok \
  --host-dir /absolute/path/to/cursor-agent/versions/2026.04.17-787b533
```

The mapping revision is `cursor-subagents-v3`; older sidecars require re-import
under the new compatibility checks rather than silently reusing old assumptions.

## Fresh Cursor Agent CLI audit (2026-10-18)

The [2026.10.16 CLI audit](../../../docs/evidence/native-adapters/cursor-loading-2026.10.16.json)
used the official x86_64 package in a private temporary directory; it did not
run the installer or modify the user's Cursor installation. Guarded `--help`
and `--version` show no local subagent-list operation. The package's workspace
scanner source includes `.md`, `.mdc`, and `.markdown` patterns under
`.cursor/agents/`. A no-input CLI startup against a synthetic AOK-exported
workspace reached `Press any key to log in...` and was terminated after eight
seconds with network and keychain access denied. It produced no role inventory.
The official ACP handler source similarly makes unauthenticated `session/new`
return an authentication-required error; `initialize` reports capabilities,
not discovered project roles. No authentication or prompt was attempted.

The new release therefore supplies a filename-extension compatibility update,
but no fresh full-CLI discovery evidence. Its project `HostLoading` status
remains `not-run`; the older component result is limited to the exact earlier
component version and does not prove current CLI discovery, role selection, or
runtime enforcement.

The bounded audit is reproducible with the pinned official package archive
and a locally built AOK binary (the script does not download or install Cursor):

```sh
python3 -B scripts/native-loading/probe_cursor_fresh_cli.py \
  --package /path/to/agent-cli-package.tar.gz \
  --aok /path/to/aok
```

The script verifies the pinned archive hash, copies it and the supplied AOK
binary under a private temporary root, safely extracts only regular files and
directories there, and checks the extracted CLI and bundle hashes. It records
the supplied AOK binary hash dynamically, creates and compares a fresh AOK
export, then runs only `--version`, `--help`, and an eight-second no-input
startup under network denial, real-user-home read denial, keychain lookup
denial, and temporary write confinement. It preflights process-group signaling
and records owned process IDs before communication; failure audits are retained
under `/private/tmp`. It prints sanitized JSON without temporary absolute paths.

The inspected workspace scanner compiles its file globs with `dot: true`, so
hidden nested paths are not excluded by the glob itself (repository ignore
rules can still filter a discovered path). The adapter requires a nonempty
filename stem for every supported extension.

## Isolated ACP startup audit (2026-10-18)

The [ACP audit](../../../docs/evidence/native-adapters/codex-cursor-discovery-2026.10.18.json)
inspected the installed version's documented ACP flow and copied CLI. The CLI
started with a disposable home/project, no credentials, denied network access,
and denied Mach service lookups to protect the user's keychain. Under that
privacy boundary it failed before ACP initialization with
`SecItemCopyMatching failed -50`. This is a startup failure under the chosen
boundary, not a successful agent-discovery result or proof of a native parser
failure. No authentication or prompt completed, and the boundary was retained.

The documented ACP flow includes authentication before session creation;
`session/prompt` would begin an agent turn. No unauthenticated custom-subagent
listing interface was identified. The earlier shipped-component result remains
valid but does not close the full CLI gate. Runtime enforcement remains unverified.

The 2026-10-18 isolated ACP probe sent only `initialize`, `initialized`, and
`session/new`, using a synthetic invalid API key with network and keychain
access denied. It timed out after 30 seconds, returned an initialize result
and one sanitized `session/new` internal error, and returned no discovery
response. This remains no loading evidence. The exact AOK-rendered draft and
probe record are retained in [testdata](testdata/probe-aok-rendered.md) and
[evidence](../../../docs/evidence/native-adapters/cursor-agy-offline-2026.10.18.json).


## Authenticated isolated CLI verification (2026-10-18)

Fresh browser OAuth was explicitly approved and completed in a private native
file store. Backend `status --format json` returned authenticated user details
in all three controls; reports retain only that boolean, never personal fields.
The temporary OAuth store was removed after verification. Subsequent probes
used private copies of the existing Cursor CLI credentials.

The [authenticated terminal evidence](../../../docs/evidence/native-adapters/cursor-authenticated-terminal-2026-10-18.json)
uses the pinned standalone executable and a real controlling terminal. The
[extended run](../../../docs/evidence/native-adapters/cursor-authenticated-extended-2026-10-18.json)
waited 60 seconds per control. Both captured no UI output. The
[official-launcher run](../../../docs/evidence/native-adapters/cursor-authenticated-wrapper-2026-10-18.json)
uses the package's `cursor-agent` wrapper and bundled Node entrypoint; it
captured 83 bytes per control without reaching a recognized prompt in 45 seconds.
The [terminal-protocol follow-up](../../../docs/evidence/native-adapters/cursor-authenticated-terminal-protocol-2026-10-18.json)
answered only recognized standard terminal queries; none were observed. It also
reached no prompt. These bounded observations establish neither discovery nor
a native parser failure.
The earlier zero-byte run without a controlling terminal is retained as a
superseded harness attempt, not loading evidence.

Each run uses a fresh home/project for an AOK-exported role, an absent role,
and the same frontmatter with a blank prompt body. Native processes cannot
read the real user home or access Keychain services. Writes are confined to
the owned temporary root, `/dev/null`, `/dev/tty`, and the exact allocated PTY
slave. Network is allowed for authentication and initialization. Harmless
terminal fixtures confirm controlling-terminal ownership and capture under the
guard. Closing the master before group cleanup avoids terminal teardown hangs.
No ordinary prompt, Enter submission, agent selection, or model turn was sent.

Reproduce with the pinned official archive and a built AOK binary:

```sh
python3 -B scripts/native-loading/probe_cursor_authenticated.py \
  --package /path/to/agent-cli-package.tar.gz --aok /path/to/aok \
  --use-existing-cli-login --menu-timeout 30 \
  --evidence-out docs/evidence/native-adapters/cursor-new-observation.json
```

The helper copies only the named Cursor CLI access/refresh Keychain items into
private temporary native file stores, rejects expired credentials, checks
backend authentication, and removes copies in `finally`. It does not refresh or
alter the original store. For a separately authorized fresh browser OAuth flow,
`prepare_cursor_login.py` creates a private, short-lived session; pass its root
with `--isolated-login-root` instead. Remove that owned root after use. Evidence
keeps exact executed sources, hashes and process identities. `HostLoading`
remains `not-run` and runtime enforcement remains unverified.


## Verified project command-picker discovery — 2026-10-18

The [reviewed gate decision](../../../docs/evidence/native-adapters/cursor-project-discovery-decision-2026-10-18.json)
binds the [native TUI observation](../../../docs/evidence/native-adapters/cursor-command-picker-2026-10-18.json)
and its executed source. The full adapter-exported description appears in the
positive project's current command-picker screen and is absent from both the
missing-file and same-frontmatter/blank-body controls. Every case authenticated
against the backend, searched `/reviewer`, and sent no Enter. The echoed name is
not discovery evidence; the older literal menu-marker string did not match.

This supersedes earlier `not-run` discovery conclusions for the exact pinned
2026.10.16-e3cb207 package and project scope. It establishes discovery/registration,
not selected-agent startup, prompt-body use, model selection, inference or runtime
policy enforcement. Networking was allowed; an immutable PATH shim refused named
Git retrieval verbs. This is a qualified isolated loading result, not a global
network restriction or a default-environment startup claim.
