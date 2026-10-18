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
- **Homebrew / Scoop / winget** — same-repo manifests (`bucket/`,
  `packaging/homebrew/aok.rb`, `packaging/winget/`), regenerated from Release
  `SHA256SUMS` / `index.json`. Unsigned Releases + checksums are the default
  trust model. **PyPI is not planned**
  for the Go `aok` binary.
- **Host compatibility tracking** — `docs/targets-support-matrix.md` tracks
  Min / Tested host versions (or explicitly `Untracked`); recording process and
  optional release smoke live in `docs/host-compatibility.md` and
  `docs/release-checklist.md`.
- **Deeper non-Codex / non-Claude import guides** — `docs/adapters/` pages plus
  expanded READMEs for Cursor, OpenCode, Gemini, OpenClaw, and Hermes Agent
  (prerequisites, bundle paths, verification, failure modes).
- **Scaffold pin maintenance** — `packs/phased-rollout-template` tooling /
  Python runtime floors / Actions refreshed (see CHANGELOG Unreleased).
- **Native Cursor packaging** — `aok render bundle --target cursor` emits an
  additive `.cursor/rules` + `.cursor/skills` tree (hooks as checklist) beside
  `aok-bundle.json`; see `docs/adapters/cursor.md`.

## Planned themes

_(none — planned ROADMAP themes from the 0.6.0 cut are shipped)_

## Related docs

- [VERSIONING.md](VERSIONING.md)
- [docs/targets-support-matrix.md](docs/targets-support-matrix.md)
- [docs/cli-distribution.md](docs/cli-distribution.md)
- [CONTRIBUTING.md](CONTRIBUTING.md)

## Embedded repository delivery

The repository-delivery pack adds deterministic preservation and delivery helpers,
an optional Python bridge in the Go CLI, and complete installed/rendered resources.
See [design and acceptance](docs/implementation/repository-delivery.md) and
[validation evidence](docs/evidence/repository-delivery-validation.md). Windows
certification and live runtime orchestration remain future work; embedding does not
add a producer, heartbeat, recovery, approval quorum or AOK submission endpoint.
