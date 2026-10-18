"""Validate host-reasoned architecture gaps against actual source evidence.

This module does not infer meaning from research or implementation text. The host
supplies findings and observable checks; this module binds them to source bytes.
"""
import re

from .core import DeliveryError, digest, git, root, source_path
from .evidence import require, seal, snapshot, validate_seal
from . import github

ROLES = {'implementation', 'research', 'adr', 'roadmap'}
KEY = r'[a-z0-9][a-z0-9_-]{0,79}'


def _text(value, field):
    require(isinstance(value, str) and bool(value.strip()) and '\0' not in value,
            'Nonempty text required: ' + field)
    return value


def _marker(key):
    return f'<!-- repo-delivery:backlog:{key} -->'


def _end(key):
    return f'<!-- /repo-delivery:backlog:{key} -->'


def _issues(issues):
    require(isinstance(issues, (list, tuple)), 'Issues must be a list')
    numbers = set()
    for issue in issues:
        require(isinstance(issue, dict), 'Invalid issue record')
        github._number(issue.get('number'))
        require(issue['number'] not in numbers, 'Duplicate issue number')
        numbers.add(issue['number'])
        require(isinstance(issue.get('body'), str) and isinstance(issue.get('title'), str), 'Missing issue text')
        require(issue.get('state') in ('OPEN', 'CLOSED'), 'Missing issue state')
    return issues


def _match(item, issues):
    numbered = [i for i in issues if i['number'] == item.get('issue_number')]
    marked = [i for i in issues if _marker(item['key']) in i['body']]
    require(len(marked) <= 1, 'Ambiguous stable issue marker')
    if item.get('issue_number') is not None:
        require(bool(numbered), 'Explicit issue number is absent')
        require(not marked or marked[0]['number'] == item['issue_number'], 'Issue number conflicts with marker')
        return numbered[0]
    return marked[0] if marked else None


def _render(item, numbers=None):
    lines = [_marker(item['key']), item['summary'], '', '## Acceptance criteria']
    for criterion in item['acceptance_criteria']:
        lines.append(f"- [ ] {criterion['action']} → {criterion['expected']}")
    lines.extend(['', '## Dependencies'])
    for key in item['depends_on']:
        lines.append(f'- Depends on #{numbers[key]}' if numbers else f'- Depends on `{key}`')
    if not item['depends_on']:
        lines.append('- None')
    lines.extend(['', '## Source evidence'])
    lines.extend(f'- `{p}`' for p in item['evidence'])
    lines.append(_end(item['key']))
    return '\n'.join(lines)


