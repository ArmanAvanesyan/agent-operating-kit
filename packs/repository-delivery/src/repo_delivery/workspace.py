"""Snapshot-bound, explicitly authorized isolated delivery and local cleanup."""
import json
import os
from pathlib import Path
import shutil
import tempfile

from .core import (DeliveryError, canonical, digest, file_record, git, inspect,
                   root, safe_path, source_path, source_snapshot, verify_checkpoint)


def _authorize(actions, action):
    if action not in actions:
        raise DeliveryError(f'Action is not authorized: {action}')


def _state(repo, workspace_only=False):
    info, index, entries, files = source_snapshot(root(repo), ())
    if workspace_only:
        info.pop('refs')
        info.pop('worktrees_base64')
    ignored = [(name, file_record(root(repo), name)[0]) for name in info['ignored']]
    return digest(canonical([info, digest(index), digest(entries),
                             [record for record, _ in files], ignored]))


def _revision(repo, baseline):
    return git(repo, 'rev-parse', '--verify', '--end-of-options', baseline + '^{commit}').decode().strip()


def _destination(repo, destination, branch):
    requested = Path(destination).absolute()
    dest = requested.parent.resolve() / requested.name
    if not dest.parent.is_dir() or dest.exists() or dest.is_symlink():
        raise DeliveryError('Destination must be new, with canonical existing parent')
    common = Path(os.fsdecode(git(repo, 'rev-parse', '--git-common-dir')).strip())
    common = (root(repo) / common).resolve()
    if (root(repo) == dest or root(repo) in dest.parents
            or common == dest or common in dest.parents):
        raise DeliveryError('Destination must be outside source repository and Git storage')
    if branch.startswith('-') or git(repo, 'check-ref-format', '--branch', branch, optional=True) is None:
        raise DeliveryError('Invalid delivery branch')
    return dest


def _clone(repo, revision, destination, branch):
    # Reserve exclusively before invoking Git; never clean up another owner's path.
    try:
        destination.mkdir(mode=0o700)
    except FileExistsError as exc:
        raise DeliveryError('Destination appeared before clone') from exc
    try:
        git(repo, '-c', 'core.hooksPath=/dev/null', 'clone', '--no-local', '--no-checkout',
            '--', str(repo), str(destination))
        git(destination, '-c', 'core.hooksPath=/dev/null', 'fetch', '--no-tags', 'origin', revision)
        git(destination, '-c', 'core.hooksPath=/dev/null', 'checkout', '-b', branch, revision)
    except BaseException:
        shutil.rmtree(destination)
        raise


def isolate(repo, baseline, destination, branch, authorized_actions=()):
    """Clone the pinned commit without touching source working/index state."""
    _authorize(authorized_actions, 'isolate')
    repo = root(repo)
    dest = _destination(repo, destination, branch)
    revision, before = _revision(repo, baseline), _state(repo)
    created = False
    try:
        _clone(repo, revision, dest, branch)
        created = True
        if _state(repo) != before:
            raise DeliveryError('Source changed during isolation')
    except BaseException:
        if created:
            shutil.rmtree(dest)
        raise
    return {'schema_version': 1, 'kind': 'isolated-checkout', 'repository': str(repo),
            'destination': str(dest), 'baseline_revision': revision, 'branch': branch}


def _seal(plan):
    plan['plan_sha256'] = digest(canonical(plan))
    return plan


def _check(plan, kind):
    body = {k: v for k, v in plan.items() if k != 'plan_sha256'}
    if plan.get('kind') != kind or plan.get('plan_sha256') != digest(canonical(body)):
        raise DeliveryError('Invalid or modified plan')
    if _state(plan['repository']) != plan['source_sha256']:
        raise DeliveryError('Source changed since plan')


def _tree(repo, revision):
    result = {}
    if revision is None:
        return result
    for item in git(repo, 'ls-tree', '-rz', revision).split(b'\0'):
        if not item:
            continue
        meta, path = item.split(b'\t', 1)
        mode, kind, oid = meta.split()
        if kind != b'blob':
            raise DeliveryError('Submodule ports are unsupported')
        name = os.fsdecode(path)
        safe_path(name)
        data = git(repo, 'cat-file', 'blob', oid.decode())
        result[name] = {'type': 'symlink' if mode == b'120000' else 'file',
                        'sha256': digest(data), 'executable': mode == b'100755'}
    return result


