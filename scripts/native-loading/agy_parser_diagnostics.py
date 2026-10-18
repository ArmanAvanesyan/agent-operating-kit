"""Classify fixed AGY frontmatter diagnostics from one isolated CLI log.

This helper only reads the bounded log below a caller-owned isolated root. It
does not start the host, inspect a source profile, or retain log text.
"""
import os
from pathlib import Path
import re
import stat

FIXTURE_RELATIVE_PATH = ".agents/agents/aok-probe/agent.md"
LOG_RELATIVE_PATH = "native-parser.log"
MAX_LOG_BYTES = 1024 * 1024

_PARSER_MARKERS = (
    ("frontmatter_yaml_unmarshal", "failed to unmarshal frontmatter yaml"),
    ("frontmatter_yaml_parse", "failed to parse frontmatter yaml"),
    ("frontmatter_parse", "failed to parse frontmatter"),
    ("frontmatter_decode", "failed to decode frontmatter"),
)
_YAML_SYNTAX_PATTERNS = (
    ("yaml_line_error", re.compile(r"\byaml:\s*line\s+\d+\s*:", re.I)),
    ("yaml_unmarshal_error", re.compile(r"\byaml: unmarshal errors?\s*:", re.I)),
    ("yaml_syntax_error", re.compile(r"\b(?:yaml|yaml\.v[123])[^\n]{0,80}\bsyntax error\b", re.I)),
)


def _unavailable():
    return {"status": "unavailable", "truncated": False,
            "fixtureAttributed": False, "diagnosticRows": 0, "bytesRead": 0, "rowsRead": 0,
            "markerCounts": {label: 0 for label, _ in _PARSER_MARKERS} |
                             {label: 0 for label, _ in _YAML_SYNTAX_PATTERNS}}


def _owned_dir(fd):
    info = os.fstat(fd)
    return stat.S_ISDIR(info.st_mode) and info.st_uid == os.getuid()


def _open_log(root_fd):
    """Open only the fixed top-level log directly beneath the owned root."""
    name = Path(LOG_RELATIVE_PATH).name
    before = os.stat(name, dir_fd=root_fd, follow_symlinks=False)
    if not stat.S_ISREG(before.st_mode):
        raise OSError("unsafe log file")
    fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=root_fd)
    opened = os.fstat(fd)
    if (not stat.S_ISREG(opened.st_mode) or opened.st_uid != os.getuid() or
            opened.st_nlink != 1 or opened.st_ino != before.st_ino or
            opened.st_dev != before.st_dev):
        os.close(fd)
        raise OSError("unsafe log file")
    return fd


def classify(root, fixture_relative_path=FIXTURE_RELATIVE_PATH):
    """Return stable aggregate labels for the exact owned isolated log."""
    result = _unavailable()
    try:
        if fixture_relative_path != FIXTURE_RELATIVE_PATH:
            return result
        root = Path(root).absolute()
        if root.resolve(strict=True) != root:
            return result
        root_info = root.lstat()
        if (not stat.S_ISDIR(root_info.st_mode) or root_info.st_uid != os.getuid() or
                stat.S_IMODE(root_info.st_mode) != 0o700):
            return result
        root_fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        try:
            opened_root = os.fstat(root_fd)
            if (opened_root.st_ino != root_info.st_ino or opened_root.st_dev != root_info.st_dev or
                    opened_root.st_uid != os.getuid() or stat.S_IMODE(opened_root.st_mode) != 0o700):
                return result
            fd = _open_log(root_fd)
        finally:
            os.close(root_fd)
        try:
            initial_info = os.fstat(fd)
            content = bytearray()
            while len(content) <= MAX_LOG_BYTES:
                block = os.read(fd, min(65536, MAX_LOG_BYTES + 1 - len(content)))
                if not block:
                    break
                content.extend(block)
            final_info = os.fstat(fd)
            if (final_info.st_uid != os.getuid() or final_info.st_nlink != 1 or
                    final_info.st_ino != initial_info.st_ino or
                    final_info.st_dev != initial_info.st_dev or
                    final_info.st_size != initial_info.st_size or
                    final_info.st_mtime_ns != initial_info.st_mtime_ns or
                    len(content) != min(initial_info.st_size, MAX_LOG_BYTES + 1)):
                return result
        finally:
            os.close(fd)
    except (OSError, ValueError, RuntimeError):
        return result

    try:
        final_root = root.lstat()
        if (not stat.S_ISDIR(final_root.st_mode) or final_root.st_uid != os.getuid() or
                stat.S_IMODE(final_root.st_mode) != 0o700 or
                final_root.st_ino != opened_root.st_ino or final_root.st_dev != opened_root.st_dev):
            return result
    except OSError:
        return result

    truncated = len(content) > MAX_LOG_BYTES
    text = bytes(content[:MAX_LOG_BYTES]).decode("utf-8", "replace")
    counts = {label: 0 for label, _ in _PARSER_MARKERS}
    counts.update({label: 0 for label, _ in _YAML_SYNTAX_PATTERNS})
    expected_fixture = str((root / fixture_relative_path).absolute())
    fixture_attributed = False
    fixture_token = re.compile(r"(?<![A-Za-z0-9_./-])" + re.escape(expected_fixture) +
                               r"(?![A-Za-z0-9_./-])")
    diagnostic_rows = 0
    rows = text.splitlines()
    context_counts = {label: 0 for label in ("agent", "yaml", "frontmatter", "warn", "error", "load")}
    for row in rows:
        lower = row.lower()
        for label in context_counts:
            context_counts[label] += int(label in lower)
        row_labels = set()
        yaml_parse = "failed to parse frontmatter yaml" in lower
        for label, phrase in _PARSER_MARKERS:
            if label == "frontmatter_parse" and yaml_parse:
                continue
            if phrase in lower:
                row_labels.add(label)
        for label, pattern in _YAML_SYNTAX_PATTERNS:
            if pattern.search(row):
                row_labels.add(label)
        if row_labels:
            diagnostic_rows += 1
            for label in row_labels:
                counts[label] += 1
            if fixture_token.search(row):
                fixture_attributed = True
    return {"status": "truncated" if truncated else "available",
            "truncated": truncated, "fixtureAttributed": fixture_attributed and not truncated,
            "diagnosticRows": diagnostic_rows, "markerCounts": counts,
            "bytesRead": min(len(content), MAX_LOG_BYTES), "rowsRead": len(rows),
            "contextCounts": context_counts}
