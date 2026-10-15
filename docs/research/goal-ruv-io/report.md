# Goal ruv io configuration logic and browser verification

This report extracts both Advanced configuration controls on https://goal.ruv.io and https://goal.ruv.io/agents. It separates observable browser behavior, deployed frontend implementation, and published backend implementation. The research settings partially affect AI requests; agent settings are persisted without being wired to execution.

The exact default objects, including complete prompt strings, are in [defaults.json](defaults.json). The original supplied Claude session is retained as [source material](evidence/claude-session.txt). Its assertions are not treated as verified evidence.

## Evidence and scope

- Browser verification: isolated Chrome profile through Playwright, against the live public URLs. No user browser profile or saved settings are modified.
- Original source snapshot: ruvnet/ruflo commit `1882006a02487590c8c66a7eec8d7504f6af33c3`, subtree `v3/goal_ui`. Copied source is under [evidence/source](evidence/source/), with its upstream license.
- Initial deployed frontend: [index HTML](evidence/deployed/initial-index.html), [JavaScript](evidence/deployed/initial-index-r7I6ZcN4.js), and [formatted JavaScript](evidence/deployed/initial-index-r7I6ZcN4.pretty.js).
- Browser artifacts: [step results](evidence/browser/steps.json), [request and response bodies](evidence/browser/network.json), numbered accessibility snapshots and field inventories under [evidence/browser](evidence/browser/), and corresponding [screenshots](evidence/screenshots/).
- Dates, hashes, URLs and sanitization details are in [manifest.json](manifest.json).
- The bundled backend source is the public Supabase function implementation. The live frontend calls Cloud Run instead. Browser-observed responses are reported separately; private Cloud Run internals cannot be established from frontend source alone.

The native browser connector failed to initialize, including after reset. Playwright uses real installed Chrome to inspect and interact with the live site. The automation script is [verify.cjs](evidence/browser/verify.cjs); replay instructions are in [README.md](README.md).

## Control mapping

| URL | User supplied XPath | Expected control and component |
|---|---|---|
| `/` | `//*[@id="root"]/div[2]/div[2]/div/div/div/button/span` | Advanced → Advanced Research Settings; `GoalInput` opens `ReviseResearchForm` |
| `/agents` | `//*[@id="root"]/div[2]/div/div[2]/div[1]/div/button` | Advanced Settings → Advanced Agent Configuration; `AdvancedSettingsModal` |

The research form is also reused by the post-report Revise Research dialog. These entry paths have different initialization and submit behavior.

## Research configuration

The form has Guidance, GOAP, Prompts, Parameters, Actions, and Filters tabs. Guidance is initially active. There are 28 editable configuration leaves plus three state-definition members without editors. The complete field inventory follows. Defaults apply before any category optimization or saved in-memory changes.

### Guidance

| Field | Default | UI domain | Consumer |
|---|---|---|---|
| `goal` | empty string | Unrestricted textarea; no nonempty submit check in this dialog | Updates parent goal if truthy and different; Advanced submit does not launch a plan |
| `researchGuidance.focusAreas` | `[]` | Free-text list | Added to system prompt |
| `researchGuidance.excludeTopics` | `[]` | Free-text list | Added to system prompt |
| `researchGuidance.depth` | `moderate` | `surface`, `moderate`, `deep` | Published backend requests 2–3, 4–5, or 7–10 sentences per finding |
| `researchGuidance.perspective` | `technical` | `technical`, `business`, `academic`, `practical` | Appended perspective instruction |
| `researchGuidance.timeframe` | `recent` | `recent`, `current-year`, `past-year`, `past-2-years`, `all-time` | Sent but unused by published research-step function |

List Add and Enter trim nonempty input, append, then clear the input. Removal is by index. No duplicate prevention, count cap, domain validation, or conflict resolution is implemented. Input that has not been added is not part of the submitted list.

The main objective textarea has its own local `goal` state, separate from parent `userGoal` and `researchConfig.goal`. Advanced initialization prefers `initialConfig` over `currentGoal`, so these three goal values can diverge. Applying a goal from Advanced does not populate the main objective textarea.

### GOAP

