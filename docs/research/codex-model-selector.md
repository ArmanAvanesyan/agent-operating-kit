# Codex model selector: exported decision table

Source: https://learn.chatgpt.com/docs/model-selection (ModelSelection.react bundle, ModelRecommender data). Benchmark basis: https://katias-benchmark.openai.chatgpt.site/#insights (section "When to step up", reviewed 2026-09-17, 48/56 tasks).

Policy (verbatim): Task-specific editorial recommendations informed by reviewed benchmark results. Moving toward quality never lowers the model tier or reasoning effort. Adjacent priorities can recommend the same configuration. Quality increases reasoning effort over balanced. These user-directed tradeoffs are starting points, not measured usage, cost, or guaranteed quality improvements. Benchmark results below describe only the tested configurations; additional slider settings are editorial choices. Category fallbacks are labeled separately. Current recommendations use GPT-6.1 Sol in place of GPT-6 Sol; historical benchmark results in the rationales retain their original model identity.

Questions: 1) What type of work are you doing? 2) What type of task are you doing? 3) What do you want to prioritize? Priorities: usage (shown as "Lower latency & costs" on the developer site, "More usage" on the ChatGPT site), balanced, quality ("Higher quality outputs").

Verification on 2026-10-15: the raw JSON exactly matches the live official React
bundle. Normalized recommendations are checked against that raw snapshot. Benchmark
metadata and numerical rationales are statements from the selector; underlying benchmark
runs were not independently reproduced. This is an export, not an AOK runtime policy.

## Decision table

### Creating deliverables  (`knowledge`)

| Task | Lower latency & costs | Balanced | Higher quality | Basis | Benchmark tasks |
|---|---|---|---|---|---|
| Make small edits to existing files (`preciseEdits`) | Luna · Low | Luna · Medium | Luna · High | user-directed | 04-slide-deck-edit, 06-workbook-repair |
| Create slides or spreadsheets (`createFiles`) | Luna · Medium | GPT-6.1 Sol · Medium | Astra · Extra high | user-directed | 05-cash-flow-scenarios, 24-native-research-deck, 16-image-deck-reconstruction |
| Adapt existing content (`adaptContent`) | GPT-6.1 Sol · Low | GPT-6.1 Sol · Medium | GPT-6.1 Sol · High | user-directed | 33-native-handbook-edit, 42-concise-audience-writing |
| Create a coordinated set of files from input data (`linkedDecision`) | GPT-6.1 Sol · Medium | GPT-6.1 Sol · High | GPT-6.1 Sol · Extra high | user-directed | 18-acquisition-decision-pack |
| Turn source material into a final output (`sourceDeck`) | GPT-6.1 Sol · Medium | GPT-6.1 Sol · Medium | Astra · Extra high | user-directed | 41-brand-narrative-deck, 03-board-deck |
| Create a high-quality, complex deliverable (`sourceRevisions`) | GPT-6.1 Sol · Medium | Astra · High | Astra · Extra high | user-directed | 52-decision-lineage-depth, 25-supply-chain-decision-pack, 16-image-deck-reconstruction |
| Other (`knowledgeOther`) | GPT-6.1 Sol · Low | GPT-6.1 Sol · Medium | Astra · Extra high | editorial-fallback | 04-slide-deck-edit, 06-workbook-repair, 05-cash-flow-scenarios, 24-native-research-deck, 33-native-handbook-edit, 16-image-deck-reconstruction, 42-concise-audience-writing, 18-acquisition-decision-pack, 41-brand-narrative-deck, 03-board-deck, 52-decision-lineage-depth, 25-supply-chain-decision-pack |

Rationales:

- **Make small edits to existing files**: Luna scores 100 on surgical slide editing and workbook repair, matching Astra. These are bounded edits to existing files, not a general editing guarantee.
- **Create slides or spreadsheets**: Luna medium passes the spreadsheet at 99 and the editable visual-brief deck at 100. Sol and Astra medium pass the spreadsheet at 100. Astra extra high is the closest tested reference reconstruction at 96.6; Sol extra high scores 96. These separately tested tasks do not establish a universal file-creation advantage.
- **Adapt existing content**: Astra scores 100 on the structured handbook edit and 98.83 on audience-specific writing, retaining all 104 required facts. Sol omits a required qualification in the writing task. This is a fixed message packet, not a general writing guarantee.
- **Create a coordinated set of files from input data**: Astra medium and extra high score 99.5 and pass the linked decision-pack requirements. Sol and Luna fail required source mapping or changed-decision checks. No result is perfect; clarity or chart-label deductions remain.
- **Turn source material into a final output**: Astra completes the brand-and-narrative deck at 95. Sol omits required facts; Luna misses palette and handoff requirements. The broader final-output label generalizes from a presentation task and does not establish superior visual taste.
- **Create a high-quality, complex deliverable**: User-requested grouping for complex deliverables requiring exact source traceability or close visual matching. Astra extra high passed the tested document-lineage requirements and was the closest tested visual-reference reconstruction. This does not establish a general guarantee for every complex deliverable; the separate supply-chain source-coverage task remained incomplete for all models.
- **Other**: Category-level fallback inferred from the reviewed tasks, not a separately measured benchmark result. More usage favors a smaller configuration; higher quality favors additional reasoning or a larger model for unspecified requirements.

