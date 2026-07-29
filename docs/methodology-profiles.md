# Methodology Profiles

AOK keeps the work system, flow framework, and engineering practices separate:

```text
Work system:       GitHub or Azure DevOps
Flow:              exactly one
Practice overlays: zero or more
```

The work system maps neutral fields and states. Methodology changes planning,
WIP, role emphasis, and hook expectations without changing the task-packet
schema or the `Inbox` through `Done` status model.

## Support

| Profile | Kind | Support |
|---|---|---|
| Kanban | flow | active, default |
| XP | overlay | active |
| Lean | overlay | active |
| FDD | flow | scaffolded |
| DSDM | flow | scaffolded |
| AUP | flow | scaffolded |
| AM | overlay | planned |

Only active profiles can be selected. Scaffolded and planned profiles expose
validated catalog metadata but do not render operational policy.

## Initialize a Project

Kanban with no overlays is the default:

```bash
aok project init /path/to/project
```

Select active overlays by repeating `--overlay`:

```bash
aok project init /path/to/project \
  --flow kanban \
  --overlay xp \
  --overlay lean
```

An existing explicit selection is preserved when init receives no methodology
flags. Providing `--overlay` without `--flow` is rejected.

## Reconfigure

```bash
aok methodology set /path/to/project \
  --flow kanban \
  --overlay xp \
  --overlay lean

aok methodology show /path/to/project
aok methodology validate /path/to/project
aok methodology list
```

`methodology set` replaces the complete selection. Omitting `--overlay` clears
all overlays. Overlay order is canonical and does not depend on flag order.

The selection is stored in `.aok/project.json`:

```json
{
  "methodology": {
    "flow": "kanban",
    "overlays": ["xp", "lean"]
  }
}
```

The rendered policy is `agents/methodology.md`. It is generated from the flow
fragment followed by overlay fragments. Do not edit it directly.

## Safety and Migration

AOK refuses to overwrite an unmarked or manually changed methodology policy.
Use `--force` only when the generated file should be replaced; AOK writes a
timestamped backup first.

Projects without methodology metadata remain valid as legacy implicit
Kanban-without-overlays projects. Run project init again or use `methodology
set` to materialize an explicit selection and generated policy.

Methodology commands preserve work-system bindings and unrelated manifest
keys. Work-system validation remains independent.
