# Claude Code Marketplace Compatibility

This directory is the AOK-owned Claude Code marketplace entrypoint. Content
originally migrated from `claude-code-toolbox` is maintained entirely in AOK.
The toolbox is retired; registration and installation require no access to it.

Register this directory as a Claude Code marketplace:

```json
{
  "extraKnownMarketplaces": {
    "aok-claude-code": {
      "source": {
        "source": "directory",
        "path": "/path/to/agent-operating-kit/adapters/claude-code/marketplace"
      }
    }
  }
}
```

The `.claude-plugin/marketplace.json` file points at AOK pack assets under
`packs/*/claude-code/` and the root control plugin under
`adapters/claude-code/aok-plugin/`. Pack assets remain Claude Code-specific
compatibility projections; the pack manifest beside each pack is the AOK source
index.
