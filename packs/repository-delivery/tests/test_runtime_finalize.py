"""Fenced finalization contract tests; mocked peers never touch runtime queues."""
import json
import os
from pathlib import Path
import tempfile
import unittest

from repo_delivery.adapters import SwarmdClient
from repo_delivery.core import DeliveryError

REFERENCE = dict(task_id='task', attempt_id='attempt', worker='forge',
                 revision='a'*40, tree='b'*40, environment='fixture')
FIELDS = {'id': 'string', 'attempt_id': 'string', 'worker': 'string',
          'lease_token': 'string', 'revision': 'string', 'tree': 'string',
          'environment': 'string', 'success': 'boolean'}


class FinalizeTests(unittest.TestCase):
    def peer(self, *, schema=True, result=None, error=None):
        calls = []
        def transport(method, params):
            calls.append((method, params))
            if method == 'initialize':
                return {'protocolVersion': '2024-11-05', 'serverInfo': {'name': 'swarmd', 'version': 'fixture'}}
            if method == 'notifications/initialized': return None
            if method == 'tools/list':
                return {'tools': [{'name': 'task.finalize', 'inputSchema': {
                    'type': 'object', 'required': list(FIELDS),
                    'properties': {k: {'type': v} for k, v in FIELDS.items()}}}] if schema else []}
            if error: raise RuntimeError(error)
            return result or {'isError': False, 'content': [{'type': 'text', 'text': json.dumps({'id':'task','status':'complete','claimed_by':'forge','lease_token':'secret'})}]}
        return SwarmdClient(transport), calls

    def test_finalizes_exact_attempt_and_returns_only_sanitized_receipt(self):
        client, calls = self.peer()
        receipt = client.finish(REFERENCE, REFERENCE, lease_token='secret', authorized_actions=['finalize-attempt'])
        self.assertEqual(receipt['outcome'], 'complete')
        self.assertNotIn('secret', json.dumps(receipt))
        self.assertEqual(calls[-1][1]['name'], 'task.finalize')
        self.assertEqual(calls[-1][1]['arguments'], dict(id='task', **{k:v for k,v in REFERENCE.items() if k != 'task_id'}, lease_token='secret', success=True))

    def test_invalid_authorization_and_identity_do_not_launch_peer(self):
        for kwargs in [{}, {'authorized_actions':['finalize-attempt'], 'success':1}]:
            client, calls = self.peer()
            with self.assertRaises(DeliveryError): client.finish(REFERENCE, REFERENCE, lease_token='secret', **kwargs)
            self.assertEqual(calls, [])
        client, calls = self.peer()
        with self.assertRaises(DeliveryError):
            client.finish(REFERENCE, dict(REFERENCE,worker='other'), lease_token='secret', authorized_actions=['finalize-attempt'])
        self.assertEqual(calls, [])

    def test_unsupported_peer_never_falls_back_to_legacy(self):
        client, calls = self.peer(schema=False)
        with self.assertRaisesRegex(DeliveryError, 'capability'):
            client.finish(REFERENCE, REFERENCE, lease_token='secret', authorized_actions=['finalize-attempt'])
        self.assertFalse(any(m == 'tools/call' for m, _ in calls))

    def test_wire_error_and_conflicting_terminal_reply_are_not_success(self):
        for options in [{'error':'secret'}, {'result': {'isError':True,'content':[{'type':'text','text':'secret'}]}},
                        {'result':{'isError':False,'content':[{'type':'text','text':'{"id":"task","status":"failed","claimed_by":"forge"}'}]}}]:
            client, calls = self.peer(**options)
            with self.assertRaises(DeliveryError) as caught:
                client.finish(REFERENCE, REFERENCE, lease_token='secret', authorized_actions=['finalize-attempt'])
            self.assertNotIn('secret', str(caught.exception))
            self.assertIsNone(caught.exception.__cause__)
            self.assertEqual(sum(m == 'tools/call' for m,_ in calls), 1)