| Field | Default | UI domain | Consumer |
|---|---|---|---|
| `goapConfig.executionMode` | `closed` | `focused`, `closed`, `open` | Displayed action metric and config card; not a different planner |
| `goapConfig.enableReplanning` | `true` | Boolean | Checks whether to notify on a returned AI error |
| `goapConfig.replanningTriggers` | Action failure, Low confidence results, Missing preconditions | Independent switches; also Timeout exceeded and State mismatch | Only Action failure is tested; no planner reinvocation |
| `goapConfig.costOptimization` | `true` | Boolean | Display and logging |
| `goapConfig.parallelExecution` | `true` | Boolean | Display and logging; loop remains sequential |

Replanning triggers remain selectable when replanning is off. Off does not clear them.

### Prompts

| Field | Default location | Consumer |
|---|---|---|
| `prompts.systemPrompt` | `defaults.json` research prompts | Truthy custom prompt replaces published backend default; guidance modifiers are then appended |
| `prompts.searchQueryTemplate` | `defaults.json` research prompts | Sent but unused; advertised `{topic}`, `{year}`, `{keywords}` variables are not substituted in this path |
| `prompts.analysisPrompt` | `defaults.json` research prompts | Sent but unused |
| `prompts.synthesisPrompt` | `defaults.json` research prompts | Sent but unused |

All four are unrestricted textareas. Empty system prompt uses the backend fallback; whitespace-only text remains truthy. Exact multiline default values are retained in defaults.json rather than shortened here.

### Parameters

| Field | Default | Slider min–max, step | Consumer |
|---|---:|---|---|
| `parameters.maxSources` | 15 | 5–50, 5 | Prompt asks for up to N findings per step; no distinct-source cap or output truncation |
| `parameters.minConfidence` | 85 | 50–99, 5 | Prompt threshold and function schema minimum divided by 100 |
| `parameters.maxSteps` | 7 | 3–15, 1 | Displayed metrics; does not truncate the seven UI research actions |
| `parameters.parallelAgents` | 3 | 1–10, 1 | Sent but unused |
| `parameters.timeout` | 120 seconds | 30–300, 30 | Sent but unused; no fetch deadline/cancellation |

The published function sets confidence maximum to 0.95. A minimum above 95% creates inconsistent schema bounds; UI does not resolve this. The slider's declared maximum is 99 even though its step grid is five. Generated preset values need not fit the UI step grid.

### Actions

| Field | Default | UI domain | Consumer |
|---|---|---|---|
| `actionConfig.maxActionCost` | 5 | Slider 1–10, step 1 | Display card only |
| `actionConfig.enableFallbacks` | `true` | Boolean | No execution consumer |
| `actionConfig.validatePreconditions` | `true` | Boolean | No execution consumer; planner independently checks fixed preconditions |
| `actionConfig.trackEffects` | `true` | Boolean | No execution consumer; UI updates state regardless |

### Filters

| Field | Default | UI domain | Consumer |
|---|---|---|---|
| `filters.dateRange` | `past-year` | `past-week`, `past-month`, `past-3-months`, `past-6-months`, `past-year`, `past-2-years`, `all` | Literal date-range instruction in published backend prompt |
| `filters.sourceTypes` | academic, technical, industry | Independent academic, technical, industry, news, blogs, documentation switches | Prompt preference |
| `filters.excludeDomains` | `[]` | Free-text Add/Enter/remove list | Prompt prohibition; no domain normalization/filter enforcement |
| `filters.languages` | en | Independent en, es, fr, de, zh, ja switches | Sent but unused |

All source types/languages may be switched off. Empty source types invoke the backend's generic source guidance.

### State definition without editors

```json
{
  "currentState": {"goalDefined": true, "informationGathered": false},
  "goalState": {"verified": true, "insightsGenerated": true},
  "stateGaps": [
    "Information needs to be gathered",
    "Analysis required",
    "Insights need generation"
  ]
}
```

