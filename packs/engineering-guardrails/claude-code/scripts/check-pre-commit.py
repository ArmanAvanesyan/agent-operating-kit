#!/usr/bin/env python3
"""PreToolUse hook on `git commit` — emit a reminder if tests appear stale.

Heuristic: compare the mtime of the most recently modified `.py` file under
`app/` (or `src/`) against `.pytest_cache/CACHEDIR.TAG`. If the cache is
older than the newest source file, surface a warning to Claude that tests
likely have not been run since the last edit.

Non-blocking — emits an `additionalContext` reminder rather than denying.
The DoD includes "CI green + merged" so blocking at the commit layer is too
aggressive; this just nudges.
"""

from __future__ import annotations

import json
import os
import shlex
import sys
import time


_SOURCE_DIRS = ("app", "src", "lib")
_CACHE_PATHS = (".pytest_cache/CACHEDIR.TAG", ".pytest_cache/v/cache/lastfailed")
_STALE_SECONDS = 600   # if cache is older than the newest source by >10 min, warn


def _is_git_commit(command: str) -> bool:
    """True if the command is some form of `git commit ...` (not amend/revert/log)."""
    try:
        parts = shlex.split(command)
    except ValueError:
        return False
    if len(parts) < 2:
        return False
    if parts[0] != "git":
        return False
    # Match `git commit` but not `git commit-tree`, `git log --grep=commit`, etc.
    return parts[1] == "commit"


def _newest_source_mtime(root: str) -> float:
    newest: float = 0.0
    for src_dir in _SOURCE_DIRS:
        full = os.path.join(root, src_dir)
        if not os.path.isdir(full):
            continue
        for dirpath, _dirs, files in os.walk(full):
            for name in files:
                if not name.endswith(".py"):
                    continue
                try:
                    mtime = os.path.getmtime(os.path.join(dirpath, name))
                except OSError:
                    continue
                if mtime > newest:
                    newest = mtime
    return newest


def _cache_mtime(root: str) -> float | None:
    for rel in _CACHE_PATHS:
        full = os.path.join(root, rel)
        if os.path.isfile(full):
            try:
                return os.path.getmtime(full)
            except OSError:
                continue
    return None


def main() -> int:
    try:
        data = json.load(sys.stdin)
    except json.JSONDecodeError:
        return 0

    if data.get("tool_name") != "Bash":
        return 0
    command = (data.get("tool_input") or {}).get("command") or ""
    if not _is_git_commit(command):
        return 0

    cwd = data.get("cwd") or os.getcwd()
    src_mtime = _newest_source_mtime(cwd)
    if src_mtime == 0:
        return 0   # no source dir found, nothing to compare

    cache_mtime = _cache_mtime(cwd)
    if cache_mtime is None:
        # No pytest cache at all — recommend running tests
        json.dump(
            {
                "hookSpecificOutput": {
                    "hookEventName": "PreToolUse",
                    "additionalContext": (
                        "Reminder: no .pytest_cache found — tests may not have been run in this "
                        "checkout. The project DoD is 'CI green + merged'; run `uv run pytest -x` "
                        "(or the project's equivalent) before committing to catch regressions early."
                    ),
                }
            },
            sys.stdout,
        )
        return 0

    if src_mtime > cache_mtime + _STALE_SECONDS:
        gap_min = int((src_mtime - cache_mtime) / 60)
        json.dump(
            {
                "hookSpecificOutput": {
                    "hookEventName": "PreToolUse",
                    "additionalContext": (
                        f"Reminder: source files have been modified ~{gap_min} min after the "
                        f"last pytest run. Consider re-running `uv run pytest -x` before "
                        f"committing — the DoD is 'CI green + merged'."
                    ),
                }
            },
            sys.stdout,
        )
    return 0


if __name__ == "__main__":
    sys.exit(main())
