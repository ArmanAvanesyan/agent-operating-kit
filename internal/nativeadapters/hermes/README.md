# Hermes profile-core adapter

This adapter uses Hermes' documented profile home files, not a fabricated
standalone agent Markdown format. It accepts one explicitly supplied
`profile` inventory rooted at an isolated Hermes home/profile directory. The
required inventory is `config.yaml` plus `SOUL.md`; `profile.yaml` is optional.
All other paths are rejected. In particular, `.env`, `auth.json`, OAuth and
token stores, session databases, memory, logs, cron state, caches, skills,
archives, and runtime files are not imported or exported.

The projection is intentionally partial:

- A non-empty string `config.yaml` `model.default` maps to
  `executionProfile.model.selector`. The accepted legacy scalar `model: id`
  shape is retained and mapped because the inspected Hermes loader normalizes
  it to `model.default`.
- A string `profile.yaml` `description` maps to `agent.identity.purpose`; this
  metadata describes a profile's role for routing. Other profile metadata is
  retained inertly.
- `SOUL.md` is Hermes' primary agent identity prompt. Its exact UTF-8 bytes are
  stored under the canonical `instructions.md` asset/source name and emitted
  back to Hermes as the native `SOUL.md` file.
- The profile ID itself comes from the isolated home directory name, not these
  files. The adapter does not invent an `agent.metadata.id` value.
- Unknown config/profile fields survive as inert native data. Credential-like
  keys, credential assignments in text, and unrecognized paths are rejected.

The shared engine still marks these imports as incomplete drafts and runtime
enforcement as unverified. The reproducible loader probe uses a clean
environment, a Darwin write sandbox rooted at a unique temporary directory,
and process-group timeouts. The installed source's direct config loader,
profile-metadata reader, and SOUL prompt loader were exercised against AOK-
rendered files in a disposable `HERMES_HOME`; malformed config and missing
profile controls were also checked. This verifies those component loaders, but
does not establish full Hermes CLI profile discovery/selection. The Hermes
launcher is avoided by this final probe after an earlier invocation entered
dependency preparation and was stopped; the descriptor
continues to report host loading as unavailable and this package does not claim
a full profile loading or readiness gate. See
`docs/evidence/native-adapters/hermes-profile-loader.json`.

The selected inventory does not prove that a full Hermes home is complete.
Hermes profiles may also depend on `.env`, auth stores, skills, MCP data, and
other runtime state; those dependencies intentionally remain outside this
safe projection. The native adapter file API carries file bytes and paths but
not filesystem type metadata, so it cannot independently establish that a
provided file was not reached through a symlink. Callers must capture only
explicit regular files from the chosen profile root.

## Source basis and version limits

Format facts were checked against the official [Profiles](https://hermesagent.nousresearch.com/docs/user-guide/profiles/),
[Configuration](https://hermesagent.nousresearch.com/docs/user-guide/configuration/),
and [Personality & SOUL.md](https://hermesagent.nousresearch.com/docs/user-guide/features/personality/)
documentation, plus installed source under
`~/.hermes/hermes-agent/hermes_cli/profiles.py`, `config.py`, and
`hermes_constants.py`. The inspected source checkout identifies as
`v0.21.6+canary.20261010T070026Z-249-gd4d2ab7b5` at commit
`d4d2ab7b5d104166e7e22a02cafd12f1fe456a31`; its `pyproject.toml` reports a
placeholder version (`0.0.0`), so source-to-installed-CLI equivalence was not
established. `adapterVersion` is the local mapping revision, not a verified
Hermes runtime version.

The YAML parser rejects duplicate keys, tags, anchors, aliases, merge keys,
date/time values, invalid UTF-8/BOM, non-finite numbers, and documents beyond
4 MiB or 64 nested levels. `SOUL.md` has the same size and UTF-8 bound.
