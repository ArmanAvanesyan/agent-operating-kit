import os
from pathlib import Path
import sys
from unittest.mock import patch

from test_core import RepositoryFixture
from repo_delivery.core import DeliveryError
from repo_delivery.evidence import snapshot, new_context, run_gates, record_review, assert_ready, delivery_status

class EvidenceTests(RepositoryFixture):
    def spec(self):
        return {'objective':'Implement fixture', 'files_in_scope':['a'],
                'gate_commands':[[sys.executable,'-c','print("gate")']],
                'review_policy':'independent','authorized_actions':['commit','open-pr','merge'],
                'actor_id':'implementer','actor_context':'implementation-session',
                'environment_keys':['REPO_DELIVERY_TEST_ENV']}

    def context(self):
        return new_context(self.repo, self.spec(), 'HEAD')

    def review(self, context):
        return record_review(context, {'reviewer':'reviewer','reviewer_context':'separate-session',
                'snapshot_sha256':snapshot(self.repo)['snapshot_sha256'], 'outcome':'approved',
                'report':'No findings'})

    def test_readiness_binds_bytes_not_old_head(self):
        context = self.context()
        (self.repo/'a').write_text('new working tree')
        gates = run_gates(context)
        review = self.review(context)
        self.assertEqual(assert_ready(context,gates,review)['snapshot_sha256'],gates['snapshot_sha256'])
        (self.repo/'a').write_text('changed with same HEAD and status')
        with self.assertRaisesRegex(DeliveryError,'stale'):
            assert_ready(context,gates,review)

    def test_failed_or_mutating_gate_cannot_certify_source(self):
        spec = self.spec()
        spec['gate_commands']=[[sys.executable,'-c','raise SystemExit(3)']]
        context = new_context(self.repo,spec,'HEAD')
        gates = run_gates(context)
        self.assertFalse(gates['passed'])
        with self.assertRaises(DeliveryError):
            assert_ready(context,gates,self.review(context))
        spec['gate_commands']=[[sys.executable,'-c','open("a","w").write("mutated")']]
        gates = run_gates(new_context(self.repo,spec,'HEAD'))
        self.assertFalse(gates['passed'])
        self.assertFalse(gates['source_unchanged'])

    def test_independent_review_distinct_from_implementer(self):
        context=self.context()
        for update in [{'reviewer':'implementer'}, {'reviewer_context':'implementation-session'}]:
            review={'reviewer':'reviewer','reviewer_context':'separate','snapshot_sha256':snapshot(self.repo)['snapshot_sha256'], 'outcome':'approved','report':'report'}
            review.update(update)
            with self.assertRaises(DeliveryError):
                record_review(context,review)

    def test_context_tamper_scope_and_environment(self):
        context=self.context()
        invalid=dict(context,authorized_actions=['cleanup'])
        with self.assertRaises(DeliveryError):
            run_gates(invalid)
        (self.repo/'outside').write_text('out of scope')
        with self.assertRaisesRegex(DeliveryError,'scope'):
            run_gates(context)
        (self.repo/'outside').unlink()
        with patch.dict(os.environ, {'REPO_DELIVERY_TEST_ENV':'first'}):
            gates=run_gates(context)
            review=self.review(context)
        with patch.dict(os.environ, {'REPO_DELIVERY_TEST_ENV':'second'}):
            with self.assertRaisesRegex(DeliveryError,'environment'):
                assert_ready(context,gates,review)

    def test_resume_reconciles_stale_or_missing_evidence(self):
        context=self.context()
        self.assertFalse(delivery_status(context,None,None)['ready'])
        gates=run_gates(context)
        review=self.review(context)
        self.assertTrue(delivery_status(context,gates,review)['ready'])
        (self.repo/'a').write_text('change')
        self.assertFalse(delivery_status(context,gates,review)['ready'])

    def test_gate_timeout_and_no_review_policy(self):
        spec=self.spec()
        spec['review_policy']='none'
        spec['gate_timeout_seconds']=0.05
        spec['gate_commands']=[[sys.executable,'-c','import time; time.sleep(1)']]
        context=new_context(self.repo,spec,'HEAD')
        self.assertFalse(run_gates(context)['passed'])
        spec['gate_commands']=[[sys.executable,'-c','pass']]
        context=new_context(self.repo,spec,'HEAD')
        self.assertTrue(assert_ready(context,run_gates(context),None))

    def test_scope_includes_staged_content_even_when_working_bytes_restored(self):
        (self.repo/'outside').write_text('baseline')
        self.git('add','outside')
        self.git('commit','-m','outside baseline')
        context=self.context()
        (self.repo/'outside').write_text('staged out of scope')
        self.git('add','outside')
        (self.repo/'outside').write_text('baseline')
        with self.assertRaisesRegex(DeliveryError,'scope'):
            run_gates(context)

    def test_selected_ignored_payload_requires_scope(self):
        (self.repo/'.gitignore').write_text('ignored\n')
        self.git('add','.gitignore')
        self.git('commit','-m','ignore')
        (self.repo/'ignored').write_text('payload')
        spec=self.spec()
        spec['include_ignored']=['ignored']
        context=new_context(self.repo,spec,'HEAD')
        with self.assertRaisesRegex(DeliveryError,'scope'):
            run_gates(context)
