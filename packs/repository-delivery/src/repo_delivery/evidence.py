"""Snapshot-bound local delivery contracts and gate/review receipts.

Receipts attest local observations; the harness owns reviewer identity and permissions.
"""
import os
from pathlib import Path
import platform
import re
import signal
import subprocess
import sys
import time

from .core import (DeliveryError, canonical, compare, digest, git, root,
                   safe_path, source_snapshot, verify_checkpoint)

ACTIONS = {'isolate', 'port', 'cleanup', 'commit', 'push', 'open-pr', 'merge', 'update-backlog', 'install-hook', 'finalize-attempt'}


def require(condition, message):
    if not condition:
        raise DeliveryError(message)


def snapshot(repo, include_ignored=()):
    info, _, entries, files = source_snapshot(root(repo), include_ignored)
    record = {'schema_version': 1, 'kind': 'source-snapshot',
              'repository': info['repository'], 'head': info['head'],
              'index_sha256': digest(entries), 'files': [r for r, _ in files],
              'included_ignored': sorted(include_ignored)}
    return dict(record, snapshot_sha256=digest(canonical(record)))


def seal(record, field):
    return dict(record, **{field: digest(canonical(record))})


def validate_seal(record, field):
    require(isinstance(record, dict) and field in record, 'Missing evidence digest')
    payload = {key: value for key, value in record.items() if key != field}
    require(record[field] == digest(canonical(payload)), 'Evidence digest mismatch')


