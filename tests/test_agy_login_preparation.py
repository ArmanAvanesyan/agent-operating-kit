import importlib.util
import json
import os
from datetime import datetime, timedelta, timezone
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import uuid
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/native-loading/prepare_agy_login.py"
sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location("prepare_agy_login", SCRIPT)
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


class AgyLoginPreparationTests(unittest.TestCase):
    def test_terminal_queries_are_fragment_safe_deduplicated_and_never_user_input(self):
        answered = set()
        self.assertEqual(probe.terminal_query_replies(b"\x1b[>", answered), [])
        replies = probe.terminal_query_replies(b"\x1b[>c", answered)
        self.assertEqual(replies, [("secondary-attributes", b"\x1b[>0;276;0c")])
        self.assertEqual(probe.terminal_query_replies(b"\x1b[>c", answered), [])
        self.assertEqual(probe.terminal_query_replies(b"AB\x1b[6n", set()), [("cursor-position", b"\x1b[1;3R")])

    def test_common_authorization_code_field_labels_are_exact(self):
        for label in (b"Enter code:", b"Paste the authorization code:", b"Enter the code from your browser:", b"Authorization code:"):
            with self.subTest(label=label):
                self.assertTrue(probe.state_from_bytes(label)["authorizationCodePromptObserved"])
        self.assertFalse(probe.state_from_bytes(b"Write code to implement a feature")["authorizationCodePromptObserved"])

    def test_title_requires_instruction_and_cursor_inside_editable_box(self):
        instruction = "Copy the code in the browser and paste below:\r\n"
        field = "Authorization code\r\n╭──────────╮\r\n│          │\r\n╰──────────╯"
        active = (instruction + field + "\x1b[4;3H").encode()
        self.assertTrue(probe.state_from_bytes(active)["authorizationCodePromptObserved"])
        for raw in (
            b"Authorization Code",
            b"Authorization Code\r\nFollow the link in your browser",
            (instruction + field).encode(),  # Cursor outside the editable box.
            (field + "\x1b[3;3H").encode(),  # Instructions absent.
        ):
            with self.subTest(raw=raw):
                self.assertFalse(probe.state_from_bytes(raw)["authorizationCodePromptObserved"])

    def test_unboxed_code_field_requires_known_footer_and_cursor_at_footer_end(self):
        display = (
            "Copy the code in your browser and paste below:\r\n"
            "Authorization code\r\n\r\n\r\n"
            "shift+up/down navigate"
        )
        active = probe.state_from_bytes(display.encode())
        self.assertTrue(active["authorizationCodePromptObserved"])
        self.assertEqual(active["codeFieldGeometry"][0]["cursorRow"], 4)
        self.assertEqual(active["codeFieldGeometry"][0]["cursorColumn"], len("shift+up/down navigate"))

        placeholder = probe.state_from_bytes(
            display.replace("Authorization code", "Authorization code...").encode()
        )
        self.assertTrue(placeholder["authorizationCodePromptObserved"])
        self.assertEqual(placeholder["codeFieldGeometry"][0]["titleDecorationCodepoints"], [46, 46, 46])

        bad_screens = {
            "title and instructions only": "Copy the code in your browser and paste below:\r\nAuthorization code",
            "unknown footer": display.replace("shift+up/down navigate", "shift+up/down choose profile"),
            "nonblank form contents": display.replace("Authorization code\r\n\r\n", "Authorization code\r\nnot-empty\r\n"),
            "cursor on other row": display + "\x1b[4;10H",
            "cursor before footer end": display + "\x1b[5;22H",
            "unknown title punctuation": display.replace("Authorization code", "Authorization code.."),
            "extended title": display.replace("Authorization code", "Authorization code (required)"),
        }
        for label, raw in bad_screens.items():
            with self.subTest(label=label):
                state = probe.state_from_bytes(raw.encode())
                self.assertFalse(state["authorizationCodePromptObserved"])

    def test_public_form_footer_keeps_only_known_navigation_words(self):
        self.assertEqual(probe.public_form_footer("shift+up/down navigate"),
                         "shift+up/down navigate")
        self.assertIsNone(probe.public_form_footer(
            "shift+up/down navigate OneTimeCode123 private-value"))

    def test_selected_numbered_google_oauth_choice_starts_only_approved_flow(self):
        state = probe.state_from_bytes(b"Welcome to Antigravity\r\nSelect login method:\r\n > 1. Google OAuth\r\nEnter to select")
        self.assertTrue(probe.may_press_enter(state))
        state = probe.state_from_bytes(b"Welcome to Antigravity\r\nSelect login method:\r\n   1. Google OAuth\r\nEnter to select")
        self.assertFalse(probe.may_press_enter(state))

    def test_explicit_native_login_choice_can_start_approved_browser_flow(self):
        state = probe.state_from_bytes("Welcome to Antigravity\r\n❯ Login\r\nEnter to select".encode())
        self.assertTrue(probe.may_press_enter(state))
        state = probe.state_from_bytes(b"Login using a secret password\nEnter to select")
        self.assertFalse(probe.may_press_enter(state))
        self.assertFalse(probe.may_press_enter(probe.state_from_bytes(
            b"Sign in with Google\n> 2. Use existing account\nEnter to select")))

    def test_profile_denies_keychain_real_home_and_cli_writes_but_allows_oauth_network(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            cli = root / "antigravity"
            cli.write_bytes(b"test")
            profile = probe.sandbox_profile(root, cli, "/dev/ttys005")
        self.assertIn('(deny mach-lookup (global-name "com.apple.securityd"))', profile)
        self.assertIn("(deny file-read*", profile)
        self.assertIn("(deny file-write*", profile)
        self.assertIn("(literal \"/dev/ttys005\")", profile)
        self.assertIn("(deny file-write* (subpath", profile)
        self.assertNotIn("deny network", profile)

    def test_environment_is_private_and_disables_updates(self):
        with tempfile.TemporaryDirectory() as temp:
            env = probe.env_for(Path(temp))
        self.assertEqual(env["HOME"], str(Path(temp) / "home"))
        self.assertEqual(env["AGY_CLI_DISABLE_AUTO_UPDATE"], "true")
        self.assertEqual(env["BROWSER"], "/usr/bin/open")
        self.assertEqual(env["PYTHONDONTWRITEBYTECODE"], "1")
        self.assertNotIn("API_KEY", env)

    def test_enter_requires_auth_google_option_and_explicit_enter_hint(self):
        permitted = probe.state_from_bytes(b"> 1. Google OAuth\nEnter to select")
        self.assertTrue(probe.may_press_enter(permitted))
        self.assertFalse(probe.may_press_enter(probe.state_from_bytes(
            b"Sign in with Google\nEnter to select")))
        self.assertFalse(probe.may_press_enter(probe.state_from_bytes(
            b"  1. Google OAuth\nEnter to select")))
        self.assertFalse(probe.may_press_enter({**permitted, "authBarrierObserved": False}))
        self.assertFalse(probe.may_press_enter({**permitted, "googleSignInOptionObserved": False}))
        self.assertFalse(probe.may_press_enter({**permitted, "enterHintObserved": False}))
        self.assertFalse(probe.may_press_enter(probe.state_from_bytes(b"Press Enter to continue")))

    def test_authorization_code_input_requires_tty(self):
        with mock.patch.object(probe.os, "isatty", return_value=False):
            with self.assertRaisesRegex(RuntimeError, "requires a TTY"):
                probe._disable_terminal_echo(77)

    def test_authorization_code_tty_echo_is_disabled_and_restored(self):
        original = [0, 0, 0, probe.termios.ECHO | probe.termios.ICANON, 0, 0, [b"\n"]]
        with mock.patch.object(probe.os, "isatty", return_value=True), \
             mock.patch.object(probe.termios, "tcgetattr", return_value=original), \
             mock.patch.object(probe.termios, "tcsetattr") as set_attrs:
            saved = probe._disable_terminal_echo(77)
            changed = set_attrs.call_args.args[2]
            self.assertEqual(saved, original)
            self.assertFalse(changed[3] & probe.termios.ECHO)
            probe._restore_terminal(77, saved)
            self.assertEqual(set_attrs.call_count, 2)
            self.assertIs(set_attrs.call_args.args[2], original)

    def test_authorization_code_is_read_only_after_exact_native_prompt(self):
        with tempfile.TemporaryDirectory() as temp:
            audit = Path(temp) / "audit.jsonl"
            reads = [
                b"Welcome\n> 1. Google OAuth\nEnter to select",
                b"\x1b[2J\x1b[HAuthorization code:",
                b"AbC.123\n",
                b"\x1b[2J\x1b[HWelcome\nAsk anything\n\xe2\x9d\xaf ",
                b"",
            ]
            read_fds = [
                ([77], [], []), ([77], [], []), ([78], [], []), ([77], [], []),
            ]
            sent = []
            with mock.patch.object(probe.select, "select", side_effect=read_fds), \
                 mock.patch.object(probe.os, "read", side_effect=lambda _fd, _size: reads.pop(0)), \
                 mock.patch.object(probe.os, "write", side_effect=lambda _fd, data: sent.append(data) or len(data)), \
                 mock.patch.object(probe.termios, "tcflush"), \
                 mock.patch.object(probe.time, "monotonic", side_effect=[0.0] * 20):
                state, enter_sent = probe._capture_login(
                    77, 10.0, enter_sent=False, audit_path=audit, case_root=temp,
                    authorization_code_fd=78,
                )
            self.assertTrue(enter_sent)
            self.assertEqual(sent, [b"\r", b"AbC.123\r"])
            self.assertFalse(state["authorizationCodePromptObserved"])
            self.assertTrue(probe.is_ready(state))
            events = [json.loads(line) for line in audit.read_text().splitlines()]
            submitted = [event for event in events if event["event"] == "authorization-code-submitted"]
            self.assertEqual(len(submitted), 1)
            self.assertTrue(submitted[0]["authorizationCodeSubmitted"])
            self.assertEqual(set(submitted[0]), {"event", "at", "authorizationCodeSubmitted"})
            self.assertNotIn("AbC.123", audit.read_text())

    def test_authorization_code_wrong_prompt_and_oversize_never_submit_code(self):
        with tempfile.TemporaryDirectory() as temp:
            audit = Path(temp) / "audit.jsonl"
            reads = [b"> 1. Google OAuth\nEnter to select",
                     b"\x1b[2J\x1b[HEnter API key:", b""]
            readiness = [([77], [], []), ([77], [], []), ([77], [], [])]
            sent = []
            with mock.patch.object(probe.select, "select", side_effect=readiness), \
                 mock.patch.object(probe.os, "read", side_effect=lambda _fd, _size: reads.pop(0)), \
                 mock.patch.object(probe.os, "write", side_effect=lambda _fd, data: sent.append(data) or len(data)), \
                 mock.patch.object(probe.time, "monotonic", side_effect=[0.0] * 12):
                _, enter_sent = probe._capture_login(
                    77, 10.0, enter_sent=False, audit_path=audit, case_root=temp,
                    authorization_code_fd=78,
                )
            self.assertTrue(enter_sent)
            self.assertEqual(sent, [b"\r"])
            self.assertNotIn("authorization-code-submitted", audit.read_text())

        with tempfile.TemporaryDirectory() as temp:
            audit = Path(temp) / "audit.jsonl"
            reads = [b"> 1. Google OAuth\nEnter to select",
                     b"\x1b[2J\x1b[HAuthorization code:", b"A" * 1025]
            with mock.patch.object(probe.select, "select", side_effect=[([77], [], []), ([77], [], []), ([78], [], [])]), \
                 mock.patch.object(probe.os, "read", side_effect=lambda _fd, _size: reads.pop(0)), \
                 mock.patch.object(probe.os, "write") as write, \
                 mock.patch.object(probe.time, "monotonic", side_effect=[0.0] * 12):
                with self.assertRaisesRegex(RuntimeError, "exceeds limit"):
                    probe._capture_login(
                        77, 10.0, enter_sent=False, audit_path=audit, case_root=temp,
                        authorization_code_fd=78,
                    )
            self.assertEqual([call.args[1] for call in write.call_args_list], [b"\r"])
            self.assertNotIn("A" * 100, audit.read_text())

    def test_private_code_file_validation_rejects_symlink_hardlink_and_unsafe_modes(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            private = root / "task"
            private.mkdir(mode=0o700)
            private.chmod(0o700)
            regular = private / "code.txt"
            regular.write_text("AbC123\n")
            regular.chmod(0o600)
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root):
                self.assertEqual(probe.validate_authorization_code_file(regular), regular)
                self.assertEqual(probe.read_authorization_code_file(regular)[0], b"AbC123\n")

                link = private / "link.txt"
                link.symlink_to(regular)
                with self.assertRaisesRegex(RuntimeError, "permissions or type"):
                    probe.validate_authorization_code_file(link)

                hardlink = private / "hardlink.txt"
                os.link(regular, hardlink)
                with self.assertRaisesRegex(RuntimeError, "permissions or type"):
                    probe.validate_authorization_code_file(regular)
                hardlink.unlink()

                regular.chmod(0o644)
                with self.assertRaisesRegex(RuntimeError, "permissions or type"):
                    probe.validate_authorization_code_file(regular)

    def test_private_code_file_enforces_exact_size_and_character_limits(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            private = root / "task"
            private.mkdir(mode=0o700)
            private.chmod(0o700)
            code_file = private / "code.txt"
            code_file.write_bytes(b"A" * 1024 + b"\n")
            code_file.chmod(0o600)
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root):
                code, _ = probe.read_authorization_code_file(code_file)
                self.assertEqual(len(code), 1025)
                code_file.write_bytes(b"A" * 1025 + b"\n")
                with self.assertRaisesRegex(RuntimeError, "permissions or type"):
                    probe.validate_authorization_code_file(code_file)
                code_file.write_bytes(b"not-a-code!\n")
                with self.assertRaisesRegex(RuntimeError, "invalid characters"):
                    probe.read_authorization_code_file(code_file)

    def test_prepare_cleans_rejected_private_regular_file_but_never_follows_symlink(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            private = root / "task"
            private.mkdir(mode=0o700)
            private.chmod(0o700)
            rejected = private / "unsafe-mode.txt"
            rejected.write_text("not-read\n")
            rejected.chmod(0o644)

            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root):
                with self.assertRaisesRegex(RuntimeError, "permissions or type"):
                    probe.prepare("unused", authorization_code_file=rejected)
            self.assertFalse(rejected.exists())

            target = private / "target.txt"
            target.write_text("target remains\n")
            target.chmod(0o600)
            link = private / "input-link.txt"
            link.symlink_to(target)
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root):
                with self.assertRaisesRegex(RuntimeError, "permissions or type"):
                    probe.prepare("unused", authorization_code_file=link)
            self.assertTrue(link.is_symlink())
            self.assertEqual(target.read_text(), "target remains\n")

            outside = root / "outside"
            outside.mkdir(mode=0o700)
            outside.chmod(0o700)
            outside_file = outside / "input.txt"
            outside_file.write_text("outside remains\n")
            outside_file.chmod(0o600)
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", private):
                with self.assertRaisesRegex(RuntimeError, "outside the private"):
                    probe.prepare("unused", authorization_code_file=outside_file)
            self.assertEqual(outside_file.read_text(), "outside remains\n")

    def test_file_code_is_read_only_at_native_code_prompt_and_unlinked_before_submit(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            private = root / "task"
            private.mkdir(mode=0o700)
            private.chmod(0o700)
            code_file = private / "authorization-code.txt"
            code_file.write_text("AbC.123\n")
            code_file.chmod(0o600)
            audit = root / "audit.jsonl"
            chunks = [
                b"> 1. Google OAuth\nEnter to select",
                b"\x1b[2J\x1b[HAuthorization code:",
                b"\x1b[2J\x1b[HWelcome\nAsk anything\n\xe2\x9d\xaf ",
                b"",
            ]
            select_calls = {"count": 0}
            sent = []
            real_read = os.read

            def select_ready(fds, _w, _x, _timeout):
                self.assertNotIn(None, fds, "file-only mode must not pass a missing fd to select")
                self.assertEqual(fds, [77])
                select_calls["count"] += 1
                if select_calls["count"] in (1, 2, 4):
                    return [77], [], []
                return [], [], []

            def write_native(_fd, data):
                sent.append(data)
                if data == b"AbC.123\r":
                    self.assertFalse(code_file.exists(), "file must be removed before submission")
                return len(data)

            def read_fd(fd, size):
                return chunks.pop(0) if fd == 77 else real_read(fd, size)

            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root), \
                 mock.patch.object(probe.select, "select", side_effect=select_ready), \
                 mock.patch.object(probe.os, "read", side_effect=read_fd), \
                 mock.patch.object(probe.os, "write", side_effect=write_native), \
                 mock.patch.object(probe.time, "monotonic", return_value=0.0):
                state, enter_sent = probe._capture_login(
                    77, 10.0, enter_sent=False, audit_path=audit, case_root=root,
                    authorization_code_file=code_file,
                )

            self.assertTrue(enter_sent)
            self.assertEqual(sent, [b"\r", b"AbC.123\r"])
            self.assertTrue(probe.is_ready(state))
            events = [json.loads(line) for line in audit.read_text().splitlines()]
            submitted = [event for event in events if event["event"] == "authorization-code-submitted"]
            self.assertEqual(len(submitted), 1)
            self.assertNotIn("AbC.123", audit.read_text())
            self.assertFalse(code_file.exists())

    def test_file_code_wrong_prompt_does_not_read_or_submit(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            private = root / "task"
            private.mkdir(mode=0o700)
            private.chmod(0o700)
            code_file = private / "authorization-code.txt"
            code_file.write_text("AbC123\n")
            code_file.chmod(0o600)
            audit = root / "audit.jsonl"
            chunks = [b"> 1. Google OAuth\nEnter to select",
                      b"\x1b[2J\x1b[HEnter API key:", b""]
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root), \
                 mock.patch.object(probe.select, "select", side_effect=[([77], [], []), ([77], [], []), ([77], [], [])]), \
                 mock.patch.object(probe.os, "read", side_effect=lambda _fd, _size: chunks.pop(0)), \
                 mock.patch.object(probe.os, "write") as write, \
                 mock.patch.object(probe, "read_authorization_code_file", side_effect=AssertionError("read before native prompt")), \
                 mock.patch.object(probe.time, "monotonic", return_value=0.0):
                _, enter_sent = probe._capture_login(
                    77, 10.0, enter_sent=False, audit_path=audit, case_root=root,
                    authorization_code_file=code_file,
                )
            self.assertTrue(enter_sent)
            self.assertEqual([call.args[1] for call in write.call_args_list], [b"\r"])
            self.assertNotIn("authorization-code-submitted", audit.read_text())
            self.assertTrue(code_file.exists())

    def test_file_code_gate_is_rechecked_after_fresh_master_screen(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            private = root / "task"
            private.mkdir(mode=0o700)
            private.chmod(0o700)
            code_file = private / "authorization-code.txt"
            code_file.write_text("AbC123\n")
            code_file.chmod(0o600)
            audit = root / "audit.jsonl"
            chunks = [
                b"> 1. Google OAuth\nEnter to select",
                b"\x1b[2J\x1b[HAuthorization code:",
                b"\x1b[2J\x1b[HWelcome to Antigravity\nStarting local services",
                b"",
            ]
            sent = []

            def read_fd(fd, _size):
                self.assertEqual(fd, 77)
                return chunks.pop(0)

            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root), \
                 mock.patch.object(probe.select, "select", side_effect=lambda *_: ([77], [], [])), \
                 mock.patch.object(probe.os, "read", side_effect=read_fd), \
                 mock.patch.object(probe.os, "write", side_effect=lambda _fd, data: sent.append(data) or len(data)), \
                 mock.patch.object(probe, "read_authorization_code_file", side_effect=AssertionError("stale code prompt used")), \
                 mock.patch.object(probe.time, "monotonic", return_value=0.0):
                state, enter_sent = probe._capture_login(
                    77, 10.0, enter_sent=False, audit_path=audit, case_root=root,
                    authorization_code_file=code_file,
                )
            self.assertTrue(enter_sent)
            self.assertFalse(state["authorizationCodePromptObserved"])
            self.assertEqual(sent, [b"\r"])
            self.assertTrue(code_file.exists())
            self.assertNotIn("authorization-code-submitted", audit.read_text())

    def test_color_scheme_continue_requires_exact_setup_screen_and_safe_context(self):
        base = (b"Welcome to Antigravity\nChoose your color scheme:\n"
                b"> Dark\nTerms and privacy information\nPress Enter to continue")
        positive = probe.state_from_bytes(base)
        self.assertTrue(probe.may_continue_color_scheme(positive))
        self.assertFalse(positive["errorObserved"])
        self.assertFalse(positive["trustPromptObserved"])
        self.assertFalse(positive["termsAcceptancePromptObserved"])

        cases = {
            "missing color scheme title": b"Welcome to Antigravity\nChoose a rendering mode\nPress Enter to continue",
            "missing continue hint": b"Welcome to Antigravity\nChoose your color scheme:\n> Dark",
            "missing welcome label": b"Choose your color scheme:\n> Dark\nPress Enter to continue",
            "authentication barrier": b"Welcome to Antigravity\nSign in to continue\nChoose your color scheme:\nPress Enter to continue",
            "normal prompt": b"Welcome to Antigravity\nChoose your color scheme:\nPress Enter to continue\nAsk anything\n\xe2\x9d\xaf ",
            "terminal ambiguity": base + b"\x1b[!p",
            "generic error": b"Welcome to Antigravity\nChoose your color scheme:\nError: unrelated status\nPress Enter to continue",
            "workspace trust": b"Welcome to Antigravity\nDo you trust this workspace directory?\nChoose your color scheme:\nPress Enter to continue",
            "wrapped workspace trust": b"Welcome to Antigravity\nDo you trust this\nworkspace directory?\nChoose your color scheme:\nPress Enter to continue",
            "terms acceptance": b"Welcome to Antigravity\nPlease accept the terms\nChoose your color scheme:\nPress Enter to continue",
            "wrapped terms acceptance": b"Welcome to Antigravity\nPlease accept\nour terms\nChoose your color scheme:\nPress Enter to continue",
        }
        for label, raw in cases.items():
            with self.subTest(label=label):
                state = probe.state_from_bytes(raw)
                self.assertFalse(probe.may_continue_color_scheme(state))
                if "trust" in label:
                    self.assertTrue(state["trustPromptObserved"])
                if "terms" in label:
                    self.assertTrue(state["termsAcceptancePromptObserved"])
                if label == "generic error":
                    self.assertTrue(state["errorObserved"])

    def test_color_scheme_preview_error_is_excluded_only_with_full_framed_context(self):
        def framed(inner):
            width = 72
            return "│" + inner.ljust(width)[:width] + "│"

        header = "Choose your color scheme:"
        go = framed("func main() {")
        error = framed("✗ error: compilation failed")
        warning = framed("⚠ warning: deprecation warning")
        source = framed("file:///tmp/aok-preview/main.go")
        footer = "Press Enter to continue"

        def screen(err=error, warn=warning, link=source, include_go=True,
                   include_header=True, extra_error=None):
            rows = ["Welcome to Antigravity"]
            if include_header:
                rows.append(header)
            rows.extend([framed("package main"), go] if include_go else [framed("package main")])
            rows.extend([err, warn, link, framed("}"), footer])
            if extra_error:
                rows.insert(1, extra_error)
            return "\r\n".join(rows).encode()

        positive = probe.state_from_bytes(screen())
        self.assertTrue(positive["rawGenericErrorObserved"])
        self.assertFalse(positive["errorObserved"])
        self.assertEqual(len(positive["previewSampleErrorRows"]), 1)
        self.assertTrue(probe.may_continue_color_scheme(positive))

        negative_cases = {
            "changed warning": screen(warn=framed("⚠ warning: unrelated warning")),
            "missing Go context": screen(include_go=False),
            "missing source link": screen(link=framed("source unavailable")),
            "missing color header": screen(include_header=False),
        }
        for label, raw in negative_cases.items():
            with self.subTest(label=label):
                state = probe.state_from_bytes(raw)
                self.assertEqual(state["previewSampleErrorRows"], [])
                self.assertTrue(state["errorObserved"])
                self.assertFalse(probe.may_continue_color_scheme(state))

        genuine = probe.state_from_bytes(screen(extra_error="Error: unrelated native failure"))
        self.assertEqual(len(genuine["previewSampleErrorRows"]), 1)
        self.assertTrue(genuine["rawGenericErrorObserved"])
        self.assertTrue(genuine["errorObserved"])
        self.assertFalse(probe.may_continue_color_scheme(genuine))

    def test_color_scheme_diagnostics_are_fixed_and_do_not_retain_error_row_text(self):
        private_text = b"Welcome to Antigravity\nChoose your color scheme:\nError: private-user@example.test token=private-value\nPress Enter to continue"
        state = probe.state_from_bytes(private_text)
        safe = probe._safe_state(state)
        self.assertTrue(state["errorObserved"])
        self.assertTrue(state["errorRowDiagnostics"])
        self.assertFalse(probe.may_continue_color_scheme(state))
        encoded = json.dumps(safe)
        self.assertNotIn("private-user@example.test", encoded)
        self.assertNotIn("private-value", encoded)
        self.assertTrue(safe["errorRowDiagnostics"][0]["fixedWords"])

    def test_capture_submits_code_then_continues_color_scheme_once(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            private = root / "task"
            private.mkdir(mode=0o700)
            private.chmod(0o700)
            code_file = private / "authorization-code.txt"
            code_file.write_text("OneTimeCode123\n")
            code_file.chmod(0o600)
            audit = root / "audit.jsonl"
            chunks = [
                b"Welcome to Antigravity\n> 1. Google OAuth\nEnter to select",
                b"\x1b[2J\x1b[HAuthorization code:",
                b"\x1b[2J\x1b[HWelcome to Antigravity\nChoose your color scheme:\n> Dark\nPress Enter to continue",
                b"\x1b[2J\x1b[HWelcome to Antigravity\nChoose your color scheme:\n> Dark\nPress Enter to continue",
                b"\x1b[2J\x1b[HWelcome to Antigravity\nAsk anything\n\xe2\x9d\xaf ",
            ]
            select_calls = {"count": 0}
            sent = []
            real_read = os.read

            def select_ready(fds, _w, _x, _timeout):
                self.assertNotIn(None, fds)
                self.assertEqual(fds, [77])
                select_calls["count"] += 1
                # Poll the private file after the code prompt is rendered.
                if select_calls["count"] == 3:
                    return [], [], []
                return [77], [], []

            def read_fd(fd, size):
                return chunks.pop(0) if fd == 77 else real_read(fd, size)

            def write_native(_fd, data):
                sent.append(data)
                if data == b"OneTimeCode123\r":
                    self.assertFalse(code_file.exists(), "code file must be unlinked before submission")
                return len(data)

            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root), \
                 mock.patch.object(probe.select, "select", side_effect=select_ready), \
                 mock.patch.object(probe.os, "read", side_effect=read_fd), \
                 mock.patch.object(probe.os, "write", side_effect=write_native), \
                 mock.patch.object(probe.time, "monotonic", return_value=0.0):
                state, enter_sent = probe._capture_login(
                    77, 10.0, enter_sent=False, audit_path=audit, case_root=root,
                    authorization_code_file=code_file,
                )

            self.assertTrue(enter_sent)
            self.assertTrue(probe.is_ready(state))
            self.assertEqual(sent, [b"\r", b"OneTimeCode123\r", b"\r"])
            self.assertFalse(code_file.exists())
            self.assertNotIn("OneTimeCode123", audit.read_text())
            audit_events = [json.loads(line) for line in audit.read_text().splitlines()]
            color_events = [event for event in audit_events
                            if event["event"] == "native-color-scheme-continued"]
            self.assertEqual(len(color_events), 1)
            self.assertEqual(color_events[0]["purpose"], "visual-appearance-only")

    def test_capture_retains_generic_preview_error_checkpoint_without_entering_theme(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            private = root / "task"
            private.mkdir(mode=0o700)
            private.chmod(0o700)
            code_file = private / "authorization-code.txt"
            code_file.write_text("OneTimeCode123\n")
            code_file.chmod(0o600)
            audit = root / "audit.jsonl"
            chunks = [
                b"> 1. Google OAuth\nEnter to select",
                b"\x1b[2J\x1b[HAuthorization code:",
                ("\x1b[2J\x1b[HWelcome to Antigravity\nChoose your color scheme:\n"
                 "Error: Request failed\nWarning: Sample warning\nPress Enter to continue").encode(),
            ]
            ready_calls = [[77], [77], [], [77]]
            sent = []
            real_read = os.read

            def read_fd(fd, _size):
                if fd == 77:
                    return chunks.pop(0)
                return real_read(fd, _size)

            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root), \
                 mock.patch.object(probe.select, "select", side_effect=lambda *_: (ready_calls.pop(0), [], [])), \
                 mock.patch.object(probe.os, "read", side_effect=read_fd), \
                 mock.patch.object(probe.os, "write", side_effect=lambda _fd, data: sent.append(data) or len(data)), \
                 mock.patch.object(probe.time, "monotonic", return_value=0.0):
                state, enter_sent = probe._capture_login(
                    77, 10.0, enter_sent=False, audit_path=audit, case_root=root,
                    authorization_code_file=code_file, retain_onboarding=True,
                )
            self.assertTrue(enter_sent)
            self.assertTrue(state["authorizationCodeSubmitted"])
            self.assertTrue(state["colorSchemeScreenObserved"])
            self.assertTrue(state["errorObserved"])
            self.assertEqual(state["errorCategories"], ["generic-error"])
            self.assertEqual(sent, [b"\r", b"OneTimeCode123\r"])
            self.assertNotIn("OneTimeCode123", audit.read_text())

    def test_resume_theme_continuation_still_requires_strict_error_free_gate(self):
        screen = ("Welcome to Antigravity\nChoose your color scheme:\n> Dark\n"
                  "Press Enter to continue").encode()
        with tempfile.TemporaryDirectory() as temp:
            audit = Path(temp) / "audit.jsonl"
            for raw, expected in ((screen + b"\nError: Request failed", []), (screen, [b"\r"])):
                chunks = [raw, b"Welcome to Antigravity\nAsk anything\n\xe2\x9d\xaf "]
                sent = []
                ready_calls = [[77], [77]]
                with mock.patch.object(probe.select, "select", side_effect=lambda *_: (ready_calls.pop(0), [], [])), \
                     mock.patch.object(probe.os, "read", side_effect=lambda _fd, _size: chunks.pop(0)), \
                     mock.patch.object(probe.os, "write", side_effect=lambda _fd, data: sent.append(data) or len(data)), \
                     mock.patch.object(probe.time, "monotonic", return_value=0.0):
                    probe._capture_login(77, 10.0, enter_sent=True, audit_path=audit,
                                         case_root=temp, resume_onboarding=True)
                self.assertEqual(sent, expected)

    @mock.patch.object(probe.sys, "platform", "darwin")
    @mock.patch.object(probe.os, "uname", lambda: os.uname_result(("Darwin", "", "", "", "x86_64")))
    def test_prepare_removes_unused_code_file_on_blocked_return(self):
        with tempfile.TemporaryDirectory() as temp:
            tmp = Path(temp)
            root_parent = (tmp / "roots").resolve()
            root_parent.mkdir()
            code_dir = root_parent / "code-input"
            code_dir.mkdir(mode=0o700)
            code_dir.chmod(0o700)
            code_file = code_dir / "authorization-code.txt"
            code_file.write_text("unused\n")
            code_file.chmod(0o600)
            binary = tmp / "vendor"
            binary.write_bytes(b"pinned test fixture")
            made = []

            def owned(prefix):
                path = root_parent / f"{prefix}{len(made)}"
                path.mkdir(mode=0o700)
                made.append(path)
                return path

            def blocked_run(*_args, **_kwargs):
                return {"promptReady": False, "state": {"errorObserved": True}}

            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root_parent), \
                 mock.patch.object(probe, "_owned_dir", side_effect=owned), \
                 mock.patch.object(probe, "_record_dependencies", return_value={"prepare_agy_login.py": "a" * 64}), \
                 mock.patch.object(probe, "file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_preflight", return_value={"terminated": True}), \
                 mock.patch.object(probe, "run_login", side_effect=blocked_run):
                result = probe.prepare(binary, timeout=3, authorization_code_file=code_file)
            self.assertEqual(result["status"], "blocked")
            self.assertFalse(code_file.exists())

    def test_terminal_state_is_sanitized_and_ready_requires_prompt_without_barrier(self):
        state = probe.state_from_bytes(
            b"sensitive-user@sample.test\nAsk anything\n\xe2\x9d\xaf "
        )
        self.assertTrue(probe.is_ready(state))
        self.assertNotIn("sensitive-user@sample.test", json.dumps(probe._safe_state(state)))
        self.assertFalse(probe.is_ready({**state, "authBarrierObserved": True}))
        self.assertFalse(probe.is_ready({**state, "errorObserved": True}))

    def test_ambiguous_terminal_stream_blocks_auth_enter_and_prompt_ready(self):
        state = probe.state_from_bytes(
            b"Sign in with Google\nEnter to select\x1b[?1047h"
        )
        self.assertTrue(state["terminalAmbiguityObserved"])
        self.assertFalse(state["authBarrierObserved"])
        self.assertFalse(state["localPromptObserved"])
        self.assertFalse(probe.may_press_enter(state))
        self.assertFalse(probe.is_ready(state))

    def test_concealed_login_choice_cannot_trigger_enter(self):
        state = probe.state_from_bytes(
            b"\x1b[8mSign in with Google\nEnter to select\x1b[28m"
        )
        self.assertTrue(state["terminalAmbiguityObserved"])
        self.assertFalse(state["authBarrierObserved"])
        self.assertFalse(probe.may_press_enter(state))
        self.assertFalse(probe.is_ready(state))

    def test_black_on_black_login_choice_cannot_trigger_enter(self):
        state = probe.state_from_bytes(
            b"\x1b[30;40mSign in with Google\nEnter to select\x1b[39;49m"
        )
        self.assertFalse(state["terminalAmbiguityObserved"])
        self.assertFalse(state["authBarrierObserved"])
        self.assertFalse(probe.may_press_enter(state))
        self.assertFalse(probe.is_ready(state))

    def test_active_synchronized_output_cannot_trigger_enter(self):
        state = probe.state_from_bytes(
            b"Sign in with Google\nEnter to select\x1b[?2026h"
        )
        self.assertTrue(state["terminalAmbiguityObserved"])
        self.assertFalse(state["authBarrierObserved"])
        self.assertFalse(probe.may_press_enter(state))
        self.assertFalse(probe.is_ready(state))

    def test_completed_synchronized_output_pair_preserves_prompt(self):
        state = probe.state_from_bytes(
            b"> 1. Google OAuth\nEnter to select\x1b[?2026h\x1b[?2026l"
        )
        self.assertFalse(state["terminalAmbiguityObserved"])
        self.assertTrue(state["authBarrierObserved"])
        self.assertTrue(probe.may_press_enter(state))

    def test_auth_text_before_terminal_clear_does_not_mask_ready_screen(self):
        state = probe.state_from_bytes(
            b"Sign in with Google\nPress Enter to continue\x1b[2J\x1b[HAsk anything\n\xe2\x9d\xaf "
        )
        self.assertFalse(state["authBarrierObserved"])
        self.assertTrue(state["localPromptObserved"])
        self.assertTrue(probe.is_ready(state))

    def test_capture_preserves_fragmented_wire_bytes_and_audits_sanitized_state_changes(self):
        with tempfile.TemporaryDirectory() as temp:
            audit = Path(temp) / "audit.jsonl"
            chunks = [
                b"> 1. Google OAu",
                b"th\nEnter to",
                b" select",
                b"\x1b[2",
                b"J\x1b[HAsk anything\n",
                b"\xe2\x9d\xaf ",
            ]
            sent = []
            clock = {"value": 0.0}

            def monotonic():
                clock["value"] += 0.1
                return clock["value"]

            def select_ready(*_args):
                return ([77], [], []) if chunks else ([], [], [])

            def read_chunk(_fd, _size):
                return chunks.pop(0)

            with mock.patch.object(probe.select, "select", side_effect=select_ready), \
                 mock.patch.object(probe.os, "read", side_effect=read_chunk), \
                 mock.patch.object(probe.os, "write", side_effect=lambda _fd, data: sent.append(data) or len(data)), \
                 mock.patch.object(probe.time, "monotonic", side_effect=monotonic):
                state, enter_sent = probe._capture_login(
                    77, 2.0, enter_sent=False, audit_path=audit, case_root=temp
                )

            self.assertTrue(enter_sent)
            self.assertEqual(sent, [b"\r"])
            self.assertTrue(probe.is_ready(state))
            events = [json.loads(line) for line in audit.read_text().splitlines()]
            transitions = [event for event in events if event["event"] == "terminal-state-change"]
            self.assertGreaterEqual(len(transitions), 2)
            self.assertTrue(any(event["state"]["authBarrierObserved"] for event in transitions))
            self.assertTrue(any(event["state"]["localPromptObserved"] for event in transitions))
            self.assertNotIn("Sign in with Google", audit.read_text())

    def test_stable_terminal_ambiguity_returns_without_enter(self):
        with tempfile.TemporaryDirectory() as temp:
            audit = Path(temp) / "audit.jsonl"
            ticks = {"count": 0}
            def monotonic():
                ticks["count"] += 1
                return 2.1 if ticks["count"] >= 6 else 0.0
            sent = []
            with mock.patch.object(probe.select, "select", side_effect=[([77], [], []), ([], [], [])]), \
                 mock.patch.object(probe.os, "read", return_value=b"Ready\x1b[!p"), \
                 mock.patch.object(probe.os, "write", side_effect=lambda _fd, data: sent.append(data) or len(data)), \
                 mock.patch.object(probe.time, "monotonic", side_effect=monotonic):
                state, enter_sent = probe._capture_login(
                    77, 100.0, enter_sent=False, audit_path=audit, case_root=temp
                )
            self.assertFalse(enter_sent)
            self.assertEqual(sent, [])
            self.assertTrue(state["terminalAmbiguityObserved"])
            events = [json.loads(line) for line in audit.read_text().splitlines()]
            self.assertTrue(any(event["event"] == "stable-terminal-ambiguity" for event in events))

    def test_home_is_removed_even_when_invalid_marker_write_fails(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            home = root / "home"
            home.mkdir()
            (home / "credential-placeholder").write_text("private")
            marker = {"valid": True}
            with mock.patch.object(probe, "_write_invalid_marker", side_effect=OSError("private")):
                with self.assertRaisesRegex(RuntimeError, "private HOME"):
                    probe.invalidate_and_remove_private_home(root, marker)
            self.assertFalse(home.exists())
            self.assertFalse((root / "owned-session.json").exists())

    def test_marker_ttl_is_at_most_fifteen_minutes_and_starts_invalid(self):
        created = datetime(2026, 10, 18, tzinfo=timezone.utc)
        marker = probe.session_marker("123e4567-e89b-12d3-a456-426614174000", created)
        expires = datetime.fromisoformat(marker["expiresAt"].replace("Z", "+00:00"))
        self.assertLessEqual(expires - created, timedelta(minutes=15))
        self.assertFalse(marker["valid"])

    def _checkpoint(self, parent, *, created=None, expires=None, phase="color-scheme",
                    valid=False, ready=False, binary_sha=None):
        parent = Path(parent)
        root = parent / f"aok-agy-login-checkpoint-{uuid.uuid4()}"
        root.mkdir(mode=0o700)
        root.chmod(0o700)
        home = root / "home"
        home.mkdir(mode=0o700)
        home.chmod(0o700)
        cli = root / "antigravity"
        cli.write_bytes(b"pinned test cli")
        cli.chmod(0o500)
        now = datetime.now(timezone.utc)
        created = created or now - timedelta(minutes=1)
        expires = expires or created + timedelta(minutes=15)
        marker = probe.session_marker(str(uuid.uuid4()), created)
        marker.update({"valid": valid, "readyPromptObserved": ready,
                       "phase": phase,
                       "binarySHA256": binary_sha or probe.PINNED_BINARY_SHA256})
        marker["expiresAt"] = expires.isoformat().replace("+00:00", "Z")
        marker_path = root / "owned-session.json"
        marker_path.write_text(json.dumps(marker), encoding="utf-8")
        marker_path.chmod(0o600)
        return root, marker

    def test_checkpoint_validator_accepts_only_current_invalid_color_checkpoint(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp).resolve()
            root, marker = self._checkpoint(parent)
            with mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256):
                validated = probe.validate_onboarding_checkpoint(root, private_temp_root=parent)
            self.assertEqual(validated["root"], root)
            self.assertFalse(validated["marker"]["valid"])
            self.assertEqual(validated["marker"]["phase"], "color-scheme")
            with self.assertRaisesRegex(ValueError, "prompt-ready"):
                probe.AGY.validate_isolated_login_root(root, private_temp_root=parent)

            after_root, _ = self._checkpoint(parent, phase="after-color-scheme")
            with mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256):
                self.assertEqual(probe.validate_onboarding_checkpoint(
                    after_root, private_temp_root=parent)["marker"]["phase"], "after-color-scheme")

    def test_checkpoint_validator_rejects_expired_forged_and_symlinked_checkpoints(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp).resolve()
            expired = datetime.now(timezone.utc) - timedelta(minutes=20)
            root, _ = self._checkpoint(parent, created=expired, expires=expired + timedelta(minutes=15))
            with mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256):
                with self.assertRaisesRegex(RuntimeError, "current onboarding"):
                    probe.validate_onboarding_checkpoint(root, private_temp_root=parent)

            wrong_phase_root, _ = self._checkpoint(parent, phase="workspace-trust")
            with mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256):
                with self.assertRaisesRegex(RuntimeError, "current onboarding"):
                    probe.validate_onboarding_checkpoint(wrong_phase_root, private_temp_root=parent)

            forged, _ = self._checkpoint(parent, binary_sha="0" * 64)
            with mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256):
                with self.assertRaisesRegex(RuntimeError, "current onboarding"):
                    probe.validate_onboarding_checkpoint(forged, private_temp_root=parent)

            outside = parent / "outside"
            outside.mkdir()
            link = parent / "aok-agy-login-checkpoint-link"
            link.symlink_to(outside, target_is_directory=True)
            with self.assertRaisesRegex(RuntimeError, "canonical private temporary"):
                probe.validate_onboarding_checkpoint(link, private_temp_root=parent)

    def test_checkpoint_rejects_symlink_inside_private_home(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp).resolve()
            root, _ = self._checkpoint(parent)
            outside = parent / "outside-secret"
            outside.write_text("do not read")
            (root / "home" / "credential-link").symlink_to(outside)
            with mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256):
                with self.assertRaisesRegex(RuntimeError, "unsafe file"):
                    probe.validate_onboarding_checkpoint(root, private_temp_root=parent)

    def test_known_native_log_link_is_unlinked_without_following_target(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp).resolve()
            root, _ = self._checkpoint(parent)
            target = parent / "outside-log-target"
            target.write_text("private target remains untouched")
            gemini = root / "home" / ".gemini"
            cli_dir = gemini / "antigravity-cli"
            gemini.mkdir(mode=0o700)
            cli_dir.mkdir(mode=0o700)
            gemini.chmod(0o700)
            cli_dir.chmod(0o700)
            link = cli_dir / "cli.log"
            link.symlink_to(target)
            with mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256):
                validated = probe.validate_onboarding_checkpoint(
                    root, private_temp_root=parent, cleanup_native_log=True)
            self.assertEqual(validated["root"], root)
            self.assertFalse(link.is_symlink())
            self.assertEqual(target.read_text(), "private target remains untouched")

    def test_resume_cleans_only_known_log_link_and_unknown_or_escaping_links_still_fail(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp).resolve()
            root, _ = self._checkpoint(parent)
            outside = parent / "outside"
            outside.mkdir()
            target = outside / "target"
            target.write_text("unchanged")
            gemini = root / "home" / ".gemini"
            cli_dir = gemini / "antigravity-cli"
            gemini.mkdir(mode=0o700)
            cli_dir.mkdir(mode=0o700)
            gemini.chmod(0o700)
            cli_dir.chmod(0o700)
            (cli_dir / "cli.log").symlink_to(target)
            (root / "home" / "unrelated-link").symlink_to(target)
            with mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256):
                with self.assertRaisesRegex(RuntimeError, "unsafe file"):
                    probe.validate_onboarding_checkpoint(
                        root, private_temp_root=parent, cleanup_native_log=True)
            self.assertTrue((root / "home" / "unrelated-link").is_symlink())
            self.assertEqual(target.read_text(), "unchanged")

            escaped_root, _ = self._checkpoint(parent)
            escaped_home = escaped_root / "home"
            (outside / "antigravity-cli").mkdir(mode=0o700)
            (escaped_home / ".gemini").symlink_to(outside, target_is_directory=True)
            with self.assertRaisesRegex(RuntimeError, "native log parent chain"):
                probe.remove_native_log_link(escaped_home)

    def test_checkpoint_private_home_depth_limit_is_enforced(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp).resolve()
            root, _ = self._checkpoint(parent)
            current = root / "home"
            for _ in range(33):
                current = current / "nested"
                current.mkdir(mode=0o700)
            with mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256):
                with self.assertRaisesRegex(RuntimeError, "HOME exceeds limits"):
                    probe.validate_onboarding_checkpoint(root, private_temp_root=parent)

    def test_color_checkpoint_retention_is_diagnostic_and_does_not_allow_errors_except_generic(self):
        base = {"colorSchemeScreenObserved": True, "welcomeLabelObserved": True,
                "authBarrierObserved": False, "localPromptObserved": False,
                "terminalAmbiguityObserved": False, "errorObserved": False,
                "errorCategories": []}
        self.assertTrue(probe.may_retain_color_checkpoint(base))
        self.assertTrue(probe.may_retain_color_checkpoint(
            {**base, "errorObserved": True, "errorCategories": ["generic-error"]}))
        for change in (
            {"errorObserved": True, "errorCategories": ["sandbox"]},
            {"authBarrierObserved": True}, {"localPromptObserved": True},
            {"terminalAmbiguityObserved": True}, {"welcomeLabelObserved": False},
        ):
            with self.subTest(change=change):
                self.assertFalse(probe.may_retain_color_checkpoint({**base, **change}))

    def test_after_color_checkpoint_requires_continuation_proof_and_safe_nonready_screen(self):
        after = {"colorSchemeScreenObserved": False, "colorSchemeContinued": True,
                 "authBarrierObserved": False, "localPromptObserved": False,
                 "terminalAmbiguityObserved": False, "errorObserved": False}
        self.assertTrue(probe.may_retain_color_checkpoint(after))
        self.assertEqual(probe.onboarding_checkpoint_phase(after), "after-color-scheme")
        self.assertTrue(probe.may_retain_color_checkpoint(
            {**after, "errorObserved": True, "errorCategories": ["generic-error"]}))
        self.assertTrue(probe.may_retain_color_checkpoint(
            {**after, "colorSchemeContinued": False}, source_phase="after-color-scheme"))
        for change in ({"colorSchemeContinued": False}, {"authBarrierObserved": True},
                       {"localPromptObserved": True}, {"terminalAmbiguityObserved": True},
                       {"errorObserved": True, "errorCategories": ["sandbox"]}):
            with self.subTest(change=change):
                self.assertFalse(probe.may_retain_color_checkpoint({**after, **change}))

    def test_capture_checkpoints_stable_after_color_screen_without_extra_input(self):
        with tempfile.TemporaryDirectory() as temp:
            audit = Path(temp) / "audit.jsonl"
            chunks = [
                b"Welcome to Antigravity\nChoose your color scheme:\n> Dark\nPress Enter to continue",
                b"\x1b[2J\x1b[HWelcome to Antigravity\nDo you trust this workspace directory?",
            ]
            ready_calls = [[77], [77]]
            sent = []
            ticks = {"value": 0.0}

            def monotonic():
                ticks["value"] += 0.4
                return ticks["value"]

            def select_ready(*_args):
                return (ready_calls.pop(0), [], []) if ready_calls else ([], [], [])

            with mock.patch.object(probe.select, "select", side_effect=select_ready), \
                 mock.patch.object(probe.os, "read", side_effect=lambda _fd, _size: chunks.pop(0)), \
                 mock.patch.object(probe.os, "write", side_effect=lambda _fd, data: sent.append(data) or len(data)), \
                 mock.patch.object(probe.time, "monotonic", side_effect=monotonic):
                state, enter_sent = probe._capture_login(
                    77, 100.0, enter_sent=True, audit_path=audit, case_root=temp,
                    resume_onboarding=True, resume_phase="color-scheme",
                )
            self.assertTrue(enter_sent)
            self.assertEqual(sent, [b"\r"])
            self.assertTrue(state["colorSchemeContinued"])
            self.assertTrue(state["trustPromptObserved"])
            events = [json.loads(line) for line in audit.read_text().splitlines()]
            stable = [event for event in events if event["event"] == "stable-onboarding-checkpoint-observed"]
            self.assertEqual(len(stable), 1)
            self.assertEqual(stable[0]["phase"], "after-color-scheme")

    def test_unchanged_theme_screen_after_enter_is_after_color_and_resume_does_not_repeat_enter(self):
        theme = b"Welcome to Antigravity\nChoose your color scheme:\n> Dark\nPress Enter to continue"

        def capture_once(temp, *, phase):
            audit = Path(temp) / f"{phase}.jsonl"
            chunks = [theme]
            ready_calls = [[77]]
            sent = []
            ticks = {"value": 0.0}

            def monotonic():
                ticks["value"] += 0.4
                return ticks["value"]

            def select_ready(*_args):
                return (ready_calls.pop(0), [], []) if ready_calls else ([], [], [])

            with mock.patch.object(probe.select, "select", side_effect=select_ready), \
                 mock.patch.object(probe.os, "read", side_effect=lambda _fd, _size: chunks.pop(0)), \
                 mock.patch.object(probe.os, "write", side_effect=lambda _fd, data: sent.append(data) or len(data)), \
                 mock.patch.object(probe.time, "monotonic", side_effect=monotonic):
                state, _ = probe._capture_login(
                    77, 100.0, enter_sent=True, audit_path=audit, case_root=temp,
                    resume_onboarding=True, resume_phase=phase,
                )
            events = [json.loads(line) for line in audit.read_text().splitlines()]
            checkpoint = next(event for event in events
                              if event["event"] == "stable-onboarding-checkpoint-observed")
            return state, sent, checkpoint["phase"]

        with tempfile.TemporaryDirectory() as temp:
            state, sent, phase = capture_once(temp, phase="color-scheme")
            self.assertTrue(state["colorSchemeScreenObserved"])
            self.assertTrue(state["colorSchemeContinued"])
            self.assertEqual(sent, [b"\r"])
            self.assertEqual(phase, "after-color-scheme")

            state2, sent2, phase2 = capture_once(temp, phase="after-color-scheme")
            self.assertTrue(state2["colorSchemeScreenObserved"])
            self.assertTrue(state2["colorSchemeContinued"])
            self.assertEqual(sent2, [])
            self.assertEqual(phase2, "after-color-scheme")

    def test_explicit_visual_restart_allows_one_guarded_enter_per_process(self):
        theme = (b"Welcome to Antigravity\r\nChoose your color scheme:\r\n"
                 b"> Dark\r\nPress Enter to continue")
        with tempfile.TemporaryDirectory() as temp:
            audit = Path(temp) / "restart.jsonl"
            chunks = [theme, theme]
            ready_calls = [[77], [77]]
            sent = []
            ticks = {"value": 0.0}

            def monotonic():
                ticks["value"] += 0.4
                return ticks["value"]

            def select_ready(*_args):
                return (ready_calls.pop(0), [], []) if ready_calls else ([], [], [])

            with mock.patch.object(probe.select, "select", side_effect=select_ready), \
                 mock.patch.object(probe.os, "read", side_effect=lambda _fd, _size: chunks.pop(0)), \
                 mock.patch.object(probe.os, "write", side_effect=lambda _fd, data: sent.append(data) or len(data)), \
                 mock.patch.object(probe.time, "monotonic", side_effect=monotonic):
                state, enter_sent = probe._capture_login(
                    77, 100.0, enter_sent=True, audit_path=audit, case_root=temp,
                    resume_onboarding=True, resume_phase="after-color-scheme",
                    restart_visual_setup=True,
                )
            self.assertTrue(enter_sent)
            self.assertEqual(sent, [b"\r"])
            self.assertTrue(state["colorSchemeContinued"])
            events = [json.loads(line) for line in audit.read_text().splitlines()]
            self.assertEqual(sum(event["event"] == "visual-setup-restart-authorized" for event in events), 1)
            continued = [event for event in events if event["event"] == "native-color-scheme-continued"]
            self.assertEqual(len(continued), 1)
            self.assertTrue(continued[0]["explicitRestart"])

    def test_preview_error_checkpoint_stays_blocked_and_manager_rejects_it(self):
        def framed(inner):
            return "│" + inner.ljust(72)[:72] + "│"

        preview = "\r\n".join((
            "Welcome to Antigravity", "Choose your color scheme:",
            framed("func main() {"), framed("✗ error: compilation failed"),
            framed("⚠ warning: deprecation warning"),
            framed("file:///tmp/aok-preview/main.go"), "Press Enter to continue",
            "Error: native backend unavailable",
        )).encode()
        state = probe.state_from_bytes(preview)
        self.assertEqual(state["previewSampleErrorRows"], [3])
        self.assertTrue(state["rawGenericErrorObserved"])
        self.assertTrue(state["errorObserved"])
        self.assertTrue(probe.may_retain_color_checkpoint(state))
        self.assertFalse(probe.may_continue_color_scheme(state))
        self.assertFalse(probe.is_ready(state))

        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp).resolve()
            root, marker = self._checkpoint(parent, phase="after-color-scheme")
            marker["valid"] = False
            marker["readyPromptObserved"] = False
            marker_path = root / "owned-session.json"
            marker_path.write_text(json.dumps(marker), encoding="utf-8")
            marker_path.chmod(0o600)
            with self.assertRaisesRegex(ValueError, "not currently prompt-ready"):
                probe.AGY.validate_isolated_login_root(root, private_temp_root=parent)

    def test_visual_restart_flag_requires_resume_and_rejects_new_code(self):
        with self.assertRaisesRegex(RuntimeError, "requires --resume-login-root"):
            probe.prepare(restart_visual_setup=True)
        with self.assertRaisesRegex(RuntimeError, "do not provide another code"):
            probe.prepare(authorization_code_stdin=True, resume_login_root="/unused",
                          restart_visual_setup=True)
        with mock.patch.object(probe, "_prepare", return_value={"status": "blocked"}) as prepare:
            result = probe.prepare(resume_login_root="/unused", restart_visual_setup=True)
        self.assertEqual(result["status"], "blocked")
        self.assertTrue(prepare.call_args.kwargs["restart_visual_setup"])

    @mock.patch.object(probe.sys, "platform", "darwin")
    @mock.patch.object(probe.os, "uname", lambda: os.uname_result(("Darwin", "", "", "", "x86_64")))
    def test_restart_preserves_invalid_checkpoint_expiry_when_preview_error_still_blocks(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp).resolve()
            root, original = self._checkpoint(parent, phase="after-color-scheme")
            expected_expiry = original["expiresAt"]
            audit_roots = []

            def owned(prefix):
                path = parent / f"{prefix}{len(audit_roots)}"
                path.mkdir(mode=0o700)
                audit_roots.append(path)
                return path

            blocked = {"colorSchemeScreenObserved": True, "colorSchemeContinueObserved": True,
                       "welcomeLabelObserved": True, "authBarrierObserved": False,
                       "localPromptObserved": False, "terminalAmbiguityObserved": False,
                       "errorObserved": True, "errorCategories": ["generic-error"],
                       "previewSampleErrorRows": [3], "rawGenericErrorObserved": True,
                       "authorizationCodeSubmitted": True}
            binary = parent / "vendor"
            binary.write_bytes(b"pinned test fixture")

            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", parent), \
                 mock.patch.object(probe, "_owned_dir", side_effect=owned), \
                 mock.patch.object(probe, "_record_dependencies", return_value={"prepare_agy_login.py": "a" * 64}), \
                 mock.patch.object(probe, "file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_preflight", return_value={"terminated": True}), \
                 mock.patch.object(probe, "run_login", return_value={"promptReady": False, "state": blocked}) as run:
                result = probe.prepare(binary, timeout=3, resume_login_root=root,
                                       retain_onboarding=True, restart_visual_setup=True)

            updated = json.loads((root / "owned-session.json").read_text())
            self.assertEqual(result["status"], "checkpoint")
            self.assertEqual(updated["expiresAt"], expected_expiry)
            self.assertEqual(updated["phase"], "after-color-scheme")
            self.assertFalse(updated["valid"])
            self.assertFalse(updated["readyPromptObserved"])
            self.assertTrue(run.call_args.kwargs["restart_visual_setup"])
            with self.assertRaisesRegex(ValueError, "not currently prompt-ready"):
                probe.AGY.validate_isolated_login_root(root, private_temp_root=parent)

    @mock.patch.object(probe.sys, "platform", "darwin")
    @mock.patch.object(probe.os, "uname", lambda: os.uname_result(("Darwin", "", "", "", "x86_64")))
    def test_prepare_retains_only_explicit_color_screen_diagnosis_and_resume_uses_fresh_audit(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp).resolve()
            binary = parent / "vendor"
            binary.write_bytes(b"pinned test fixture")
            made = []

            def owned(prefix):
                path = parent / f"{prefix}{len(made)}"
                path.mkdir(mode=0o700)
                path.chmod(0o700)
                made.append(path)
                return path

            checkpoint_state = {
                "colorSchemeScreenObserved": True, "colorSchemeContinueObserved": True,
                "welcomeLabelObserved": True, "authBarrierObserved": False,
                "localPromptObserved": False, "terminalAmbiguityObserved": False,
                "errorObserved": True, "errorCategories": ["generic-error"],
                "authorizationCodeSubmitted": True,
            }
            log_target = parent / "external-native-log-target"
            log_target.write_text("target content is not inspected")

            def retained_run(root, *_args, **_kwargs):
                probe.env_for(root)
                cli_dir = Path(root) / "home" / ".gemini" / "antigravity-cli"
                cli_dir.mkdir(parents=True, mode=0o700)
                cli_dir.chmod(0o700)
                (cli_dir / "cli.log").symlink_to(log_target)
                return {"promptReady": False, "state": checkpoint_state}

            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", parent), \
                 mock.patch.object(probe, "_owned_dir", side_effect=owned), \
                 mock.patch.object(probe, "_record_dependencies", return_value={"prepare_agy_login.py": "a" * 64}), \
                 mock.patch.object(probe, "file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_preflight", return_value={"terminated": True}), \
                 mock.patch.object(probe, "run_login", side_effect=retained_run) as run:
                retained = probe.prepare(binary, timeout=3, retain_onboarding=True,
                                         authorization_code_file=None)
            root = Path(retained["root"])
            audit1 = Path(retained["auditRoot"])
            marker = json.loads((root / "owned-session.json").read_text())
            self.assertEqual(retained["status"], "checkpoint")
            self.assertEqual(marker["phase"], "color-scheme")
            self.assertFalse(marker["valid"])
            self.assertFalse(marker["readyPromptObserved"])
            self.assertTrue((root / "home").is_dir())
            self.assertTrue(run.call_args.kwargs["retain_onboarding"])
            self.assertTrue(retained["nativeLogLinkRemoved"])
            self.assertFalse((root / "home" / ".gemini" / "antigravity-cli" / "cli.log").exists())
            self.assertEqual(log_target.read_text(), "target content is not inspected")

            for name in ("native-start.gate", "runner-started", "native-exec-attempted"):
                sentinel = root / name
                sentinel.write_text("owned sentinel\n")
                sentinel.chmod(0o600)
            unrelated = root / "unrelated"
            unrelated.write_text("preserve\n")
            unrelated.chmod(0o600)
            ready_state = {"localPromptObserved": True, "authBarrierObserved": False,
                           "errorObserved": False, "terminalAmbiguityObserved": False}
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", parent), \
                 mock.patch.object(probe, "_owned_dir", side_effect=owned), \
                 mock.patch.object(probe, "_record_dependencies", return_value={"prepare_agy_login.py": "b" * 64}), \
                 mock.patch.object(probe, "file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_preflight", return_value={"terminated": True}), \
                 mock.patch.object(probe, "run_login", return_value={"promptReady": True, "state": ready_state}) as resumed_run:
                resumed = probe.prepare(binary, timeout=3, resume_login_root=root)
            updated = json.loads((root / "owned-session.json").read_text())
            self.assertEqual(resumed["status"], "prompt-ready")
            self.assertNotEqual(Path(resumed["auditRoot"]), audit1)
            self.assertEqual(Path(resumed["root"]), root)
            self.assertTrue(updated["valid"])
            self.assertNotIn("phase", updated)
            self.assertTrue((root / "unrelated").exists())
            for name in ("native-start.gate", "runner-started", "native-exec-attempted"):
                self.assertFalse((root / name).exists())
            kwargs = resumed_run.call_args.kwargs
            self.assertTrue(kwargs["resume_onboarding"])
            self.assertFalse(kwargs["retain_onboarding"])
            self.assertIsNone(kwargs["authorization_code_fd"])

    def test_resume_sentinel_removal_rejects_symlink_and_preserves_target(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            target = root / "outside"
            target.write_text("preserve")
            (root / "runner-started").symlink_to(target)
            with self.assertRaisesRegex(RuntimeError, "sentinel is unsafe"):
                probe.reset_resume_sentinels(root)
            self.assertEqual(target.read_text(), "preserve")

    @mock.patch.object(probe.sys, "platform", "darwin")
    @mock.patch.object(probe.os, "uname", lambda: os.uname_result(("Darwin", "", "", "", "x86_64")))
    def test_resume_blocked_result_removes_home_without_explicit_retain(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp).resolve()
            root, _ = self._checkpoint(parent)
            binary = parent / "vendor"
            binary.write_bytes(b"pinned test fixture")
            made = []

            def owned(prefix):
                path = parent / f"{prefix}{len(made)}"
                path.mkdir(mode=0o700)
                path.chmod(0o700)
                made.append(path)
                return path

            theme_error = {"colorSchemeScreenObserved": True, "welcomeLabelObserved": True,
                           "authBarrierObserved": False, "localPromptObserved": False,
                           "terminalAmbiguityObserved": False, "errorObserved": True,
                           "errorCategories": ["generic-error"],
                           "authorizationCodeSubmitted": False}
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", parent), \
                 mock.patch.object(probe, "_owned_dir", side_effect=owned), \
                 mock.patch.object(probe, "_record_dependencies", return_value={"prepare_agy_login.py": "c" * 64}), \
                 mock.patch.object(probe, "file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_private_file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_preflight", return_value={"terminated": True}), \
                 mock.patch.object(probe, "run_login", return_value={"promptReady": False, "state": theme_error}):
                result = probe.prepare(binary, timeout=3, resume_login_root=root)
            self.assertEqual(result["status"], "blocked")
            self.assertFalse((root / "home").exists())
            marker = json.loads((root / "owned-session.json").read_text())
            self.assertFalse(marker["valid"])

    def test_run_login_closes_master_before_owned_group_cleanup(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            (root / "home").mkdir()
            (root / "tmp").mkdir()
            cli = root / "antigravity"
            cli.write_bytes(b"copied-cli")
            audit = root / "audit.jsonl"
            order = []
            proc = mock.Mock(pid=47201, returncode=0, poll=mock.Mock(return_value=0))
            ready = {"localPromptObserved": True, "authBarrierObserved": False,
                     "errorObserved": False, "readyHintObserved": True}
            def close(fd):
                order.append(f"close-{fd}")
            def cleanup(*_args, **_kwargs):
                order.append("cleanup")
            with mock.patch.object(probe.pty, "openpty", return_value=(80, 81)), \
                 mock.patch.object(probe.fcntl, "ioctl"), \
                 mock.patch.object(probe.os, "set_blocking"), \
                 mock.patch.object(probe.os, "ttyname", return_value="/dev/ttys008"), \
                 mock.patch.object(probe.subprocess, "Popen", return_value=proc) as popen, \
                 mock.patch.object(probe.CURSOR, "audit_spawned_child", return_value=47201), \
                 mock.patch.object(probe.AGY, "terminate_owned_group", side_effect=cleanup), \
                 mock.patch.object(probe, "_capture_login", return_value=(ready, False)), \
                 mock.patch.object(probe, "_disable_terminal_echo", return_value=["original-tty-state"]), \
                 mock.patch.object(probe, "_restore_terminal") as restore_terminal, \
                 mock.patch.object(probe.os, "close", side_effect=close):
                result = probe.run_login(root, audit, cli, timeout=20, authorization_code_fd=0)
            self.assertTrue(result["promptReady"])
            self.assertEqual(popen.call_args.kwargs["preexec_fn"], probe.AGY.claim_controlling_tty)
            self.assertTrue(popen.call_args.kwargs["start_new_session"])
            self.assertLess(order.index("close-80"), order.index("cleanup"))
            self.assertLess(order.index("close-81"), order.index("close-80"))
            restore_terminal.assert_called_once_with(0, ["original-tty-state"])

    def test_platform_guard_rejects_unsupported_host_before_preparation(self):
        with mock.patch.object(probe.sys, "platform", "linux"), \
             mock.patch.object(probe, "_owned_dir") as owned:
            with self.assertRaisesRegex(RuntimeError, "pinned for Darwin x64"):
                probe._prepare()
            owned.assert_not_called()

    @mock.patch.object(probe.sys, "platform", "darwin")
    @mock.patch.object(probe.os, "uname", lambda: os.uname_result(("Darwin", "", "", "", "x86_64")))
    def test_prepare_keeps_credentials_only_on_ready_and_invalidates_failure(self):
        with tempfile.TemporaryDirectory() as temp:
            tmp = Path(temp)
            binary = tmp / "vendor"
            binary.write_bytes(b"pinned test fixture")
            root_parent = tmp / "roots"
            root_parent.mkdir()
            audit_parent = tmp / "audits"
            audit_parent.mkdir()
            made = []
            def owned(prefix):
                p = root_parent / f"{prefix}{len(made)}"
                p.mkdir(mode=0o700)
                made.append(p)
                return p
            def ready_run(root, audit_path, cli, timeout, **_kwargs):
                probe.env_for(root)
                return {"promptReady": True, "state": {"localPromptObserved": True}}
            def blocked_run(root, audit_path, cli, timeout, **_kwargs):
                return {"promptReady": False, "state": {"errorObserved": True}}
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root_parent), \
                 mock.patch.object(probe, "_owned_dir", side_effect=owned), \
                 mock.patch.object(probe, "_record_dependencies", return_value={"prepare_agy_login.py": "a" * 64}), \
                 mock.patch.object(probe, "file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_preflight", return_value={"terminated": True}), \
                 mock.patch.object(probe, "run_login", side_effect=ready_run):
                result = probe.prepare(binary, timeout=3)
            marker = json.loads((Path(result["root"]) / "owned-session.json").read_text())
            self.assertEqual(result["status"], "prompt-ready")
            self.assertTrue(marker["valid"])
            self.assertTrue(marker["readyPromptObserved"])
            self.assertEqual(marker["backendAuthentication"], "not-independently-verified")
            self.assertEqual(result["backendAuthentication"], "not-independently-verified")
            self.assertTrue((Path(result["root"]) / "home").is_dir())

            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root_parent), \
                 mock.patch.object(probe, "_owned_dir", side_effect=owned), \
                 mock.patch.object(probe, "_record_dependencies", return_value={"prepare_agy_login.py": "a" * 64}), \
                 mock.patch.object(probe, "file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_preflight", return_value={"terminated": True}), \
                 mock.patch.object(probe, "run_login", side_effect=blocked_run):
                result = probe.prepare(binary, timeout=3)
            marker = json.loads((Path(result["root"]) / "owned-session.json").read_text())
            self.assertEqual(result["status"], "blocked")
            self.assertFalse(marker["valid"])
            self.assertFalse((Path(result["root"]) / "home").exists())

    @mock.patch.object(probe.sys, "platform", "darwin")
    @mock.patch.object(probe.os, "uname", lambda: os.uname_result(("Darwin", "", "", "", "x86_64")))
    def test_keyboard_interrupt_invalidates_marker_and_removes_private_home(self):
        with tempfile.TemporaryDirectory() as temp:
            tmp = Path(temp)
            binary = tmp / "vendor"
            binary.write_bytes(b"pinned test fixture")
            root_parent = tmp / "roots"
            root_parent.mkdir()
            audit_parent = tmp / "audits"
            audit_parent.mkdir()
            made = []

            def owned(prefix):
                p = root_parent / f"{prefix}{len(made)}"
                p.mkdir(mode=0o700)
                made.append(p)
                return p

            def interrupt_after_home(root, audit_path, cli, timeout, **_kwargs):
                probe.env_for(root)
                (Path(root) / "home" / "credential-placeholder").write_text("private")
                raise KeyboardInterrupt()

            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", root_parent), \
                 mock.patch.object(probe, "_owned_dir", side_effect=owned), \
                 mock.patch.object(probe, "_record_dependencies", return_value={"prepare_agy_login.py": "a" * 64}), \
                 mock.patch.object(probe, "file_sha", return_value=probe.PINNED_BINARY_SHA256), \
                 mock.patch.object(probe, "_preflight", return_value={"terminated": True}), \
                 mock.patch.object(probe, "run_login", side_effect=interrupt_after_home):
                with self.assertRaises(KeyboardInterrupt):
                    probe.prepare(binary, timeout=3)

            root = next(path for path in made
                        if path.name.startswith("aok-agy-session-") and "audit" not in path.name)
            marker = json.loads((root / "owned-session.json").read_text())
            self.assertFalse(marker["valid"])
            self.assertEqual(marker["backendAuthentication"], "not-independently-verified")
            self.assertFalse((root / "home").exists())

    def test_main_prints_only_status_and_private_roots(self):
        result = {"status": "blocked", "root": "/private/tmp/session", "auditRoot": "/private/tmp/audit",
                  "observation": {"nativeOutput": "must not print"}}
        with mock.patch.object(probe, "prepare", return_value=result), \
             mock.patch.object(sys, "stdout", new_callable=__import__("io").StringIO) as output:
            status = probe.main(["--timeout", "10"])
        self.assertEqual(status, 1)
        printed = json.loads(output.getvalue())
        self.assertEqual(set(printed), {"status", "root", "auditRoot"})
        self.assertNotIn("nativeOutput", output.getvalue())
        ready_result = {"status": "prompt-ready", "root": "/private/tmp/session",
                        "auditRoot": "/private/tmp/audit",
                        "backendAuthentication": "not-independently-verified"}
        with mock.patch.object(probe, "prepare", return_value=ready_result), \
             mock.patch.object(sys, "stdout", new_callable=__import__("io").StringIO):
            self.assertEqual(probe.main(["--timeout", "10"]), 0)
        checkpoint_result = {"status": "checkpoint", "root": "/private/tmp/session",
                             "auditRoot": "/private/tmp/audit"}
        with mock.patch.object(probe, "prepare", return_value=checkpoint_result), \
             mock.patch.object(sys, "stdout", new_callable=__import__("io").StringIO):
            self.assertEqual(probe.main(["--timeout", "10"]), 1)

    def test_fresh_private_session_root_name_cannot_satisfy_login_detector(self):
        for root_text in (
            "/private/tmp/aok-agy-session-12345678",
            "/private/tmp/aok-agy-session-1234…",
        ):
            with self.subTest(root_text=root_text):
                state = probe.AGY.state_from_bytes(
                    ("Welcome to Antigravity\r\nWorkspace: " + root_text).encode())
                self.assertFalse(state["authBarrierObserved"])
        old_root = probe.AGY.state_from_bytes(
            b"Welcome to Antigravity\r\nWorkspace: /private/tmp/aok-agy-login-1234")
        self.assertTrue(old_root["authBarrierObserved"])


if __name__ == "__main__":
    unittest.main()
