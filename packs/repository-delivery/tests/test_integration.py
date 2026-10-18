import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import sys
import threading
from test_core import RepositoryFixture
from repo_delivery.core import DeliveryError
from repo_delivery.evidence import new_context
from repo_delivery.integration import run_integration, assert_integration

class IntegrationTests(RepositoryFixture):
    def setUp(self):
        super().setUp()
        class Service(BaseHTTPRequestHandler):
            def do_GET(self):
                data=json.dumps({'version':'fixture-1.0'}).encode()
                self.send_response(200); self.send_header('Content-Length',str(len(data)));self.end_headers();self.wfile.write(data)
            def log_message(self,*args):
                pass
        self.server=ThreadingHTTPServer(('127.0.0.1',0),Service)
        threading.Thread(target=self.server.serve_forever,daemon=True).start()
        self.addCleanup(self.server.server_close)
        self.addCleanup(self.server.shutdown)

    def context(self):
        return new_context(self.repo,{'objective':'integration fixture','files_in_scope':['a'],
            'gate_commands':[[sys.executable,'-c','pass']], 'review_policy':'none',
            'authorized_actions':[],'actor_id':'coder','actor_context':'coding'},'HEAD')

    def spec(self):
        return {'required_mode':'real','services':[{'name':'http-fixture','mode':'real',
            'endpoint':f'http://127.0.0.1:{self.server.server_port}/version',
            'version_field':'version','expected_version':'fixture-1.0'}]}

    def test_real_http_proof_and_stale_source(self):
        context=self.context()
        proof=run_integration(context,self.spec())
        self.assertTrue(proof['passed'])
        self.assertEqual(proof['mode'],'real')
        self.assertEqual(proof['services'][0]['observed_version'],'fixture-1.0')
        assert_integration(context,proof)
        (self.repo/'a').write_text('changed')
        with self.assertRaises(DeliveryError):
            assert_integration(context,proof)

    def test_mock_and_mixed_never_substitute_required_real(self):
        context=self.context()
        for services in [[{'name':'mock','mode':'mock','version':'mock-v1'}],self.spec()['services']+[{'name':'mock','mode':'mock','version':'mock-v1'}]]:
            proof=run_integration(context,{'required_mode':'real','services':services})
            self.assertFalse(proof['passed'])
            self.assertFalse(proof['gates_executed'])
            self.assertIn(proof['mode'],('mock','mixed'))
        proof=run_integration(context,{'required_mode':'mixed','services':services})
        self.assertTrue(proof['passed'])

    def test_unavailable_and_wrong_version_report_blocker(self):
        context=self.context()
        spec=self.spec()
        spec['services'][0]['expected_version']='wrong'
        proof=run_integration(context,spec)
        self.assertFalse(proof['passed'])
        self.assertFalse(proof['gates_executed'])
        self.assertTrue(proof['blockers'])
        self.server.shutdown();self.server.server_close()
        spec=self.spec();spec['timeout_seconds']=0.1
        proof=run_integration(context,spec)
        self.assertFalse(proof['passed'])
        self.assertIn('unavailable',proof['blockers'][0])

    def test_bad_endpoint_and_duplicate_services_rejected(self):
        context=self.context()
        for endpoint in ['file:///etc/passwd','http://user:secret@example.test/version','http://localhost/version?token=secret']:
            spec=self.spec();spec['services'][0]['endpoint']=endpoint
            with self.assertRaises(DeliveryError):
                run_integration(context,spec)
        spec=self.spec();spec['services']*=2
        with self.assertRaises(DeliveryError):
            run_integration(context,spec)

    def test_required_real_policy_cannot_use_plain_gates_or_mock_proof(self):
        from repo_delivery.evidence import assert_ready,run_gates
        context=self.context()
        from repo_delivery.evidence import seal
        context={k:v for k,v in context.items() if k!='context_sha256'}
        context['integration_required_mode']='real'
        context=seal(context,'context_sha256')
        with self.assertRaisesRegex(DeliveryError,'proof'):
            assert_ready(context,run_gates(context),None)
        mock=run_integration(context,{'required_mode':'mock','services':[{'name':'mock','mode':'mock','version':'fixture'}]})
        self.assertFalse(mock['passed'])
        self.assertFalse(mock['gates_executed'])
        with self.assertRaises(DeliveryError):
            assert_ready(context,mock,None)
        self.assertTrue(assert_ready(context,run_integration(context,self.spec()),None))
