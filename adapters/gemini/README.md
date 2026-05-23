# Gemini Adapter

Status: active.

The Gemini adapter renders AOK content into Gemini-friendly contracts for use in
multi-agent workflows.

## Render Shape

- Task packets render as structured work specs.
- Roles render as instruction-level policy contracts.
- Skills and hooks render as reusable execution guidance.
- Bundle metadata is emitted as a generated import bundle in
  `targetRender.gemini.payload`; host runtime invocation remains external.

## Supported Commands

- `aok render bundle <pack> --target gemini`

## Known Gaps

- Runtime invocation remains host-defined; AOK emits portable JSON contracts.
