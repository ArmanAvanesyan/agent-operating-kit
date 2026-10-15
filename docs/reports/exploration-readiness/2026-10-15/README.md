# AOK exploration and readiness execution

This run applies the repository's two skills to AOK itself on 2026-10-15. The selected
scope is **local development of the contributor/Go CLIs and skill helpers**. It does not
assess production service integration, external agent installation or release publishing.

- [Exploration report](exploration.md): project map, execution paths, command matrix,
  coverage, findings and handoff.
- [Readiness report](readiness.md): profile capabilities, observed checks and limitations.
- [Readiness input](readiness-input.json) and [assessment](readiness-assessment.json):
  skill-local evidence records and consistency verdict.
- [Command records](commands.json): exact argv, working directory, UTC timing, expected
  and observed exit codes and stdout/stderr paths.
- [Source state](source-state.json) and [fix patch](evidence/fixes.patch): base revision
  and hashes for the modified/inspected working tree, including uncommitted fixes.
- [Artifact integrity](integrity.json): SHA-256 values for saved evidence and reports,
  excluding the integrity file itself.

The base commit is `9dec920f6cb202e4733f5d8d9504441d51cfe729`; this task's fixes are
uncommitted at report time. Revision alone does not identify the tested source; consult
the source hashes and patch. Initial inventory includes the report directory created by
the task and deliberately omits excluded/deep paths.

Historical failures remain in the evidence: `extensionless-before` reproduces the helper
rejection with expected exit 2; `python-tests` records a failing assertion in the newly
added macOS regression test. Final acceptance uses the corrected `focused-tests-final`
and `python-tests-final` runs, not those earlier outcomes.

Commands using `/tmp` include disposable paths. The scratch downstream project was removed
at the end of its checks. The test binary at `/tmp/aok-readiness-build/aok` is outside the
checkout and may be deleted. Existing optional Rust tool binaries were reused via a
child-process PATH; no global tools or user-level AOK settings were installed or changed.
No secrets or credential values were collected. Empty stderr logs are retained to make
successful invocations explicit. Evidence was captured by subprocess with separate stdout
and stderr; no unobserved command is represented as executed.

The fix patch is stored without context lines to keep the committed evidence whitespace-clean.
Use `git apply --unidiff-zero` when applying it to the stated base; the PR diff remains
the normal review surface. Its format was regenerated for publication; test logs are unchanged.
