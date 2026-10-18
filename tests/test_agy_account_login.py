import importlib.util
from pathlib import Path
import unittest
from unittest import mock

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location(
    'agy_account_test', ROOT / 'scripts/native-loading/prepare_agy_account_login.py')
probe = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(probe)


class AccountLoginTests(unittest.TestCase):
    def test_empty_partial_and_account_frames_never_index_rows_or_retain_identity(self):
        observer = probe.ApprovedAccountSetup()
        for wire in (b'', b'\x1b[', b'\x1b[2J', b'\r\n', b'Antigravity CLI 1.4.2'):
            self.assertIsInstance(observer.observe(wire), dict)
        border = '─' * 160
        rows = ['Antigravity CLI 1.4.2', 'person@example.com (Antigravity Example Tier)',
                'Gemini 3.8 Flash (High)', border, '>', border, '? for shortcuts']
        state = observer.observe('\r\n'.join(rows).encode())
        self.assertTrue(state['localPromptObserved'])
        self.assertNotIn('person@example.com', str(state))

    def test_hooks_restored_after_capture_exception(self):
        observer = probe.ApprovedAccountSetup()
        original_state, original_select = probe.login.state_from_bytes, probe.login.select.select
        observer.original_capture = mock.Mock(side_effect=RuntimeError('test'))
        with self.assertRaisesRegex(RuntimeError, 'test'):
            observer.capture(1, 2, case_root='/private/tmp/example', audit_path=Path('/unused'))
        self.assertIs(probe.login.state_from_bytes, original_state)
        self.assertIs(probe.login.select.select, original_select)

    def test_actions_require_stable_current_screen_and_cannot_replay(self):
        observer = probe.ApprovedAccountSetup()
        observer.original_select = mock.Mock(return_value=([], [], []))
        observer.snapshot = ({}, 'exact frame', 'hash')
        observer.changed = 0
        observer.master = 9
        observer.root = Path('/unused')
        observer.audit_path = Path('/unused-audit')
        with mock.patch.object(probe.local, 'onboarding_action', return_value=('disable-optional-sharing', b'\r')), \
             mock.patch.object(probe.login, 'append_audit'), mock.patch.object(probe.os, 'write') as write:
            observer.select([], [], [], 0)
            self.assertEqual(write.call_count, 1)
            observer.snapshot = ({}, 'exact frame', 'hash')
            observer.changed = 0
            observer.select([], [], [], 0)
            self.assertEqual(write.call_count, 1)
            observer.actions.clear()
            observer.original_select.return_value = ([9], [], [])
            observer.select([], [], [], 0)
            self.assertEqual(write.call_count, 1)

    def test_operator_cannot_duplicate_original_color_action(self):
        observer = probe.ApprovedAccountSetup()
        observer.original_select = mock.Mock(return_value=([], [], []))
        observer.snapshot = ({}, 'color', 'hash')
        observer.root = Path('/unused')
        observer.changed = 0
        with mock.patch.object(probe.local, 'onboarding_action', return_value=('color', b'\r')), \
             mock.patch.object(probe.os, 'write') as write:
            observer.select([], [], [], 0)
            write.assert_not_called()


if __name__ == '__main__':
    unittest.main()