These initialize/populate the state assessment card; actual planning uses separately hard-coded eight-flag states. Display state is advanced by loop index before each AI request, rather than by validated results. The published source card counts all true current-state entries divided by target-state key count and can exceed 100%. The deployed card differs: it counts target entries whose current value matches, divides by at least one target entry, and clamps to 100%. This display still advances by step index rather than validated research results.

## Research presets and combinations

Research presets call an AI optimizer; they do not contain deterministic numeric configurations. Each sends `{preset, currentGoal}`. Repeated selections may return different settings. Published optimizer uses `google/gemini-2.5-flash` and forced `generate_optimized_config` output.

| Label | ID | Published intent |
|---|---|---|
| Academic Research | academic-deep | Maximum rigor, peer review, 90%+ confidence, cross-referencing, methodology/citations/reproducibility, seminal work |
| Industry Quick Scan | industry-quick | Speed, actionable business/market/industry insights, 75%+ confidence, surface/moderate, applications/ROI/trends, 6–12 months |
| Competitive Intel | competitive-analysis | Competitors, reports/news/filings/social media, positioning/strategies/strengths/weaknesses, moderate/deep, business strategy, parallel competitor research |
| Technical Study | technical-feasibility | Engineering, papers/docs/GitHub, deep implementation, architecture/performance/limitations/trade-offs, 85%+ confidence |
| Market Trends | market-trends | Trends/predictions, market/financial/industry data, growth/opportunities/disruption, moderate/broad, recent with historical context |
| Medical/Clinical | medical-clinical | Journals/trials/PubMed, 90%+ confidence, deep safety/efficacy/evidence/outcomes, exclude non-peer-reviewed sources |
| Startup Validation | startup-validation | Market size/competition/customer needs, reports/surveys, practical business, gaps/validation/go-to-market, moderate/broad, cost-effective parallel research |
| Policy & Regulatory | policy-regulatory | Government/legal/policy sources, accuracy/recency, compliance/frameworks/trends, deep risk assessment, academic/legal, official over opinion |

Exact intent strings and fallback goals are preserved in `evidence/source/supabase/functions/optimize-research-config/index.ts`.

### Optimizer output contract

| Section | Required | Optional and restrictions |
|---|---|---|
| researchGuidance | depth, perspective, timeframe | focusAreas and excludeTopics; depth enum surface/moderate/deep; other strings unrestricted |
| prompts | Section optional | systemPrompt |
| parameters | maxSources, minConfidence, maxSteps | sources 5–25; confidence 70–95; steps 5–10; optional agents 1–5 and timeout 60–300; number, not integer |
| filters | dateRange, sourceTypes | excludeDomains; strings unrestricted |
| goapConfig | executionMode, enableReplanning | optional costOptimization and parallelExecution; mode focused/closed/open |

All sections other than prompts are required by the top-level tool schema. Focus counts of 2–4 and exclusion counts of 0–3 are natural-language descriptions, not `minItems`/`maxItems`. The top level forbids additional properties, but nested objects lack equivalent strictness. The function JSON-parses and forwards returned tool arguments without independent schema validation.

### Merge rules

For each of researchGuidance, prompts, parameters, filters, and goapConfig:

```javascript
next.section = { ...current.section, ...generated.section };
```

The rest of current config is retained. Returned fields win; omitted fields survive; arrays replace rather than concatenate. Goal, stateDefinition and actionConfig are retained. There is no selected-preset ID in ResearchConfig and no compatibility matrix. Manual edits after optimization win until another preset overwrites them.

Preset buttons are disabled while a request is pending, but other edits, Cancel and Submit remain available. The response merges against the config captured when the request began; intervening edits can be lost. There is no abort on close. A missing config response silently leaves values unchanged; returned errors produce Optimization Failed. These race/malformed-response cases are source-derived unless listed as browser tests.

### Category combinations

GoalInput calls goal generation and optimizer in parallel. Finance maps to market-trends; Business to startup-validation; Marketing to competitive-analysis; Medical to medical-clinical; Education to academic-deep; Coding/Technical/AI & ML to technical-feasibility. The optimizer gets an empty goal, not the concurrently generated goal. Only the first returned goal populates the main textarea. Parent configuration uses the same per-section merge, with functional state updates. An optimizer error does not invalidate successful goal generation, so the combined success message can overstate optimization success.

