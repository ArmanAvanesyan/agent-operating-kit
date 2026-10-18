"""Opt-in real swarmd process/SQLite/MCP integration, always a fresh temp DB.

Set REPO_DELIVERY_SWARMD_BINARY to a binary built from a pinned clean revision.
No hosted service, live work queue, or embedder network is involved. Fake embedding
is configured but unused: these tests exercise task APIs, not vector memory.
"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from repo_delivery.adapters import StdioTransport, SwarmdClient, consume_swarmd_dump
from repo_delivery.core import DeliveryError


class RuntimeFixture(unittest.TestCase):
    def setUp(self):
        self.binary = Path(os.environ['REPO_DELIVERY_SWARMD_BINARY']).resolve(strict=True)
        self.temp = tempfile.TemporaryDirectory(prefix='repo-delivery-runtime-test-')
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)
        self.database = self.directory / 'fixture.db'
        # Remove inherited provider settings: only the fixture database is opened.
        environment = {k: v for k, v in os.environ.items() if not k.startswith('SWARMD_')}
        environment['SWARMD_EMBEDDER_PROVIDER'] = 'fake'
        self.environment = environment
        environment_patch = patch.dict(os.environ, environment, clear=True)
        environment_patch.start()
        self.addCleanup(environment_patch.stop)
        initialized = self.command('init', '--db', str(self.database))
        self.assertIn('initialized', initialized)

    def command(self, *args):
        return subprocess.check_output([str(self.binary), *args], cwd=self.directory,
                                       env=self.environment, stderr=subprocess.PIPE,
                                       text=True, timeout=15)

    def transport(self):
        return StdioTransport([str(self.binary), 'mcp', '--db', str(self.database)], timeout=10)


@unittest.skipUnless(os.environ.get('REPO_DELIVERY_SWARMD_BINARY'),
                     'Opt-in real runtime test: set REPO_DELIVERY_SWARMD_BINARY')
class SwarmdRuntimeIntegrationTests(RuntimeFixture):
    def test_real_mcp_read_after_fixture_task_creation(self):
        with self.transport() as transport:
            client = SwarmdClient(transport)
            client.initialize()
            result = transport('tools/call', {'name': 'task.create', 'arguments': {
                'title': 'Disposable runtime fixture', 'spec': 'Integration proof only',
                'assignee': 'fixture'}})
            self.assertFalse(result['isError'])
            created = json.loads(result['content'][0]['text'])
            observed = client.task(created['id'])
            self.assertEqual(observed['task']['id'], created['id'])
            self.assertEqual(observed['task']['status'], 'pending')
            self.assertEqual(client.server['name'], 'swarmd')
        # A new process observes persisted state from the same disposable DB.
        with self.transport() as transport:
            observed = SwarmdClient(transport).task(created['id'])
            self.assertEqual(observed['task']['title'], 'Disposable runtime fixture')
        rows = [json.loads(line) for line in self.command('dump', '--db', str(self.database), '--table', 'tasks').splitlines()]
        self.assertEqual([row['id'] for row in rows], [created['id']])

    def test_real_empty_attempt_export_and_tool_errors(self):
        data = self.command('dump', '--db', str(self.database), '--table', 'execution_attempts')
        self.assertEqual(data, '')
        export = self.directory / 'attempts.ndjson'
        export.write_text(data)
        with self.assertRaisesRegex(DeliveryError, 'Missing'):
            consume_swarmd_dump(export, 'nonexistent-fixture-attempt')
        with self.transport() as transport:
            with self.assertRaisesRegex(DeliveryError, 'tool failed'):
                SwarmdClient(transport).task('nonexistent-fixture-task')


@unittest.skipUnless(os.environ.get('REPO_DELIVERY_SWARMD_BINARY') and os.environ.get('REPO_DELIVERY_SWARMD_FIXTURE_BINARY'),
                     'Set runtime and test-only attempt fixture binaries')
class FencedRuntimeIntegrationTests(RuntimeFixture):
    """Real service-created attempts, SQLite persistence, and MCP finalization."""
    def fixture(self, *, outcome='complete', scenario=None):
        repository = self.directory/'source'
        repository.mkdir()
        def git(*args):
            return subprocess.check_output(['git',*args],cwd=repository,text=True).strip()
        git('init','-q'); git('config','user.name','Fixture'); git('config','user.email','fixture@example.invalid')
        (repository/'artifact').write_text('revision-bound fixture\n')
        git('add','.'); git('commit','-qm','fixture')
        directory=self.directory/'attempt'
        command=[str(Path(os.environ['REPO_DELIVERY_SWARMD_FIXTURE_BINARY']).resolve(strict=True)),
                 '--db',str(self.database),'--directory',str(directory),'--outcome',outcome,
                 '--revision',git('rev-parse','HEAD'),'--tree',git('rev-parse','HEAD^{tree}')]
        if scenario: command.extend(['--scenario',scenario])
        result=subprocess.run(command,env=self.environment,cwd=self.directory,capture_output=True,text=True,timeout=15,check=True)
        self.assertEqual(result.stdout,'')
        reference=json.loads((directory/'reference.json').read_text())
        from repo_delivery.runtime_finalize import credentials
        token=credentials((directory/'credentials.json').resolve(),reference)
        return reference,token,directory

    def test_real_fenced_success_persists_handoff_and_review_child(self):
        reference,token,_=self.fixture()
        with self.transport() as transport:
            result=SwarmdClient(transport).finish(reference,reference,lease_token=token,authorized_actions=['finalize-attempt'])
            self.assertEqual(result['outcome'],'complete')
            self.assertNotIn(token,json.dumps(result))
        with self.transport() as transport:
            task=SwarmdClient(transport).task(reference['task_id'])
            self.assertEqual(task['task']['status'],'complete')
            self.assertEqual(len(task['children']),1)
        attempts=[json.loads(line) for line in self.command('dump','--db',str(self.database),'--table','execution_attempts').splitlines()]
        self.assertEqual(attempts[0]['state'],'complete')
        self.assertEqual(attempts[0]['id'],reference['attempt_id'])

    def test_real_cli_fenced_finalization_keeps_token_out_of_output(self):
        import sys
        reference,token,directory=self.fixture()
        command=self.directory/'host-command.json'
        command.write_text(json.dumps([str(self.binary),'mcp','--db',str(self.database)]))
        output=self.directory/'finalization.json'
        environment=dict(self.environment, PYTHONPATH=str(Path(__file__).resolve().parents[1]/'src'))
        result=subprocess.run([sys.executable,'-m','repo_delivery.cli','runtime','finish',
              '--command',str(command),'--reference',str(directory/'reference.json'),
              '--expected',str(directory/'reference.json'),'--credentials',str((directory/'credentials.json').resolve()),
              '--outcome','complete','--authorize','finalize-attempt','--output',str(output)],
              env=environment,cwd=self.directory,capture_output=True,text=True,timeout=15,check=True)
        self.assertEqual(json.loads(result.stdout)['outcome'],'complete')
        self.assertNotIn(token,result.stdout+result.stderr+output.read_text())

    def test_real_failed_outcome_and_retry_conflict(self):
        reference,token,_=self.fixture(outcome='failed')
        with self.transport() as transport:
            client=SwarmdClient(transport)
            result=client.finish(reference,reference,lease_token=token,success=False,authorized_actions=['finalize-attempt'])
            self.assertEqual(result['outcome'],'failed')
            with self.assertRaises(DeliveryError):
                client.finish(reference,reference,lease_token=token,success=False,authorized_actions=['finalize-attempt'])
        with self.transport() as transport:
            observed=SwarmdClient(transport).task(reference['task_id'])
            self.assertEqual(observed['task']['status'],'failed')
            self.assertEqual(observed['children'],[])

    def test_real_wrong_token_revision_and_outcome_do_not_mutate(self):
        reference,token,_=self.fixture()
        cases=[(reference,'wrong-token',True),(dict(reference,revision='f'*40),token,True),(reference,token,False)]
        for supplied,credential,success in cases:
            with self.transport() as transport:
                with self.assertRaises(DeliveryError):
                    SwarmdClient(transport).finish(supplied,supplied,lease_token=credential,success=success,authorized_actions=['finalize-attempt'])
            with self.transport() as transport:
                observed=SwarmdClient(transport).task(reference['task_id'])
                self.assertEqual(observed['task']['status'],'claimed')
                self.assertEqual(observed['children'],[])

    def test_real_changed_artifact_refuses_finalization_atomically(self):
        reference,token,directory=self.fixture()
        (directory/'provider.log').write_text('tampered artifact after record\n')
        with self.transport() as transport:
            with self.assertRaises(DeliveryError):
                SwarmdClient(transport).finish(reference,reference,lease_token=token,authorized_actions=['finalize-attempt'])
        with self.transport() as transport:
            observed=SwarmdClient(transport).task(reference['task_id'])
            self.assertEqual(observed['task']['status'],'claimed')
            self.assertEqual(observed['children'],[])
        attempts=[json.loads(line) for line in self.command('dump','--db',str(self.database),'--table','execution_attempts').splitlines()]
        self.assertEqual(attempts[0]['state'],'recorded')

    def test_real_expired_and_revoked_leases_refuse_finalization(self):
        for scenario in ['expired','revoked']:
            with self.subTest(scenario=scenario):
                # Each fixture gets its own fresh DB and directory.
                if scenario=='revoked':
                    self.database=self.directory/'revoked.db'
                    self.command('init','--db',str(self.database))
                    import shutil
                    shutil.rmtree(self.directory/'source'); shutil.rmtree(self.directory/'attempt')
                reference,token,_=self.fixture(scenario=scenario)
                with self.transport() as transport:
                    with self.assertRaises(DeliveryError):
                        SwarmdClient(transport).finish(reference,reference,lease_token=token,authorized_actions=['finalize-attempt'])
                with self.transport() as transport:
                    self.assertEqual(SwarmdClient(transport).task(reference['task_id'])['task']['status'],'claimed')
