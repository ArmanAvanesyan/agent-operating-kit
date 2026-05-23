#!/usr/bin/env python3
"""PreToolUse:Write|Edit hook — alembic CONCURRENTLY discipline.

Rules:
  1. CONCURRENTLY index ops must wrap in `op.get_context().autocommit_block()`.
     Postgres rejects CONCURRENTLY inside a transaction; alembic wraps
     migrations in a transaction by default.
  2. A migration using CONCURRENTLY must not also contain regular DDL
     (ALTER TABLE / CREATE TABLE / etc.) — the autocommit_block ends the
     transaction, leaving subsequent DDL un-transactional. Put the
     CONCURRENTLY op in its own revision file.
"""

from __future__ import annotations

import json
import re
import sys


_DDL_PATTERN = re.compile(
    r"\bop\.(create_table|drop_table|alter_column|add_column|drop_column|"
    r"create_check_constraint|create_foreign_key|drop_constraint)\("
)


def _normalize(path: str) -> str:
    return path.replace("\\", "/")


def _is_migration(path: str) -> bool:
    p = _normalize(path)
    return ("/alembic/versions/" in p or "/migrations/versions/" in p) and p.endswith(".py")


def _deny(reason: str) -> int:
    json.dump(
        {
            "hookSpecificOutput": {
                "hookEventName": "PreToolUse",
                "permissionDecision": "deny",
                "permissionDecisionReason": reason,
            }
        },
        sys.stdout,
    )
    return 0


def main() -> int:
    try:
        data = json.load(sys.stdin)
    except json.JSONDecodeError:
        return 0

    tool = data.get("tool_name", "")
    tool_input = data.get("tool_input") or {}
    file_path = tool_input.get("file_path") or ""

    if not _is_migration(file_path):
        return 0

    if tool == "Write":
        content = tool_input.get("content") or ""
    elif tool == "Edit":
        content = tool_input.get("new_string") or ""
    else:
        return 0

    if not re.search(r"\bCONCURRENTLY\b", content, re.IGNORECASE):
        return 0

    if "autocommit_block" not in content:
        return _deny(
            f"Migration uses `CONCURRENTLY` without `op.get_context().autocommit_block()`. "
            f"Postgres rejects CONCURRENTLY inside a transaction; alembic wraps migrations in "
            f"a transaction by default. Wrap the index op in "
            f"`with op.get_context().autocommit_block(): op.execute(...)`. "
            f"See ADR-011. Path: {file_path}"
        )

    if _DDL_PATTERN.search(content):
        return _deny(
            f"Migration mixes CONCURRENTLY index ops with regular DDL (ALTER/CREATE TABLE etc.). "
            f"The autocommit_block ends the transaction, leaving subsequent DDL un-transactional "
            f"and irreversible on failure. Put the CONCURRENTLY index in its own revision file. "
            f"See ADR-011. Path: {file_path}"
        )

    return 0


if __name__ == "__main__":
    sys.exit(main())