### Software engineering  (`engineering`)

| Task | Lower latency & costs | Balanced | Higher quality | Basis | Benchmark tasks |
|---|---|---|---|---|---|
| Fix a well-scoped bug (`focusedBug`) | Luna · Low | Luna · Medium | Luna · High | user-directed | 29-focused-patch-portfolio |
| Update or debug existing software (`updateSoftware`) | Luna · Medium | GPT-6.1 Sol · Medium | GPT-6.1 Sol · Extra high | user-directed | 44-audio-editor-regression, 51-versioned-service-release, 21-ledger-online-migration, 10-date-boundary-fix, 29-focused-patch-portfolio |
| Build application features (`applicationFeatures`) | GPT-6.1 Sol · Low | GPT-6.1 Sol · Medium | GPT-6.1 Sol · Extra high | user-directed | 50-async-ui-continuity, 55-atomic-replay-ledger |
| Migrate an app or service (`serviceMigration`) | GPT-6.1 Sol · Medium | GPT-6.1 Sol · High | Astra · Extra high | user-directed | 09-job-queue-migration, 21-ledger-online-migration |
| Build polished applications (`appRecovery`) | GPT-6.1 Sol · Medium | Astra · Medium | Astra · Extra high | user-directed | 17-offline-field-operations, 35-changing-booking-app, 20-resilient-dispatch-console |
| Other (`engineeringOther`) | GPT-6.1 Sol · Medium | GPT-6.1 Sol · Medium | Astra · Extra high | editorial-fallback | 29-focused-patch-portfolio, 44-audio-editor-regression, 10-date-boundary-fix, 51-versioned-service-release, 21-ledger-online-migration, 50-async-ui-continuity, 55-atomic-replay-ledger, 31-incremental-event-normalizer, 49-streaming-contract-interactions, 48-bounded-memory-query-engine, 09-job-queue-migration, 17-offline-field-operations, 35-changing-booking-app, 20-resilient-dispatch-console |

Rationales:

- **Fix a well-scoped bug**: All four models score 100 in all three runs of the eight-patch portfolio. Requested fixes, regression behavior, and handoffs pass. The three runs reuse one small packet, so they show repeatability on that input rather than a general reliability rate. Medium and high provide editorial options for additional checking, not measured improvements over low.
- **Update or debug existing software**: Luna, Sol and Astra score 100 at medium and extra high on the editor regression task. Medium service-release runs also score 100; the separate invoice-ledger migration was tested at extra high. Sol and Astra score 100 at low on the date-boundary repair, while Luna required a later medium probe under changed conditions. These results support scoped updates, not general software parity.
- **Build application features**: Sol and Astra pass at 100 on the asynchronous UI and atomic-ledger contracts. Luna retains focus or diagnostic failures despite passing core behavior. The supporting results apply to these tasks, not every application feature.
- **Migrate an app or service**: Astra scores 100 at medium and extra high on the job-queue migration. Sol scores 95 and Luna 93–94 with operational corrections remaining. All three score 100 on the separate invoice-ledger migration, so this is not a blanket migration advantage.
- **Build polished applications**: Astra passes the booking app at 100 and the offline app at 96–97. Other models leave required UI or recovery defects. Astra still fails the dispatch-console task at 98. The polished-application label is editorial; these tasks measured completeness and recovery rather than general visual polish.
- **Other**: Category-level fallback inferred from the reviewed tasks, not a separately measured benchmark result. More usage favors a smaller configuration; higher quality favors additional reasoning or a larger model for unspecified requirements. The consolidated Software engineering category retains the former engineering fallback; named debugging tasks retain their task-specific settings.

### 3D and design  (`design`)

