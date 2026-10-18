# Repo delivery

`repo-delivery` is the Python engine embedded in AOK's repository-delivery pack for repeatable repository delivery. The local foundation supplies Git inventory, private preservation checkpoints, verification, and comparison against a pinned baseline. [Delivery workflows](delivery.md) add isolation and port plans, snapshot-bound gate and review evidence, optional GitHub operations, and local cleanup plans. [Planning and optional integrations](integrations.md) add architecture-to-backlog guidance, integration proof, and explicit adapter/companion boundaries. Read the [design](design.md) for ownership and preservation boundaries, and the [roadmap](roadmap.md) for milestone scope.

## Requirements and invocation

Use Python 3.11 or later and Git. From this pack's directory, invoke the bundled script directly; installing the plugin into a host or connecting GitHub is not required to use the local helpers.

```bash
python3 -I scripts/repo-delivery --help
```

The following example creates a disposable repository under the system temporary directory. It does not modify another checkout. Keep the shell variables in the same session for all three blocks.

```bash
example_root=$(mktemp -d)
git init -q "$example_root/source"
git -C "$example_root/source" config user.name 'Repo Delivery Example'
git -C "$example_root/source" config user.email 'example@localhost'
printf 'baseline\n' > "$example_root/source/example.txt"
git -C "$example_root/source" add example.txt
git -C "$example_root/source" commit -qm 'Create example baseline'
printf 'local change\n' > "$example_root/source/example.txt"
python3 -I scripts/repo-delivery inspect --repo "$example_root/source"
```

Inventory is JSON evidence about this repository, its status, branches, and worktrees. Each worktree's uncommitted content needs its own checkpoint; a shared repository bundle does not preserve every worktree's files.

## Create and verify a checkpoint

```bash
python3 -I scripts/repo-delivery checkpoint create --repo "$example_root/source" --output "$example_root/checkpoint"
python3 -I scripts/repo-delivery checkpoint verify "$example_root/checkpoint"
```

Use a new output directory outside the source repository and its Git metadata. Tracked and untracked working content is collected alongside index and Git recovery material. Ignored files are excluded unless selected with repeated `--include-ignored PATH` arguments. Checkpoints may contain sensitive source content; keep them private and local.

Verification checks structure and recorded content hashes. It does not prove authenticity or live database consistency. See the [preservation boundaries](design.md#preservation-boundaries) for unsupported repository layouts and external data. Checkpoint creation never deletes original files. The [port workflow](delivery.md#preserve-and-port) transfers selected working content to a new destination; it does not provide a general restore CLI.

## Compare local content

```bash
python3 -I scripts/repo-delivery compare --repo "$example_root/source" --baseline HEAD
```

The comparison records the resolved baseline revision and compares actual local content with that revision. This includes untracked paths that may already exist at the baseline. Use the resulting evidence to distinguish equivalent content from useful changes before proposing a port or consolidation.

## Agent entrypoints

The [setup skill](../skills/repo-delivery-setup/SKILL.md) orients a target repository without altering its configuration. The [workspace inspection skill](../skills/repo-delivery-workspace-inspect/SKILL.md) gathers local preservation and comparison evidence. The [delivery guide](delivery.md) routes issue implementation, selected checkpoint ports, PR convergence, and workspace reconciliation to their dedicated skills. Helpers enforce recorded evidence and authorization; the host supplies implementation tools and optional GitHub credentials. The [planning and integration guide](integrations.md) routes architecture-to-backlog and real-integration-proof work. No skill supplies additional merge authority or creates a remote task scheduler; the optional hook companion requires a separate explicit installation.

## Development checks

From the project directory, create a development environment and install the pinned test and build dependencies:

```bash
python3.11 -m venv .venv
.venv/bin/pip install -r config/requirements-dev.txt
make quality build
```

`make quality` runs compilation, real Git fixture tests, coverage with an 80% minimum, and diff whitespace checks. `make build` produces a wheel and source distribution. The [validation record](validation.md) identifies locally executed checks; configured CI is not evidence of a remote run.
