# AOK Migrate Toolbox

Use this skill when the user is moving from `claude-code-toolbox` to Agent
Operating Kit.

## Workflow

1. Treat AOK as the source of truth.
2. Register the AOK Claude Code marketplace:

```bash
aok claude setup --enable all
```

3. Confirm the Claude Code marketplace key is `aok-claude-code`.
4. Replace old plugin suffixes with the AOK marketplace suffix:

| Old | New |
|---|---|
| `engineering-guardrails@<old-marketplace>` | `engineering-guardrails@aok-claude-code` |
| `ship-pipeline@<old-marketplace>` | `ship-pipeline@aok-claude-code` |
| `phased-rollout-template@<old-marketplace>` | `phased-rollout-template@aok-claude-code` |
| `batch-resolve-conflicts@<old-marketplace>` | `batch-resolve-conflicts@aok-claude-code` |

5. Keep `agent-operating-kit@aok-claude-code` enabled as the control plugin.
6. Run AOK validation:

```bash
aok validate
```

## Rollback

Use the timestamped settings backup created by `aok claude setup`, or restore
the previous `extraKnownMarketplaces` and `enabledPlugins` entries manually.
