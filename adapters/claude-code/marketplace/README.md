# Claude Code Marketplace Compatibility

This directory preserves a Claude Code marketplace entrypoint for content that
originated in `claude-code-toolbox`.

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
`packs/*/claude-code/`. Those assets remain Claude Code-specific compatibility
projections; the pack manifest beside each pack is the AOK source index.
