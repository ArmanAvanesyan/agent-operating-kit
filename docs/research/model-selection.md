# Model selection research index

Reviewed 2026-10-15. The existing exports distinguish official guidance, source
selector data, and repository-derived recommendations. They have different evidence
bases and must not be combined into a single measured ranking.

| Surface | Research | Data basis |
|---|---|---|
| Codex | [Selector export](codex-model-selector.md), [JSON](codex-model-selector.json), [raw data](codex-model-selector-raw.json) | Published selector with task-specific benchmark references and editorial choices |
| Claude | [Selection guidance](claude-model-selection.md), [JSON](claude-model-selection.json) | Published guidance plus explicitly derived task mapping |
| OpenCode | [Requested free models](opencode-model-selection.md), [JSON](opencode-model-selection.json) | Official catalog and live installed CLI listing |
| Antigravity CLI (`agy`) | [Requested models/variants](agy-model-selection.md), [JSON](agy-model-selection.json) | Official documentation and live CLI listing |

The new inventories add five OpenCode options and six agy variants. Existing Codex
and Claude snapshots were inspected for structure and provenance, not comprehensively
revalidated or rewritten. Model labels, provider IDs, reasoning settings, price offers,
and measured capability are separate facts. A listing establishes discoverability,
not task suitability or a successful inference request.

Existing capability: research exports and local CLI discovery evidence.
Proposed addition: evaluate these candidates against shared repository task packets,
then adopt routing only where results justify it. No runtime model registry, adapter,
selection policy, or supported-target declaration changes in this update.

Evidence was collected using read-only `opencode models opencode`, `agy models`,
and `agy --help`; no inference prompt was sent. Outputs, including stderr, are in
[the dated evidence directory](model-selection-evidence/2026-10-15/).
