from __future__ import annotations

import json
import importlib.util
from importlib.machinery import SourceFileLoader
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
AOK = ROOT / "scripts" / "aok"


class MethodologyTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        loader = SourceFileLoader("aok_methodology_test", str(AOK))
        spec = importlib.util.spec_from_loader(loader.name, loader)
        assert spec is not None
        module = importlib.util.module_from_spec(spec)
        loader.exec_module(module)
        cls.aok_module = module

    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.project = Path(self.temp.name) / "project"
        self.project.mkdir()

    def tearDown(self) -> None:
        self.temp.cleanup()

    def run_aok(self, *args: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            [sys.executable, str(AOK), *args],
            text=True,
            capture_output=True,
        )

    def manifest(self) -> dict:
        return json.loads((self.project / ".aok" / "project.json").read_text())

    def policy(self) -> str:
        return (self.project / "agents" / "methodology.md").read_text()

    def test_fresh_init_defaults_to_explicit_kanban(self) -> None:
        result = self.run_aok("project", "init", str(self.project))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(
            self.manifest()["methodology"],
            {"flow": "kanban", "overlays": []},
        )
        self.assertIn("## Flow: Kanban", self.policy())
        self.assertNotIn("## Overlay:", self.policy())

    def test_init_composes_overlays_in_canonical_order_and_is_idempotent(self) -> None:
        first = self.run_aok(
            "project",
            "init",
            str(self.project),
            "--flow",
            "kanban",
            "--overlay",
            "lean",
            "--overlay",
            "xp",
        )
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertEqual(
            self.manifest()["methodology"],
            {"flow": "kanban", "overlays": ["xp", "lean"]},
        )
        first_policy = self.policy()
        self.assertLess(
            first_policy.index("## Overlay: Extreme Programming"),
            first_policy.index("## Overlay: Lean"),
        )

        second = self.run_aok("project", "init", str(self.project))
        self.assertEqual(second.returncode, 0, second.stderr)
        self.assertEqual(self.policy(), first_policy)
        self.assertEqual(
            self.manifest()["methodology"],
            {"flow": "kanban", "overlays": ["xp", "lean"]},
        )

    def test_invalid_and_nonactive_selections_do_not_mutate_project(self) -> None:
        cases = [
            ("--flow", "unknown"),
            ("--flow", "fdd"),
            ("--flow", "kanban", "--overlay", "am"),
            ("--flow", "kanban", "--overlay", "xp", "--overlay", "xp"),
            ("--overlay", "xp"),
        ]
        for case in cases:
            with self.subTest(case=case):
                target = Path(self.temp.name) / ("case-" + str(cases.index(case)))
                target.mkdir()
                result = self.run_aok("project", "init", str(target), *case)
                self.assertEqual(result.returncode, 1)
                self.assertEqual(list(target.iterdir()), [])

    def test_incompatible_active_overlay_is_rejected(self) -> None:
        profiles = {
            "kanban": {
                "id": "kanban",
                "kind": "flow",
                "supportLevel": "active",
            },
            "xp": {
                "id": "xp",
                "kind": "overlay",
                "supportLevel": "active",
                "order": 10,
                "compatibleFlows": [],
            },
        }
        with self.assertRaises(self.aok_module.AokError) as raised:
            self.aok_module.normalize_methodology_selection(
                "kanban", ["xp"], profiles
            )
        self.assertIn("not compatible", str(raised.exception))

    def test_methodology_set_replaces_selection_and_preserves_manifest_keys(self) -> None:
        initialized = self.run_aok("project", "init", str(self.project))
        self.assertEqual(initialized.returncode, 0, initialized.stderr)
        manifest_path = self.project / ".aok" / "project.json"
        manifest = self.manifest()
        manifest["workSystem"] = {"provider": "github", "marker": "keep"}
        manifest["customKey"] = {"keep": True}
        manifest_path.write_text(json.dumps(manifest))

        with_overlays = self.run_aok(
            "methodology",
            "set",
            str(self.project),
            "--flow",
            "kanban",
            "--overlay",
            "lean",
            "--overlay",
            "xp",
        )
        self.assertEqual(with_overlays.returncode, 0, with_overlays.stderr)
        cleared = self.run_aok(
            "methodology",
            "set",
            str(self.project),
            "--flow",
            "kanban",
        )
        self.assertEqual(cleared.returncode, 0, cleared.stderr)
        final = self.manifest()
        self.assertEqual(final["methodology"], {"flow": "kanban", "overlays": []})
        self.assertEqual(final["workSystem"]["marker"], "keep")
        self.assertEqual(final["customKey"], {"keep": True})

    def test_project_init_preserves_selection_and_all_manifest_keys(self) -> None:
        initialized = self.run_aok(
            "project",
            "init",
            str(self.project),
            "--flow",
            "kanban",
            "--overlay",
            "xp",
        )
        self.assertEqual(initialized.returncode, 0, initialized.stderr)
        manifest_path = self.project / ".aok" / "project.json"
        manifest = self.manifest()
        manifest["workSystem"] = {"provider": "github", "marker": "keep"}
        manifest["futureKey"] = "keep"
        manifest_path.write_text(json.dumps(manifest))

        refreshed = self.run_aok("project", "init", str(self.project))
        self.assertEqual(refreshed.returncode, 0, refreshed.stderr)
        final = self.manifest()
        self.assertEqual(final["methodology"], {"flow": "kanban", "overlays": ["xp"]})
        self.assertEqual(final["workSystem"]["marker"], "keep")
        self.assertEqual(final["futureKey"], "keep")

        explicit_reset = self.run_aok(
            "project", "init", str(self.project), "--flow", "kanban"
        )
        self.assertEqual(explicit_reset.returncode, 0, explicit_reset.stderr)
        self.assertEqual(
            self.manifest()["methodology"],
            {"flow": "kanban", "overlays": []},
        )

    def test_manual_policy_edit_requires_force_and_creates_backup(self) -> None:
        initialized = self.run_aok(
            "project",
            "init",
            str(self.project),
            "--flow",
            "kanban",
            "--overlay",
            "xp",
        )
        self.assertEqual(initialized.returncode, 0, initialized.stderr)
        policy_path = self.project / "agents" / "methodology.md"
        policy_path.write_text(policy_path.read_text() + "\nmanual edit\n")
        old_manifest = self.manifest()

        refused = self.run_aok(
            "methodology",
            "set",
            str(self.project),
            "--flow",
            "kanban",
            "--overlay",
            "lean",
        )
        self.assertEqual(refused.returncode, 1)
        self.assertIn("Refusing to overwrite modified methodology policy", refused.stderr)
        self.assertEqual(self.manifest(), old_manifest)
        self.assertIn("manual edit", policy_path.read_text())

        forced = self.run_aok(
            "methodology",
            "set",
            str(self.project),
            "--flow",
            "kanban",
            "--overlay",
            "lean",
            "--force",
        )
        self.assertEqual(forced.returncode, 0, forced.stderr)
        backups = list((self.project / ".aok-backups").glob("*/agents/methodology.md"))
        self.assertEqual(len(backups), 1)
        self.assertIn("manual edit", backups[0].read_text())
        self.assertIn("## Overlay: Lean", self.policy())

    def test_legacy_project_is_implicit_kanban(self) -> None:
        manifest_path = self.project / ".aok" / "project.json"
        manifest_path.parent.mkdir()
        manifest_path.write_text("{}")
        result = self.run_aok(
            "methodology", "validate", str(self.project), "--json"
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertTrue(report["valid"])
        self.assertTrue(report["implicit"])
        self.assertEqual(report["selection"], {"flow": "kanban", "overlays": []})
        self.assertEqual(report["checks"][-1]["status"], "implicit-default")

    def test_explicit_selection_detects_stale_and_missing_policy(self) -> None:
        initialized = self.run_aok("project", "init", str(self.project))
        self.assertEqual(initialized.returncode, 0, initialized.stderr)
        policy_path = self.project / "agents" / "methodology.md"
        policy_path.write_text(policy_path.read_text() + "\nstale\n")
        stale = self.run_aok(
            "methodology", "validate", str(self.project), "--json"
        )
        self.assertEqual(stale.returncode, 1)
        self.assertEqual(json.loads(stale.stdout)["checks"][-1]["status"], "mismatched")
        project_validation = self.run_aok("project", "validate", str(self.project))
        self.assertEqual(project_validation.returncode, 1)
        self.assertIn("methodology policy mismatched", project_validation.stdout)

        policy_path.unlink()
        missing = self.run_aok(
            "methodology", "validate", str(self.project), "--json"
        )
        self.assertEqual(missing.returncode, 1)
        self.assertEqual(json.loads(missing.stdout)["checks"][-1]["status"], "missing")

    def test_catalog_exposes_support_levels_and_task_statuses_stay_stable(self) -> None:
        listed = self.run_aok("methodology", "list", "--json")
        self.assertEqual(listed.returncode, 0, listed.stderr)
        profiles = {item["id"]: item for item in json.loads(listed.stdout)["profiles"]}
        self.assertEqual(profiles["kanban"]["supportLevel"], "active")
        self.assertEqual(profiles["xp"]["kind"], "overlay")
        self.assertEqual(profiles["fdd"]["supportLevel"], "scaffolded")
        self.assertEqual(profiles["am"]["supportLevel"], "planned")

        task_schema = json.loads((ROOT / "schemas" / "task-packet.schema.json").read_text())
        self.assertEqual(
            task_schema["properties"]["status"]["enum"],
            ["inbox", "ready", "in-progress", "qa", "review", "blocked", "done"],
        )

    def test_set_requires_initialized_project(self) -> None:
        set_result = self.run_aok(
            "methodology", "set", str(self.project), "--flow", "kanban"
        )
        self.assertEqual(set_result.returncode, 1)
        self.assertIn("Project is not initialized", set_result.stderr)
        show_result = self.run_aok("methodology", "show", str(self.project))
        self.assertEqual(show_result.returncode, 1)
        self.assertIn("Project is not initialized", show_result.stderr)
        validate_result = self.run_aok(
            "methodology", "validate", str(self.project), "--json"
        )
        self.assertEqual(validate_result.returncode, 1)
        self.assertEqual(
            json.loads(validate_result.stdout)["checks"][0],
            {"name": "manifest", "status": "missing"},
        )


if __name__ == "__main__":
    unittest.main()
