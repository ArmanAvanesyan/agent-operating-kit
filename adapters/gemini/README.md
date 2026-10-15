# Gemini Adapter

Status: active.  
Support level: `generated-bundle`.  
Host version: untracked (see `docs/host-compatibility.md`).

**Ops guide (SoT for import steps):** [`docs/adapters/gemini.md`](../../docs/adapters/gemini.md).

The Gemini adapter renders AOK content into Gemini-friendly contracts for use in
multi-agent workflows. Delivery is a **generated import bundle** only.

## Prerequisites

- Gemini CLI or app that can take project/context files
- Kit checkout or release `*-bundles.zip`

## Generate or download

```bash
./scripts/aok render bundle <pack> --target gemini
# → build/bundles/gemini/<pack>/
```

Release zip: `bundles/gemini/<pack>/`.

## Import steps (summary)

1. Obtain `bundles/gemini/<pack>/`.
2. Load `roles.md` / `skills.md` / `tasks.md` into Gemini context.
3. Use `hooks.md` as operator checklists (`renders` includes `hook` to match
   emitted bundle files).
4. AOK does not invoke Gemini runtimes or install a Gemini-native package.

Full steps, verification, and failure modes:
[`docs/adapters/gemini.md`](../../docs/adapters/gemini.md).

## Render Shape

- Task packets render as structured work specs.
- Roles render as instruction-level policy contracts.
- Skills and hooks render as reusable execution guidance.
- Bundle metadata is emitted under `targetRender.gemini` / `aok-bundle.json`.

## Supported Commands

- `aok render pack <pack> --target gemini`
- `aok render bundle <pack> --target gemini`

## Known Gaps

- Runtime invocation remains host-defined; AOK emits portable JSON/markdown.
- No native Gemini package; host version untracked.
