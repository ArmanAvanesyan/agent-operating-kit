import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "agy_parser_diagnostics", ROOT / "scripts/native-loading/agy_parser_diagnostics.py")
diagnostics = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(diagnostics)


class AgyParserDiagnosticTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.root.chmod(0o700)
        self.log = self.root / diagnostics.LOG_RELATIVE_PATH
        self.fixture_path = str(self.root / diagnostics.FIXTURE_RELATIVE_PATH)

    def write_log(self, text):
        self.log.write_text(text, encoding="utf-8")
        self.log.chmod(0o600)

    def test_exact_fixture_path_and_parser_marker_on_same_row_attributes(self):
        self.write_log(f"ERROR {self.fixture_path}: failed to unmarshal frontmatter YAML\n")

        result = diagnostics.classify(self.root)

        self.assertEqual(result["status"], "available")
        self.assertTrue(result["fixtureAttributed"])
        self.assertEqual(result["diagnosticRows"], 1)
        self.assertGreater(result["bytesRead"], 0)
        self.assertEqual(result["rowsRead"], 1)
        self.assertEqual(result["contextCounts"]["frontmatter"], 1)
        self.assertEqual(result["markerCounts"]["frontmatter_yaml_unmarshal"], 1)

    def test_cross_row_and_other_fixture_diagnostics_do_not_attribute(self):
        self.write_log(
            f"{self.fixture_path}\n"
            "failed to decode frontmatter\n"
            f"/tmp/other/agent.md: failed to parse frontmatter YAML\n")

        result = diagnostics.classify(self.root)

        self.assertFalse(result["fixtureAttributed"])
        self.assertEqual(result["diagnosticRows"], 2)
        self.assertEqual(result["markerCounts"]["frontmatter_decode"], 1)
        self.assertEqual(result["markerCounts"]["frontmatter_yaml_parse"], 1)

    def test_log_mutation_during_read_is_unavailable(self):
        self.write_log(f"{self.fixture_path}: failed to parse frontmatter\n")
        original_read = os.read
        mutated = False
        def read(fd, size):
            nonlocal mutated
            block = original_read(fd, size)
            if not mutated:
                mutated = True
                with self.log.open('a') as out:
                    out.write('new native log row\n')
            return block
        with mock.patch.object(diagnostics.os, 'read', side_effect=read):
            result = diagnostics.classify(self.root)
        self.assertEqual(result['status'], 'unavailable')
        self.assertFalse(result['fixtureAttributed'])

    def test_fixture_path_substrings_do_not_attribute(self):
        for wrong in (self.fixture_path + '.backup', self.fixture_path + '2',
                      '/prefix' + self.fixture_path):
            with self.subTest(path=wrong):
                self.write_log(f"{wrong}: failed to unmarshal frontmatter YAML\n")
                self.assertFalse(diagnostics.classify(self.root)['fixtureAttributed'])

    def test_yaml_syntax_marker_is_counted_but_not_fixture_attributed_without_path(self):
        self.write_log("yaml: line 17: did not find expected key\n")

        result = diagnostics.classify(self.root)

        self.assertEqual(result["markerCounts"]["yaml_line_error"], 1)
        self.assertFalse(result["fixtureAttributed"])

    def test_fixed_native_parser_log_is_read_and_missing_log_is_unavailable(self):
        self.assertEqual(diagnostics.classify(self.root)["status"], "unavailable")
        self.write_log(f"{self.fixture_path}: failed to parse frontmatter\n")

        result = diagnostics.classify(self.root)

        self.assertEqual(result["status"], "available")
        self.assertTrue(result["fixtureAttributed"])

    def test_log_symlink_is_unavailable_even_when_target_is_inside_root(self):
        actual = self.root / "alternate.log"
        actual.write_text(f"{self.fixture_path}: failed to parse frontmatter\n", encoding="utf-8")
        actual.chmod(0o600)
        self.log.symlink_to(actual)

        self.assertEqual(diagnostics.classify(self.root)["status"], "unavailable")

    def test_outside_root_symlink_is_unavailable(self):
        outside = Path(self.temp.name).parent / (Path(self.temp.name).name + "-outside")
        outside.write_text(f"{self.fixture_path}: failed to parse frontmatter\n", encoding="utf-8")
        self.addCleanup(lambda: outside.unlink(missing_ok=True))
        self.log.symlink_to(outside)

        self.assertEqual(diagnostics.classify(self.root)["status"], "unavailable")

    def test_fifo_and_hardlinked_log_are_unavailable(self):
        os.mkfifo(self.log)
        self.assertEqual(diagnostics.classify(self.root)["status"], "unavailable")
        self.log.unlink()
        self.write_log("failed to parse frontmatter\n")
        os.link(self.log, self.root / "second-link")

        self.assertEqual(diagnostics.classify(self.root)["status"], "unavailable")

    def test_truncation_returns_only_stable_fields_without_log_content(self):
        secret = "synthetic-private-secret-value"
        self.write_log("ordinary " + secret + "\n" + ("x" * diagnostics.MAX_LOG_BYTES) +
                       f"\n{self.fixture_path}: failed to parse frontmatter\n")

        result = diagnostics.classify(self.root)
        encoded = json.dumps(result)

        self.assertEqual(result["status"], "truncated")
        self.assertTrue(result["truncated"])
        self.assertFalse(result["fixtureAttributed"])
        self.assertNotIn(secret, encoded)
        self.assertNotIn(str(self.root), encoded)
        self.assertTrue(set(result) == {"status", "truncated", "fixtureAttributed",
                                        "diagnosticRows", "markerCounts", "bytesRead", "rowsRead", "contextCounts"})

    def test_non_private_or_noncanonical_root_is_unavailable(self):
        self.write_log(f"{self.fixture_path}: failed to parse frontmatter\n")
        self.root.chmod(0o755)

        self.assertEqual(diagnostics.classify(self.root)["status"], "unavailable")


if __name__ == "__main__":
    unittest.main()
