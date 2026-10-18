# Cursor project subagent adapter

This adapter handles one explicitly supplied Markdown subagent under
`.cursor/agents/`. It does not scan a project, run Cursor, or prove that Cursor
loads a given file. `HostLoading` is `not-run` and the shared engine reports
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

## Installed loader verification and scalar compatibility

The [2026.04.17 component probe](../../../docs/evidence/native-adapters/cursor-component-2026.04.17.json)
invokes the shipped `LocalSubagentsService` against an AOK-generated file.
Only the bundle startup entry is replaced in memory to expose its existing
module export; the loader module/factories remain unchanged. HOME/project are
temporary; Node TCP connect, HTTP(S) request and fetch are guarded. The positive fixture is discovered
with its name, description, model and prompt; absent and malformed controls return
no agents. This is component discovery, not full CLI startup, selection or
inference. `HostLoading` stays `not-run` for the overall CLI gate.

This installed loader splits frontmatter lines at their first colon and does
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

The mapping revision is `cursor-subagents-v2`; older sidecars require re-import
under the new compatibility checks rather than silently reusing old assumptions.
