import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock
import base64
from datetime import datetime, timedelta, timezone
import uuid


DIRECTORY = Path(__file__).resolve().parents[1] / "scripts/native-loading"
sys.path.insert(0, str(DIRECTORY))
try:
    spec = importlib.util.spec_from_file_location("cursor_authenticated_probe", DIRECTORY / "probe_cursor_authenticated.py")
    probe = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(probe)
    import prepare_cursor_login as prepare
finally:
    sys.path.pop(0)


def token(expiry):
    return "test." + base64.urlsafe_b64encode(json.dumps({"exp": expiry}).encode()).decode().rstrip("=") + ".test"


class CredentialBoundaryTests(unittest.TestCase):
    def test_only_named_cursor_items_enter_private_native_store(self):
        access = token(time.time() + 3600)
        responses = [subprocess.CompletedProcess([], 0, access.encode() + b"\n", b""),
                     subprocess.CompletedProcess([], 0, b"synthetic-refresh\n", b"")]
        with tempfile.TemporaryDirectory() as temp:
            with mock.patch.object(probe.subprocess, "run", side_effect=responses) as run:
                auth = probe.copy_login(Path(temp))
            self.assertEqual(auth.stat().st_mode & 0o777, 0o600)
            self.assertEqual(auth.parent.stat().st_mode & 0o777, 0o700)
            self.assertEqual(json.loads(auth.read_text()), {"accessToken": access, "refreshToken": "synthetic-refresh"})
            self.assertEqual([call.args[0] for call in run.call_args_list], [
                ["/usr/bin/security", "find-generic-password", "-w", "-s", "cursor-access-token", "-a", "cursor-user"],
                ["/usr/bin/security", "find-generic-password", "-w", "-s", "cursor-refresh-token", "-a", "cursor-user"],
            ])

    def test_invalid_credential_encoding_never_appears_in_error(self):
        secret = b"never-expose-this-credential-\xff"
        with tempfile.TemporaryDirectory() as temp:
            with mock.patch.object(probe.subprocess, "run", return_value=subprocess.CompletedProcess([], 0, secret, b"")):
                with self.assertRaises(RuntimeError) as raised:
                    probe.copy_login(Path(temp))
            self.assertNotIn("never-expose", str(raised.exception))
            self.assertTrue(raised.exception.__suppress_context__)
            self.assertFalse((Path(temp) / ".cursor/auth.json").exists())

    def test_expired_login_is_not_copied_or_refreshed(self):
        responses = [subprocess.CompletedProcess([], 0, token(0).encode(), b""),
                     subprocess.CompletedProcess([], 0, b"synthetic-refresh", b"")]
        with tempfile.TemporaryDirectory() as temp:
            with mock.patch.object(probe.subprocess, "run", side_effect=responses) as run:
                with self.assertRaisesRegex(RuntimeError, "fresh browser login"):
                    probe.copy_login(Path(temp))
            self.assertEqual(run.call_count, 2)
            self.assertFalse((Path(temp) / ".cursor/auth.json").exists())


