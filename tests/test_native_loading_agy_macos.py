import importlib.util
import json
import os
import pty
from pathlib import Path
import subprocess
import sys
import tempfile
from datetime import datetime, timedelta, timezone
import uuid
from types import SimpleNamespace
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SCRIPT = ROOT / "scripts/native-loading/probe-agy-macos-interactive-manager.py"
sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location("agy_macos_interactive_probe", SCRIPT)
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


class AgyMacOSInteractiveTests(unittest.TestCase):
    def test_profile_is_write_confined_and_limits_keychain_mach_lookup(self):
        with tempfile.TemporaryDirectory() as temp:
            profile = probe.sbpl(Path(temp))
        self.assertIn("(deny file-write*)", profile)
        self.assertIn("(deny mach-lookup)", profile)
        self.assertIn('global-name "com.apple.securityd"', profile)
        self.assertNotIn("deny network", profile)

    def _make_prompt_ready_profile(self, parent, *, expires_delta=timedelta(minutes=10),
                                   valid=True, ready=True):
        parent = Path(parent).resolve()
        profile = parent / f"login-profile-{uuid.uuid4()}"
        profile.mkdir(mode=0o700)
        home = profile / "home"
        home.mkdir(mode=0o700)
        config = home / ".config"
        config.mkdir(mode=0o700)
        (config / "profile.json").write_text('{"example":"private"}')
        (config / "profile.json").chmod(0o600)
        created = datetime.now(timezone.utc) - timedelta(minutes=1)
        marker = {
            "kind": "aok-agy-native-login/v1",
            "id": str(uuid.uuid4()),
            "createdAt": created.isoformat().replace("+00:00", "Z"),
            "expiresAt": (created + expires_delta).isoformat().replace("+00:00", "Z"),
            "valid": valid,
            "readyPromptObserved": ready,
            "backendAuthentication": "not-independently-verified",
        }
        marker_path = profile / "owned-session.json"
        marker_path.write_text(json.dumps(marker))
        marker_path.chmod(0o600)
        return profile, marker

    def test_isolated_login_profile_marker_and_copy_are_private_and_bounded(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp)
            source, _ = self._make_prompt_ready_profile(parent)
            destination = parent / "manager-home"
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", parent):
                validated = probe.validate_isolated_login_root(source)
                copied = probe.copy_isolated_login_home(source, destination)
            self.assertEqual(validated["home"], source / "home")
            self.assertEqual(copied, {"filesCopied": 1, "bytesCopied": len('{"example":"private"}')})
            self.assertEqual((destination / ".config/profile.json").read_text(), '{"example":"private"}')
            self.assertEqual((destination / ".config/profile.json").stat().st_mode & 0o777, 0o600)
            self.assertEqual((destination / ".config").stat().st_mode & 0o777, 0o700)

    def test_isolated_profile_rejects_invalid_marker_and_expired_profile(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp)
            profile, _ = self._make_prompt_ready_profile(parent, valid=False)
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", parent):
                with self.assertRaisesRegex(ValueError, "not currently prompt-ready"):
                    probe.validate_isolated_login_root(profile)
                profile2, marker2 = self._make_prompt_ready_profile(parent, expires_delta=timedelta(seconds=10))
                marker2["createdAt"] = (datetime.now(timezone.utc) - timedelta(minutes=20)).isoformat().replace("+00:00", "Z")
                marker2["expiresAt"] = (datetime.now(timezone.utc) - timedelta(minutes=19)).isoformat().replace("+00:00", "Z")
                (profile2 / "owned-session.json").write_text(json.dumps(marker2))
                (profile2 / "owned-session.json").chmod(0o600)
                with self.assertRaisesRegex(ValueError, "not currently prompt-ready"):
                    probe.validate_isolated_login_root(profile2)

    def test_isolated_profile_copy_rejects_symlinks_and_hardlinks(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp)
            source, _ = self._make_prompt_ready_profile(parent)
            extra = source / "home/.config/linked"
            extra.symlink_to(source / "home/.config/profile.json")
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", parent):
                with self.assertRaisesRegex(ValueError, "unsupported file entry"):
                    probe.copy_isolated_login_home(source, parent / "dest-symlink")
            extra.unlink()
            os.link(source / "home/.config/profile.json", source / "home/.config/hardlink")
            with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", parent):
                with self.assertRaisesRegex(ValueError, "unsupported file entry"):
                    probe.copy_isolated_login_home(source, parent / "dest-hardlink")

    def test_isolated_profile_sandbox_denies_keychain_and_source_profile(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "manager"
            source = Path(temp) / "login"
            root.mkdir()
            source.mkdir()
            profile = probe.sbpl_isolated_profile(root, source)
        self.assertIn('(deny mach-lookup (global-name \"com.apple.securityd\"))', profile)
        self.assertNotIn('(allow mach-lookup', profile)
        self.assertIn(f'(deny file-read* (subpath {json.dumps(str(source.resolve()))}))', profile)
        self.assertIn('(deny file-write*)', profile)

    def test_candidate_requires_manager_identity_and_all_controls(self):
        pos = {"commandSent": True, "managerSectionObserved": True, "nameObserved": True,
               "descriptionObserved": True, "authBarrierObserved": False, "errorObserved": False,
               "parseErrorObserved": False}
        absent = {**pos, "nameObserved": False, "descriptionObserved": False}
        malformed = {**absent, "parseErrorObserved": True}
        self.assertTrue(probe.has_candidate_manager_evidence({
            "positive": pos, "absent": absent, "malformed": malformed,
        }))
        self.assertFalse(probe.has_candidate_manager_evidence({"positive": pos, "absent": absent}))
        self.assertFalse(probe.has_candidate_manager_evidence({
            "positive": {**pos, "nameObserved": False}, "absent": absent, "malformed": malformed,
        }))
        self.assertFalse(probe.has_candidate_manager_evidence({
            "positive": pos, "absent": absent, "malformed": {**malformed, "authBarrierObserved": True},
        }))

    def test_auth_barrier_never_counts_as_local_ready_prompt(self):
        state = probe.state_from_bytes(b"Sign in to continue\r\n\xe2\x9d\xaf ")
        self.assertTrue(state["authBarrierObserved"])
        self.assertFalse(state["localPromptObserved"])
        self.assertFalse(probe.qualifies_positive({**state, "commandSent": False}))

    def test_ambiguous_terminal_stream_cannot_authorize_manager_command(self):
        state = probe.state_from_bytes(b"Ask anything\r\n" + "❯ ".encode() + b"\x1b[?1047h")
        self.assertTrue(state["terminalAmbiguityObserved"])
        self.assertFalse(state["localPromptObserved"])
        self.assertFalse(probe.may_send_manager_command(state))
        self.assertFalse(probe.qualifies_positive({**state, "commandSent": True}))

    def test_concealed_manager_prompt_cannot_authorize_command(self):
        state = probe.state_from_bytes(b"Ask anything\r\n\x1b[8m\xe2\x9d\xaf \x1b[28m")
        self.assertTrue(state["terminalAmbiguityObserved"])
        self.assertFalse(state["localPromptObserved"])
        self.assertFalse(probe.may_send_manager_command(state))

    def test_prompt_requires_native_marker_plus_ready_hint(self):
        self.assertTrue(probe.state_from_bytes(b"Ask anything\r\n\xe2\x9d\xaf ")["localPromptObserved"])
        self.assertFalse(probe.state_from_bytes(b"banner with >")["localPromptObserved"])
        self.assertFalse(probe.state_from_bytes(b"\xe2\x9d\xaf ")["localPromptObserved"])
        self.assertFalse(probe.state_from_bytes(b"Type a message")["localPromptObserved"])

    def test_errors_are_reduced_to_fixed_sanitized_categories(self):
        state = probe.state_from_bytes(
            b"Error: sandbox-exec: Operation not permitted for /Users/private/account\n"
            b"Traceback (most recent call last):\n"
        )
        self.assertTrue(state["errorObserved"])
        self.assertEqual(state["errorCategories"], ["sandbox", "python", "generic-error"])
        self.assertNotIn("text", state)
        self.assertNotIn("account", json.dumps(state))

    def test_preflight_fails_closed_before_any_native_case(self):
        with tempfile.TemporaryDirectory() as temp:
            with mock.patch.object(probe, "signal_preflight", side_effect=RuntimeError("EPERM")), \
                 mock.patch.object(probe, "run_manager_case") as run_case:
                with self.assertRaisesRegex(RuntimeError, "EPERM"):
                    probe.run_all_cases(Path(temp), Path(temp), Path(temp) / "antigravity")
            run_case.assert_not_called()

    def test_output_path_outside_repository_is_rejected_before_creation(self):
        with tempfile.TemporaryDirectory() as temp:
            outside = Path(temp) / "must-not-exist"
            evidence = outside / "evidence.json"
            audit_path = ROOT / "docs/evidence/native-adapters/never-written.jsonl"
            prior_argv = sys.argv
            try:
                sys.argv = [str(SCRIPT), "--aok", str(Path(temp) / "missing-aok"),
                            "--binary", str(Path(temp) / "missing-binary"),
                            "--evidence-out", str(evidence), "--audit-out", str(audit_path)]
                with mock.patch.object(probe.sys, "platform", "darwin"), \
                     mock.patch.object(probe.os, "uname", return_value=SimpleNamespace(machine="x86_64")):
                    with self.assertRaisesRegex(SystemExit, "inside this checkout"):
                        probe.main()
            finally:
                sys.argv = prior_argv
            self.assertFalse(outside.exists())
            self.assertFalse(audit_path.exists())

    def test_process_identity_is_audited_before_gate_release_and_auth_does_not_send(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            project = root / "project"
            project.mkdir()
            proc = mock.Mock(pid=49201, poll=mock.Mock(return_value=None))
            order = []
            def append(event, **kwargs):
                order.append(event)
            def close_fd(fd):
                order.append(f"close-{fd}")
            def cleanup(*_args, **_kwargs):
                order.append("cleanup")
            with mock.patch.object(probe.pty, "openpty", return_value=(90, 91)), \
                 mock.patch.object(probe.fcntl, "ioctl"), \
                 mock.patch.object(probe.os, "set_blocking"), \
                 mock.patch.object(probe.subprocess, "Popen", return_value=proc) as popen, \
                 mock.patch.object(probe.audit, "audit_spawned_child", return_value=49201) as audited, \
                 mock.patch.object(probe, "terminate_owned_group", side_effect=cleanup), \
                 mock.patch.object(probe, "append_audit", side_effect=append), \
                 mock.patch.object(probe.Path, "touch", autospec=True, side_effect=lambda p: order.append("gate")), \
                 mock.patch.object(probe, "_capture", return_value=b"Sign in to continue\r\n\xe2\x9d\xaf "), \
                 mock.patch.object(probe.os, "write") as write, \
                 mock.patch.object(probe.os, "close", side_effect=close_fd):
                state = probe.run_manager_case(root, project, root / "antigravity", "positive")
            audited.assert_called_once()
            command = popen.call_args.args[0]
            self.assertIn(str(root / "antigravity"), command)
            self.assertNotIn("--workspace", command)
            self.assertEqual(popen.call_args.kwargs["cwd"], project)
            self.assertEqual(popen.call_args.kwargs["preexec_fn"], probe.claim_controlling_tty)
            self.assertLess(order.index("native-startup-gated"), order.index("gate"))
            self.assertLess(order.index("close-90"), order.index("cleanup"))
            write.assert_not_called()
            popen.assert_called_once()
            self.assertFalse(state["commandSent"])

    def test_audit_failure_reaps_before_no_case_result(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            project = root / "project"
            project.mkdir()
            proc = mock.Mock(pid=49202)
            proc.send_signal = mock.Mock()
            proc.wait = mock.Mock()
            proc.poll.return_value = 0
            with mock.patch.object(probe.pty, "openpty", return_value=(90, 91)), \
                 mock.patch.object(probe.fcntl, "ioctl"), \
                 mock.patch.object(probe.os, "set_blocking"), \
                 mock.patch.object(probe.subprocess, "Popen", return_value=proc), \
                 mock.patch.object(probe.audit, "audit_spawned_child", side_effect=RuntimeError("audit failed")), \
                 mock.patch.object(probe.os, "close"):
                with self.assertRaisesRegex(RuntimeError, "audit failed"):
                    probe.run_manager_case(root, project, root / "antigravity", "positive")

    def test_cleanup_skips_sigkill_when_owned_group_is_confirmed_absent(self):
        proc = mock.Mock(pid=49203, poll=mock.Mock(return_value=0))
        with tempfile.TemporaryDirectory() as temp, \
             mock.patch.object(probe, "AUDIT_PATH", Path(temp) / "audit.jsonl"), \
             mock.patch.object(probe.os, "killpg", return_value=None) as killpg, \
             mock.patch.object(probe.subprocess, "run", return_value=subprocess.CompletedProcess([], 1, "", "")):
            probe.terminate_owned_group(proc, 49203, "test-absent-group")
            killpg.assert_called_once_with(49203, probe.signal.SIGTERM)
            self.assertIn('"groupCleanupFailed": false', (Path(temp) / "audit.jsonl").read_text())

    def test_cleanup_kills_and_verifies_surviving_group(self):
        proc = mock.Mock(pid=49204, poll=mock.Mock(return_value=0))
        with tempfile.TemporaryDirectory() as temp, \
             mock.patch.object(probe, "AUDIT_PATH", Path(temp) / "audit.jsonl"), \
             mock.patch.object(probe.os, "killpg", return_value=None) as killpg, \
             mock.patch.object(probe.subprocess, "run", side_effect=[
                 subprocess.CompletedProcess([], 0, "49206\n", ""),
                 subprocess.CompletedProcess([], 0, f"{os.getuid()} 49204\n", ""),
                 subprocess.CompletedProcess([], 1, "", ""),
             ]):
            probe.terminate_owned_group(proc, 49204, "test-surviving-group")
            self.assertEqual(killpg.call_args_list, [
                mock.call(49204, probe.signal.SIGTERM), mock.call(49204, probe.signal.SIGKILL),
            ])

    def test_cleanup_permission_error_remains_invalid(self):
        proc = mock.Mock(pid=49205, poll=mock.Mock(return_value=0))
        with tempfile.TemporaryDirectory() as temp, \
             mock.patch.object(probe, "AUDIT_PATH", Path(temp) / "audit.jsonl"), \
             mock.patch.object(probe.os, "killpg", side_effect=PermissionError("EPERM")), \
             mock.patch.object(probe.subprocess, "run", return_value=subprocess.CompletedProcess([], 2, "", "error")), \
             mock.patch.object(probe.audit, "direct_child_cleanup", return_value=True):
            with self.assertRaisesRegex(RuntimeError, "descendants may remain"):
                probe.terminate_owned_group(proc, 49205, "test-uncertain-group")
            record = (Path(temp) / "audit.jsonl").read_text()
            self.assertIn('"groupCleanupFailed": true', record)
            self.assertIn('"descendantsMayRemain": true', record)

    def test_sigterm_permission_error_is_success_only_if_metadata_proves_absence(self):
        proc = mock.Mock(pid=49207, poll=mock.Mock(return_value=0))
        with tempfile.TemporaryDirectory() as temp, \
             mock.patch.object(probe, "AUDIT_PATH", Path(temp) / "audit.jsonl"), \
             mock.patch.object(probe.os, "killpg", side_effect=PermissionError("EPERM")), \
             mock.patch.object(probe.subprocess, "run", return_value=subprocess.CompletedProcess([], 1, "", "")):
            probe.terminate_owned_group(proc, 49207, "test-term-denied-absent")
            record = (Path(temp) / "audit.jsonl").read_text()
            self.assertIn('"groupAbsentVerified": true', record)
            self.assertIn('"priorSignalErrors": ["PermissionError"]', record)

    def test_case_observation_is_fsynced_before_cleanup_failure(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            project = root / "project"
            project.mkdir()
            audit_path = root / "audit.jsonl"
            proc = mock.Mock(pid=49208, poll=mock.Mock(return_value=0))
            chunks = [b"Ask anything\r\n\xe2\x9d\xaf ",
                      b"Available Agents\naok-probe\nSynthetic AGY adapter discovery fixture"]
            def cleanup(*_args, **_kwargs):
                text = audit_path.read_text()
                self.assertIn('"event": "native-case-observed"', text)
                raise RuntimeError("cleanup invalid")
            with mock.patch.object(probe, "AUDIT_PATH", audit_path), \
                 mock.patch.object(probe.pty, "openpty", return_value=(90, 91)), \
                 mock.patch.object(probe.fcntl, "ioctl"), \
                 mock.patch.object(probe.os, "set_blocking"), \
                 mock.patch.object(probe.subprocess, "Popen", return_value=proc), \
                 mock.patch.object(probe.audit, "audit_spawned_child", return_value=49208), \
                 mock.patch.object(probe, "append_audit", wraps=probe.append_audit), \
                 mock.patch.object(probe.Path, "touch", autospec=True), \
                 mock.patch.object(probe, "_capture", side_effect=chunks), \
                 mock.patch.object(probe.os, "write"), \
                 mock.patch.object(probe.os, "close"), \
                 mock.patch.object(probe, "terminate_owned_group", side_effect=cleanup):
                with self.assertRaisesRegex(RuntimeError, "cleanup invalid"):
                    probe.run_manager_case(root, project, root / "antigravity", "positive")
            audit_lines = audit_path.read_text().splitlines()
            event = next(json.loads(line) for line in audit_lines
                         if '"native-case-observed"' in line)
            self.assertEqual(event["state"]["commandSent"], True)
            self.assertTrue(event["state"]["nameObserved"])


if __name__ == "__main__":
    unittest.main()
