#!/usr/bin/env python3
"""Bounded, no-input Gemini CLI project-agent discovery check for macOS."""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import pty
import re
import select
import shutil
import signal
import struct
import subprocess
import tempfile
import termios
import time
import fcntl
from pathlib import Path


def kill_group(process: subprocess.Popen[bytes]) -> None:
    try:
        os.killpg(process.pid, signal.SIGTERM)
    except ProcessLookupError:
        return
    try:
        process.wait(timeout=1)
    except subprocess.TimeoutExpired:
        pass
    try:
        os.killpg(process.pid, signal.SIGKILL)
    except ProcessLookupError:
        pass
    try:
        process.wait(timeout=2)
    except subprocess.TimeoutExpired:
        try:
            os.kill(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        process.wait(timeout=2)


def run_bounded(args: list[str], env: dict[str, str], timeout: int) -> str:
    proc = subprocess.Popen(
        args, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
        start_new_session=True,
    )
    try:
        output, _ = proc.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        kill_group(proc)
        raise RuntimeError(f"bounded command timed out after {timeout}s: {args[-1]}")
    if proc.returncode:
        raise RuntimeError(
            f"command exited {proc.returncode}: {output[-3000:].decode(errors='replace')}"
        )
    return output.decode(errors="replace")


def run_tui(command: list[str], env: dict[str, str], cwd: Path, root: Path, seconds: int) -> tuple[str, int | None]:
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 38, 120, 0, 0))
    proc = subprocess.Popen(
        command, env=env, stdin=slave, stdout=slave, stderr=slave,
        cwd=cwd, start_new_session=True, close_fds=True,
    )
    os.close(slave)
    output = bytearray()
    deadline = time.monotonic() + seconds
    try:
        while time.monotonic() < deadline and len(output) < 1_000_000:
            ready, _, _ = select.select([master], [], [], min(0.25, max(0, deadline - time.monotonic())))
            if ready:
                try:
                    chunk = os.read(master, min(65536, 1_000_000 - len(output)))
                except OSError:
                    break
                if not chunk:
                    break
                output.extend(chunk)
            if proc.poll() is not None and not ready:
                break
    finally:
        kill_group(proc)
        os.close(master)
    text = output.decode(errors="replace")
    text = re.sub(r"\x1b\[[0-?]*[ -/]*[@-~]", "", text)
    text = re.sub(r"\x1b\][^\x07]*(?:\x07|\x1b\\)", "", text)
    text = text.replace(str(root), "<TEMP_ROOT>").replace(str(root.resolve()), "<TEMP_ROOT>")
    text = text.replace(env["HOME"], "<HOME>").replace(env["TMPDIR"], "<TEMP_ROOT>")
    text = re.sub(r"\r\n?", "\n", text)
    return text, proc.returncode


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("fixture", type=Path, help="one AOK-exported Gemini agent Markdown file")
    parser.add_argument("--name", required=True, help="expected native agent name")
    parser.add_argument("--description", required=True, help="expected native agent description")
    parser.add_argument("--timeout", type=int, default=8, help="seconds per no-input TUI launch (max 15)")
    ns = parser.parse_args()
    if not ns.fixture.is_file() or ns.timeout < 1 or ns.timeout > 15:
        parser.error("fixture must exist and timeout must be between 1 and 15")
    if not shutil.which("npm") or not Path("/usr/bin/sandbox-exec").is_file():
        raise RuntimeError("npm and macOS /usr/bin/sandbox-exec are required")

    root = Path(tempfile.mkdtemp(prefix="aok-gemini-discovery-"))
    resolved = root.resolve()
    result: dict[str, object] = {"hostVersion": "@google/gemini-cli@0.61.0", "timeoutSeconds": ns.timeout}
    try:
        paths = {name: root / name for name in (
            "npm-prefix", "npm-global-prefix", "npm-cache", "home-positive",
            "home-empty", "project-positive", "project-empty",
        )}
        for path in paths.values():
            path.mkdir(parents=True, exist_ok=True)
        userconfig = root / "npm-userconfig"
        globalconfig = root / "npm-globalconfig"
        userconfig.write_text("registry=https://registry.npmjs.org/\n", encoding="utf-8")
        globalconfig.write_text("", encoding="utf-8")
        profile = (
            f'(version 1)(allow default)(deny file-write*)'
            f'(allow file-write* (subpath "{resolved}") (literal "/dev/null") (literal "/dev/tty"))'
        )
        sandbox = ["/usr/bin/sandbox-exec", "-p", profile]
        path_env = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

        install_env = {
            "PATH": path_env, "HOME": str(paths["home-positive"]),
            "TMPDIR": str(root), "TMP": str(root), "TEMP": str(root),
            "NPM_CONFIG_PREFIX": str(paths["npm-global-prefix"]),
            "npm_config_prefix": str(paths["npm-global-prefix"]),
            "NPM_CONFIG_CACHE": str(paths["npm-cache"]),
            "npm_config_cache": str(paths["npm-cache"]),
            "NPM_CONFIG_USERCONFIG": str(userconfig),
            "NPM_CONFIG_GLOBALCONFIG": str(globalconfig),
            "NPM_CONFIG_REGISTRY": "https://registry.npmjs.org/",
            "npm_config_registry": "https://registry.npmjs.org/",
            "npm_config_fetch_timeout": "30000", "npm_config_fetch_retries": "0",
        }
        install_log = run_bounded(
            sandbox + ["/usr/bin/env", "-i"]
            + [f"{key}={value}" for key, value in install_env.items()]
            + [shutil.which("npm") or "npm", "install", "--prefix", str(paths["npm-prefix"]),
               "--ignore-scripts", "--no-audit", "--no-fund", "@google/gemini-cli@0.61.0"],
            install_env, 120,
        )
        gemini = paths["npm-prefix"] / "node_modules/.bin/gemini"
        if not gemini.is_file():
            raise RuntimeError("pinned Gemini package installed without its binary")

        fixture_data = ns.fixture.read_bytes()
        (paths["project-positive"] / ".gemini/agents").mkdir(parents=True)
        (paths["project-positive"] / ".gemini/agents/repository-explorer.md").write_bytes(fixture_data)
        fixture_hash = hashlib.sha256(fixture_data).hexdigest()
        for key in ("home-positive", "home-empty"):
            home = paths[key]
            (home / ".gemini").mkdir()
            (home / ".gemini/settings.json").write_text(json.dumps({
                "general": {"enableAutoUpdate": False, "enableAutoUpdateNotification": False},
                "experimental": {"enableAgents": True},
                "privacy": {"usageStatisticsEnabled": False},
            }), encoding="utf-8")

        def tui(project: Path, home: Path) -> tuple[str, int | None]:
            env = {
                "PATH": path_env, "HOME": str(home), "TMPDIR": str(root),
                "TMP": str(root), "TEMP": str(root),
                "XDG_CONFIG_HOME": str(home / ".config"),
                "XDG_DATA_HOME": str(home / ".local/share"),
                "GEMINI_CLI_TRUST_WORKSPACE": "true",
                "NPM_CONFIG_PREFIX": str(paths["npm-global-prefix"]),
                "npm_config_prefix": str(paths["npm-global-prefix"]),
                "NPM_CONFIG_CACHE": str(paths["npm-cache"]),
                "npm_config_cache": str(paths["npm-cache"]),
                "NPM_CONFIG_USERCONFIG": str(userconfig),
                "NPM_CONFIG_GLOBALCONFIG": str(globalconfig),
            }
            command = sandbox + ["/usr/bin/env", "-i"] + [f"{k}={v}" for k, v in env.items()] + [
                "/usr/bin/perl", "-e", f"alarm {ns.timeout}; exec @ARGV", str(gemini), "--debug"
            ]
            return run_tui(command, env, project, root, ns.timeout)

        positive, positive_exit = tui(paths["project-positive"], paths["home-positive"])
        empty, empty_exit = tui(paths["project-empty"], paths["home-empty"])
        panel = "New Agents Discovered" in positive
        identity = ns.name in positive and ns.description in positive
        negative_identity = ns.name not in empty and ns.description not in empty
        result.update({
            "fixtureSHA256": fixture_hash,
            "positive": {"exitCode": positive_exit, "discoveryPanel": panel,
                         "expectedIdentity": identity, "transcript": positive[-6000:]},
            "emptyControl": {"exitCode": empty_exit, "customIdentityAbsent": negative_identity,
                             "transcript": empty[-6000:]},
            "modelInvocation": "not-run", "authenticationSelected": False,
            "inputSent": False, "npmInstallBoundSeconds": 120,
            "writeSandbox": "all host child writes denied except under temporary root and /dev/null,/dev/tty",
            "npmInstallOutputTail": install_log[-1500:],
        })
        result["verified"] = bool(panel and identity and negative_identity)
        print(json.dumps(result, indent=2), flush=True)
        return 0 if result["verified"] else 1
    finally:
        shutil.rmtree(root, ignore_errors=True)


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(json.dumps({"verified": False, "error": str(exc)}, indent=2))
        raise SystemExit(1)
