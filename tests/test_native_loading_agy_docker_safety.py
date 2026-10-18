import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
MODULE_PATH = ROOT / "scripts/native-loading/probe-agy-linux-container.py"
spec = importlib.util.spec_from_file_location("agy_linux_probe", MODULE_PATH)
probe = importlib.util.module_from_spec(spec)
spec.loader.exec_module(probe)


class DockerSafetyTests(unittest.TestCase):
    def valid_config(self):
        return {
            "Config": {"User": "1000:1000", "Tty": True, "OpenStdin": False},
            "HostConfig": {"NetworkMode": "none", "ReadonlyRootfs": True,
                           "Binds": [], "Tmpfs": {"/probe": "size=1m", "/tmp": "size=1m"},
                           "CapDrop": ["ALL"], "SecurityOpt": ["no-new-privileges"]},
            "Mounts": [],
        }

    def test_container_requires_closed_stdin_and_private_configuration(self):
        probe.validate_container_config(self.valid_config())
        for change in (
            lambda c: c["Config"].update(OpenStdin=True),
            lambda c: c["HostConfig"].update(NetworkMode="bridge"),
            lambda c: c["HostConfig"].update(Binds=["/Users:/host"]),
            lambda c: c["HostConfig"].update(ReadonlyRootfs=False),
            lambda c: c["Mounts"].append({"Source": "/host"}),
        ):
            config = self.valid_config()
            change(config)
            with self.assertRaises(RuntimeError):
                probe.validate_container_config(config)

    def qualification_fixture(self):
        clean = {"exitCode": 0, "expectedNameObserved": False, "expectedDescriptionObserved": False,
                 "authPromptMarkerObserved": False, "parseErrorMarkerObserved": False, "errorMarkerObserved": False}
        tty = {"returnCode": 0, "expectedNameObserved": False, "expectedDescriptionObserved": False,
               "authPromptMarkerObserved": False, "parseErrorMarkerObserved": False, "errorMarkerObserved": False}
        controls = {}
        for case in ("positive", "absent", "malformed"):
            controls[case] = {"containerReturnCode": 0, "cleanupVerified": True, "summary": {
                "version": {"exitCode": 0, "version14": True}, "nonTTY": dict(clean)}, "tty": dict(tty)}
        controls["positive"]["summary"]["nonTTY"].update(
            expectedNameObserved=True, expectedDescriptionObserved=True)
        return controls

    def test_name_only_error_or_incomplete_control_is_not_candidate_evidence(self):
        result = self.qualification_fixture()
        self.assertTrue(probe.has_candidate_roster_evidence(result))

        name_only = self.qualification_fixture()
        name_only["positive"]["summary"]["nonTTY"]["expectedDescriptionObserved"] = False
        self.assertFalse(probe.has_candidate_roster_evidence(name_only))

        error_output = self.qualification_fixture()
        error_output["positive"]["summary"]["nonTTY"]["errorMarkerObserved"] = True
        self.assertFalse(probe.has_candidate_roster_evidence(error_output))

        missing_control = self.qualification_fixture()
        del missing_control["malformed"]
        self.assertFalse(probe.has_candidate_roster_evidence(missing_control))

        unclean_control = self.qualification_fixture()
        unclean_control["absent"]["cleanupVerified"] = False
        self.assertFalse(probe.has_candidate_roster_evidence(unclean_control))

    def test_probe_gate_status_is_never_promoted_by_observation(self):
        result = self.qualification_fixture()
        self.assertTrue(probe.has_candidate_roster_evidence(result))
        self.assertEqual(probe.native_loading_status(result), "not-verified")

        name_only = self.qualification_fixture()
        name_only["positive"]["summary"]["nonTTY"]["expectedDescriptionObserved"] = False
        self.assertEqual(probe.native_loading_status(name_only), "not-verified")

    @mock.patch.object(probe, "remove_owned_container", return_value=True)
    @mock.patch.object(probe, "docker")
    @mock.patch.object(probe.subprocess, "Popen")
    def test_unsafe_created_container_is_removed_before_start(self, popen, docker, cleanup):
        docker.side_effect = [
            subprocess.CompletedProcess([], 0, b"container-id\n", b""),
            subprocess.CompletedProcess([], 0, b'[{"Config":{"User":"1000:1000","Tty":true,"OpenStdin":false},"HostConfig":{"NetworkMode":"bridge"},"Mounts":[]}]', b""),
        ]
        with self.assertRaises(RuntimeError):
            probe.run_container_case("positive", "nonce", "image", "aok-probe", "description")
        popen.assert_not_called()
        self.assertEqual(cleanup.call_count, 1)
        self.assertEqual(cleanup.call_args.args, ("container-id", "nonce"))

    @mock.patch.object(probe, "remove_owned_container", return_value=False)
    @mock.patch.object(probe, "docker", return_value=subprocess.CompletedProcess([], 1, b"", b"create failed"))
    def test_failed_create_requires_cleanup_confirmation(self, docker, cleanup):
        with self.assertRaises(RuntimeError):
            probe.run_container_case("positive", "nonce", "image", "aok-probe", "description")
        cleanup.assert_called_once_with("aok-agy-linux-nonce-positive", "nonce")

    @mock.patch.object(probe, "image_has_nonce", side_effect=[True, False])
    @mock.patch.object(probe, "docker", return_value=subprocess.CompletedProcess([], 1, b"", b"daemon error"))
    def test_image_removal_error_is_not_reported_as_verified(self, docker, inspect):
        with self.assertRaises(RuntimeError):
            probe.remove_owned_image("tag", "nonce")


if __name__ == "__main__":
    unittest.main()
