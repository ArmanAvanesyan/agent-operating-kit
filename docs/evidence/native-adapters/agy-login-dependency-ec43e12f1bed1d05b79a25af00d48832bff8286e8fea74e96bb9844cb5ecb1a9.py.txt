#!/usr/bin/env python3
"""Probe AGY's interactive /agents manager with a task-owned macOS profile.

Uses only the pinned copied Darwin CLI and an existing native keyring login.
It never copies/reads credentials, sends ordinary prompts, selects an agent,
or runs any imported command. Terminal text is inspected in memory and only
sanitized markers are written to evidence.
"""
import argparse
from datetime import datetime, timezone
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
    """Write guard; only securityd Mach lookup is allowed for keychain use."""
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


def signal_preflight(root):
    """Prove complete process-group cleanup with the exact AGY sandbox profile."""
    command = ["/usr/bin/sandbox-exec", "-p", sbpl(root), "/bin/sleep", "30"]
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


def state_from_bytes(data):
    text = ANSI.sub(b"", data).decode("utf-8", "replace").replace("\r", "\n")
    tail = "\n".join(text.splitlines()[-4:])
    error_categories = [name for name, pattern in ERROR_CATEGORIES if pattern.search(text)]
    return {
        "startupDiagnostics": classify(data),
        "authBarrierObserved": bool(AUTH.search(text)),
        "localPromptObserved": bool(PROMPT_MARKER.search(tail)) and bool(PROMPT_READY_HINT.search(tail)),
        "readyHintObserved": bool(PROMPT_READY_HINT.search(tail)),
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


def may_send_manager_command(state):
    return (state.get("localPromptObserved") is True and not state.get("authBarrierObserved") and
            not state.get("errorObserved"))


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
        except OSError:
            break
        if not block:
            break
        chunks.append(block)
        total += len(block)
        current = b"\n".join(chunks)
        state = state_from_bytes(current)
        if state["authBarrierObserved"] or state["localPromptObserved"] or state["errorObserved"]:
            break
    return b"\n".join(chunks)


def claim_controlling_tty():
    """Attach inherited PTY fd 0 as the new session's controlling terminal."""
    fcntl.ioctl(0, termios.TIOCSCTTY, 0)


def run_manager_case(root, project, cli, case, timeout=25):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    gate = Path(root) / ("start-" + case)
    gate_script = (
        "import os,sys,time,pathlib; gate=pathlib.Path(sys.argv[1]); exe=sys.argv[2]; "
        "gate.with_suffix('.runner').touch(); "
        "args=sys.argv[3:]; "
        "\nwhile not gate.exists(): time.sleep(0.025)\n"
        "gate.with_suffix('.exec').touch(); os.execv(exe,[exe,*args])"
    )
    interpreter = str(Path(sys.executable).resolve(strict=True))
    argv = ["/usr/bin/sandbox-exec", "-p", sbpl(root), interpreter, "-B", "-c",
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
            os.write(master, b"/agents\n")
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
                         "runnerStarted", "nativeExecAttempted", "startupDiagnostics", "authBarrierObserved", "localPromptObserved", "readyHintObserved",
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
            not state.get("parseErrorObserved"))


def qualifies_absent(state):
    return (state.get("commandSent") is True and state.get("managerSectionObserved") is True and
            not state.get("nameObserved") and not state.get("descriptionObserved") and
            not state.get("authBarrierObserved") and not state.get("errorObserved") and
            not state.get("parseErrorObserved"))


def qualifies_malformed(state):
    return (state.get("commandSent") is True and state.get("managerSectionObserved") is True and
            not state.get("nameObserved") and not state.get("descriptionObserved") and
            state.get("parseErrorObserved") is True and not state.get("authBarrierObserved"))


def has_candidate_manager_evidence(cases):
    return (isinstance(cases, dict) and all(k in cases for k in ("positive", "absent", "malformed")) and
            qualifies_positive(cases["positive"]) and qualifies_absent(cases["absent"]) and
            qualifies_malformed(cases["malformed"]))


def run_all_cases(root, projects, cli):
    preflight = signal_preflight(root)
    cases = {}
    for case in ("positive", "absent", "malformed"):
        cases[case] = run_manager_case(root, projects / case, cli, case)
    return preflight, cases


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--aok", required=True, type=Path)
    parser.add_argument("--binary", type=Path, default=PINNED_BINARY)
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
        owned_aok, rendered, rendered_sha, description = agy.base.prepare_aok(aok_source, REPO, root)
        projects = agy.create_project_inputs(root, rendered)

        # The subprocess-group signal preflight uses the exact same profile as
        # AGY, before the native binary is started for any case.
        preflight, cases = run_all_cases(root, projects, owned_cli)
        report = {
            "target": "antigravity", "scope": "project", "version": "1.4.2",
            "binarySha256": binary_hash, "ownedBinarySha256": file_sha(owned_cli),
            "aokSha256": sha(owned_aok.read_bytes()), "fixtureSha256": rendered_sha,
            "expectedName": "aok-probe", "expectedDescription": description,
            "authScope": "existing native account use through AGY CLI keychain lookup only; no credential read/copy",
            "sandbox": {"originalHomeReads": "denied", "writesOutsideOwnedTemporaryRoot": "denied",
                        "keychainMachLookup": "com.apple.securityd only", "network": "allowed for native account refresh and /agents manager metadata",
                        "browserLaunch": "blocked", "ordinaryPrompt": "not sent", "agentSelection": "not performed"},
            "signalPreflight": preflight,
            "cases": cases,
            "nativeLoading": "not-verified",
            "candidateManagerEvidence": has_candidate_manager_evidence(cases),
            "runtimeEnforcement": "unverified", "modelInvocation": "not-run",
            "authentication": "existing-keyring-only; no fresh OAuth attempted",
            "probeSource": str(snapshot.relative_to(REPO)), "probeSourceSha256": source_hash,
            "diagnosticsHelperSHA256": file_sha(Path(__file__).with_name("native_startup_diagnostics.py")),
            "launcherInterpreterSHA256": file_sha(Path(sys.executable).resolve(strict=True)),
            "launcherInterpreter": "resolved current Python executable; Apple developer-tools shim bypassed",
            "auditPath": str(AUDIT_PATH.relative_to(REPO)),
            "observedAt": datetime.now(timezone.utc).isoformat(),
        }
    evidence_out.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
