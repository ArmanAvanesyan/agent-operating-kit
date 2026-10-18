#!/usr/bin/env python3
"""Probe AGY's interactive /agents manager with a task-owned macOS profile.

Uses only the pinned copied Darwin CLI and an existing native keyring login.
It never copies/reads credentials, sends ordinary prompts, selects an agent,
or runs any imported command. Terminal text is inspected in memory and only
sanitized markers are written to evidence.
"""
import argparse
from datetime import datetime, timedelta, timezone
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import pty
import pwd
import re
import select
import shutil
import signal
import stat
import subprocess
import struct
import sys
import tempfile
import termios
import time
import uuid

sys.dont_write_bytecode = True
_diagnostics_spec = importlib.util.spec_from_file_location("native_startup_diagnostics", Path(__file__).with_name("native_startup_diagnostics.py"))
_diagnostics = importlib.util.module_from_spec(_diagnostics_spec)
_diagnostics_spec.loader.exec_module(_diagnostics)
classify = _diagnostics.classify
_terminal_spec = importlib.util.spec_from_file_location("native_terminal", Path(__file__).with_name("native_terminal.py"))
_terminal = importlib.util.module_from_spec(_terminal_spec)
_terminal_spec.loader.exec_module(_terminal)


def terminal_text(data, *, width=80, height=24):
    return _terminal.render_screen(data, width=width, height=height)


REPO = Path(__file__).resolve().parents[2]
CURSOR_SCRIPT = REPO / "scripts/native-loading/probe_cursor_fresh_cli.py"
_spec = importlib.util.spec_from_file_location("cursor_audit_helpers", CURSOR_SCRIPT)
audit = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(audit)
AGY_SCRIPT = REPO / "scripts/native-loading/probe-agy-interactive-manager.py"
_agy_spec = importlib.util.spec_from_file_location("agy_interactive_helpers", AGY_SCRIPT)
agy = importlib.util.module_from_spec(_agy_spec)
_agy_spec.loader.exec_module(agy)

PINNED_BINARY_SHA256 = "fa85a90abded17bcd1b185bbd1d6b1b82065f093ad4700d4dbacd3be2433294c"
PINNED_BINARY = Path("/private/tmp/aok-agy-1.4.2-x64/antigravity")
PRIVATE_TEMP_ROOT = Path("/private/tmp")
AUDIT_PATH = None
AUTH = re.compile(r"sign[ -]?in|log[ -]?in|authenticate|oauth|authentication required|api key", re.I)
ERROR = re.compile(r"\berror\b|failed|fatal|invalid", re.I)
PARSE_ERROR = re.compile(r"parse error|invalid yaml|failed to parse|malformed agent", re.I)
ERROR_CATEGORIES = (
    ("sandbox", re.compile(r"sandbox-exec|sandbox violation|operation not permitted", re.I)),
    ("permission", re.compile(r"permission denied|\beperm\b|eacces", re.I)),
    ("python", re.compile(r"\bpython(?:3(?:\.\d+)*)?\b|traceback", re.I)),
    ("developer-tools", re.compile(r"xcrun|developer tools|xcode-select", re.I)),
    ("missing-file", re.compile(r"no such file|not found|cannot find", re.I)),
    ("keychain", re.compile(r"secitem|keychain|securityd", re.I)),
    ("dynamic-loader", re.compile(r"dyld|library not loaded|image not found", re.I)),
    ("generic-error", ERROR),
)
PROMPT_MARKER = re.compile(r"(?:❯|›)[ \t]*$")
PROMPT_READY_HINT = re.compile(
    r"ask anything|what would you like to do|type a message|enter (?:a )?prompt|send a message|write a message",
    re.I,
)
MANAGER_HEADER = re.compile(r"available agents", re.I)
AGY_CLI_VERSION_HEADER = re.compile(
    r"[ \t\u2580-\u259f]*Antigravity CLI [0-9]+\.[0-9]+\.[0-9]+[ \t\u2580-\u259f]*", re.I)
AGY_MODEL_STATUS = re.compile(r"[ \t\u2580-\u259f]*Gemini API key[ \t\u2580-\u259f]*", re.I)
AGY_ACCOUNT_STATUS = re.compile(
    r"[ \t\u2580-\u259f]*[^\s@()]{1,64}@[^\s@()]{1,189}\.[A-Za-z]{2,24}"
    r" \(Antigravity (?P<tier>[A-Za-z]{1,24}(?: [A-Za-z]{1,24})?)\)"
    r"[ \t\u2580-\u259f]*")
