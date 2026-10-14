# Hermes Agent Adapter

Status: active.  
Support level: `generated-bundle`.  
Host version: untracked (see `docs/host-compatibility.md`).

The Hermes Agent adapter renders AOK roles, task packets, hooks, and skills into
Hermes-oriented contracts as a **generated import bundle**.

## Generated-bundle import expectations

```bash
./scripts/aok render bundle <pack> --target hermes-agent
# → build/bundles/hermes-agent/<pack>/
```

Import assignment, role, skill, and lifecycle files from the bundle (or release
bundles zip) into the host workflow. Keep `aok-bundle.json` if you need a
parseable record. Runtime event wiring is not installed by AOK; connect MCP or
host hooks manually using the rendered contracts.

## Render Shape

- Task packets render as structured assignment specs.
- Roles and hooks render with escalation and execution guidance.
- MCP config references support external runtime hooks on the pack render.
- Bundle metadata is emitted under `targetRender.hermes-agent` / `aok-bundle.json`.

## Supported Commands

- `aok render pack <pack> --target hermes-agent`
- `aok render bundle <pack> --target hermes-agent`

## Known Gaps

- Runtime-specific event wiring is documented and manually connected by host flows.
- Host Hermes version untracked until recorded in `docs/targets-support-matrix.md`.
