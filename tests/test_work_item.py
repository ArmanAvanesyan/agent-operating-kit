from __future__ import annotations

import hashlib
import importlib.util
import json
import shutil
import subprocess
import sys
import tempfile
import unittest
from importlib.machinery import SourceFileLoader
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
AOK = ROOT / "scripts" / "aok"


class WorkItemTests(unittest.TestCase):
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

    def init(self, *args: str) -> None:
        result = self.run_aok("project", "init", str(self.project), *args)
        self.assertEqual(result.returncode, 0, result.stderr)

    def scaffold(self, *args: str) -> dict:
        result = self.run_aok(
            "work-item", "scaffold", str(self.project), *args, "--json"
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        return json.loads(result.stdout)

    def test_catalog_counts_order_and_task_contract_stability(self) -> None:
        catalog_result = self.run_aok("work-item", "list", "--json")
        self.assertEqual(catalog_result.returncode, 0, catalog_result.stderr)
        catalog = json.loads(catalog_result.stdout)
        self.assertEqual(catalog["catalogCount"], 19)
        self.assertEqual(catalog["effectiveCount"], 19)

        cases = [
            (("kanban",), 8),
            (("kanban", "xp"), 9),
            (("kanban", "lean"), 10),
            (("kanban", "xp", "lean"), 11),
            (("fdd",), 9),
            (("dsdm",), 9),
            (("aup",), 14),
        ]
        for case, expected in cases:
            with self.subTest(case=case):
                args = ["work-item", "list", "--flow", case[0]]
                for overlay in case[1:]:
                    args.extend(["--overlay", overlay])
                args.append("--json")
                result = self.run_aok(*args)
                self.assertEqual(result.returncode, 0, result.stderr)
                data = json.loads(result.stdout)
                self.assertEqual(data["effectiveCount"], expected)
                self.assertEqual(
                    [item["id"] for item in data["types"][:8]],
                    [
                        "epic",
                        "feature",
                        "task",
                        "bug",
                        "spike",
                        "incident",
                        "risk",
                        "change",
                    ],
                )

        task_schema = json.loads(
            (ROOT / "schemas" / "task-packet.schema.json").read_text()
        )
        self.assertNotIn("workItemType", task_schema["required"])
        self.assertEqual(
            set(task_schema["properties"]["workItemType"]["enum"]),
            {item["id"] for item in catalog["types"]},
        )
        self.assertEqual(
            task_schema["properties"]["status"]["enum"],
            ["inbox", "ready", "in-progress", "qa", "review", "blocked", "done"],
        )
        self.assertEqual(
            task_schema["properties"]["phase"]["enum"],
            ["intake", "planning", "build", "qa", "review", "release", "operate"],
        )

    def test_list_is_deterministic_and_rejects_invalid_combinations(self) -> None:
        first = self.run_aok(
            "work-item",
            "list",
            "--flow",
            "kanban",
            "--overlay",
            "lean",
            "--overlay",
            "xp",
            "--json",
        )
        second = self.run_aok(
            "work-item",
            "list",
            "--flow",
            "kanban",
            "--overlay",
            "xp",
            "--overlay",
            "lean",
            "--json",
        )
        self.assertEqual(first.returncode, 0, first.stderr)
        self.assertEqual(first.stdout, second.stdout)
        self.assertEqual(
            json.loads(first.stdout)["selection"]["overlays"], ["xp", "lean"]
        )

        invalid = [
            ("--overlay", "xp"),
            ("--flow", "fdd", "--overlay", "xp"),
            ("--flow", "kanban", "--overlay", "xp", "--overlay", "xp"),
            ("--flow", "am"),
        ]
        for args in invalid:
            with self.subTest(args=args):
                result = self.run_aok("work-item", "list", *args)
                self.assertEqual(result.returncode, 1)

        am = self.run_aok("work-item", "list", "--flow", "kanban", "--overlay", "am")
        self.assertEqual(am.returncode, 0, am.stderr)

    def test_scaffold_all_outputs_is_idempotent_and_preserves_project(self) -> None:
        self.init(
            "--flow",
            "kanban",
            "--overlay",
            "lean",
            "--overlay",
            "xp",
        )
        project_manifest = self.project / ".aok" / "project.json"
        before = project_manifest.read_bytes()
        first = self.scaffold()
        self.assertEqual(first["effectiveCount"], 11)
        self.assertEqual(first["providers"], ["neutral", "github", "azure-devops"])
        self.assertFalse(first["projectManifestChanged"])
        self.assertFalse(first["remoteMutation"])
        self.assertEqual(len(first["files"]), 35)
        self.assertEqual(project_manifest.read_bytes(), before)

        neutral = self.project / "agents/tasks/types/aok-xp-user-story.md"
        github = self.project / ".github/ISSUE_TEMPLATE/aok-xp-user-story.yml"
        azure = (
            self.project
            / "docs/aok/work-items/azure-devops/aok-xp-user-story.md"
        )
        for path in (neutral, github, azure):
            self.assertTrue(path.exists(), path)
        self.assertIn("Test-First Plan", neutral.read_text())
        self.assertIn('type: "Feature"', github.read_text())
        self.assertIn("AOK work-item type:** `xp-user-story`", github.read_text())
        self.assertNotIn("\nlabels:", github.read_text())
        self.assertIn("desired-state guidance", azure.read_text())
        self.assertFalse(
            (self.project / ".github/ISSUE_TEMPLATE/config.yml").exists()
        )
        self.assertFalse((self.project / ".github/PULL_REQUEST_TEMPLATE.md").exists())

        before_files = {
            item["path"]: (self.project / item["path"]).read_bytes()
            for item in json.loads(
                (self.project / ".aok/work-item-scaffold.json").read_text()
            )["files"]
        }
        second = self.scaffold()
        self.assertTrue(
            all(item["status"] == "unchanged" for item in second["files"])
        )
        for relative, content in before_files.items():
            self.assertEqual((self.project / relative).read_bytes(), content)
        validation = self.run_aok(
            "work-item", "validate", str(self.project), "--json"
        )
        self.assertEqual(validation.returncode, 0, validation.stderr)
        self.assertTrue(json.loads(validation.stdout)["valid"])

    def test_github_forms_use_only_default_types_and_constrained_yaml(self) -> None:
        self.scaffold("--flow", "aup", "--provider", "github")
        forms = sorted((self.project / ".github/ISSUE_TEMPLATE").glob("aok-*.yml"))
        self.assertEqual(len(forms), 14)
        mapped = {}
        for form in forms:
            text = form.read_text()
            self.assertTrue(
                text.startswith(
                    "# Generated by AOK work-item scaffold. Do not edit directly.\n"
                )
            )
            lines = text.splitlines()
            self.assertEqual(lines[1].split(": ", 1)[0], "name")
            self.assertEqual(lines[2].split(": ", 1)[0], "description")
            native_type = json.loads(lines[3].split(": ", 1)[1])
            self.assertIn(native_type, {"Feature", "Task", "Bug"})
            self.assertEqual(lines[4], "body:")
            self.assertNotIn("\t", text)
            self.assertNotIn("\nlabels:", text)
            for line in lines:
                if ": \"" in line:
                    json.loads(line.split(": ", 1)[1])
            mapped[form.stem] = native_type
        self.assertEqual(mapped["aok-aup-defect"], "Bug")
        self.assertEqual(mapped["aok-aup-technical"], "Task")
        self.assertEqual(mapped["aok-aup-use-case"], "Feature")

    def test_methodology_specific_neutral_templates_render_contract_fields(self) -> None:
        cases = {
            "fdd": {
                "file": "aok-fdd-feature.md",
                "phrases": [
                    "Subject Area and Business Activity",
                    "Feature ID and Canonical Name",
                    "Parsed Name Parts",
                    "Feature Milestones",
                ],
            },
            "dsdm": {
                "file": "aok-dsdm-requirement.md",
                "phrases": [
                    "Requirement ID, Theme, and Outcome",
                    "MoSCoW Priorities",
                    "Project Increment and Timebox",
                    "Business Acceptance",
                ],
            },
            "aup": {
                "file": "aok-aup-non-functional.md",
                "phrases": [
                    "Work-Item ID, Outcome, and Priority",
                    "Iteration Link and Objective",
                    "Quality Attribute and Context",
                    "Discipline Implications",
                ],
            },
        }
        for flow, expected in cases.items():
            with self.subTest(flow=flow):
                target = Path(self.temp.name) / flow
                target.mkdir()
                result = self.run_aok(
                    "work-item",
                    "scaffold",
                    str(target),
                    "--flow",
                    flow,
                    "--provider",
                    "neutral",
                    "--json",
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                text = (
                    target / "agents/tasks/types" / expected["file"]
                ).read_text()
                for phrase in expected["phrases"]:
                    self.assertIn(phrase, text)

        overlay_target = Path(self.temp.name) / "overlays"
        overlay_target.mkdir()
        overlay = self.run_aok(
            "work-item",
            "scaffold",
            str(overlay_target),
            "--flow",
            "kanban",
            "--overlay",
            "xp",
            "--overlay",
            "lean",
            "--provider",
            "neutral",
        )
        self.assertEqual(overlay.returncode, 0, overlay.stderr)
        self.assertIn(
            "Test-First Plan",
            (
                overlay_target
                / "agents/tasks/types/aok-xp-user-story.md"
            ).read_text(),
        )
        self.assertIn(
            "Current Condition and Waste",
            (
                overlay_target
                / "agents/tasks/types/aok-lean-improvement.md"
            ).read_text(),
        )

    def test_azure_uses_verified_default_requirement_type_offline(self) -> None:
        manifest_path = self.project / ".aok/project.json"
        manifest_path.parent.mkdir()
        manifest = {
            "methodology": {"flow": "kanban", "overlays": []},
            "workSystem": {
                "provider": "azure-devops",
                "supportLevel": "scaffolded",
                "verifiedAt": "2026-07-30T12:00:00+00:00",
                "workItemType": {
                    "name": "Product Backlog Item",
                    "referenceName": "Microsoft.VSTS.Scrum.ProductBacklogItem",
                },
            },
            "custom": {"preserve": True},
        }
        manifest_path.write_text(json.dumps(manifest))
        before = manifest_path.read_bytes()
        report = self.scaffold(
            "--flow", "dsdm", "--provider", "azure-devops"
        )
        self.assertEqual(report["effectiveCount"], 9)
        desired = json.loads(
            (
                self.project
                / "docs/aok/work-items/azure-devops/desired-state.json"
            ).read_text()
        )
        requirement = next(
            item for item in desired["mappings"] if item["aokType"] == "dsdm-requirement"
        )
        self.assertEqual(requirement["providerType"], "Product Backlog Item")
        self.assertEqual(
            desired["defaultRequirementWorkItemType"], "Product Backlog Item"
        )
        self.assertFalse(desired["remoteMutation"])
        self.assertEqual(manifest_path.read_bytes(), before)

    def test_scaffold_conflict_is_atomic_force_backs_up_and_validation_fails_stale(self) -> None:
        self.init()
        self.scaffold("--provider", "neutral")
        edited = self.project / "agents/tasks/types/aok-bug.md"
        untouched = self.project / "agents/tasks/types/aok-task.md"
        edited.write_text(edited.read_text() + "\nmanual edit\n")
        untouched_hash = hashlib.sha256(untouched.read_bytes()).hexdigest()

        refused = self.run_aok(
            "work-item",
            "scaffold",
            str(self.project),
            "--provider",
            "neutral",
            "--json",
        )
        self.assertEqual(refused.returncode, 1)
        self.assertIn("Refusing to overwrite", refused.stderr)
        self.assertIn("manual edit", edited.read_text())
        self.assertEqual(
            hashlib.sha256(untouched.read_bytes()).hexdigest(), untouched_hash
        )

        invalid = self.run_aok("work-item", "validate", str(self.project), "--json")
        self.assertEqual(invalid.returncode, 1)
        self.assertFalse(json.loads(invalid.stdout)["valid"])

        forced = self.scaffold("--provider", "neutral", "--force")
        changed = next(
            item
            for item in forced["files"]
            if item["path"] == "agents/tasks/types/aok-bug.md"
        )
        self.assertEqual(changed["status"], "updated")
        self.assertIn("manual edit", (self.project / changed["backup"]).read_text())
        self.assertNotIn("manual edit", edited.read_text())

        edited.unlink()
        missing = self.run_aok("work-item", "validate", str(self.project), "--json")
        self.assertEqual(missing.returncode, 1)
        checks = json.loads(missing.stdout)["checks"]
        self.assertIn(
            {"name": "agents/tasks/types/aok-bug.md", "status": "missing"},
            checks,
        )

    def test_legacy_inference_and_scaffolded_flow_do_not_change_methodology(self) -> None:
        legacy = self.scaffold("--provider", "neutral")
        self.assertEqual(
            legacy["selection"], {"flow": "kanban", "overlays": []}
        )
        self.assertFalse((self.project / ".aok/project.json").exists())

        other = Path(self.temp.name) / "fdd"
        other.mkdir()
        manifest_path = other / ".aok/project.json"
        manifest_path.parent.mkdir()
        manifest_path.write_text(
            json.dumps(
                {
                    "methodology": {"flow": "kanban", "overlays": ["xp"]},
                    "workSystem": {"provider": "github", "marker": "preserve"},
                    "custom": "preserve",
                }
            )
        )
        before = manifest_path.read_bytes()
        result = self.run_aok(
            "work-item",
            "scaffold",
            str(other),
            "--flow",
            "fdd",
            "--provider",
            "neutral",
            "--json",
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(report["selection"], {"flow": "fdd", "overlays": []})
        self.assertEqual(report["effectiveCount"], 9)
        self.assertTrue(
            (other / "agents/tasks/types/aok-fdd-feature.md").exists()
        )
        self.assertEqual(manifest_path.read_bytes(), before)

    def test_selection_and_provider_changes_replace_only_recorded_generated_files(self) -> None:
        self.init("--flow", "kanban", "--overlay", "xp")
        self.scaffold()
        xp_form = self.project / ".github/ISSUE_TEMPLATE/aok-xp-user-story.yml"
        self.assertTrue(xp_form.exists())

        changed = self.scaffold(
            "--flow", "dsdm", "--provider", "neutral"
        )
        self.assertEqual(changed["selection"], {"flow": "dsdm", "overlays": []})
        self.assertEqual(changed["providers"], ["neutral"])
        self.assertFalse(xp_form.exists())
        self.assertTrue(
            (self.project / "agents/tasks/types/aok-dsdm-requirement.md").exists()
        )
        self.assertFalse(
            (self.project / "docs/aok/work-items/github/desired-state.json").exists()
        )
        self.assertTrue(
            any(item["status"] == "removed" for item in changed["files"])
        )
        validation = self.run_aok(
            "work-item", "validate", str(self.project), "--json"
        )
        self.assertEqual(validation.returncode, 0, validation.stderr)

    def test_validation_requires_manifest(self) -> None:
        result = self.run_aok("work-item", "validate", str(self.project), "--json")
        self.assertEqual(result.returncode, 1)
        report = json.loads(result.stdout)
        self.assertFalse(report["valid"])
        self.assertEqual(
            report["checks"], [{"name": "manifest", "status": "missing"}]
        )

    def test_force_backs_up_foreign_manifest_without_removing_its_files(self) -> None:
        self.init()
        manifest_path = self.project / ".aok/work-item-scaffold.json"
        foreign_file = self.project / "notes.md"
        content = "<!-- Generated by AOK work-item scaffold. Do not edit directly. -->\nKeep me.\n"
        foreign_file.write_text(content)
        manifest_path.write_text(json.dumps({
            "generatedBy": "another-tool",
            "kind": "foreign-manifest",
            "files": [{"path": "notes.md", "sha256": hashlib.sha256(content.encode()).hexdigest()}],
        }))
        original = manifest_path.read_bytes()
        refused = self.run_aok("work-item", "scaffold", str(self.project), "--provider", "neutral")
        self.assertEqual(refused.returncode, 1, refused.stderr)
        self.assertEqual(manifest_path.read_bytes(), original)
        self.assertEqual(foreign_file.read_text(), content)

        forced = self.scaffold("--provider", "neutral", "--force")
        metadata = next(item for item in forced["files"] if item["path"] == ".aok/work-item-scaffold.json")
        self.assertEqual((self.project / metadata["backup"]).read_bytes(), original)
        self.assertEqual(foreign_file.read_text(), content)
        validated = self.run_aok("work-item", "validate", str(self.project), "--json")
        self.assertEqual(validated.returncode, 0, validated.stderr)

    def test_scaffold_rejects_symlinked_manifest_even_with_force(self) -> None:
        self.init()
        project_manifest = self.project / ".aok/project.json"
        original = project_manifest.read_bytes()
        (self.project / ".aok/work-item-scaffold.json").symlink_to(project_manifest)
        result = self.run_aok("work-item", "scaffold", str(self.project), "--provider", "neutral", "--force")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("must not be a symlink", result.stderr)
        self.assertEqual(project_manifest.read_bytes(), original)
        self.assertFalse((self.project / "agents/tasks/types").exists())

    def test_scaffold_rejects_manifest_parent_outside_target(self) -> None:
        outside = Path(self.temp.name) / "outside"
        outside.mkdir()
        (self.project / ".aok").symlink_to(outside, target_is_directory=True)
        result = self.run_aok("work-item", "scaffold", str(self.project), "--flow", "kanban", "--force")
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertIn("manifest escapes target", result.stderr)
        self.assertEqual(list(outside.iterdir()), [])
        self.assertFalse((self.project / "agents").exists())

    def test_kit_validation_rejects_malformed_catalog_references(self) -> None:
        isolated = Path(self.temp.name) / "isolated-kit"
        (isolated / "scripts").mkdir(parents=True)
        (isolated / "schemas").mkdir()
        shutil.copy2(AOK, isolated / "scripts/aok")
        shutil.copytree(ROOT / "methodologies", isolated / "methodologies")
        shutil.copytree(ROOT / "work-items", isolated / "work-items")
        shutil.copy2(
            ROOT / "schemas/task-packet.schema.json",
            isolated / "schemas/task-packet.schema.json",
        )
        catalog_path = isolated / "work-items/catalog.json"
        catalog = json.loads(catalog_path.read_text())
        catalog["types"][0]["taskPacketFields"].append("unknownField")
        catalog["types"][1]["hierarchy"]["parentTypes"] = ["missing-type"]
        catalog["types"][2]["sourcePath"] = "templates/missing.md"
        catalog["types"][3]["providerMappings"]["github"]["issueType"] = "Custom"
        catalog_path.write_text(json.dumps(catalog))

        loader = SourceFileLoader(
            "aok_malformed_work_item_test", str(isolated / "scripts/aok")
        )
        spec = importlib.util.spec_from_loader(loader.name, loader)
        assert spec is not None
        module = importlib.util.module_from_spec(spec)
        loader.exec_module(module)
        errors: list[str] = []
        module.validate_work_item_catalog(errors)
        self.assertTrue(
            any("references unknown task field unknownField" in error for error in errors),
            errors,
        )
        self.assertTrue(
            any("hierarchy references unknown type missing-type" in error for error in errors),
            errors,
        )
        self.assertTrue(
            any("Work-item template is missing" in error for error in errors), errors
        )
        self.assertTrue(
            any("invalid GitHub issue type" in error for error in errors), errors
        )


if __name__ == "__main__":
    unittest.main()
