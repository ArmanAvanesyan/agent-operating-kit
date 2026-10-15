# Claude model selection: exported guidance

Companion to `codex-model-selector.md`. Checked 2026-10-15.

No interactive selector was found in the reviewed Anthropic guidance pages listed below.
This is a synthesis of published guidance, not a claim about every Anthropic surface.
The review checked current source text; it did not replay an interactive browser flow.
Section 8 is a repository-derived mapping, not Anthropic guidance. API prices, defaults
and model availability are a dated snapshot and can differ by provider or account.

Sources:

- Choosing the right model: https://platform.claude.com/docs/en/about-claude/models/choosing-a-model
- Models overview: https://platform.claude.com/docs/en/models/overview
- Effort: https://platform.claude.com/docs/en/build-with-claude/effort
- Optimizing for cost and intelligence: https://platform.claude.com/docs/en/about-claude/models/optimizing-for-cost-and-intelligence
- Claude Code model configuration: https://code.claude.com/docs/en/model-config
- Blog, choosing a model and effort level in Claude Code: https://claude.com/blog/claude-model-and-effort-level-in-claude-code
- Claude Academy tutorial (decision table only): https://academy.claude.com/tutorials/choosing-the-right-claude-model

## 1. Current lineup

| | Claude Fable 5.1 | Claude Opus 5.5 | Claude Sonnet 5.5 | Claude Haiku 5.5 |
|---|---|---|---|---|
| Positioning | Demanding reasoning and long-horizon agentic work | Long-running agentic coding and knowledge work | Best combination of speed and intelligence | High-volume, latency-sensitive tasks such as classification, extraction, routing |
| API id | `claude-fable-5-1` | `claude-opus-5-5` | `claude-sonnet-5-5` | `claude-haiku-5-5` |
| Latency | Slower | Moderate | Fast | Fastest |
| Price per MTok (in / out) | $10 / $50 | $4 / $20 | $2 / $10 | from $0.10 / $0.50 |
| Thinking | Adaptive, always on | Adaptive, always on | Adaptive | Adaptive |
| Default effort | `high` | `medium` | `high` (API); `medium` in Claude Code | `medium` |
| API context / max output | 1M / 128K | 1M / 128K | 1M / 128K | 1M / 128K |

Claude Mythos 5.1 (`claude-mythos-5-1`) has the same capabilities as Fable 5.1 and is limited
to verified organizations. Legacy models still served: Fable 5, Opus 5, Opus 4.8, 4.7, 4.6,
4.5, Sonnet 5, Sonnet 4.6, Haiku 4.5.

## 2. Model selection matrix (official)

"Most workloads start with Claude Opus 5.5."

| When you need | Start with | Example use cases |
|---|---|---|
| The highest available capability | Claude Fable 5.1 | Agent sessions that run for hours, multistep deep research, analysis carried through to a finished document, spreadsheet, or deck |
| Complex agentic coding and enterprise work | Claude Opus 5.5 | Multihour autonomous coding agents, large-scale refactoring, complex systems engineering, vision-heavy workflows, computer use |
| Speed and capability for everyday coding, agent, and enterprise workloads | Claude Sonnet 5.5 | Code generation, data analysis, content creation, visual understanding, agentic tool use |
| The lowest latency and price | Claude Haiku 5.5 | Real-time applications, high-volume intelligent processing, cost-sensitive deployments needing strong reasoning, sub-agent tasks |

Two starting strategies: efficiency-first (start on Haiku 5.5, upgrade only for capability
gaps) for prototyping, tight latency, cost-sensitive and high-volume work; capability-first
(start on Opus 5.5, then lower effort or downgrade) for complex reasoning, scientific or
mathematical work, nuanced understanding, accuracy over cost, advanced coding and
high-autonomy agents. Escalate to Fable 5.1 only when Opus 5.5 at `xhigh` or `max` still
falls short on demanding reasoning or long-horizon agentic work.

## 3. Effort levels (official)