ANSI = re.compile(rb"\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))")


def sha(data):
    return hashlib.sha256(data).hexdigest()


def file_sha(path):
    h = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def require_file_sha(path, expected):
    actual = file_sha(path)
    if actual != expected:
        raise RuntimeError("Pinned AGY executable hash mismatch")
    return actual


def sbpl(root):
    """Write guard; default mode permits only the native keychain service."""
    root = Path(root).resolve(strict=True)
    real_home = Path(pwd.getpwuid(os.getuid()).pw_dir).resolve()
    q = lambda value: json.dumps(str(value))
    return (
        "(version 1)(allow default)"
        f"(deny file-read* (subpath {q(real_home)}))"
        "(deny file-write*)"
        f"(allow file-write* (subpath {q(root)}) (literal \"/dev/null\") (literal \"/dev/tty\"))"
        "(deny mach-lookup)"
        '(allow mach-lookup (global-name "com.apple.securityd"))'
        '(deny process-exec (literal "/usr/bin/open"))'
        '(deny process-exec (literal "/usr/bin/osascript"))'
    )


def sbpl_isolated_profile(root, source_root):
    """Constrain a copied private profile without accessing native keychain."""
    root = Path(root).resolve(strict=True)
    source_root = Path(source_root).resolve(strict=True)
    real_home = Path(pwd.getpwuid(os.getuid()).pw_dir).resolve()
    q = lambda value: json.dumps(str(value))
    return (
        "(version 1)(allow default)"
        f"(deny file-read* (subpath {q(real_home)}))"
        f"(deny file-read* (subpath {q(source_root)}))"
        "(deny file-write*)"
        f"(allow file-write* (subpath {q(root)}) (literal \"/dev/null\") (literal \"/dev/tty\"))"
        '(deny mach-lookup (global-name "com.apple.securityd"))'
        '(deny process-exec (literal "/usr/bin/open"))'
        '(deny process-exec (literal "/usr/bin/osascript"))'
    )


def _private_owned_path(path, *, mode=None):
    path = Path(path)
    info = path.lstat()
    if stat.S_ISLNK(info.st_mode) or not stat.S_ISDIR(info.st_mode):
        raise ValueError("isolated login root must be a real directory")
    if info.st_uid != os.getuid() or (mode is not None and stat.S_IMODE(info.st_mode) != mode):
        raise ValueError("isolated login root ownership or permissions are invalid")
    if path.resolve(strict=True) != path.absolute():
        raise ValueError("isolated login root must be canonical")
    return info


def validate_isolated_login_root(path, *, now=None, private_temp_root=None):
    """Validate the bounded marker and private HOME without reading file contents."""
    path = Path(path).absolute()
    temp_root = Path(private_temp_root or PRIVATE_TEMP_ROOT).resolve(strict=True)
    if path.parent.resolve(strict=True) != temp_root:
        raise ValueError("isolated login root must be directly under private temporary storage")
    _private_owned_path(path, mode=0o700)
    marker_path = path / "owned-session.json"
    marker_stat = marker_path.lstat()
    if (not stat.S_ISREG(marker_stat.st_mode) or stat.S_ISLNK(marker_stat.st_mode) or
            marker_stat.st_uid != os.getuid() or marker_stat.st_nlink != 1 or
            stat.S_IMODE(marker_stat.st_mode) != 0o600 or marker_stat.st_size > 16 * 1024):
        raise ValueError("isolated login marker is not a private regular file")
    marker = json.loads(marker_path.read_text(encoding="utf-8"))
    try:
        session_id = uuid.UUID(marker.get("id", ""))
        created = datetime.fromisoformat(marker.get("createdAt", "").replace("Z", "+00:00"))
        expires = datetime.fromisoformat(marker.get("expiresAt", "").replace("Z", "+00:00"))
    except (ValueError, TypeError, AttributeError) as error:
        raise ValueError("isolated login marker identity or times are invalid") from error
    current = now or datetime.now(timezone.utc)
    if (marker.get("kind") != "aok-agy-native-login/v1" or session_id.version != 4 or
            str(session_id) != marker.get("id") or marker.get("valid") is not True or
            marker.get("readyPromptObserved") is not True or
            marker.get("backendAuthentication") != "not-independently-verified" or
            created.tzinfo is None or expires.tzinfo is None or current.tzinfo is None or
            expires <= current or created > current or expires <= created or
            expires - created > timedelta(minutes=15)):
        raise ValueError("isolated login marker is not currently prompt-ready")
    home = path / "home"
    _private_owned_path(home, mode=0o700)
    return {"root": path, "home": home, "marker": marker}


