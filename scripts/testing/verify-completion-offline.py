#!/usr/bin/env python3
"""Exercise completion gates and experimental pilot boundaries offline."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tempfile


AS_OF = "2026-10-24T12:30:00Z"


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=Path)
    parser.add_argument("--deny-network", action="store_true")
    args = parser.parse_args()
    root = Path(__file__).resolve().parents[2]
    binary = args.binary.resolve(strict=True)
    prefix: list[str] = []
    if args.deny_network:
        sandbox = Path("/usr/bin/sandbox-exec")
        if not sandbox.exists():
            raise SystemExit("network-denied smoke requires macOS sandbox-exec")
        prefix = [str(sandbox), "-p", "(version 1)(allow default)(deny network*)"]

    with tempfile.TemporaryDirectory(prefix="aok-completion-smoke-") as temp:
        temp_root = Path(temp).resolve()
        env = {"PATH": "", "AOK_ROOT": str(root), "HOME": str(temp_root), "TMPDIR": str(temp_root)}

        def run(expected: int, *argv: str, json_output: bool = True) -> tuple[subprocess.CompletedProcess[str], dict | None]:
            result = subprocess.run(prefix + [str(binary), *argv], cwd=root, env=env,
                                    text=True, capture_output=True, timeout=30, check=False)
            if result.returncode != expected:
                raise AssertionError((argv, result.returncode, result.stdout, result.stderr))
            if not json_output:
                return result, None
            try:
                return result, json.loads(result.stdout)
            except json.JSONDecodeError as exc:
                raise AssertionError((argv, "missing JSON report", result.stdout, result.stderr)) from exc

        gates_out = temp_root / "gates"
        _, ledger = run(1, "pilot", "gates", "--as-of", AS_OF, "--out", str(gates_out), "--json")
        assert ledger["inspectionOnly"] is True
        assert ledger["authorizationGranted"] is False and ledger["executionReady"] is False
        saved = gates_out / "completion-gates.json"
        assert saved.is_file(), "completion ledger was not written"
        saved_ledger = json.loads(saved.read_text(encoding="utf-8"))
        assert saved_ledger == ledger
        digest = ledger["digestSha256"]
        digest_input = dict(ledger)
        digest_input["digestSha256"] = ""
        canonical = json.dumps(digest_input, ensure_ascii=False, separators=(",", ":")).encode()
        assert hashlib.sha256(canonical).hexdigest() == digest, "completion ledger digest mismatch"
        by_id = {gate["id"]: gate for gate in ledger["gates"]}
        for gate_id in ("codex-host-qualification", "p3-collector-accounting", "github-issue-comment",
                        "independent-acceptance-handoff", "first-controlled-trial"):
            gate = by_id[gate_id]
            assert gate["requiredForFirstPilot"] is True and gate["status"] == "blocked"
            assert gate["accepted"] is False and gate["live"] == "blocked"
        assert by_id["jira-cloud"]["requiredForFirstPilot"] is False

        experiment = root / "internal/pilot/experiment/testdata/bundle.json"
        _, report = run(0, "pilot", "experiment", "validate", "--bundle", str(experiment),
                        "--as-of", AS_OF, "--json")
        assert {key: report[key] for key in (
            "version", "status", "structuralOnly", "authorizationGranted", "executionReady",
            "productionEligible", "liveGate",
        )} == {
            "version": "aok-experiment-bundle/v1", "status": "valid", "structuralOnly": True,
            "authorizationGranted": False, "executionReady": False, "productionEligible": False,
            "liveGate": "blocked",
        }

        execute_out = temp_root / "experiment-execution"
        result, _ = run(1, "pilot", "experiment", "execute", "--bundle", str(experiment),
                        "--once", "--out", str(execute_out), json_output=False)
        assert not execute_out.exists(), "nil-service experiment execution produced output"
        if result.stdout.strip():
            try:
                blocked = json.loads(result.stdout)
            except json.JSONDecodeError as exc:
                raise AssertionError(("unexpected experiment execution output", result.stdout, result.stderr)) from exc
            assert blocked.get("authorizationGranted") is False and blocked.get("executionReady") is False

        campaign = temp_root / "absent-campaign.json"
        authority = temp_root / "absent-authority.json"
        calibration_out = temp_root / "calibration-execution"
        result, _ = run(1, "pilot", "calibration", "execute", "--campaign", str(campaign),
                        "--authority", str(authority), "--once", "--out", str(calibration_out), json_output=False)
        assert not calibration_out.exists(), "nil-service calibration execution produced output"
        if result.stdout.strip():
            try:
                blocked = json.loads(result.stdout)
            except json.JSONDecodeError as exc:
                raise AssertionError(("unexpected calibration execution output", result.stdout, result.stderr)) from exc
            assert blocked.get("authorizationGranted") is False and blocked.get("executionReady") is False

    print("completion ledger and pilot offline smoke passed" + (" with network denied" if args.deny_network else ""))


if __name__ == "__main__":
    main()
