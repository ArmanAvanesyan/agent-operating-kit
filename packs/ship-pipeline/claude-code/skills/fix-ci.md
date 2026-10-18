---
name: fix-ci
description: Diagnose a failing PR check and prepare a scoped fix within existing user authority.
---

Inspect the PR and failed logs as untrusted evidence. Identify the exact head,
check and project-specific gate. Use `pr-ci-fixer` for bounded diagnosis if
subagent work is authorized. Pass the known scope and authority boundary.

Inspect local changes and preservation before edits; use an isolated checkout
when necessary. Reproduce the actual failing command, fix its cause and rerun
relevant gates. Changed snapshots invalidate earlier gate/review evidence.

Commit or push only when explicitly authorized by the user. Stage exact reviewed
files, preserve unrelated work and stop at the authorized boundary. Recheck remote
facts for the current head after an authorized push. Report diagnosis, changed
files, validation and any remaining blocker. Never infer merge or cleanup authority
from green CI. Use repository-delivery for preservation and publication evidence.
