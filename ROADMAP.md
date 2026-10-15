# Roadmap

Short near-term directions for Agent Operating Kit. This is a stub, not a
commitment calendar.

## Shipped

- **Thin Go CLI binary** — cross-platform `aok` binaries build in release CI and
  installers place the binary on `PATH` without Python on the device; Python
  `scripts/aok` remains for contributors and kit authorship.
  See [docs/cli-distribution.md](docs/cli-distribution.md) and
  [docs/release-checklist.md](docs/release-checklist.md).
- **Go CLI full command parity (`task` / `hooks` / `import`)** — release binary
  matches Python for `task sync|packet`, `hooks pre|after`, and
  `import history` (`task` still shells out to `gh`).

## Planned themes

- **Go release builder** — rewrite `scripts/release/build.py` as a Go maintainer
  tool so release hosts need Go + git checkout only.
- **Optional code signing** — secrets-gated macOS / Windows (and optional Linux)
  signing in release CI; unsigned path remains default when secrets are absent.
- **Homebrew / Scoop / winget** — package-manager install channels that consume
  GitHub Release assets. **PyPI is not planned** for the Go `aok` binary.
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
