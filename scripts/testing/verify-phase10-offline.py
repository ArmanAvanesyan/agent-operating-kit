#!/usr/bin/env python3
"""Exercise P10 offline commands with no credentials or executable discovery."""
from __future__ import annotations
import argparse
import hashlib
import json
import shutil
import subprocess
import tempfile
from pathlib import Path


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=Path)
    parser.add_argument("--deny-network", action="store_true")
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    repo = Path(__file__).resolve().parents[2]
    prefix = []
    if args.deny_network:
        prefix = ["/usr/bin/sandbox-exec", "-p", "(version 1)(allow default)(deny network*)"]
    with tempfile.TemporaryDirectory(prefix="aok-p10-smoke-") as temp:
        root = Path(temp).resolve()
        env = {"PATH": "", "HOME": str(root), "AOK_ROOT": str(repo), "TMPDIR": str(root)}
        def run(expected: int, *argv: str):
            result = subprocess.run(prefix + [str(binary), *argv], cwd=repo, env=env,
                                    text=True, capture_output=True, timeout=45, check=False)
            if result.returncode != expected:
                raise AssertionError((argv, result.returncode, result.stdout, result.stderr))
            return json.loads(result.stdout) if result.stdout.strip() else {}
        def write(name, value):
            path = root / name
            path.write_text(json.dumps(value), encoding="utf-8")
            return str(path)
        rows = run(0, "host", "list", "--json")
        assert len(rows) == 9
        assert all(r["authorizationGranted"] is False and r["productionActivation"] == "disabled" for r in rows)
        run(0, "methodology", "list", "--json")
        run(0, "work-item", "list", "--json")
        policy = write("driver.json", {"version": "aok-host-driver-input/v1", "policy": {
            "host": "opencode", "hostVersion": "fixture", "workspace": str(root),
            "model": "explicit-fixture", "executable": "/fixture/host", "executableSha256": "a"*64,
            "authMode": "native-login", "accountScope": "fixture-account", "profileSha256": "b"*64,
            "configurationSha256": "c"*64, "maxSeconds": 30, "maxBytes": 4096, "allowDelegation": False}})
        report = run(0, "host", "inspect", "--host", "opencode", "--policy", policy,
                     "--as-of", "2026-10-24T12:00:00Z", "--out", str(root / "inspect"), "--json")
        assert report["installedBinary"] == "unknown" and report["executionReady"] is False
        report = run(1, "host", "qualify", "--host", "opencode", "--policy", policy,
                     "--as-of", "2026-10-24T12:00:00Z", "--out", str(root / "qualify"), "--json")
        assert report["liveRouteQualification"] == "blocked"
        scope = {"provider": "github", "origin": "https://github.com", "tenantId": "fixture-org",
                 "projectId": "fixture-project", "processId": "", "processType": ""}
        observed = write("observation.json", {"version": "aok-work-system-plan/v1", "scope": scope,
            "observedAt": "2026-10-24T12:00:00Z", "revision": "fixture", "complete": True, "resources": []})
        desired = write("desired.json", {"version": "aok-work-system-plan/v1", "scope": scope,
            "resources": [{"id": "", "kind": "project-field", "name": "AOK", "parentId": "", "value": {"type": "TEXT"}}]})
        report = run(0, "work-system", "plan", "--observation", observed, "--desired", desired,
                     "--as-of", "2026-10-24T12:00:00Z", "--out", str(root / "plan"), "--json")
        assert report["status"] == "draft" and report["authorizationGranted"] is False
        plan = str(root / "plan/plan.json")
        run(0, "work-system", "validate-plan", "--plan", plan, "--json")
        report = run(1, "work-system", "apply", "--plan", plan, "--change", "0",
                     "--out", str(root / "apply"), "--json")
        assert report["status"] == "blocked" and report["executionReady"] is False
        # Campaign captures and validates a real closed planning corpus.
        shutil.copytree(repo / "internal/planning/testdata/corpus", root / "corpus")
        selection_policy = {"version": "aok-selection/v1", "defaultsVersion": "aok-selection-defaults/v1",
            "routes": [], "priority": "quality", "standardFloor": .80, "highRiskFloor": .95,
            "fallback": [], "pin": "", "waiveHysteresis": False}
        baseline = Path(write("base-policy.json", selection_policy))
        plan_bytes = (root / "corpus/plan.json").read_bytes()
        spec = write("campaign.json", {"version": "aok-campaign-proposal/v1", "id": "fixture",
            "frozenAt": "2026-10-24T11:00:00Z", "nodes": [{"id": "task", "requirements": ["explicit"],
                "dependsOn": [], "plan": {"path": "corpus/plan.json", "sha256": hashlib.sha256(plan_bytes).hexdigest()}, "corpus": "corpus"}],
            "unresolved": [], "basePolicy": {"path": "base-policy.json", "sha256": hashlib.sha256(baseline.read_bytes()).hexdigest()},
            "allowedPriorities": ["quality", "cost"], "evidencePolicySha256": "a" * 64, "cohorts": []})
        report = run(0, "campaign", "plan", "--spec", spec, "--as-of", "2026-10-24T12:00:00Z",
                     "--out", str(root / "campaign"), "--json")
        assert report["status"] == "advisory" and report["executionReady"] is False
        # Existing output is immutable.
        run(1, "campaign", "plan", "--spec", spec, "--as-of", "2026-10-24T12:00:00Z",
            "--out", str(root / "campaign"), "--json")
    print("Phase 10 empty-PATH/no-credentials offline smoke passed" + (" with network denied" if args.deny_network else ""))


if __name__ == "__main__":
    main()
