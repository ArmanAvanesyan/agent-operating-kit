# Migration From Claude Code Toolbox

`claude-code-toolbox` is retired. `agent-operating-kit` owns and maintains its
replacement Claude Code adapter assets and workflow packs. AOK installation,
validation, rendering, and runtime use do not require the old repository or a
local toolbox checkout. Old names below identify settings to migrate only.

## What Changed

- Claude Code plugin content moved conceptually into AOK packs.
- The Claude Code marketplace shape is preserved under
  `adapters/claude-code/marketplace/` and `packs/*/claude-code/`.
- A root `agent-operating-kit` Claude Code plugin now exposes AOK setup,
  diagnostics, rendering, migration, and pack discovery workflows.
- AOK pack manifests now carry canonical intent and cross-target render policy.
- Each pack now has `pack.content.json` as the neutral source.

## Pack Mapping

| Old toolbox plugin | New AOK pack |
|---|---|
| `engineering-guardrails` | `packs/engineering-guardrails` |
| `ship-pipeline` | `packs/ship-pipeline` |
| `phased-rollout-template` | `packs/phased-rollout-template` |
| `batch-resolve-conflicts` | `packs/batch-resolve-conflicts` |

## Migration Path

1. Install AOK from `ArmanAvanesyan/agent-operating-kit`.
2. Enable the Claude Code adapter assets from AOK instead of cloning the old
   toolbox as the source of truth.
3. For existing Claude Code projects, keep current enabled plugin names while
   switching the marketplace path to AOK's Claude Code compatibility assets. The
   canonical marketplace name is `aok-claude-code`.
4. Use the setup helper when possible so existing settings are backed up before
   editing: `aok claude setup --enable all`.
   After verifying the replacements, remove old marketplace entries (`toolbox`,
   `claude-code-toolbox`, or custom keys pointing at the retired checkout) and
   old enabled-plugin suffixes from user and project-local settings. The helper
   registers AOK; do not assume it removes every legacy entry.
5. For new targets and future tools, generate target bundles with:
   `./scripts/aok render bundles --target all`.
6. For target-specific contract inspection, generate target contracts with:
   `./scripts/aok render pack <pack-id> --target <target>`.

## Before And After Settings

Before migration, a Claude Code setup may point directly at a local
`claude-code-toolbox` marketplace or use a non-canonical marketplace key:

```json
{
  "extraKnownMarketplaces": {
    "claude-code-toolbox": {
      "source": {
        "source": "directory",
        "path": "/path/to/claude-code-toolbox"
      }
    }
  },
  "enabledPlugins": {
    "engineering-guardrails@claude-code-toolbox": true,
    "ship-pipeline@claude-code-toolbox": true
  }
}
```

After migration, point Claude Code at AOK's compatibility marketplace and use
the canonical marketplace key `aok-claude-code`. The safe helper command is:

```bash
aok claude setup --enable all
```

The resulting settings shape is:

```json
{
  "extraKnownMarketplaces": {
    "aok-claude-code": {
      "source": {
        "source": "directory",
        "path": "/path/to/agent-operating-kit/adapters/claude-code/marketplace"
      }
    }
  },
  "enabledPlugins": {
    "agent-operating-kit@aok-claude-code": true,
    "engineering-guardrails@aok-claude-code": true,
    "ship-pipeline@aok-claude-code": true
  }
}
```

The plugin names stay the same. The marketplace suffix changes to
`@aok-claude-code`.

## Pack-By-Pack Migration

| Keep enabled if you used | Enable from AOK |
|---|---|
| AOK control workflows | `agent-operating-kit@aok-claude-code` |
| `engineering-guardrails@<old-marketplace>` | `engineering-guardrails@aok-claude-code` |
| `ship-pipeline@<old-marketplace>` | `ship-pipeline@aok-claude-code` |
| `phased-rollout-template@<old-marketplace>` | `phased-rollout-template@aok-claude-code` |
| `batch-resolve-conflicts@<old-marketplace>` | `batch-resolve-conflicts@aok-claude-code` |

## Validate The Migration

From the AOK checkout:

```bash
./scripts/aok validate
./scripts/aok render pack engineering-guardrails --target claude-code
./scripts/aok render pack ship-pipeline --target claude-code
```

If Claude Code cannot find a plugin, check:

- `~/.claude/settings.json` uses `aok-claude-code`.
- the marketplace path points to
  `/path/to/agent-operating-kit/adapters/claude-code/marketplace`.
- enabled plugin entries use the same `@aok-claude-code` suffix.

## Shipping Workflow Dependency

AOK's `/ship-pr` uses the AOK `repository-delivery` capability. Install/enable
that capability alongside `ship-pipeline`; its helpers require Python 3.11+.
The workflow preserves local work and requires explicit authority for publication
and merge. It does not retain the toolbox's unconditional auto-merge behavior.
See [repository delivery](repository-delivery.md).

## Recovery

The setup helper's timestamped settings backup can recover unrelated settings.
Keep the AOK marketplace path and plugin suffixes when recovering configuration;
the retired toolbox is not a supported rollback source.

Before deleting an old local checkout, preserve any uncommitted work and verify
that user and project settings no longer reference it. No toolbox clone or
remote repository is needed afterward.

## Compatibility and Historical Names

The four plugin names remain available through `aok-claude-code`; old marketplace
keys and `@toolbox` suffixes are not aliases and must be migrated.
All maintenance belongs in AOK's pack manifests, neutral content, and
`packs/*/claude-code/` assets. The old repository need not be retained as a pointer.

The Python renderer retains the legacy `targetContract` value
`claude-code-toolbox-compatible` for existing contract consumers. This is a
compatibility identifier, not a repository reference or an external dependency.
