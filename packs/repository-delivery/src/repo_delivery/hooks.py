"""Explicit, local POSIX Git pre-push companion. Never runs gate commands."""
import json
import os
from pathlib import Path
import shlex
import stat
import subprocess
import sys

from .core import DeliveryError, git, root
from .evidence import assert_ready, require


def _plain_path(value):
    require(isinstance(value, (str, Path)), 'Invalid hook path')
    path = Path(value)
    require(path.is_absolute(), 'Hook paths must be absolute')
    require('..' not in path.parts, 'Hook traversal is unsupported')
    for candidate in (path, *path.parents):
        require(not candidate.is_symlink(), 'Hook paths cannot contain symlinks')
    return path


def _private_json(value):
    path = _plain_path(value)
    try:
        info = path.stat()
        require(stat.S_ISREG(info.st_mode), 'Hook input must be a regular file')
        require(info.st_uid == os.getuid() and info.st_mode & 0o077 == 0,
                'Hook inputs must be private and owned by the current user')
        require(info.st_size <= 16 * 1024 * 1024, 'Hook input is too large')
        result = json.loads(path.read_text())
        require(isinstance(result, dict), 'Hook input must be an object')
        return result
    except (OSError, ValueError) as exc:
        raise DeliveryError('Cannot read hook evidence: ' + str(exc)) from exc


def _configuration(config_path):
    config_path = _plain_path(config_path)
    config = _private_json(config_path)
    require(set(config) == {'schema_version', 'kind', 'repository', 'context',
                           'gates', 'review', 'fail_mode'} and
            config.get('schema_version') == 1 and
            config.get('kind') == 'git-pre-push-config' and
            config.get('fail_mode') == 'closed', 'Unsupported hook configuration')
    repo = root(_plain_path(config['repository']))
    require(str(repo) == config['repository'], 'Hook repository must be canonical')
    for value in (config_path, config['context'], config['gates'], config['review']):
        if value is None:
            continue
        path = _plain_path(value)
        require(not path.is_relative_to(repo), 'Hook configuration and evidence must be outside source')
    return config, repo


def check_hook(config_path):
    """Require current local receipts and push authority, without remote assertions."""
    config, repo = _configuration(config_path)
    require(not git(repo, 'status', '--porcelain=v1', '-z', '--untracked-files=all'),
            'Pre-push evidence requires clean tracked, staged, and untracked source')
    context = _private_json(config['context'])
    require(context.get('repository') == str(repo), 'Hook context repository mismatch')
    require('push' in context.get('authorized_actions', []), 'Push authorization required')
    gates = _private_json(config['gates'])
    review = _private_json(config['review']) if config['review'] is not None else None
    try:
        current = assert_ready(context, gates, review)
    except (KeyError, TypeError, ValueError) as exc:
        raise DeliveryError('Malformed hook evidence') from exc
    return {'schema_version': 1, 'kind': 'hook-check', 'repository': str(repo),
            'ready': True, 'snapshot_sha256': current['snapshot_sha256'],
            'remote_review_verified': False}


def install_hook(repo, config_path, authorized_actions):
    """Install only an explicitly authorized POSIX companion, preserving existing hooks."""
    require('install-hook' in authorized_actions, 'Hook installation authorization required')
    require(os.name == 'posix', 'Only POSIX Git pre-push hosts are supported')
    config, configured_repo = _configuration(config_path)
    repo = root(repo)
    require(repo == configured_repo, 'Hook configuration repository mismatch')
    git_directory = subprocess.check_output(['git', 'rev-parse', '--absolute-git-dir'], cwd=repo).decode().removesuffix('\n')
    common_directory = subprocess.check_output(['git', 'rev-parse', '--path-format=absolute', '--git-common-dir'], cwd=repo).decode().removesuffix('\n')
    require(Path(git_directory) == Path(common_directory), 'Linked worktree hook installation is unsupported')
    configured = subprocess.run(['git', 'config', '--get-all', 'core.hooksPath'],
                                cwd=repo, capture_output=True, check=False)
    require(configured.returncode == 1, 'Custom core.hooksPath is unsupported')
    # Git resolves linked-worktree metadata; never assume .git is a directory.
    result = subprocess.run(['git', 'rev-parse', '--path-format=absolute', '--git-common-dir'],
                            cwd=repo, capture_output=True, check=False)
    require(result.returncode == 0, 'Cannot resolve Git hooks directory')
    directory = _plain_path(str(Path(result.stdout.decode().removesuffix('\n')) / 'hooks'))
    require(directory.is_dir(), 'Git hooks directory is missing')
    hook = directory / 'pre-push'
    require(not hook.exists() and not hook.is_symlink(), 'Refusing existing pre-push hook')
    # Verify all evidence before activation. No command from the context is executed.
    check_hook(config_path)
    source = str(Path(__file__).resolve().parents[1])
    bootstrap = ('import sys; from pathlib import Path; source=Path(sys.argv[1]); '
                 'expected=source / "repo_delivery" / "hooks.py"; '
                 'expected.is_file() or sys.exit("Installed repository-delivery helper is missing; reinstall the authorized hook"); '
                 'sys.path.insert(0, str(source)); from repo_delivery import hooks; '
                 'Path(hooks.__file__).resolve() == expected.resolve() or sys.exit("Installed helper identity mismatch"); '
                 'raise SystemExit(hooks.hook_main(sys.argv[2]))')
    command = [sys.executable, '-I', '-c', bootstrap, source, str(_plain_path(config_path))]
    script = '#!/bin/sh\n# Explicit repo-delivery POSIX companion; local evidence only.\nexec ' + shlex.join(command) + '\n'
    try:
        fd = os.open(hook, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o700)
        with os.fdopen(fd, 'w') as stream:
            stream.write(script)
    except OSError as exc:
        raise DeliveryError('Cannot install pre-push hook: ' + str(exc)) from exc
    return {'schema_version': 1, 'kind': 'hook-installation', 'host': 'posix-git-pre-push',
            'repository': str(repo), 'hook': str(hook), 'config': str(_plain_path(config_path)),
            'fail_mode': 'closed'}


def hook_main(config_path):
    try:
        check_hook(config_path)
        config, repo = _configuration(config_path)
        head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=repo).decode().removesuffix('\n')
        updates = sys.stdin.read(1024 * 1024 + 1)
        require(len(updates) <= 1024 * 1024, 'Push update input is too large')
        lines = [line.split() for line in updates.splitlines()]
        require(bool(lines) and all(len(line) == 4 and line[1] == head for line in lines),
                'Only pushes of the evidenced current HEAD are supported')
        return 0
    except (DeliveryError, OSError, KeyError, TypeError, ValueError) as exc:
        print('repo-delivery pre-push blocked: ' + str(exc), file=sys.stderr)
        return 1