def plan_backlog(repo, spec, existing_issues=()):
    require(isinstance(spec, dict), 'Planning specification must be an object')
    require(not set(spec) - {'github_repository', 'baseline', 'sources', 'items'}, 'Unknown planning fields')
    repository = github._repository(spec.get('github_repository'))
    repo = root(repo)
    baseline = _text(spec.get('baseline'), 'baseline')
    baseline = git(repo, 'rev-parse', '--verify', '--end-of-options', baseline+'^{commit}').decode().strip()
    current = snapshot(repo)
    sources = spec.get('sources')
    require(isinstance(sources, list) and bool(sources), 'Source evidence required')
    roles, names = set(), set()
    for source in sources:
        require(isinstance(source, dict) and set(source) == {'role', 'path', 'sha256'}, 'Invalid source record')
        require(isinstance(source['role'], str) and source['role'] in ROLES, 'Unknown source role')
        name = _text(source['path'], 'source path')
        path = source_path(repo, name)
        require(path.is_file() and not path.is_symlink(), 'Evidence must be a regular source file')
        require(source['sha256'] == digest(path.read_bytes()), 'Source evidence hash mismatch')
        require((source['role'], name) not in names, 'Duplicate source evidence')
        names.add((source['role'], name)); roles.add(source['role'])
        require(any(f['path'] == name for f in current['files']), 'Source is excluded from snapshot')
    require(roles == ROLES, 'Implementation, research, ADR and roadmap sources required')
    items = spec.get('items')
    require(isinstance(items, list) and bool(items), 'Actionable backlog items required')
    normalized, keys = [], set()
    issues = _issues(existing_issues)
    for item in items:
        require(isinstance(item, dict) and not set(item) - {'key', 'title', 'summary', 'acceptance_criteria', 'evidence', 'depends_on', 'issue_number'}, 'Invalid backlog item')
        key = item.get('key')
        require(isinstance(key, str) and bool(re.fullmatch(KEY, key)) and key not in keys, 'Invalid or duplicate item key')
        keys.add(key)
        _text(item.get('title'), 'title'); _text(item.get('summary'), 'summary')
        require('<!--' not in item['summary'], 'Reserved marker in summary')
        criteria = item.get('acceptance_criteria')
        require(isinstance(criteria, list) and bool(criteria), 'Observable acceptance criteria required')
        for criterion in criteria:
            require(isinstance(criterion, dict) and set(criterion) == {'action', 'expected'}, 'Criteria require action and expected observation')
            for field in ('action', 'expected'):
                _text(criterion[field], field)
                require('<!--' not in criterion[field], 'Reserved marker in criterion')
        evidence = item.get('evidence')
        require(isinstance(evidence, list) and bool(evidence) and all(isinstance(p, str) and any(n == p for _, n in names) for p in evidence), 'Unknown item source evidence')
        deps = item.get('depends_on')
        require(isinstance(deps, list) and all(isinstance(d, str) for d in deps) and len(deps) == len(set(deps)), 'Invalid dependencies')
        if item.get('issue_number') is not None:
            github._number(item['issue_number'])
        match = _match(item, issues)
        normalized.append(dict(item, issue_number=match['number'] if match else None,
                               action='reuse' if match and match['state'] == 'CLOSED' else 'update' if match else 'create',
                               body=_render(item)))
    require(all(d in keys and d != i['key'] for i in normalized for d in i['depends_on']), 'Unknown or self dependency')
    assigned = [i['issue_number'] for i in normalized if i['issue_number'] is not None]
    require(len(assigned) == len(set(assigned)), 'Multiple work items target the same issue')
    ordered, pending = [], list(normalized)
    while pending:
        ready = [i for i in pending if all(d in {v['key'] for v in ordered} for d in i['depends_on'])]
        require(bool(ready), 'Dependency cycle')
        for item in ready:
            ordered.append(item); pending.remove(item)
    require(snapshot(repo)['snapshot_sha256'] == current['snapshot_sha256'], 'Sources changed during planning')
    return seal(dict(schema_version=1, kind='backlog-plan', repository=str(repo),
                     github_repository=repository, baseline_revision=baseline,
                     snapshot_sha256=current['snapshot_sha256'], sources=sources, items=ordered), 'plan_sha256')


def _remote(repository):
    # --paginate is essential: truncating at CLI list defaults can create duplicates.
    result = github._gh(['api', '--paginate', '--slurp', f'repos/{repository}/issues?state=all&per_page=100'])
    require(isinstance(result, list) and all(isinstance(page, list) and all(isinstance(i, dict) for i in page) for page in result), 'Invalid remote issue response')
    return _issues([dict(i, body=i.get('body') or '', state=str(i.get('state', '')).upper())
                    for page in result for i in page if 'pull_request' not in i])


def _live_matches(plan, issues, numbers):
    matches = {item['key']: _match(item, issues) for item in plan['items']}
    observed = [match['number'] for match in matches.values() if match]
    require(len(observed) == len(set(observed)), 'Multiple work items target the same issue')
    for key, number in numbers.items():
        match = matches[key]
        require(match is not None and match['number'] == number,
                'Issue identity changed; reconcile before retrying')
    return matches


