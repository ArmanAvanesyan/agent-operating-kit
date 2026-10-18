import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    'agy_account_manager_test', ROOT / 'scripts/native-loading/probe_agy_account_manager.py')
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


class AccountManagerTests(unittest.TestCase):
    def test_account_capture_never_reads_logs_or_keeps_terminal_rows(self):
        capture = probe.AccountCapture()
        with mock.patch.object(probe.local, 'synthetic_load_diagnostics') as logs, \
             mock.patch.object(probe.local, 'public_synthetic_rows') as rows:
            self.assertEqual(capture.manager_rows('private identity', Path('/unused')), [])
            self.assertEqual(capture.diagnostics(Path('/unused')), [])
            logs.assert_not_called()
            rows.assert_not_called()

    def test_cleanup_runs_even_if_marker_invalidation_fails(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve() / 'owned'
            root.mkdir(mode=0o700)
            (root / 'home').mkdir()
            (root / 'home/private').write_text('synthetic fixture')
            with self.assertRaises(FileNotFoundError):
                probe.cleanup_owned_case(root)
            self.assertFalse(root.exists())

    def test_cleanup_never_follows_marker_symlink(self):
        with tempfile.TemporaryDirectory() as temp:
            parent = Path(temp).resolve()
            outside = parent / 'outside'; outside.write_text('unchanged')
            root = parent / 'owned'; root.mkdir(mode=0o700)
            (root / 'owned-session.json').symlink_to(outside)
            with self.assertRaises(OSError):
                probe.cleanup_owned_case(root)
            self.assertEqual(outside.read_text(), 'unchanged')
            self.assertFalse(root.exists())

    def test_profile_copied_once_per_case_across_preflight_and_launch(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp).resolve()
            source = root / 'source'; source.mkdir()
            aok = root / 'aok'; aok.write_bytes(b'built-binary-fixture')
            out = root / 'evidence.json'
            fd = os.open(out, os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            copied = []
            bad_audit = False
            def copy(src, dest):
                self.assertEqual(src, source)
                dest.mkdir()
                copied.append(dest)
                return {'filesCopied': 1, 'bytesCopied': 1}
            def run(**_):
                case_root = root / ('case-' + str(len(copied)))
                case_root.mkdir(mode=0o700)
                for _ in range(2):
                    env = probe.login.env_for(case_root)
                    self.assertEqual(env['HOME'], str(case_root / 'home'))
                audit = root / ('audit-' + str(len(copied))); audit.mkdir()
                (audit / 'audit.jsonl').write_text('malformed JSON' if bad_audit else '{"event":"native-login-cleanup","cleaned":true}\n')
                (case_root / 'owned-session.json').write_text('{"valid":false}')
                capture = probe.login._capture_login.__self__
                capture.command_sent = True
                capture.manager = {'managerSectionObserved': True, 'nameObserved': len(copied) == 1,
                                   'descriptionObserved': len(copied) == 1,
                                   'parseErrorObserved': len(copied) == 3,
                                   'authBarrierObserved': False, 'errorObserved': False}
                return {'root': str(case_root), 'auditRoot': str(audit)}
            with mock.patch.object(probe.login.AGY, 'validate_isolated_login_root', return_value={'root': source}), \
                 mock.patch.object(probe.login.AGY, 'copy_isolated_login_home', side_effect=copy), \
                 mock.patch.object(probe.local, 'open_evidence_output', return_value=fd) as output, \
                 mock.patch.object(probe.login.AGY.agy.base, 'prepare_aok', return_value=(aok, b'fixture', 'hash', 'description')), \
                 mock.patch.object(probe.login, 'env_for', side_effect=lambda r: {'HOME': str(r / 'home')}), \
                 mock.patch.object(probe.login, 'prepare', side_effect=run), \
                 mock.patch.object(probe.login, 'invalidate_and_remove_private_home'), \
                 mock.patch.object(probe.subprocess, 'run'):
                self.assertEqual(probe.main(['--aok', str(aok), '--isolated-login-root', str(source),
                    '--evidence-out', str(out), '--accept-required-terms',
                    '--disable-optional-sharing', '--trust-owned-fixtures']), 0)
                bad_audit = True
                output.return_value = os.open(root / 'failed.json', os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
                with self.assertRaises(json.JSONDecodeError):
                    probe.main(['--aok', str(aok), '--isolated-login-root', str(source),
                        '--evidence-out', str(root / 'failed.json'), '--accept-required-terms',
                        '--disable-optional-sharing', '--trust-owned-fixtures'])
                self.assertFalse(copied[-1].parent.exists())
            self.assertEqual(len(copied), 4)
            report = json.loads(out.read_text())
            self.assertTrue(report['candidateNativeLoadingEvidence'])
            self.assertFalse(report['accountRowsRetained'])
            self.assertFalse(report['nativeLogsRead'])
            self.assertTrue(all(v['privateRootRemoved'] for v in report['cases'].values()))
            self.assertTrue(source.exists())


if __name__ == '__main__':
    unittest.main()