class MenuBoundaryTests(unittest.TestCase):
    @unittest.skipUnless(hasattr(probe.termios, "TIOCSCTTY"), "requires POSIX controlling terminals")
    def test_detached_child_really_owns_its_controlling_terminal(self):
        master, slave = probe.pty.openpty()
        try:
            process = subprocess.Popen([sys.executable, "-B", "-c",
                                        "import os; f=os.open('/dev/tty',os.O_RDWR); assert os.isatty(f); assert os.tcgetpgrp(f)==os.getpgrp(); os.close(f); print('owned-tty')"],
                                       stdin=slave, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                       start_new_session=True, preexec_fn=probe.claim_controlling_terminal)
            out, err = process.communicate(timeout=5)
            self.assertEqual(process.returncode, 0, err.decode())
            self.assertEqual(out.strip(), b"owned-tty")
        finally:
            os.close(master)
            os.close(slave)

    def inspect(self, chunks):
        with tempfile.TemporaryDirectory() as temp:
            proc = mock.Mock(pid=23456)
            with mock.patch.object(probe.pty, "openpty", return_value=(90, 91)), \
                 mock.patch.object(probe.fcntl, "ioctl"), \
                 mock.patch.object(probe, "guard", return_value=[]), \
                 mock.patch.object(probe.subprocess, "Popen", return_value=proc), \
                 mock.patch.object(probe.safe, "audit_spawned_child", return_value=23456), \
                 mock.patch.object(probe.safe, "terminate_owned_group") as cleanup, \
                 mock.patch.object(probe.select, "select", return_value=([90], [], [])), \
                 mock.patch.object(probe.os, "read", side_effect=chunks), \
                 mock.patch.object(probe.os, "set_blocking"), \
                 mock.patch.object(probe.os, "ttyname", return_value="/dev/ttys-owned-fixture"), \
                 mock.patch.object(probe.os, "write") as write, \
                 mock.patch.object(probe.os, "close"):
                try:
                    result = probe.inspect_menu(Path("copied-cli"), Path(temp), Path(temp), {})
                finally:
                    cleanup.assert_called_once_with(proc, 23456, "native-slash-menu")
            return result, write

    def test_terminal_responses_are_fixed_protocol_only(self):
        self.assertEqual(probe.terminal_responses(b"ordinary text or /agents"), [])
        self.assertEqual(probe.terminal_responses(b"\x1b[6n"), [(b"\x1b[1;1R", "cursor-position")])

    def test_menu_filter_never_submits_a_prompt(self):
        result, write = self.inspect([b"/ for commands", b"reviewer Reviews changes for correctness and security."])
        write.assert_called_once_with(90, b"/reviewer")
        self.assertFalse(result["enterSent"])
        self.assertTrue(result["expectedDescriptionObserved"])

    def test_header_only_slash_does_not_claim_ready_without_menu_response(self):
        result, write = self.inspect([b"Cursor Agent Auto", b""])
        self.assertEqual([call.args[1] for call in write.call_args_list], [b"/"])
        self.assertFalse(result["readyPromptObserved"])
        self.assertFalse(result["enterSent"])

    def test_header_slash_filters_only_after_native_menu_response(self):
        result, write = self.inspect([b"Cursor Agent Auto", b"\r\nmodels help", b""])
        self.assertEqual([call.args[1] for call in write.call_args_list], [b"/", b"reviewer"])
        self.assertTrue(result["readyPromptObserved"])
        self.assertFalse(result["enterSent"])

    def test_audit_failure_still_closes_terminal_and_stops_child(self):
        with mock.patch.object(probe.safe, "append_audit", side_effect=OSError("audit failed")):
            with self.assertRaisesRegex(OSError, "audit failed"):
                self.inspect([b"Press any key to log in..."])

    def test_authentication_barrier_never_receives_keys(self):
        result, write = self.inspect([b"Press any key to log in..."])
        write.assert_not_called()
        self.assertTrue(result["loginBarrierObserved"])

    def test_ambiguous_terminal_stream_blocks_native_menu_input(self):
        result, write = self.inspect([
            b"cursor auto ask anything\r\n" + "❯ ".encode() + b"\x1b[?1047h",
            b"",
        ])
        write.assert_not_called()
        self.assertFalse(result["readyPromptObserved"])
        self.assertTrue(result["terminalAmbiguityObserved"])
        self.assertTrue(result["cleanupCompleted"])

    def test_concealed_prompt_cannot_authorize_native_menu_input(self):
        result, write = self.inspect([
            b"cursor auto ask anything\r\n\x1b[8m\xe2\x9d\xaf /reviewer\x1b[28m",
            b"",
        ])
        write.assert_not_called()
        self.assertFalse(result["readyPromptObserved"])
        self.assertTrue(result["terminalAmbiguityObserved"])


