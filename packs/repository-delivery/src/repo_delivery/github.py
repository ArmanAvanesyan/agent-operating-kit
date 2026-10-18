"""Explicit GitHub CLI boundary. No implicit push, auto-merge or branch deletion."""
import json
import os
import re
import subprocess
from urllib.parse import quote
from .core import DeliveryError, git

PR_FIELDS = ('number,url,state,headRefOid,headRefName,baseRefName,isDraft,'
             'mergeStateStatus,reviewDecision,statusCheckRollup')


def _repository(value):
    if not isinstance(value, str) or not re.fullmatch(r'[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+', value):
        raise DeliveryError('GitHub repository must be OWNER/REPO')
    return value


def _number(number):
    if type(number) is not int or number < 1:
        raise DeliveryError('Issue or pull request number must be positive')
    return str(number)


def _gh(args, *, data=None, parse=True, cwd=None):
    try:
        result = subprocess.run(['gh', *args], input=data, text=True, capture_output=True,
                                cwd=cwd, env=dict(os.environ, GH_PROMPT_DISABLED='1'))
    except OSError as exc:
        raise DeliveryError('GitHub CLI is unavailable; install gh on PATH and authenticate on the host before using GitHub operations') from exc
    if result.returncode:
        raise DeliveryError('GitHub operation failed; reconcile remote state before retrying')
    if not parse:
        return result.stdout.strip()
    try:
        return json.loads(result.stdout)
    except (ValueError, TypeError) as exc:
        raise DeliveryError('GitHub returned invalid JSON') from exc


def inspect_issue(repository, number):
    return _gh(['issue', 'view', _number(number), '--repo', _repository(repository),
                '--json', 'number,url,title,body,state,labels,assignees'])


def inspect_pr(repository, number):
    return _gh(['pr', 'view', _number(number), '--repo', _repository(repository),
                '--json', PR_FIELDS])


def readiness(pr, required_checks, require_review=True):
    """An explicit empty check list opts out; omitted policy never passes."""
    reasons = []
    if not isinstance(required_checks, list) or any(not isinstance(x, str) or not x for x in required_checks):
        reasons.append('Required check policy must be an explicit list')
        required_checks = []
    if pr.get('state') != 'OPEN':
        reasons.append('Pull request is not open')
    if pr.get('isDraft') is not False:
        reasons.append('Pull request is draft or draft state is unknown')
    if pr.get('mergeStateStatus') not in ('CLEAN', 'HAS_HOOKS'):
        reasons.append('Pull request mergeability is not clean')
    if require_review and pr.get('reviewDecision') != 'APPROVED':
        reasons.append('GitHub review approval is missing')
    checks = {}
    for item in pr.get('statusCheckRollup') or []:
        if 'context' in item:
            name, passed = item['context'], item.get('state') == 'SUCCESS'
        else:
            name = item.get('name')
            passed = item.get('status') == 'COMPLETED' and item.get('conclusion') == 'SUCCESS'
        checks.setdefault(name, []).append(passed)
    for name in required_checks:
        if not checks.get(name) or not all(checks[name]):
            reasons.append(f'Required check has not passed: {name}')
    return {'ready': not reasons, 'reasons': reasons}


def _authorize(context, action):
    if action not in context.get('authorized_actions', []):
        raise DeliveryError(f'Action is not authorized: {action}')


def _binding(context, repository):
    from .evidence import validate_context
    validate_context(context)
    if context.get('github_repository') != _repository(repository):
        raise DeliveryError('GitHub repository does not match the task context')


def _terminal_identity(context, gates, pr):
    from .evidence import snapshot, validate_seal
    validate_seal(gates, 'receipt_sha256')
    current = snapshot(context['repository'], context['include_ignored'])
    if (gates.get('kind') not in ('gate-receipt', 'integration-proof')
            or gates.get('context_sha256') != context['context_sha256']
            or gates.get('snapshot_sha256') != current['snapshot_sha256']):
        raise DeliveryError('Merged pull request is not bound to this task evidence')
    required_mode = context.get('integration_required_mode')
    if required_mode is not None and (gates.get('kind') != 'integration-proof' or gates.get('mode') != required_mode or gates.get('passed') is not True):
        raise DeliveryError('Merged task requires matching integration evidence')
    if pr.get('headRefOid') != _local_head(context):
        raise DeliveryError('Merged pull request head does not match this task')


