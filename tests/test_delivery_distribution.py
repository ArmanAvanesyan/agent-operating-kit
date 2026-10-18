"""Optional installed-artifact smoke test, reusable outside either source checkout.

AOK_DELIVERY_ARTIFACT points at an extracted kit with aok binary in bin/aok.
All mutations and checkpoint bytes stay in disposable repositories.
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


@unittest.skipUnless(os.environ.get("AOK_DELIVERY_ARTIFACT"), "installed artifact not selected")
class DeliveryArtifactTests(unittest.TestCase):
    def test_installed_flow_and_stale_evidence(self):
        artifact = Path(os.environ["AOK_DELIVERY_ARTIFACT"]).resolve()
        binary = artifact / "bin/aok"
        payload = artifact / "packs/repository-delivery"
        self.assertTrue((artifact / "skills/repository-delivery/SKILL.md").is_file())
        self.assertEqual(len(list((payload / "skills").glob("*/SKILL.md"))), 8)
        self.assertEqual(len(list((payload / "config/schemas").glob("*.json"))), 25)
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory)
            home = work / "home"
            installed = home / ".local/share/agent-operating-kit/agent-operating-kit"
            installed.parent.mkdir(parents=True)
            shutil.copytree(artifact, installed)
            binary = installed / "bin/aok"
            env = dict(os.environ, HOME=str(home), AOK_DELIVERY_PYTHON=sys.executable)
            env.pop("AOK_ROOT", None)
            env.pop("PYTHONPATH", None)
            def run(*args, success=True):
                p = subprocess.run([str(binary), "delivery", *map(str, args)], cwd=work,
                                   env=env, capture_output=True, text=True, timeout=30)
                if success:
                    self.assertEqual(p.returncode, 0, p.stderr)
                    return json.loads(p.stdout)
                self.assertNotEqual(p.returncode, 0)
                return p
            repo = work / "repo"
            repo.mkdir()
            def git(*args):
                return subprocess.check_output(["git", "-C", str(repo), *args], stderr=subprocess.PIPE)
            git("init", "-b", "main")
            git("config", "user.name", "Fixture")
            git("config", "user.email", "fixture@example.invalid")
            (repo / "a").write_text("initial")
            git("add", "a")
            git("commit", "-m", "initial")
            (repo / "a").write_bytes(b"staged\x00")
            git("add", "a")
            (repo / "a").write_bytes(b"working\xff")
            (repo / "untracked").write_text("retain")
            before = git("status", "--porcelain=v1", "-z")
            run("inspect", "--repo", repo)
            cp = work / "checkpoint"
            run("checkpoint", "create", "--repo", repo, "--output", cp)
            run("checkpoint", "verify", cp)
            plan = run("port", "plan", "--repo", repo, "--checkpoint", cp,
                       "--baseline", "main", "--destination", work / "port",
                       "--branch", "feat/port", "--select", "a")
            planfile = work / "plan.json"
            planfile.write_text(json.dumps(plan))
            run("port", "apply", "--plan", planfile, "--authorize", "port")
            self.assertEqual((work / "port/a").read_bytes(), b"working\xff")
            self.assertFalse((work / "port/untracked").exists())
            spec = {"objective": "Fixture", "files_in_scope": ["a", "untracked"],
                    "gate_commands": [[sys.executable, "-c", "print('gate')"]],
                    "review_policy": "independent", "authorized_actions": [],
                    "actor_id": "implementer", "actor_context": "implementation"}
            specfile = work / "spec.json"
            specfile.write_text(json.dumps(spec))
            ctx = run("delivery", "init", "--repo", repo, "--baseline", "HEAD", "--spec", specfile)
            ctxfile = work / "context.json"
            ctxfile.write_text(json.dumps(ctx))
            gates = run("gates", "run", "--context", ctxfile)
            gatesfile = work / "gates.json"
            gatesfile.write_text(json.dumps(gates))
            review = {"reviewer": "independent", "reviewer_context": "separate",
                      "snapshot_sha256": gates["snapshot_sha256"],
                      "outcome": "approved", "report": "fixture review"}
            reviewfile = work / "review-input.json"
            reviewfile.write_text(json.dumps(review))
            receipt = run("review", "record", "--context", ctxfile, "--input", reviewfile)
            reviewfile = work / "review.json"
            reviewfile.write_text(json.dumps(receipt))
            run("delivery", "status", "--context", ctxfile, "--gates", gatesfile, "--review", reviewfile)
            self.assertEqual(before, git("status", "--porcelain=v1", "-z"))
            (repo / "a").write_text("changed after evidence")
            stale = run("delivery", "status", "--context", ctxfile, "--gates", gatesfile, "--review", reviewfile, success=False)
            self.assertFalse(json.loads(stale.stdout)["ready"])


class ShippingPolicyTests(unittest.TestCase):
    def test_compatibility_entrypoints_preserve_authority_and_exact_scope(self):
        root = Path(__file__).resolve().parents[1]
        paths = ["skills/ship-pr.md", "skills/fix-ci.md", "agents/pr-ci-fixer.md"]
        for rel in paths:
            text = (root / "packs/ship-pipeline/claude-code" / rel).read_text()
            self.assertNotIn("git add -A", text)
            self.assertNotIn("--delete-branch", text)
            self.assertIn("author", text.lower())
            self.assertIn("preserv", text.lower())
