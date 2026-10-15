# Goal ruv io configuration research

Start with [the full report](report.md). It includes every field/default, preset patch, merge rule, persistence behavior, execution consumer, and live-browser differences.

- [Exact default objects](defaults.json), including complete multiline prompt strings.
- [Evidence manifest and checksums](manifest.json).
- [Original Claude session](evidence/claude-session.txt).
- [Browser step log](evidence/browser/steps.json), [execution step log](evidence/browser/execution-steps.json), [preset network observations](evidence/browser/network.json), and [execution network observations](evidence/browser/execution-network.json).
- [Screenshot index](screenshots.md).
- [Browser walkthrough and differences](report.md#browser-findings-and-corrections).

Source snapshots retain upstream code as research evidence. They are not installed AOK adapters, plugins, or runtime functionality. The upstream license is retained in evidence/source/LICENSE. Credentials embedded in public JavaScript are redacted; the manifest distinguishes original-download hashes from stored-file hashes. Network captures include request/response bodies only, not authentication headers, browser cookies, or personal profile data.

[Artifact verification results](verification.json) include exact comparisons of all saved preset/combination objects against source-defined patches.

## Replay browser verification

Requires Node.js, Playwright, and installed Google Chrome. The scripts use fresh nonpersistent browser contexts. They click actual live controls and make real preset/research requests; backend success depends on the public service. Research requests may incur usage on that service. Artifacts are written beside the scripts and screenshots under evidence/screenshots.

```bash
AOK_PLAYWRIGHT_MODULE=/absolute/path/to/node_modules/playwright \
  node docs/research/goal-ruv-io/evidence/browser/verify.cjs
AOK_PLAYWRIGHT_MODULE=/absolute/path/to/node_modules/playwright \
  node docs/research/goal-ruv-io/evidence/browser/verify-execution.cjs
```

The original raw logs preserve probe failures: querying background controls before the closing dialog unmounted, and an exact text locator that did not match list chips containing remove buttons. Follow-up observations reconcile these in the report. A raw FAIL entry is not silently rewritten as PASS. The first research execution observation was also stopped too early; its partial logs are retained with an execution-attempt1 prefix. The corrected run captures all seven steps and final report. Preset BACKEND_ERROR entries are real observed service failures.

No exhaustive Cartesian product or successful AI preset generation is claimed. All fields and declared domains are extracted; representative combinations, persistence, requests and failures are exercised. Private deployed backend code remains unavailable.
