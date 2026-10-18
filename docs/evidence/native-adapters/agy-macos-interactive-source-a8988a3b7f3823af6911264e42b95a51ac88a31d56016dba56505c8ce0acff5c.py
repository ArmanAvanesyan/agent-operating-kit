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


def signal_preflight(root):
    """Prove complete process-group cleanup with the exact AGY sandbox profile."""
    command = ["/usr/bin/sandbox-exec", "-p", sbpl(root), "/bin/sleep", "30"]
    proc = subprocess.Popen(command, cwd=root, env=env_for(root), stdin=subprocess.DEVNULL,
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                            start_new_session=True, close_fds=True)
    try:
        pgid = audit.audit_spawned_child(root, proc, "agy-signal-preflight")
        append_audit("signal-preflight-owned", pid=proc.pid, pgid=pgid)
        audit.terminate_owned_group(proc, pgid, "agy-signal-preflight", audit_file=AUDIT_PATH)
        if proc.poll() is None:
            raise RuntimeError("process-group preflight did not reap its leader")
        append_audit("signal-preflight-complete", pid=proc.pid, pgid=pgid, reaped=True)
        return {"pid": proc.pid, "pgid": pgid, "terminated": True}
    except Exception:
        if proc.poll() is None:
            try:
                audit.terminate_owned_group(proc, proc.pid, "agy-signal-preflight-failure",
                                            audit_file=AUDIT_PATH)
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
    return {
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


def run_manager_case(root, project, cli, case, timeout=25):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    gate = Path(root) / ("start-" + case)
    gate_script = (
        "import os,sys,time,pathlib; gate=pathlib.Path(sys.argv[1]); exe=sys.argv[2]; "
        "args=sys.argv[3:]; "
        "\nwhile not gate.exists(): time.sleep(0.025)\n"
        "os.execv(exe,[exe,*args])"
    )
    # AGY receives no speculative workspace flag; cwd is the initialized
    # synthetic Git project, matching the prior verified CLI invocation shape.
    argv = ["/usr/bin/python3", "-B", "-c", gate_script, str(gate), str(cli)]
    process = None
    try:
        process = subprocess.Popen(["/usr/bin/sandbox-exec", "-p", sbpl(root), *argv],
                                   cwd=project, env=env_for(root), stdin=slave, stdout=slave,
                                   stderr=slave, start_new_session=True, close_fds=True)
    except Exception:
        os.close(master)
        os.close(slave)
        raise
    os.close(slave)
    try:
        pgid = audit.audit_spawned_child(root, process, "agy-interactive-" + case)
    except Exception:
        os.close(master)
        raise
    started = time.monotonic()
    sent = False
    try:
        append_audit("native-startup-gated", case=case, pid=process.pid, pgid=pgid,
                     gate="case-owned-startup-gate")
        gate.touch()
        raw = _capture(master, timeout)
        state = state_from_bytes(raw)
        if may_send_manager_command(state):
            os.write(master, b"/agents\n")
            sent = True
            append_audit("documented-agents-command-sent", case=case, pid=process.pid, pgid=pgid)
            raw += _capture(master, 12)
            state = state_from_bytes(raw)
        state["commandSent"] = sent
        state["durationSeconds"] = round(time.monotonic() - started, 2)
        return state
    finally:
        try:
            audit.terminate_owned_group(process, pgid, "agy-interactive-" + case, audit_file=AUDIT_PATH)
        finally:
            os.close(master)


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
            "auditPath": str(AUDIT_PATH.relative_to(REPO)),
            "observedAt": datetime.now(timezone.utc).isoformat(),
        }
    evidence_out.write_text(json.dumps(report, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps(report, indent=2, sort_keys=True))


if __name__ == "__main__":
    main()