def _value(record):
    if not record or record['type'] == 'missing':
        return None
    return {'type': record['type'], 'sha256': record['sha256'],
            'executable': record['type'] == 'file' and bool(record['mode'] & 0o111)}


def plan_port(checkpoint, repo, baseline, destination, branch, selected=None):
    """Compare retained working bytes against both archive HEAD and pinned baseline."""
    repo = root(repo)
    dest = _destination(repo, destination, branch)
    checkpoint = Path(checkpoint).resolve()
    if checkpoint == dest or checkpoint in dest.parents:
        raise DeliveryError('Port destination must be outside checkpoint archive')
    receipt = verify_checkpoint(checkpoint)
    manifest = json.loads((checkpoint / 'manifest.json').read_text())
    records = {item['path']: item for item in manifest['files']}
    revision, before = _revision(repo, baseline), _state(repo)
    baseline_tree = _tree(repo, revision)
    with tempfile.TemporaryDirectory() as temp:
        store = Path(temp)
        git(store, 'init', '--bare')
        bundle = checkpoint / 'repository.bundle'
        if bundle.exists():
            git(store, 'bundle', 'unbundle', str(bundle))
        old_tree = _tree(store, manifest['inventory']['head'])
    names = set(records) | set(old_tree)
    if selected is not None:
        selected = set(selected)
        for name in selected:
            safe_path(name)
        if not selected <= names:
            raise DeliveryError('Selected path is absent from checkpoint')
    entries = []
    for name in sorted(names):
        local = _value(records.get(name))
        old, base = old_tree.get(name), baseline_tree.get(name)
        if selected is not None and name not in selected:
            state = 'omitted'
        elif local == base:
            state = 'identical'
        elif local == old:
            state = 'unchanged-local'
        elif base != old:
            state = 'conflict'
        else:
            state = 'delete' if local is None else 'write'
        entries.append({'path': name, 'state': state})
    if _state(repo) != before:
        raise DeliveryError('Source changed during planning')
    return _seal({'schema_version': 1, 'kind': 'port-plan', 'repository': str(repo),
                  'source_sha256': before, 'checkpoint': str(checkpoint),
                  'manifest_sha256': receipt['manifest_sha256'], 'baseline_revision': revision,
                  'destination': str(dest), 'branch': branch, 'entries': entries})


def apply_port(plan, authorized_actions=()):
    """Apply selected conflict-free working changes into a new isolated clone."""
    _authorize(authorized_actions, 'port')
    _check(plan, 'port-plan')
    checkpoint = Path(plan['checkpoint'])
    if verify_checkpoint(checkpoint)['manifest_sha256'] != plan['manifest_sha256']:
        raise DeliveryError('Checkpoint changed since plan')
    if any(item['state'] == 'conflict' for item in plan['entries']):
        raise DeliveryError('Conflicting retained changes require a new explicit selection')
    repo = root(plan['repository'])
    dest = _destination(repo, plan['destination'], plan['branch'])
    if checkpoint.resolve() == dest or checkpoint.resolve() in dest.parents:
        raise DeliveryError('Port destination must be outside checkpoint archive')
    manifest = json.loads((checkpoint / 'manifest.json').read_text())
    records = {item['path']: item for item in manifest['files']}
    created = False
    try:
        _clone(repo, plan['baseline_revision'], dest, plan['branch'])
        created = True
        for item in plan['entries']:
            if item['state'] not in ('write', 'delete'):
                continue
            target = source_path(dest, item['path'])
            if target.is_dir() and not target.is_symlink():
                raise DeliveryError('Directory/file collision in port')
            if target.exists() or target.is_symlink():
                target.unlink()
            if item['state'] == 'delete':
                continue
            record = records[item['path']]
            target.parent.mkdir(parents=True, exist_ok=True)
            if record['type'] == 'symlink':
                target.symlink_to(record['target'])
            else:
                source = source_path(checkpoint / 'files', item['path'])
                target.write_bytes(source.read_bytes())
                target.chmod(record['mode'])
        _check(plan, 'port-plan')
        if verify_checkpoint(checkpoint)['manifest_sha256'] != plan['manifest_sha256']:
            raise DeliveryError('Checkpoint changed during port')
    except BaseException:
        if created:
            shutil.rmtree(dest)
        raise
    return {'schema_version': 1, 'kind': 'port-receipt', 'destination': str(dest),
            'baseline_revision': plan['baseline_revision'], 'plan_sha256': plan['plan_sha256']}