| Level | Description | Typical use case |
|---|---|---|
| `max` | Absolute maximum capability, no constraint on token spend | Deepest reasoning and most thorough analysis |
| `xhigh` | Extended capability for long-horizon work | Long-running agentic and coding tasks over 30 minutes with token budgets in the millions |
| `high` | Spends as many tokens as the task needs | Complex reasoning, difficult coding, agentic tasks |
| `medium` | Balanced, moderate token savings | Agentic tasks needing a balance of speed, cost, performance |
| `low` | Most efficient, some capability reduction | Simple tasks needing best speed and lowest cost, such as subagents |

Effort applies to every output token, including tool calls. Lower effort means fewer and
terser tool calls and no preamble. Effort is a behavioral signal, not a token budget.
Setting the model's default explicitly is identical to omitting the parameter.

Per-model recommendations (these override the generic table):

| Model | Default | Recommendation |
|---|---|---|
| Fable 5.1 | `high` | Start at `high`. Step up to `xhigh` or `max` for the most capability-sensitive agentic and coding work. Step down to `medium` or `low` for routine or latency-sensitive work once evals hold. |
| Fable 5 | `high` | Same shape. Lower effort on Fable 5 often exceeds `xhigh` on prior models. |
| Opus 5.5 | `medium` | Thinking cannot be turned off, so effort is the primary control. Run a fresh effort sweep; do not carry settings over from Opus 5. `medium` on Opus 5.5 matches or exceeds Opus 5 at `high`. |
| Opus 5 | `high` | Step up to `xhigh` for demanding coding and agents, `max` only when unconstrained spend is justified. Use `low` and `medium` liberally where evals hold. |
| Opus 4.8 / 4.7 | `high` (API); Claude Code defaults 4.7 to `xhigh` | Start at `xhigh` for coding and agentic work, `high` minimum for intelligence-sensitive work, `medium` for cost-sensitive, `max` only with measured headroom. |
| Sonnet 5.5 | `high` (API) | Levels recalibrated versus Sonnet 5. Agentic coding and multistep tool use: start `medium` for well-specified tasks, `high` for harder or longer ones. Chat and latency-sensitive: `medium` or `low`. `xhigh` or `max` only with measured gain. |
| Sonnet 5 | `high` | `xhigh` for the hardest coding and agents; `medium` comparable to Sonnet 4.6 at `high`; `low` for high-volume chat. |
| Sonnet 4.6 | `high` | Recommended `medium` for most applications. |
| Haiku 5.5 | `medium` | `low` for chat, short tool tasks, simple high-volume requests (may skip searches or checks in long agent prompts). `high` for knowledge work, longer agent tasks, strict instruction following. `xhigh` or `max` only with measured gain; compare against Sonnet 5.5. |

## 4. Claude Code: aliases, defaults, and when to use each effort

| Alias | Resolves to (Anthropic API) | Intended use |
|---|---|---|
| `fable` | Fable 5.1 | Hardest and longest-running tasks |
| `best` | `fable` where available, else `opus` | |
| `opus` | Opus 5.5 | Complex reasoning |
| `sonnet` | Sonnet 5.5 | Daily coding |
| `haiku` | Haiku 5.5 | Simple tasks |
| `opusplan` | `opus` in plan mode, `sonnet` for execution | |
| `default` | Account/provider/organization runtime default; docs list Opus 5.5 for eligible accounts | |

Default effort in Claude Code: `medium` on Opus 5.5, Sonnet 5.5, Haiku 5.5; `xhigh` on
Opus 4.7; `high` elsewhere. `ultrathink` adds an in-context instruction for the turn; it does not change API effort.

Effort persistence is per model: Enter saves the selection; Alt+Enter applies it only
to the session where supported. Opus 5.5 and later models ignore legacy user-level
`effortLevel` and start at their own defaults until selected explicitly. Project, local,
managed and `--settings` effort overrides still apply across models. See model-config.

