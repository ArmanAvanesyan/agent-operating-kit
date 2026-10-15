# Package manager packaging

Templates and generators for distributing the Go `aok` binary via package
managers that consume **GitHub Release** assets.

| Channel | Templates | Notes |
|---|---|---|
| Homebrew | `homebrew/aok.rb.tmpl` | Formula for a tap such as `ArmanAvanesyan/homebrew-aok` |
| Scoop | `scoop/aok.json.tmpl` | Bucket manifest for Windows amd64 |
| winget | `winget/*.yaml.tmpl` | Manifests for `winget-pkgs` or a private source |

Regenerate from a published release (or local `dist/release`):

```bash
./scripts/packaging/generate-from-release.sh --version 0.6.0
# or from local artifacts:
./scripts/packaging/generate-from-release.sh --dist dist/release
```

Outputs land under `packaging/out/`.

## PyPI

**Not planned.** The release `aok` CLI is a native Go binary. Do not publish a
fake Python wheel. Use GitHub Releases, installers, or Homebrew / Scoop / winget.

## CI

`.github/workflows/packaging.yml` regenerates manifests on `v*` tags. Pushing to
an external tap / Scoop bucket / winget-pkgs is **token-gated** (`PACKAGING_PUSH_TOKEN`
or related secrets); without tokens the workflow only uploads artifacts.
