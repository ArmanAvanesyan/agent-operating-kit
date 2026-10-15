from __future__ import annotations

import json
import importlib.util
from importlib.machinery import SourceFileLoader
import shutil
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
        self.assertEqual(
            task_schema["properties"]["phase"]["enum"],
            ["intake", "planning", "build", "qa", "review", "release", "operate"],
        )
        self.assertEqual(
            task_schema["properties"]["moscow"]["enum"],
            ["must", "should", "could", "wont"],
        )
        self.assertTrue(
            {
                "lifecycleStage",
                "featureId",
                "subjectArea",
                "businessActivity",
                "featureName",
                "featureNameParts",
                "estimatedEffortDays",
                "featureMilestones",
                "requirementId",
                "requirementTheme",
                "requirementOutcome",
                "projectIncrement",
                "projectMoscow",
                "incrementMoscow",
                "timeboxMoscow",
                "requirementType",
                "iterationId",
                "iterationObjective",
                "disciplineEvidence",
                "moscow",
                "timebox",
            }.issubset(
                task_schema["properties"]
            )
        )
        self.assertFalse(
            {
                "lifecycleStage",
                "featureId",
                "subjectArea",
                "businessActivity",
                "featureName",
                "featureNameParts",
                "estimatedEffortDays",
                "featureMilestones",
                "requirementId",
                "requirementTheme",
                "requirementOutcome",
                "projectIncrement",
                "projectMoscow",
                "incrementMoscow",
                "timeboxMoscow",
                "requirementType",
                "iterationId",
                "iterationObjective",
                "disciplineEvidence",
                "moscow",
                "timebox",
            }.intersection(
                task_schema["required"]
            )
        )
        self.assertEqual(
            task_schema["properties"]["estimatedEffortDays"],
            {"type": "integer", "minimum": 1, "maximum": 10},
        )
        self.assertEqual(
            task_schema["properties"]["requirementType"]["enum"],
            [
                "use-case",
                "user-story",
                "non-functional",
                "business-rule",
                "defect",
                "technical",
            ],
        )
        self.assertEqual(
            task_schema["properties"]["disciplineEvidence"]["required"],
            [
                "model",
                "implementation",
                "test",
                "deployment",
                "configurationManagement",
                "projectManagement",
                "environment",
            ],
        )

        self.assertEqual(profiles["dsdm"]["lifecycle"]["initialStage"], "pre-project")
        self.assertEqual(profiles["dsdm"]["catalogCounts"]["roles"], 13)
        self.assertGreaterEqual(profiles["dsdm"]["catalogCounts"]["artifacts"], 14)
        self.assertIn(
            {"from": "build-by-feature", "to": "design-by-feature", "loop": True},
            profiles["fdd"]["lifecycle"]["transitions"],
        )
        fdd_build_gate = next(
            gate for gate in profiles["fdd"]["gates"] if gate["id"] == "start-feature-build"
        )
        self.assertIn("featureId", fdd_build_gate["requiredInputs"])
        dsdm_foundations_gate = next(
            gate
            for gate in profiles["dsdm"]["gates"]
            if gate["id"] == "approve-foundations"
        )
        self.assertEqual(
            dsdm_foundations_gate["requiredInputs"],
            ["requirementId", "requirementOutcome", "projectMoscow", "acceptanceCriteria"],
        )
        self.assertIn(
            "dsdm-development-approach-definition",
            dsdm_foundations_gate["requiredArtifacts"],
        )
        dsdm_close_timebox = next(
            gate for gate in profiles["dsdm"]["gates"] if gate["id"] == "close-timebox"
        )
        self.assertEqual(
            (dsdm_close_timebox["from"], dsdm_close_timebox["to"]),
            ("evolutionary-development", "evolutionary-development"),
        )
        self.assertIn(
            "timebox-midpoint-review",
            [item["id"] for item in profiles["dsdm"]["ceremonies"]],
        )
        self.assertEqual(
            [stage["id"] for stage in profiles["aup"]["lifecycle"]["stages"]],
            ["inception", "elaboration", "construction", "transition"],
        )
        self.assertEqual(
            next(
                item
                for item in profiles["aup"]["ceremonies"]
                if item["id"] == "iteration-planning"
            )["lifecycleStages"],
            ["inception", "elaboration", "construction", "transition"],
        )
        self.assertEqual(
            (
                next(
                    gate
                    for gate in profiles["aup"]["gates"]
                    if gate["id"] == "complete-construction-iteration"
                )["from"],
                next(
                    gate
                    for gate in profiles["aup"]["gates"]
                    if gate["id"] == "complete-construction-iteration"
                )["to"],
            ),
            ("construction", "construction"),
        )
        fdd_contract = profiles["fdd"]["featureDefinition"]
        self.assertEqual(
            [item["id"] for item in fdd_contract["hierarchy"]],
            ["subjectArea", "businessActivity", "feature"],
        )
        self.assertEqual(fdd_contract["hierarchy"][1]["alias"], "Feature Set")
        self.assertEqual(
            [item["weight"] for item in fdd_contract["milestones"]],
            [1, 40, 3, 45, 10, 1],
        )
        self.assertEqual(
            [item["cumulative"] for item in fdd_contract["milestones"]],
            [1, 41, 44, 89, 99, 100],
        )
        dsdm_contract = profiles["dsdm"]["requirementDefinition"]
        self.assertEqual(
            [item["id"] for item in dsdm_contract["hierarchy"]],
            ["requirementTheme", "requirement", "acceptanceCriterion"],
        )
        self.assertEqual(
            [item["field"] for item in dsdm_contract["moscow"]["levels"]],
            ["projectMoscow", "incrementMoscow", "timeboxMoscow"],
        )
        self.assertEqual(
            [item["id"] for item in dsdm_contract["timeboxing"]["checkpoints"]],
            ["kick-off", "mid-timebox", "close-out"],
        )
        self.assertNotIn("milestones", dsdm_contract)
        aup_contract = profiles["aup"]["iterationDefinition"]
        self.assertEqual(
            aup_contract["selectedWork"]["types"],
            [
                "use-case",
                "user-story",
                "non-functional",
                "business-rule",
                "defect",
                "technical",
            ],
        )
        self.assertEqual(
            [item["id"] for item in aup_contract["disciplines"]],
            [
                "model",
                "implementation",
                "test",
                "deployment",
                "configuration-management",
                "project-management",
                "environment",
            ],
        )
        self.assertEqual(
            [
                (item["lifecycleStage"], item["id"])
                for item in aup_contract["milestones"]
            ],
            [
                ("inception", "lco"),
                ("elaboration", "lca"),
                ("construction", "ioc"),
                ("transition", "pr"),
            ],
        )
        self.assertNotIn("weight", json.dumps(aup_contract))
        self.assertNotIn("moscow", json.dumps(aup_contract).lower())

    def test_validate_feature_accepts_short_and_classic_names(self) -> None:
        short = self.run_aok(
            "methodology",
            "validate-feature",
            "--name",
            "  Generate   PDF   for   Invoice  ",
            "--json",
        )
        self.assertEqual(short.returncode, 0, short.stderr)
        short_report = json.loads(short.stdout)
        self.assertTrue(short_report["valid"])
        self.assertEqual(short_report["normalizedName"], "Generate PDF for Invoice")
        self.assertEqual(short_report["style"], "short")
        self.assertEqual(
            short_report["parts"],
            {
                "action": "Generate",
                "result": "PDF",
                "preposition": "for",
                "object": "Invoice",
                "style": "short",
            },
        )

        classic = self.run_aok(
            "methodology",
            "validate-feature",
            "--name",
            "Calculate the total amount of a Sale",
            "--json",
        )
        self.assertEqual(classic.returncode, 0, classic.stderr)
        classic_report = json.loads(classic.stdout)
        self.assertEqual(classic_report["style"], "classic")
        self.assertEqual(classic_report["canonicalName"], "Calculate total amount of Sale")
        self.assertEqual(classic_report["parts"]["article"], "a")

        multiword = self.run_aok(
            "methodology",
            "validate-feature",
            "--name",
            "Calculate total quantity sold by Retail Outlet for Item",
            "--json",
        )
        self.assertEqual(multiword.returncode, 0, multiword.stderr)
        self.assertEqual(
            json.loads(multiword.stdout)["parts"]["object"],
            "Retail Outlet for Item",
        )

    def test_validate_feature_rejects_malformed_names_deterministically(self) -> None:
        invalid_names = [
            "",
            "Generate for Invoice",
            "Generate PDF around Invoice",
            "Generate PDF",
            "Calculate the total of Sale",
            "Calculate total of a Sale",
        ]
        for name in invalid_names:
            with self.subTest(name=name):
                result = self.run_aok(
                    "methodology", "validate-feature", "--name", name, "--json"
                )
                self.assertEqual(result.returncode, 1)
                report = json.loads(result.stdout)
                self.assertFalse(report["valid"])
                self.assertIsNone(report["parts"])
                self.assertTrue(report["errors"])
                repeated = self.run_aok(
                    "methodology", "validate-feature", "--name", name, "--json"
                )
                self.assertEqual(repeated.stdout, result.stdout)

        text = self.run_aok(
            "methodology",
            "validate-feature",
            "--name",
            "Generate PDF for Invoice",
        )
        self.assertEqual(text.returncode, 0, text.stderr)
        self.assertIn("FDD feature name validation: passed", text.stdout)
        self.assertIn("Canonical short name: Generate PDF for Invoice", text.stdout)

    def test_validate_requirement_accepts_project_and_three_level_definitions(self) -> None:
        project_only = self.run_aok(
            "methodology",
            "validate-requirement",
            "--id",
            "BILL-14",
            "--outcome",
            "  Produce   customer-ready invoice PDF ",
            "--project-moscow",
            "must",
            "--acceptance",
            "  PDF matches approved invoice content ",
            "--json",
        )
        self.assertEqual(project_only.returncode, 0, project_only.stderr)
        project_report = json.loads(project_only.stdout)
        self.assertTrue(project_report["valid"])
        self.assertEqual(project_report["requirement"]["id"], "BILL-14")
        self.assertEqual(
            project_report["requirement"]["outcome"],
            "Produce customer-ready invoice PDF",
        )
        self.assertEqual(project_report["priorities"], {"project": "must"})

        complete = self.run_aok(
            "methodology",
            "validate-requirement",
            "--id",
            "REG2-003",
            "--theme",
            " Regulatory   Reporting ",
            "--outcome",
            "Publish an approved regulatory return",
            "--project-moscow",
            "must",
            "--acceptance",
            "The return passes schema validation",
            "--acceptance",
            "The Business Ambassador accepts the demonstration",
            "--increment",
            "INC-1",
            "--increment-moscow",
            "should",
            "--timebox",
            "TB-03",
            "--timebox-moscow",
            "could",
            "--json",
        )
        self.assertEqual(complete.returncode, 0, complete.stderr)
        report = json.loads(complete.stdout)
        self.assertEqual(report["requirement"]["theme"], "Regulatory Reporting")
        self.assertEqual(
            report["priorities"]["projectIncrement"],
            {"id": "INC-1", "moscow": "should"},
        )
        self.assertEqual(
            report["priorities"]["timebox"],
            {"id": "TB-03", "moscow": "could"},
        )

        text = self.run_aok(
            "methodology",
            "validate-requirement",
            "--id",
            "REQ-014",
            "--outcome",
            "Produce customer-ready invoice PDF",
            "--project-moscow",
            "must",
            "--acceptance",
            "The PDF is accepted",
        )
        self.assertEqual(text.returncode, 0, text.stderr)
        self.assertIn("DSDM requirement validation: passed", text.stdout)
        self.assertIn("Requirement ID: REQ-014", text.stdout)

    def test_validate_requirement_rejects_structural_errors_deterministically(self) -> None:
        cases = [
            (
                [
                    "--id", "REQ-14", "--outcome", "Outcome",
                    "--project-moscow", "must", "--acceptance", "Accepted",
                    "--increment", " ",
                ],
                "project increment must be non-empty",
            ),
            (
                [
                    "--id", "REQ-14", "--outcome", "Outcome",
                    "--project-moscow", "must", "--acceptance", "Accepted",
                    "--timebox", " ",
                ],
                "timebox must be non-empty",
            ),
            (
                [
                    "--id", "req-14", "--outcome", "Outcome",
                    "--project-moscow", "must", "--acceptance", "Accepted",
                ],
                "uppercase namespace",
            ),
            (
                [
                    "--id", "REQ-14", "--outcome", " ",
                    "--project-moscow", "must", "--acceptance", "Accepted",
                ],
                "outcome must be non-empty",
            ),
            (
                [
                    "--id", "REQ-14", "--outcome", "Outcome",
                    "--project-moscow", "urgent", "--acceptance", "Accepted",
                ],
                "project MoSCoW priority",
            ),
            (
                [
                    "--id", "REQ-14", "--outcome", "Outcome",
                    "--project-moscow", "must", "--acceptance", "Accepted",
                    "--acceptance", " Accepted ",
                ],
                "acceptance criteria must be unique",
            ),
            (
                [
                    "--id", "REQ-14", "--outcome", "Outcome",
                    "--project-moscow", "must", "--acceptance", "Accepted",
                    "--increment", "INC-1",
                ],
                "project increment and increment MoSCoW",
            ),
            (
                [
                    "--id", "REQ-14", "--outcome", "Outcome",
                    "--project-moscow", "must", "--acceptance", "Accepted",
                    "--timebox-moscow", "must",
                ],
                "timebox and timebox MoSCoW",
            ),
        ]
        for arguments, message in cases:
            with self.subTest(message=message):
                result = self.run_aok(
                    "methodology", "validate-requirement", *arguments, "--json"
                )
                self.assertEqual(result.returncode, 1)
                report = json.loads(result.stdout)
                self.assertFalse(report["valid"])
                self.assertTrue(any(message in error for error in report["errors"]))
                repeated = self.run_aok(
                    "methodology", "validate-requirement", *arguments, "--json"
                )
                self.assertEqual(repeated.stdout, result.stdout)

    def test_validate_iteration_accepts_every_stage_and_complete_disciplines(self) -> None:
        discipline_args = [
            "--discipline", "model=docs/model.md",
            "--discipline", "implementation=PR-142",
            "--discipline", "test=reports/tests.xml",
            "--discipline", "deployment=not-applicable:No release activity",
            "--discipline", "configuration-management=build-142",
            "--discipline", "project-management=docs/iteration-plan.md",
            "--discipline", "environment=ci/run-912",
        ]
        expected = {
            "inception": "lco",
            "elaboration": "lca",
            "construction": "ioc",
            "transition": "pr",
        }
        construction_stdout = ""
        for stage, milestone in expected.items():
            with self.subTest(stage=stage):
                result = self.run_aok(
                    "methodology",
                    "validate-iteration",
                    "--id",
                    "I-12",
                    "--stage",
                    stage,
                    "--objective",
                    "  Deliver   invoice generation increment ",
                    "--work-item",
                    "use-case=UC-03",
                    "--work-item",
                    "non-functional=NFR-01",
                    *discipline_args,
                    "--json",
                )
                self.assertEqual(result.returncode, 0, result.stderr)
                report = json.loads(result.stdout)
                self.assertTrue(report["valid"])
                self.assertEqual(
                    report["iteration"]["objective"],
                    "Deliver invoice generation increment",
                )
                self.assertEqual(report["milestoneTarget"]["id"], milestone)
                self.assertEqual(
                    report["iteration"]["selectedWork"],
                    [
                        {"type": "use-case", "id": "UC-03"},
                        {"type": "non-functional", "id": "NFR-01"},
                    ],
                )
                self.assertEqual(
                    report["iteration"]["disciplineEvidence"]["deployment"],
                    {"notApplicableRationale": "No release activity"},
                )
                if stage == "construction":
                    construction_stdout = result.stdout

        reversed_discipline_args = [
            item
            for pair in reversed(
                list(zip(discipline_args[0::2], discipline_args[1::2]))
            )
            for item in pair
        ]
        reordered = self.run_aok(
            "methodology",
            "validate-iteration",
            "--id",
            "I-12",
            "--stage",
            "construction",
            "--objective",
            "  Deliver   invoice generation increment ",
            "--work-item",
            "use-case=UC-03",
            "--work-item",
            "non-functional=NFR-01",
            *reversed_discipline_args,
            "--json",
        )
        self.assertEqual(reordered.returncode, 0, reordered.stderr)
        self.assertEqual(reordered.stdout, construction_stdout)

        text = self.run_aok(
            "methodology",
            "validate-iteration",
            "--id",
            "I-12",
            "--stage",
            "construction",
            "--objective",
            "Deliver invoice generation increment",
            "--work-item",
            "use-case=UC-03",
            *discipline_args,
        )
        self.assertEqual(text.returncode, 0, text.stderr)
        self.assertIn("AUP iteration validation: passed", text.stdout)
        self.assertIn("Milestone target: Initial Operational Capability (IOC)", text.stdout)

    def test_validate_iteration_rejects_structural_errors_deterministically(self) -> None:
        complete_disciplines = [
            "--discipline", "model=model.md",
            "--discipline", "implementation=PR-1",
            "--discipline", "test=tests.xml",
            "--discipline", "deployment=deploy.md",
            "--discipline", "configuration-management=build-1",
            "--discipline", "project-management=plan.md",
            "--discipline", "environment=ci-1",
        ]
        base = [
            "--id", "I-12",
            "--stage", "construction",
            "--objective", "Deliver outcome",
            "--work-item", "use-case=UC-03",
        ]
        cases = [
            ([
                "--id", "I-12", "--stage", "construction",
                "--objective", "Deliver outcome",
                *complete_disciplines,
            ], "at least one selected work item is required"),
            ([
                "--id", "i-12", "--stage", "construction",
                "--objective", "Deliver outcome", "--work-item", "use-case=UC-03",
                *complete_disciplines,
            ], "uppercase namespace"),
            ([
                "--id", "I-12", "--stage", "unknown",
                "--objective", "Deliver outcome", "--work-item", "use-case=UC-03",
                *complete_disciplines,
            ], "iteration stage must be one of"),
            ([
                "--id", "I-12", "--stage", "construction",
                "--objective", " ", "--work-item", "use-case=UC-03",
                *complete_disciplines,
            ], "objective must be non-empty"),
            ([*base, "--work-item", "use-case=UC-03", *complete_disciplines],
             "selected work id 'UC-03' is duplicated"),
            ([
                "--id", "I-12", "--stage", "construction",
                "--objective", "Deliver outcome", "--work-item", "feature=UC-03",
                *complete_disciplines,
            ], "selected work type 'feature'"),
            ([*base, *complete_disciplines[:-2]], "missing discipline evidence"),
            ([*base], "missing discipline evidence"),
            ([*base, *complete_disciplines, "--discipline", "model=other.md"],
             "discipline 'model' is duplicated"),
            ([*base, *complete_disciplines[:-2], "--discipline", "unknown=value"],
             "unknown discipline 'unknown'"),
            ([
                *base,
                *complete_disciplines[:-2],
                "--discipline", "environment=not-applicable:",
            ], "non-empty not-applicable rationale"),
            ([
                *base,
                *complete_disciplines[:-2],
                "--discipline", "environment=",
            ], "needs non-empty evidence"),
        ]
        for arguments, message in cases:
            with self.subTest(message=message):
                result = self.run_aok(
                    "methodology", "validate-iteration", *arguments, "--json"
                )
                self.assertEqual(result.returncode, 1)
                report = json.loads(result.stdout)
                self.assertFalse(report["valid"])
                self.assertTrue(any(message in error for error in report["errors"]), report)
                repeated = self.run_aok(
                    "methodology", "validate-iteration", *arguments, "--json"
                )
                self.assertEqual(repeated.stdout, result.stdout)

    def test_scaffold_exports_assets_without_changing_project_state(self) -> None:
        initialized = self.run_aok(
            "project", "init", str(self.project), "--flow", "kanban", "--overlay", "xp"
        )
        self.assertEqual(initialized.returncode, 0, initialized.stderr)
        manifest_path = self.project / ".aok" / "project.json"
        manifest_before = manifest_path.read_bytes()

        result = self.run_aok(
            "methodology", "scaffold", str(self.project), "--flow", "fdd", "--json"
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertEqual(report["flow"], "fdd")
        self.assertFalse(report["selectionChanged"])
        self.assertFalse(report["lifecycleStateChanged"])
        self.assertEqual(manifest_path.read_bytes(), manifest_before)
        self.assertTrue((self.project / "agents" / "roles" / "fdd-chief-programmer.md").exists())
        self.assertTrue(
            (
                self.project
                / "docs"
                / "methodology"
                / "fdd"
                / "artifacts"
                / "feature-list.md"
            ).exists()
        )
        readme = self.project / "docs" / "methodology" / "fdd" / "README.md"
        self.assertIn("scaffolded, not active", readme.read_text())
        domain_expert = (
            self.project / "agents" / "roles" / "fdd-domain-expert.md"
        ).read_text()
        self.assertIn("Assignment: `human`", domain_expert)
        self.assertIn("must not assume its approval authority", domain_expert)

        repeated = self.run_aok(
            "methodology", "scaffold", str(self.project), "--flow", "fdd", "--json"
        )
        self.assertEqual(repeated.returncode, 0, repeated.stderr)
        repeated_report = json.loads(repeated.stdout)
        self.assertTrue(
            all(item["status"] == "unchanged" for item in repeated_report["files"])
        )
        self.assertEqual(manifest_path.read_bytes(), manifest_before)

    def test_scaffold_conflict_is_atomic_and_force_creates_backup(self) -> None:
        first = self.run_aok(
            "methodology", "scaffold", str(self.project), "--flow", "aup"
        )
        self.assertEqual(first.returncode, 0, first.stderr)
        role_path = self.project / "agents" / "roles" / "aup-stakeholder.md"
        untouched_path = self.project / "agents" / "roles" / "aup-developer.md"
        untouched_before = untouched_path.read_bytes()
        role_path.write_text(role_path.read_text() + "\nlocal edit\n")

        refused = self.run_aok(
            "methodology", "scaffold", str(self.project), "--flow", "aup"
        )
        self.assertEqual(refused.returncode, 1)
        self.assertIn("Refusing to overwrite", refused.stderr)
        self.assertIn("local edit", role_path.read_text())
        self.assertEqual(untouched_path.read_bytes(), untouched_before)

        forced = self.run_aok(
            "methodology",
            "scaffold",
            str(self.project),
            "--flow",
            "aup",
            "--force",
            "--json",
        )
        self.assertEqual(forced.returncode, 0, forced.stderr)
        report = json.loads(forced.stdout)
        changed = next(
            item for item in report["files"] if item["path"] == "agents/roles/aup-stakeholder.md"
        )
        self.assertEqual(changed["status"], "updated")
        backup_path = self.project / changed["backup"]
        self.assertIn("local edit", backup_path.read_text())
        self.assertNotIn("local edit", role_path.read_text())

    def test_curated_templates_and_generic_fallback_are_deterministic(self) -> None:
        result = self.run_aok(
            "methodology", "scaffold", str(self.project), "--flow", "fdd"
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        marker = (
            "<!-- AOK methodology scaffold. Edit freely; "
            "re-scaffold requires --force. -->\n"
        )
        curated = [
            "overall-model",
            "feature-list",
            "feature-plan",
            "design-package",
            "inspection-record",
            "feature-status-report",
        ]
        for name in curated:
            with self.subTest(template=name):
                source = ROOT / "methodologies" / "fdd" / "templates" / f"{name}.md"
                output = (
                    self.project
                    / "docs"
                    / "methodology"
                    / "fdd"
                    / "artifacts"
                    / f"{name}.md"
                )
                self.assertEqual(output.read_text(), marker + source.read_text().strip() + "\n")

        dsdm_project = Path(self.temp.name) / "dsdm-project"
        dsdm_project.mkdir()
        dsdm = self.run_aok(
            "methodology", "scaffold", str(dsdm_project), "--flow", "dsdm"
        )
        self.assertEqual(dsdm.returncode, 0, dsdm.stderr)
        dsdm_artifacts = json.loads(
            (ROOT / "methodologies" / "dsdm" / "artifacts.json").read_text()
        )["artifacts"]
        self.assertEqual(len(dsdm_artifacts), 15)
        for artifact in dsdm_artifacts:
            with self.subTest(template=artifact["id"]):
                template = artifact["template"]
                source = ROOT / "methodologies" / template["sourcePath"]
                output = dsdm_project / template["outputPath"]
                self.assertEqual(output.read_text(), marker + source.read_text().strip() + "\n")
        prl = (
            dsdm_project
            / "docs"
            / "methodology"
            / "dsdm"
            / "artifacts"
            / "prioritised-requirements-list.md"
        ).read_text()
        self.assertIn("Project MoSCoW", prl)
        self.assertNotIn("<action> <result>", prl)
        review = (
            dsdm_project
            / "docs"
            / "methodology"
            / "dsdm"
            / "artifacts"
            / "timebox-review-record.md"
        ).read_text()
        self.assertIn("Business Ambassador decision", review)
        self.assertNotIn("cumulative progress", review.lower())

        repeated = self.run_aok(
            "methodology", "scaffold", str(dsdm_project), "--flow", "dsdm", "--json"
        )
        self.assertEqual(repeated.returncode, 0, repeated.stderr)
        self.assertTrue(
            all(item["status"] == "unchanged" for item in json.loads(repeated.stdout)["files"])
        )

        aup_project = Path(self.temp.name) / "aup-project"
        aup_project.mkdir()
        aup = self.run_aok(
            "methodology", "scaffold", str(aup_project), "--flow", "aup"
        )
        self.assertEqual(aup.returncode, 0, aup.stderr)
        aup_artifacts = json.loads(
            (ROOT / "methodologies" / "aup" / "artifacts.json").read_text()
        )["artifacts"]
        self.assertEqual(len(aup_artifacts), 11)
        for artifact in aup_artifacts:
            with self.subTest(template=artifact["id"]):
                template = artifact["template"]
                source = ROOT / "methodologies" / template["sourcePath"]
                output = aup_project / template["outputPath"]
                self.assertEqual(output.read_text(), marker + source.read_text().strip() + "\n")
        requirements = (
            aup_project
            / "docs"
            / "methodology"
            / "aup"
            / "artifacts"
            / "requirements-model.md"
        ).read_text()
        self.assertIn("Use case", requirements)
        self.assertNotIn("MoSCoW", requirements)
        iteration_plan = (
            aup_project
            / "docs"
            / "methodology"
            / "aup"
            / "artifacts"
            / "iteration-plan.md"
        ).read_text()
        self.assertIn("Configuration Management", iteration_plan)
        self.assertIn("Environment", iteration_plan)
        self.assertNotIn("cumulative percentage", iteration_plan.lower())

        repeated_aup = self.run_aok(
            "methodology", "scaffold", str(aup_project), "--flow", "aup", "--json"
        )
        self.assertEqual(repeated_aup.returncode, 0, repeated_aup.stderr)
        self.assertTrue(
            all(
                item["status"] == "unchanged"
                for item in json.loads(repeated_aup.stdout)["files"]
            )
        )

        generic = (
            self.project
            / "docs"
            / "methodology"
            / "fdd"
            / "artifacts"
            / "feature-build.md"
        ).read_text()
        self.assertIn("Owners: `fdd-class-owner`", generic)
        self.assertIn("<!-- Complete this section for the project. -->", generic)

    def test_scaffold_accepts_only_scaffolded_flows(self) -> None:
        cases = [
            ("kanban", "only scaffolded flows"),
            ("am", "not a flow"),
            ("unknown", "Unknown methodology flow"),
        ]
        for flow, message in cases:
            with self.subTest(flow=flow):
                result = self.run_aok(
                    "methodology", "scaffold", str(self.project), "--flow", flow
                )
                self.assertEqual(result.returncode, 1)
                self.assertIn(message, result.stderr)
        self.assertEqual(list(self.project.iterdir()), [])

    def test_kit_validation_rejects_malformed_methodology_references(self) -> None:
        isolated_root = Path(self.temp.name) / "isolated-kit"
        (isolated_root / "scripts").mkdir(parents=True)
        shutil.copy2(AOK, isolated_root / "scripts" / "aok")
        shutil.copytree(ROOT / "methodologies", isolated_root / "methodologies")
        (isolated_root / "templates" / "agents").mkdir(parents=True)
        shutil.copy2(
            ROOT / "templates" / "agents" / "methodology.md",
            isolated_root / "templates" / "agents" / "methodology.md",
        )

        artifacts_path = isolated_root / "methodologies" / "fdd" / "artifacts.json"
        artifacts = json.loads(artifacts_path.read_text())
        artifacts["artifacts"][0]["owners"] = ["fdd-missing-role"]
        artifacts_path.write_text(json.dumps(artifacts))
        profile_path = isolated_root / "methodologies" / "profiles" / "fdd.json"
        profile = json.loads(profile_path.read_text())
        profile["gates"][0]["requiredArtifacts"] = ["fdd-missing-artifact"]
        profile_path.write_text(json.dumps(profile))
        contract_path = (
            isolated_root / "methodologies" / "fdd" / "feature-contract.json"
        )
        contract = json.loads(contract_path.read_text())
        contract["milestones"][1]["cumulative"] = 99
        contract["gateInputs"].remove("featureName")
        contract_path.write_text(json.dumps(contract))
        dsdm_contract_path = (
            isolated_root / "methodologies" / "dsdm" / "requirement-contract.json"
        )
        dsdm_contract = json.loads(dsdm_contract_path.read_text())
        dsdm_contract["timeboxing"]["checkpoints"][1]["order"] = 99
        dsdm_contract["gateInputs"].remove("projectMoscow")
        dsdm_contract_path.write_text(json.dumps(dsdm_contract))
        aup_contract_path = (
            isolated_root / "methodologies" / "aup" / "iteration-contract.json"
        )
        aup_contract = json.loads(aup_contract_path.read_text())
        aup_contract["milestones"][2]["gate"] = "wrong-gate"
        aup_contract["gateInputs"].remove("iterationId")
        aup_contract_path.write_text(json.dumps(aup_contract))

        loader = SourceFileLoader(
            "aok_malformed_methodology_test", str(isolated_root / "scripts" / "aok")
        )
        spec = importlib.util.spec_from_loader(loader.name, loader)
        assert spec is not None
        module = importlib.util.module_from_spec(spec)
        loader.exec_module(module)
        errors: list[str] = []
        module.validate_methodologies(errors)
        self.assertTrue(
            any("unknown role 'fdd-missing-role'" in error for error in errors), errors
        )
        self.assertTrue(
            any("unknown artifact 'fdd-missing-artifact'" in error for error in errors),
            errors,
        )
        self.assertTrue(
            any("invalid cumulative progress" in error for error in errors), errors
        )
        self.assertTrue(
            any("unknown FDD input 'featureName'" in error for error in errors), errors
        )
        self.assertTrue(
            any("checkpoint ordering is invalid" in error for error in errors), errors
        )
        self.assertTrue(
            any("unknown DSDM input 'projectMoscow'" in error for error in errors), errors
        )
        self.assertTrue(
            any("lifecycle milestones are invalid" in error for error in errors), errors
        )
        self.assertTrue(
            any("unknown AUP input 'iterationId'" in error for error in errors), errors
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
