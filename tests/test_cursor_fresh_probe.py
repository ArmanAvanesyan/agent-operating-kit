import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / "scripts/native-loading/probe_cursor_fresh_cli.py"
SPEC = importlib.util.spec_from_file_location("cursor_probe", SCRIPT)
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


class ProbeProcessSafetyTests(unittest.TestCase):
    def test_preflight_records_owned_group_before_signal_failure(self):
        with tempfile.TemporaryDirectory() as temp:
            proc = mock.Mock(pid=48123, poll=mock.Mock(return_value=None))
            with mock.patch.object(probe.subprocess, "Popen", return_value=proc), \
                 mock.patch.object(probe.os, "getpgid", return_value=48123), \
                 mock.patch.object(probe.os, "killpg", side_effect=PermissionError("EPERM")):
                with self.assertRaisesRegex(RuntimeError, "preflight failed"):
                    probe.signal_preflight(Path(temp))
            records = [json.loads(line) for line in (Path(temp) / "process-audit.jsonl").read_text().splitlines()]
            self.assertTrue(any(r.get("pid") == 48123 and r.get("expectedPGID") == 48123 for r in records))
            self.assertEqual([call.args[0] for call in proc.send_signal.call_args_list], [
                probe.signal.SIGTERM, probe.signal.SIGKILL,
            ])
            self.assertTrue(any(record.get("groupCleanupFailed") for record in records))

    def test_direct_fallback_still_attempts_kill_after_term_permission_error(self):
        proc = mock.Mock(pid=48130)
        proc.send_signal.side_effect = [PermissionError("EPERM"), None]
        proc.wait.side_effect = [subprocess.TimeoutExpired(["x"], 0.5), 0]
        proc.poll.side_effect = [None, 0]
        self.assertTrue(probe.direct_child_cleanup(proc))
        self.assertEqual([call.args[0] for call in proc.send_signal.call_args_list], [
            probe.signal.SIGTERM, probe.signal.SIGKILL,
        ])

    def test_group_audit_failure_prevents_communication(self):
        with tempfile.TemporaryDirectory() as temp:
            proc = mock.Mock(pid=48124)
            audit = Path(temp) / "process-audit.jsonl"
            def clean_direct(_proc, _pgid, purpose, **_kwargs):
                proc.send_signal(probe.signal.SIGTERM)
                proc.wait(timeout=0.5)
                (Path(temp) / "cleanup.jsonl").write_text(purpose)
            with mock.patch.object(probe, "AUDIT_PATH", audit), \
                 mock.patch.object(probe.subprocess, "Popen", return_value=proc), \
                 mock.patch.object(probe.os, "getpgid", side_effect=ProcessLookupError("gone")), \
                 mock.patch.object(probe, "terminate_owned_group", side_effect=clean_direct):
                with self.assertRaisesRegex(RuntimeError, "audit failed"):
                    probe.run_bounded(["host"], Path(temp))
            proc.communicate.assert_not_called()
            proc.send_signal.assert_called_once_with(probe.signal.SIGTERM)
            self.assertTrue((Path(temp) / "cleanup.jsonl").exists())
            records = [json.loads(line) for line in audit.read_text().splitlines()]
            self.assertEqual(records[0]["pid"], 48124)

    def test_timeout_terminates_then_kills_only_owned_group(self):
        with tempfile.TemporaryDirectory() as temp:
            proc = mock.Mock(pid=48125)
            proc.communicate.side_effect = [subprocess.TimeoutExpired(["x"], 1), (b"late", None)]
            proc.wait.side_effect = [subprocess.TimeoutExpired(["x"], 1), 0]
            with mock.patch.object(probe, "AUDIT_PATH", Path(temp) / "audit.jsonl"), \
                 mock.patch.object(probe.subprocess, "Popen", return_value=proc), \
                 mock.patch.object(probe.os, "getpgid", return_value=48125), \
                 mock.patch.object(probe.os, "killpg") as killpg:
                with self.assertRaisesRegex(RuntimeError, "timed out.*pid=48125 pgid=48125"):
                    probe.run_bounded(["host"], Path(temp), timeout=0.01)
            self.assertEqual([call.args for call in killpg.call_args_list], [
                (48125, probe.signal.SIGTERM), (48125, probe.signal.SIGKILL),
            ])
            self.assertEqual(proc.communicate.call_count, 2)

    def test_successful_parent_exit_still_cleans_background_process_group(self):
        with tempfile.TemporaryDirectory() as temp:
            proc = mock.Mock(pid=48128, returncode=0)
            proc.communicate.return_value = (b"ok", None)
            with mock.patch.object(probe, "AUDIT_PATH", Path(temp) / "audit.jsonl"), \
                 mock.patch.object(probe.subprocess, "Popen", return_value=proc), \
                 mock.patch.object(probe.os, "getpgid", return_value=48128), \
                 mock.patch.object(probe.os, "killpg") as killpg:
                code, output = probe.run_bounded(["host"], Path(temp))
            self.assertEqual((code, output), (0, b"ok"))
            self.assertEqual([call.args for call in killpg.call_args_list], [
                (48128, probe.signal.SIGTERM), (48128, probe.signal.SIGKILL),
            ])
            proc.communicate.assert_called_once()

    def test_detached_pipe_timeout_closes_pipe_without_unbounded_communicate(self):
        with tempfile.TemporaryDirectory() as temp:
            proc = mock.Mock(pid=48126, stdout=mock.Mock())
            proc.communicate.side_effect = [
                subprocess.TimeoutExpired(["x"], 1, output=b"partial"),
                subprocess.TimeoutExpired(["x"], 1, output=b"partial"),
            ]
            with mock.patch.object(probe, "AUDIT_PATH", Path(temp) / "audit.jsonl"), \
                 mock.patch.object(probe.subprocess, "Popen", return_value=proc), \
                 mock.patch.object(probe.os, "getpgid", return_value=48126), \
                 mock.patch.object(probe, "terminate_owned_group"):
                with self.assertRaisesRegex(RuntimeError, "detached pipe"):
                    probe.run_bounded(["host"], Path(temp), timeout=0.01)
            self.assertEqual(proc.communicate.call_count, 2)
            proc.stdout.close.assert_called_once()

    def test_audit_survives_temporary_workspace_unwind_on_main_failure(self):
        with tempfile.TemporaryDirectory() as temp:
            package = Path(temp) / "package.tgz"
            aok = Path(temp) / "aok"
            package.write_bytes(b"package")
            aok.write_bytes(b"aok")
            extracted = None

            def extract(_archive, destination):
                nonlocal extracted
                extracted = destination / "dist-package"
                extracted.mkdir(parents=True)
                (extracted / "cursor-agent-sea").write_bytes(b"cli")
                (extracted / "index.js").write_bytes(b"index")

            def fail_after_audit(*_args, **_kwargs):
                probe.AUDIT_PATH.write_text('{"pid":48127,"pgid":48127}\n')
                raise RuntimeError("mocked vendor boundary failure")

            prior_argv = sys.argv
            try:
                sys.argv = [str(SCRIPT), "--package", str(package), "--aok", str(aok)]
                with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", temp), \
                     mock.patch.object(probe.platform, "machine", return_value="x86_64"), \
                     mock.patch.object(probe.sys, "platform", "darwin"), \
                     mock.patch.object(probe, "require_sha", return_value="verified"), \
                     mock.patch.object(probe, "safe_extract", side_effect=extract), \
                     mock.patch.object(probe, "signal_preflight", return_value={"terminated": True}), \
                     mock.patch.object(probe, "run_bounded", side_effect=fail_after_audit):
                    with self.assertRaisesRegex(RuntimeError, "mocked vendor"):
                        probe.main()
            finally:
                sys.argv = prior_argv
            audit = probe.AUDIT_PATH
            self.assertTrue(audit.is_file())
            self.assertFalse(probe.ACTIVE_ROOT.exists())
            self.assertIn('"pgid":48127', audit.read_text())
            probe.AUDIT_DIR and __import__("shutil").rmtree(probe.AUDIT_DIR)
            probe.ACTIVE_ROOT = probe.AUDIT_DIR = probe.AUDIT_PATH = None

    def test_pty_fds_close_if_group_audit_fails(self):
        proc = mock.Mock(pid=48129)
        with tempfile.TemporaryDirectory() as temp:
            with mock.patch.object(probe.pty, "openpty", return_value=(90, 91)), \
                 mock.patch.object(probe.fcntl, "ioctl"), \
                 mock.patch.object(probe.subprocess, "Popen", return_value=proc), \
                 mock.patch.object(probe.os, "close") as close_fd, \
                 mock.patch.object(probe.os, "getpgid", side_effect=ProcessLookupError("gone")), \
                 mock.patch.object(probe, "terminate_owned_group") as cleanup:
                with self.assertRaisesRegex(RuntimeError, "audit failed"):
                    probe.read_no_input(["host"], Path(temp))
        cleanup.assert_called_once()
        self.assertEqual([call.args[0] for call in close_fd.call_args_list], [91, 90])

    def test_preflight_failure_stops_main_before_any_aok_command(self):
        with tempfile.TemporaryDirectory() as temp:
            package = Path(temp) / "package.tgz"
            aok = Path(temp) / "aok"
            package.write_bytes(b"package")
            aok.write_bytes(b"aok")
            prior_argv = sys.argv
            try:
                sys.argv = [str(SCRIPT), "--package", str(package), "--aok", str(aok)]
                with mock.patch.object(probe, "PRIVATE_TEMP_ROOT", temp), \
                     mock.patch.object(probe.platform, "machine", return_value="x86_64"), \
                     mock.patch.object(probe.sys, "platform", "darwin"), \
                     mock.patch.object(probe, "require_sha", return_value="verified"), \
                     mock.patch.object(probe, "safe_extract") as extract, \
                     mock.patch.object(probe, "signal_preflight", side_effect=RuntimeError("EPERM; audit retained")), \
                     mock.patch.object(probe, "run_bounded") as run_command:
                    with self.assertRaisesRegex(RuntimeError, "EPERM"):
                        probe.main()
                extract.assert_called_once()
                run_command.assert_not_called()
            finally:
                sys.argv = prior_argv
                probe.AUDIT_DIR and __import__("shutil").rmtree(probe.AUDIT_DIR, ignore_errors=True)
                probe.ACTIVE_ROOT = probe.AUDIT_DIR = probe.AUDIT_PATH = None


if __name__ == "__main__":
    unittest.main()
