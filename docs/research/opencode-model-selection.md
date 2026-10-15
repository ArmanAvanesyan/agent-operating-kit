# OpenCode model selection

Checked 2026-10-15. Requested subset; companion data: [JSON](opencode-model-selection.json).

| Requested option | Verified OpenCode selector |
|---|---|
| DeepSeek V4.1 Flash Free | `opencode/deepseek-v4.1-flash-free` |
| Nemotron 3 Ultra Free | `opencode/nemotron-3-ultra-free` |
| Nemotron 3.5 Lightning Free | `opencode/nemotron-3.5-lightning-free` |
| Nemotron 3.5 Super VL Free | `opencode/nemotron-3.5-super-vl-free` |
| LongCat 2.5 Preview Free | `opencode/longcat-2.5-preview-free` |

All selectors appeared in the installed CLI's `opencode models opencode` output:
[stdout](model-selection-evidence/2026-10-15/opencode-models.stdout.txt),
[stderr](model-selection-evidence/2026-10-15/opencode-models.stderr.txt).
Listing does not demonstrate successful inference, account quota, or tool-call quality.

The [official catalog](https://opencode.ai/v2/docs/console/models/) confirms these
free offers and the `opencode/<model-id>` selection format. Free offers can change;
Nemotron free endpoints have trial/data-use conditions, including restrictions on
personal or confidential data. Review the current catalog before using repository content.
No permanent zero-price guarantee, effort setting, or capability ranking is inferred.

## Proposed evaluation

Use each as a candidate for a fixed repository exploration, documentation, bug-fix,
and test-generation packet. Compare correctness, tool-call failures, elapsed time,
retries, and cost per accepted result. Include image input only in a separate vision
packet; the “VL” label alone does not establish usable vision in this client.
These are proposed experiments, not measured recommendations.

This is research data only. AOK defaults, adapters, and execution behavior are unchanged.
See the [research index](model-selection.md) for the relationship to existing exports.
