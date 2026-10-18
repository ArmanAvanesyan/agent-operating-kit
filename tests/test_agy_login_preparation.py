import importlib.util
import json
import os
from datetime import datetime, timedelta, timezone
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
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
                        if path.name.startswith("aok-agy-login-") and "audit" not in path.name)
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


if __name__ == "__main__":
    unittest.main()
