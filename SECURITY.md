# Security Policy

## Supported versions

Security fixes are considered for the latest released kit version on `main`
(see `.codex-plugin/plugin.json` and GitHub Releases). Older tags may not
receive backports.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security problems.

Report privately using one of these channels:

1. **GitHub Security Advisories** (preferred): open a private advisory on this
   repository via
   [Security → Advisories → Report a vulnerability](https://github.com/ArmanAvanesyan/agent-operating-kit/security/advisories/new).
2. If advisories are unavailable, contact the maintainer through GitHub:
   [@ArmanAvanesyan](https://github.com/ArmanAvanesyan).

Include:

- A clear description of the issue and impact
- Steps to reproduce or a proof of concept if safe to share privately
- Affected version / commit if known

You should receive an acknowledgment when the report is seen. Please allow
reasonable time for assessment and a fix or advisory before any public
disclosure.

## Scope notes

AOK is primarily manifests, schemas, and a stdlib Python CLI. Typical concerns
include unsafe path handling in install/render scripts, privilege issues in
Linux system installers, and supply-chain problems in release artifacts — not
hosted multi-tenant services.
