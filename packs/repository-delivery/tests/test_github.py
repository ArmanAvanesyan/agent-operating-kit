"""Mock GitHub boundary tests; no remote operations."""
import unittest
from unittest.mock import patch
from repo_delivery.core import DeliveryError
from repo_delivery import github


class GitHubTests(unittest.TestCase):
    def setUp(self):
        self.binding = patch('repo_delivery.github._binding')
        self.binding.start()
        self.addCleanup(patch.stopall)

    def pr(self, **updates):
        value = dict(state='OPEN', headRefOid='a'*40, headRefName='feature',
                     baseRefName='main', isDraft=False, mergeStateStatus='CLEAN',
                     reviewDecision='APPROVED', statusCheckRollup=[
                         dict(__typename='CheckRun', name='tests', status='COMPLETED', conclusion='SUCCESS')])
        value.update(updates)
        return value

    def test_readiness_requires_explicit_check_policy(self):
        self.assertFalse(github.readiness(self.pr(), None)['ready'])
        self.assertTrue(github.readiness(self.pr(), [])['ready'])

    def test_checkrun_and_statuscontext(self):
        self.assertTrue(github.readiness(self.pr(), ['tests'])['ready'])
        pr = self.pr(statusCheckRollup=[dict(__typename='StatusContext', context='ci', state='SUCCESS')])
        self.assertTrue(github.readiness(pr, ['ci'])['ready'])
        self.assertFalse(github.readiness(pr, ['missing'])['ready'])

    def test_fail_closed_on_unready_states(self):
        for changes in [dict(isDraft=True), dict(state='CLOSED'), dict(mergeStateStatus='UNKNOWN'),
                        dict(reviewDecision='REVIEW_REQUIRED'), dict(statusCheckRollup=[]),
                        dict(statusCheckRollup=[dict(name='tests', status='COMPLETED', conclusion='FAILURE')]),
                        dict(statusCheckRollup=[dict(name='tests', status='IN_PROGRESS', conclusion='SUCCESS')])]:
            with self.subTest(changes=changes):
                self.assertFalse(github.readiness(self.pr(**changes), ['tests'])['ready'])

    @patch('repo_delivery.github._terminal_identity')
    @patch('repo_delivery.github._gh')
    def test_merged_reconciliation_does_not_merge_again(self, gh, identity):
        gh.return_value = self.pr(state='MERGED')
        result = github.merge_pr({'authorized_actions':['merge']}, {}, {}, 'owner/repo', 3, ['tests'])
        self.assertEqual(result['state'], 'already-complete')
        self.assertEqual(gh.call_count, 1)

    def test_mutation_authorization_required(self):
        with self.assertRaises(DeliveryError):
            github.merge_pr({}, {}, {}, 'owner/repo', 3, [])
        with self.assertRaises(DeliveryError):
            github.open_pr({}, {}, {}, 'owner/repo', 'title', 'body', 'main', 'feature')

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.github._assert_ready', return_value={'head':'a'*40})
    @patch('repo_delivery.github._gh')
    def test_merge_head_fence(self, gh, evidence, local):
        gh.return_value = self.pr(headRefOid='b'*40)
        with self.assertRaises(DeliveryError):
            github.merge_pr({'authorized_actions':['merge']}, {}, {}, 'owner/repo', 3, ['tests'])
        self.assertEqual(gh.call_count, 1)

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.github._assert_ready', return_value={'head':'a'*40})
    @patch('repo_delivery.github._gh')
    def test_merge_uses_atomic_head_match_and_rechecks_result(self, gh, evidence, local):
        gh.side_effect = [self.pr(), '', self.pr(state='MERGED')]
        result = github.merge_pr({'authorized_actions':['merge']}, {}, {}, 'owner/repo', 3, ['tests'])
        self.assertEqual(result['state'], 'merged')
        self.assertIn('--match-head-commit', gh.call_args_list[1].args[0])

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.github._assert_ready', return_value={'head':'a'*40})
    @patch('repo_delivery.github._gh')
    def test_open_reconciles_existing(self, gh, evidence, local):
        gh.side_effect = [{'object':{'sha':'a'*40}}, [self.pr(number=4)]]
        result = github.open_pr({'authorized_actions':['open-pr']}, {}, {}, 'owner/repo', 'T', 'B', 'main', 'feature')
        self.assertEqual(result['state'], 'existing')
        self.assertEqual(gh.call_count, 2)

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.github._assert_ready', return_value={'head':'a'*40})
    @patch('repo_delivery.github._gh')
    def test_open_refuses_unpushed_head(self, gh, evidence, local):
        gh.return_value = {'object':{'sha':'b'*40}}
        with self.assertRaises(DeliveryError):
            github.open_pr({'authorized_actions':['open-pr']}, {}, {}, 'owner/repo', 'T', 'B', 'main', 'feature')

    def test_invalid_repository_and_numbers(self):
        for repository in ['--help', 'owner/repo/extra', 'https://github.com/o/r']:
            with self.assertRaises(DeliveryError):
                github.inspect_pr(repository, 1)
        with self.assertRaises(DeliveryError):
            github.inspect_issue('owner/repo', -1)

    @patch('repo_delivery.github.subprocess.run')
    def test_cli_boundary_and_failures(self, run):
        run.return_value.returncode = 0
        run.return_value.stdout = '{"number": 3}'
        self.assertEqual(github.inspect_issue('owner/repo', 3)['number'], 3)
        self.assertEqual(run.call_args.args[0][:3], ['gh', 'issue', 'view'])
        self.assertNotIn('shell', run.call_args.kwargs)
        run.return_value.stdout = 'not json'
        with self.assertRaises(DeliveryError):
            github.inspect_pr('owner/repo', 3)
        run.return_value.returncode = 1
        with self.assertRaises(DeliveryError):
            github.inspect_pr('owner/repo', 3)
        run.side_effect = FileNotFoundError()
        with self.assertRaises(DeliveryError):
            github.inspect_pr('owner/repo', 3)

    @patch('repo_delivery.github.git')
    def test_local_source_must_be_clean(self, git):
        git.return_value = b' M dirty'
        with self.assertRaises(DeliveryError):
            github._local_head({'repository': '/fixture'})
        git.side_effect = [b'', b'abc\n']
        self.assertEqual(github._local_head({'repository':'/fixture'}), 'abc')

    def test_duplicate_failed_check_cannot_hide_behind_success(self):
        self.assertFalse(github.readiness(self.pr(statusCheckRollup=[
            dict(context='tests', state='SUCCESS'), dict(context='tests', state='FAILURE')]), ['tests'])['ready'])
        self.assertTrue(github.readiness(self.pr(reviewDecision=''), ['tests'], False)['ready'])

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.github._assert_ready', return_value={'head':'a'*40})
    @patch('repo_delivery.github._gh')
    def test_open_create_preserves_body_without_push(self, gh, evidence, local):
        gh.side_effect = [{'object':{'sha':'a'*40}}, [], {'object':{'sha':'a'*40}},
                          'https://github.com/owner/repo/pull/3', self.pr()]
        result = github.open_pr({'repository':'/fixture', 'authorized_actions':['open-pr']}, {}, {},
                                'owner/repo', 'T', 'body\nsecond line', 'main', 'feature')
        self.assertEqual(result['state'], 'created')
        self.assertEqual(gh.call_args_list[3].kwargs['data'], 'body\nsecond line')
        self.assertEqual(gh.call_args_list[3].args[0][:2], ['pr', 'create'])
        self.assertIn('--head', gh.call_args_list[3].args[0])

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.github._assert_ready', return_value={'head':'a'*40})
    @patch('repo_delivery.github._gh')
    def test_closed_duplicate_and_mismatched_existing_refuse(self, gh, evidence, local):
        for candidates in [[self.pr(state='CLOSED')], [self.pr(), self.pr()],
                           [self.pr(headRefOid='b'*40)]]:
            gh.side_effect = [{'object':{'sha':'a'*40}}, candidates]
            with self.assertRaises(DeliveryError):
                github.open_pr({'authorized_actions':['open-pr']}, {}, {}, 'owner/repo', 'T', 'B', 'main', 'feature')

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.github._assert_ready', return_value={'head':'a'*40})
    @patch('repo_delivery.github._gh')
    def test_unready_and_unconfirmed_merge_fail(self, gh, evidence, local):
        context = {'authorized_actions':['merge']}
        gh.side_effect = [self.pr(isDraft=True)]
        with self.assertRaises(DeliveryError):
            github.merge_pr(context, {}, {}, 'owner/repo', 3, ['tests'])
        gh.side_effect = [self.pr(), '', self.pr()]
        with self.assertRaises(DeliveryError):
            github.merge_pr(context, {}, {}, 'owner/repo', 3, ['tests'])

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.github._assert_ready', return_value={'head':'a'*40})
    def test_invalid_branch_refuses(self, evidence, local):
        for branch in ['', '--help', 'fork:branch']:
            with self.assertRaises(DeliveryError):
                github.open_pr({'authorized_actions':['open-pr']}, {}, {}, 'owner/repo', 'T', 'B', 'main', branch)

    @patch('repo_delivery.github._local_head', return_value='b'*40)
    @patch('repo_delivery.github._assert_ready', return_value={'head':'a'*40})
    @patch('repo_delivery.github._gh')
    def test_local_head_changed_after_evidence_refuses(self, gh, evidence, local):
        context = {'authorized_actions':['open-pr', 'merge']}
        with self.assertRaises(DeliveryError):
            github.open_pr(context, {}, {}, 'owner/repo', 'T', 'B', 'main', 'feature')
        gh.return_value = self.pr()
        with self.assertRaises(DeliveryError):
            github.merge_pr(context, {}, {}, 'owner/repo', 3, ['tests'])

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.github._assert_ready')
    @patch('repo_delivery.github._gh')
    def test_remote_read_time_source_change_prevents_mutation(self, gh, evidence, local):
        evidence.side_effect = [{'head':'a'*40}, DeliveryError('stale source')]
        gh.side_effect = [self.pr()]
        with self.assertRaises(DeliveryError):
            github.merge_pr({'authorized_actions':['merge']}, {}, {}, 'owner/repo', 3, ['tests'])
        self.assertEqual(gh.call_count, 1)

    @patch('repo_delivery.evidence.validate_context')
    def test_explicit_github_binding_required(self, validate):
        self.binding.stop()
        github._binding({'github_repository':'owner/repo'}, 'owner/repo')
        validate.assert_called_once()
        for context in [{}, {'github_repository':'owner/other'}]:
            with self.assertRaises(DeliveryError):
                github._binding(context, 'owner/repo')

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.evidence.snapshot', return_value={'snapshot_sha256':'snapshot'})
    @patch('repo_delivery.evidence.validate_seal')
    def test_terminal_reconciliation_requires_same_task_source_and_head(self, seal, snapshot, head):
        context = {'repository':'/fixture', 'include_ignored':[], 'context_sha256':'context'}
        gate = {'kind':'gate-receipt', 'context_sha256':'context', 'snapshot_sha256':'snapshot'}
        github._terminal_identity(context, gate, self.pr())
        for overrides in [{'kind':'other'}, {'context_sha256':'other'}, {'snapshot_sha256':'other'}]:
            with self.assertRaises(DeliveryError):
                github._terminal_identity(context, dict(gate, **overrides), self.pr())
        with self.assertRaises(DeliveryError):
            github._terminal_identity(context, gate, self.pr(headRefOid='b'*40))
        seal.side_effect = DeliveryError('invalid seal')
        with self.assertRaises(DeliveryError):
            github._terminal_identity(context, gate, self.pr())

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.github._assert_ready', return_value={'head':'a'*40})
    @patch('repo_delivery.github._gh')
    def test_explicit_policy_can_waive_github_review_only(self, gh, evidence, local):
        gh.side_effect = [self.pr(reviewDecision=''), '', self.pr(state='MERGED')]
        result = github.merge_pr({'authorized_actions':['merge'], 'github_review_required':False},
                                 {}, {}, 'owner/repo', 3, ['tests'])
        self.assertEqual(result['state'], 'merged')
        self.assertEqual(evidence.call_count, 2)

    @patch('repo_delivery.github._local_head', return_value='a'*40)
    @patch('repo_delivery.github._assert_ready', return_value={'head':'a'*40})
    @patch('repo_delivery.github._gh')
    def test_create_detects_remote_race_and_wrong_created_identity(self, gh, evidence, local):
        context = {'repository':'/fixture', 'authorized_actions':['open-pr']}
        for changed in [dict(headRefOid='b'*40), dict(headRefName='other'),
                        dict(baseRefName='other'), dict(state='CLOSED')]:
            gh.side_effect = [{'object':{'sha':'a'*40}}, [], {'object':{'sha':'a'*40}},
                              'https://github.com/owner/repo/pull/3', self.pr(**changed)]
            with self.assertRaisesRegex(DeliveryError, 'reconcile remote state'):
                github.open_pr(context, {}, {}, 'owner/repo', 'T', 'B', 'main', 'feature')
        gh.side_effect = [{'object':{'sha':'a'*40}}, [], {'object':{'sha':'b'*40}}]
        with self.assertRaisesRegex(DeliveryError, 'Remote branch changed'):
            github.open_pr(context, {}, {}, 'owner/repo', 'T', 'B', 'main', 'feature')
        for url in ['https://github.com/other/repo/pull/3', 'https://evil.example/pull/3',
                    'https://github.com/owner/repo/pull/0']:
            gh.side_effect = [{'object':{'sha':'a'*40}}, [], {'object':{'sha':'a'*40}}, url]
            with self.assertRaisesRegex(DeliveryError, 'URL is unexpected'):
                github.open_pr(context, {}, {}, 'owner/repo', 'T', 'B', 'main', 'feature')
