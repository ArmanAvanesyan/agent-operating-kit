import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

from repo_delivery.core import inspect, compare, create_checkpoint, verify_checkpoint, DeliveryError

class RepositoryFixture(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / "repo"
        self.repo.mkdir()
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        (self.repo / "a").write_text("initial\n")
        self.git("add", ".")
        self.git("commit", "-m", "initial")

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.repo, stderr=subprocess.PIPE)

    def dirty(self):
        (self.repo / "a").write_bytes(b"staged\x00")
        self.git("add", "a")
        (self.repo / "a").write_bytes(b"working\x00")
        (self.repo / "new\nname").write_text("new")
        (self.repo / ".gitignore").write_text("private\n")
        (self.repo / "private").write_text("secret")
        (self.repo / "link").symlink_to("/outside/missing")


class RepositoryTests(RepositoryFixture):
    def test_checkpoint_roundtrip_and_source_unchanged(self):
        self.dirty()
        before = self.git("status", "--porcelain=v1", "-z", "--untracked-files=all")
        index = (self.repo / ".git/index").read_bytes()
        out = self.root / "checkpoint"
        receipt = create_checkpoint(self.repo, out, ["private"])
        self.assertEqual(verify_checkpoint(out), receipt)
        self.assertEqual(before, self.git("status", "--porcelain=v1", "-z", "--untracked-files=all"))
        self.assertEqual(index, (self.repo / ".git/index").read_bytes())
        manifest = json.loads((out / "manifest.json").read_text())
        self.assertIn("private", [x["path"] for x in manifest["files"]])
        self.assertEqual(os.readlink(out / "files/link"), "/outside/missing")
        self.assertTrue(list((out / "objects").iterdir()))
        self.assertTrue((out / "repository.bundle").exists())
        self.assertEqual(out.stat().st_mode & 0o777, 0o700)

    def test_ignored_opt_in_and_collision(self):
        self.dirty()
        out = self.root / "checkpoint"
        create_checkpoint(self.repo, out)
        self.assertFalse((out / "files/private").exists())
        with self.assertRaises(DeliveryError):
            create_checkpoint(self.repo, out)
        with self.assertRaises(DeliveryError):
            create_checkpoint(self.repo, self.repo / "bad")
        with self.assertRaises(DeliveryError):
            create_checkpoint(self.repo, self.root / "bad", ["../escape"])

    def test_tampering(self):
        out = self.root / "checkpoint"
        create_checkpoint(self.repo, out)
        (out / "files/a").write_text("tampered")
        with self.assertRaises(DeliveryError):
            verify_checkpoint(out)

    def test_compare_pins_baseline_and_deletions(self):
        (self.repo / "a").unlink()
        (self.repo / "extra").write_text("extra")
        result = compare(self.repo, "main")
        self.assertEqual(result["baseline_revision"], self.git("rev-parse", "main").decode().strip())
        self.assertEqual({x["path"]: x["state"] for x in result["files"]}, {"a": "deleted", "extra": "local-only"})

    def test_inventory_and_invalid_repository(self):
        result = inspect(self.repo)
        self.assertEqual(result["branch"], "main")
        self.assertIn("refs/heads/main", result["refs"])
        with self.assertRaises(DeliveryError):
            inspect(self.root)

    def test_unborn(self):
        self.repo = self.root / "empty"
        self.repo.mkdir()
        self.git("init", "-b", "main")
        (self.repo / "new").write_text("new")
        out = self.root / "checkpoint"
        create_checkpoint(self.repo, out)
        verify_checkpoint(out)
        self.assertIsNone(inspect(self.repo)["head"])

if __name__ == "__main__":
    unittest.main()

