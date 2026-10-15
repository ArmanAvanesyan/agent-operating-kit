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
- **Go release builder** — `cmd/aok-release` (via `scripts/release/build.sh`)
  builds kit zips, cross-compiled binaries, installers, bundles (via Go
  `aok render bundles`), `SHA256SUMS`, and `index.json`.
- **Optional code signing** — secrets-gated macOS / Windows (and optional Linux
  cosign) in release CI via `scripts/release/sign.sh`; unsigned path remains
  default when secrets are absent.
- **Homebrew / Scoop / winget** — package-manager templates under `packaging/`,
  regenerated from Release `SHA256SUMS` / `index.json`. **PyPI is not planned**
  for the Go `aok` binary.
- **Host compatibility tracking** — `docs/targets-support-matrix.md` tracks
  Min / Tested host versions (or explicitly `Untracked`); recording process and
  optional release smoke live in `docs/host-compatibility.md` and
  `docs/release-checklist.md`.
- **Deeper non-Codex / non-Claude import guides** — `docs/adapters/` pages plus
  expanded READMEs for Cursor, OpenCode, Gemini, OpenClaw, and Hermes Agent
  (prerequisites, bundle paths, verification, failure modes).

## Planned themes

- **Native Cursor packaging** — move beyond generated import bundles toward a
  clearer native Cursor rules/skills/hooks story when a stable format exists.
- **Scaffold pin maintenance** — refresh pins in
  `packs/phased-rollout-template` (tooling, Python/runtime floors, Actions) when
  regenerating that template.

## Related docs

- [VERSIONING.md](VERSIONING.md)
- [docs/targets-support-matrix.md](docs/targets-support-matrix.md)
- [docs/cli-distribution.md](docs/cli-distribution.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)