def copy_isolated_login_home(source_root, destination_home, *, max_files=512,
                             max_bytes=64 * 1024 * 1024):
    """Copy only bounded, single-link regular files into a fresh private HOME."""
    verified = validate_isolated_login_root(source_root)
    destination_home = Path(destination_home)
    destination_home.mkdir(mode=0o700, parents=False, exist_ok=False)
    destination_home.chmod(0o700)
    files = 0
    total = 0
    source_home = verified["home"]
    for current, directories, names in os.walk(source_home, topdown=True, followlinks=False):
        source_dir = Path(current)
        relative = source_dir.relative_to(source_home)
        target_dir = destination_home / relative
        if relative.parts:
            target_dir.mkdir(mode=0o700, parents=True, exist_ok=False)
        for name in directories:
            info = (source_dir / name).lstat()
            if not stat.S_ISDIR(info.st_mode) or stat.S_ISLNK(info.st_mode) or info.st_uid != os.getuid():
                raise ValueError("isolated login HOME contains an unsupported directory entry")
        for name in names:
            source = source_dir / name
            info = source.lstat()
            if (not stat.S_ISREG(info.st_mode) or stat.S_ISLNK(info.st_mode) or
                    info.st_uid != os.getuid() or info.st_nlink != 1):
                raise ValueError("isolated login HOME contains an unsupported file entry")
            files += 1
            if files > max_files or total + info.st_size > max_bytes:
                raise ValueError("isolated login HOME exceeds copy limits")
            flags = os.O_RDONLY | getattr(os, "O_NOFOLLOW", 0)
            fd = os.open(source, flags)
            try:
                opened = os.fstat(fd)
                if not stat.S_ISREG(opened.st_mode) or opened.st_nlink != 1 or opened.st_ino != info.st_ino:
                    raise ValueError("isolated login HOME changed during copy")
                target = target_dir / name
                with os.fdopen(fd, "rb", closefd=False) as incoming, target.open("xb") as outgoing:
                    copied = 0
                    while True:
                        block = incoming.read(1024 * 1024)
                        if not block:
                            break
                        copied += len(block)
                        if total + copied > max_bytes:
                            raise ValueError("isolated login HOME exceeds copy limits")
                        outgoing.write(block)
                    if copied != opened.st_size:
                        raise ValueError("isolated login HOME changed during copy")
                total += copied
                target.chmod(0o600)
            finally:
                os.close(fd)
    return {"filesCopied": files, "bytesCopied": total}


def env_for(root):
    home = Path(root) / "home"
    for p in (home, Path(root) / "tmp", home / ".config", home / ".cache"):
        p.mkdir(parents=True, exist_ok=True)
    return {
        "PATH": "/usr/bin:/bin",
        "HOME": str(home),
        "TMPDIR": str(Path(root) / "tmp"),
        "XDG_CONFIG_HOME": str(home / ".config"),
        "XDG_CACHE_HOME": str(home / ".cache"),
        "TERM": "xterm-256color",
        "BROWSER": "/usr/bin/false",
        "AGY_CLI_DISABLE_AUTO_UPDATE": "true",
    }


def append_audit(event, **fields):
    if AUDIT_PATH is None:
        raise RuntimeError("process audit path is not initialized")
    record = {"event": event, "at": datetime.now(timezone.utc).isoformat(), **fields}
    with Path(AUDIT_PATH).open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(record, sort_keys=True) + "\n")
        stream.flush()
        os.fsync(stream.fileno())


def _group_members(pgid):
    """Return PIDs from exact PGID lookup after checking only UID/PGID metadata."""
    result = subprocess.run(["/usr/bin/pgrep", "-g", str(pgid)], stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                            text=True, env={"PATH": "/usr/bin:/bin"}, timeout=2, check=False)
    if result.returncode == 1:
        return []
    if result.returncode != 0:
        raise RuntimeError("scoped process-group metadata lookup failed")
    pids = []
    for line in result.stdout.splitlines():
        if not line.isdecimal():
            raise RuntimeError("scoped process-group lookup returned malformed PID data")
        pid = int(line)
        detail = subprocess.run(["/bin/ps", "-o", "uid=", "-o", "pgid=", "-p", str(pid)],
                                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                                stderr=subprocess.DEVNULL, text=True,
                                env={"PATH": "/usr/bin:/bin"}, timeout=2, check=False)
        if detail.returncode != 0:
            raise RuntimeError("could not verify scoped process UID/PGID metadata")
        values = detail.stdout.split()
        if len(values) != 2 or not all(value.isdecimal() for value in values):
            raise RuntimeError("scoped process UID/PGID metadata was malformed")
        uid, observed_pgid = map(int, values)
        if uid != os.getuid() or observed_pgid != pgid:
            raise RuntimeError("scoped process group contains a foreign or mismatched process")
        pids.append(pid)
    if not pids:
        raise RuntimeError("process-group lookup reported matches without PIDs")
    return pids


