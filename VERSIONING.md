# Versioning

Agent Operating Kit uses **independent versioning** for the kit release, hook
catalog, and individual packs. Matching numbers across those surfaces is not
required.

## Kit version (releases)

**Source of truth for kit / release version:**
`.codex-plugin/plugin.json` → `version`.

GitHub Releases and installer artifact names should track that value (for
example `0.4.0`). Release packaging lives under `scripts/release/` and is
documented in `docs/release-checklist.md`.

When cutting a kit release, bump `.codex-plugin/plugin.json` (and mirror kit
plugin metadata that intentionally tracks the kit, such as
`adapters/claude-code/aok-plugin/plugin.json`, when those files are part of the
same release).

## Hooks catalog

`hooks.json` has its own `version` field. It versions the hook catalog and
command wiring, not the kit release.

**Current skew (intentional):** kit `0.4.0` vs `hooks.json` `0.1.0`. Do not
force these to match unless a release process explicitly decides to couple
them.

## Packs

Each pack versions its own content via pack manifests / `pack.content.json`
(and optional `claude-code/plugin.json` where present). Many packs remain at
`0.1.0` (or nearby, such as `0.2.0` for a pack-specific bump) while the kit is
at `0.4.0`. That is expected: pack content can ship or stabilize on a different
cadence than the kit CLI and installers.

## Schemas and adapters

- Schema changes are described in `CHANGELOG.md` and
  `docs/architecture.md` (additive preferred).
- Adapters do not require a global version lockstep with the kit; document
  support level and gaps in each adapter README.

## Summary

| Surface | Version location | Tracks kit? |
|---------|------------------|-------------|
| Kit / GitHub release | `.codex-plugin/plugin.json` | — |
| Hooks catalog | `hooks.json` | No (independent) |
| Packs | `packs/*/pack.content.json` (and related) | No (independent) |
