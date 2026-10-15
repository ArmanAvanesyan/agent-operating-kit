# Native adapter interface

The shared interface and conformance harness support explicit native projections.
OpenCode and Claude Code project Markdown are registered; see its [mapping and host evidence](../internal/nativeadapters/opencode/README.md).
Claude also supports plugin parsing/rendering, but its plugin loading gate remains
unverified. See [Claude mappings and evidence](../internal/nativeadapters/claude/README.md).
Codex project TOML is also registered; [its source mapping and parser evidence](../internal/nativeadapters/codex/README.md) do not yet establish child-agent loading.
Other known targets without a registered adapter report a blocked result.
Shared parser tests alone do not establish native completion.

The Go CLI accepts explicit operations, target, scope and input:

```text
aok agent native inspect --target opencode --scope project --input agent.md --json
aok agent native import --target opencode --scope project --input agent.md --out /new/draft
aok agent native export --target opencode --scope project --input /draft/aok-native.json --source agent.md --out /new/export
aok agent native compare --target opencode --scope project --input agent.md --against /draft/aok-native.json
```

Import accepts an optional explicit `--sidecar` to retain prior provenance.
Export requires the current source files so it can detect drift. Compare requires
an explicit reference bundle. Repeat `--require <canonical-field>` for controls
that must map; missing or unsupported required mappings block compatibility.
Exit codes remain 0 for valid checks, 1 for blocked/invalid results, and 2 for
command misuse. Import/export write reports and any safe draft artifacts only to
a new explicit output directory. They never modify installed configuration.

## Input and output boundaries

A single-file input reads that file alone. A directory requires
`aok-native-input.json` listing exactly the files to capture:

```json
{
  "version": "aok-native-input/v1",
  "files": [
    {"path": ".opencode/agents/researcher.md", "format": "markdown"}
  ]
}
```

This is an AOK capture inventory, not a native configuration format or a substitute
for native loading evidence. No credential/configuration-home discovery occurs.
Inputs are bounded to 128 files and 4 MiB total; symlinks, special files, traversal
and ambiguous duplicate names are rejected. The serialized bundle also has a
4 MiB limit, so large raw sources may exceed the bundle limit after provenance
encoding. Reports contain field paths and hashes, not source contents or values.

Output creates private files and directories, rejects case-insensitive and
file/directory collisions, portable reserved names, symlink ancestors, input
overlap and existing destinations. The output parent must already exist.
`report.json`, `aok-native.json`, and `aok-native-input.json` are reserved output
names. Exports include the input inventory so their directory can be explicitly
re-imported or compared.

## Projection, provenance and comparison

Adapters decode native files into a mapping projection using canonical AOK field
paths plus inert unknown native content. This projection is an **incomplete
draft**, not a new operating model or a schema-valid canonical Agent. It does not
invent missing assignment, output, environment, verifier or lifecycle references.
Canonical export validates those references using the existing resolver:

```text
aok agent native export --target opencode --scope project --input assignment.json --root /corpus --out /new/candidate
```

`--root` selects canonical export and is mutually exclusive with `--source`.
The target must match the selected execution profile. The candidate binds the
immutable snapshot digest, pinned assignment/agent/profile and all source hashes.
Literal instruction files are materialized from the same captured bytes, in
declared order, separated by two newline bytes. Parameterized templates block
export. Assets are inert bundle content. Native projections remain incomplete
and required unsupported controls remain blocked; hashes do not establish
readiness, authorization or runtime enforcement.

The versioned bundle binds the adapter version, target, scope, exact source bytes,
SHA-256 hashes and decoded projection. Raw bytes preserve source comments and
formatting as provenance; rendering may normalize formatting. Integrity and
source drift failures prevent export. Unknown fields survive supported-field
round trips, and collisions with mapped fields are errors. Common credential-bearing
decoded key names (including token, clientSecret, refreshToken, secretKey, credentials and provider-prefixed API-key/token environment names) are rejected; diagnostics do not echo their values. This is not a
general secret scanner for arbitrary prose.

Comparison uses decoded semantics rather than serialized formatting. Differences
name fields without printing values. A bundle digest detects drift; it is not a
signature or authorization proof. A missing sidecar cannot reconstruct source
lineage and produces an incomplete import. The original source can still be
inspected explicitly.

## Native gates remain separate

Descriptors record supported scopes, field mappings, adapter versions and host
loading evidence. Required unsupported controls block the mapping result.
Host loading evidence is independent of runtime enforcement: accepting a field
does not prove the host enforces it. No adapter API executes commands, dispatches
work, calls a model, installs tools or changes global configuration.

The conformance harness uses a synthetic adapter to test scope/version mismatches,
unknown preservation, semantic differences, missing/tampered provenance, source
drift and unsupported required controls. Each real adapter must additionally
provide its own official format references and isolated host loading evidence.
