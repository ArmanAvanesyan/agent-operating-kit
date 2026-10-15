# Package manager packaging

Templates and generators for distributing the Go `aok` binary via package
managers that consume **GitHub Release** assets.

**Releases are unsigned by default.** Trust binaries via the Release
`SHA256SUMS` file. macOS Gatekeeper / Windows SmartScreen may warn once on
first run; that is expected without code-signing certificates.
`scripts/release/sign.sh` skips signing when secrets or tools are absent and
never fails the release.

| Channel | Templates | Same-repo published path | Install (this repo only) |
|---|---|---|---|
| Scoop | `scoop/aok.json.tmpl` | `bucket/aok.json` | `scoop bucket add aok https://github.com/ArmanAvanesyan/agent-operating-kit` then `scoop install aok` |
| Homebrew | `homebrew/aok.rb.tmpl` | `packaging/homebrew/aok.rb` | `brew install --formula https://raw.githubusercontent.com/ArmanAvanesyan/agent-operating-kit/main/packaging/homebrew/aok.rb` (classic `user/tap` deferred) |
| winget | `winget/*.yaml.tmpl` | `packaging/winget/*.yaml` | Keep manifests here; submit a PR to `microsoft/winget-pkgs` when ready |

Regenerate from a published release (or local `dist/release`):

```bash
./scripts/packaging/generate-from-release.sh --version 0.6.0
# or from local artifacts:
./scripts/packaging/generate-from-release.sh --dist dist/release
```

The script always writes CI staging under `packaging/out/` (gitignored) and,
unless `--no-publish`, syncs the same-repo published paths above so Scoop /
Homebrew / winget docs match tracked files.

## PyPI

**Not planned.** The release `aok` CLI is a native Go binary. Do not publish a
fake Python wheel. Use GitHub Releases, installers, or Homebrew / Scoop / winget.

## CI

`.github/workflows/packaging.yml` regenerates manifests on `v*` tags. Pushing to
an external tap / Scoop bucket / winget-pkgs is **token-gated** (`PACKAGING_PUSH_TOKEN`
or related secrets) and optional — same-repo layout is enough for install.
Without tokens the workflow only uploads `packaging/out/` artifacts.
