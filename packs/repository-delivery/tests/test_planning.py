"""Real Git planning evidence; remote mutations mocked explicitly."""
import hashlib
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
from repo_delivery.core import DeliveryError
from repo_delivery.planning import plan_backlog, apply_backlog


class PlanningTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        self.git('init', '-q')
        self.git('config', 'user.name', 'Fixture')
        self.git('config', 'user.email', 'fixture@example.invalid')
        sources = []
        for role in ('implementation', 'research', 'adr', 'roadmap'):
            name = role + '.txt'
            (self.repo / name).write_text(role)
            sources.append(dict(role=role, path=name, sha256=hashlib.sha256(role.encode()).hexdigest()))
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture')
        self.spec = dict(github_repository='owner/repo', baseline='HEAD', sources=sources,
                         items=[self.item('foundation'), self.item('delivery', ['foundation'])])

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.repo), *args])

    def item(self, key, deps=None):
        return dict(key=key, title=key.title(), summary='Implement observed gap',
                    acceptance_criteria=[dict(action='Run regression test', expected='Exit status is zero')],
                    evidence=['implementation.txt', 'research.txt'], depends_on=deps or [])

    def test_plan_pins_evidence_and_orders_dependencies(self):
        self.spec['items'].reverse()
        plan = plan_backlog(self.repo, self.spec)
        self.assertEqual([i['key'] for i in plan['items']], ['foundation', 'delivery'])
        self.assertEqual(plan['baseline_revision'], self.git('rev-parse', 'HEAD').decode().strip())
        self.assertEqual(plan['items'][0]['action'], 'create')
        self.assertIn('Exit status is zero', plan['items'][0]['body'])

    def test_reuses_stable_marker_and_explicit_number(self):
        existing = [dict(number=4, title='Existing', body='Human notes\n<!-- repo-delivery:backlog:foundation -->', state='OPEN')]
        self.assertEqual(plan_backlog(self.repo, self.spec, existing)['items'][0]['issue_number'], 4)
        self.spec['items'][0]['issue_number'] = 8
        with self.assertRaises(DeliveryError):
            plan_backlog(self.repo, self.spec, existing)

    def test_rejects_invalid_evidence_dependencies_and_criteria(self):
        import copy
        variants = []
        spec = copy.deepcopy(self.spec); spec['sources'][0]['sha256'] = '0'*64; variants.append(spec)
        spec = copy.deepcopy(self.spec); spec['sources'].pop(); variants.append(spec)
        spec = copy.deepcopy(self.spec); spec['items'][0]['depends_on'] = ['unknown']; variants.append(spec)
        spec = copy.deepcopy(self.spec); spec['items'][0]['depends_on'] = ['delivery']; variants.append(spec)
        spec = copy.deepcopy(self.spec); spec['items'][0]['acceptance_criteria'] = ['works']; variants.append(spec)
        spec = copy.deepcopy(self.spec); spec['items'][0]['evidence'] = ['absent']; variants.append(spec)
        for spec in variants:
            with self.subTest(spec=spec), self.assertRaises(DeliveryError):
                plan_backlog(self.repo, spec)

    @patch('repo_delivery.github._gh')
    def test_apply_authorization_binding_and_staleness_precede_remote(self, gh):
        plan = plan_backlog(self.repo, self.spec)
        for repository, actions in [('owner/repo', []), ('other/repo', ['update-backlog'])]:
            with self.assertRaises(DeliveryError):
                apply_backlog(plan, repository, actions)
        (self.repo / 'research.txt').write_text('changed')
        with self.assertRaises(DeliveryError):
            apply_backlog(plan, 'owner/repo', ['update-backlog'])
        gh.assert_not_called()

    @patch('repo_delivery.github._gh')
    def test_apply_creates_dependency_numbers_and_resume_reuses_markers(self, gh):
        plan = plan_backlog(self.repo, self.spec)
        remote = []
        calls = []
        def call(args, **kwargs):
            if args[0] == 'api':
                return [list(remote)]
            if args[:2] == ['issue', 'create']:
                number = len(remote) + 10
                remote.append(dict(number=number, title=args[args.index('--title')+1], body=kwargs['data'], state='OPEN'))
                calls.append(kwargs['data'])
                return f'https://github.com/owner/repo/issues/{number}'
            if args[:2] == ['issue', 'edit']:
                raise AssertionError('Unchanged resume must not edit')
            raise AssertionError(args)
        gh.side_effect = call
        result = apply_backlog(plan, 'owner/repo', ['update-backlog'])
        self.assertEqual(result['issues'], {'foundation': 10, 'delivery': 11})
        self.assertIn('Depends on #10', calls[1])
        apply_backlog(plan, 'owner/repo', ['update-backlog'])
        self.assertEqual(len(calls), 2)

    @patch('repo_delivery.github._gh')
    def test_update_preserves_human_content(self, gh):
        self.spec['items'] = [self.item('foundation')]
        marker = '<!-- repo-delivery:backlog:foundation -->'
        existing = dict(number=4, title='Human title', state='OPEN', body='Keep this\n'+marker)
        plan = plan_backlog(self.repo, self.spec, [existing])
        def remote(args, **kwargs):
            if args[0] == 'api':
                return [[existing]]
            existing['body'] = kwargs['data']
            return ''
        gh.side_effect = remote
        apply_backlog(plan, 'owner/repo', ['update-backlog'])
        edits = [c for c in gh.call_args_list if c.args[0][:2] == ['issue', 'edit']]
        self.assertIn('Keep this', edits[0].kwargs['data'])
        self.assertNotIn('--title', edits[0].args[0])

    @patch('repo_delivery.github._gh')
    def test_live_issue_cannot_own_two_planned_items(self, gh):
        plan = plan_backlog(self.repo, self.spec)
        issue = dict(number=7, title='Combined', state='OPEN', body=(
            '<!-- repo-delivery:backlog:foundation -->\n'
            '<!-- repo-delivery:backlog:delivery -->'))
        gh.return_value = [[issue]]
        with self.assertRaisesRegex(DeliveryError, 'same issue'):
            apply_backlog(plan, 'owner/repo', ['update-backlog'])
        self.assertTrue(all(call.args[0][0] == 'api' for call in gh.call_args_list))

    @patch('repo_delivery.github._gh')
    def test_update_must_be_confirmed_without_replaying(self, gh):
        self.spec['items'] = [self.item('foundation')]
        issue = dict(number=4, title='Title', state='OPEN', body='<!-- repo-delivery:backlog:foundation -->')
        plan = plan_backlog(self.repo, self.spec, [issue])
        gh.side_effect = [[[issue]], [[issue]], '', [[issue]]]
        with self.assertRaisesRegex(DeliveryError, 'unconfirmed'):
            apply_backlog(plan, 'owner/repo', ['update-backlog'])
        self.assertEqual(sum(c.args[0][:2] == ['issue', 'edit'] for c in gh.call_args_list), 1)

    def test_rejects_ambiguous_markers_and_reused_number(self):
        marker = '<!-- repo-delivery:backlog:foundation -->'
        issues = [dict(number=n, title='Issue', body=marker, state='OPEN') for n in (1, 2)]
        with self.assertRaises(DeliveryError):
            plan_backlog(self.repo, self.spec, issues)
        self.spec['items'][0]['issue_number'] = 1
        self.spec['items'][1]['issue_number'] = 1
        with self.assertRaises(DeliveryError):
            plan_backlog(self.repo, self.spec, issues[:1])

    def test_symlink_evidence_rejected(self):
        (self.repo / 'research.txt').unlink()
        (self.repo / 'research.txt').symlink_to('implementation.txt')
        with self.assertRaises(DeliveryError):
            plan_backlog(self.repo, self.spec)

    @patch('repo_delivery.github._gh')
    def test_uncertain_create_never_retried(self, gh):
        plan = plan_backlog(self.repo, self.spec)
        gh.side_effect = [[[]], [[]], DeliveryError('uncertain')]
        with self.assertRaisesRegex(DeliveryError, 'uncertain'):
            apply_backlog(plan, 'owner/repo', ['update-backlog'])
        self.assertEqual(sum(c.args[0][:2] == ['issue', 'create'] for c in gh.call_args_list), 1)

    @patch('repo_delivery.github._gh')
    def test_changed_issue_and_malformed_remote_fail_closed(self, gh):
        self.spec['items'] = [self.item('foundation')]
        issue = dict(number=4, title='Human title', state='OPEN', body='<!-- repo-delivery:backlog:foundation -->')
        plan = plan_backlog(self.repo, self.spec, [issue])
        gh.side_effect = [[[issue]], [[dict(issue, body=issue['body']+'concurrent')]]]
        with self.assertRaisesRegex(DeliveryError, 'Issue changed'):
            apply_backlog(plan, 'owner/repo', ['update-backlog'])
        gh.side_effect = [{'invalid': 'response'}]
        with self.assertRaises(DeliveryError):
            apply_backlog(plan, 'owner/repo', ['update-backlog'])

    @patch('repo_delivery.github._gh')
    def test_closed_issue_reused_without_reopening(self, gh):
        self.spec['items'] = [self.item('foundation')]
        issue = dict(number=4, title='Done', state='CLOSED', body='<!-- repo-delivery:backlog:foundation -->')
        plan = plan_backlog(self.repo, self.spec, [issue])
        self.assertEqual(plan['items'][0]['action'], 'reuse')
        gh.return_value = [[issue]]
        self.assertEqual(apply_backlog(plan, 'owner/repo', ['update-backlog'])['issues'], {'foundation': 4})
        self.assertEqual(gh.call_count, 1)


if __name__ == '__main__':
    unittest.main()
