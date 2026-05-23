# AOK Setup

Use this skill when the user wants to install, enable, or verify Agent Operating
Kit for Claude Code and Codex.

## Workflow

1. Locate the AOK checkout or installed release directory.
2. Run `aok doctor` if `aok` is on `PATH`; otherwise run
   `./scripts/aok doctor` from the AOK directory.
3. Register the Claude Code marketplace:

```bash
aok claude setup --enable agent-operating-kit
```

If `aok` is not on `PATH`, run:

```bash
./scripts/aok claude setup --enable agent-operating-kit
```

4. Ask the user to restart or refresh Claude Code if the plugin list does not
   update immediately.
5. Confirm `agent-operating-kit@aok-claude-code` is enabled before enabling
   workflow packs.

## Success Signal

- `aok doctor` reports required files and marketplace registration as `OK`.
- Claude Code settings include `agent-operating-kit@aok-claude-code`.
- The AOK marketplace key is `aok-claude-code`.
