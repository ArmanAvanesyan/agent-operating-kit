# Migration From Existing Projects

Use existing agent instructions as source material, not as global truth.

1. Identify project-specific facts: repo names, product architecture, deployment environments, credentials, internal URLs, and legacy warnings.
2. Keep those facts in the target project's `AGENTS.md`, not in this kit.
3. Extract reusable workflow patterns into `agents/` templates.
4. Preserve existing instructions by generating backups or proposed files before replacement.
5. Record migration evidence in a task packet or setup note.

Previous AI sessions are usable only if they exist in files, GitHub comments, PRs, logs, or exported transcripts. Do not assume hidden chat memory exists.
