import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock


SCRIPT = Path(__file__).resolve().parents[1] / 'scripts/native-loading/probe-cursor-agy-offline.py'
SPEC = importlib.util.spec_from_file_location('offline_probe', SCRIPT)
PROBE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PROBE)


class TimeoutOutputTest(unittest.TestCase):
    def test_confined_timeout_keeps_cumulative_communicate_output_once(self):
        prefix = b'json-line\n'
        cumulative = b'json-line\nsecond-line\n'
        proc = mock.Mock()
        proc.pid = 123
        proc.communicate.side_effect = [
            subprocess.TimeoutExpired(['probe'], 1, output=prefix, stderr=b''),
            (cumulative, b''),
        ]
        with tempfile.TemporaryDirectory() as temp, \
             mock.patch.object(PROBE.subprocess, 'Popen', return_value=proc), \
             mock.patch.object(PROBE.os, 'killpg'):
            rc, out, err = PROBE.confined(['probe'], root=Path(temp), env={})
        self.assertEqual(rc, 124)
        self.assertEqual(out, cumulative)
        self.assertEqual(out.count(b'json-line'), 1)
        self.assertEqual(err, b'')

    def test_confined_timeout_escalates_and_retains_last_cumulative_output(self):
        prefix = b'partial\n'
        cumulative = b'partial\ncomplete\n'
        proc = mock.Mock()
        proc.pid = 456
        proc.communicate.side_effect = [
            subprocess.TimeoutExpired(['probe'], 1, output=prefix, stderr=b''),
            subprocess.TimeoutExpired(['probe'], 2, output=prefix, stderr=b''),
            (cumulative, b''),
        ]
        with tempfile.TemporaryDirectory() as temp, \
             mock.patch.object(PROBE.subprocess, 'Popen', return_value=proc), \
             mock.patch.object(PROBE.os, 'killpg') as killpg:
            rc, out, _ = PROBE.confined(['probe'], root=Path(temp), env={})
        self.assertEqual(rc, 124)
        self.assertEqual(out, cumulative)
        self.assertEqual(out.count(b'partial'), 1)
        self.assertEqual(killpg.call_count, 2)


if __name__ == '__main__':
    unittest.main()
