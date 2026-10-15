# Adapter-native parsing, adoption and rendering

Status: proposed adapter contract. This document does not implement import/export.
AOK's neutral source of truth must remain compatible with tool-specific definitions.
Support both directions; do not ask users to manually rewrite an existing native agent.

```text
Native file / scoped host config
            |
      parse + inspect         (no execution or installation)
            |
Neutral candidate + extensions + source locations + AdapterReport
            |
      explicit adoption       (resolve conflicts and choose ownership)
            |
Canonical Agent / profiles / referenced capability and output contracts
            |
      resolve + validate
            |
Native agent + related scoped settings + launch plan + AOK sidecar
            |
      separate installation / external execution
```

## Required adapter operations

| Operation | Contract |
|---|---|
| detect | Identify client, format, scope and supported version; ambiguous detection requires selection |
| parse | Read native files into typed fields and preserve opaque unsupported fields; no executable evaluation |
| normalize | Map IDs, effort/variant settings, permissions and dependencies with source provenance |
| inspect | Discover inherited host config and available capability/model metadata without inference |
| adopt | Preview canonical additions and conflicts; write only authorized desired-state files |
| render | Produce native files and associated config/launch arguments from a resolved manifest |
| compare | Detect native drift, inherited changes and non-roundtrippable values |
| verify | Parse generated files, compare semantics and eventually run isolated host discovery tests |

These are operation names, not commands currently present in `scripts/aok`.

## Native target strategy

| Target | Existing AOK capability | Proposed agent projection / parsing scope |
|---|---|---|
| OpenCode | Generated bundle; no automatic config rewrite | Native agent Markdown and `agent` config objects; model/permission mapping and sidecar for outputs |
| Claude Code | Compatibility marketplace and curated native assets | Project/user/plugin/session subagent definitions, with scope-specific fields and separate plugin dependencies |
| Codex | Existing plugin/setup/render support | Native agent configuration through a versioned adapter after fresh format verification; never assume Claude frontmatter applies |
| Cursor | Generated contracts and existing adapter assets | Versioned native agent/rule/skill surfaces where verified; unsupported controls stay diagnosed |
| Gemini | Existing target adapter | Verify host-specific definitions/settings; distinguish from Antigravity CLI |
| OpenClaw / Hermes Agent | Existing generated target contracts | Inspect respective native formats and compatibility before advertising agent import |
| Antigravity CLI (`agy`) | Model research only; no target in current adapter schema enum | New adapter project; initially model/effort launch mapping and neutral guidance unless agent-native format is verified |

Target presence in AOK does not establish support for this new Agent kind. All agent
import/export support is proposed. Each adapter must publish a format/version/scope
matrix and native validation evidence; unknown versions cannot inherit a tested badge.

## Verified source basis and illustrative fixtures

Checked 2026-10-15:

