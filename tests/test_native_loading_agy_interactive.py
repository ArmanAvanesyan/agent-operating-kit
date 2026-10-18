import importlib.util
import json
from pathlib import Path
import subprocess
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    "agy_interactive_probe", ROOT / "scripts/native-loading/probe-agy-interactive-manager.py")
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


class InteractiveManagerTests(unittest.TestCase):
    def test_only_local_ready_prompt_allows_documented_command(self):
        ready = probe.sanitized_state(b"Welcome\r\n\xe2\x9d\xaf ")
        self.assertTrue(probe.may_send_manager_command(ready))

        auth = probe.sanitized_state(b"Sign in to continue\r\n\xe2\x9d\xaf ")
        self.assertFalse(probe.may_send_manager_command(auth))

        no_prompt = probe.sanitized_state(b"Starting Antigravity\r\n")
        self.assertFalse(probe.may_send_manager_command(no_prompt))

        error = probe.sanitized_state(b"fatal error: configuration invalid")
        self.assertFalse(probe.may_send_manager_command(error))

    def test_manager_evidence_requires_section_and_exact_name_and_description(self):
        state = probe.sanitized_state(
            "Available Agents\n aok-probe\n Synthetic AGY adapter discovery fixture\n".encode())
        state["commandSent"] = True
        case = {"cleanupVerified": True, "state": state}
        self.assertTrue(probe.qualifies_fixture(case, state, positive=True))

        name_only = probe.sanitized_state(b"Available Agents\naok-probe\n")
        name_only["commandSent"] = True
        self.assertFalse(probe.qualifies_fixture({"cleanupVerified": True}, name_only, positive=True))

        heading_only = probe.sanitized_state(b"Available Agents\nDefault agent\n")
        heading_only["commandSent"] = True
        self.assertFalse(probe.qualifies_fixture({"cleanupVerified": True}, heading_only, positive=True))

    def test_positive_absent_and_malformed_controls_are_all_required(self):
        def case(text, cleanup=True):
            state = probe.sanitized_state(text)
            state["commandSent"] = True
            return {"cleanupVerified": cleanup, "state": state}

        cases = {
            "positive": case(b"Available Agents\naok-probe\nSynthetic AGY adapter discovery fixture"),
            "absent": case(b"Available Agents\nDefault agent"),
            "malformed": case(b"Available Agents\nDefault agent"),
        }
        self.assertTrue(probe.has_candidate_manager_evidence(cases))
        self.assertFalse(probe.has_candidate_manager_evidence({k: v for k, v in cases.items() if k != "malformed"}))

        cases["malformed"]["state"] = probe.sanitized_state(
            b"Available Agents\nerror: failed to parse malformed agent")
        cases["malformed"]["state"]["commandSent"] = True
        self.assertFalse(probe.has_candidate_manager_evidence(cases))

        cases["malformed"]["state"] = probe.sanitized_state(b"Available Agents\nDefault agent")
        cases["malformed"]["state"]["commandSent"] = True
        cases["absent"]["cleanupVerified"] = False
        self.assertFalse(probe.has_candidate_manager_evidence(cases))

    def test_interactive_container_requires_real_tty_and_stdin(self):
        valid = {
            "Config": {"User": "1000:1000", "Tty": True, "OpenStdin": True},
            "HostConfig": {"NetworkMode": "none", "ReadonlyRootfs": True, "Binds": [],
                           "Tmpfs": {"/probe": "size=1m", "/tmp": "size=1m"},
                           "CapDrop": ["ALL"], "SecurityOpt": ["no-new-privileges"]},
            "Mounts": [],
        }
        probe.validate_interactive_config(valid)
        for key, value in (("OpenStdin", False), ("Tty", False)):
            bad = {**valid, "Config": {**valid["Config"], key: value}}
            with self.assertRaises(RuntimeError):
                probe.validate_interactive_config(bad)
        bad_network = {**valid, "HostConfig": {**valid["HostConfig"], "NetworkMode": "bridge"}}
        with self.assertRaises(RuntimeError):
            probe.validate_interactive_config(bad_network)

    @mock.patch.object(probe.base, "docker")
    def test_pinned_image_preflight_uses_digest_id_without_pull(self, docker):
        docker.side_effect = [
            subprocess.CompletedProcess([], 0, b'"unix:///Users/test/.docker/run/docker.sock"', b""),
            subprocess.CompletedProcess([], 0, b"docker-desktop 29.4.0 Docker Desktop", b""),
            subprocess.CompletedProcess([], 0, json.dumps([{
                "Id": "sha256:4db36457f406501e6f608802e5da617e5fbd0e80b75901b6a09de1ae5a667d32",
                "Os": "linux", "Architecture": "amd64",
                "RepoDigests": [probe.base.IMAGE_DIGEST], "RepoTags": [probe.base.IMAGE],
            }]).encode(), b""),
        ]
        with mock.patch.object(probe.Path, "home", return_value=Path("/Users/test")):
            result = probe.inspect_local_pinned_image()
        self.assertEqual(result["cacheStatus"], "already-present; no pull performed")
        self.assertFalse(any("pull" in call.args[0] for call in docker.call_args_list))

    @mock.patch.object(probe.base, "docker")
    def test_pinned_image_mismatch_fails_without_pull(self, docker):
        docker.side_effect = [
            subprocess.CompletedProcess([], 0, b'"unix:///Users/test/.docker/run/docker.sock"', b""),
            subprocess.CompletedProcess([], 0, b"docker-desktop 29.4.0 Docker Desktop", b""),
            subprocess.CompletedProcess([], 0, b'[{"Id":"sha256:wrong","Os":"linux","Architecture":"amd64"}]', b""),
        ]
        with mock.patch.object(probe.Path, "home", return_value=Path("/Users/test")):
            with self.assertRaises(RuntimeError):
                probe.inspect_local_pinned_image()
        self.assertEqual(docker.call_count, 3)


if __name__ == "__main__":
    unittest.main()
