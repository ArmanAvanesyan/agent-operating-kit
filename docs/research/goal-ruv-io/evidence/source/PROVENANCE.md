# Upstream source provenance

Repository: https://github.com/ruvnet/ruflo

Commit: 1882006a02487590c8c66a7eec8d7504f6af33c3

Copied directories: v3/goal_ui/src and v3/goal_ui/supabase.

The public Supabase publishable key literal in src/integrations/supabase/client.ts is redacted. License is retained as LICENSE. No copied source is installed or executed as AOK functionality.

## Trace index

| Concern | Source snapshot |
|---|---|
| Research fields, defaults, draft initialization, preset merge, list handling | src/components/ReviseResearchForm.tsx |
| Research opening, submission, category merge, plan and execution | src/pages/Index.tsx |
| Objective input, category mapping and goal generation | src/components/GoalInput.tsx |
| Planner state/search and goal parser | src/lib/goapPlanner.ts |
| State display and config display | src/components/StateAssessmentCard.tsx; src/components/GOAPConfigDisplay.tsx |
| Agent fields, defaults, preset deltas, persistence | src/components/agents/AdvancedSettingsModal.tsx |
| Agent settings mounting without executor callback | src/pages/Agents.tsx |
| Separate widget enableAI and aiModel controls | src/components/WidgetCustomizer.tsx |
| Published backend prompt and output schema | supabase/functions/research-step/index.ts |
| Published optimizer intent strings and output schema | supabase/functions/optimize-research-config/index.ts |
| Public API path, distinct from the two UI controls | supabase/functions/research-api/index.ts |

Deployed JavaScript is a different build. In ../deployed/initial-index-r7I6ZcN4.pretty.js, useful anchors are `const v0` (research form), `const sC` (state display), `const Oc` (research defaults), `function k$` (agent settings), `agenticflow-settings` (persistence), `const fr` (disabled persistence flag), and `async function Xi` (Cloud Run request adapter). Backend implementation cannot be reconstructed from that request adapter alone.
