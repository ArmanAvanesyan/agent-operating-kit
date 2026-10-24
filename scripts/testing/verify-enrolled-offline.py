#!/usr/bin/env python3
"""Prove the stock enrolled service binary stays inert offline and credential-free."""
from __future__ import annotations
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=Path)
    parser.add_argument("--deny-network", action="store_true")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    prefix: list[str] = []
    if args.deny_network:
        sandbox = Path("/usr/bin/sandbox-exec")
        if not sandbox.exists():
            raise SystemExit("network-denied smoke requires macOS sandbox-exec")
        prefix = [str(sandbox), "-p", "(version 1)(allow default)(deny network*)"]
    with tempfile.TemporaryDirectory(prefix="aok-enrolled-offline-") as temp:
        root = Path(temp).resolve()
        canary = "fixture-never-resolved-secret-canary"
        env = {"PATH": "", "HOME": str(root), "TMPDIR": str(root), "AOK_FAKE_BROKER": canary}
        def run(expected: int, *command: str) -> subprocess.CompletedProcess[str]:
            result = subprocess.run(prefix + [str(binary), *command], env=env, cwd=root,
                                    text=True, capture_output=True, timeout=10, check=False)
            assert result.returncode == expected, (command, result.returncode, result.stdout, result.stderr)
            assert canary not in result.stdout + result.stderr
            return result
        run(0, "inspect", "--help")
        run(2, "inspect", "--config", "missing", "--root", "missing")
        run(1, "inspect", "--config", "missing", "--root", "missing", "--as-of", "2026-10-24T12:00:00Z")
        for service in ("authority", "verifier", "anchor"):
            result = run(1, service, "serve", "--config", "missing", "--root", "missing")
            assert "disabled" in result.stderr
        for command in ("worker", "qualify"):
            result = run(1, command, "--config", "missing", "--root", "missing")
            assert "disabled" in result.stderr
        assert not list(root.iterdir()), "blocked services created state or output"
        bundle = root / "enrollment"
        prepared = run(0, "prepare", "--out", str(bundle))
        assert json.loads(prepared.stdout)["executionReady"] is False
        before = {str(p.relative_to(bundle)): hashlib.sha256(p.read_bytes()).hexdigest()
                  for p in bundle.rglob("*") if p.is_file()}
        checked = run(1, "preflight", "--bundle", str(bundle), "--as-of",
                      "2026-10-24T12:00:00Z", "--json")
        report = json.loads(checked.stdout)
        assert report["offlineOnly"] is True and report["status"] == "blocked"
        assert report["executionReady"] is False and report["authorizationGranted"] is False
        assert report["productionEligible"] is False
        after = {str(p.relative_to(bundle)): hashlib.sha256(p.read_bytes()).hexdigest()
                 for p in bundle.rglob("*") if p.is_file()}
        assert before == after, "preflight modified enrollment files"
        run(1, "prepare", "--out", str(bundle))
    print("Enrolled service offline smoke passed; live and production gates remain blocked.")

if __name__ == "__main__":
    main()