def terminate_owned_group(proc, pgid, purpose, grace=1.0):
    """Terminate our exact group; skip SIGKILL only on scoped metadata absence."""
    if pgid != proc.pid:
        raise RuntimeError("refusing to signal a process group not owned by this child")
    signal_errors = []
    try:
        try:
            os.killpg(pgid, signal.SIGTERM)
        except (ProcessLookupError, PermissionError) as error:
            signal_errors.append(type(error).__name__)
        try:
            proc.wait(timeout=grace)
        except subprocess.TimeoutExpired:
            pass
        members = _group_members(pgid)
        if members:
            try:
                os.killpg(pgid, signal.SIGKILL)
            except (ProcessLookupError, PermissionError) as error:
                signal_errors.append(type(error).__name__)
            try:
                proc.wait(timeout=grace)
            except subprocess.TimeoutExpired:
                pass
            members = _group_members(pgid)
        if members or proc.poll() is None:
            raise RuntimeError("owned process group remains or leader is unreaped")
        append_audit("owned-group-cleanup", purpose=purpose, pid=proc.pid, pgid=pgid,
                     groupCleanupFailed=False, directChildFallback=False,
                     groupAbsentVerified=True, priorSignalErrors=signal_errors)
        return
    except Exception as error:
        try:
            child_reaped = audit.direct_child_cleanup(proc, grace=grace)
        except Exception:
            child_reaped = False
        try:
            append_audit("owned-group-cleanup", purpose=purpose, pid=proc.pid, pgid=pgid,
                         groupCleanupFailed=True, directChildFallback=True,
                         directChildReaped=child_reaped, descendantsMayRemain=True,
                         errorType=type(error).__name__)
        except Exception:
            pass
        raise RuntimeError(
            f"owned process-group cleanup failed: purpose={purpose} pid={proc.pid} pgid={pgid}; "
            f"direct-child fallback reaped={child_reaped}; descendants may remain"
        ) from error


def signal_preflight(root, profile=None):
    """Prove complete process-group cleanup with the exact AGY sandbox profile."""
    command = ["/usr/bin/sandbox-exec", "-p", profile or sbpl(root), "/bin/sleep", "30"]
    proc = subprocess.Popen(command, cwd=root, env=env_for(root), stdin=subprocess.DEVNULL,
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                            start_new_session=True, close_fds=True)
    try:
        pgid = audit.audit_spawned_child(root, proc, "agy-signal-preflight")
        append_audit("signal-preflight-owned", pid=proc.pid, pgid=pgid)
        terminate_owned_group(proc, pgid, "agy-signal-preflight")
        if proc.poll() is None:
            raise RuntimeError("process-group preflight did not reap its leader")
        append_audit("signal-preflight-complete", pid=proc.pid, pgid=pgid, reaped=True)
        return {"pid": proc.pid, "pgid": pgid, "terminated": True}
    except Exception:
        if proc.poll() is None:
            try:
                terminate_owned_group(proc, proc.pid, "agy-signal-preflight-failure")
            except Exception:
                pass
        try:
            append_audit("signal-preflight-failed", pid=proc.pid, pgid=proc.pid,
                         parentReaped=proc.poll() is not None)
        except Exception:
            pass
        raise


