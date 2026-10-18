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
enforcement as unverified. A bounded probe directly invoked the installed
environment's `venv/bin/python -B -m hermes_cli.main profile list` against an
AOK-rendered profile in an isolated `HERMES_HOME`; the generated profile was
listed with its configured model. Missing and malformed configuration controls
were also checked. Separately, the exact installed workspace's
`agent.prompt_builder.load_soul_md(home_override=...)` returned the AOK-rendered
SOUL body with surrounding whitespace stripped and its expected body hash,
and returned `None` for a missing profile. The exported file itself was
unchanged after CLI startup.
The profile-list command does not load SOUL; these are separate observations.
The descriptor records verified profile loading for the directly invoked
installed entrypoint, with CLI selection by the package-manager launcher left
unverified. Runtime enforcement and canonical readiness remain unverified.
Repeat the bounded installed-environment check with
[`probe-hermes-installed.py`](../../../scripts/native-loading/probe-hermes-installed.py).
See [`hermes-cli-profile-list.json`](../../../docs/evidence/native-adapters/hermes-cli-profile-list.json)
and [`hermes-profile-loader.json`](../../../docs/evidence/native-adapters/hermes-profile-loader.json).

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
`hermes_constants.py`. The directly invoked installed workspace reported
`Hermes Agent vunknown (2026.9.24)` on Python 3.14.7. Its relevant source
files are pinned by SHA-256 in the evidence record. The environment path was selected explicitly; a
package-manager launcher receipt linking the normal `hermes` command to this
environment was not verified. The separately inspected source checkout's Git
revision is not used as the installed runtime identity. `adapterVersion` is the
local mapping revision, not a Hermes runtime version.

The YAML parser rejects duplicate keys, tags, anchors, aliases, merge keys,
date/time values, invalid UTF-8/BOM, non-finite numbers, and documents beyond
4 MiB or 64 nested levels. `SOUL.md` has the same size and UTF-8 bound.
