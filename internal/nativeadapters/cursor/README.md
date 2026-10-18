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
