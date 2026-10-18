# Static foundations validation record

Validated 2026-10-18 on the Stage 15 integration branch. This records static
checks; it is not native execution or permission-enforcement evidence.

- Full Go package tests and built CLI validation cover operational schemas,
  references, resolution, planning/decisions, parsers and adapter behavior.
- The shared eight-adapter conformance test checks declared scopes, incomplete
  drafts, required/optional unsupported controls, semantic round trips, source
  drift and bundle integrity. Runtime enforcement must remain `unverified`.
- Local cross-builds passed for Linux, macOS and Windows, each on amd64 and
  arm64 with CGO disabled. CI repeats these builds with Go 1.22. Cross-builds
  are not target-platform execution tests.
- Python regressions: 61 tests, 2 skips; contributor entrypoint compilation and
  kit validation passed. This checkout has a shell release build script, not
  `scripts/release/build.py`.
- Historical proposal checkers passed: agent spec (17 schemas, 17 examples,
  39 negative cases) and planning (8 schemas, 7 examples, 20 negative cases).
- Existing pack renders passed for every declared pack/target combination.

The exact commit checks and merge status remain authoritative in GitHub PRs.
Independent Luna review is an agent review, not a human approval. The continuation
PRs were reviewed at these heads before squash merging:

| PR | Scope | Independently reviewed head |
|---|---|---|
| [57](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/57) | Cursor | `6f578da34588ece621846d3440c26155a73d2c42` |
| [58](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/58) | Gemini | `09550dca8c3e6e595e511486072fd9be842d0e27` |
| [59](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/59) | OpenClaw | `0a8a648aaaab52c5d6538ad09078b8a2e4009843` |
| [60](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/60) | Hermes | `261c7a5beb278fe6c9d5ec404b05d2e7deedf61f` |
| [61](https://github.com/ArmanAvanesyan/agent-operating-kit/pull/61) | agy | `8b7206267b522457f2d5bdb38cc56f626d39005d` |

Review fixes included OpenClaw unknown-field nesting and literal-key preservation,
Hermes credential-assignment rejection without diagnostic echo, and path-aware
handling of Hermes' legacy `model.api` credential alias while retaining provider
API URLs. Regression fixtures use synthetic values.

## Release gate remains incomplete

OpenCode and Claude have versioned project discovery evidence. Codex, Cursor,
Gemini, OpenClaw, Hermes and agy still lack verified native loading for generated
fixtures. See [the operator gate table](../../native-adapter-operations.md) and
[compatibility inventory](../../implementation/agent-foundations/native-inventory.md).
No installer, global configuration, inference run or release publication was
performed as part of these checks.
