#!/usr/bin/env python3
"""SessionStart hook — emit a brief reminder of active conventions.

Looks for a CLAUDE.md in the current working directory and surfaces a short
summary, so Claude starts each session knowing what conventions apply.
"""

from __future__ import annotations

import json
import os
import sys


def _find_claude_md(start: str) -> str | None:
    """Walk up from start looking for CLAUDE.md."""
    current = os.path.abspath(start)
    while True:
        candidate = os.path.join(current, "CLAUDE.md")
        if os.path.isfile(candidate):
            return candidate
        parent = os.path.dirname(current)
        if parent == current:
            return None
        current = parent


def main() -> int:
    try:
        data = json.load(sys.stdin)
    except json.JSONDecodeError:
        data = {}

    cwd = data.get("cwd") or os.getcwd()
    claude_md = _find_claude_md(cwd)

    parts = [
        "**engineering-guardrails active**: PreToolUse hooks will block writes that violate "
        "`from __future__ import annotations`, no-assert-in-production, and alembic CONCURRENTLY "
        "discipline."
    ]
    if claude_md:
        parts.append(f"Project conventions: see `{os.path.relpath(claude_md, cwd)}`.")
    else:
        parts.append("No CLAUDE.md found in or above cwd.")

    msg = " ".join(parts)
    json.dump(
        {
            "hookSpecificOutput": {
                "hookEventName": "SessionStart",
                "additionalContext": msg,
            }
        },
        sys.stdout,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
