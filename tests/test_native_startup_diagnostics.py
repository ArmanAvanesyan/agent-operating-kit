import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
DIRECTORY = ROOT / 'scripts/native-loading'
spec = importlib.util.spec_from_file_location('startup_diagnostics', DIRECTORY / 'native_startup_diagnostics.py')
diagnostics = importlib.util.module_from_spec(spec)
spec.loader.exec_module(diagnostics)


class StartupDiagnosticsTests(unittest.TestCase):
    def test_reports_fixed_labels_without_private_text(self):
        result = diagnostics.classify(b'Failed xcodebuild for private-user@example.test token=secret-value')
        self.assertEqual(result, {'fixedWordsObserved': ['failed', 'xcodebuild'], 'terminalQueryLabels': [], 'rawTextRetained': False})
        self.assertNotIn('secret', json.dumps(result))

    def test_redraw_does_not_erase_transient_failure_labels(self):
        wire = (b'failed to parse frontmatter YAML\r\n'
                b'\x1b[2J\x1b[HAvailable Agents\r\n> default')
        result = diagnostics.classify(wire)
        self.assertIn('failed', result['fixedWordsObserved'])
        self.assertFalse(result['rawTextRetained'])

    @unittest.skipUnless(shutil.which('node'), 'Node required for harmless observer fixture')
    def test_observer_excludes_unrecognized_marks_and_values(self):
        with tempfile.TemporaryDirectory() as temp:
            trace = Path(temp) / 'trace.jsonl'
            env = dict(os.environ, AOK_STARTUP_TRACE=str(trace))
            subprocess.run([shutil.which('node'), '--require', str(DIRECTORY / 'cursor_startup_observer.cjs'),
                            '-e', "const {performance}=require('node:perf_hooks'); performance.mark('cli/willAuth'); performance.mark('private-user@example.test'); setTimeout(()=>{},25);"],
                           env=env, check=True, capture_output=True, timeout=5)
            rows = [json.loads(line) for line in trace.read_text().splitlines()]
            self.assertEqual(len(rows), 1)
            self.assertEqual(set(rows[0]), {'mark', 'pid'})
            self.assertEqual(rows[0]['mark'], 'cli/willAuth')
            self.assertNotIn('private-user', trace.read_text())