def state_from_bytes(data, *, width=160, height=80):
    try:
        text = terminal_text(data, width=width, height=height)
    except _terminal.TerminalAmbiguity as error:
        return {
            "terminalAmbiguitySite": _terminal.ambiguity_site(error),
            "startupDiagnostics": classify(data),
            "terminalAmbiguityObserved": True,
            "authBarrierObserved": False,
            "localPromptObserved": False,
            "readyHintObserved": False,
            "framedNativePromptObserved": False,
            "framedNativeManagerObserved": False,
            "publicModelStatusExceptionObserved": False,
            "welcomeLabelObserved": False,
            "workspaceLabelObserved": False,
            "modelSelectionLabelObserved": False,
            "noAgentsLabelObserved": False,
            "managerSectionObserved": False,
            "nameObserved": False,
            "descriptionObserved": False,
            "parseErrorObserved": False,
            "errorObserved": False,
            "errorCategories": ["terminal-ambiguity"],
            "bytesObserved": len(data),
        }
    rows = text.splitlines()
    tail = "\n".join(rows[-4:])
    framed_prompt, prompt_status_row = _framed_native_prompt(rows)
    framed_manager, manager_status_row = _framed_native_manager(rows)
    status_row = prompt_status_row if framed_prompt else manager_status_row
    status_exception = framed_prompt or framed_manager
    auth_rows = [row for index, row in enumerate(rows) if not status_exception or index != status_row]
    auth_text = "\n".join(auth_rows)
    error_categories = [name for name, pattern in ERROR_CATEGORIES if pattern.search(text)]
    return {
        "startupDiagnostics": classify(data),
        "terminalAmbiguityObserved": False,
        "authBarrierObserved": bool(AUTH.search(auth_text)),
        "localPromptObserved": framed_prompt or (bool(PROMPT_MARKER.search(tail)) and bool(PROMPT_READY_HINT.search(tail))),
        "readyHintObserved": bool(PROMPT_READY_HINT.search(tail)) or framed_prompt,
        "framedNativePromptObserved": framed_prompt,
        "framedNativeManagerObserved": framed_manager,
        "publicModelStatusExceptionObserved": status_exception and status_row is not None,
        "welcomeLabelObserved": bool(re.search(r"welcome to antigravity|antigravity cli", text, re.I)),
        "workspaceLabelObserved": bool(re.search(r"workspace|project", text, re.I)),
        "modelSelectionLabelObserved": bool(re.search(r"select (?:a )?model|choose (?:a )?model", text, re.I)),
        "noAgentsLabelObserved": bool(re.search(r"no (?:custom )?agents(?: found| available)?", text, re.I)),
        "managerSectionObserved": bool(MANAGER_HEADER.search(text)),
        "nameObserved": bool(re.search(r"(?<![\w-])aok-probe(?![\w-])", text)),
        "descriptionObserved": "Synthetic AGY adapter discovery fixture" in text,
        "parseErrorObserved": bool(PARSE_ERROR.search(text)),
        "errorObserved": bool(ERROR.search(text)),
        "errorCategories": error_categories,
        "bytesObserved": len(data),
    }


def _native_status_row(row):
    """Public provider row or bounded account/product envelope; never retain identity.

    A matching row alone grants no input. The complete version/model/prompt
    frame is still required, and other authentication/error rows still block.
    Product tiers are labels, not evidence of backend authentication.
    """
    if AGY_MODEL_STATUS.fullmatch(row):
        return True
    account = AGY_ACCOUNT_STATUS.fullmatch(row)
    return bool(account and not AUTH.search(account['tier']) and
                not ERROR.search(account['tier']))


def _framed_native_prompt(rows):
    """Recognize the pinned CLI's exact local `>` prompt panel, not arbitrary `>`."""
    separator = "─" * 160
    all_text = "\n".join(rows)
    if re.search(r"(?:select|choose)(?: a)? model", all_text, re.I):
        return False, None
    for index in range(len(rows) - 3):
        if not AGY_CLI_VERSION_HEADER.fullmatch(rows[index]):
            continue
        status_index = index + 1
        if not _native_status_row(rows[status_index]):
            continue
        model_row = rows[index + 2]
        if (not re.fullmatch(r"[ \t\u2580-\u259f]*[A-Za-z][A-Za-z0-9 ._()/-]{0,80}[ \t\u2580-\u259f]*", model_row) or
                not re.search(r"\bGemini\b", model_row, re.I)):
            continue
        for prompt_index in range(index + 3, len(rows) - 3):
            top = rows[prompt_index]
            prompt = rows[prompt_index + 1]
            bottom = rows[prompt_index + 2]
            footer = rows[prompt_index + 3]
            if (top == separator and prompt == ">" and bottom == separator and
                    footer.startswith("? for shortcuts")):
                return True, status_index
    return False, None


