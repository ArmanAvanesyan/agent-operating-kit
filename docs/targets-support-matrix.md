# Targets Support Matrix

AOK renders neutral pack content into target-specific contracts and
self-contained bundles. Native usability differs by target, so the support
level below is explicit.

Host AI tool versions use **Min** (declared floor, if any) and **Tested**
(last verified build). Either cell may be `Untracked`. See
[Host compatibility](host-compatibility.md) for the recording process and
log template.

## Target Matrix

| Target | Status | Support level | Min | Tested | Import / bundle path | Known gaps |
|---|---|---|---|---|---|---|
| `codex` | Active | Native install | Untracked | Untracked | Multi-platform release installers (`agent-operating-kit-<version>-<platform>-installer.*`) register the Codex local plugin; pack bundles under `build/bundles/codex/<pack>/` (also in release `*-bundles.zip`) | Neutral→skill generation is curated; keep installer wording multi-platform (not macOS-only) |
| `claude-code` | Active | Compatibility asset | Untracked | Untracked | `aok-claude-code` marketplace under `adapters/claude-code/marketplace/`; pack assets in `packs/*/claude-code/`; bundles under `build/bundles/claude-code/<pack>/` | Plugin hooks may depend on `CLAUDE_PLUGIN_ROOT`; marketplace plugin versions can lag the kit version |
| `cursor` | Active | Generated bundle | Untracked | Untracked | `build/bundles/cursor/<pack>/` (generic bundle + `.cursor/rules` / `.cursor/skills`) — [import guide](adapters/cursor.md) | Executable `.cursor/hooks.json` and marketplace `.cursor-plugin` not emitted; hooks are checklist-only |
| `opencode` | Active | Generated bundle | Untracked | Untracked | `build/bundles/opencode/<pack>/` — [import guide](adapters/opencode.md) | No native OpenCode installer; contract JSON + markdown guidance only |
| `gemini` | Active | Generated bundle | Untracked | Untracked | `build/bundles/gemini/<pack>/` — [import guide](adapters/gemini.md) | Host runtime invocation external; no native Gemini package |
| `openclaw` | Active | Generated bundle | Untracked | Untracked | `build/bundles/openclaw/<pack>/` — [import guide](adapters/openclaw.md) | Native installer / tool-call wiring is follow-up |
| `hermes-agent` | Active | Generated bundle | Untracked | Untracked | `build/bundles/hermes-agent/<pack>/` — [import guide](adapters/hermes-agent.md) | Runtime event wiring remains host-defined |

Support level meanings (schema enum):

- `native-install` — kit ships or registers a host-native package/plugin
- `compatibility-asset` — preserved host plugin/marketplace assets, not a separate operating model
- `generated-bundle` — self-contained import directory/zip; no claim of native packaging

### How to read Min / Tested

| Cell value | Meaning |
|---|---|
| `Untracked` | No declared floor (Min) or no recorded smoke (Tested) |
| `tested YYYY-MM-DD: <version>` | Last successful smoke on that UTC date |
| A bare version string | Allowed shorthand when the footnote/log has the full record |

Do **not** invent Min versions without a real compatibility decision. Prefer
leaving Min as `Untracked` until a floor is intentional.

## Pack Coverage

| Pack | Codex | Claude Code | Cursor | OpenCode | Gemini | OpenClaw | Hermes Agent |
|---|---|---|---|---|---|---|---|
| `engineering-guardrails` | Active | Active | Active | Active | Active | Active | Active |
| `ship-pipeline` | Active | Active | Active | Active | Active | Active | Active |
| `phased-rollout-template` | Active | Active | Active | Active | Active | Active | Active |
| `batch-resolve-conflicts` | Active | Active | Active | Active | Active | Active | Active |

## Render Commands

Render one pack for one target:

```bash
./scripts/aok render pack engineering-guardrails --target codex
```

Render one pack for every target:

```bash
./scripts/aok render pack engineering-guardrails --target all
```

Render every pack for every target:

```bash
./scripts/aok render all --target all
```

Generate self-contained bundles for every pack and target:

```bash
./scripts/aok render bundles --target all
```

## Output Locations

Without `--out`, single-pack renders write to:

```text
build/renders/<pack>/<target>.json
```

Bulk renders write to:

```text
build/renders/<target>/<pack>.json
```

Bundle output writes to:

```text
build/bundles/<target>/<pack>/
build/bundles/index.json
```

Use `--out PATH` to write somewhere else, for example:

```bash
./scripts/aok render all --target codex --out /tmp/aok-renders
```