class RecoveryTests(RepositoryFixture):
    def test_recover_index_worktree_and_unmerged_branch(self):
        self.git("branch", "retained")
        self.git("checkout", "retained")
        (self.repo / "branch-only").write_text("unmerged")
        self.git("add", ".")
        self.git("commit", "-m", "unmerged")
        retained = self.git("rev-parse", "HEAD")
        self.git("checkout", "main")
        self.dirty()
        out = self.root / "checkpoint"
        create_checkpoint(self.repo, out, ["private"])
        original_status = self.git("status", "--porcelain=v1", "-z", "--untracked-files=all")
        recovered = self.root / "recovered"
        subprocess.run(["git", "clone", str(out / "repository.bundle"), str(recovered)], check=True, capture_output=True)
        subprocess.run(["git", "-C", str(recovered), "checkout", "main"], check=True, capture_output=True)
        for blob in (out / "objects").iterdir():
            proc = subprocess.run(["git", "-C", str(recovered), "hash-object", "-w", "--stdin"], input=blob.read_bytes(), capture_output=True, check=True)
            self.assertEqual(proc.stdout.strip().decode(), blob.name)
        import shutil
        shutil.copyfile(out / "index", recovered / ".git/index")
        manifest = json.loads((out / "manifest.json").read_text())
        for record in manifest["files"]:
            dest = recovered / record["path"]
            if dest.exists() or dest.is_symlink():
                dest.unlink()
            dest.parent.mkdir(parents=True, exist_ok=True)
            if record["type"] == "symlink":
                dest.symlink_to(record["target"])
            elif record["type"] == "file":
                shutil.copyfile(out / "files" / record["path"], dest)
                dest.chmod(record["mode"])
        status = subprocess.check_output(["git", "-C", str(recovered), "status", "--porcelain=v1", "-z", "--untracked-files=all"])
        self.assertEqual(status, original_status)
        self.assertEqual(subprocess.check_output(["git", "-C", str(recovered), "rev-parse", "refs/remotes/origin/retained"]), retained)

    def test_detached_and_executable_comparison(self):
        self.git("checkout", "--detach")
        self.assertIsNone(inspect(self.repo)["branch"])
        (self.repo / "a").chmod(0o755)
        self.assertEqual(compare(self.repo, "HEAD")["files"][0]["state"], "modified")
        create_checkpoint(self.repo, self.root / "detached")

    def test_split_index_rejected(self):
        self.git("update-index", "--split-index")
        with self.assertRaisesRegex(DeliveryError, "Split"):
            create_checkpoint(self.repo, self.root / "split")

    def test_embedded_repository_rejected(self):
        inner = self.repo / "nested"
        inner.mkdir()
        subprocess.run(["git", "init", str(inner)], capture_output=True, check=True)
        (inner / "content").write_text("nested")
        with self.assertRaises(DeliveryError):
            create_checkpoint(self.repo, self.root / "embedded")

    def test_verify_rejects_extra_and_metadata_symlink(self):
        out = self.root / "checkpoint"
        create_checkpoint(self.repo, out)
        (out / "extra").write_text("extra")
        with self.assertRaises(DeliveryError):
            verify_checkpoint(out)
        (out / "extra").unlink()
        (out / "receipt.json").unlink()
        (out / "receipt.json").symlink_to("/missing")
        with self.assertRaises(DeliveryError):
            verify_checkpoint(out)

    def test_tracked_embedded_repository_rejected(self):
        inner = self.repo / "nested"
        inner.mkdir()
        (inner / "content").write_text("nested")
        self.git("add", ".")
        self.git("commit", "-m", "tracked directory")
        subprocess.run(["git", "init", str(inner)], capture_output=True, check=True)
        with self.assertRaisesRegex(DeliveryError, "Embedded"):
            create_checkpoint(self.repo, self.root / "embedded-tracked")

    def test_self_consistent_incomplete_manifest_rejected(self):
        from repo_delivery.core import canonical, digest
        out = self.root / "checkpoint"
        create_checkpoint(self.repo, out)
        manifest = json.loads((out / "manifest.json").read_text())
        del manifest["artifacts"]["index"]
        (out / "index").unlink()
        (out / "manifest.json").write_bytes(canonical(manifest))
        receipt = {"schema_version": 1, "kind": "preservation-receipt", "manifest_sha256": digest(canonical(manifest))}
        (out / "receipt.json").write_bytes(canonical(receipt))
        with self.assertRaises(DeliveryError):
            verify_checkpoint(out)