### Unresolved combinations accepted by the form

Focus and exclude the same topic: both instructions are emitted. Timeframe all-time plus dateRange past-week: only dateRange is consumed by published backend. Replanning off with selected triggers: values retained, notification disabled. Parallel execution on with one agent: accepted, execution sequential. Maximum action cost below fixed costs: no filtering. No source types: generic instruction. Confidence above 95%: conflicting schema bounds. No validation reconciles these combinations.

## Research persistence and execution

Advanced opens a draft from parent researchConfig. Editing/presets change the draft. Cancel, close, Escape or outside dismissal discard the draft on unmount. Submit is labeled Start Revised Research, but the Advanced callback only commits settings, closes, optionally changes parent goal, and displays Advanced Settings Applied. Reopen retains applied config. Main reset restores defaults and clears run state.

The deployed bundle includes optional goal/widget/research config persistence under a flag compiled to false. Consequently reload/route remount resets these settings in this build. Execution separately writes local trajectory history using an IndexedDB-backed store; this does not restore the Advanced config. Trajectory includes goal and config hashes, goal text, per-step findings, timestamps and report metadata. Its config hash uses goapConfig, researchGuidance and parameters, omitting prompts and filters; presetId is populated from depth rather than a true preset ID. The report recommendation count can capture the render's previous recommendations because state updates are asynchronous.

The separate post-report Revise dialog does not receive initialConfig and starts from defaults plus currentGoal. Its handler sets researchConfig then immediately calls a planning function captured from the previous render. In deployed code, automatically scheduled execution also captures that old config. This is a source-derived stale-closure risk, not a claim of a browser-reproduced revision run.

### Fixed planner

| Sequence | Action | Cost |
|---:|---|---:|
| 1 | Goal Analysis / analyzeGoal | 1 |
| 2 | State Assessment / assessState | 1 |
| 3 | Web Search / gatherInformation | 2 |
| 4 | Document Analysis / analyzeDocuments | 2 |
| 5 | Knowledge Synthesis / synthesizeKnowledge | 2 |
| 6 | Insight Generation / generateInsights | 2 |
| 7 | Verification / verify | 1 |

Total fixed cost is 11. Planner A* orders by accumulated cost plus unmet truthy goal conditions. It checks truthy preconditions and merges effects. The fixed dependency chain yields the same seven actions; earlier actions may remain applicable, so “exactly one applicable action at every state” is inaccurate. It does not switch algorithm, heuristic, action library or goal state from either configuration UI.

Goal parsing selects a domain through successive substring rules: technology (quantum/computing), business (market/business), software engineering (architecture/software), energy (energy/renewable), with later matches winning; default general. Verb similarly resolves research → analyze → investigate → compare → evaluate, later matching rules winning. Keywords are the first five whitespace-separated words longer than four characters. This changes descriptions, not plan structure.

Displayed Sub-goals is `min(max(2, ceil(goal.split(' ').length / 10)), maxSteps)`. Displayed Actions is maxSteps+3 for open, min(5,maxSteps) for focused, and maxSteps for closed. Neither controls actual plan length.

### Deployed sequence and public-source differences

Initial deployed code waits 1.5 seconds, generates the plan, and schedules execution 0.5 seconds later. Current repository instead waits for Start Research. Execution waits 4.5 seconds for intro animation, then processes each step sequentially: 0.5-second UI delay, AI request if enabled, 2-second delay, mark complete, 0.5-second delay. After seven steps it requests final recommendations and reveals final analysis after a further second.

Normal request includes goal, step title/description/type, separate widget aiModel, researchGuidance/prompts/parameters/filters, and previousStepsData. No stateDefinition, goapConfig or actionConfig is sent. The default separate widget settings are enableAI=true and aiModel=google/gemini-2.5-flash. Disabling AI skips all step/final calls and leaves illustrative step content.

Published backend assembles custom system prompt (or fallback), then depth, perspective, focus and exclusion modifiers. User prompt adds goal/current step, previous finding titles/content, finding count, sources/domains, confidence and date range. It requests Google Search grounding and structured tool output, but does not independently enforce source/date/domain restrictions or truncate findings. Returning a confidence schema minimum is not evidence that returned claims were verified.

