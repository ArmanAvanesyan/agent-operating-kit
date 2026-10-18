#!/usr/bin/env python3
"""Reproduce the bounded no-input audit for Cursor Agent 2026.10.16.

This verifies package identity, generates a synthetic AOK export, checks the
documented CLI help surface, and observes startup without typing or prompting
the model. It never downloads or installs Cursor and never uses credentials.
"""

import argparse
from datetime import datetime, timezone
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pty
import platform
import pwd
import re
import select
import shutil
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time
import tarfile

sys.dont_write_bytecode = True

VERSION = "2026.10.16-e3cb207"
PRIVATE_TEMP_ROOT = "/private/tmp"
PACKAGE_SHA256 = "93372e384e9397b54b1e4fdb1812662f68e7b61c0c60cb3f2f4b788c9e0779c4"
CLI_SHA256 = "9c9673dc34c8aac856a3206519d8b595586b77ff62d1a18e0cd73a126d0e8d1b"
INDEX_SHA256 = "c3295e7743ad8c39f967bcfd0acc6c20d47e983a4d7b94254282ef74b27e5033"
FIXTURE_SHA256 = "3a248c5de1d02dec0aa40218074d5beafa081747f2db5326c7ae92e402c8eae5"
ACTIVE_ROOT = None
AUDIT_DIR = None
AUDIT_PATH = None


def sha(data):
    return hashlib.sha256(data).hexdigest()


def file_sha(path):
    h = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def require_sha(path, expected, label):
    actual = file_sha(path)
    if actual != expected:
        raise RuntimeError(f"{label} SHA-256 mismatch: {actual}")
    return actual


def safe_extract(archive, destination):
    destination.mkdir(parents=True, exist_ok=False)
    root = destination.resolve()
    with tarfile.open(archive, "r:gz") as tar:
        for member in tar.getmembers():
            name = member.name.replace("\\", "/")
            parts = Path(name).parts
            if not name or name.startswith("/") or any(part in ("", ".", "..") for part in parts):
                raise RuntimeError("unsafe path in verified vendor archive")
            if not (member.isdir() or member.isfile()):
                raise RuntimeError("unsupported link or special file in vendor archive")
            target = (root / Path(*parts)).resolve()
            if target != root and root not in target.parents:
                raise RuntimeError("vendor archive path escaped extraction root")
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            source = tar.extractfile(member)
            if source is None:
                raise RuntimeError("vendor archive regular file has no contents")
            with source, target.open("xb") as output:
                shutil.copyfileobj(source, output, length=1024 * 1024)
            target.chmod(member.mode & 0o755)


def sandbox_prefix(root):
    home = Path(pwd.getpwuid(os.getuid()).pw_dir).resolve()
    q = lambda value: json.dumps(str(value))
    profile = (
        '(version 1)(allow default)(deny network*)'
        '(deny mach-lookup (global-name "com.apple.securityd"))'
        f'(deny file-read* (subpath {q(home)}))'
        '(deny file-write*)'
        f'(allow file-write* (subpath {q(root)}) '
        '(literal "/dev/null") (literal "/dev/tty"))'
    )
    return ["/usr/bin/sandbox-exec", "-p", profile]


def env_for(root):
    home = root / "home"
    temp = root / "tmp"
    config = root / "config"
    data = root / "data"
    for path in (home, temp, config, data):
        path.mkdir(parents=True, exist_ok=True)
    return [
        "/usr/bin/env", "-i", "PATH=/usr/bin:/bin",
        f"HOME={home}", f"TMPDIR={temp}", f"CURSOR_CONFIG_DIR={config}", f"CURSOR_DATA_DIR={data}",
    ]


def audit_group(root, proc, purpose):
    record = {"event": "owned_process_group", "purpose": purpose, "pid": proc.pid, "expectedPGID": proc.pid}
    audit_file = AUDIT_PATH or (root / "process-audit.jsonl")
    with audit_file.open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(record, sort_keys=True) + "\n")
        stream.flush()
        os.fsync(stream.fileno())
    pgid = os.getpgid(proc.pid)
    if pgid != proc.pid:
        raise RuntimeError(f"owned process group invariant failed: pid={proc.pid} pgid={pgid}")
    record["pgid"] = pgid
    with audit_file.open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(record, sort_keys=True) + "\n")
        stream.flush()
        os.fsync(stream.fileno())
    return pgid


def append_audit(record, audit_file=None):
    audit_file = audit_file or AUDIT_PATH
    if audit_file is None:
        return
    with audit_file.open("a", encoding="utf-8") as stream:
        stream.write(json.dumps(record, sort_keys=True) + "\n")
        stream.flush()
        os.fsync(stream.fileno())