def _framed_native_manager(rows):
    """Recognize the exact public AGY manager layout before exempting its API status row."""
    separator = "─" * 160
    workspace = re.compile(
        r"\s*Workspace: (?P<root>/private/tmp/aok-agy-session-[A-Za-z0-9_-]{1,96})"
        r"/\.agents/agents/\{agent_name\}/agent\.md\s*")
    keyboard = "Keyboard: ↑/↓ Navigate  enter Select / Toggle  k Kill Active Subagent  esc Go Back"
    if re.search(r"(?:select|choose)(?: a)? model", "\n".join(rows), re.I):
        return False, None
    for header in range(len(rows) - 3):
        if not AGY_CLI_VERSION_HEADER.fullmatch(rows[header]):
            continue
        status = header + 1
        model = header + 2
        if (not _native_status_row(rows[status]) or
                not re.fullmatch(r"[ \t\u2580-\u259f]*[A-Za-z][A-Za-z0-9 ._()/-]{0,80}[ \t\u2580-\u259f]*", rows[model]) or
                not re.search(r"\bGemini\b", rows[model], re.I)):
            continue
        for top_index in range(model + 1, len(rows) - 7):
            if (rows[top_index] != separator or rows[top_index + 1] != ">" or
                    rows[top_index + 2] != separator or rows[top_index + 3] != "Create New Agents"):
                continue
            workspace_index = top_index + 4
            global_index = top_index + 5
            workspace_match = workspace.fullmatch(rows[workspace_index])
            if not workspace_match:
                continue
            root = workspace_match.group("root")
            global_agents = re.compile(
                r"\s*Global: " + re.escape(root) +
                r"/home/\.gemini/config/agents/\{agent_name\}/agent\.md\s*")
            if not global_agents.fullmatch(rows[global_index]):
                continue
            available = next((i for i in range(global_index + 1, len(rows))
                              if rows[i] == "Available Agents"), None)
            if available is None:
                continue
            if not any(row.startswith(keyboard) for row in rows[available + 1:]):
                continue
            return True, status
    return False, None


def may_send_manager_command(state):
    return (state.get("localPromptObserved") is True and not state.get("authBarrierObserved") and
            not state.get("errorObserved") and not state.get("terminalAmbiguityObserved"))


def _capture(master, seconds, limit=512 * 1024):
    end = time.monotonic() + seconds
    chunks = []
    total = 0
    while time.monotonic() < end and total < limit:
        ready, _, _ = select.select([master], [], [], min(0.15, max(0.0, end - time.monotonic())))
        if not ready:
            continue
        try:
            block = os.read(master, min(8192, limit - total))
        except BlockingIOError:
            continue
        except OSError:
            break
        if not block:
            break
        chunks.append(block)
        total += len(block)
        current = b"".join(chunks)
        state = state_from_bytes(current)
        if state["authBarrierObserved"] or state["localPromptObserved"] or state["errorObserved"]:
            break
    return b"".join(chunks)


def claim_controlling_tty():
    """Attach inherited PTY fd 0 as the new session's controlling terminal."""
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)