class CredentialTests(unittest.TestCase):
    def test_private_credentials_identity_permissions_and_symlinks(self):
        from repo_delivery.runtime_finalize import credentials
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            path = root/'credentials.json'
            data = {k:REFERENCE[k] for k in ('task_id','attempt_id','worker')}
            data['lease_token'] = 'private-token'
            path.write_text(json.dumps(data)); path.chmod(0o600)
            self.assertEqual(credentials(path, REFERENCE), 'private-token')
            for mode in [0o644,0o400,0o700]:
                path.chmod(mode)
                with self.assertRaises(DeliveryError): credentials(path, REFERENCE)
            path.chmod(0o600)
            with self.assertRaises(DeliveryError): credentials(path, dict(REFERENCE,task_id='other'))
            with self.assertRaises(DeliveryError): credentials(Path('relative'), REFERENCE)
            link = root/'link'; link.symlink_to(path)
            with self.assertRaises(DeliveryError): credentials(link, REFERENCE)
            link.unlink(); link.symlink_to(root, target_is_directory=True)
            with self.assertRaises(DeliveryError): credentials(link/'credentials.json', REFERENCE)
            path.write_text(json.dumps(dict(data,unexpected='value')))
            with self.assertRaises(DeliveryError): credentials(path, REFERENCE)
            path.write_text('{"lease_token":"secret","lease_token":"again"}')
            with self.assertRaises(DeliveryError): credentials(path, REFERENCE)
            path.write_text('x'*16385)
            with self.assertRaises(DeliveryError): credentials(path, REFERENCE)

    def test_cli_checks_credentials_before_launching_transport(self):
        from repo_delivery.cli import main
        from unittest.mock import patch
        import contextlib
        import io
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            reference = root/'reference'; reference.write_text(json.dumps(REFERENCE))
            command = root/'command'; command.write_text('["must-not-launch"]')
            credentials = root/'credentials'
            credentials.write_text(json.dumps(dict(task_id='wrong',attempt_id='attempt',worker='forge',lease_token='secret')))
            credentials.chmod(0o600)
            with patch('repo_delivery.adapters.StdioTransport') as transport, contextlib.redirect_stderr(io.StringIO()) as stderr:
                self.assertEqual(main(['runtime','finish','--command',str(command),'--reference',str(reference),'--expected',str(reference), '--credentials',str(credentials),'--outcome','complete','--authorize','finalize-attempt']), 1)
                transport.assert_not_called()
                self.assertNotIn('secret', stderr.getvalue())

    def test_cli_success_private_receipt_and_failed_outcome(self):
        from repo_delivery.cli import main
        from unittest.mock import patch
        import contextlib
        import io
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory).resolve()
            reference=root/'reference'; reference.write_text(json.dumps(REFERENCE))
            command=root/'command'; command.write_text('["fixture-peer"]')
            credentials=root/'credentials'; credentials.write_text(json.dumps(dict(task_id='task',attempt_id='attempt',worker='forge',lease_token='secret'))); credentials.chmod(0o600)
            client,calls=FinalizeTests().peer(result={'isError':False,'content':[{'type':'text','text':'{"id":"task","status":"failed","claimed_by":"forge"}'}]})
            output=root/'receipt'
            with patch('repo_delivery.adapters.StdioTransport') as transport, contextlib.redirect_stdout(io.StringIO()) as stdout:
                transport.return_value.__enter__.return_value=client.transport
                self.assertEqual(main(['runtime','finish','--command',str(command),'--reference',str(reference),'--expected',str(reference),'--credentials',str(credentials),'--outcome','failed','--authorize','finalize-attempt','--output',str(output)]),0)
            self.assertEqual(json.loads(output.read_text())['outcome'],'failed')
            self.assertNotIn('secret',stdout.getvalue()+output.read_text())
            self.assertEqual(output.stat().st_mode & 0o777,0o600)

    def test_malformed_schema_refuses_mutation(self):
        for bad in [None, [], {'type':'object','required':['id'],'properties':{}}, {'type':'object','required':None}]:
            client,calls=FinalizeTests().peer()
            original=client.transport
            def transport(method,params):
                if method=='tools/list': return {'tools':[{'name':'task.finalize','inputSchema':bad}]}
                return original(method,params)
            client.transport=transport
            with self.assertRaisesRegex(DeliveryError,'capability'):
                client.finish(REFERENCE,REFERENCE,lease_token='secret',authorized_actions=['finalize-attempt'])
            self.assertFalse(any(m=='tools/call' for m,_ in calls))

    def test_receipt_matches_schema(self):
        import jsonschema
        client,_=FinalizeTests().peer()
        result=client.finish(REFERENCE,REFERENCE,lease_token='secret',authorized_actions=['finalize-attempt'])
        schema=json.loads((Path(__file__).resolve().parents[1]/'config/schemas/runtime-finalization.json').read_text())
        jsonschema.Draft202012Validator(schema).validate(result)