- [OpenCode agents](https://opencode.ai/docs/agents/): agent JSON config or Markdown
  in `.opencode/agents/`; frontmatter supports description, mode, model and permission.
  Filename supplies identity. [Fixture](examples/native/repository-explorer.opencode.md)
  uses this shape; native loading was not tested.
- [Claude Code subagents](https://code.claude.com/docs/en/sub-agents): Markdown
  frontmatter plus instructions, including name, description, model and tools.
  Project/user and plugin scopes differ: plugin agents ignore hooks, mcpServers and
  permissionMode. [Fixture](examples/native/repository-explorer.claude.md) is a
  project-scope shape, not a plugin installation.
- [Saved agy inventory/help](../../research/agy-model-selection.md) verifies model
  IDs and CLI flags, not a native agent-file format.
- [Existing configuration comparison](../../research/agent-tool-config-comparison.md)
  is prior research, not current adapter certification. Its older OpenCode path
  variants do not supersede the checked source above.

Native fixtures demonstrate a limited read-only instruction projection. They are not
complete equivalents of the neutral report-writing assignment. The accompanying
[AdapterReport](examples/adapter-report.json) is blocked because required write-scope
and other controls are not proven. The neutral contract must not become “ready” simply
because native syntax can be rendered. No full third-party documentation is vendored.

## Field mapping and loss semantics

| Neutral content | Possible native mapping | Required handling |
|---|---|---|
| Identity/description | Native name/filename/description | Preserve stable AOK ID in sidecar; report identifier normalization/collisions |
| Roles/instructions | Prompt body or referenced instruction file | Record ordered sources and hashes; prose projection is advisory |
| Model/provider | Native model field, connection config or launch flag | Exact discovered ID; never fabricate an API identifier from a label |
| Reasoning | Variant, native effort/budget key or CLI flag | Validate model+client combination; reject unsupported explicit settings |
| Tools | Native allow/deny policy | Translate names using registered bindings; omissions must not inherit broad access |
| MCP | Scoped native connection/tool policy | Record scope and authentication reference; wider session exposure blocks export unless independently constrained |
| Plugin/skill | Native dependency registration/preload or guidance reference | Installation, discovery and authorization remain distinct |
| Environment/limits | Native controls or executor-owned enforcement | Every mandatory limit needs enforcement owner; no advisory downgrade |
| Output contract | Native structured-output feature where supported, otherwise sidecar + executor validator | Prompt template alone does not enforce schema or evidence quality |
| Assignment/lifecycle | Native session prompt plus runtime reference | Preserve task status/phase authority in existing task packet/runtime |

AdapterReport field states: exact, transformed, guidance-only, unsupported,
preserved-extension. Enforcement states: native, executor, advisory, none. Overall
status is compatible, lossy, or blocked. Any missing required control is blocked;
lossy may describe optional presentation loss but never authorize weaker permissions.
Required losses cannot be “accepted” into execution by a flag; change the contract
explicitly or supply verified enforcement and resolve again.

## Round trip and adoption rules

Preserve unknown native fields as adapter-namespaced opaque data with original source
locations, content hashes and host version. A sidecar stores neutral references and
fields without native equivalents; it contains no secrets. Unknown fields are not
executed or injected into prompts. Adapter extension validators must explicitly claim
executable fields. Preserve comments/format using a concrete syntax tree when possible;
otherwise render a separate file and report formatting/comment loss.

Import/export equality means equivalent supported semantics, not byte identity. Test
native → neutral candidate → native and neutral → native + sidecar → neutral. A native-only
re-import without the sidecar must report missing output/permission/dependency facts,
not reconstruct them from prose. Conflicts between sidecar and native settings block.
Existing host files are not overwritten automatically. Adoption previews exactly which
files become AOK-owned; imports outside that set remain user-owned snapshots.

Plugins may move configuration into a wider scope. Never translate an agent-scoped MCP
or permission into plugin/session settings without detecting the scope change. Where
scope changes are necessary, isolate the host/session and verify equivalent effective
access first. Credentials are redacted to references; importing literal credentials
reports their presence without echoing or copying values into research/config output.

## Adapter conformance contract (proposed addition)

[AdapterConformance schema](schemas/adapter-conformance.schema.json) and
[draft example](examples/adapter-conformance.json) describe a single adapter/version,
host-version range, native format set and scope. A project-scope result never certifies
plugin/user/session behavior. The example is draft with all tests not-run.

Each field is classified independently:

| Support | Meaning | Acceptance rule |
|---|---|---|
| native | Host represents and enforces the relevant semantics | Require scope/version-specific evidence |
| executor-backed | Host projection relies on an external executor | Require executor/version evidence and an actual binding in the resolved run |
| guidance-only | Prompt/text expresses intent | Allowed only for advisory behavior; never satisfies required security/limit controls |
| unsupported | No verified representation/enforcement path | Required field blocks resolution |

For descriptive identity/prompt fields, native support means representation/loading;
for security, budget and lifecycle fields it additionally means enforcement. These
must not be conflated. AdapterReport mapping states describe transformations; conformance
support describes effective implementation. `preserved-extension` is preservation,
not evidence of execution support. A compatible report alone does not certify a host.

Conformance requires evidence for syntax/discovery, both round-trip directions, scope
inheritance, actual denied-action enforcement, drift detection and credential redaction.
Syntax must use the host's parser where available; a Markdown parser alone does not
certify host loading. Compare permission semantics, not just output bytes. Tests use
scratch workspaces/config homes and no real remote mutations. Plugin/session-wide MCP
or permission expansion is a required negative case. Model/effort mismatch, unknown
native keys, removed aliases and missing sidecars are required negative fixtures.

Only mark verified when all required field mappings and test categories pass for the
recorded scope/version, with reproducible evidence. Unknown or newly incompatible host
versions need reinspection; failed mandatory enforcement blocks execution even if prior
certification exists. Version drift can expire certification; it never silently expands
a host-version range. Every adapter operation must preserve user-owned files and avoid
executing imported commands. Review adoption and installation plans separately.
