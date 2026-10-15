from __future__ import annotations

import importlib.util
import stat
import subprocess
import sys
import tempfile
import unittest
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]


class ReleaseTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        spec = importlib.util.spec_from_file_location(
            "aok_release_test", ROOT / "scripts/release/build.py"
        )
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        cls.builder = module

    def test_packaged_kit_passes_validation_after_extraction(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory)
            archive_path = target / "kit.zip"
            self.builder.write_zip(
                archive_path, "kit", self.builder.iter_files(self.builder.KIT_PATHS)
            )
            extracted = target / "extracted"
            with zipfile.ZipFile(archive_path) as archive:
                archive.extractall(extracted)
                # Python extraction omits permissions; restore the modes a ZIP
                # installer uses, including executable Codex setup helpers.
                for item in archive.infolist():
                    (extracted / item.filename).chmod(
                        stat.S_IMODE(item.external_attr >> 16)
                    )
            result = subprocess.run(
                [sys.executable, str(extracted / "kit/scripts/aok"), "validate"],
                capture_output=True,
                text=True,
            )
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)

    def test_all_catalog_archives_include_work_item_templates(self) -> None:
        for inputs in (
            self.builder.KIT_PATHS,
            self.builder.CODEX_PLUGIN_PATHS,
            self.builder.PACK_PATHS,
        ):
            with self.subTest(inputs=inputs):
                relative = {
                    path.relative_to(ROOT).as_posix()
                    for path in self.builder.iter_files(inputs)
                }
                self.assertIn("work-items/catalog.json", relative)
                self.assertEqual(
                    len([
                        name for name in relative
                        if name.startswith("work-items/templates/")
                        and name.endswith(".md")
                    ]),
                    19,
                )
                self.assertIn("methodologies/aup/iteration-contract.json", relative)


if __name__ == "__main__":
    unittest.main()
