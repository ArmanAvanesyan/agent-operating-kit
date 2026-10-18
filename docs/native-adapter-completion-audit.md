# Native adapter completion audit

Audited 2026-10-18 from `93f778b225c632ff31372ab69651581a5ad57fbe`, against the
[original foundation delivery contract](implementation/agent-foundations/README.md).
Two independent read-only adapter audits covered all eight targets; the primary
agent inspected shared boundaries and integrated the fixes. A separate reviewer
checks the final commit before merging. The
[machine-readable audit](evidence/native-adapters/completion-audit-2026-10-18/audit.json)
binds the retained native evidence and current validation logs by SHA-256.

**Result:** each target has at least one bounded native-loading scope supported
by saved native observations. Static projection, provenance and shared engine
checks pass for the implemented subset. These are native formats and actual
client/component observations, not neutral pack bundles. This audit replays
static validation and inspects existing host evidence; it does **not** rerun
all native clients, logins, models, or installed environments.

This supports the original minimum-one-native-scope gate. It does not establish
complete support for every client setting, every supported parsing scope, or
runtime policy enforcement. Native import/export still emits blocked,
`draft-incomplete` projections; it does not materialize a fully validated
canonical Agent/Assignment into an executable installation. No release is
published and no workflow/model/optimizer is run by this audit.

## Gate-by-gate assessment

| Original acceptance gate | Evidence and verdict | Remaining boundary |
|---|---|---|
| At least one real native loading scope for each of eight clients | Qualified within the versions/scopes below; exact artifact hashes are in the audit manifest | Historical host evidence is not a fresh installation or all-platform certification |
| Native parsing, strict duplicate/unsafe syntax rejection | Native parser tests plus adapter positive/negative fixtures; offline Go tests pass | AOK may reject inputs a host accepts; AOK rejection is not proof of native rejection |
| Supported-field semantic round trips | Adapter-specific tests and shared conformance; built CLI import → export → compare for all eight targets and both extra scopes | Comments, ordering, formatting and implicit host defaults are not byte-preservation promises; originals remain sidecar provenance |
| Unknown native content and provenance preserved | Adapter tests retain unknown values/assets; exported candidates are decoded and compared before return | Unknown native controls remain inert data to AOK, not supported/enforced canonical controls |
| Sidecar loss, source drift and tampering | Shared engine digest/tampering tests; built CLI re-import with sidecar, loss warning without sidecar, drift rejection on import/export | Integrity hashes detect mutation, not trusted authorship; missing sidecars cannot reconstruct canonical identity |
| Version/scope differences explicit | Scope-specific E2E assertions include verified Codex registration, unverified Codex standalone and unverified Claude plugin | Other versions, plugin loading and launcher equivalents remain outside retained proof |
| Required unsupported/unenforceable controls block | Common required network-control tests and adapter-specific controls; imports/exports stay incomplete | An optional/unknown host field can survive without proving safe runtime policy |
| Unsafe native inputs and bounded output | Native parser, input inventory, path/symlink, credential-key, output collision/no-overwrite and CLI tests | Supplied inputs must be credential-free; preserved instructions/assets are not a general secret scanner |
| Native loading never implies runtime enforcement | Every tested report keeps `runtimeEnforcement: unverified`; unsupported required controls remain blocked | No execution permission, sandbox, tool/MCP/skill/plugin enforcement or authorization is established |
| End-to-end Go and Python regression | Built binary with empty PATH; Go/Python suites, kit validation, compilation, proposal and render checks | Live workflow dispatch, inference, RL, optimization and routing remain later milestones |
| Distribution | Go module targets 1.22; PR CI checks Go 1.22, binary execution and six OS/architecture builds | Build success is not native host loading on each platform; no global install or release publication |

The static conformance implementation is in
[`conformance_test.go`](../internal/nativeadapters/conformance_test.go),
[`engine_test.go`](../internal/nativeadapter/engine_test.go), and the individual
adapter test files. The actual executable boundary is
[`TestFoundationsCLI`](../cmd/aok/foundations_test.go), which builds AOK then
runs with an allow-listed environment and empty PATH. The native loading gate
uses the separately saved host observations below, never a successful Go parse
or generated neutral bundle as a substitute.

## Eight native-loading scopes and limits

