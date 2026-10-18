"""Verify native-probe write boundaries and bounded child cleanup on Darwin."""
import importlib.util
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest

MODULE = Path(__file__).resolve().parents[1] / "scripts/native-loading/native_confinement.py"
spec = importlib.util.spec_from_file_location("native_confinement", MODULE)
confinement = importlib.util.module_from_spec(spec)
spec.loader.exec_module(confinement)


@unittest.skipUnless(sys.platform == "darwin" and Path("/usr/bin/sandbox-exec").is_file(),
                     "Darwin native-probe write boundary")
class NativeProbeConfinementTests(unittest.TestCase):
    def test_writes_stay_within_owned_root(self):
        with tempfile.TemporaryDirectory(prefix="aok-confinement-test-") as tmp:
            parent = Path(tmp).resolve()
            root = parent / "allowed"
            root.mkdir()
            outside = parent / "outside"
            code = ("from pathlib import Path; import sys; "
                    "Path(sys.argv[1]).write_text('owned'); "
                    "Path(sys.argv[2]).write_text('must not exist')")
            result = confinement.run_confined(
                [sys.executable, "-B", "-c", code, root / "inside", outside], root=root,
                env={"PATH": "/usr/bin:/bin", "HOME": str(root), "TMPDIR": str(root)})
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual((root / "inside").read_text(), "owned")
            self.assertFalse(outside.exists())
            self.assertIn("PermissionError", result.stderr)

    def test_timeout_terminates_native_process_group(self):
        with tempfile.TemporaryDirectory(prefix="aok-confinement-timeout-") as tmp:
            root = Path(tmp).resolve()
            child = ("from pathlib import Path; import sys,time; "
                     "Path(sys.argv[1]).write_text('ready'); time.sleep(1.4); "
                     "Path(sys.argv[2]).write_text('survived')")
            parent = ("import subprocess,sys,time; "
                      "subprocess.Popen([sys.executable,'-B','-c',sys.argv[1],sys.argv[2],sys.argv[3]]); "
                      "time.sleep(30)")
            with self.assertRaises(subprocess.TimeoutExpired):
                confinement.run_confined(
                    [sys.executable, "-B", "-c", parent, child, root / "ready", root / "survived"],
                    root=root, timeout=1,
                    env={"PATH": "/usr/bin:/bin", "HOME": str(root), "TMPDIR": str(root)})
            self.assertTrue((root / "ready").exists(), "child must start before timeout")
            time.sleep(1.6)
            self.assertFalse((root / "survived").exists(), "native child outlived its timeout")
