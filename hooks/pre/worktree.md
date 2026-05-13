# pre_worktree

Protect local work and enforce branch discipline before edits.

## Checks

- run inside the target repo, not an unrelated workspace root
- verify current branch
- do not work directly on `main`
- inspect dirty files
- identify user changes that must not be overwritten
- confirm target issue and intended branch name

## Suggested Commands

```bash
pwd
git status --short
git branch --show-current
git remote -v
```

## Output

```markdown
Repo:
Branch:
Dirty Files:
User Changes To Preserve:
Branch Action Needed:
```
