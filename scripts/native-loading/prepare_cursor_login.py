#!/usr/bin/env python3
"""Start official Cursor browser login into a private disposable native store.

Only the login command runs. Browser authentication may require the operator.
This never copies global configuration or prints native output/credentials.
The returned root is secret-bearing: keep it outside the repository, and remove
it after the authenticated probe finishes. A pending attempt expires after 8m.
"""
import argparse
from datetime import datetime, timedelta, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import uuid

sys.dont_write_bytecode = True
import probe_cursor_authenticated as auth
import probe_cursor_fresh_cli as safe


def write_private_json(path, value):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as stream:
        json.dump(value, stream)


def run_login(root, cli, env):
    state_path = root / "login-state.json"
    credentials = root / "home/.cursor/auth.json"
    marker_path = root / "owned-session.json"
    proc, group = None, None
    succeeded, cleanup_ok = False, False
    try:
        proc = subprocess.Popen(auth.guard(root) + [str(cli), "login"], env=env, cwd=root,
                                stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                start_new_session=True)
        group = safe.audit_spawned_child(root, proc, "isolated-browser-login")
        write_private_json(state_path, {"status": "awaiting-browser-authentication", "pid": proc.pid,
                                       "pgid": group, "nativeOutputRetained": False})
        print(json.dumps({"status": "awaiting-browser-authentication", "privateRoot": str(root)}), flush=True)
        try:
            out, err = proc.communicate(timeout=480)
            safe.require_sha(cli, safe.CLI_SHA256, "Cursor CLI after login")
            succeeded = proc.returncode == 0 and credentials.is_file()
            write_private_json(state_path, {"status": "login-complete" if succeeded else "login-not-complete",
                                            "exitCode": proc.returncode, "nativeOutputRetained": False,
                                            "stdoutBytes": len(out), "stderrBytes": len(err)})
        except subprocess.TimeoutExpired:
            write_private_json(state_path, {"status": "login-expired", "nativeOutputRetained": False})
    finally:
        try:
            if group is not None:
                safe.terminate_owned_group(proc, group, "isolated-browser-login")
                cleanup_ok = True
        finally:
            if proc:
                proc.stdout.close()
                proc.stderr.close()
            safe.AUDIT_PATH = None
            if not succeeded or not cleanup_ok:
                credentials.unlink(missing_ok=True)
                if credentials.exists():
                    raise RuntimeError("failed login credential cleanup is unconfirmed")
                write_private_json(marker_path, {"kind": "aok-cursor-native-login/v1", "valid": False})
            else:
                marker = json.loads(marker_path.read_text())
                marker["valid"] = True
                created = datetime.fromisoformat(marker["createdAt"])
                marker["expiresAt"] = min(datetime.now(timezone.utc) + timedelta(minutes=8), created + timedelta(minutes=16)).isoformat()
                write_private_json(marker_path, marker)
    print(json.dumps({"status": "login-complete" if succeeded else "login-not-complete",
                      "privateRoot": str(root)}), flush=True)
    return succeeded


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--package", type=Path, required=True)
    args = parser.parse_args()
    safe.require_sha(args.package, safe.PACKAGE_SHA256, "Cursor official archive")
    root = Path(tempfile.mkdtemp(prefix="aok-cursor-login-", dir="/private/tmp")).resolve()
    root.chmod(0o700)
    now = datetime.now(timezone.utc)
    marker = {"kind": "aok-cursor-native-login/v1", "id": str(uuid.uuid4()), "valid": False,
              "createdAt": now.isoformat(), "expiresAt": (now + timedelta(minutes=8)).isoformat()}
    write_private_json(root / "owned-session.json", marker)
    audit = Path(tempfile.mkdtemp(prefix="aok-cursor-login-audit-", dir="/private/tmp"))
    audit.chmod(0o700)
    safe.AUDIT_PATH = audit / "process-audit.jsonl"
    source = Path(__file__).read_bytes()
    (audit / "executed-prepare.py").write_bytes(source)
    (audit / "prepare-sha256.txt").write_text(hashlib.sha256(source).hexdigest())
    package = root / "package.tar.gz"
    shutil.copyfile(args.package, package)
    safe.require_sha(package, safe.PACKAGE_SHA256, "copied Cursor archive")
    safe.safe_extract(package, root / "host")
    cli = root / "host/dist-package/cursor-agent-sea"
    safe.require_sha(cli, safe.CLI_SHA256, "copied Cursor CLI")
    env = auth.environment(root)
    env.pop("NO_OPEN_BROWSER", None)
    auth.preflight(root, env)
    write_private_json(root / "login-state.json", {"status": "browser-login-starting", "nativeOutputRetained": False})
    run_login(root, cli, env)


if __name__ == "__main__":
    main()