On returned AI error, optional Action failure replanning only produces a toast; no replanner call occurs. Thrown exceptions show a connection error without this trigger check. The loop continues and marks the step completed. Original illustrative content can then become subsequent context. Final report gets goal, model and all step findings, with no config. Settings influence final results indirectly through context, not explicit reapplication. Failure generating final report is not surfaced with the same step-error notification.

The live frontend uses a Cloud Run functions endpoint; published source uses Supabase invoke and a Lovable gateway. This is a provenance boundary, not evidence of identical backend behavior.

## Agent configuration

There are 22 editable fields in four tabs. All change draft state and can be persisted. No inspected execution code consumes these fields.

| Tab | Field | Default | Domain |
|---|---|---|---|
| Swarm | swarm.topology | hierarchical | mesh, hierarchical, ring, star |
| Swarm | swarm.maxAgents | 10 | Slider 1–50, step 1 |
| Swarm | swarm.strategy | adaptive | balanced, specialized, adaptive |
| Swarm | swarm.autoScaling.enabled | true | Boolean |
| Swarm | swarm.autoScaling.minAgents | 2 | Number input, HTML min=1, parseInt |
| Swarm | swarm.autoScaling.maxAgents | 20 | Number input, HTML min=1, parseInt |
| Swarm | swarm.autoScaling.scaleUpThreshold | 80 | Slider 0–100%, step 5 |
| Swarm | swarm.autoScaling.scaleDownThreshold | 20 | Slider 0–100%, step 5 |
| GOAP | goap.algorithm | a-star | a-star, greedy, dijkstra, bfs, dfs |
| GOAP | goap.heuristic | manhattan | manhattan, euclidean, hamming, custom |
| GOAP | goap.costMethod | hybrid | uniform, time, resources, tokens, hybrid |
| GOAP | goap.optimization.enabled | true | Boolean |
| GOAP | goap.optimization.detectParallel | true | Boolean |
| GOAP | goap.optimization.removeRedundant | true | Boolean |
| Execution | execution.strategy | adaptive | sequential, parallel, hybrid, adaptive |
| Execution | execution.maxParallelTasks | 5 | Slider 1–20, step 1 |
| Execution | execution.timeout | 300000 ms | Slider 30000–900000 ms, step 30000; displayed seconds |
| Execution | execution.enableQualityGates | true | Boolean |
| Model | modelRouter.primaryProvider | anthropic | anthropic, openrouter, gemini, local |
| Model | modelRouter.strategy | balanced | cost, speed, quality, privacy, balanced |
| Model | modelRouter.maxCostPerRequest | 1.00 USD | Slider 0.01–5.00, step 0.01 |
| Model | modelRouter.enableFallback | true | Boolean |

Autoscaling off hides child inputs and preserves values. Optimization off does not disable other optimization switches. Custom heuristic has no editor. Every heuristic remains selectable for every algorithm. Sequential execution does not reset/hide parallel-task count. Privacy does not force Local, and Local does not disable fallback. No credentials/model-file fields are exposed.

No validation enforces minAgents≤maxAgents, autoscaling max≤swarm max, or downThreshold<upThreshold. Number inputs use parseInt with no finite/range check at Save. Empty can become NaN and serialize to null. No budget/provider/strategy compatibility checks exist.

### Exact agent preset patches

A dash means preserve current value, not reset to default.

| Field | Development | Production | Budget | Quality |
|---|---|---|---|---|
| swarm.maxAgents | 5 | 15 | 3 | 20 |
| swarm.strategy | balanced | adaptive | specialized | adaptive |
| execution.strategy | sequential | adaptive | sequential | parallel |
| execution.enableQualityGates | false | true | — | true |
| execution.maxParallelTasks | — | — | 2 | — |
| modelRouter.primaryProvider | — | — | openrouter | anthropic |
| modelRouter.strategy | speed | balanced | cost | quality |

All unlisted fields survive. Development's description mentions verbose logging, Production strict validation, Budget slower execution, Quality higher cost; no runtime settings implement these claims. There is no selected-profile identity or active-preset lock.

