# Antigravity CLI (agy) model selection

Checked 2026-10-15. Requested subset; companion data: [JSON](agy-model-selection.json).

| Requested option | Verified CLI model ID | Label effort |
|---|---|---|
| Gemini 3.8 Flash (High) | `gemini-3.8-flash-high` | high |
| Gemini 3.8 Flash (Medium) | `gemini-3.8-flash-medium` | medium |
| Gemini 3.8 Flash (Low) | `gemini-3.8-flash-low` | low |
| Claude Opus 4.6 (Thinking) | `claude-opus-4-6-thinking` | unspecified |
| Claude Sonnet 4.6 (Thinking) | `claude-sonnet-4-6` | unspecified |
| GPT-OSS 120B (medium) | `gpt-oss-120b-medium` | medium |

All six IDs and names appeared in live `agy models`:
[stdout](model-selection-evidence/2026-10-15/agy-models.stdout.txt),
[stderr](model-selection-evidence/2026-10-15/agy-models.stderr.txt).
The CLI capitalizes GPT-OSS's label as `(Medium)`; the requested spelling is retained above.
The Sonnet selector intentionally has no `-thinking` suffix.

[Google's model catalog](https://antigravity.google/docs/models) lists the families,
plan-dependent availability, and scheduled removal of Claude 4.6 and GPT-OSS 120B
on **2026-11-02**. These entries are a dated inventory, not durable defaults.
The catalog says model changes persist within a conversation and take effect after
an active turn completes or is cancelled. This behavior was not interactively replayed.

[CLI help](model-selection-evidence/2026-10-15/agy-help.stderr.txt) confirms
`--model` and `--effort`. The [official effort reference](https://antigravity.google/docs/cli/commands/effort/)
says effort availability depends on the model; `--effort` overrides only the current
invocation/session, while interactive confirmation saves a default. “Thinking” is not
an effort tier. Do not assume every listed model supports every global effort value.

## Proposed evaluation

Compare the three Gemini variants on the same tasks before selecting an effort default.
Use the Claude and GPT-OSS entries as comparison candidates while available, with a
replacement evaluation before their removal. No cross-provider ranking, benchmark,
quota, inference success, or account entitlement was established by listing models.

This is research only; no AOK target support, routing, or defaults were added.
See the [research index](model-selection.md).
