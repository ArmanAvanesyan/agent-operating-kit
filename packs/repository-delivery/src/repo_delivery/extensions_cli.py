"""Planning, integration evidence and explicitly optional companion interfaces."""
import os
from pathlib import Path

from . import adapters, hooks, integration, planning
from .core import DeliveryError, canonical, git, root
from .delivery_cli import _object, _outside, load

COMMANDS = {'backlog', 'integration', 'aok', 'runtime', 'hook'}


def add_commands(commands):
    def group(name, operations):
        p = commands.add_parser(name).add_subparsers(dest='operation', required=True)
        return {name: p.add_parser(name) for name in operations}

    def args(parser, *names):
        for name in names:
            parser.add_argument('--' + name, required=True)

    def output(parsers):
        for parser in parsers.values():
            parser.add_argument('--output')

    p = group('backlog', ['plan', 'apply']); output(p)
    args(p['plan'], 'repo', 'spec'); p['plan'].add_argument('--issues')
    args(p['apply'], 'plan', 'repository'); p['apply'].add_argument('--authorize', action='append', required=True)
    p = group('integration', ['run', 'verify']); output(p)
    args(p['run'], 'context', 'spec'); args(p['verify'], 'context', 'proof')
    p = group('aok', ['consume']); output(p)
    args(p['consume'], 'packet', 'schema', 'schema-sha256')
    for name in ('policy', 'policy-schema', 'policy-schema-sha256'):
        p['consume'].add_argument('--' + name)
    p = group('runtime', ['task', 'reference', 'validate', 'finish']); output(p)
    p['task'].add_argument('--command', dest='host_command', required=True); args(p['task'], 'task-id'); p['task'].add_argument('--timeout', type=float, default=10)
    args(p['reference'], 'export', 'attempt-id'); args(p['validate'], 'reference', 'expected')
    p['finish'].add_argument('--command', dest='host_command', required=True)
    args(p['finish'], 'reference', 'expected', 'credentials')
    p['finish'].add_argument('--outcome', choices=['complete', 'failed'], required=True)
    p['finish'].add_argument('--authorize', action='append', required=True)
    p['finish'].add_argument('--timeout', type=float, default=10)
    p = group('hook', ['install', 'check']); output(p)
    args(p['install'], 'repo', 'config'); args(p['check'], 'config')
    p['install'].add_argument('--authorize', action='append', required=True)


def dispatch(args):
    ctx = _object(args.context) if hasattr(args, 'context') else None
    plan = _object(args.plan) if hasattr(args, 'plan') else None
    spec = _object(args.spec) if hasattr(args, 'spec') else None
    config = _object(args.config) if hasattr(args, 'config') else None
    protected = []
    for metadata in (ctx, plan, config):
        if not metadata:
            continue
        if metadata.get('repository'):
            protected.append(metadata['repository'])
        for item in metadata.get('preservation', []):
            protected.append(item['path'])
    if config and config.get('context'):
        for item in _object(config['context']).get('preservation', []):
            protected.append(item['path'])
    repo = getattr(args, 'repo', None) or (ctx or plan or config or {}).get('repository')
    if repo:
        actual = root(repo)
        protected.append(actual)
        common = os.fsdecode(git(actual, 'rev-parse', '--git-common-dir')).removesuffix('\n')
        protected.append((actual / common).resolve())
    target = _outside(args.output, protected) if args.output else None
    if args.command == 'backlog':
        if args.operation == 'plan':
            result = planning.plan_backlog(args.repo, spec, load(args.issues) if args.issues else [])
        else:
            result = planning.apply_backlog(plan, args.repository, args.authorize)
    elif args.command == 'integration':
        if args.operation == 'run':
            result = integration.run_integration(ctx, spec)
        else:
            result = integration.assert_integration(ctx, _object(args.proof))
    elif args.command == 'aok':
        result = adapters.consume_aok(args.packet, args.schema, args.schema_sha256,
                  policy_path=args.policy, policy_schema_path=args.policy_schema,
                  policy_schema_sha256=args.policy_schema_sha256)
    elif args.command == 'runtime':
        if args.operation == 'reference':
            result = adapters.consume_swarmd_dump(args.export, args.attempt_id)
        elif args.operation == 'validate':
            reference = adapters.validate_attempt_reference(_object(args.reference), _object(args.expected))
            result = {'schema_version': 1, 'kind': 'attempt-reference-check', 'reference': reference,
                      'live_lease_verified': False, 'managed_finish_supported': None}
        elif args.operation == 'finish':
            from .runtime_finalize import credentials
            reference = adapters.validate_attempt_reference(_object(args.reference), _object(args.expected))
            if 'finalize-attempt' not in args.authorize:
                raise DeliveryError('Explicit finalize-attempt authorization required')
            token = credentials(args.credentials, reference)
            # Prevent output aliases from replacing any input or private credential.
            if target:
                inputs = (args.reference, args.expected, args.credentials, args.host_command)
                if any(target.resolve() == Path(p).resolve() for p in inputs):
                    raise DeliveryError('Output overlaps runtime inputs')
            with adapters.StdioTransport(load(args.host_command), args.timeout) as transport:
                result = adapters.SwarmdClient(transport).finish(reference, reference, lease_token=token,
                         success=args.outcome == 'complete', authorized_actions=args.authorize)
        else:
            with adapters.StdioTransport(load(args.host_command), args.timeout) as transport:
                result = adapters.SwarmdClient(transport).task(args.task_id)
    elif args.command == 'hook':
        result = (hooks.install_hook(args.repo, args.config, args.authorize)
                  if args.operation == 'install' else hooks.check_hook(args.config))
    else:
        raise DeliveryError('Unknown extension command')
    if target:
        fd = os.open(target, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'wb') as stream:
            stream.write(canonical(result))
    return result
