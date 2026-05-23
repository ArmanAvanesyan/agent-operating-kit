#!/usr/bin/env python3
"""PreToolUse:Write hook — block .py file writes missing `from __future__ import annotations`.

Skips: tests/, alembic/versions/, scripts/, conftest.py, and re-export-only
__init__.py files.
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
    "/scripts/",
)


def _normalize(path: str) -> str:
    return path.replace("\\", "/")


def _is_exempt_path(path: str) -> bool:
    p = _normalize(path)
    if any(pat in p for pat in _EXEMPT_PATTERNS):
        return True
    if re.search(r"/test_[^/]+\.py$", p):
        return True
    return False


def _is_marker_init(path: str, content: str) -> bool:
    if not _normalize(path).endswith("/__init__.py"):
        return False
    significant = [
        line
        for line in content.splitlines()
        if line.strip()
        and not line.lstrip().startswith("#")
        and not line.lstrip().startswith('"""')
        and not line.lstrip().startswith("'''")
    ]
    if not significant:
        return True
    for line in significant:
        stripped = line.lstrip()
        if not (
            stripped.startswith("from .")
            or stripped.startswith("import ")
            or stripped.startswith("__all__")
        ):
            return False
    return True


def main() -> int:
    try:
        data = json.load(sys.stdin)
    except json.JSONDecodeError:
        return 0

    if data.get("tool_name") != "Write":
        return 0

    tool_input = data.get("tool_input") or {}
    file_path = tool_input.get("file_path") or ""
    content = tool_input.get("content") or ""

    if not file_path.endswith(".py"):
        return 0
    if _is_exempt_path(file_path):
        return 0

    stripped_body = "\n".join(
        line
        for line in content.splitlines()
        if line.strip()
        and not line.lstrip().startswith("#")
        and not line.lstrip().startswith('"""')
        and not line.lstrip().startswith("'''")
    ).strip()
    if not stripped_body:
        return 0

    if _is_marker_init(file_path, content):
        return 0

    head = "\n".join(content.splitlines()[:25])
    if re.search(r"^from __future__ import annotations\s*$", head, re.MULTILINE):
        return 0

    json.dump(
        {
            "hookSpecificOutput": {
                "hookEventName": "PreToolUse",
                "permissionDecision": "deny",
                "permissionDecisionReason": (
                    f"Production .py file missing `from __future__ import annotations` "
                    f"(project convention: forward-compatible type hints in 3.12). "
                    f"Path: {file_path}. Add the import at the top of the file "
                    f"(after the module docstring)."
                ),
            }
        },
        sys.stdout,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
