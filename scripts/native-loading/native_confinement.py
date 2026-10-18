"""Darwin-only file-write boundary for native host verification processes.

This guards filesystem writes, not network traffic or model execution. Callers
must separately use synthetic inputs, remove credentials and avoid inference.
"""
import json
import os
from pathlib import Path
import signal
import subprocess
import sys


def run_confined(command, *, root, env, cwd=None, timeout=30):
    if not isinstance(env, dict):
        raise ValueError("Native probe environment must be an explicit allow-list")
    root = Path(root).resolve(strict=True)
    cwd = root if cwd is None else Path(cwd).resolve(strict=True)
    if cwd != root and root not in cwd.parents:
        raise ValueError("Native probe cwd must stay within its owned root")
    sandbox = Path("/usr/bin/sandbox-exec")
    if sys.platform != "darwin" or not sandbox.is_file():
        raise RuntimeError("This native probe requires Darwin sandbox-exec write confinement")
    profile = ('(version 1)(allow default)(deny file-write*)'
               '(allow file-write* (subpath ' + json.dumps(str(root)) + ')'
               ' (literal "/dev/null") (literal "/dev/tty"))')
    proc = subprocess.Popen([str(sandbox), "-p", profile, *map(str, command)],
                            cwd=cwd, env=env, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            text=True, start_new_session=True)
    try:
        stdout, stderr = proc.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        _signal_group(proc.pid, signal.SIGTERM)
        try:
            proc.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            _signal_group(proc.pid, signal.SIGKILL)
            try:
                proc.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                # A detached descendant may retain the pipes. Do not let pipe
                # cleanup turn a bounded probe into an unbounded wait.
                proc.stdout.close()
                proc.stderr.close()
                proc.wait(timeout=2)
        raise
    return subprocess.CompletedProcess(command, proc.returncode, stdout, stderr)


def _signal_group(pid, sig):
    try:
        os.killpg(pid, sig)
    except ProcessLookupError:
        pass  # The process group finished between timeout and termination.
