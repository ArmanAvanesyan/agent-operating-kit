# Scoped exploration and evidence

Start with product purpose and repository boundaries. For multi-repo projects, map shared
contracts and owners before examining each relevant repository; inaccessible repositories
are explicit gaps. Do not infer integration from co-location.

Inventory metadata first, then read relevant manifests, documentation and selected source.
The inventory uses fixed exclusions rather than full gitignore semantics; inspect its
coverage and omissions before expanding scope. It counts files, not lines of code. It does
not read configuration values, validate instructions or execute project code.

A command matrix records purpose, working directory, source declaration, prerequisites and
expected result. Mark commands documented-but-untested until actually run. Existing setup
scripts may install dependencies, contact services or change data: inspect them before use.

Capture only material findings: architecture and execution paths, dependencies/consumers,
verification surfaces, configuration names, blockers and decisions. Label claims observed,
documented-but-untested, inferred or unknown. Preserve human rationale and explicitly
identify superseded decisions instead of overwriting history silently.

Finish when the selected scope has a source-backed map, usable commands, relevant
prerequisites and an actionable handoff. Exploration can succeed while execution remains
blocked. Readiness needs observed task-specific checks; inventory output alone cannot prove it.
