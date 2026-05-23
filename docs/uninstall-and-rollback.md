# Uninstall And Rollback

This guide removes a local AOK install or rolls it back to an earlier checkout.
It does not remove projects that were initialized with `aok project init`.

## What Install Adds

The release installer copies the kit to:

```text
~/Library/Application Support/Agent Operating Kit/agent-operating-kit
```

It then registers:

- `~/.agents/plugins/marketplace.json`
- `~/.agents/plugins/plugins/agent-operating-kit` as a symlink to the installed
  kit
- `~/.local/bin/aok` as a symlink to `scripts/aok`
- `~/Library/Application Support/Agent Operating Kit/setup-report.md`

Claude Code setup may also add an `aok-claude-code` entry to
`~/.claude/settings.json`.

## Uninstall Local AOK

Remove the plugin symlink:

```bash
rm ~/.agents/plugins/plugins/agent-operating-kit
```

Remove the CLI shim:

```bash
rm ~/.local/bin/aok
```

Remove the installed kit and setup report:

```bash
rm -rf "$HOME/Library/Application Support/Agent Operating Kit"
```

Edit `~/.agents/plugins/marketplace.json` and remove the plugin entry whose
`name` is `agent-operating-kit`.

If you added Claude Code compatibility, edit `~/.claude/settings.json` and
remove:

- `extraKnownMarketplaces.aok-claude-code`
- any `enabledPlugins` entries ending in `@aok-claude-code`

## Roll Back To Another Checkout

If you keep two AOK checkouts, point the symlinks back to the checkout you want:

```bash
rm ~/.agents/plugins/plugins/agent-operating-kit
ln -s /path/to/previous/agent-operating-kit ~/.agents/plugins/plugins/agent-operating-kit
rm ~/.local/bin/aok
ln -s /path/to/previous/agent-operating-kit/scripts/aok ~/.local/bin/aok
```

Then verify:

```bash
/path/to/previous/agent-operating-kit/scripts/aok doctor
/path/to/previous/agent-operating-kit/scripts/aok validate
```

## Roll Back Project Init

Project init writes files into the target project. Rollback depends on what the
init output reported:

- `created`: remove the created file if you do not want AOK in that project.
- `unchanged`: no action needed.
- `preserved AGENTS.md`: remove `AGENTS.aok-proposed.md` and `AGENTS.aok.diff`.
- `updated`: restore the previous file from `.aok-backups/<timestamp>/`.

`aok project init` also writes `.aok/project.json`. Remove `.aok/` only if the
project should no longer track AOK initialization metadata.

## Validate Removal

After uninstalling, these paths should be absent unless another local install
owns them:

```bash
test ! -e ~/.agents/plugins/plugins/agent-operating-kit
test ! -e ~/.local/bin/aok
```

If Claude Code still shows AOK plugins, recheck `~/.claude/settings.json` for
`aok-claude-code`.
