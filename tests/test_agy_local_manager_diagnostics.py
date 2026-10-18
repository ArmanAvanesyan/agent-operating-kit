import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "agy_local_manager_probe", ROOT / "scripts/native-loading/probe_agy_local_manager.py")
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


class SyntheticLoadDiagnosticTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.log = self.root / "home/.gemini/antigravity-cli/cli.log"
        self.log.parent.mkdir(parents=True)

    def test_known_synthetic_log_returns_redacted_bounded_diagnostics(self):
        self.log.write_text(
            "agent aok-probe loaded from agent.md\n"
            "custom agent description: Synthetic AGY adapter discovery fixture\n"
            "ordinary startup event\n", encoding="utf-8")

        rows = probe.synthetic_load_diagnostics(self.root)

        self.assertEqual(len(rows), 2)
        self.assertTrue(any("aok-probe" in row for row in rows))
        self.assertTrue(any("agent.md" in row for row in rows))
        self.assertNotIn(str(self.root), json.dumps(rows))

    def test_exact_log_symlink_to_outside_is_never_followed(self):
        outside = self.root.parent / (self.root.name + "-outside.log")
        outside.write_text("aok-probe agent.md", encoding="utf-8")
        self.addCleanup(lambda: outside.unlink(missing_ok=True))
        self.log.symlink_to(outside)

        self.assertEqual(probe.synthetic_load_diagnostics(self.root), [])

    def test_diagnostics_are_capped_at_sixteen_matching_rows(self):
        self.log.write_text("".join(f"aok-probe loaded {i}\n" for i in range(30)),
                            encoding="utf-8")

        rows = probe.synthetic_load_diagnostics(self.root)

        self.assertEqual(len(rows), 16)

    def test_unknown_files_are_not_scanned(self):
        (self.log.parent / "other.log").write_text("aok-probe agent.md", encoding="utf-8")

        self.assertEqual(probe.synthetic_load_diagnostics(self.root), [])

    def test_safe_manager_state_preserves_sanitized_native_flags(self):
        source = {
            "managerSectionObserved": True,
            "nameObserved": True,
            "descriptionObserved": True,
            "parseErrorObserved": False,
            "noAgentsLabelObserved": False,
            "authBarrierObserved": False,
            "errorObserved": False,
            "errorCategories": [],
            "text": "do not retain this input",
            "account": "private@example.test",
        }

        safe = probe.safe_manager_state(source)

        for key in ("managerSectionObserved", "nameObserved", "descriptionObserved",
                    "parseErrorObserved", "noAgentsLabelObserved"):
            self.assertEqual(safe[key], source[key])
        encoded = json.dumps(safe)
        self.assertNotIn("private@example.test", encoded)
        self.assertNotIn("do not retain", encoded)


if __name__ == "__main__":
    unittest.main()
