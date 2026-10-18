# Codex custom-agent adapter

The adapter has two explicit inventories:

| Scope | Required input files | Host-loading status |
|---|---|---|
| `project` | One `.codex/agents/<name>.toml` | `not-run`; standalone files are not registered by this scope. |
| `project-registered` | Exactly `.codex/config.toml` and one `.codex/agents/<name>.toml` | `verified` for project registration/loading checks on Codex CLI 0.162.1 only. |

`project-registered` selects `[agents.<name>]` using the role filename stem.
Its `description` must exactly match the role file, and its `config_file` must
be exactly `agents/<name>.toml` relative to `.codex/config.toml`. The adapter
does not resolve paths, access the filesystem, or open any other configured
role. Absolute paths, traversal, backslashes, mismatched filenames, missing
files from the explicit inventory, and selected description mismatches are
rejected. Names reserved by Codex's `[agents]` settings (including `enabled`,
`max_depth`, `max_threads`, and the default-subagent, interruption,
concurrency, and runtime settings) are rejected as role identities in this
scope.

The selected `description` maps to `agent.identity.purpose`; the TOML role
fields map as follows:

| AOK field | Codex TOML field | Handling |
|---|---|---|
| `agent.metadata.id` | `name` | Required nonempty string, maximum 128 bytes; registered scope requires exact filename-stem equality. |
| `agent.identity.purpose` | `description` | Required nonempty string; must match the project registration. |
| `executionProfile.model.selector` | `model` | Optional string; omission remains omitted. |
| `executionProfile.model.reasoning.mode` + `.effort` | `model_reasoning_effort` | `provider-default` omits the setting; `effort` writes the explicit value. Other reasoning modes are unsupported. |
| `agent.instructions.sources` | `developer_instructions` | Exactly one materialized instruction source. |

Standalone role TOML values remain under `Unknown` as `toml.*`. In the
registered scope, unrelated root settings and sibling registrations are
preserved under `Unknown.codex.config`; extension keys on the selected
registration are preserved under `Unknown.codex.registered.role.*`. Encoding
reconstructs the selected registration from mapped fields and rejects a
renamed role that collides with a preserved sibling, including case-folded
names. Global scalar settings such as `agents.enabled`, `agents.max_depth`,
`agents.max_threads`, and default-subagent settings remain root config values;
they are never interpreted as role tables. A selected identity also cannot
collide with a preserved scalar `[agents]` key, including unknown future
settings. Sibling `config_file` values that
lexically alias the selected output path after slash normalization, path
cleaning, and case folding are rejected without opening those paths. A warning identifies
unmapped project config settings. Sibling `config_file` references receive a
separate warning and are never opened. Unknown native settings can affect host
behavior beyond this adapter's mapping; their preservation is not validation
or enforcement.

No equivalence is invented for AOK filesystem/network/action permissions,
MCP/tool/skill/plugin pins, execution limits, environment controls, or other
reasoning modes. Required unsupported controls remain blocked by the shared
engine. `project-registered` loading evidence establishes project registration
and startup loading/validation of the selected file for the tested host
version; it does not establish agent selection, instruction use, model
compatibility, or runtime enforcement. Import/export candidates remain
`draft-incomplete`.

The official [Codex configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference)
documents `[agents.<name>]`, `description`, and `config_file`, whose paths are
relative to the config file containing the declaration. The official
[subagent guide](https://developers.openai.com/codex/subagents/) documents
`.codex/agents/` role files. The versioned isolated probe is recorded at
[`codex-project-registered-0.162.1.json`](../../../docs/evidence/native-adapters/codex-project-registered-0.162.1.json).
It uses only synthetic role data, a disposable trusted project and isolated
`CODEX_HOME`; it sends app-server initialize/config/read and an ephemeral
read-only thread/start, never a turn, prompt, login request, or model call.
Positive, absent-registration, malformed-config, malformed-role, and missing-
role controls are included. AOK output is rebuilt from this adapter before
host checks. The recorded result applies only to the tested project-registered
scope and Codex CLI version 0.162.1; standalone `project` remains `not-run`.

To repeat with local binaries, first copy the installed 0.162.1 Codex executable
into a disposable path, then pass that copy and a freshly built AOK binary:

```sh
python3 -B scripts/native-loading/verify_codex_project_registered.py \
  --aok /absolute/path/to/aok \
  --codex-copy /absolute/path/to/copied/codex
```

The harness rebuilds a registered native export via `agent native import` and
`agent native export`, runs both binaries with explicit environment allow-lists,
and confines host writes to its temporary root while denying host network
access. Its disposable `CODEX_HOME` uses file-based credential storage and
inherits no credential variables. It prints sanitized JSON evidence and removes its
temporary files on exit. The script source hash is stored in the evidence
record.

Adapter version 0.2.0 changes the registered-scope projection. Existing
0.1.x sidecars should be re-imported from their explicit native inventory
before exporting with the new scope; no automatic migration or filesystem
discovery occurs.
