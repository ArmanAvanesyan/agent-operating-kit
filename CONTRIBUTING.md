# Contributing

Thanks for improving Agent Operating Kit (AOK). This kit is stdlib Python plus
JSON manifests — keep changes small, validated, and documented.

## Prerequisites

- Python 3 (stdlib only; no pip install required for kit validation)
- A Unix-like shell, or Git Bash / WSL on Windows for `./scripts/aok`
- Optional: Go toolchain when building the thin release CLI locally (see below)

## Which CLI to use

| Situation | Use |
|---|---|
| Pack, adapter, schema, or docs work; kit authorship | `./scripts/aok` (Python) |
| `task` / `hooks` / `import` without a Go build | `./scripts/aok` (Python; still supported) |
| `task` / `hooks` / `import` on the release entrypoint | Go CLI from `cmd/aok` or installed `aok` |
| Exercising the end-user release entrypoint | Build/run the Go CLI from `cmd/aok` |
| End-user install without a checkout | Release binary on `PATH` (see [docs/cli-distribution.md](docs/cli-distribution.md)) |

Prefer `./scripts/aok` for kit authorship. `task` / `hooks` / `import` work on
both the Python checkout CLI and the Go release binary (keep Python working in
parallel). Use the Go CLI from `cmd/aok` when exercising the release entrypoint.
Release CI builds the Go binary; end users do not need Go or Python.

### Building the Go CLI locally

Module: `github.com/ArmanAvanesyan/agent-operating-kit`. Main package: `cmd/aok`.

```bash
go build -o dist/aok ./cmd/aok
./dist/aok version
go test ./...
```

CI pins Go **1.22.x** (see `.github/workflows/ci.yml`). Cross-compile notes and
artifact names live in [docs/cli-distribution.md](docs/cli-distribution.md).
Set `AOK_ROOT` to override kit-root discovery when needed.

## Validate locally

From the repository root:

```bash
./scripts/aok validate
python3 -m py_compile scripts/aok
go test ./...
# Release packaging (optional local dry-run):
# ./scripts/release/build.sh dist/release
```

Optional smoke checks after pack or adapter edits:

```bash
./scripts/aok render pack engineering-guardrails --target codex
./scripts/aok render pack engineering-guardrails --target cursor
```

`validate` is dependency-free. It checks required docs, schemas, adapter and
pack manifests, pack content, JSON syntax, Claude Code marketplace metadata,
and supported target coverage.

## Project layout

| Path | Role |
|------|------|
| `schemas/` | JSON Schema for neutral AOK objects |
| `core/` | Conceptual model |
| `packs/` | Reusable workflow content |
| `adapters/` | Per-target mappings and import docs |
| `scripts/aok` | Python kit CLI (authorship, validate/render helpers; dual `task`/`hooks`/`import`) |
| `cmd/aok` | Go release CLI (end-user `PATH` binary, including `task`/`hooks`/`import`) |
| `docs/` | Operator and integration guides |

## Adding a pack

1. Create `packs/<pack-id>/` with kebab-case `pack-id`.
2. Add `pack.json` (manifest) and `pack.content.json` (neutral content). Follow
   `schemas/pack.schema.json` and an existing pack such as
   `packs/engineering-guardrails/`.
3. List `supportedTargets`, `sourceRefs`, `adapters`, and `targetRender` for
   each target you claim.
4. If Claude Code assets ship with the pack, keep them under
   `packs/<pack-id>/claude-code/` and reference them from the manifest.
5. Add a short `README.md` in the pack directory describing purpose and gaps.
6. Run `./scripts/aok validate` and
   `./scripts/aok render pack <pack-id> --target all` (or each supported
   target).

Pack versions live in pack manifests / content and may lag the kit version.
See [VERSIONING.md](VERSIONING.md).

## Adding an adapter

1. Create `adapters/<target>/` with `adapter.json` matching
   `schemas/tool-adapter.schema.json`.
2. Set honest `supportLevel`, `renders`, and `limitations`.
3. Document import steps and known gaps in `adapters/<target>/README.md`.
4. Ensure packs that claim the target include a `targetRender.<target>` entry.
5. Run `./scripts/aok validate` and sample renders for that target.

## Extending a schema

1. Prefer additive fields in `schemas/*.schema.json`.
2. Update any manifests or content that must populate the new field.
3. Note breaking renames or removals in `CHANGELOG.md` and call out migration
   steps (see also `docs/architecture.md` compatibility policy).
4. Re-run `./scripts/aok validate`.

## Documentation

- Operator guides: [`docs/`](docs/)
- Architecture and source-of-truth rules: [`docs/architecture.md`](docs/architecture.md)
- CLI distribution (Go binary vs Python): [`docs/cli-distribution.md`](docs/cli-distribution.md)
- Target support matrix: [`docs/targets-support-matrix.md`](docs/targets-support-matrix.md)
- Release steps: [`docs/release-checklist.md`](docs/release-checklist.md)
- Version policy: [`VERSIONING.md`](VERSIONING.md)
- Near-term backlog: [`ROADMAP.md`](ROADMAP.md)

Update the relevant `docs/` page when behavior or import paths change. Keep the
README focused on install and first success.

## Maintainer: release signing secrets

Optional. Configure repository secrets (see [docs/release-checklist.md](docs/release-checklist.md))
only when you want signed GitHub Release binaries. Without them,
`.github/workflows/release.yml` still publishes unsigned artifacts via
`scripts/release/sign.sh` (no-op / skip path).

Do not commit certificates or private keys. Prefer GitHub Actions secrets and
rotate credentials after any leak.

## Pull requests

Use the PR template checklist. Include validation output (or CI green) and docs
updates when user-facing behavior changes. See [SECURITY.md](SECURITY.md) for
vulnerability reports — do not open public issues for security flaws.
