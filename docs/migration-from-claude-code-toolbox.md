# Migration From Claude Code Toolbox

`agent-operating-kit` is now the canonical project. `claude-code-toolbox`
content is represented inside AOK as Claude Code adapter assets and curated
workflow packs.

## What Changed

- Claude Code plugin content moved conceptually into AOK packs.
- The Claude Code marketplace shape is preserved under
  `adapters/claude-code/marketplace/` and `packs/*/claude-code/`.
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
        "path": "/path/to/claude-code-toolbox/marketplace"
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

## Migration Rollback

To roll back temporarily:

1. Restore the old `extraKnownMarketplaces` entry in `~/.claude/settings.json`.
2. Change enabled plugin suffixes back to the old marketplace key.
3. Leave the AOK checkout in place if other targets use its generated renders.

Rollback should be temporary. New pack content and cross-target behavior should
continue to land in AOK first.

## Compatibility Note

The old toolbox repository can remain as a compatibility pointer, but new
content and integration decisions should land in AOK first. If a Claude Code
feature remains target-specific, keep it under the relevant pack's `claude-code/`
and update both `pack.content.json` and `pack.json`.

## Deprecation and Compatibility

- Existing `claude-code-toolbox` usage is still supported via compatible plugin
  names in `adapters/claude-code/marketplace/.claude-plugin/marketplace.json`.
- Non-Claude targets consume generated, self-contained import bundles; native
  marketplace installers are planned where not yet available.
