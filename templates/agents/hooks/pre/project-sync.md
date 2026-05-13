# pre_project_sync

Load the latest GitHub Project context before work starts.

## Inputs

- Project item URL or ID
- linked issue or PR
- target repo, if known

## Steps

1. Fetch the Project item and linked issue.
2. Fetch linked PRs, comments, labels, assignees, and status.
3. Capture current Project fields.
4. Update the task packet with latest state.

## Pass Output

- current task packet
- issue/PR links
- status and blockers
- latest relevant comments

## Fail Output

- missing repo/project/item identifier
- connector or `gh` error
- ambiguous target item
