# Work-System Bootstrap

AOK separates local project initialization from remote work-system setup:

```bash
aok project init /path/to/project
aok work-system setup github --owner OWNER --project-number 12 --path /path/to/project
aok work-system validate /path/to/project
```

## Support Levels

- `planned`: the provider contract is designed but has no CLI implementation.
- `scaffolded`: AOK performs live read-only discovery, prints an idempotent
  desired-state plan, validates an existing work system, and can record a
  verified local binding.
- `active`: AOK can create or update remote work-system resources.

GitHub Projects and Azure DevOps are currently `scaffolded`. The commands do not
create or update remote projects, fields, states, columns, issues, work items,
or policies. There is intentionally no `--apply` option.

## Neutral Contract

Every provider must represent:

- Status: `Inbox`, `Ready`, `In Progress`, `QA`, `Review`, `Blocked`, `Done`
- Phase: `Intake`, `Planning`, `Build`, `QA`, `Review`, `Release`, `Operate`
- Priority: `Critical`, `High`, `Medium`, `Low`
- Text: Primary Agent, QA Agent, Target Repo, PR URL, Evidence URL, Blocker
  Reason, Next Handoff
- Number: Attempt

## GitHub Projects

Authenticate with `gh auth login`, then discover by number or exact title:

```bash
aok work-system setup github \
  --owner example-org \
  --project-number 12 \
  --json

aok work-system setup github \
  --owner example-org \
  --project-title "Agent Work" \
  --path /path/to/project \
  --write-binding
```

Status, Phase, and Priority must be single-select fields, Attempt must be a
number, and the remaining fields must be text. Exact duplicate project, field,
or option names are nonconformant.

## Azure DevOps

Install the Azure DevOps CLI extension and authenticate with Microsoft Entra,
`az devops login`, or `AZURE_DEVOPS_EXT_PAT`. AOK never reads or writes the
token itself.

```bash
aok work-system setup azure-devops \
  --organization https://dev.azure.com/example \
  --project Platform \
  --team "Platform Team" \
  --board Stories \
  --path /path/to/project \
  --write-binding
```

The project must use an inherited or custom process. AOK discovers the default
requirement backlog work-item type, or accepts `--work-item-type` by name or
reference name. Neutral Status maps to process states and one-to-one board
columns. Phase and Priority are picklists, Attempt is an integer, and the
remaining fields are strings.

State categories are:

- Inbox and Ready: Proposed
- In Progress, QA, Review, and Blocked: InProgress
- Done: Completed

System processes are reported as blocking mismatches; this scaffold never
creates an inherited process.

## Verified Binding

`--write-binding` succeeds only when every live check passes. It adds a
`workSystem` object to `.aok/project.json` containing provider resource IDs,
URLs, field/option mappings, and a verification timestamp. It never stores
credentials. A failed check leaves the file unchanged.

`aok project init` preserves an existing binding. Re-run live validation with:

```bash
aok work-system validate /path/to/project
aok work-system validate /path/to/project --json
```

Validation fails when authentication is unavailable, a bound resource cannot
be found, provider IDs or mappings drift, or the neutral contract is no longer
satisfied.

Remote create/ensure operations, sample items, and PR policy setup are deferred
until a provider reaches `active`.
