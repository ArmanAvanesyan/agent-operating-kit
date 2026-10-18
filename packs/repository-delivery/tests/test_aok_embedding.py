"""Embedding boundaries; no credentials, active worktrees or runtime DBs."""
import builtins
import hashlib
import io
import json
from pathlib import Path
import unittest
from unittest.mock import patch
from repo_delivery import cli, core, adapters, github


class EmbeddingTests(unittest.TestCase):
    def test_historical_schema_is_compatible_with_canonical_owner(self):
        kit = Path(__file__).resolve().parents[3]
        canonical = kit / "schemas/task-packet.schema.json"
        if not canonical.exists():
            self.skipTest("standalone pack: canonical schema not distributed here")
        frozen = Path(__file__).parent / "fixtures/aok/task-packet.schema.json"
        if not frozen.exists():
            frozen = next((Path(__file__).parent / "fixtures").rglob("task-packet.schema.json"))
        self.assertEqual(json.loads(canonical.read_bytes()), json.loads(frozen.read_bytes()))

    def test_missing_git_is_actionable_even_for_optional_query(self):
        with patch("repo_delivery.core.subprocess.run", side_effect=FileNotFoundError):
            with self.assertRaisesRegex(core.DeliveryError, "install Git"):
                core.git("/unused", "status", optional=True)

    def test_missing_gh_is_confined_to_github(self):
        with patch("repo_delivery.github.subprocess.run", side_effect=FileNotFoundError):
            with self.assertRaisesRegex(core.DeliveryError, "install gh"):
                github.inspect_issue("fixture/repo", 1)

    def test_missing_optional_dependency_is_actionable(self):
        original = builtins.__import__
        def isolated(name, *args, **kwargs):
            if name == "jsonschema":
                raise ImportError("deliberately unavailable")
            return original(name, *args, **kwargs)
        with patch("builtins.__import__", isolated):
            with self.assertRaisesRegex(core.DeliveryError, "AOK_DELIVERY_PYTHON"):
                adapters.consume_aok("unused", "unused", "a" * 64)

    def test_windows_fails_before_preservation_or_execution(self):
        stderr = io.StringIO()
        with patch("sys.platform", "win32"), patch("sys.stderr", stderr), patch.object(cli, "inspect") as inspect:
            self.assertEqual(cli.main(["inspect", "--repo", "/unused"]), 1)
            inspect.assert_not_called()
        self.assertIn("private POSIX preservation", stderr.getvalue())

    def test_unsupported_optional_validator_version_is_rejected(self):
        with patch("importlib.metadata.version", return_value="4.25.0"):
            with self.assertRaisesRegex(core.DeliveryError, "jsonschema>=4.26,<5"):
                adapters.consume_aok("unused", "unused", "a" * 64)

    def test_all_pack_manifests_match_owned_schema(self):
        from jsonschema import Draft202012Validator
        kit = Path(__file__).resolve().parents[3]
        schema = kit / "schemas/pack.schema.json"
        if not schema.exists():
            self.skipTest("standalone pack: owner schema unavailable")
        validator = Draft202012Validator(json.loads(schema.read_text()))
        for manifest in (kit / "packs").glob("*/pack.json"):
            with self.subTest(pack=manifest.parent.name):
                validator.validate(json.loads(manifest.read_text()))
