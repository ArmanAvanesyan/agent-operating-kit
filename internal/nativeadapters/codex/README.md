# Codex custom-agent adapter

This adapter reads and writes one project-scoped custom agent at
`.codex/agents/<name>.toml`. It uses the shared strict TOML parser and never
discovers files, modifies Codex configuration, invokes a model, or executes
commands from a document.

| AOK field | Codex TOML field | Handling |
|---|---|---|
| `agent.metadata.id` | `name` | Required nonempty string, maximum 128 bytes. Codex uses `name` as identity; filename is a conventional location. Export uses `<name>.toml`. |
| `agent.identity.purpose` | `description` | Required nonempty role guidance string. |
| `executionProfile.model.selector` | `model` | Optional string; omission remains omission so the model can inherit from the parent/defaults. |
| `executionProfile.model.reasoning.mode` + `.effort` | `model_reasoning_effort` | `provider-default` omits the setting; `effort` writes the explicit effort. Other modes, including token budget and model variant, are rejected as unsupported. |
| `agent.instructions.sources` | `developer_instructions` | Exactly one source with a matching `Document.Assets` entry; the instruction string is retained. |

Unknown top-level TOML values are retained under `Unknown` and re-emitted.
No equivalence is invented for AOK filesystem/network/action permissions,
MCP/tool/skill/plugin pins, execution limits, environment controls, or other
reasoning modes. Codex custom agents inherit parent permissions, provider,
MCP servers, and sandbox/approval controls; the agent file cannot override
those controls. Required AOK controls without a direct mapping block in the
shared adapter engine. Capability inheritance does not establish that any
particular AOK restriction is enforced.

The official [Codex subagent documentation](https://developers.openai.com/codex/subagents/)
documents project files under `.codex/agents/`, required `name`, `description`,
and `developer_instructions`, optional `model` and
`model_reasoning_effort`, inheritance/default behavior, and the fact that
`name` is authoritative even when the filename differs. Reasoning-effort
values depend on the selected model. The installed 0.162.1 app-server JSON
schema defines `ReasoningEffort` as a nonempty string, so this adapter retains
provider-advertised spelling (including newer values) without asserting model
compatibility.

The installed host was Codex CLI 0.162.1. A previous isolated
`app-server initialize` plus `config/read` probe returned `agents: null`, which
does not prove that a custom agent file loads. Accordingly, descriptor
`hostLoading` remains `not-run`. No live host-loading or enforcement claim is
made. A future safe loading probe should use a disposable project and isolated
`CODEX_HOME`, request read-only configuration/agent discovery without sending
a task or prompt to a model, capture the resulting registration evidence, and
keep the evidence scoped to that exact CLI version and project layout.

Imports and exports remain `draft-incomplete`. Canonical export validates the
assignment corpus and maps explicit effort settings while preserving captured
source hashes. Required unsupported controls remain blocked.
Successful TOML parsing or semantic roundtrip does not indicate execution
readiness or runtime enforcement.

The [startup parser evidence](../../../docs/evidence/native-adapters/codex-0.162.1.json)
compares AOK-generated TOML with malformed and invalid-effort controls in an
isolated HOME/CODEX_HOME. It sends initialization and an ephemeral thread-start
request, never a turn or model prompt. The malformed control warns, while invalid effort spelling does not warn at
startup; AOK likewise validates only nonempty effort strings. Native parser acceptance is verified;
child-agent selection and instruction use are not. The descriptor conservatively
retains `not-run` for the full loading gate. To repeat, import the fixture with
an explicit native inventory, export it, compare the sidecar, then start the
isolated app server and inspect `configWarning` notifications for each control.

## Follow-up isolated visibility investigation

The [loopback request-construction probe](../../../docs/evidence/native-adapters/codex-loading-0.162.1.json)
checks both installed `multi_agent` and `multi_agent_v2` tool protocols against
positive, absent and malformed generated-file controls. A custom provider points
only to an ephemeral loopback sink that returns HTTP 400 without a model
implementation. HOME/CODEX_HOME/cwd are disposable, credentials are not inherited,
and analytics/feedback are disabled. No inference or generated tool call runs.
Neither protocol exposes the generated agent identity or description, so this
does not demonstrate custom-role discovery or selection. The earlier app-server
parser evidence remains the narrower positive result; the loading gate is open.

```sh
python3 scripts/native-loading/verify_codex.py --aok /absolute/path/to/aok \
  --codex /absolute/path/to/codex
```

The harness saves structural observations and request hashes rather than whole
model-visible payloads. Request hashes vary with ephemeral session identifiers.