def direct_child_cleanup(proc, grace=0.5):
    """Best-effort bounded fallback; it cannot guarantee descendant cleanup."""
    try:
        proc.send_signal(signal.SIGTERM)
    except ProcessLookupError:
        pass
    except OSError:
        # Still attempt a bounded wait and SIGKILL below; preserve the group
        # cleanup failure in the caller's audit record.
        pass
    try:
        proc.wait(timeout=grace)
    except subprocess.TimeoutExpired:
        pass
    except OSError:
        pass
    if proc.poll() is None:
        try:
            proc.send_signal(signal.SIGKILL)
        except ProcessLookupError:
            pass
        except OSError:
            pass
        try:
            proc.wait(timeout=grace)
        except subprocess.TimeoutExpired:
            return False
        except OSError:
            return False
    return proc.poll() is not None


def terminate_owned_group(proc, pgid, purpose, grace=1.0, audit_file=None):
    try:
        terminate_group(proc, pgid, grace)
        append_audit({"event": "owned_group_cleanup", "purpose": purpose, "pid": proc.pid,
                      "pgid": pgid, "groupCleanupFailed": False, "directChildFallback": False}, audit_file)
        return
    except Exception as error:
        fallback_error = None
        try:
            child_reaped = direct_child_cleanup(proc)
        except Exception as child_error:
            child_reaped = False
            fallback_error = type(child_error).__name__
        append_audit({"event": "owned_group_cleanup", "purpose": purpose, "pid": proc.pid,
                      "pgid": pgid, "groupCleanupFailed": True, "directChildFallback": True,
                      "directChildReaped": child_reaped, "descendantsMayRemain": True,
                      "errorType": type(error).__name__, "fallbackErrorType": fallback_error}, audit_file)
        raise RuntimeError(f"owned process-group cleanup failed: purpose={purpose} pid={proc.pid} pgid={pgid}; direct-child fallback reaped={child_reaped}; descendants may remain") from error


def audit_spawned_child(root, proc, purpose):
    try:
        return audit_group(root, proc, purpose)
    except Exception as error:
        try:
            terminate_owned_group(proc, proc.pid, f"audit-failure:{purpose}", audit_file=AUDIT_PATH or (root / "process-audit.jsonl"))
        except Exception as cleanup_error:
            raise RuntimeError(f"process audit failed for pid={proc.pid}; cleanup also failed: {type(cleanup_error).__name__}") from error
        raise RuntimeError(f"process audit failed for pid={proc.pid}; child was stopped and no command result is accepted") from error


