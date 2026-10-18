"""Contract tests use mocks; no user runtime or provider is contacted."""
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from repo_delivery.adapters import consume_aok, SwarmdClient, validate_attempt_reference
from repo_delivery.core import DeliveryError

class AdapterTests(unittest.TestCase):
    def test_aok_keeps_policy_status_and_command_boundaries(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            schema = {'$id':'fixture-task-packet','type':'object','required':['title','spec','status','acceptanceCriteria','verification']}
            (root/'schema').write_text(json.dumps(schema))
            packet = {'title':'Work','spec':'Scope','status':'blocked','acceptanceCriteria':['observable'], 'verification':{'commands':['make quality && echo x']}}
            (root/'packet').write_text(json.dumps(packet))
            sha = hashlib.sha256((root/'schema').read_bytes()).hexdigest()
            result = consume_aok(root/'packet',root/'schema',sha)
            self.assertEqual(result['packet'],packet)
            self.assertEqual(result['proposal']['verification_commands'],['make quality && echo x'])
            self.assertIsNone(result['runtime_status'])
            self.assertIn('blocked', ' '.join(result['gaps']))
            with self.assertRaises(DeliveryError):
                consume_aok(root/'packet',root/'schema','0'*64)
            (root/'packet').write_text('{}')
            with self.assertRaises(DeliveryError):
                consume_aok(root/'packet',root/'schema',sha)

    def test_reference_requires_exact_attempt_revision_and_environment(self):
        reference = {'attempt_id':'attempt','task_id':'task','worker':'forge','revision':'a'*40,'tree':'b'*40,'environment':'fixture'}
        self.assertEqual(validate_attempt_reference(reference,reference), reference)
        for key in reference:
            with self.subTest(key=key):
                with self.assertRaises(DeliveryError):
                    validate_attempt_reference({k:v for k,v in reference.items() if k != key},reference)
                with self.assertRaises(DeliveryError):
                    validate_attempt_reference(dict(reference,**{key:'different'}),reference)

    def test_no_managed_finish_downgrade_to_legacy_mcp(self):
        calls=[]
        client=SwarmdClient(lambda method,params: calls.append((method,params)))
        reference={'attempt_id':'attempt','task_id':'task','worker':'forge','revision':'a'*40,'tree':'b'*40,'environment':'fixture'}
        with self.assertRaisesRegex(DeliveryError,'authorization'):
            client.finish(reference,reference)
        self.assertEqual(calls,[])

    def test_mock_transport_task_reference_and_tool_error(self):
        calls=[]
        def transport(method,params):
            calls.append((method,params))
            if method=='initialize':
                return {'protocolVersion':'2024-11-05','serverInfo':{'name':'swarmd','version':'fixture'}}
            if method=='notifications/initialized': return None
            return {'content':[{'type':'text','text':json.dumps({'task':{'id':'task','status':'claimed'},'children':[]})}], 'isError':False}
        client=SwarmdClient(transport)
        self.assertEqual(client.task('task')['task']['id'],'task')
        self.assertEqual(calls[-1],('tools/call',{'name':'task.tree','arguments':{'id':'task'}}))
        def failed(method,params):
            if method=='initialize': return {'protocolVersion':'unsupported'}
        with self.assertRaises(DeliveryError): SwarmdClient(failed).task('task')

    def test_stdio_peer_timeout_and_remote_error(self):
        from repo_delivery.adapters import StdioTransport
        peer='import sys,json\nfor line in sys.stdin:\n r=json.loads(line)\n if "id" in r: print(json.dumps({"jsonrpc":"2.0","id":r["id"],"error":{"code":-32601,"message":"fixture"}}),flush=True)'
        with StdioTransport([sys.executable,'-u','-c',peer],timeout=1) as transport:
            with self.assertRaisesRegex(DeliveryError,'RPC'):
                transport('initialize',{})
        with StdioTransport([sys.executable,'-c','import time;time.sleep(5)'],timeout=.05) as transport:
            with self.assertRaisesRegex(DeliveryError,'timeout'):
                transport('initialize',{})

    def test_real_upstream_schema_compatibility(self):
        schema=Path(__file__).parent/'fixtures/aok/task-packet.schema.json'
        sha='ac3abdd566dd090cc45300c71ca4174ad23ddef5552e9935a8c20175fde2478d'
        with tempfile.TemporaryDirectory() as directory:
            packet=Path(directory)/'packet'
            packet.write_text(json.dumps({'title':'Work','spec':'Scope','status':'blocked','phase':'build', 'acceptanceCriteria':['observable'],'verification':{'commands':['make quality']}}))
            self.assertEqual(consume_aok(packet,schema,sha)['packet']['status'],'blocked')
            packet.write_text(packet.read_text().replace('blocked','failed'))
            with self.assertRaises(DeliveryError): consume_aok(packet,schema,sha)

    def test_durable_export_reference_uses_actual_swarmd_dump_shape(self):
        from repo_delivery.adapters import consume_swarmd_dump
        row={'id':'attempt','task_id':'task','worker':'forge','state':'recorded','environment':'fixture', 'record_json':json.dumps({'Revision':'a'*40,'Tree':'b'*40,'Environment':'fixture'})}
        with tempfile.TemporaryDirectory() as directory:
            path=Path(directory)/'dump.ndjson'
            path.write_text(json.dumps(row)+'\n')
            result=consume_swarmd_dump(path,'attempt')
            self.assertEqual(result['reference']['revision'],'a'*40)
            self.assertFalse(result['capabilities']['managed_finish'])
            for update in [{'state':'running'},{'environment':'other'},{'record_json':'{}'}]:
                path.write_text(json.dumps(dict(row,**update)))
                with self.assertRaises(DeliveryError): consume_swarmd_dump(path,'attempt')

    def test_policy_is_validated_and_retained_without_execution(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory)
            schema={'type':'object','required':['title','spec','status','acceptanceCriteria','verification']}
            (root/'schema').write_text(json.dumps(schema))
            packet={'title':'Work','spec':'Scope','status':'ready','acceptanceCriteria':['observable'],'verification':{'commands':[]}}
            (root/'packet').write_text(json.dumps(packet))
            sha=hashlib.sha256((root/'schema').read_bytes()).hexdigest()
            (root/'policy').write_text('{"gates":["host-reviewed"]}')
            (root/'policy-schema').write_text('{"type":"object","required":["gates"]}')
            policy_sha=hashlib.sha256((root/'policy-schema').read_bytes()).hexdigest()
            result=consume_aok(root/'packet',root/'schema',sha,policy_path=root/'policy',policy_schema_path=root/'policy-schema',policy_schema_sha256=policy_sha)
            self.assertEqual(result['policy'],{'gates':['host-reviewed']})
            with self.assertRaises(DeliveryError): consume_aok(root/'packet',root/'schema',sha,policy_path=root/'policy')
            (root/'schema').write_text('{"$ref":"https://example.invalid/schema"}')
            sha=hashlib.sha256((root/'schema').read_bytes()).hexdigest()
            with self.assertRaisesRegex(DeliveryError,'External'): consume_aok(root/'packet',root/'schema',sha)

    def test_stdio_malformed_envelopes_and_valid_response(self):
        from repo_delivery.adapters import StdioTransport
        for envelope in ['not-json', '{"jsonrpc":"1.0","id":1,"result":{}}', '{"jsonrpc":"2.0","id":999,"result":{}}','{"jsonrpc":"2.0","id":1}']:
            with self.subTest(envelope=envelope):
                peer='import sys;sys.stdin.readline();print('+repr(envelope)+',flush=True)'
                with StdioTransport([sys.executable,'-u','-c',peer],timeout=1) as transport:
                    with self.assertRaises(DeliveryError): transport('initialize',{})
        peer='import sys,json\nfor line in sys.stdin:\n r=json.loads(line)\n if "id" in r: print(json.dumps({"jsonrpc":"2.0","id":r["id"],"result":{"ok":True}}),flush=True)'
        with StdioTransport([sys.executable,'-u','-c',peer],timeout=1) as transport:
            self.assertIsNone(transport('notifications/initialized',{}))
            self.assertEqual(transport('ping',{}),{'ok':True})
        for timeout in [0,float('inf'),True]:
            with self.assertRaises(DeliveryError): StdioTransport([sys.executable],timeout)

    def test_swarmd_tool_errors_are_not_task_observations(self):
        initialization={'protocolVersion':'2024-11-05','serverInfo':{'name':'swarmd','version':'fixture'}}
        for result in [{'isError':True}, {'isError':False,'content':[]}, {'isError':False,'content':[{'type':'text','text':'bad'}]}, {'isError':False,'content':[{'type':'text','text':'{"task":{"id":"other"}}'}]}]:
            def transport(method,params):
                if method=='initialize': return initialization
                if method=='notifications/initialized': return None
                return result
            with self.assertRaises(DeliveryError): SwarmdClient(transport).task('task')