def run_manager_case(root, project, cli, case, timeout=25, *, isolated_source_root=None):
    master, slave = pty.openpty()
    os.set_blocking(master, False)
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 80, 160, 0, 0))
    gate = Path(root) / ("start-" + case)
    gate_script = (
        "import os,sys,time,pathlib; gate=pathlib.Path(sys.argv[1]); exe=sys.argv[2]; "
        "gate.with_suffix('.runner').touch(); "
        "args=sys.argv[3:]; "
        "\nwhile not gate.exists(): time.sleep(0.025)\n"
        "gate.with_suffix('.exec').touch(); os.execv(exe,[exe,*args])"
    )
    interpreter = str(Path(sys.executable).resolve(strict=True))
    profile = sbpl_isolated_profile(root, isolated_source_root) if isolated_source_root else sbpl(root)
    argv = ["/usr/bin/sandbox-exec", "-p", profile, interpreter, "-B", "-c",
            gate_script, str(gate), str(cli)]
    env = env_for(root)
    proc = None
    pgid = None
    try:
        # start_new_session establishes the process group; preexec_fn assigns
        # the inherited PTY as its controlling terminal for TUI code using /dev/tty.
        proc = subprocess.Popen(argv, cwd=project, env=env, stdin=slave, stdout=slave,
                                stderr=slave, start_new_session=True,
                                preexec_fn=claim_controlling_tty, close_fds=True)
        os.close(slave)
        pgid = audit.audit_spawned_child(root, proc, "agy-interactive-" + case)
        started = time.monotonic()
        sent = False
        append_audit("native-startup-gated", case=case, pid=proc.pid, pgid=pgid,
                     gate="case-owned-startup-gate")
        gate.touch()
        raw = _capture(master, timeout)
        state = state_from_bytes(raw)
        if may_send_manager_command(state):
            os.write(master, b"/agents\r")
            sent = True
            append_audit("documented-agents-command-sent", case=case, pid=proc.pid, pgid=pgid)
            raw += _capture(master, 12)
            state = state_from_bytes(raw)
        state["runnerStarted"] = gate.with_suffix(".runner").exists()
        state["nativeExecAttempted"] = gate.with_suffix(".exec").exists()
        state["commandSent"] = sent
        state["durationSeconds"] = round(time.monotonic() - started, 2)
        append_audit("native-case-observed", case=case, pid=proc.pid, pgid=pgid,
                     state={key: state[key] for key in (
                         "runnerStarted", "nativeExecAttempted", "startupDiagnostics", "terminalAmbiguityObserved", "authBarrierObserved", "localPromptObserved", "readyHintObserved",
                         "framedNativePromptObserved", "publicModelStatusExceptionObserved",
                         "welcomeLabelObserved", "workspaceLabelObserved", "modelSelectionLabelObserved",
                         "noAgentsLabelObserved", "managerSectionObserved", "nameObserved",
                         "descriptionObserved", "parseErrorObserved", "errorObserved", "errorCategories",
                         "bytesObserved", "commandSent", "durationSeconds")})
        return state
    finally:
        try:
            # Closing the master first delivers the terminal hangup to an
            # interactive child before process-group metadata is inspected.
            try:
                os.close(master)
            except OSError:
                pass
        finally:
            try:
                if proc is not None and pgid is not None:
                    terminate_owned_group(proc, pgid, "agy-interactive-" + case)
                elif proc is not None:
                    # A failed audit means we cannot safely signal descendants. Reap
                    # only the direct child and invalidate this attempt by raising.
                    audit.direct_child_cleanup(proc, grace=1.0)
                    raise RuntimeError("process identity audit failed; native case is invalid")
            finally:
                try:
                    os.close(slave)
                except OSError:
                    pass


def qualifies_positive(state):
    return (state.get("commandSent") is True and state.get("managerSectionObserved") is True and
            state.get("nameObserved") is True and state.get("descriptionObserved") is True and
            not state.get("authBarrierObserved") and not state.get("errorObserved") and
            not state.get("parseErrorObserved") and not state.get("terminalAmbiguityObserved"))


def qualifies_absent(state):
    return (state.get("commandSent") is True and state.get("managerSectionObserved") is True and
            not state.get("nameObserved") and not state.get("descriptionObserved") and
            not state.get("authBarrierObserved") and not state.get("errorObserved") and
            not state.get("parseErrorObserved") and not state.get("terminalAmbiguityObserved"))


def qualifies_malformed(state):
    return (state.get("commandSent") is True and state.get("managerSectionObserved") is True and
            not state.get("nameObserved") and not state.get("descriptionObserved") and
            state.get("parseErrorObserved") is True and not state.get("authBarrierObserved") and
            not state.get("terminalAmbiguityObserved"))


def has_candidate_manager_evidence(cases):
    return (isinstance(cases, dict) and all(k in cases for k in ("positive", "absent", "malformed")) and
            qualifies_positive(cases["positive"]) and qualifies_absent(cases["absent"]) and
            qualifies_malformed(cases["malformed"]))


