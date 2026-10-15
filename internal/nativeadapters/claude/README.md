# Claude Code subagent adapter

This adapter handles one explicit Claude Code subagent Markdown file. It
supports the project `.claude/agents/` scope and plugin `agents/` scope. It
does not discover files, start Claude Code, or install plugins. Project loading
is verified for Claude Code 2.1.281 using the [captured initialization response](../../../docs/evidence/native-adapters/claude-2.1.281.json).
Plugin loading remains `not-run`.

The canonical projection is deliberately small:

- `frontmatter.name` maps to `agent.metadata.id`.
- `frontmatter.description` maps to `agent.identity.purpose`.
- A string `frontmatter.model` maps to `executionProfile.model.selector`.
- The Markdown body is retained byte-for-byte in the `instructions.md` asset,
  and `agent.instructions.sources` points to that asset.
- Other frontmatter remains under `Unknown` as `frontmatter.<key>`. A source
  path that differs from the conventional path is retained as `file.name`.

Claude fields such as `tools`, `disallowedTools`, `permissionMode`,
`mcpServers`, `hooks`, and `skills` are not converted into AOK capability or
permission references. They remain inert unknown native data. The descriptor
marks corresponding controls unsupported, so required controls cannot be
reported as satisfied. In plugin scope, the adapter also warns when
`hooks`, `mcpServers`, or `permissionMode` appears: Claude Code documents
these frontmatter fields as ignored for plugin subagents. The values remain
retained so export does not silently discard them.

Encoding emits a YAML frontmatter block and the retained prompt body. It keeps
the source path when present, otherwise using `.claude/agents/<name>.md` for
project scope or `agents/<name>.md` for plugin scope. It rejects unsupported
canonical fields/assets and invalid retained paths rather than dropping them.
Native YAML parsing enforces the shared 4 MiB, 64-level, duplicate-key, tag,
anchor, alias, and unsupported-date limits.

## Source basis

Field names, body semantics, project/plugin locations, and plugin restrictions
were checked against the official Claude Code documentation on 2026-10-16:

- [Create custom subagents](https://code.claude.com/docs/en/sub-agents)
- [Plugins overview](https://code.claude.com/docs/en/plugins)

Claude Code does not guarantee a particular frontmatter model vocabulary in
the subagent format docs, so this adapter preserves any non-empty model string
without exhaustive validation against a live host; the synthetic `haiku` fixture
was observed in the host initialization response. `adapterVersion` is the local mapping
revision, not a Claude Code release version.

## Reproducing the loading gate

Use the explicit `testdata/project/aok-native-input.json` inventory for import,
then export the resulting bundle with that original directory as `--source`.
Compare the generated directory against its exported sidecar. Import/export
remain blocked incomplete drafts; the supported-field compare is valid.

Create an isolated temporary HOME and `CLAUDE_CONFIG_DIR`, inherit no credential
variables, disable auto-update/telemetry, and use the generated directory as cwd.
The evidence records all flags: start the stream-JSON client and send only the
`initialize` control request, then assert the returned `agents` entry. Terminate
the process before sending any user message. This proves identity/model discovery,
not prompt execution, tool enforcement or plugin behavior. Native source files
are selected through an inventory so their project/plugin paths remain explicit.
