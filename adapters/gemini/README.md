# Gemini Adapter

Status: active.  
Support level: `generated-bundle`.  
Host version: untracked (see `docs/host-compatibility.md`).

The Gemini adapter renders AOK content into Gemini-friendly contracts for use in
multi-agent workflows. Delivery is a **generated import bundle** only.

## Generated-bundle import expectations

```bash
./scripts/aok render bundle <pack> --target gemini
# → build/bundles/gemini/<pack>/
```

Treat `build/bundles/gemini/<pack>/` (or the release bundles zip path) as
reusable role, skill, hook, and task guidance:

- `aok-bundle.json` — canonical contract
- markdown + per-item JSON — copy or adapt into Gemini CLI/app prompts or
  project docs as your host expects

AOK does not invoke Gemini runtimes or install a Gemini-native package.

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
