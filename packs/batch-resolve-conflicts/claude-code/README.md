# batch-resolve-conflicts

Slash command `/batch-resolve-conflicts <PR-numbers>` that fans out one
sub-agent per conflicting PR, in parallel. Each sub-agent rebases its PR
onto `origin/main`, resolves common conflicts using built-in canonical
rules, force-pushes, watches CI, and merges.

## Usage

```
/batch-resolve-conflicts "4,5,9,11"
```

The coordinator:

1. Validates each PR is OPEN and CONFLICTING
2. Pre-fetches the canonical versions of well-known shared files
   (`pyproject.toml`, `app/db/repositories/__init__.py`, etc.)
3. Spawns N sub-agents in parallel — one per PR
4. Each sub-agent rebases + resolves + runs gates + force-pushes + merges
5. Renders a live status table as agents report back

## Built-in conflict resolution rules

The skill ships with these defaults for shared infrastructure files:

| File | Resolution |
|---|---|
| `pyproject.toml` | Start from `origin/main`, port the PR's unique additions (deps/overrides/omits) |
| `app/db/repositories/__init__.py` | Take `origin/main` (canonical re-export list) |
| `app/db/session.py` | Take `origin/main` |
| `app/pipeline/{audio,llm,transcribe,templates}/__init__.py` | Take `origin/main` |
| `app/storage/__init__.py` | Take `origin/main` |
| Any PR-specific new file | Keep the PR's version |

PRs with conflicts in files outside this list get escalated rather than
guessed.

## When to use

- After a batch of merges left several open PRs in "Conflicting" state
- You know the conflicts are in shared infrastructure files, not business
  logic
- Parallel rebasing beats serial (3 PRs × 5 min beats 1 × 15 min)

## When NOT to use

- A single PR with app-specific conflicts (use a code-reviewer subagent
  directly)
- Conflicts in business-logic files where the canonical rules don't apply
- Anything requiring user judgment on which side to keep

## Install

This plugin ships as part of the `toolbox` marketplace. Enable in your
settings:

```json
{
  "enabledPlugins": {
    "batch-resolve-conflicts@toolbox": true
  }
}
```

## History

Extracted from the voice-2-text-am project Phases 2/3/4 where we used
this exact pattern three separate times to recover from
post-batch-merge conflict avalanches.
