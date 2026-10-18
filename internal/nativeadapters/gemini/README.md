# Gemini CLI subagent adapter

This adapter handles one explicitly supplied project subagent Markdown file
under `.gemini/agents/`. It does not scan files, invoke Gemini CLI, or install
the unavailable Gemini host. Its descriptor sets `hostLoading` to
`unavailable`; the shared engine also reports runtime enforcement as
`unverified`. An import/export candidate or semantic comparison is not a
native readiness or host-loading gate completion.

The supported projection is limited to documented literal fields:

- `name` → `agent.metadata.id`.
- `description` → `agent.identity.purpose`.
- Optional non-empty string `model` → `executionProfile.model.selector`.
- Markdown body → byte-retained `instructions.md` asset referenced by
  `agent.instructions.sources`.
- Other frontmatter, including `kind`, `tools`, `mcpServers`, `temperature`,
  `max_turns`, and `timeout_mins`, remains inert unknown native data under
  `frontmatter.<key>`. No native tool or execution behavior is inferred.
- A non-default input path is retained as `file.name`.

Encoding rejects unmapped canonical fields/assets, invalid source mappings,
and unsafe retained paths. The shared native parser limits documents to 4 MiB
and 64 nested levels and rejects invalid UTF-8/BOM, duplicate YAML keys,
explicit tags, anchors/aliases/merge keys, non-finite numbers, and YAML date
values.

## Source basis and limits

The format, required `name`/`description`, optional `model`, system-prompt body,
and project path were checked against the official Gemini CLI subagent docs on
2026-10-16: [Subagents](https://geminicli.com/docs/core/subagents/).

The environment has no Gemini CLI host, so this implementation makes no claim
about a particular installed version, file discovery, actual model selection,
or runtime behavior. `adapterVersion` is only the local mapping revision.