def _assert_ready(context, gates, review):
    from .evidence import assert_ready
    return assert_ready(context, gates, review)


def _local_head(context):
    repo = context['repository']
    if git(repo, 'status', '--porcelain=v1', '-z', '--untracked-files=all'):
        raise DeliveryError('Commit the tested sources and rerun evidence before remote mutation')
    return git(repo, 'rev-parse', '--verify', 'HEAD').decode().strip()


def open_pr(context, gate_receipt, review_receipt, repository, title, body, base, head):
    """Create once, or reconcile a same-head PR. Requires an already pushed head."""
    _authorize(context, 'open-pr')
    _binding(context, repository)
    tested = _assert_ready(context, gate_receipt, review_receipt)
    local = _local_head(context)
    if tested.get('head') != local:
        raise DeliveryError('Local HEAD changed after evidence verification')
    if not all(isinstance(x, str) and x and not x.startswith('-') for x in (base, head)):
        raise DeliveryError('Explicit base and head branches are required')
    if ':' in head:
        raise DeliveryError('Cross-repository PR heads are not supported')
    remote = _gh(['api', f'repos/{repository}/git/ref/heads/{quote(head, safe="")}'])
    if remote.get('object', {}).get('sha') != local:
        raise DeliveryError('Remote branch does not match tested local HEAD')
    existing = _gh(['pr', 'list', '--repo', repository, '--head', head, '--base', base,
                    '--state', 'all', '--json', PR_FIELDS])
    matches = [p for p in existing if p.get('headRefOid') == local]
    if len(matches) > 1:
        raise DeliveryError('Ambiguous existing pull requests')
    if matches:
        if matches[0].get('state') == 'CLOSED':
            raise DeliveryError('Existing pull request is closed')
        return {'state': 'already-complete' if matches[0].get('state') == 'MERGED' else 'existing',
                'pull_request': matches[0]}
    if existing:
        raise DeliveryError('Existing pull request has a different head; reconcile first')
    remote = _gh(['api', f'repos/{repository}/git/ref/heads/{quote(head, safe="")}'])
    if remote.get('object', {}).get('sha') != local:
        raise DeliveryError('Remote branch changed; reconcile before creating a pull request')
    _assert_ready(context, gate_receipt, review_receipt)
    # Body is stdin to preserve arbitrary newlines and avoid process argv disclosure.
    url = _gh(['pr', 'create', '--repo', repository, '--base', base, '--head', head,
               '--title', title, '--body-file', '-'], data=body, parse=False,
              cwd=context['repository'])
    if not re.fullmatch(r'https://github\.com/' + re.escape(repository) + r'/pull/[1-9][0-9]*', url):
        raise DeliveryError('Created pull request URL is unexpected; reconcile remote state before retrying')
    created = _gh(['pr', 'view', url, '--repo', repository, '--json', PR_FIELDS])
    if (created.get('headRefOid') != local or created.get('headRefName') != head
            or created.get('baseRefName') != base or created.get('state') != 'OPEN'):
        raise DeliveryError('Created pull request differs from tested task; reconcile remote state before retrying')
    return {'state': 'created', 'url': url, 'pull_request': created}


def merge_pr(context, gates, review, repository, number, required_checks):
    """Recheck remote readiness, then merge with a server-side tested-head fence."""
    _authorize(context, 'merge')
    _binding(context, repository)
    pr = inspect_pr(repository, number)
    if pr.get('state') == 'MERGED':
        _terminal_identity(context, gates, pr)
        return {'state': 'already-complete', 'pull_request': pr}
    tested = _assert_ready(context, gates, review)
    local = _local_head(context)
    if tested.get('head') != local:
        raise DeliveryError('Local HEAD changed after evidence verification')
    if pr.get('headRefOid') != local:
        raise DeliveryError('Pull request head does not match tested local HEAD')
    result = readiness(pr, required_checks, require_review=context.get('github_review_required', True))
    if not result['ready']:
        raise DeliveryError('; '.join(result['reasons']))
    _assert_ready(context, gates, review)
    _gh(['pr', 'merge', _number(number), '--repo', repository, '--merge',
         '--match-head-commit', local], parse=False)
    current = inspect_pr(repository, number)
    if current.get('state') != 'MERGED':
        raise DeliveryError('Merge is not confirmed; reconcile remote state before retrying')
    return {'state': 'merged', 'pull_request': current}