def _worktrees(repo):
    items = {}
    for block in os.fsdecode(git(repo, 'worktree', 'list', '--porcelain', '-z')).split('\0\0'):
        fields = dict(line.split(' ', 1) if ' ' in line else (line, '')
                      for line in block.split('\0') if line)
        if 'worktree' in fields:
            items[str(Path(fields['worktree']).resolve())] = fields
    return items


def _merged(repo, revision, baseline):
    return git(repo, 'merge-base', '--is-ancestor', revision, baseline, optional=True) is not None


def plan_cleanup(repo, branches=(), worktrees=()):
    """Plan only explicitly named local refs/worktrees; retain dirty and unmerged work."""
    repo = root(repo)
    before = _state(repo)
    baseline = _revision(repo, 'HEAD')
    trees = _worktrees(repo)
    entries = []
    occupied = {item.get('branch') for item in trees.values()}
    for branch in branches:
        if branch.startswith('-') or git(repo, 'check-ref-format', '--branch', branch, optional=True) is None:
            raise DeliveryError('Invalid branch')
        ref = 'refs/heads/' + branch
        revision = _revision(repo, ref)
        state = ('retained-checked-out' if ref in occupied else
                 'delete' if _merged(repo, revision, baseline) else 'retained-unmerged')
        entries.append({'type': 'branch', 'name': branch, 'revision': revision, 'state': state})
    for name in worktrees:
        path = str(Path(name).resolve())
        if path not in trees:
            raise DeliveryError('Worktree is not registered')
        info = inspect(path)
        dirty = info['status_base64'] or info['ignored']
        state = ('retained-primary' if path == next(iter(trees)) or path == str(repo) else
                 'retained-locked' if 'locked' in trees[path] else
                 'retained-dirty' if dirty else
                 'delete' if info['head'] and _merged(repo, info['head'], baseline) else 'retained-unmerged')
        entries.append({'type': 'worktree', 'name': path, 'revision': info['head'],
                        'source_sha256': _state(path, workspace_only=True), 'state': state})
    if _state(repo) != before:
        raise DeliveryError('Source changed during planning')
    return _seal({'schema_version': 1, 'kind': 'cleanup-plan', 'repository': str(repo),
                  'source_sha256': before, 'baseline_revision': baseline, 'entries': entries})


def apply_cleanup(plan, authorized_actions=()):
    """Revalidate the entire plan before non-forced local Git deletion."""
    _authorize(authorized_actions, 'cleanup')
    _check(plan, 'cleanup-plan')
    branches = [x['name'] for x in plan['entries'] if x['type'] == 'branch']
    worktrees = [x['name'] for x in plan['entries'] if x['type'] == 'worktree']
    if plan_cleanup(plan['repository'], branches, worktrees) != plan:
        raise DeliveryError('Cleanup targets changed since plan')
    completed = []
    for item in plan['entries']:
        if item['state'] != 'delete':
            continue
        if item['type'] == 'branch':
            # No -D: Git independently checks merged status again.
            if _revision(plan['repository'], 'refs/heads/' + item['name']) != item['revision']:
                raise DeliveryError('Branch changed during cleanup')
            git(plan['repository'], 'branch', '-d', '--', item['name'])
            if git(plan['repository'], 'show-ref', '--verify', '--quiet', 'refs/heads/' + item['name'], optional=True) is not None:
                raise DeliveryError('Branch deletion was not confirmed')
        else:
            if _state(item['name'], workspace_only=True) != item['source_sha256']:
                raise DeliveryError('Worktree changed during cleanup')
            git(plan['repository'], 'worktree', 'remove', '--', item['name'])
            if item['name'] in _worktrees(plan['repository']) or Path(item['name']).exists():
                raise DeliveryError('Worktree removal was not confirmed')
        completed.append(item['name'])
    return {'schema_version': 1, 'kind': 'cleanup-receipt', 'plan_sha256': plan['plan_sha256'],
            'completed': completed}