| Task | Lower latency & costs | Balanced | Higher quality | Basis | Benchmark tasks |
|---|---|---|---|---|---|
| Design a page or interface (`visualDesign`) | GPT-6.1 Sol · Medium | GPT-6.1 Sol · Medium | GPT-6.1 Sol · High | editorial-fallback | 20-resilient-dispatch-console, 35-changing-booking-app, 41-brand-narrative-deck |
| Create editable 3D models (`editableParts`) | GPT-6.1 Sol · Low | GPT-6.1 Sol · Medium | GPT-6.1 Sol · High | user-directed | 39-multiview-parametric-cad |
| Animate a 3D model (`mechanicalModel`) | GPT-6.1 Sol · Medium | GPT-6.1 Sol · High | GPT-6.1 Sol · Extra high | user-directed | 02-parametric-desk-lamp |
| Build interactive 3D experiences (`interactiveScene`) | GPT-6.1 Sol · Medium | GPT-6.1 Sol · High | Astra · Extra high | user-directed | 01-tidal-observatory, 19-kinetic-pavilion |
| Other (`designOther`) | GPT-6.1 Sol · Medium | GPT-6.1 Sol · High | Astra · Extra high | editorial-fallback | 39-multiview-parametric-cad, 02-parametric-desk-lamp, 01-tidal-observatory, 19-kinetic-pavilion |

Rationales:

- **Design a page or interface**: Editorial starting points for page and interface design, informed by related app-interface and visual-brief tasks. Sol medium offers the smaller configuration; GPT-6.1 Sol medium and high offer options for more demanding work and additional checking. This is not a separately measured design benchmark or a claim of general visual taste or measured gains from higher effort.
- **Create editable 3D models**: Sol and Astra score 100 on all six CAD parts and their revisions. Luna geometry matches but a required named parameter is absent. This tests the editing contract, not general spatial ability.
- **Animate a 3D model**: Keep GPT-6.1 Sol at high reasoning as the balanced animation setting, with medium for lower latency and cost and extra high for more reasoning. These effort choices are editorial, not directly measured animation results. In the related mechanical-model task, Astra scores 98 with an accepted shape compromise. Luna scores 92 and needs intersection, diffuser, and cable-guide repairs; Sol scores 83 and fails mechanical requirements. One lamp does not establish a general animation or design hierarchy.
- **Build interactive 3D experiences**: Astra extra high scores 99.2 and fixes geometry and mobile inspection issues present at medium. Luna historically passing outputs still need geometry or mobile-tour repairs; Sol extra high fails the shoreline check. This is not a general 3D ranking or a perfect result.
- **Other**: Category-level fallback inferred from the reviewed tasks, not a separately measured benchmark result. More usage favors a smaller configuration; higher quality favors additional reasoning or a larger model for unspecified requirements.

### Research & analysis  (`research`)

| Task | Lower latency & costs | Balanced | Higher quality | Basis | Benchmark tasks |
|---|---|---|---|---|---|
| Organize information and prioritize work (`organizeInformation`) | Luna · Low | Luna · Medium | GPT-6.1 Sol · High | user-directed | 30-grounded-record-extraction, 13-slack-email-triage, 32-inbox-action-routing, 12-customer-data-pipeline |
| Search documents, messages, and emails (`searchSources`) | Luna · Medium | Luna · High | Luna · Extra high | user-directed | 27-long-context-authority-audit |
| Research decisions and verify facts (`researchDecisions`) | GPT-6.1 Sol · Low | GPT-6.1 Sol · Medium | GPT-6.1 Sol · Extra high | user-directed | 43-evidence-verification-batch, 14-vendor-decision-memo, 56-qualified-source-reconciliation, 15-conflicting-product-plan |
| Analyze data and solve problems (`analyzeProblems`) | Luna · Medium | GPT-6.1 Sol · High | Astra · Extra high | user-directed | 11-kpi-causal-diagnosis, 22-rollout-identification-study, 28-thermal-inverse-solver, 23-crew-recovery-optimizer, 45-proof-counterexample-portfolio |
| Other (`researchOther`) | GPT-6.1 Sol · Medium | GPT-6.1 Sol · Medium | GPT-6.1 Sol · Extra high | editorial-fallback | 30-grounded-record-extraction, 13-slack-email-triage, 32-inbox-action-routing, 27-long-context-authority-audit, 43-evidence-verification-batch, 14-vendor-decision-memo, 56-qualified-source-reconciliation, 15-conflicting-product-plan, 12-customer-data-pipeline, 11-kpi-causal-diagnosis, 22-rollout-identification-study, 28-thermal-inverse-solver, 23-crew-recovery-optimizer, 45-proof-counterexample-portfolio |

Rationales:

