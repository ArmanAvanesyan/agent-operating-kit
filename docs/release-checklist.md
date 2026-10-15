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
python3 -m unittest discover -s tests -v
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
- `docs/work-system-bootstrap.md` matches the scaffolded provider behavior and
  binding schema.
- `docs/methodology-profiles.md` matches the active profile catalog, generated
  default policy, and project CLI.
- `docs/work-item-types.md` matches the 19-type catalog, local provider
  renderers, and offline scaffold validation.
- Release kit, Codex plugin, and pack archives include the neutral
  `methodologies/` and `work-items/` catalogs and templates.
- Extracted release kits pass `scripts/aok validate`, including required sync
  and release workflow files and executable Codex setup helpers. The release
  tests verify the archive contents and extracted-kit validation.

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

### Optional host smoke (Codex + Claude Code + Cursor)

When practical, smoke the three primary hosts against this kit version and
record results using the template in
[host-compatibility.md](host-compatibility.md):

1. Install or enable the kit / pack for **Codex**, **Claude Code**, and
   **Cursor** (native installer, marketplace, or generated bundle import).
2. Confirm at least one pack surface is visible to the host (rule, skill, or
   plugin) for each smoke.
3. Update **Tested** cells in
   [targets-support-matrix.md](targets-support-matrix.md) (and swarmd notes in
   [swarmd-integration.md](swarmd-integration.md) if exercised).
4. Leave cells as `Untracked` when a host was not available — do not invent
   versions.

This step is optional for cutting a release; unsigned binaries and package
manifests do not depend on it.

**Intentional defaults (0.6.1):** unsigned Releases + `SHA256SUMS` trust, and
same-repo Scoop/Homebrew/winget layout (no external tap/bucket required). See
[cli-distribution.md](cli-distribution.md) and [`packaging/README.md`](../packaging/README.md).

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
**Missing secrets must not fail the release** — unsigned artifacts remain the
default and are fully valid. Do not require these secrets to exist before
tagging.

| Platform | Mechanism | Secrets / vars |
|---|---|---|
| macOS | `codesign` on darwin binaries | `MACOS_CERTIFICATE` (base64 PKCS#12), `MACOS_CERTIFICATE_PWD`, optional `MACOS_SIGNING_IDENTITY` |
| macOS notarize (optional) | `xcrun notarytool` stub | `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_SPECIFIC_PASSWORD`; set secret/env `AOK_ENABLE_NOTARIZE=1` to attempt |
| Windows | Azure Trusted Signing or Authenticode PFX | Trusted Signing: `AZURE_TRUSTED_SIGNING_*` (see exact names below); or `WINDOWS_CERT` + `WINDOWS_CERT_PWD` (base64 PFX) with `osslsigncode`/`signtool` on the runner |
| Linux | Optional cosign over `SHA256SUMS` (off by default) | Repo **variable** `AOK_ENABLE_LINUX_SIGN=1` + secret `COSIGN_KEY` (+ optional `COSIGN_PASSWORD`) |

### Exact secret and variable names (`sign.sh`)

Repository **secrets** (GitHub Actions `secrets.*`), all optional:

| Name | Used for |
|---|---|
| `MACOS_CERTIFICATE` | base64-encoded macOS PKCS#12 |
| `MACOS_CERTIFICATE_PWD` | PKCS#12 password |
| `MACOS_SIGNING_IDENTITY` | optional codesign identity string |
| `APPLE_ID` | notarization |
| `APPLE_TEAM_ID` | notarization |
| `APPLE_APP_SPECIFIC_PASSWORD` | notarization |
| `AOK_ENABLE_NOTARIZE` | set to `1` to attempt `notarytool` |
| `WINDOWS_CERT` | base64-encoded Windows PFX |
| `WINDOWS_CERT_PWD` | PFX password |
| `AZURE_TRUSTED_SIGNING_CLIENT_ID` | Azure Trusted Signing |
| `AZURE_TRUSTED_SIGNING_TENANT_ID` | Azure Trusted Signing |
| `AZURE_TRUSTED_SIGNING_CLIENT_SECRET` | Azure Trusted Signing |
| `AZURE_TRUSTED_SIGNING_ACCOUNT` | Azure Trusted Signing |
| `AZURE_TRUSTED_SIGNING_PROFILE` | Azure Trusted Signing |
| `AZURE_TRUSTED_SIGNING_ENDPOINT` | Azure Trusted Signing |
| `COSIGN_KEY` | optional Linux checksum signing |
| `COSIGN_PASSWORD` | optional cosign key password |

Repository **variables** (GitHub Actions `vars.*`), all optional:

| Name | Used for |
|---|---|
| `AOK_ENABLE_LINUX_SIGN` | set to `1` to enable the cosign path |

When none of the above are set, `sign.sh` prints skip messages and exits 0.

Local dry-run (always safe without secrets):

```bash
./scripts/release/sign.sh dist/release
```

## Package manager manifests

After GitHub Release assets exist:

```bash
./scripts/packaging/generate-from-release.sh --version <version>
```

Confirm same-repo published paths pin the correct URLs and SHA256 digests:

- `bucket/aok.json` (Scoop)
- `packaging/homebrew/aok.rb` (Homebrew raw-URL formula)
- `packaging/winget/*.yaml` (winget; submit to winget-pkgs when ready)

`packaging/out/` is CI staging (gitignored). Token-gated external tap/bucket
pushes remain optional (see `packaging/README.md`). **Unsigned Releases +
SHA256SUMS** are the supported trust model; **PyPI is not planned** for the Go
binary.

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
