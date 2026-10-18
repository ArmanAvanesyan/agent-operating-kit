"""Milestone 2 CLI argument and JSON-file boundaries."""
import json
import os
from pathlib import Path

from .core import DeliveryError, canonical, git, root
from . import evidence, workspace, github


def add_commands(commands):
    def command(name, sub=None):
        parser = commands.add_parser(name)
        if sub:
            children = parser.add_subparsers(dest='operation', required=True)
            return {name: children.add_parser(name) for name in sub}
        return parser

    def argument(parser, *names, **kw):
        parser.add_argument(*names, **kw)

    def repo(parser):
        argument(parser, '--repo', required=True)

    def output(parser):
        argument(parser, '--output')

    def context(parser, receipts=False):
        argument(parser, '--context', required=True)
        if receipts:
            argument(parser, '--gates', required=True)
            argument(parser, '--review')

    def identity(parser):
        argument(parser, '--repository', required=True)
        argument(parser, '--number', required=True, type=int)

    p = command('snapshot'); repo(p); output(p)
    delivery = command('delivery', ['init', 'status'])
    p = delivery['init']; repo(p); output(p)
    argument(p, '--baseline', required=True); argument(p, '--spec', required=True)
    p = delivery['status']; context(p); output(p)
    argument(p, '--gates'); argument(p, '--review')
    p = command('gates', ['run'])['run']; context(p); output(p)
    p = command('review', ['record'])['record']; context(p); output(p)
    argument(p, '--input', required=True)
    p = command('workspace', ['isolate'])['isolate']; repo(p); output(p)
    for name in ('baseline', 'destination', 'branch'):
        argument(p, '--' + name, required=True)
    argument(p, '--authorize', action='append', required=True)
    port = command('port', ['plan', 'apply'])
    p = port['plan']; repo(p); output(p)
    for name in ('checkpoint', 'baseline', 'destination', 'branch'):
        argument(p, '--' + name, required=True)
    argument(p, '--select', action='append')
    cleanup = command('cleanup', ['plan', 'apply'])
    p = cleanup['plan']; repo(p); output(p)
    argument(p, '--branch', action='append', default=[])
    argument(p, '--worktree', action='append', default=[])
    for p in (port['apply'], cleanup['apply']):
        output(p); argument(p, '--plan', required=True)
        argument(p, '--authorize', action='append', required=True)
    p = command('issue', ['inspect'])['inspect']; identity(p); output(p)
    pr = command('pr', ['inspect', 'readiness', 'open', 'merge'])
    for name in ('inspect', 'readiness', 'merge'):
        identity(pr[name]); output(pr[name])
    for name in ('readiness', 'merge'):
        argument(pr[name], '--required-checks', required=True)
    argument(pr['readiness'], '--no-review-required', action='store_true')
    p = pr['open']; output(p); context(p, True)
    for name in ('repository', 'title', 'body-file', 'base', 'head'):
        argument(p, '--' + name, required=True)
    context(pr['merge'], True)


def load(path):
    try:
        return json.loads(Path(path).read_bytes())
    except (OSError, ValueError) as exc:
        raise DeliveryError('Cannot read JSON input: ' + str(path)) from exc


def _outside(output, protected):
    target = Path(output).absolute()
    target = target.parent.resolve() / target.name
    if target.exists() or target.is_symlink() or not target.parent.is_dir():
        raise DeliveryError('Evidence output must be a new file with an existing parent')
    for location in protected:
        path = Path(location).resolve()
        if path == target or path in target.parents:
            raise DeliveryError('Evidence output must be outside repositories and checkpoints')
    return target


def _object(path):
    value = load(path)
    if not isinstance(value, dict):
        raise DeliveryError('Context, specification and plan inputs must be JSON objects')
    return value


def dispatch(args):
    ctx = _object(args.context) if hasattr(args, 'context') else None
    plan = _object(args.plan) if hasattr(args, 'plan') else None
    spec = _object(args.spec) if args.command == 'delivery' and getattr(args, 'operation', None) == 'init' else None
    protected = []
    for item in (ctx or {}).get('preservation', []):
        protected.append(item['path'])
    if spec:
        protected.extend(spec.get('preservation', []))
    local_repo = getattr(args, 'repo', None) or (ctx or plan or {}).get('repository')
    if local_repo:
        actual = root(local_repo)
        protected.append(actual)
        common = os.fsdecode(git(actual, 'rev-parse', '--git-common-dir')).removesuffix('\n')
        protected.append((actual / common).resolve())
    checkpoint = getattr(args, 'checkpoint', None) or (plan or {}).get('checkpoint')
    if checkpoint:
        protected.append(checkpoint)
    if getattr(args, 'destination', None):
        protected.append(args.destination)
    elif plan and plan.get('destination'):
        protected.append(plan['destination'])
    target = _outside(args.output, protected) if args.output else None
    operation = getattr(args, 'operation', None)
    if args.command == 'snapshot':
        result = evidence.snapshot(args.repo)
    elif args.command == 'delivery':
        if operation == 'init':
            result = evidence.new_context(args.repo, spec, args.baseline)
        else:
            result = evidence.delivery_status(ctx, load(args.gates) if args.gates else None,
                                              load(args.review) if args.review else None)
    elif args.command == 'gates':
        result = evidence.run_gates(ctx)
    elif args.command == 'review':
        result = evidence.record_review(ctx, load(args.input))
    elif args.command == 'workspace':
        result = workspace.isolate(args.repo, args.baseline, args.destination, args.branch, args.authorize)
    elif args.command == 'port':
        if operation == 'plan':
            result = workspace.plan_port(args.checkpoint, args.repo, args.baseline,
                                         args.destination, args.branch, args.select)
        else:
            result = workspace.apply_port(plan, args.authorize)
    elif args.command == 'cleanup':
        if operation == 'plan':
            result = workspace.plan_cleanup(args.repo, args.branch, args.worktree)
        else:
            result = workspace.apply_cleanup(plan, args.authorize)
    elif args.command == 'issue':
        result = github.inspect_issue(args.repository, args.number)
    elif args.command == 'pr':
        if operation == 'inspect':
            result = github.inspect_pr(args.repository, args.number)
        elif operation == 'readiness':
            pr = github.inspect_pr(args.repository, args.number)
            result = dict(github.readiness(pr, load(args.required_checks), require_review=not args.no_review_required), pull_request=pr)
        else:
            gates = load(args.gates)
            review = load(args.review) if args.review else None
            if operation == 'open':
                result = github.open_pr(ctx, gates, review, args.repository, args.title,
                                        Path(args.body_file).read_text(), args.base, args.head)
            else:
                result = github.merge_pr(ctx, gates, review, args.repository,
                                         args.number, load(args.required_checks))
    else:
        raise DeliveryError('Unknown delivery command')
    if target:
        # Exclusive creation: never overwrite a receipt or follow a late symlink.
        fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'wb') as stream:
            stream.write(canonical(result))
    return result
