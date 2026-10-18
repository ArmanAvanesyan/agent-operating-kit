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

## Supported scopes and host evidence

Each adapter declares a scope and a separate host-loading state. Host evidence
is narrower than field parsing and remains separate from runtime enforcement.

| Target | Scope | Host-loading evidence | Boundary |
|---|---|---|---|
| `opencode` | `project` | Verified discovery only for OpenCode 1.18.35 | Isolated project file discovery only; no model or tool execution, and no permission-enforcement claim. |
| `claude-code` | `project`, `plugin` | Verified project discovery only for Claude Code 2.1.281 | Evidence applies to project scope only; plugin parsing is not a discovery check. No model execution or permission-enforcement claim. |
| `codex` | `project` | `not-run` | Parser and round-trip checks only. |
| `cursor` | `project` | `not-run` | Parser and round-trip checks only. |
| `agy` | `project` | `not-run` | Parser and round-trip checks only. |
| `gemini` | `project` | Verified project discovery for temporary Gemini CLI 0.61.0 | Guarded positive/empty TUI checks before authentication; no invocation or runtime-enforcement claim. |
| `openclaw` | `isolated-home` | `unavailable` | Host unavailable for loading verification. |
| `hermes-agent` | `profile` | `unavailable` | No safe native loader probe established. |

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
