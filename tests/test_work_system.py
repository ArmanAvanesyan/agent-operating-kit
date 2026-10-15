from __future__ import annotations

import json
import os
import stat
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
AOK = ROOT / "scripts" / "aok"

FAKE_TOOL = r"""#!/usr/bin/python3
import json
import os
import sys
from pathlib import Path

data = json.loads(os.environ.get("AOK_FAKE_DATA", "{}"))
tool = Path(sys.argv[0]).name
args = sys.argv[1:]
log = os.environ.get("AOK_FAKE_LOG")
if log:
    with open(log, "a") as handle:
        handle.write(json.dumps([tool] + args) + "\n")

def output(value):
    if value == "__MALFORMED__":
        print("{broken")
    else:
        print(json.dumps(value))

if tool == "gh":
    if args[:2] == ["auth", "status"]:
        sys.exit(int(data.get("ghAuthExit", 0)))
    if args[:2] == ["project", "view"]:
        output(data["github"]["project"])
    elif args[:2] == ["project", "list"]:
        output({"projects": data["github"].get("projects", [])})
    elif args[:2] == ["project", "field-list"]:
        output({"fields": data["github"]["fields"]})
    elif args[:2] == ["api", "graphql"]:
        output({"data": {"node": {"fields": {"nodes": data["github"].get("fieldTypes", [])}}}})
    else:
        print("unexpected gh command", file=sys.stderr)
        sys.exit(91)
elif tool == "az":
    if args[:3] == ["devops", "project", "show"]:
        output(data["azure"]["project"])
    elif args[:3] == ["devops", "team", "list"]:
        output(data["azure"]["teams"])
    elif args[:2] == ["devops", "invoke"]:
        resource = args[args.index("--resource") + 1]
        output(data["azure"]["resources"][resource])
    else:
        print("unexpected az command", file=sys.stderr)
        sys.exit(92)
else:
    sys.exit(93)
"""


def github_fixture() -> dict:
    select = {
        "Status": ["Inbox", "Ready", "In Progress", "QA", "Review", "Blocked", "Done"],
        "Phase": ["Intake", "Planning", "Build", "QA", "Review", "Release", "Operate"],
        "Priority": ["Critical", "High", "Medium", "Low"],
    }
    fields = []
    field_types = []
    index = 1
    for name, options in select.items():
        fields.append(
            {
                "id": f"F{index}",
                "name": name,
                "type": "ProjectV2SingleSelectField",
                "options": [{"id": f"{name}-{value}", "name": value} for value in options],
            }
        )
        index += 1
    for name in [
        "Primary Agent",
        "QA Agent",
        "Target Repo",
        "PR URL",
        "Evidence URL",
        "Blocker Reason",
        "Next Handoff",
    ]:
        field_id = f"F{index}"
        fields.append({"id": field_id, "name": name, "type": "ProjectV2Field"})
        field_types.append({"id": field_id, "name": name, "dataType": "TEXT"})
        index += 1
    fields.append({"id": "F11", "name": "Attempt", "type": "ProjectV2Field"})
    field_types.append({"id": "F11", "name": "Attempt", "dataType": "NUMBER"})
    return {
        "github": {
            "project": {
                "id": "PVT_1",
                "number": 12,
                "title": "Agent Work",
                "url": "https://github.com/orgs/acme/projects/12",
            },
            "projects": [],
            "fields": fields,
            "fieldTypes": field_types,
        }
    }


def azure_fixture(customization: str = "inherited") -> dict:
    fields = []
    for name, values in {
        "Phase": ["Intake", "Planning", "Build", "QA", "Review", "Release", "Operate"],
        "Priority": ["Critical", "High", "Medium", "Low"],
    }.items():
        fields.append(
            {
                "name": name,
                "referenceName": f"Custom.{name}",
                "type": "picklistString",
                "allowedValues": values,
            }
        )
    for name in [
        "Primary Agent",
        "QA Agent",
        "Target Repo",
        "PR URL",
        "Evidence URL",
        "Blocker Reason",
        "Next Handoff",
    ]:
        fields.append({"name": name, "referenceName": f"Custom.{name.replace(' ', '')}", "type": "string"})
    fields.append({"name": "Attempt", "referenceName": "Custom.Attempt", "type": "integer"})
    categories = {
        "Inbox": "Proposed",
        "Ready": "Proposed",
        "In Progress": "InProgress",
        "QA": "InProgress",
        "Review": "InProgress",
        "Blocked": "InProgress",
        "Done": "Completed",
    }
    states = [
        {"id": f"S-{name}", "name": name, "stateCategory": category}
        for name, category in categories.items()
    ]
    columns = [{"id": f"C-{name}", "name": name} for name in categories]
    return {
        "azure": {
            "project": {
                "id": "PROJECT-1",
                "name": "Platform",
                "url": "https://dev.azure.com/acme/Platform",
                "defaultTeam": {"name": "Platform Team"},
                "capabilities": {
                    "processTemplate": {
                        "templateTypeId": "PROCESS-1",
                        "templateName": "AOK Agile",
                    }
                },
            },
            "teams": [{"id": "TEAM-1", "name": "Platform Team"}],
            "resources": {
                "processes": {
                    "id": "PROCESS-1",
                    "name": "AOK Agile",
                    "customizationType": customization,
                },
                "workItemTypes": [
                    {
                        "name": "User Story",
                        "referenceName": "AOKAgile.UserStory",
                    },
                    {
                        "name": "Bug",
                        "referenceName": "AOKAgile.Bug",
                    },
                ],
                "fields": fields,
                "states": states,
                "boards": [{"id": "BOARD-1", "name": "Stories"}],
                "columns": columns,
            },
        }
    }


class WorkSystemTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temp = tempfile.TemporaryDirectory()
        self.base = Path(self.temp.name)
        self.bin = self.base / "bin"
        self.bin.mkdir()
        for name in ("gh", "az"):
            path = self.bin / name
            path.write_text(FAKE_TOOL)
            path.chmod(path.stat().st_mode | stat.S_IXUSR)
        self.project = self.base / "project"
        self.project.mkdir()
        self.log = self.base / "calls.jsonl"

    def tearDown(self) -> None:
        self.temp.cleanup()

    def run_aok(self, *args: str, data: dict | None = None, path: str | None = None) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        env["PATH"] = path if path is not None else f"{self.bin}:{env['PATH']}"
        env["AOK_FAKE_DATA"] = json.dumps(data or {})
        env["AOK_FAKE_LOG"] = str(self.log)
        return subprocess.run(
            [sys.executable, str(AOK), *args],
            text=True,
            capture_output=True,
            env=env,
        )

    def test_github_conformant_plan_writes_and_validates_binding(self) -> None:
        data = github_fixture()
        result = self.run_aok(
            "work-system",
            "setup",
            "github",
            "--owner",
            "acme",
            "--project-number",
            "12",
            "--path",
            str(self.project),
            "--write-binding",
            "--json",
            data=data,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertTrue(report["conformant"])
        manifest = json.loads((self.project / ".aok" / "project.json").read_text())
        binding = manifest["workSystem"]
        self.assertEqual(binding["fields"]["Attempt"]["type"], "NUMBER")
        self.assertNotIn("token", json.dumps(binding).lower())

        validated = self.run_aok(
            "work-system", "validate", str(self.project), "--json", data=data
        )
        self.assertEqual(validated.returncode, 0, validated.stderr)
        self.assertTrue(json.loads(validated.stdout)["conformant"])
        calls = self.log.read_text()
        self.assertNotIn("field-create", calls)
        self.assertNotIn("project create", calls)

    def test_github_missing_and_wrong_fields_refuse_binding(self) -> None:
        data = github_fixture()
        data["github"]["fields"] = [
            field for field in data["github"]["fields"] if field["name"] != "Evidence URL"
        ]
        attempt = next(field for field in data["github"]["fields"] if field["name"] == "Attempt")
        attempt["dataType"] = "TEXT"
        attempt["type"] = "TEXT"
        result = self.run_aok(
            "work-system",
            "setup",
            "github",
            "--owner",
            "acme",
            "--project-number",
            "12",
            "--path",
            str(self.project),
            "--write-binding",
            "--json",
            data=data,
        )
        self.assertEqual(result.returncode, 1)
        report = json.loads(result.stdout)
        self.assertFalse(report["conformant"])
        self.assertFalse((self.project / ".aok" / "project.json").exists())
        self.assertTrue(
            any("field-create" in item and "Evidence URL" in item for item in report["futureOperations"])
        )

    def test_github_duplicate_title_is_mismatched(self) -> None:
        data = github_fixture()
        data["github"]["projects"] = [
            {"id": "P1", "number": 1, "title": "Agent Work"},
            {"id": "P2", "number": 2, "title": "Agent Work"},
        ]
        result = self.run_aok(
            "work-system",
            "setup",
            "github",
            "--owner",
            "acme",
            "--project-title",
            "Agent Work",
            "--json",
            data=data,
        )
        self.assertEqual(result.returncode, 0)
        report = json.loads(result.stdout)
        self.assertFalse(report["conformant"])
        self.assertEqual(report["checks"][0]["status"], "mismatched")

    def test_github_duplicate_field_and_option_are_mismatched(self) -> None:
        data = github_fixture()
        status = next(field for field in data["github"]["fields"] if field["name"] == "Status")
        status["options"].append(dict(status["options"][0]))
        priority = next(field for field in data["github"]["fields"] if field["name"] == "Priority")
        data["github"]["fields"].append(dict(priority))
        first = self.run_aok(
            "work-system",
            "setup",
            "github",
            "--owner",
            "acme",
            "--project-number",
            "12",
            "--json",
            data=data,
        )
        second = self.run_aok(
            "work-system",
            "setup",
            "github",
            "--owner",
            "acme",
            "--project-number",
            "12",
            "--json",
            data=data,
        )
        self.assertEqual(first.returncode, 0)
        self.assertEqual(first.stdout, second.stdout)
        checks = json.loads(first.stdout)["checks"]
        self.assertTrue(any(item["name"] == "field:Status" and item["status"] == "mismatched" for item in checks))
        self.assertTrue(any(item["name"] == "field:Priority" and item["status"] == "mismatched" for item in checks))

    def test_github_auth_failure_is_reported(self) -> None:
        data = github_fixture()
        data["ghAuthExit"] = 1
        result = self.run_aok(
            "work-system",
            "setup",
            "github",
            "--owner",
            "acme",
            "--project-number",
            "12",
            data=data,
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("Command failed: gh auth status", result.stderr)

    def test_malformed_provider_json_fails_closed(self) -> None:
        data = github_fixture()
        data["github"]["project"] = "__MALFORMED__"
        result = self.run_aok(
            "work-system",
            "setup",
            "github",
            "--owner",
            "acme",
            "--project-number",
            "12",
            data=data,
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("malformed JSON", result.stderr)

    def test_missing_tool_is_reported(self) -> None:
        empty_bin = self.base / "empty-bin"
        empty_bin.mkdir()
        result = self.run_aok(
            "work-system",
            "setup",
            "github",
            "--owner",
            "acme",
            "--project-number",
            "12",
            path=str(empty_bin),
        )
        self.assertEqual(result.returncode, 1)
        self.assertIn("Required command is not installed: gh", result.stderr)

    def test_project_init_preserves_binding(self) -> None:
        manifest = self.project / ".aok" / "project.json"
        manifest.parent.mkdir()
        binding = {"provider": "github", "marker": "keep"}
        manifest.write_text(json.dumps({"workSystem": binding}))
        result = self.run_aok("project", "init", str(self.project))
        self.assertEqual(result.returncode, 0, result.stderr)
        refreshed = json.loads(manifest.read_text())
        self.assertEqual(refreshed["workSystem"], binding)

    def test_validate_detects_stale_binding_ids(self) -> None:
        data = github_fixture()
        created = self.run_aok(
            "work-system",
            "setup",
            "github",
            "--owner",
            "acme",
            "--project-number",
            "12",
            "--path",
            str(self.project),
            "--write-binding",
            data=data,
        )
        self.assertEqual(created.returncode, 0, created.stderr)
        manifest_path = self.project / ".aok" / "project.json"
        manifest = json.loads(manifest_path.read_text())
        manifest["workSystem"]["projectId"] = "STALE"
        manifest_path.write_text(json.dumps(manifest))
        result = self.run_aok(
            "work-system", "validate", str(self.project), "--json", data=data
        )
        self.assertEqual(result.returncode, 1)
        checks = json.loads(result.stdout)["checks"]
        self.assertEqual(checks[-1]["name"], "binding")
        self.assertEqual(checks[-1]["status"], "mismatched")

    def test_azure_conformant_plan_writes_and_validates_binding(self) -> None:
        data = azure_fixture()
        result = self.run_aok(
            "work-system",
            "setup",
            "azure-devops",
            "--organization",
            "https://dev.azure.com/acme",
            "--project",
            "Platform",
            "--path",
            str(self.project),
            "--write-binding",
            "--json",
            data=data,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        report = json.loads(result.stdout)
        self.assertTrue(report["conformant"])
        binding = json.loads((self.project / ".aok" / "project.json").read_text())["workSystem"]
        self.assertEqual(binding["fields"]["Status"]["referenceName"], "System.State")

        validated = self.run_aok(
            "work-system", "validate", str(self.project), "--json", data=data
        )
        self.assertEqual(validated.returncode, 0, validated.stderr)
        self.assertTrue(json.loads(validated.stdout)["conformant"])
        for call in self.log.read_text().splitlines():
            argv = json.loads(call)
            if argv[:3] == ["az", "devops", "invoke"]:
                self.assertIn("GET", argv)

    def test_azure_system_process_and_missing_column_are_nonconformant(self) -> None:
        data = azure_fixture("system")
        data["azure"]["resources"]["columns"].pop()
        data["azure"]["resources"]["states"].pop()
        result = self.run_aok(
            "work-system",
            "setup",
            "azure-devops",
            "--organization",
            "https://dev.azure.com/acme",
            "--project",
            "Platform",
            "--work-item-type",
            "AOKAgile.UserStory",
            "--json",
            data=data,
        )
        self.assertEqual(result.returncode, 0)
        report = json.loads(result.stdout)
        self.assertFalse(report["conformant"])
        details = "\n".join(check["detail"] for check in report["checks"])
        self.assertIn("inherited/custom process is required", details)
        self.assertIn("ensure Azure board column", "\n".join(report["futureOperations"]))
        self.assertIn("ensure Azure state", "\n".join(report["futureOperations"]))


if __name__ == "__main__":
    unittest.main()