| Target | Retained host/scope | What the native evidence establishes | Limits and next verification |
|---|---|---|---|
| OpenCode | 1.18.35, project | [Isolated `--pure debug agent` resolution](evidence/native-adapters/opencode-1.18.35.json) observes rendered metadata/model/config | Configuration resolution only. Broad inherited defaults are recorded; selection, tool execution, model behavior and permission enforcement are unverified |
| Claude Code | 2.1.281, project | [Isolated initialization response](evidence/native-adapters/claude-2.1.281.json) discovers the supplied agent metadata | Plugin scope is parse/render only and reports `not-run`; hooks/MCP/permission-mode differences are warned/preserved, not implemented as canonical policy. No inference or prompt-use proof |
| Codex | CLI 0.162.1, `project-registered` | [Explicit config registration and referenced role loading](evidence/native-adapters/codex-project-registered-0.162.1.json), including malformed/missing-file controls | Standalone `project` remains `not-run`. No child-agent invocation, model/effort execution or runtime-enforcement proof |
| Cursor | 2026.10.16-e3cb207, project | [Command-picker decision/evidence](evidence/native-adapters/cursor-project-discovery-decision-2026-10-18.json): exact description appears only for the valid project, absent/blank-body controls suppress it | Uses an explicit Git retrieval shim, not a global OS network restriction. Selected-agent startup, prompt-body use, inference and enforcement unverified; scanner-compatible Markdown subset only |
| Gemini | CLI 0.61.0, project | [Bundled loader plus native TUI discovery](evidence/native-adapters/gemini-cli-0.61.0.json): exported definition appears, empty control omits it; loader rejects malformed YAML | TUI discovery is before authentication; loader rejection is component-level. Invocation, personal/global scope and runtime enforcement unverified |
| OpenClaw | 2026.9.4, Linux Node 26.7.0, isolated HOME | [Network-disabled container roster/workspace controls](evidence/native-adapters/openclaw-container-2026.9.4.json) read the rendered roster and exact workspace bytes | Darwin CLI unverified. Bounded single-agent/default-workspace projection, no tool-call wiring or runtime enforcement |
| Hermes Agent | `vunknown (2026.9.24)`, source-hash-bound installed environment, profile | [Direct installed CLI profile listing](evidence/native-adapters/hermes-cli-profile-list.json) reads metadata/model; [adapter notes](../internal/nativeadapters/hermes/README.md) distinguish separate SOUL loader evidence | No reliable release identity beyond recorded sources; package-manager launcher equivalence, complete runtime profile sufficiency, inference and enforcement unverified. Environment/auth/session/private-memory files are excluded |
| agy | 1.4.2 Darwin x64, project | [v2 discovery cycles](agy-discovery-cycle-verification.md): nine observations show valid → one-byte malformed → exact repair, with cold malformed, absent and unchanged guard controls | Explicit native YAML-error reporting remains unverified; historical strict comparator stays unqualified. No in-process reload, selected-agent execution, backend-auth proof, global/plugin loading or runtime enforcement |

“Verified” here denotes only the stated scope and observation. It does not
upgrade untested fields, extra scopes, original installed versions or platforms.
Hermes is source-identified rather than a reliably versioned release; agy uses a
separately reviewed observable-discovery contract rather than claiming an error
message the native client never showed. These qualifications remain part of the
acceptance rationale.

## Static mapping boundaries

OpenCode maps identity, purpose, model, sampling values and body. Claude, Cursor
and Gemini map identity/purpose/model/body subsets. Codex also maps reasoning
effort and explicit registration. OpenClaw maps a bounded roster/workspace, and
Hermes a bounded config/profile/SOUL projection. agy maps native identity,
description, model and instructions. Per-adapter READMEs and field descriptors
remain authoritative; omitted models/effort are not resolved by running a host.

Unknown values are preserved semantically with original bytes/hashes for
provenance. Native tool/MCP/plugin/skill/hooks values are not silently converted
into canonical capability references or considered enforceable. Required controls
without a supported native mapping or available enforcement owner block. Native
names/metadata need not already satisfy the stricter canonical Agent contract;
imports are incomplete drafts for review, not ready-to-run canonical objects.

Explicit directory inputs require `aok-native-input.json`. A loose file is
captured by basename only. Adapters requiring project-relative paths, including
Cursor, require an inventory preserving that path; putting a loose file physically
inside `.cursor/agents` does not change this capture rule. OpenCode accepts an
explicit loose Markdown file as well as the project layout and normalizes its
export path. Import/export writes only to explicit new output directories and
never installs configurations or executes imported commands.

## Fixes from this audit

1. **Inferred Cursor formats:** the adapter accepted `.mdc`/`.markdown` while the
   input loader inferred `unknown`. Inventory entries omitting `format` failed
   decoding. The loader now infers Markdown case-insensitively for all three
   extensions. Native path constraints remain unchanged. A failing unit regression
   reproduced this before the fix; positive CLI inventory and negative loose-file
   tests cover the actual boundary.
2. **Untraceable evidence metadata:** Gemini carried prose instead of an artifact
   reference; OpenClaw appended prose to a path. Both descriptors now reference
   actual evidence files. Shared conformance resolves every declared evidence
   reference to a nonempty repository artifact. This is a traceability check, not
   re-verification of native loading.
3. **E2E scope/provenance gaps:** existing built-binary checks covered eight
   adapters but only Codex's unverified standalone scope. Add registered Codex and
   unverified Claude plugin cases, scope-specific host/runtime assertions,
   sidecar re-import/loss and import/export drift failures. All run with empty PATH.
4. **Status/documentation drift:** remove the stale “two gates open” summary;
   distinguish seven-target pack/install smoke status from eight-target native
   agent projections. Old agy probe conclusions remain historical. Clarify that
   native loading, static semantic round trips, pack installation and runtime
   policy have different evidence.

No native adapter gains a new host-loading scope or runtime capability in this
change. Parser/input format inference is the only production behavior fix;
Gemini/OpenClaw metadata changes improve evidence traceability.

## Rerun evidence

See [audit.json](evidence/native-adapters/completion-audit-2026-10-18/audit.json)
for exact commands, source hashes, retained host-artifact hashes and log hashes.
The [E2E event log](evidence/native-adapters/completion-audit-2026-10-18/end-to-end.jsonl)
records the built binary SHA-256 and each target/scope/operation result.
[Go](evidence/native-adapters/completion-audit-2026-10-18/go-tests.log) and
[Python](evidence/native-adapters/completion-audit-2026-10-18/python-tests.log)
logs cover the current regression runs; Python includes the saved agy evidence
integrity replay, not fresh authentication.

The original native minimum-scope gate is qualified with the limits above.
Outstanding native scopes and runtime milestones remain explicit work, and no
passing parser/test/CI result grants authorization or certifies runtime policy.
