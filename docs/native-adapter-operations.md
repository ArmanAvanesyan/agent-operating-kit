# Native adapter operations

The Go `aok` binary has an explicit native-agent command group for inspecting,
importing, exporting, and comparing bounded native agent files. These commands
operate on supplied files and return a reviewable projection. They do not
launch an agent or make a native configuration effective.

## Go CLI entrypoints

Build the binary from a checkout with Go installed:

```sh
go build -o ./bin/aok ./cmd/aok
```

Run native commands through that binary:

```sh
./bin/aok agent native --help
```

The built Go binary has no Python runtime requirement. `./scripts/aok` is a
separate contributor entrypoint for other kit operations; it is not required to
run `aok agent native` from the compiled binary.

The broader Go command groups include `aok agent validate`, `aok agent resolve`,
`aok planning validate|ready`, and `aok decision validate-result`. The native
group is:

```text
aok agent native inspect|import|export|compare \
  --target <target> --scope <scope> --input <file-or-inventory-directory>
```

Use `--json` for machine-readable report output. Native operations already
print a JSON report to standard output. `--require <canonical-field>` may be
repeated to require specific controls; missing or unsupported required controls
block the operation. Import and export require `--out <new-directory>`.

The integrated native command group registers all eight targets listed below.
Native adapter registration is separate from pack rendering: the pack renderer
continues to support its seven declared targets (`codex`, `claude-code`,
`cursor`, `opencode`, `gemini`, `openclaw`, and `hermes-agent`). `agy` is
available as a native-agent adapter target but is not a pack-render target.

## Explicit inputs

A single-file input captures only that file. A directory input must contain an
`aok-native-input.json` inventory listing every file to capture. The manifest
is AOK metadata, not a native host file:

```json
{
  "version": "aok-native-input/v1",
  "files": [
    {"path": ".opencode/agents/reviewer.md", "format": "markdown"}
  ]
}
```

Example project layout:

```text
native-input/
  aok-native-input.json
  .opencode/agents/reviewer.md
```

Directory inputs are constrained to 128 files and 4 MiB total. The reader
rejects symlinks, special files, path traversal, duplicate paths, and manifest
self-inclusion. It does not discover ambient native configuration, read
credential stores, or follow references to files outside the inventory.

## Inspect, import, export, and compare

Inspect parses an explicit input without creating a sidecar or changing files:

```sh
./bin/aok agent native inspect \
  --target opencode --scope project --input ./native-input --json
```

Import stores an incomplete projection and source-bound sidecar in a new
directory. Supply `--sidecar` when importing again against an existing bundle;
the current native file set must still match the bundle's originals.

```sh
./bin/aok agent native import \
  --target opencode --scope project \
  --input ./native-input --out ./native-draft
```

The output contains `aok-native.json` and `report.json`. The bundle retains the
original input bytes, hashes, adapter/target/scope identity, and an incomplete
mapping projection. It is not an executable or canonical agent definition.

Export a candidate from a sidecar only when the current source inventory is
available. `--source` supplies the explicit inventory for drift checking:

```sh
./bin/aok agent native export \
  --target opencode --scope project \
  --input ./native-draft/aok-native.json \
  --source ./native-input \
  --out ./native-candidate
```

The new output directory contains the candidate native files, an input
inventory for those rendered files, an updated `aok-native.json`, and
`report.json`. Review these files before applying them in a host environment.
Compare a candidate to its rendered sidecar with:

```sh
./bin/aok agent native compare \
  --target opencode --scope project \
  --input ./native-candidate \
  --against ./native-candidate/aok-native.json
```

Comparison uses decoded projection semantics and source hashes, not serialized
formatting. It detects source drift and sidecar tampering. A successful compare
does not establish that the host loads the candidate or enforces its controls.

Canonical export validates an assignment and its pinned corpus references,
then emits a reviewable candidate with declarative provenance:

```sh
./bin/aok agent native export \
  --target opencode --scope project \
  --input ./corpus/assignments/reviewer.json \
  --root ./corpus \
  --out ./native-from-canonical
```

