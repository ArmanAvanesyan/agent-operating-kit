#!/usr/bin/env python3
"""Exercise Phase 9 Go commands without credentials or executable discovery."""
from __future__ import annotations
import argparse
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
    root = Path(__file__).resolve().parents[2]
    binary = args.binary.resolve(strict=True)
    prefix = []
    if args.deny_network:
        sandbox = Path("/usr/bin/sandbox-exec")
        if not sandbox.exists():
            raise SystemExit("network-denied smoke requires macOS sandbox-exec")
        prefix = [str(sandbox), "-p", "(version 1)(allow default)(deny network*)"]
    with tempfile.TemporaryDirectory(prefix="aok-p9-smoke-") as temp:
        temp_root = Path(temp).resolve()
        env = {"PATH": "", "AOK_ROOT": str(root), "HOME": str(temp_root), "TMPDIR": str(temp_root)}
        def run(expected: int, *argv: str) -> dict:
            result = subprocess.run(prefix + [str(binary), *argv], cwd=root, env=env,
                                    text=True, capture_output=True, timeout=30, check=False)
            if result.returncode != expected:
                raise AssertionError((argv, result.returncode, result.stdout, result.stderr))
            try:
                return json.loads(result.stdout)
            except json.JSONDecodeError as exc:
                raise AssertionError((argv, "missing JSON report", result.stdout, result.stderr)) from exc
        ready = root / "internal/readiness/testdata"
        pilot = root / "internal/pilot/testdata"
        report = run(1, "readiness", "report", "--manifest", str(ready / "route.json"),
                     "--policy", str(ready / "policy.json"), "--as-of", "2026-10-24T12:00:00Z",
                     "--out", str(temp_root / "ready"), "--json")
        assert report["authorizationGranted"] is False and report["executionReady"] is False
        run(0, "pilot", "validate", "--manifest", str(pilot / "manifest.json"), "--policy",
            str(pilot / "policy.json"), "--as-of", "2026-10-24T12:00:00Z", "--json")
        run(0, "pilot", "replay", "--scenario", str(pilot / "scenario.json"), "--as-of",
            "2026-10-24T12:00:00Z", "--out", str(temp_root / "replay"), "--json")
        report = run(1, "pilot", "assess", "--campaign", str(pilot), "--policy",
                     str(pilot / "policy.json"), "--as-of", "2026-10-24T12:00:00Z",
                     "--out", str(temp_root / "assessment"), "--json")
        assert report["executionReady"] is False and report["productionEligible"] is False
        run(1, "pilot", "execute", "--campaign", str(pilot), "--authority", "absent",
            "--once", "--out", str(temp_root / "no-launch"), "--json")
        assert not (temp_root / "no-launch").exists()
    print("Phase 9 empty-PATH/no-credentials offline smoke passed" + (" with network denied" if args.deny_network else ""))


if __name__ == "__main__":
    main()