def _body(existing, item, numbers):
    start, end = _marker(item['key']), _end(item['key'])
    body = existing['body']
    require(body.count(start) <= 1 and body.count(end) <= 1, 'Ambiguous owned issue section')
    if start in body and end in body:
        a, b = body.index(start), body.index(end)
        require(b > a, 'Malformed owned issue section')
        return body[:a] + _render(item, numbers) + body[b+len(end):]
    require(end not in body, 'Malformed owned issue section')
    # A standalone discovery marker does not own surrounding human content.
    return body.replace(start, '').rstrip() + '\n\n' + _render(item, numbers)


def apply_backlog(plan, repository, authorized_actions=()):
    require('update-backlog' in authorized_actions, 'Action is not authorized: update-backlog')
    validate_seal(plan, 'plan_sha256')
    require(plan.get('kind') == 'backlog-plan' and plan.get('schema_version') == 1, 'Unsupported backlog plan')
    require(plan.get('github_repository') == github._repository(repository), 'GitHub repository binding mismatch')
    spec = dict(github_repository=repository, baseline=plan['baseline_revision'],
                sources=plan['sources'], items=[{k: v for k, v in i.items() if k not in ('action', 'body')} for i in plan['items']])
    require(snapshot(plan['repository'])['snapshot_sha256'] == plan['snapshot_sha256'], 'Planning snapshot is stale')
    # Structural validation is repeated even when a caller recomputes the digest.
    synthetic = [dict(number=i['issue_number'], title=i['title'], body=_marker(i['key']), state='OPEN')
                 for i in plan['items'] if i['issue_number'] is not None]
    plan_backlog(plan['repository'], spec, synthetic)
    numbers = {}
    for item in plan['items']:
        require(snapshot(plan['repository'])['snapshot_sha256'] == plan['snapshot_sha256'], 'Planning snapshot is stale')
        issues = _remote(repository)
        match = _live_matches(plan, issues, numbers)[item['key']]
        if match:
            numbers[item['key']] = match['number']
            if match['state'] == 'CLOSED':
                continue
            proposed = _body(match, item, numbers)
            if proposed == match['body']:
                continue
            # Reconcile again immediately before mutation, retaining concurrent edits.
            fresh = _live_matches(plan, _remote(repository), numbers)[item['key']]
            require(fresh is not None and fresh['number'] == match['number'] and fresh['body'] == match['body'] and fresh['state'] == 'OPEN', 'Issue changed; reconcile before retrying')
            github._gh(['issue', 'edit', str(match['number']), '--repo', repository, '--body-file', '-'], data=proposed, parse=False)
            confirmed = _live_matches(plan, _remote(repository), numbers)[item['key']]
            require(confirmed is not None and confirmed['number'] == match['number'] and confirmed['body'] == proposed and confirmed['state'] == 'OPEN', 'Updated issue is unconfirmed; reconcile remote state before retrying')
        else:
            # Query immediately before create; uncertain failures are never retried here.
            require(_live_matches(plan, _remote(repository), numbers)[item['key']] is None, 'Issue appeared; reconcile before retrying')
            url = github._gh(['issue', 'create', '--repo', repository, '--title', item['title'], '--body-file', '-'], data=_render(item, numbers), parse=False)
            found = re.fullmatch(r'https://github\.com/'+re.escape(repository)+r'/issues/([1-9][0-9]*)', url)
            require(bool(found), 'Unexpected created issue URL; reconcile remote state before retrying')
            numbers[item['key']] = int(found.group(1))
            confirmed = _live_matches(plan, _remote(repository), numbers)[item['key']]
            require(confirmed is not None and confirmed['number'] == numbers[item['key']] and confirmed['body'] == _render(item, numbers), 'Created issue is unconfirmed; reconcile remote state before retrying')
    return dict(schema_version=1, kind='backlog-result', plan_sha256=plan['plan_sha256'],
                github_repository=repository, issues=numbers)