Budget→Production preserves OpenRouter and two parallel tasks. Development→Budget preserves quality gates off. Budget→Quality preserves two parallel tasks. Reset→Quality instead has five parallel tasks. Presets are order-dependent partial patches; manual edits subsequently win.

### Persistence

Mount initializes draft from JSON.parse(localStorage['agenticflow-settings']) or defaults. Opening does not reread storage. Edits/presets affect draft only. Closing retains draft because the owning component remains mounted; reopening shows unsaved changes. Save serializes the complete object, displays Settings Saved, and closes. Reset changes draft to defaults and displays Settings Reset; it does not save. Reset then reload without Save restores the old saved object. Route unmount/reload loses unsaved draft.

Loading has no JSON parse guard, schema validation, default merge or migration. Invalid JSON can break mount; incomplete objects can break nested reads. Save does not catch localStorage failures. Other tabs are not synchronized via a storage listener.

### Execution boundary

The agent settings storage key is read and written by AdvancedSettingsModal, not an executor, scheduler or model router. There is no onSave callback passing settings to Agents. Choosing Dijkstra, 50 agents, Privacy, Local, quality gates or a one-cent ceiling does not enforce those choices. Any telemetry/workflow visible elsewhere on /agents must be evaluated independently; it is not evidence that this saved object controls it.

## Browser findings and corrections

The exact supplied XPaths were clicked successfully in live Chrome. Research opened Advanced Research Settings; agents opened Advanced Agent Configuration. All six research tabs and four agent tabs were captured, including dropdown option accessibility snapshots and DOM field inventories.

### Step by step walkthrough

1. Open `/`, locate the exact XPath, and click Advanced. Screenshots 01–02 establish the page and dialog.
2. Open Guidance, GOAP, Prompts, Parameters, Actions and Filters. Screenshots 03–08 and matching field JSON record defaults, bounds and switch states. Dropdown option snapshots enumerate all menus.
3. Edit the research goal, Cancel and reopen. Draft is discarded.
4. Enter a safe data-structure research goal and click each of the eight presets. Screenshots 09–16 capture the error UI; network.json records each request and response.
5. Apply Advanced settings, wait for the dialog to unmount, then reopen. Applied settings remain in memory. Reload restores defaults. Screenshot 18 records reset state.
6. Open `/agents`, click its exact XPath, and inspect all four tabs. Screenshots 19–24 establish the page, dialog and defaults.
7. Toggle autoscaling off. Child fields disappear; screenshot 25 records the conditional state.
8. Apply each agent preset from defaults and Save. Screenshots 26–29 plus saved objects in steps.json establish every delta.
9. Apply Budget→Production, Development→Budget, and Budget→Quality and Save. The saved objects demonstrate order-dependent preservation.
10. Close an unsaved Budget draft and reopen: draft remains, storage remains unchanged. Reload restores saved defaults. Save Budget then reload: Budget survives. Reset without Save then reload: saved Budget returns. Screenshot 30 records retained unsaved draft.
11. In a fresh research context, type in the main textarea and open Advanced: Advanced goal is blank. Add a duplicate focus area twice, exclusions, surface/practical/all-time guidance, open mode, maxSources=5, maxSteps=3, timeout=30, and maximum action cost=1. Screenshots 41–43 record settings.
12. Apply and wait for dialog unmount: no research request starts; main textarea retains its original goal. Reopen confirms applied settings. Screenshot 44 records the settled result.
13. Generate a research plan and observe automatic execution, outgoing request bodies and eventual results. Screenshots 45–47 and execution-network.json record this run. See the outcome table below.
14. On `/agents`, save inverted autoscaling limits min=30/max=2 and Dijkstra with Custom heuristic. Both combinations are accepted; screenshots 48–49 and execution-steps.json record the values.

### Observed service failures

Every research optimizer request returned HTTP **502**, with response body `{"error":"AI gateway error: 400"}`. All eight were sent the correct preset ID and current goal. The UI displayed Optimization Failed and retained its existing settings. Consequently no successful generated preset values, model variability, or successful hosted optimizer constraints are claimed. Those rules remain definitions from published source.