| Level | When to use it (Claude Code docs) |
|---|---|
| `low` | Quick exchanges where you review each result: brainstorming, a first sketch, a small change like a rename |
| `medium` | Day-to-day engineering work with a clear scope, such as implementing a new feature |
| `high` | Work where verification matters or edge cases are likely, such as fixing a bug in an existing codebase |
| `xhigh` | Deeper reasoning at higher token spend |
| `max` | Hard problems Claude should work through alone, such as finding security vulnerabilities; prone to overthinking, test before adopting |

Blog rules of thumb: pick a smaller model (Sonnet) when the work is routine, meaning edits
you can describe precisely, mechanical changes, or questions about code already in context.
Pick a larger model (Opus, Fable) when the problem is genuinely hard: subtle bugs,
unfamiliar domains, architecture decisions. Use the default effort for most tasks. When
Claude fails, diagnose: if it had the context and tried but was wrong, pick a larger model;
if it skipped a file, did not run tests, or did not double-check, raise effort. Fable is the
specialist, Opus the expert, Sonnet the generalist; effort is how thoroughly each works.

## 5. Situation table (official, cost optimization page)

| Your situation | Do this |
|---|---|
| Any workload | Turn on prompt caching and trim unneeded tokens |
| Costs too high, quality fine | Lower effort on the current model |
| Not on the latest model | Upgrade; each newer model solved at least as many tasks, usually for less per solved task |
| Choosing or switching models | Compare on cost per completed task, not per token |
| High-volume work with checkable outputs | Try Haiku 5.5 first |
| Quality not good enough | Restore effort if you lowered it; otherwise next tier up at its default effort |
| Attempts end with `max_tokens` | Raise `max_tokens` to 64,000 |
| Outputs are checkable (tests, verifier) | Run at `low`, re-run failures at `high`: about 40% cheaper than all-`high` at the same pass rate |
| A few very costly agent runs | Set a Managed Agents session budget and workspace spend limit |
| Agent runs should finish sooner | Tell the model time matters and show elapsed time: 7% to 47% less time |
| Lower-cost model stalls only on hard decisions | Try an advisor model |
| Work exceeds one context window | Orchestrator delegates bulk work to lower-cost models |

## 6. Published effort and model tradeoffs (official)

These are vendor-reported evaluations, not results reproduced by this repository.
The source uses different denominators: cost per solved task in its model comparison
and cost per attempted task in its retry/effort charts. Do not combine those series.
The SWE-bench subset contains 478 problems; most settings have two runs, with single
runs explicitly noted. The following export preserves the chart series, except the
first three rows which use the comparison table and are labeled accordingly.

| Benchmark | Model | Effort | Score | Cost and denominator |
|---|---|---|---|---|
| SWE-bench Pro subset | Sonnet 5.5 | `high` (default) | 87.6% | $0.11 per solved task |
| SWE-bench Pro subset | Opus 5.5 | `low` | 87.4% | $0.12 per solved task |
| SWE-bench Pro subset | Opus 5.5 | `medium` (default) | 92.8% | $0.22 per solved task |
| SWE-bench Pro subset | Opus 5.5 | `high` | 95.3% | $0.29 |
| SWE-bench Pro subset | Opus 5.5 | `xhigh` | 96.7% (single run) | $0.74 |
| SWE-bench Pro subset | Fable 5.1 | `high` (default) | 92.3% (single run) | $1.19 |
| SWE-bench Pro subset | Opus 5.5 | `low`, failures re-run at `high` | about 97% | about $0.17 |
| Internal agentic coding | Opus 5.5 | `medium` | 86.6% | $0.84 per attempt |
| Internal agentic coding | Fable 5.1 | `medium` | 84.2% (single run) | $2.68 per attempt |
| DeepResearch Bench II | Fable 5.1 | `low` | rubric 66% | $4.48 |
| DeepResearch Bench II | Fable 5.1 | `high` (default) | rubric 65% | $6.89 |
| GPQA Diamond | Haiku 5.5 | `medium` | 85% vs 91% for Opus 5.5 and Sonnet 5.5 | about 1/20 of Opus 5.5's cost per correct answer |
| Managed Agents task set (79) | Haiku 5.5 | `medium` | 93% passed (single run) | about $0.011 per passed attempt |