def new_context(repo, spec, baseline):
    require(isinstance(spec, dict), 'Task specification must be an object')
    allowed = {'objective', 'files_in_scope', 'gate_commands', 'review_policy',
               'authorized_actions', 'actor_id', 'actor_context', 'environment_keys',
               'include_ignored', 'preservation', 'gate_timeout_seconds', 'github_repository', 'github_review_required', 'integration_required_mode'}
    require(not set(spec) - allowed, 'Unknown task specification fields')
    for name in ('objective', 'actor_id', 'actor_context'):
        require(isinstance(spec.get(name), str) and bool(spec[name].strip()), 'Missing ' + name)
    scope = spec.get('files_in_scope')
    require(isinstance(scope, list) and bool(scope) and all(isinstance(n, str) for n in scope), 'Explicit file scope required')
    for name in scope:
        safe_path(name)
    gates = spec.get('gate_commands')
    require(isinstance(gates, list) and bool(gates), 'Repository gate commands required')
    for argv in gates:
        require(isinstance(argv, list) and bool(argv) and all(isinstance(a, str) and '\0' not in a for a in argv) and bool(argv[0]), 'Gates must be argv arrays')
    require(spec.get('review_policy') in ('independent', 'none'), 'Explicit review policy required')
    actions = spec.get('authorized_actions')
    require(isinstance(actions, list) and all(isinstance(a, str) and a in ACTIONS for a in actions), 'Invalid authorized actions')
    env_keys = spec.get('environment_keys', [])
    require(isinstance(env_keys, list) and all(isinstance(n, str) and re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', n) for n in env_keys), 'Invalid environment keys')
    ignored = spec.get('include_ignored', [])
    require(isinstance(ignored, list) and all(isinstance(n, str) for n in ignored), 'Invalid ignored selection')
    require(type(spec.get('github_review_required', True)) is bool, 'Invalid GitHub review policy')
    github_repository = spec.get('github_repository')
    require(github_repository is None or (isinstance(github_repository, str) and re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', github_repository)), 'Invalid GitHub repository')
    require(spec.get('integration_required_mode') in (None, 'real', 'mock', 'mixed'), 'Invalid required integration mode')
    timeout = spec.get('gate_timeout_seconds', 600)
    require(type(timeout) in (int, float) and 0 < timeout <= 86400, 'Invalid gate timeout')
    repo = root(repo)
    revision = git(repo, 'rev-parse', '--verify', '--end-of-options', baseline + '^{commit}').decode().strip()
    preservation = []
    require(isinstance(spec.get('preservation', []), list), 'Invalid preservation references')
    for path in spec.get('preservation', []):
        require(isinstance(path, str), 'Preservation paths must be strings')
        receipt = verify_checkpoint(path)
        preservation.append({'path': str(Path(path).resolve()), 'manifest_sha256': receipt['manifest_sha256']})
    result = {'schema_version': 1, 'kind': 'delivery-context', 'repository': str(repo),
              'baseline_revision': revision, 'objective': spec['objective'],
              'files_in_scope': sorted(set(scope)), 'gate_commands': gates,
              'review_policy': spec['review_policy'], 'authorized_actions': sorted(set(actions)),
              'actor_id': spec['actor_id'], 'actor_context': spec['actor_context'],
              'environment_keys': sorted(set(env_keys)), 'include_ignored': sorted(set(ignored)),
              'preservation': preservation, 'gate_timeout_seconds': timeout,
              'github_repository': github_repository, 'github_review_required': spec.get('github_review_required', True),
              'integration_required_mode': spec.get('integration_required_mode'),
              'initial_snapshot_sha256': snapshot(repo, ignored)['snapshot_sha256']}
    return seal(result, 'context_sha256')


def validate_context(context):
    validate_seal(context, 'context_sha256')
    require(context.get('kind') == 'delivery-context' and context.get('schema_version') == 1, 'Unsupported context')
    # Revalidate the public input contract rather than trusting a recomputed digest.
    spec = {key: context[key] for key in ('objective', 'files_in_scope', 'gate_commands',
            'review_policy', 'authorized_actions', 'actor_id', 'actor_context',
            'environment_keys', 'include_ignored', 'gate_timeout_seconds', 'github_repository', 'github_review_required')}
    spec['integration_required_mode'] = context.get('integration_required_mode')
    fresh = new_context(context['repository'], spec, context['baseline_revision'])
    require(fresh['repository'] == context['repository'], 'Repository identity changed')
    for item in context['preservation']:
        require(verify_checkpoint(item['path'])['manifest_sha256'] == item['manifest_sha256'], 'Preservation checkpoint changed')


def in_scope(context):
    changes = compare(context['repository'], context['baseline_revision'])['files']
    changed_paths = {r['path'] for r in changes if r['state'] != 'identical'}
    changed_paths.update(context['include_ignored'])
    from .core import paths
    changed_paths.update(paths(git(context['repository'], 'diff', '--cached', '--no-ext-diff', '--no-renames', '--name-only', '-z', context['baseline_revision'], '--')))
    outside = sorted(changed_paths - set(context['files_in_scope']))
    require(not outside, 'Changes outside declared file scope: ' + ', '.join(outside))


def environment(context):
    return {'platform': platform.platform(), 'python': sys.version,
            'git': git(context['repository'], '--version').decode().strip(),
            'selected_variables': {name: digest(canonical(os.environ.get(name)))
                                   for name in context['environment_keys']}}


def run_gates(context):
    validate_context(context)
    in_scope(context)
    before = snapshot(context['repository'], context['include_ignored'])
    env = environment(context)
    results = []
    for argv in context['gate_commands']:
        started = time.monotonic()
        try:
            # A new process group lets timeouts stop shell wrappers and child processes.
            process = subprocess.Popen(argv, cwd=context['repository'], stdout=subprocess.PIPE,
                                       stderr=subprocess.PIPE, start_new_session=True)
            timed_out = False
            try:
                stdout, stderr = process.communicate(timeout=context['gate_timeout_seconds'])
            except subprocess.TimeoutExpired:
                timed_out = True
                os.killpg(process.pid, signal.SIGKILL)
                stdout, stderr = process.communicate()
            code = process.returncode
        except OSError as exc:
            stdout, stderr, code, timed_out = b'', str(exc).encode(), 127, False
        results.append({'argv': argv, 'returncode': code, 'timed_out': timed_out,
                        'stdout_sha256': digest(stdout), 'stderr_sha256': digest(stderr),
                        'duration_seconds': round(time.monotonic() - started, 6)})
        if code or timed_out:
            break
    after = snapshot(context['repository'], context['include_ignored'])
    unchanged = before['snapshot_sha256'] == after['snapshot_sha256']
    passed = unchanged and env == environment(context) and len(results) == len(context['gate_commands']) and all(r['returncode'] == 0 and not r['timed_out'] for r in results)
    return seal({'schema_version': 1, 'kind': 'gate-receipt', 'context_sha256': context['context_sha256'],
                 'snapshot_sha256': before['snapshot_sha256'], 'environment': env,
                 'source_unchanged': unchanged, 'passed': passed, 'results': results}, 'receipt_sha256')


def record_review(context, review):
    validate_context(context)
    require(isinstance(review, dict) and set(review) == {'reviewer', 'reviewer_context', 'snapshot_sha256', 'outcome', 'report'}, 'Invalid review input')
    for key in ('reviewer', 'reviewer_context', 'report'):
        require(isinstance(review[key], str) and bool(review[key].strip()), 'Missing review ' + key)
    require(review['reviewer'] != context['actor_id'] and review['reviewer_context'] != context['actor_context'], 'Independent reviewer and context required')
    require(review['outcome'] in ('approved', 'changes-requested'), 'Invalid review outcome')
    require(review['snapshot_sha256'] == snapshot(context['repository'], context['include_ignored'])['snapshot_sha256'], 'Review snapshot is stale')
    return seal({'schema_version': 1, 'kind': 'review-receipt', 'context_sha256': context['context_sha256'], **review}, 'receipt_sha256')


def assert_ready(context, gates, review):
    validate_context(context)
    in_scope(context)
    current = snapshot(context['repository'], context['include_ignored'])
    require(isinstance(gates, dict), 'Gate receipt required')
    required_mode = context.get('integration_required_mode')
    if gates.get('kind') == 'integration-proof':
        from .integration import assert_integration
        assert_integration(context, gates)
        require(required_mode is None or gates.get('mode') == required_mode, 'Required integration evidence mode mismatch')
        gates = gates['gates']
    else:
        require(required_mode is None, 'Required integration proof is missing')
    validate_seal(gates, 'receipt_sha256')
    require(gates.get('kind') == 'gate-receipt' and gates.get('context_sha256') == context['context_sha256'], 'Gate context mismatch')
    require(gates.get('snapshot_sha256') == current['snapshot_sha256'], 'Gate receipt is stale')
    require(gates.get('environment') == environment(context), 'Gate environment is stale')
    require(gates.get('passed') is True and gates.get('source_unchanged') is True, 'Gates did not pass')
    results = gates.get('results')
    require(isinstance(results, list) and [r.get('argv') for r in results] == context['gate_commands'], 'Gate profile mismatch')
    require(all(r.get('returncode') == 0 and r.get('timed_out') is False for r in results), 'Gate failure')
    if context['review_policy'] == 'independent':
        require(isinstance(review, dict), 'Independent review receipt required')
        validate_seal(review, 'receipt_sha256')
        require(review.get('kind') == 'review-receipt' and review.get('context_sha256') == context['context_sha256'], 'Review context mismatch')
        input_review = {key: review.get(key) for key in ('reviewer', 'reviewer_context', 'snapshot_sha256', 'outcome', 'report')}
        record_review(context, input_review)
        require(review.get('outcome') == 'approved', 'Review changes requested')
    return current


def delivery_status(context, gates=None, review=None):
    validate_context(context)
    try:
        current = assert_ready(context, gates, review)
        reasons = []
    except DeliveryError as exc:
        current = snapshot(context['repository'], context['include_ignored'])
        reasons = [str(exc)]
    return {'schema_version': 1, 'kind': 'delivery-status', 'context_sha256': context['context_sha256'],
            'snapshot_sha256': current['snapshot_sha256'], 'ready': not reasons, 'reasons': reasons}
