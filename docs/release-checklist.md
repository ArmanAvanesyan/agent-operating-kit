# Release Checklist

Use this checklist before publishing an AOK release, tagging a version, or
announcing a new compatibility snapshot.

## Scope

- Confirm the release purpose and changed packs, adapters, docs, or schemas.
- Confirm whether the release changes user install paths, generated contracts,
  Claude Code compatibility, or project init templates.
- Check for unrelated local edits before packaging:

```bash
git status --short
```

## Required Validation

Run:

```bash
./scripts/validate.sh
./scripts/aok validate
python3 -m py_compile scripts/aok
python3 -m py_compile scripts/release/build.py
```

Render all packs for all targets:

```bash
./scripts/aok render all --target all
./scripts/aok render bundles --target all
```

Spot check at least one Codex render and one Claude Code render:

```bash
./scripts/aok render pack engineering-guardrails --target codex
./scripts/aok render pack engineering-guardrails --target claude-code
```

Spot check Codex local-environment templates:

```bash
./scripts/aok codex local-env validate templates --json
./scripts/aok codex local-env actions templates
```

## Documentation Checks

- README first success path still matches the installer and CLI commands.
- `docs/install-and-onboarding.md` still describes current install output.
- `docs/codex-local-environments.md` still matches the setup script and action
  wrapper under `templates/.codex`.
- `docs/targets-support-matrix.md` matches pack manifests.
- `docs/migration-from-claude-code-toolbox.md` uses the canonical marketplace
  key `aok-claude-code`.
- `docs/uninstall-and-rollback.md` covers the files the installer writes.

## Compatibility Checks

- Existing Claude Code plugin names still resolve:
  - `engineering-guardrails`
  - `ship-pipeline`
  - `phased-rollout-template`
  - `batch-resolve-conflicts`
- `adapters/claude-code/marketplace/.claude-plugin/marketplace.json` still
  points at preserved compatibility assets.
- Generated contracts still include every supported target:
  - `codex`
  - `claude-code`
  - `cursor`
  - `opencode`
  - `gemini`
  - `openclaw`
  - `hermes-agent`

## Release Notes

Build release artifacts:

```bash
./scripts/release/build.sh dist/release
python3 -m json.tool dist/release/index.json >/dev/null
cd dist/release && shasum -a 256 -c SHA256SUMS
```

Confirm installer artifacts are present:

- `agent-operating-kit-<version>-macos-apple-silicon-installer.zip`
- `agent-operating-kit-<version>-macos-intel-installer.zip`
- `agent-operating-kit-<version>-windows-installer.zip`
- `agent-operating-kit-<version>-linux-user-installer.tar.gz`
- `agent-operating-kit-<version>-linux-system-installer.tar.gz`

Include:

- version or tag
- user-visible install changes
- new or changed packs
- target support changes
- migration steps for Claude Code users
- validation commands and result summary

## Rollback Plan

- Keep the previous release tag available.
- Keep previous generated render artifacts or regenerate them from the previous
  tag.
- For local installs, instruct users to switch the plugin symlink back to the
  previous checkout or remove the current install using
  `docs/uninstall-and-rollback.md`.