- **Organize information and prioritize work**: Luna, Sol and Astra score 100 on the fixed structured-extraction packet. Luna medium scores 98 on live-context triage and 100 on synthetic routing; Sol medium scores 100 on both. Sol and Astra score 100 on messy-data preparation while Luna scores 97. Medium accounts for preparation and prioritization beyond simple extraction.
- **Search documents, messages, and emails**: All four models scored 100 at extra high on the fixed long-context authority audit, including superseded decisions, dependencies, and deliberately unanswerable questions. Only extra high was tested. Medium and high are user-directed editorial alternatives for the lower-priority positions; their sufficiency is not established by this benchmark.
- **Research decisions and verify facts**: Sol medium scores 100 on vendor decisions and qualified-source contracts. On the conflicting product plan, Sol extra high scores 99 and Astra extra high scores 100. Sol and Astra also score 100 at low and medium on the fact-verification task. Higher effort is not required for every fact check.
- **Analyze data and solve problems**: Astra and Luna score 100 on activation diagnosis at extra high; Sol scores 99. Astra scores 100 on the causal study and inverse solver, while Sol scores 98.5 and 100 and Luna 97 and 99. Sol and Astra medium already score 100 on the planning and proof tasks. These are task-specific results, not a general mathematical ranking.
- **Other**: Category-level fallback inferred from the reviewed tasks, not a separately measured benchmark result. More usage favors a smaller configuration; higher quality favors additional reasoning or a larger model for unspecified requirements. The consolidated research, decisions, and analysis category uses the former complex-analysis fallback to cover unspecified analytical work. This is an editorial default, not a measured result.

### Computer use  (`computer`)

| Task | Lower latency & costs | Balanced | Higher quality | Basis | Benchmark tasks |
|---|---|---|---|---|---|
| Find information or controls in an app (`preciseControls`) | GPT-6.1 Sol · Low | GPT-6.1 Sol · Medium | GPT-6.1 Sol · High | user-directed | 38-dense-interface-targeting |
| Complete a workflow in an app (`updateRecords`) | GPT-6.1 Sol · Low | GPT-6.1 Sol · Medium | GPT-6.1 Sol · High | user-directed | 07-browser-crm, 47-scoped-admin-workflows |
| Test an app or website (`websiteQa`) | GPT-6.1 Sol · Medium | GPT-6.1 Sol · High | GPT-6.1 Sol · Extra high | user-directed | 08-browser-checkout-qa |
| Other (`computerOther`) | GPT-6.1 Sol · Low | GPT-6.1 Sol · Medium | GPT-6.1 Sol · Extra high | editorial-fallback | 38-dense-interface-targeting, 07-browser-crm, 47-scoped-admin-workflows, 08-browser-checkout-qa |

Rationales:

- **Find information or controls in an app**: Use Sol medium as the default, Sol low when prioritizing lower latency and cost, and GPT-6.1 Sol high when prioritizing quality. These are user-directed editorial choices. Sol low hits 117 of 120 static targets and scores 99. Astra low hits all 120 and scores 100. Luna medium hits 118 and scores 99.3333. These results do not establish gains for the untested settings or live multi-step navigation performance.
- **Complete a workflow in an app**: Sol scores 100 on CRM reconciliation and scoped admin workflows. Luna scores 91 on CRM and 98 on admin, where saved actions pass but required screenshots are missing. This gap concerns evidence delivery, not incorrect saved values.
- **Test an app or website**: Sol and Astra find all six seeded checkout defects. Sol scores 99.75 with whitespace-only and non-ASCII ZIP cases untested; Astra extra high scores 100. Luna misses the mobile defect and needs evidence work. Physical touch and full screen-reader behavior were outside scope.
- **Other**: Category-level fallback inferred from the reviewed tasks, not a separately measured benchmark result. More usage favors a smaller configuration; higher quality favors additional reasoning or a larger model for unspecified requirements.

## Static guidance ladder (page section "Model and reasoning effort")

- Luna · Low: Fine-grained edits, well-scoped problem-solving, and simple data extraction.
- Luna · Extra high: Finding current context across multiple apps, prioritizing work, and solving problems with clear constraints.
- GPT-6.1 Sol · Medium: Complex technical work and coordinated deliverables you expect to revise.
- GPT-6.1 Sol · Extra high: Polished deliverables, connected visual systems, and decisions built from conflicting evidence.
- Astra · Low: Concise writing and content adaptation that preserve facts and nuance.
- Astra · Medium: Ambitious projects that need broad context, reliable interactions, and complete results.
- Astra · Extra high: Demanding analysis and complex deliverables with exacting requirements.
