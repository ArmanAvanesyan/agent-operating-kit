#!/usr/bin/env python3
"""Bounded metadata-only inventory; no project code or dependency installation."""
from __future__ import annotations

import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
from collections import Counter

EXCLUDED = {'.git', 'node_modules', 'vendor', 'target', 'dist', 'build', 'coverage',
            '.venv', 'venv', '__pycache__', '.pytest_cache', '.mypy_cache'}
HIDDEN = {'.github', '.codex', '.claude', '.agents', '.agent', '.cursor', '.opencode',
          '.gemini', '.aok', '.devcontainer'}
MANIFESTS = {'package.json', 'pyproject.toml', 'Cargo.toml', 'go.mod', 'Gemfile',
             'pom.xml', 'build.gradle', 'composer.json', 'Makefile', 'Justfile',
             'Dockerfile', 'compose.yml', 'compose.yaml', 'docker-compose.yml',
             'requirements.txt', 'mise.toml', '.tool-versions', '.python-version',
             '.node-version', 'rust-toolchain.toml', 'global.json'}
LOCKS = {'package-lock.json', 'pnpm-lock.yaml', 'yarn.lock', 'uv.lock', 'poetry.lock',
         'Cargo.lock', 'go.sum', 'Gemfile.lock', 'composer.lock'}
LANGUAGES = {'.py': 'Python', '.go': 'Go', '.rs': 'Rust', '.ts': 'TypeScript',
             '.tsx': 'TSX', '.js': 'JavaScript', '.jsx': 'JSX', '.java': 'Java',
             '.cs': 'C#', '.rb': 'Ruby', '.sh': 'Shell', '.c': 'C', '.cpp': 'C++'}


def positive(value: str) -> int:
    number = int(value)
    if number < 1:
        raise argparse.ArgumentTypeError('must be positive')
    return number


def git(root: Path, *args: str) -> dict:
    if not shutil.which('git'):
        return {'status': 'unavailable'}
    try:
        result = subprocess.run(['git', '-c', 'core.fsmonitor=false', '-C', str(root), *args],
                                capture_output=True, text=True, timeout=5)
        return {'status': 'observed' if result.returncode == 0 else 'unavailable',
                'value': result.stdout.strip() if result.returncode == 0 else None}
    except (OSError, subprocess.TimeoutExpired):
        return {'status': 'unavailable'}


def inventory(root: Path, max_depth: int = 4, max_files: int = 2000,
              max_entries: int = 10000) -> dict:
    root = root.resolve(strict=True)
    if not root.is_dir():
        raise ValueError('root must be a directory')
    records, omissions, errors = [], Counter(), []
    visited = 0
    limited = False

    def walk(directory: Path, depth: int) -> None:
        nonlocal visited, limited
        try:
            with os.scandir(directory) as entries:
                # Bound enumeration before sorting: huge directories cannot defeat the budget.
                children = []
                for entry in entries:
                    if visited >= max_entries:
                        limited = True
                        omissions['entry-budget'] += 1
                        break
                    visited += 1
                    children.append(entry)
            for entry in sorted(children, key=lambda x: x.name):
                path = Path(entry.path)
                rel = path.relative_to(root).as_posix()
                if len(records) >= max_files:
                    limited = True
                    omissions['file-budget'] += 1
                    break
                try:
                    if entry.is_symlink():
                        omissions['symlink'] += 1
                    elif entry.name in EXCLUDED:
                        omissions['excluded-name'] += 1
                    elif entry.is_dir(follow_symlinks=False):
                        if entry.name.startswith('.') and entry.name not in HIDDEN:
                            omissions['hidden-directory'] += 1
                        elif depth >= max_depth:
                            omissions['depth-limit'] += 1
                        else:
                            walk(path, depth + 1)
                    elif entry.is_file(follow_symlinks=False):
                        suffix = path.suffix.lower()
                        kind = ('manifest' if entry.name in MANIFESTS else
                                'lockfile' if entry.name in LOCKS else
                                'instructions' if entry.name in {'AGENTS.md', 'CLAUDE.md', 'GEMINI.md', 'SKILL.md'} else
                                'configuration' if any(p in HIDDEN for p in path.relative_to(root).parts[:-1]) else
                                'source' if suffix in LANGUAGES else 'other')
                        records.append({'path': rel, 'kind': kind,
                                        'language': LANGUAGES.get(suffix),
                                        'bytes': entry.stat(follow_symlinks=False).st_size})
                except OSError:
                    errors.append({'path': rel, 'reason': 'metadata-unavailable'})
        except OSError:
            errors.append({'path': directory.relative_to(root).as_posix(), 'reason': 'directory-unavailable'})

    walk(root, 0)
    return {'kind': 'aok-project-inventory', 'version': 1, 'root': str(root),
            'revision': git(root, 'rev-parse', 'HEAD'),
            'branch': git(root, 'branch', '--show-current'),
            'limits': {'maxDepth': max_depth, 'maxFiles': max_files, 'maxEntries': max_entries},
            'coverage': 'partial' if limited or omissions or errors else 'enumerated',
            'omissions': dict(omissions), 'errors': errors, 'files': records,
            'languages': dict(Counter(x['language'] for x in records if x['language'])),
            'limitations': ['Metadata only; no file contents or architectural conclusions.',
                            'Fixed exclusions, not full gitignore semantics; ignored paths may appear.',
                            'Symlinks are not followed. Depth counts directories below root.',
                            'Language counts describe enumerated files, not code size or importance.']}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('root', type=Path)
    parser.add_argument('--max-depth', type=positive, default=4)
    parser.add_argument('--max-files', type=positive, default=2000)
    parser.add_argument('--max-entries', type=positive, default=10000)
    args = parser.parse_args()
    try:
        result = inventory(args.root, args.max_depth, args.max_files, args.max_entries)
    except (OSError, ValueError) as error:
        parser.error(str(error))
    print(json.dumps(result, indent=2))


if __name__ == '__main__':
    main()