class GitPolicyBoundaryTests(unittest.TestCase):
    def test_existing_shim_directory_is_refused_without_mutation(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            tools = probe.prepare_git_policy(root)
            shim = tools / "git"
            original = shim.read_bytes()
            mode = shim.stat().st_mode & 0o777
            with self.assertRaises(FileExistsError):
                probe.prepare_git_policy(root)
            self.assertEqual(shim.read_bytes(), original)
            self.assertEqual(shim.stat().st_mode & 0o777, mode)
            self.assertEqual(mode, 0o500)
            self.assertNotIn("https://", original.decode())

    def test_startup_diagnostics_never_retain_tokens_or_urls(self):
        token_value = "aok-private-token-value-123"
        url_value = "https://private.example/path?access_token=hidden-value"
        captured = (f"warning: model unavailable {url_value} {token_value}\n".encode() +
                    b"\x1b[31mpermission denied\x1b[0m")
        result = probe.classify(captured)
        encoded = json.dumps(result)
        self.assertTrue(result["fixedWordsObserved"])
        self.assertFalse(result["rawTextRetained"])
        self.assertNotIn(token_value, encoded)
        self.assertNotIn(url_value, encoded)
        self.assertNotIn("hidden-value", encoded)


class PreparedLoginTests(unittest.TestCase):
    def create_session(self, root):
        root.chmod(0o700)
        directory = root / "home/.cursor"
        directory.mkdir(parents=True, mode=0o700)
        directory.parent.chmod(0o700)
        auth = directory / "auth.json"
        prepare.write_private_json(auth, {"accessToken": "test", "refreshToken": "test"})
        now = datetime.now(timezone.utc)
        marker = {"kind": "aok-cursor-native-login/v1", "id": str(uuid.uuid4()), "valid": True,
                  "createdAt": now.isoformat(), "expiresAt": (now + timedelta(minutes=8)).isoformat()}
        prepare.write_private_json(root / "owned-session.json", marker)
        return auth, marker

    def test_prepared_credentials_require_private_regular_files_and_live_marker(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            auth, marker = self.create_session(root)
            self.assertEqual(probe.validate_login_root(root), auth)
            auth.chmod(0o644)
            with self.assertRaisesRegex(RuntimeError, "permissions"):
                probe.validate_login_root(root)
            auth.chmod(0o600)
            marker["expiresAt"] = (datetime.now(timezone.utc) - timedelta(minutes=1)).isoformat()
            prepare.write_private_json(root / "owned-session.json", marker)
            with self.assertRaisesRegex(RuntimeError, "expired"):
                probe.validate_login_root(root)

    def test_symlink_credential_is_refused_before_read(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            auth, _ = self.create_session(root)
            original = root / "private-source.json"
            auth.rename(original)
            auth.symlink_to(original)
            with self.assertRaisesRegex(RuntimeError, "type"):
                probe.validate_login_root(root)

    def test_cleanup_failure_cannot_leave_login_credentials_valid(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            auth, _ = self.create_session(root)
            proc = mock.Mock(pid=23457, returncode=0, stdout=mock.Mock(), stderr=mock.Mock())
            proc.communicate.return_value = (b"not retained", b"")
            with mock.patch.object(prepare.auth, "guard", return_value=[]), \
                 mock.patch.object(prepare.subprocess, "Popen", return_value=proc), \
                 mock.patch.object(prepare.safe, "require_sha"), \
                 mock.patch.object(prepare.safe, "audit_spawned_child", return_value=23457), \
                 mock.patch.object(prepare.safe, "terminate_owned_group", side_effect=RuntimeError("cleanup failed")), \
                 mock.patch("builtins.print"):
                with self.assertRaisesRegex(RuntimeError, "cleanup failed"):
                    prepare.run_login(root, Path("copied-cli"), {})
            self.assertFalse(auth.exists())
            self.assertFalse(json.loads((root / "owned-session.json").read_text())["valid"])


if __name__ == "__main__":
    unittest.main()