The selected execution profile must name the same adapter target. The bundle
records the resolved assignment, agent and profile references, snapshot digest,
and canonical source hashes. Literal instruction sources are materialized from
captured bytes in declaration order. Parameterized instruction templates are
blocked because expansion is not implemented. Canonical pins and hashes record
what was selected; they do not authorize the agent or prove runtime readiness.

`--root` selects canonical export and cannot be combined with `--source`.
Sidecar-based export requires `--source`; compare requires `--against`; import
may use `--sidecar`. Output destinations must be new, safe directories outside
all explicit input paths. Existing output directories are not overwritten.

### Codex registered project scope

Use `--target codex --scope project-registered` with an inventory containing
exactly `.codex/config.toml` and `.codex/agents/<name>.toml`. The selected
`[agents.<name>]` registration must match the role file's name and description
and use `config_file = "agents/<name>.toml"`, relative to `.codex/config.toml`.
Selected names must be lowercase ASCII slugs; non-ASCII sibling names/references
and sibling paths with parent segments, absolute/UNC roots, tildes, colon, NUL
or trailing-dot/space components are rejected.
Reserved `[agents]` setting names and collisions with preserved sibling names
or role-file references are rejected, including case-insensitive path aliases.
Import, export and compare use the same commands above with that target/scope.
Canonical export generates both files. The older `project` scope still handles
one standalone role file and has no verified loading claim.

Root settings, sibling registrations and unknown role fields are preserved as
inert projection data. AOK does not open sibling references or prove their
dependencies are available. Review the whole generated config before applying
it: preserved settings can affect the native host beyond the selected role.
Exports create new directories; they do not merge into an installed config.
Codex adapter version 0.2.0 requires reimporting older 0.1.0 sidecars.

## Supported scopes and host evidence

Each adapter declares a scope and a separate host-loading state. Host evidence
is narrower than field parsing and remains separate from runtime enforcement.

| Target | Scope | Host-loading evidence | Boundary |
|---|---|---|---|
| `opencode` | `project` | Verified discovery only for OpenCode 1.18.35 | Isolated project file discovery only; no model or tool execution, and no permission-enforcement claim. |
| `claude-code` | `project`, `plugin` | Verified project discovery only for Claude Code 2.1.281 | Evidence applies to project scope only; plugin parsing is not a discovery check. No model execution or permission-enforcement claim. |
| `codex` | `project`, `project-registered` | Verified registered project loading for Codex CLI 0.162.1 | Registration plus referenced role-file loading; standalone `project` remains `not-run`. No child-agent invocation or runtime-enforcement claim. |
| `cursor` | `project` | Verified native command-picker discovery for 2026.10.16-e3cb207 | Authenticated positive/absent/blank-body controls under an explicit Git retrieval shim; no selected-agent startup, inference or enforcement claim. |
| `agy` | `project` | Verified startup discovery for Darwin x64 1.4.2 under `agy-project-discovery/v2` | Two isolated same-workspace/HOME cycles show valid → malformed → exact repair behavior, with cold malformed and unchanged guard controls. Explicit native parse-error reporting, invocation and runtime enforcement remain unverified. |
| `gemini` | `project` | Verified project discovery for temporary Gemini CLI 0.61.0 | Guarded positive/empty TUI checks before authentication; no invocation or runtime-enforcement claim. |
| `openclaw` | `isolated-home` | Verified Linux loading for OpenClaw 2026.9.4 / Node 26.7.0 | Network-disabled container roster and workspace controls; Darwin CLI remains unverified. |
| `hermes-agent` | `profile` | Verified direct installed CLI entrypoint, source-hash bound | Native profile list reads exported model/metadata; separate SOUL loader proof; normal launcher selection and runtime enforcement unverified. |

“Verified” in this table means only that a bounded isolated discovery probe
found the expected native agent file for that host version and scope. It does
not mean that tools, permissions, network policy, or other execution controls
were enforced. `not-run` and `unavailable` are not successful host checks.

## Static results and runtime boundaries