def run_all_cases(root, projects, cli, *, isolated_source_root=None):
    profile = sbpl_isolated_profile(root, isolated_source_root) if isolated_source_root else sbpl(root)
    preflight = signal_preflight(root, profile)
    cases = {}
    for case in ("positive", "absent", "malformed"):
        cases[case] = run_manager_case(root, projects / case, cli, case,
                                       isolated_source_root=isolated_source_root)
    return preflight, cases


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--aok", required=True, type=Path)
    parser.add_argument("--binary", type=Path, default=PINNED_BINARY)
    parser.add_argument("--isolated-login-root", type=Path,
                        help="copy an unexpired prompt-ready AOK private profile; backend authentication remains unverified")
    parser.add_argument("--evidence-out", required=True, type=Path)
    parser.add_argument("--audit-out", required=True, type=Path)
    args = parser.parse_args()
    if sys.platform != "darwin" or os.uname().machine not in ("x86_64", "i386"):
        raise SystemExit("probe is pinned for Darwin x64")
    evidence_out = args.evidence_out.resolve()
    audit_out = args.audit_out.resolve()
    if (REPO not in evidence_out.parents or REPO not in audit_out.parents or
            evidence_out == audit_out):
        raise SystemExit("evidence and audit outputs must be distinct files inside this checkout")
    if evidence_out.exists() or audit_out.exists():
        raise SystemExit("evidence paths must be new")
    if not evidence_out.parent.is_dir() or not audit_out.parent.is_dir():
        raise SystemExit("evidence and audit parent directories must already exist")
    source_hash = sha(Path(__file__).read_bytes())
    snapshot = evidence_out.parent / f"agy-macos-interactive-source-{source_hash}.py"
    if snapshot.exists() or snapshot in (evidence_out, audit_out):
        raise SystemExit("source snapshot already exists")
    binary = args.binary.resolve(strict=True)
    binary_hash = require_file_sha(binary, PINNED_BINARY_SHA256)
    aok_source = args.aok.resolve(strict=True)
    global AUDIT_PATH
    AUDIT_PATH = audit_out
    audit.AUDIT_PATH = AUDIT_PATH
    source_bytes = Path(__file__).read_bytes()
    if sha(source_bytes) != source_hash:
        raise SystemExit("probe source changed during preflight")
    snapshot.write_bytes(source_bytes)
    if sha(snapshot.read_bytes()) != source_hash:
        raise SystemExit("source snapshot verification failed")
    append_audit("source-pinned", path=str(snapshot.relative_to(REPO)), sha256=source_hash)

    with tempfile.TemporaryDirectory(prefix="agy-macos-manager-", dir=PRIVATE_TEMP_ROOT) as td:
        root = Path(td).resolve()
        owned_cli = root / "antigravity"
        shutil.copyfile(binary, owned_cli)
        owned_cli.chmod(0o500)
        if file_sha(owned_cli) != PINNED_BINARY_SHA256:
            raise RuntimeError("task-owned AGY copy failed integrity check")
        isolated_login = None
        profile_copy = None
        if args.isolated_login_root:
            isolated_login = validate_isolated_login_root(args.isolated_login_root)
            profile_copy = copy_isolated_login_home(isolated_login["root"], root / "home")
        owned_aok, rendered, rendered_sha, description = agy.base.prepare_aok(aok_source, REPO, root)
        projects = agy.create_project_inputs(root, rendered)

        # The subprocess-group signal preflight uses the exact same profile as
        # AGY, before the native binary is started for any case.
        preflight, cases = run_all_cases(
            root, projects, owned_cli,
            isolated_source_root=isolated_login["root"] if isolated_login else None,
        )
        report = {
            "target": "antigravity", "scope": "project", "version": "1.4.2",
            "binarySha256": binary_hash, "ownedBinarySha256": file_sha(owned_cli),
            "aokSha256": sha(owned_aok.read_bytes()), "fixtureSha256": rendered_sha,
            "expectedName": "aok-probe", "expectedDescription": description,
            "authScope": ("prepared private file profile; backend authentication not independently verified"
                          if isolated_login else
                          "existing native account use through AGY CLI keychain lookup only; no credential read/copy"),
            "sandbox": {"originalHomeReads": "denied", "writesOutsideOwnedTemporaryRoot": "denied",
                        "keychainMachLookup": ("denied" if isolated_login else "com.apple.securityd only"),
                        "network": "allowed for native account refresh and /agents manager metadata",
                        "browserLaunch": "blocked", "ordinaryPrompt": "not sent", "agentSelection": "not performed"},
            "signalPreflight": preflight,
            "cases": cases,
            "nativeLoading": "not-verified",
            "candidateManagerEvidence": has_candidate_manager_evidence(cases),
            "runtimeEnforcement": "unverified", "modelInvocation": "not-run",
            "authentication": ("not-independently-verified" if isolated_login else
                                "existing-keyring-only; no fresh OAuth attempted"),
            "profileSourcePath": "not-retained" if isolated_login else None,
            "profileCopy": profile_copy,
            "probeSource": str(snapshot.relative_to(REPO)), "probeSourceSha256": source_hash,
            "diagnosticsHelperSHA256": file_sha(Path(__file__).with_name("native_startup_diagnostics.py")),
            "terminalRendererSHA256": file_sha(Path(__file__).with_name("native_terminal.py")),
            "launcherInterpreterSHA256": file_sha(Path(sys.executable).resolve(strict=True)),
            "launcherInterpreter": "resolved current Python executable; Apple developer-tools shim bypassed",
            "auditPath": str(AUDIT_PATH.relative_to(REPO)),
            "observedAt": datetime.now(timezone.utc).isoformat(),
        }
    evidence_out.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
