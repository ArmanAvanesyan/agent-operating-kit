#!/usr/bin/env python3
"""Read-only, single-file queries through optional Rust CLI backends."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import signal
from pathlib import Path
import shutil
import subprocess
import tempfile
import threading

TOOLS = {'text': 'rg', 'pattern': 'ast-grep', 'outline': 'ast-grep', 'stats': 'tokei'}
SOURCE_EXTENSIONS = {'.py', '.go', '.rs', '.js', '.jsx', '.ts', '.tsx', '.java',
                     '.c', '.h', '.cpp', '.cs', '.rb', '.sh'}


def run_bounded(argv: list[str], cwd: str, timeout: float = 10,
                limit: int = 1_000_000) -> dict:
    """Drain both pipes with bounded memory and kill on excess output or timeout."""
    process = subprocess.Popen(argv, cwd=cwd, stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                               start_new_session=os.name == 'posix')
    def stop() -> None:
        try:
            if os.name == 'posix':
                os.killpg(process.pid, signal.SIGKILL)
            else:
                process.kill()
        except OSError:
            try:
                process.kill()
            except OSError:
                pass

    output = {'stdout': bytearray(), 'stderr': bytearray()}
    exceeded = threading.Event()

    def read(name: str, pipe) -> None:
        while True:
            block = pipe.read(4096)
            if not block:
                break
            remaining = max(0, limit - len(output[name]))
            output[name].extend(block[:remaining])
            if len(block) > remaining:
                exceeded.set()
                stop()
        pipe.close()

    threads = [threading.Thread(target=read, args=(name, pipe), daemon=True)
               for name, pipe in [('stdout', process.stdout), ('stderr', process.stderr)]]
    for thread in threads:
        thread.start()
    timed_out = False
    try:
        process.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        timed_out = True
        stop()
        process.wait()
    for thread in threads:
        thread.join(timeout=1)
    if any(thread.is_alive() for thread in threads):
        timed_out = True
        stop()
        for thread in threads:
            thread.join(timeout=1)
    return {'returncode': process.returncode, 'timedOut': timed_out,
            'outputLimited': exceeded.is_set(),
            **{key: bytes(value).decode('utf-8', errors='replace') for key, value in output.items()}}


def selected_file(root: Path, relative: str, max_bytes: int) -> Path:
    root = root.resolve(strict=True)
    path = root / relative
    if Path(relative).is_absolute() or '..' in Path(relative).parts:
        raise ValueError('file must be a relative path within root')
    current = root
    for part in Path(relative).parts:
        current = current / part
        if current.is_symlink():
            raise ValueError('symlink paths are not supported')
    path.resolve(strict=True).relative_to(root)
    if not path.is_file() or path.suffix.lower() not in SOURCE_EXTENSIONS:
        raise ValueError('select a supported source file, not configuration or secrets')
    if path.stat().st_size > max_bytes:
        raise ValueError('file exceeds byte limit')
    return path


def query(root: Path, relative: str, mode: str, pattern: str | None = None,
          language: str | None = None, timeout: float = 10,
          max_bytes: int = 500_000, max_output: int = 1_000_000) -> dict:
    path = selected_file(root, relative, max_bytes)
    binary = shutil.which(TOOLS[mode])
    report = {'kind': 'aok-structural-evidence', 'version': 1, 'file': relative,
              'backend': TOOLS[mode], 'mode': mode, 'query': pattern,
              'language': language, 'status': 'unavailable',
              'limitations': ['Syntax/text/statistics are not resolved references or runtime behavior.',
                              'One explicit file only; empty results are not repository-wide absence proof.']}
    if not binary:
        report['reason'] = 'tool-not-installed'
        return report
    if mode in {'text', 'pattern'} and not pattern:
        raise ValueError('pattern is required')
    if mode in {'pattern', 'outline'} and not language:
        raise ValueError('language is required')
    with tempfile.TemporaryDirectory(prefix='aok-explore-') as directory:
        # Isolate automatic project config discovery; stage only the selected source.
        config = Path(directory) / 'sgconfig.yml'
        config.write_text('ruleDirs: []\n')
        staged = Path(directory) / ('input' + path.suffix)
        with path.open('rb') as source:
            contents = source.read(max_bytes + 1)
        if len(contents) > max_bytes:
            raise ValueError('file exceeds byte limit')
        staged.write_bytes(contents)
        report['sourceSha256'] = hashlib.sha256(contents).hexdigest()
        git_binary = shutil.which('git')
        if git_binary:
            revision = run_bounded([git_binary, '-c', 'core.fsmonitor=false', 'rev-parse', 'HEAD'],
                                   str(root.resolve()), timeout=5, limit=4096)
            report['revision'] = revision['stdout'].strip() if revision['returncode'] == 0 else None
        version = run_bounded([binary, '--version'], directory, timeout=timeout, limit=4096)
        report['toolVersion'] = version['stdout'].strip()
        if mode == 'text':
            argv = [binary, '--no-config', '--json', '--', pattern, str(staged)]
        elif mode == 'pattern':
            argv = [binary, 'run', '--config', str(config), '--lang', language,
                    '--pattern', pattern, '--json=compact', str(staged)]
        elif mode == 'outline':
            help_result = run_bounded([binary, 'outline', '--help'], directory, timeout=timeout, limit=100_000)
            if help_result['returncode'] != 0:
                report['reason'] = 'outline-not-supported'
                return report
            argv = [binary, 'outline', '--config', str(config), '--lang', language,
                    '--json=compact', str(staged)]
        else:
            argv = [binary, str(staged), '--output', 'json']
        result = run_bounded(argv, directory, timeout=timeout, limit=max_output)
        report['exitCode'] = result['returncode']
        if result['timedOut'] or result['outputLimited']:
            report.update(status='incomplete', reason='timeout' if result['timedOut'] else 'output-limit')
            return report
        if result['returncode'] not in ({0, 1} if mode in {'text', 'pattern'} else {0}):
            report.update(status='error', reason='backend-failed')
            return report
        try:
            if mode == 'text':
                records = [json.loads(line) for line in result['stdout'].splitlines() if line]
                evidence = [x['data'] for x in records if x.get('type') == 'match']
            else:
                evidence = json.loads(result['stdout'] or '[]')
        except json.JSONDecodeError:
            report.update(status='error', reason='invalid-json-output')
            return report
        # Replace temporary filenames in all backend-specific result shapes.
        def normalize(value):
            if isinstance(value, str):
                return value.replace(str(staged), relative)
            if isinstance(value, list):
                return [normalize(item) for item in value]
            if isinstance(value, dict):
                return {normalize(key): normalize(item) for key, item in value.items()}
            return value

        report.update(status='observed', evidence=normalize(evidence))
    return report


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('root', type=Path)
    parser.add_argument('--file', required=True)
    parser.add_argument('--mode', choices=TOOLS, required=True)
    parser.add_argument('--pattern')
    parser.add_argument('--lang')
    args = parser.parse_args()
    try:
        result = query(args.root, args.file, args.mode, args.pattern, args.lang)
    except (OSError, ValueError) as error:
        parser.error(str(error))
    print(json.dumps(result, indent=2))
    raise SystemExit(0 if result['status'] == 'observed' else 2)


if __name__ == '__main__':
    main()
