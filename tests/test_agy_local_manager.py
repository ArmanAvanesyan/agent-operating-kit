import importlib.util
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "agy_local_manager_probe", ROOT / "scripts/native-loading/probe_agy_local_manager.py")
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


class AgyLocalManagerTests(unittest.TestCase):
    @staticmethod
    def terms_state(**changes):
        state = {"terminalAmbiguityObserved": False, "authBarrierObserved": False,
                 "localPromptObserved": False, "errorObserved": False,
                 "colorSchemeScreenObserved": False}
        state.update(changes)
        return state

    @staticmethod
    def terms_text(checkbox, done, footer="enter Confirm"):
        return ("Terms of Service & Data Use\n"
                "Google to collect and use my Interactions data,\n"
                f"{checkbox}\n{done}\n{footer}")

    def test_terms_actions_require_exact_focus_and_checkbox_state(self):
        state = self.terms_state()
        checked_focused = self.terms_text(
            "> [x] Yes, I agree to help improve Antigravity CLI by allowing",
            "[Previous]    Done [Done]", "enter Toggle")
        self.assertEqual(probe.onboarding_action(checked_focused, state, Path("/unused")),
                         ("disable-optional-sharing", b"\r"))

        unchecked_done = self.terms_text(
            "[ ] Yes, I agree to help improve Antigravity CLI by allowing",
            "[Previous]    >  Done", "enter Confirm")
        self.assertEqual(probe.onboarding_action(unchecked_done, state, Path("/unused")),
                         ("accept-terms-sharing-disabled", b"\r"))

        checked_done = self.terms_text(
            "[x] Yes, I agree to help improve Antigravity CLI by allowing",
            "[Previous]    >  Done", "enter Confirm")
        self.assertIsNone(probe.onboarding_action(checked_done, state, Path("/unused")))

        focused_unchecked = self.terms_text(
            "> [ ] Yes, I agree to help improve Antigravity CLI by allowing",
            "[Previous]    Done [Done]", "enter Toggle")
        self.assertEqual(probe.onboarding_action(focused_unchecked, state, Path("/unused")),
                         ("navigate-to-previous", b"\t"))

    def test_duplicate_or_unrecognized_checkbox_screens_never_act(self):
        state = self.terms_state()
        duplicate = self.terms_text(
            "> [ ] Yes, I agree to help improve Antigravity CLI by allowing\n"
            "[x] Yes, I agree to help improve Antigravity CLI by allowing",
            "[Previous]    >  Done")
        self.assertIsNone(probe.onboarding_action(duplicate, state, Path("/unused")))
        for text in (
            "Terms of Service & Data Use\nGoogle to collect and use my Interactions data,\n"
            "> [x] Yes, I agree to help improve Antigravity CLI by allowing",
            "Terms of Service & Data Use\nGoogle to collect and use my Interactions data,\n"
            "[ ] Yes, I agree to help improve Antigravity CLI by allowing\nDone",
            "Unknown onboarding screen",
        ):
            with self.subTest(text=text):
                self.assertIsNone(probe.onboarding_action(text, state, Path("/unused")))

    def test_terms_actions_stop_on_errors_auth_ambiguity_or_prompt(self):
        text = self.terms_text(
            "> [x] Yes, I agree to help improve Antigravity CLI by allowing",
            "[Previous]   Done [Done]", "enter Toggle")
        for change in ({"errorObserved": True}, {"authBarrierObserved": True},
                       {"terminalAmbiguityObserved": True}, {"localPromptObserved": True}):
            with self.subTest(change=change):
                self.assertIsNone(probe.onboarding_action(text, self.terms_state(**change), Path("/unused")))

    def test_trust_action_requires_exact_owned_canonical_root_and_focus(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            root.chmod(0o700)
            state = {"terminalAmbiguityObserved": False, "authBarrierObserved": False,
                     "localPromptObserved": False, "errorObserved": False,
                     "trustPromptObserved": True, "termsAcceptancePromptObserved": False}
            text = (f"Accessing workspace:\n{root}\n"
                    "> Yes, I trust this folder\nenter Confirm")
            self.assertEqual(probe.onboarding_action(text, state, root),
                             ("trust-owned-synthetic-root", b"\r"))

            negatives = (
                text.replace(str(root), str(root) + "-other"),
                text.replace("Accessing workspace:\n", ""),
                text.replace("> Yes, I trust", "  Yes, I trust"),
                text.replace("enter Confirm", "Enter Toggle"),
            )
            for bad in negatives:
                with self.subTest(bad=bad):
                    self.assertIsNone(probe.onboarding_action(bad, state, root))

            root.chmod(0o755)
            self.assertIsNone(probe.onboarding_action(text, state, root))

    def test_trust_action_stops_on_unrelated_screens_and_blocking_state(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            root.chmod(0o700)
            text = (f"Accessing workspace:\n{root}\n"
                    "> Yes, I trust this folder\nenter Confirm")
            state = {"terminalAmbiguityObserved": False, "authBarrierObserved": False,
                     "localPromptObserved": False, "errorObserved": False,
                     "trustPromptObserved": True, "termsAcceptancePromptObserved": False}
            for change in ({"errorObserved": True}, {"authBarrierObserved": True},
                           {"terminalAmbiguityObserved": True}, {"localPromptObserved": True},
                           {"termsAcceptancePromptObserved": True}):
                with self.subTest(change=change):
                    self.assertIsNone(probe.onboarding_action(text, {**state, **change}, root))
            self.assertIsNone(probe.onboarding_action(text, state, root / "different"))

    def test_case_capture_only_sends_agents_at_stable_verified_local_prompt(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            audit_path = root / "audit.jsonl"
            separator = "─" * 160
            prompt = "\r\n".join((
                "█ Antigravity CLI 1.4.2", "█ Gemini API key", "█ Gemini 2.5 Pro",
                "", "", "", "", separator, ">", separator,
                "? for shortcuts   Gemini   Ctrl+K",
            )).encode()
            clock = {"value": 0.0}
            select_calls = {"count": 0}
            writes = []

            def monotonic():
                clock["value"] += 0.2
                return clock["value"]

            def select_ready(*_args):
                select_calls["count"] += 1
                if select_calls["count"] == 1:
                    return [91], [], []
                return [], [], []

            with mock.patch.object(probe.select, "select", side_effect=select_ready), \
                 mock.patch.object(probe.os, "read", return_value=prompt), \
                 mock.patch.object(probe.os, "write", side_effect=lambda _fd, data: writes.append(data) or len(data)), \
                 mock.patch.object(probe.time, "monotonic", side_effect=monotonic):
                capture = probe.CaseCapture()
                state, _ = capture.capture(91, 5.0, audit_path=audit_path, case_root=root)

            self.assertTrue(state["localPromptObserved"])
            self.assertTrue(state["framedNativePromptObserved"])
            self.assertTrue(capture.command_sent)
            self.assertEqual(writes, [b"/agents\r"])
            self.assertEqual(capture.actions, [])
            self.assertIsNotNone(capture.prompt)

    def test_evidence_output_reserves_only_new_private_file_under_canonical_repo(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            repo = root / "repo"
            report_dir = repo / "reports"
            report_dir.mkdir(parents=True)
            output = report_dir / "evidence.json"
            descriptor = probe.open_evidence_output(output, repo=repo)
            try:
                info = os.fstat(descriptor)
                self.assertEqual(info.st_mode & 0o777, 0o600)
                os.write(descriptor, b"private report")
            finally:
                os.close(descriptor)
            self.assertEqual(output.read_bytes(), b"private report")

    def test_evidence_output_rejects_escape_and_symlink_paths_without_writes(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            repo = root / "repo"
            report_dir = repo / "reports"
            report_dir.mkdir(parents=True)
            outside = root / "outside"
            outside.mkdir()
            victim = outside / "victim.json"
            victim.write_text("unchanged")

            for path in (outside / "new.json", repo / ".." / "outside" / "new.json"):
                with self.subTest(path=path), self.assertRaises((ValueError, OSError)):
                    probe.open_evidence_output(path, repo=repo)
            self.assertEqual(victim.read_text(), "unchanged")
            self.assertFalse((outside / "new.json").exists())

            (repo / "link-parent").symlink_to(outside, target_is_directory=True)
            with self.assertRaises((ValueError, OSError)):
                probe.open_evidence_output(repo / "link-parent" / "new.json", repo=repo)
            self.assertFalse((outside / "new.json").exists())

            dangling = report_dir / "dangling.json"
            dangling.symlink_to(outside / "not-created.json")
            with self.assertRaises((ValueError, OSError)):
                probe.open_evidence_output(dangling, repo=repo)
            self.assertTrue(dangling.is_symlink())
            self.assertFalse((outside / "not-created.json").exists())

    def test_evidence_output_does_not_overwrite_existing_file(self):
        with tempfile.TemporaryDirectory() as temp:
            repo = Path(temp).resolve() / "repo"
            report_dir = repo / "reports"
            report_dir.mkdir(parents=True)
            existing = report_dir / "evidence.json"
            existing.write_text("preserve this")
            existing.chmod(0o640)
            before_mode = existing.stat().st_mode & 0o777
            with self.assertRaises((ValueError, OSError)):
                probe.open_evidence_output(existing, repo=repo)
            self.assertEqual(existing.read_text(), "preserve this")
            self.assertEqual(existing.stat().st_mode & 0o777, before_mode)


if __name__ == "__main__":
    unittest.main()
