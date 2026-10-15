# Methodology-Aware Work-Item Types

AOK keeps task packets neutral while providing work-item templates suited to a
project's flow and overlays. Work-item scaffolding is local and does not create
or modify GitHub organization issue types, labels, Azure DevOps processes,
fields, or remote templates.

## Catalog

Every export contains Epic, Feature, Task, Bug, Spike, Incident, Risk, and
Change. Methodology additions are:

| Profile | Added types |
|---|---|
| XP | User Story |
| Lean | Improvement, Experiment |
| FDD | FDD Feature |
| DSDM | DSDM Requirement |
| AUP | Use Case, User Story, Non-Functional Requirement, Business Rule, Defect, Technical Work |

XP and Lean affect active Kanban selections. FDD, DSDM, and AUP remain
scaffolded flows and may be exported explicitly without selecting them. AM is
planned and has no work-item types.

Acceptance criteria remain embedded in each work item. FDD Subject Areas and
Business Activities, DSDM increments and timeboxes, and AUP iterations remain
planning containers rather than work-item types.

## Commands

```bash
aok work-item list
aok work-item list --flow kanban --overlay xp --overlay lean --json
aok work-item scaffold /path/to/project
aok work-item validate /path/to/project
```

Export a scaffolded flow without changing `.aok/project.json`:

```bash
aok work-item scaffold /path/to/project --flow fdd
aok work-item scaffold /path/to/project --flow dsdm --provider github
aok work-item scaffold /path/to/project --flow aup --provider azure-devops
```

`--provider` accepts `neutral`, `github`, `azure-devops`, or `all`; `all` is
the default. Generated output is recorded in `.aok/work-item-scaffold.json`.
Validation is offline and compares the catalog, selection, expected paths,
generated markers, and content hashes.

Repeated scaffolding is byte-identical. A modified or conflicting generated
path causes the command to fail before changing any output. Use `--force` only
when replacement is intentional; AOK writes recoverable timestamped backups.
Conflicting scaffold manifests are also backed up before forced replacement,
and their recorded file lists are not used to delete existing files. Manifest
symlinks and manifest locations outside the target project are rejected.

## Outputs

- Neutral Markdown: `agents/tasks/types/aok-<type>.md`
- GitHub Issue Forms: `.github/ISSUE_TEMPLATE/aok-<type>.yml`
- GitHub desired state: `docs/aok/work-items/github/desired-state.json`
- Azure guidance: `docs/aok/work-items/azure-devops/aok-<type>.md`
- Azure desired state: `docs/aok/work-items/azure-devops/desired-state.json`

AOK does not create `.github/ISSUE_TEMPLATE/config.yml` or a pull-request
template.

GitHub forms preserve the exact AOK type in their content and map it to the
built-in organization type `Feature`, `Task`, or `Bug`. They do not depend on
labels. Organization administrators remain responsible for ensuring those
types are enabled.

Azure output is desired-state guidance, not an installable repository
configuration. Requirement-like types use the verified binding's default
requirement work-item type when available; otherwise the output retains the
`$defaultRequirementWorkItemType` placeholder. Active inherited-process
customization is deferred.

## Safety and Compatibility

- `workItemType` is optional in the neutral task-packet schema.
- Existing required packet fields and status/phase enums are unchanged.
- Work-item scaffolding never changes methodology selection, lifecycle state,
  work-system bindings, or remote resources.
- Provider conformance remains separate from offline scaffold validation.
