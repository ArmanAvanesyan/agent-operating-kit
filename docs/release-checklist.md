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

Run Python kit validation (content, manifests, packaging sources):

```bash
./scripts/validate.sh
./scripts/aok validate
python3 -m py_compile scripts/aok
go test ./...
```

Build release artifacts with the Go maintainer tool:

```bash
./scripts/release/build.sh dist/release
```

Build and test the thin Go CLI across the release matrix (when `cmd/aok` is
present; Go runs in CI for publish, not on end-user devices):

| OS | Arch | Artifact |
|---|---|---|
| darwin | arm64 | `aok-<version>-darwin-arm64` |
| darwin | amd64 | `aok-<version>-darwin-amd64` |
| linux | amd64 | `aok-<version>-linux-amd64` |
| linux | arm64 | `aok-<version>-linux-arm64` |
| windows | amd64 | `aok-<version>-windows-amd64.exe` |

See [cli-distribution.md](cli-distribution.md) for naming and install outcomes.

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
- `docs/install-and-onboarding.md` still describes current install output
  (binary-first end-user path; Python for developers).
- `docs/cli-distribution.md` artifact names match published binaries.
- `docs/codex-local-environments.md` still matches the setup script and action
  wrapper under `templates/.codex`.
- `docs/targets-support-matrix.md` matches pack manifests.
- `docs/migration-from-claude-code-toolbox.md` uses the canonical marketplace
  key `aok-claude-code`.
- `docs/uninstall-and-rollback.md` covers binary + shim removal and kit tree.

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

Confirm Go CLI binary artifacts are present (built by `cmd/aok-release`):

- `aok-<version>-darwin-arm64`
- `aok-<version>-darwin-amd64`
- `aok-<version>-linux-amd64`
- `aok-<version>-linux-arm64`
- `aok-<version>-windows-amd64.exe`

Confirm `index.json` lists those binaries (and installers), and `SHA256SUMS`
covers every published file. Platform installers bundle `bin/aok` /
`bin/aok.exe` and invoke that binary for `install` / `doctor` (no Python on
the end-user path).

## Optional code signing (secrets-gated)

Release CI runs `scripts/release/sign.sh` after the build and before publishing.
**Missing secrets must not fail the release** — unsigned artifacts remain valid.

| Platform | Mechanism | Secrets / vars |
|---|---|---|
| macOS | `codesign` on darwin binaries | `MACOS_CERTIFICATE` (base64 PKCS#12), `MACOS_CERTIFICATE_PWD`, optional `MACOS_SIGNING_IDENTITY` |
| macOS notarize (optional) | `xcrun notarytool` stub | `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_SPECIFIC_PASSWORD`; set secret/env `AOK_ENABLE_NOTARIZE=1` to attempt |
| Windows | Azure Trusted Signing or Authenticode PFX | Trusted Signing: `AZURE_TRUSTED_SIGNING_*` (client id/tenant/secret/account/profile/endpoint); or `WINDOWS_CERT` + `WINDOWS_CERT_PWD` (base64 PFX) with `osslsigncode`/`signtool` on the runner |
| Linux | Optional cosign over `SHA256SUMS` (off by default) | Repo var `AOK_ENABLE_LINUX_SIGN=1` + `COSIGN_KEY` (+ optional `COSIGN_PASSWORD`) |

Local dry-run (always safe without secrets):

```bash
./scripts/release/sign.sh dist/release
```

## Package manager manifests

After GitHub Release assets exist:

```bash
./scripts/packaging/generate-from-release.sh --version <version>
```

Confirm `packaging/out/` Homebrew / Scoop / winget files pin the correct URLs
and SHA256 digests. Token-gated tap/bucket pushes are optional (see
`packaging/README.md`). **PyPI is not planned** for the Go binary.

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