## 7. Academy decision table (official, no effort levels)

| Task | Model | Rationale |
|---|---|---|
| Summarizing articles | Haiku | Straightforward content extraction without complex reasoning |
| Debugging code | Sonnet | Strong coding with responsive feedback |
| Analyzing complex research papers | Opus | Deep analysis across long specialized documents |
| Building a working project from rough ideas | Fable | Plans steps autonomously, fewer mid-task check-ins |

## 8. Derived mapping onto the Codex selector axes (not official)

This section exists only so the two exports can be compared row for row. Each cell is
derived from the official rules above (cited by section). Anthropic has not published a
table in this shape. Treat it as a starting point to verify with your own evals.

| Work type | Task | Latency and cost | Balanced | Quality | Derived from |
|---|---|---|---|---|---|
| Software engineering | Fix a well-scoped bug | Sonnet 5.5 · medium | Sonnet 5.5 · high | Opus 5.5 · high | §4 `high` for bug fixing; §3 Sonnet agentic coding `medium` to `high` |
| Software engineering | Small precise edits, renames, mechanical changes | Haiku 5.5 · low | Sonnet 5.5 · low | Sonnet 5.5 · medium | §4 `low` for small changes; blog: routine work on Sonnet |
| Software engineering | Build application features | Sonnet 5.5 · medium | Opus 5.5 · medium | Opus 5.5 · high | §4 `medium` for scoped feature work; §2 Opus 5.5 default start |
| Software engineering | Migrate or refactor at scale | Opus 5.5 · medium | Opus 5.5 · high | Opus 5.5 · xhigh | §2 large-scale refactoring on Opus 5.5; §3 `xhigh` for long-horizon |
| Software engineering | Multihour autonomous build, architecture decisions | Opus 5.5 · high | Opus 5.5 · xhigh | Fable 5.1 · high or xhigh | §2 escalate to Fable 5.1 when Opus 5.5 at `xhigh`/`max` falls short |
| Creating deliverables | Adapt or edit existing content | Haiku 5.5 · low | Sonnet 5.5 · medium | Sonnet 5.5 · high | §2 Sonnet 5.5 content creation; §3 Haiku `low` for simple requests |
| Creating deliverables | Create a complex document, deck, or spreadsheet | Sonnet 5.5 · high | Opus 5.5 · medium | Fable 5.1 · high | §2 Fable 5.1 "analysis carried through to a finished document, spreadsheet, or deck" |
| Research and analysis | Search, extract, classify, route | Haiku 5.5 · low | Haiku 5.5 · medium | Sonnet 5.5 · medium | §1 Haiku positioning; §3 Haiku `low` for short tool tasks |
| Research and analysis | Analyze data and solve problems | Sonnet 5.5 · medium | Opus 5.5 · medium | Opus 5.5 · high | §2 Sonnet data analysis; §7 Opus for deep analysis |
| Research and analysis | Multistep deep research | Opus 5.5 · high | Fable 5.1 · low | Fable 5.1 · high | §2 Fable 5.1 deep research; §6 Fable `low` scored as well as `high` on DeepResearch Bench II at 35% less cost |
| Computer use | Find controls, complete a workflow, test an app | Sonnet 5.5 · medium | Opus 5.5 · medium | Opus 5.5 · high | §2 computer use and vision-heavy workflows listed under Opus 5.5 |
| Subagents and workers | Bulk fan-out work | Haiku 5.5 · low | Haiku 5.5 · medium | Sonnet 5.5 · medium | §3 `low` for subagents; §5 orchestrator delegates to lower-cost models |
| Security and hard problems | Vulnerability hunting, frontier problems | Opus 5.5 · high | Opus 5.5 · max | Fable 5.1 · xhigh | §4 `max` for finding security vulnerabilities; §2 Fable 5.1 highest capability |

Consistency rules applied, mirroring the Codex table's policy: moving toward quality never
lowers model tier; effort is monotonic within a tier, but can reset when upgrading models; adjacent priorities may share a cell; quality raises effort
before it raises the model tier, except where the official matrix names a larger model for
the task.
