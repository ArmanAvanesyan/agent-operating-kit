#!/usr/bin/env python3
"""PreToolUse:Write|Edit hook — block `assert` in production Python (bandit B101).

Asserts are stripped under `python -O`, leaving the guarded branch unprotected.
"""

from __future__ import annotations

import json
import re
import sys


_EXEMPT_PATTERNS = (
    "/tests/",
    "/conftest.py",
    "/alembic/versions/",
    "/migrations/",
    "/eval/",
    "/scripts/",
)


def _normalize(path: str) -> str:
    return path.replace("\\", "/")


def _is_exempt(path: str) -> bool:
    p = _normalize(path)
    if any(pat in p for pat in _EXEMPT_PATTERNS):
        return True
    if re.search(r"/test_[^/]+\.py$", p):
        return True
    return False


def _find_assert_violations(content: str) -> list[tuple[int, str]]:
    """Return (line_number, line_text) for each top-level assert.

    Skips lines inside triple-quoted strings and shebang/encoding lines.
    """
    violations: list[tuple[int, str]] = []
    in_doc = False
    for i, line in enumerate(content.splitlines(), start=1):
        stripped = line.lstrip()
        if stripped.startswith('"""') or stripped.startswith("'''"):
            in_doc = not in_doc
            continue
        if in_doc:
            continue
        if stripped.startswith("#"):
            continue
        if re.match(r"^[ \t]*assert[ \t(]", line):
            violations.append((i, line))
    return violations


def main() -> int:
    try:
        data = json.load(sys.stdin)
    except json.JSONDecodeError:
        return 0

    tool = data.get("tool_name", "")
    tool_input = data.get("tool_input") or {}
    file_path = tool_input.get("file_path") or ""

    if not file_path.endswith(".py"):
        return 0
    if _is_exempt(file_path):
        return 0

    if tool == "Write":
        content = tool_input.get("content") or ""
    elif tool == "Edit":
        content = tool_input.get("new_string") or ""
    else:
        return 0

    violations = _find_assert_violations(content)
    if not violations:
        return 0

    formatted = "\n".join(f"  line {n}: {text.strip()}" for n, text in violations[:5])
    json.dump(
        {
            "hookSpecificOutput": {
                "hookEventName": "PreToolUse",
                "permissionDecision": "deny",
                "permissionDecisionReason": (
                    f"Production code must not use `assert` (bandit B101). "
                    f"Asserts are stripped under `python -O`, leaving the guarded branch "
                    f"unprotected. Replace with `if x is None: raise ValueError(...)`. "
                    f"File: {file_path}\n{formatted}"
                ),
            }
        },
        sys.stdout,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