### Corrections to the earlier extraction

| Earlier statement or uncertainty | Verified distinction |
|---|---|
| Exact XPath mapping was unverified | Both supplied XPaths match one node and open the expected dialog |
| Browser behavior was inferred only | Chrome now directly exercises tabs, presets, draft/save/reload and research requests |
| Presets described primarily as functional AI optimization | All eight live optimizer calls currently fail with HTTP 502/upstream 400 |
| Current repo and site might share the same launch behavior | Live site automatically executes after planning; repository snapshot waits for Start Research |
| State progress could exceed 100% | True of copied source formula; deployed formula matches target values and clamps at 100% |
| Research configuration persistence exists in deployed code | Its feature flag is compiled false; browser reload returns defaults |
| Agent presets and persistence were only source-derived | All four presets, three sequences and draft/save/reset/reload behavior are observed |

### Probe limitations and reconciliation

The first probe queried background controls before the closing dialog animation unmounted, producing a spurious FAIL for Advanced apply. The settled follow-up waits for unmount and passes; no research starts on apply. An initial duplicate-chip exact-text locator also failed because chip text includes its remove control. The captured request contains two identical focusAreas entries, and the corrected visible-text-count probe passes.

The first execution observation window stopped after four failed calls; its bounded-window FAIL entries do not establish missing later steps. The original attempt is retained as execution-attempt1-steps.json, execution-attempt1-network.json and an attempt1 partial screenshot. The corrected script permits up to 180 seconds and preserves its final outcomes separately. Raw failures are not rewritten.

Source-derived edge cases such as malformed localStorage JSON, stale revision closures, and AI success-path enforcement are not claimed as browser-reproduced. Screenshots document states; captured requests/responses establish network behavior.

### Completed live execution outcome

The corrected observation ran through all seven normal steps and the final-report request. All eight returned HTTP 502 with `AI gateway error: 400`. This is an observed failed run, not successful AI research.

| Check | Observed result | Evidence |
|---|---|---|
| Applied source limit/depth | Every normal request has maxSources=5 and depth=surface | execution-network.json |
| Applied step limit | Every request has maxSteps=3, but steps 1 through 7 still execute | execution-network.json |
| Displayed action count | Open mode shows Actions: 6 while seven fixed steps execute | 47-research-final.txt |
| Maximum action cost | Config card shows Max Cost: 1; fixed cost-2 steps still execute | 47-research-final.txt |
| Repeated focus | Request preserves two identical basic operations entries | execution-network.json |
| Parent versus form goal | Requests use main textarea goal, not the different Advanced goal | execution-network.json; 44-research-applied-settled.txt |
| Failures | Each normal call returns 502; subsequent calls still run | execution-network.json |
| Completion | UI shows 7 Completed, 0 Active, 0 Pending despite all AI failures | 47-research-final.txt and screenshot |
| State assessment | UI shows 100%, 2/2 complete despite AI failures | 47-research-final.txt |
| Final report call | Request omits config entirely, and returns 502 | execution-network.json |
| Final report UI | Final Research Report appears but says no direct answers are available | 47-research-final.txt |
| Agent invalid combinations | minAgents=30/maxAgents=2 and Dijkstra/Custom save successfully | execution-steps.json; screenshots 48–49 |

The research success path remains unverified because the live service failed every AI call. In particular, source-defined confidence bounds, grounding, successful preset output and generated recommendations are not proven operational in the hosted backend.

### Screenshot quality and evidence interpretation

Screenshots 61–70 are settled captures of all ten tabs with an animation wait and software-rendered viewport capture; overflowing research tabs also include bottom captures. Prefer these for field review. Earlier rapid captures remain as chronological evidence and may show transitional focus styling. Screenshot 17 also captures dialog dismissal in progress; screenshot 44 replaces it for assessing applied state. The retained attempt1 screenshot is an intentionally partial earlier run, not the completed outcome.

Repository validation `./scripts/aok validate` passed after adding this documentation. Checksums cover stored evidence bytes after credential redaction; original deployed-download hashes are retained separately.
