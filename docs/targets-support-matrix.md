# Targets Support Matrix

AOK renders neutral pack content into target-specific contracts and
self-contained bundles. Native usability differs by target, so the support
level below is explicit.

## Target Matrix

| Target | Status | Support level | Notes |
|---|---|---|---|
| `codex` | Active | Native install | Release installer registers AOK as a Codex local plugin and bundles pack contracts. |
| `claude-code` | Active | Compatibility asset | Preserves native Claude Code plugin assets and exposes them through `aok-claude-code`. |
| `cursor` | Active | Generated bundle | Produces structured rules, hook checklists, templates, or skill contracts depending on the pack. |
| `opencode` | Active | Generated bundle | Produces structured rules, hook checklists, templates, or conflict contracts depending on the pack. |
| `gemini` | Active | Generated bundle | Produces structured guardrail, skill, template, or conflict contracts depending on the pack. |
| `openclaw` | Active | Generated bundle | Produces structured rules, hook checklists, template bundles, or role-skill bridge contracts. |
| `hermes-agent` | Active | Generated bundle | Produces structured rules, hook checklists, templates, or conflict contracts depending on the pack. |

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
