# Gemini CLI subagent adapter

This adapter handles one explicitly supplied project subagent Markdown file
under `.gemini/agents/`. A pinned Gemini CLI 0.61.0 package was installed in a
temporary private prefix, and its bundled `loadAgentsFromDirectory` loader
accepted an AOK-exported fixture. This establishes source-loader parsing and
loading for the project-file scope. A bounded actual-CLI positive and empty
control also verified project discovery before authentication, so
`Descriptor().HostLoading.Status` is `verified` for project discovery only.
This does not establish agent invocation, model access, or runtime enforcement;
the shared engine continues to report runtime enforcement as `unverified`.
Import/export candidates and semantic comparison do not complete those gates.

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
and project path are documented in the official [Gemini CLI subagent docs](https://geminicli.com/docs/core/subagents/).
Workspace trust and the `GEMINI_CLI_TRUST_WORKSPACE` control are described in
[trusted folders](https://geminicli.com/docs/cli/trusted-folders/); `/agents
list` is documented in the [CLI command reference](https://geminicli.com/docs/reference/commands/).
The auto-update guard uses the pinned CLI's documented
[`general.enableAutoUpdate`](https://geminicli.com/docs/reference/configuration/)
setting.
The reproducible, credential-free direct-loader probe and fixture hashes are
recorded in [`gemini-cli-0.61.0.json`](../../../docs/evidence/native-adapters/gemini-cli-0.61.0.json).
The credential-free TUI positive/empty-control procedure is reproducible with
[`probe-gemini-cli-discovery.sh`](../../../scripts/native-loading/probe-gemini-cli-discovery.sh);
it installs the pinned CLI in a temporary npm prefix, uses a no-input PTY with
process-group timeouts, creates isolated homes, disables auto-updates, confines
host writes, checks the positive and empty controls, and prints sanitized
transcripts before cleanup.

The host probe used Gemini CLI 0.61.0 from the official `@google/gemini-cli`
package. Source-loader probes used a temporary package prefix, npm cache,
home, project, and malformed/missing controls. No
authentication environment variables were passed and no model session was
invoked.
`gemini --help` does not expose the interactive `/agents list` command
documented by Gemini CLI. An initial no-input run showed that an untrusted
project skips its custom agents. The trusted positive run then displayed
`New Agents Discovered` with the expected AOK agent name and description; a
separate empty project did not show that discovery panel. Both runs used
`GEMINI_CLI_TRUST_WORKSPACE=true`, `experimental.enableAgents: true`, isolated
HOME/XDG/TMP, and source-verified `general.enableAutoUpdate: false`; npm's
runtime prefix, config, and cache were also set to temporary locations. No
login or model prompt was sent.

An earlier Gemini TUI run attempted its default automatic global npm update.
The isolated npm log and newly created paths proved attribution. The task-created
0.64.0 package, launcher, and empty namespace were removed after review; Gemini
was absent from PATH before the task and is absent again. The repeated
positive/empty verification disabled updates, set runtime npm paths to the
temporary root, and used an OS write boundary inherited by descendants. The
positive/negative discovery and cleanup are documented in the probe JSON.
`adapterVersion` remains the local mapping revision.
