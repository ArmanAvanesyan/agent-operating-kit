# AOK Pack Discovery

Use this skill when the user asks which AOK pack to use.

## Packs

| Need | Pack |
|---|---|
| Safer everyday agent sessions and guardrail hooks | `engineering-guardrails` |
| PR shipping and CI recovery | `ship-pipeline` |
| A phased project scaffold | `phased-rollout-template` |
| Coordinated branch or PR conflict resolution | `batch-resolve-conflicts` |

## Workflow

1. Ask what outcome the user wants.
2. Recommend one pack first; avoid enabling every pack unless the user wants a
   full AOK setup.
3. Explain target support:
   - Codex uses native plugin-oriented bundles.
   - Claude Code uses `aok-claude-code` marketplace plugins.
   - Cursor, OpenCode, Gemini, OpenClaw, and Hermes Agent use generated import
     bundles.
4. Render or enable the pack only after identifying the target tool.

## Commands

Enable all Claude Code plugins:

```bash
aok claude setup --enable all
```

Enable only the root plugin and one pack:

```bash
aok claude setup --enable agent-operating-kit engineering-guardrails
```