def terminate_group(proc, pgid, grace=1.0):
    if pgid != proc.pid:
        raise RuntimeError("refusing process-group signal: group is not owned by child")
    try:
        os.killpg(pgid, signal.SIGTERM)
    except ProcessLookupError:
        proc.wait(timeout=grace)
        return
    try:
        proc.wait(timeout=grace)
    except subprocess.TimeoutExpired:
        pass
    try:
        os.killpg(pgid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    proc.wait(timeout=grace)


def signal_preflight(root):
    proc = subprocess.Popen(sandbox_prefix(root) + env_for(root) + ["/bin/sleep", "30"],
                            cwd=root, start_new_session=True, stdin=subprocess.DEVNULL,
                            stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, close_fds=True)
    pgid = audit_spawned_child(root, proc, "signal-preflight")
    try:
        terminate_owned_group(proc, pgid, "signal-preflight", audit_file=AUDIT_PATH or (root / "process-audit.jsonl"))
    except Exception as error:
        raise RuntimeError(f"process-group signal preflight failed: pid={proc.pid} pgid={pgid}") from error
    if proc.poll() is None:
        raise RuntimeError(f"process-group preflight failed to terminate pid={proc.pid} pgid={pgid}")
    return {"pid": proc.pid, "pgid": pgid, "terminated": True}


def run_bounded(command, root, timeout=15, purpose="command"):
    proc = subprocess.Popen(
        sandbox_prefix(root) + env_for(root) + command,
        cwd=root, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT, start_new_session=True, close_fds=True,
    )
    pgid = audit_spawned_child(root, proc, purpose)
    try:
        output, _ = proc.communicate(timeout=timeout)
        # The leader may exit while a descendant remains in its process group.
        terminate_owned_group(proc, pgid, purpose, audit_file=AUDIT_PATH or (root / "process-audit.jsonl"))
        return proc.returncode, output
    except subprocess.TimeoutExpired:
        try:
            terminate_owned_group(proc, pgid, purpose, audit_file=AUDIT_PATH or (root / "process-audit.jsonl"))
        except Exception:
            if proc.stdout:
                proc.stdout.close()
            raise
        try:
            output, _ = proc.communicate(timeout=1.0)
        except subprocess.TimeoutExpired as detached_pipe:
            if proc.stdout:
                proc.stdout.close()
            partial = detached_pipe.output or b""
            raise RuntimeError(f"bounded command timed out with detached pipe: purpose={purpose} pid={proc.pid} pgid={pgid} bytes={len(partial)}")
        raise RuntimeError(f"bounded command timed out: purpose={purpose} pid={proc.pid} pgid={pgid} bytes={len(output)}")


def read_no_input(command, root, timeout=8, byte_limit=65536):
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    try:
        process = subprocess.Popen(
            sandbox_prefix(root) + env_for(root) + command,
            cwd=root, stdin=slave, stdout=slave, stderr=slave,
            start_new_session=True, close_fds=True,
        )
    except Exception:
        os.close(master)
        os.close(slave)
        raise
    os.close(slave)
    try:
        pgid = audit_spawned_child(root, process, "no-input-cursor-startup")
    except Exception:
        os.close(master)
        raise
    chunks = []
    total = 0
    started = time.monotonic()
    deadline = started + timeout
    try:
        while time.monotonic() < deadline and total < byte_limit:
            ready, _, _ = select.select([master], [], [], max(0, deadline - time.monotonic()))
            if not ready:
                break
            try:
                chunk = os.read(master, min(8192, byte_limit - total))
            except OSError:
                break
            if not chunk:
                break
            chunks.append(chunk)
            total += len(chunk)
    finally:
        try:
            terminate_owned_group(process, pgid, "no-input-cursor-startup", audit_file=AUDIT_PATH or (root / "process-audit.jsonl"))
        finally:
            os.close(master)
    output = b"".join(chunks).decode("utf-8", "replace")
    output = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", output)
    return process.returncode, output, total, round(time.monotonic() - started, 2)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--package", type=Path, required=True, help="official tar.gz; downloaded separately")
    parser.add_argument("--host-dir", type=Path, help=argparse.SUPPRESS)
    parser.add_argument("--aok", type=Path, required=True, help="AOK Go CLI binary")
    args = parser.parse_args()

    package = args.package.resolve(strict=True)
    aok = args.aok.resolve(strict=True)
    require_sha(package, PACKAGE_SHA256, "official Cursor package")
    if platform.machine() not in ("x86_64", "AMD64") or sys.platform != "darwin":
        raise RuntimeError("probe package is pinned for darwin-x64 only")

    repo = Path(__file__).resolve().parents[2]
    fixture = repo / "internal/nativeadapters/cursor/testdata/reviewer.md"
    require_sha(fixture, FIXTURE_SHA256, "AOK Cursor fixture")

    global ACTIVE_ROOT, AUDIT_DIR, AUDIT_PATH
    AUDIT_DIR = Path(tempfile.mkdtemp(prefix="aok-cursor-owned-audit-", dir=PRIVATE_TEMP_ROOT))
    AUDIT_PATH = AUDIT_DIR / "process-audit.jsonl"
    with tempfile.TemporaryDirectory(prefix="acf-", dir=PRIVATE_TEMP_ROOT) as temporary:
        root = Path(temporary).resolve()
        ACTIVE_ROOT = root
        owned = root / "owned"
        owned.mkdir()
        owned_package = owned / "agent-cli-package.tar.gz"
        shutil.copyfile(package, owned_package)
        require_sha(owned_package, PACKAGE_SHA256, "copied official Cursor package")
        host_dir = owned / "host"
        safe_extract(owned_package, host_dir)
        host_dir = host_dir / "dist-package"
        cli = host_dir / "cursor-agent-sea"
        index = host_dir / "index.js"
        require_sha(cli, CLI_SHA256, "copied Cursor CLI")
        require_sha(index, INDEX_SHA256, "copied Cursor CLI bundle source")
        owned_aok = owned / "aok"
        shutil.copyfile(aok, owned_aok)
        owned_aok.chmod(0o700)
        aok_sha = file_sha(owned_aok)
        # Prove whole-process-group signaling before any vendor executable runs.
        preflight_result = signal_preflight(root)
        source = root / "project"
        native = source / ".cursor/agents/reviewer.md"
        native.parent.mkdir(parents=True)
        shutil.copyfile(fixture, native)
        (source / "aok-native-input.json").write_text(json.dumps({
            "version": "aok-native-input/v1",
            "files": [{"path": ".cursor/agents/reviewer.md", "format": "markdown"}],
        }, indent=2) + "\n")

        aok_prefix = str(owned_aok)
        common = [aok_prefix, "agent", "native"]
        import_code, import_bytes = run_bounded(common + [
            "import", "--target", "cursor", "--scope", "project",
            "--input", str(source), "--out", str(root / "draft"), "--json",
        ], root, purpose="aok-import")
        imported = json.loads(import_bytes)
        if import_code != 1 or imported.get("status") != "blocked":
            raise RuntimeError("expected an incomplete import draft to be blocked")
        export_code, export_bytes = run_bounded(common + [
            "export", "--target", "cursor", "--scope", "project",
            "--input", str(root / "draft/aok-native.json"), "--source", str(source),
            "--out", str(root / "export"), "--json",
        ], root, purpose="aok-export")
        exported = json.loads(export_bytes)
        if export_code != 1 or exported.get("status") != "blocked":
            raise RuntimeError("expected incomplete export to remain blocked")
        compare_code, compare_bytes = run_bounded(common + [
            "compare", "--target", "cursor", "--scope", "project",
            "--input", str(root / "export"), "--against", str(root / "export/aok-native.json"), "--json",
        ], root, purpose="aok-compare")
        compared = json.loads(compare_bytes)
        if compare_code != 0 or compared.get("status") != "valid":
            raise RuntimeError("expected the export's static comparison to be valid")
        rendered = root / "export/.cursor/agents/reviewer.md"
        rendered_sha = file_sha(rendered)
        if rendered_sha != "d64ff08526ad4d1354a5f72eea9370b1a9d771abf7392b00aa876f9a1faa5acf":
            raise RuntimeError("AOK export did not match the retained rendered fixture")

        version_code, version_out = run_bounded([str(cli), "--version"], root, purpose="cursor-version")
        help_code, help_out = run_bounded([str(cli), "--help"], root, purpose="cursor-help")
        version = version_out.decode("utf-8", "replace").strip()
        help_text = help_out.decode("utf-8", "replace")
        if version_code != 0 or version != VERSION or help_code != 0:
            raise RuntimeError("fresh Cursor CLI version/help check failed")
        help_lower = help_text.lower()
        has_agent_list = any(label in help_lower for label in ("agent list", "agents list", "subagent list", "subagents list"))

        startup_code, startup, startup_bytes, startup_duration = read_no_input([
            str(cli), "--workspace", str(root / "export"), "--trust",
        ], root)
        if "Press any key to log in..." not in startup:
            raise RuntimeError("no-input CLI startup did not reach the expected isolated login gate")
        startup_normalized = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", startup).replace(str(root), "<TEMP_ROOT>")
        selected_lines = [line.strip() for line in startup.splitlines() if "Cursor Agent" in line or "Press any key to log in" in line]
        sanitized_startup = " | ".join(selected_lines).replace(str(root), "<TEMP_ROOT>")
        report = {
            "observedAt": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "hostVersion": version,
            "packageSHA256": PACKAGE_SHA256,
            "cliSHA256": CLI_SHA256,
            "bundleIndexSHA256": INDEX_SHA256,
            "aokSHA256": aok_sha,
            "sandbox": {
                "network": "denied",
                "keychainMachLookup": "denied",
                "realUserHomeRead": "denied",
                "writes": "private temporary root and /dev/null,/dev/tty only",
                "environment": "env -i; private HOME/TMPDIR/CURSOR_CONFIG_DIR/CURSOR_DATA_DIR",
                "processGroupPreflight": preflight_result,
            },
            "helpHasReadOnlyAgentList": has_agent_list,
            "aokExport": {
                "import": imported.get("status"),
                "export": exported.get("status"),
                "compare": compared.get("status"),
                "sourceSHA256": FIXTURE_SHA256,
                "renderedSHA256": rendered_sha,
            },
            "noInputStartup": {
                "command": "cursor-agent --workspace <AOK-exported-synthetic-project> --trust",
                "inputSent": False,
                "durationSeconds": startup_duration,
                "capturedBytes": startup_bytes,
                "exitCode": startup_code,
                "sanitizedObservation": sanitized_startup,
                "sanitizedOutputSHA256": sha(startup_normalized.encode("utf-8")),
                "agentInventoryObserved": False,
            },
            "nativeLoading": "not-run",
            "runtimeEnforcement": "unverified",
            "modelExecution": "not-run",
        }
        print(json.dumps(report, indent=2, sort_keys=True))
    shutil.rmtree(AUDIT_DIR)
    ACTIVE_ROOT = None
    AUDIT_DIR = None
    AUDIT_PATH = None


if __name__ == "__main__":
    try:
        main()
    except Exception as error:
        audit = AUDIT_PATH
        if audit and audit.is_file():
            preserved = Path(tempfile.mkdtemp(prefix="aok-cursor-failure-audit-", dir=PRIVATE_TEMP_ROOT)) / "process-audit.jsonl"
            shutil.copyfile(audit, preserved)
            print(json.dumps({"status": "failed", "errorType": type(error).__name__,
                              "processAuditPath": str(preserved)}, sort_keys=True), file=sys.stderr)
        else:
            print(json.dumps({"status": "failed", "errorType": type(error).__name__}, sort_keys=True), file=sys.stderr)
        raise SystemExit(1)
