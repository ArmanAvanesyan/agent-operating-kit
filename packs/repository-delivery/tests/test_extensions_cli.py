import json
from pathlib import Path
import sys
from unittest.mock import patch
import test_delivery_cli as delivery_fixture
from test_core import RepositoryFixture
from repo_delivery.core import create_checkpoint

class ExtensionCLITests(RepositoryFixture):
    write_json=delivery_fixture.DeliveryCLITests.write_json
    call=delivery_fixture.DeliveryCLITests.call
    spec=delivery_fixture.DeliveryCLITests.spec
    context=delivery_fixture.DeliveryCLITests.context

    def test_mock_proof_cli_and_required_real_failure(self):
        context=self.context()
        spec=self.write_json('integration.json',{'required_mode':'mock','services':[{'name':'fixture','mode':'mock','version':'v1'}]})
        output=str(self.root/'proof.json')
        proof=self.call('integration','run','--context',context,'--spec',spec,'--output',output)
        self.assertTrue(proof['passed'])
        self.assertEqual(self.call('integration','verify','--context',context,'--proof',output)['mode'],'mock')
        second=self.root/'second-context.json'
        task_spec=self.write_json('real-policy.json',dict(self.spec(),integration_required_mode='real'))
        self.call('delivery','init','--repo',str(self.repo),'--baseline','HEAD','--spec',task_spec,'--output',str(second))
        self.assertFalse(self.call('delivery','status','--context',str(second),'--gates',output,expect=1)['ready'])

    def test_integration_metadata_preserves_checkpoint(self):
        cp=self.root/'cp'
        create_checkpoint(self.repo,cp)
        context=self.context(preservation=[str(cp)])
        spec=self.write_json('integration.json',{'required_mode':'mock','services':[{'name':'fixture','mode':'mock','version':'v1'}]})
        self.call('integration','run','--context',context,'--spec',spec,'--output',str(cp/'bad.json'),expect=1)
        self.assertFalse((cp/'bad.json').exists())

    def test_backlog_plan_and_apply_dispatch(self):
        spec=self.write_json('backlog.json',{'example':'input'})
        with patch('repo_delivery.planning.plan_backlog',return_value={'kind':'backlog-plan'}) as mock:
            self.call('backlog','plan','--repo',str(self.repo),'--spec',spec)
            self.assertEqual(mock.call_args.args[2],[])
        plan=self.write_json('plan.json',{'repository':str(self.repo)})
        with patch('repo_delivery.planning.apply_backlog',return_value={'kind':'backlog-result'}) as mock:
            self.call('backlog','apply','--plan',plan,'--repository','owner/repo','--authorize','update-backlog')
            self.assertEqual(mock.call_args.args[2],['update-backlog'])

    def test_runtime_command_does_not_overwrite_family(self):
        command=self.write_json('command.json',[sys.executable,'-c','pass'])
        with patch('repo_delivery.adapters.StdioTransport') as transport, patch('repo_delivery.adapters.SwarmdClient') as client:
            client.return_value.task.return_value={'task':{'id':'fixture'}}
            self.assertEqual(self.call('runtime','task','--command',command,'--task-id','fixture')['task']['id'],'fixture')
            self.assertEqual(transport.call_args.args[0],[sys.executable,'-c','pass'])
        ref={'attempt_id':'a','task_id':'t','worker':'w','revision':'a'*40,'tree':'b'*40,'environment':'test'}
        path=self.write_json('reference.json',ref)
        result=self.call('runtime','validate','--reference',path,'--expected',path)
        self.assertFalse(result['live_lease_verified'])
        with patch('repo_delivery.adapters.consume_swarmd_dump',return_value={'kind':'swarmd-reference'}) as consumer:
            self.call('runtime','reference','--export',path,'--attempt-id','a')
            self.assertEqual(consumer.call_args.args,(path,'a'))

    def test_optional_aok_and_hook_dispatch(self):
        with patch('repo_delivery.adapters.consume_aok',return_value={'kind':'aok-consumption'}) as consume:
            self.call('aok','consume','--packet','packet','--schema','schema','--schema-sha256','a'*64)
            self.assertEqual(consume.call_args.args,('packet','schema','a'*64))
        config=self.write_json('hook.json',{'repository':str(self.repo)})
        with patch('repo_delivery.hooks.check_hook',return_value={'kind':'hook-check','ready':True}):
            self.call('hook','check','--config',config)
        with patch('repo_delivery.hooks.install_hook',return_value={'kind':'hook-installation'}) as install:
            self.call('hook','install','--repo',str(self.repo),'--config',config,'--authorize','install-hook')
            self.assertEqual(install.call_args.args[2],['install-hook'])

    def test_runtime_task_real_cli_with_simulated_stdio_peer(self):
        import subprocess
        peer='''import sys,json
for line in sys.stdin:
 r=json.loads(line)
 if 'id' not in r: continue
 if r['method']=='initialize':
  result={'protocolVersion':'2024-11-05','serverInfo':{'name':'swarmd','version':'fixture'}}
 else:
  result={'isError':False,'content':[{'type':'text','text':json.dumps({'task':{'id':'fixture'}})}]}
 print(json.dumps({'jsonrpc':'2.0','id':r['id'],'result':result}),flush=True)
'''
        command=self.write_json('peer.json',[sys.executable,'-u','-c',peer])
        proc=subprocess.run([sys.executable,str(Path(__file__).resolve().parents[1]/'scripts/repo-delivery'),
             'runtime','task','--command',command,'--task-id','fixture'],capture_output=True,text=True,timeout=10)
        self.assertEqual(proc.returncode,0,proc.stderr)
        self.assertEqual(json.loads(proc.stdout)['task']['id'],'fixture')