The adapters parse and validate explicitly supplied native bytes, preserve
unmapped native fields where supported, materialize inert instruction assets,
bind source hashes, and render candidate files. Unsupported required controls
block compatibility. Imports and exports always remain `draft-incomplete` in this release, including
canonical export. No command promotes a candidate into an execution-ready agent.

The native adapter API and CLI do not invoke models, dispatch agents, optimize
plans, infer permissions, modify global configuration, or grant authorization.
They do not prove that native tools enforce AOK permissions or that a host will
accept a candidate. Runtime enforcement is always reported as `unverified`.
Host-loading evidence is not a substitute for execution evidence.

Exit codes are:

- `0`: operation report status is `valid`.
- `1`: operation is blocked or invalid, or a runtime command error occurred.
- `2`: command usage is invalid or required arguments are missing.

A zero exit code means the requested static check passed. It does not imply
host loading, model inference, optimization, permission enforcement, or
authorization. Blocked imports/exports may still write candidate files and
reports for review.


## Cursor filename compatibility

Cursor project inventories accept case-insensitive `.md`, `.mdc`, and
`.markdown` filenames under `.cursor/agents/`, including nested paths.
Declare `format: markdown` in `aok-native-input.json`. Export preserves a
retained native path and extension; a canonical export still defaults to `.md`.
Empty filename stems are rejected. Standalone basenames do not establish a
Cursor project scope. The `cursor-subagents-v3` mapping requires reimporting
older sidecars; host loading and runtime enforcement remain unverified.


## Authenticated Cursor verification follow-up

The [Cursor adapter notes](../internal/nativeadapters/cursor/README.md#authenticated-isolated-cli-verification-2026-10-18)
record successful isolated backend authentication with the pinned 2026.10.16
CLI. Controlling-terminal and official-launcher attempts did not reach native
role discovery. This earlier authentication-only result is superseded by the
reviewed project discovery follow-up below; runtime enforcement is unverified.


## agy investigation history

The following paragraphs retain the earlier probe conclusions. The current
scoped status is established by the [v2 discovery gate review](agy-discovery-cycle-verification.md),
which closes project startup discovery without claiming a native parser error.

### Interactive manager follow-up

The [agy adapter notes](../internal/nativeadapters/agy/README.md#interactive-manager-follow-up-2026-10-18)
separate the isolated Linux authentication barrier from the Darwin generic
startup error under an existing-keyring-only configuration. Neither reached a
ready prompt or sent `/agents`. Source snapshots, control outcomes and process
audits are retained. Native loading and runtime enforcement remain unverified.

### Native startup diagnosis (historical)

The [isolated startup investigation](native-startup-diagnostics.md) corrects the
agy macOS launcher failure and records Cursor initialization diagnostics. The agy
native-loading gate remains open; Cursor project discovery is now verified.
The [onboarding follow-up](agy-onboarding-verification.md) fixes the code form,
preview false positive and terminal controls, records sharing-disabled consent,
and reaches the native `/agents` manager in synthetic-only isolated sessions.
The manager omits the custom fixture in every tested layout, so the project
loading gate remains open. Backend authentication, inference and runtime policy
enforcement are unverified.


The [account-mode follow-up](agy-account-verification.md) proves positive project
agent discovery in a fresh isolated OAuth profile. The valid agent name and
description appear; absent and malformed controls omit them. No explicit native
parse-error message was observed for malformed YAML, so the established full
three-control gate remains open and the descriptor stays `not-run`. This result
supersedes the earlier lack of positive discovery without claiming invocation,
backend authentication, or runtime enforcement. Temporary credentials were removed.


The [malformed-YAML investigation](agy-malformed-verification.md) adds explicit
`--parser-diagnostics` to the isolated account manager helper. It uses the
verified native `--log-file` option, reads only the bounded owned log after
process cleanup, and retains fixed labels/counts without log text. Account-mode
logs were readable, nonempty and untruncated, but supplied no recognized
frontmatter/YAML rejection. The agy gate remains open; no omission-only or
generic error evidence promotes the descriptor.
