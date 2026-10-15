# OpenCode native agent adapter

This adapter reads one project-scoped Markdown agent, either an explicit
standalone `<name>.md` input or `.opencode/agents/<name>.md` in an inventory.
It writes the native `.opencode/agents/<name>.md` location. The frontmatter is parsed as bounded YAML and
the Markdown body is kept as a materialized `instructions.md` asset. It does
not discover files, invoke OpenCode, or execute a model.

The native projection is intentionally small:

| AOK field | OpenCode location | Handling |
|---|---|---|
| `agent.metadata.id` | Markdown filename | Exact basename; noncanonical AOK slugs warn and are preserved without normalization. |
| `agent.identity.purpose` | `description` | Required nonempty string. |
| `executionProfile.model.selector` | `model` | Optional nonempty provider/model selector; omitted stays omitted. |
| `executionProfile.model.generation.temperature` | `temperature` | Optional number in AOK’s supported 0–2 range; provider acceptance is not guaranteed. |
| `executionProfile.model.generation.topP` | `top_p` | Optional number in the documented 0–1 range. |
| `agent.instructions.sources` | Markdown body | Exactly one source with a matching `Document.Assets` entry. The body is retained byte-for-byte. |

Other top-level frontmatter is preserved under `Unknown` and re-emitted as
frontmatter. No equivalents are invented for AOK permission roots, network
allowlists, process constraints, MCP/capability references, role/mode,
delegation, lifecycle, evaluation, output contracts, or execution limits.
Required filesystem, process, network, or MCP controls therefore block through
the shared adapter engine. Absence of OpenCode fields remains absence; this
adapter does not turn host defaults into AOK guarantees. OpenCode-specific
reasoning options are preserved as unknown configuration. AOK reasoning
controls remain unsupported until provider-specific behavior is established.

The host-loading descriptor is `verified` for OpenCode 1.18.35, project scope.
The [captured evidence](../../../docs/evidence/native-adapters/opencode-1.18.35.json)
records importing and exporting `testdata/probe.md`, comparing the result, then
loading the generated file with `opencode --pure debug agent probe` under a
temporary HOME/XDG setup. The host resolved the expected model, body, and
parsed `bash`/`edit` denials. No model or tool executed; policy enforcement
remains unverified. Evidence applies only to that host version and fixture. The official agent
configuration reference documents Markdown frontmatter, filename-based agent
names, prompts, models, permissions, temperature, and `top_p`:
<https://opencode.ai/docs/agents/>.

Before repeating the probe, create a temporary project and isolated
`HOME`, `XDG_CONFIG_HOME`, `XDG_DATA_HOME`, and `XDG_CACHE_HOME`; place a
synthetic Markdown agent under that project's `.opencode/agents/`; run only
`opencode --pure debug agent <name>`; inspect the resolved output; then remove
the temporary directory. Do not use `run` or another model-execution command.

Every imported or exported bundle remains `draft-incomplete`. Canonical export
with `--root` validates the assignment corpus and binds its hashes, concatenating
literal instruction sources in declared order with two newline bytes. Instruction
parameters block export because template expansion is not implemented. Required
controls without native mappings remain blocked even when a candidate is emitted. A successful
parse or semantic roundtrip does not establish runtime enforcement or
execution readiness.
