# Roadmap

Short near-term directions for Agent Operating Kit. This is a stub, not a
commitment calendar.

## Shipped

- **Thin Go CLI binary** — cross-platform `aok` binaries build in release CI and
  installers place the binary on `PATH` without Python on the device; Python
  `scripts/aok` remains for contributors and for `task` / `hooks` / `import`.
  See [docs/cli-distribution.md](docs/cli-distribution.md) and
  [docs/release-checklist.md](docs/release-checklist.md).

## Planned themes

- **Native Cursor packaging** — move beyond generated import bundles toward a
  clearer native Cursor rules/skills/hooks story when a stable format exists.
- **Deeper non-Codex / non-Claude import guides** — expand adapter READMEs and
  ops docs for Cursor, OpenCode, Gemini, OpenClaw, and Hermes Agent with
  current host import steps and known gaps.
- **Host compatibility tracking** — keep
  `docs/targets-support-matrix.md` current with min/tested host versions (or
  explicitly “untracked”) and support levels.
- **Scaffold pin maintenance** — refresh pins in
  `packs/phased-rollout-template` (tooling, Python/runtime floors, Actions) when
  regenerating that template.

## Related docs

- [VERSIONING.md](VERSIONING.md)
- [docs/targets-support-matrix.md](docs/targets-support-matrix.md)
- [docs/cli-distribution.md](docs/cli-distribution.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
