import contextlib
import io
import json
import os
from pathlib import Path
import sys
from unittest.mock import patch

from test_core import RepositoryFixture
from repo_delivery.cli import main
from repo_delivery.core import create_checkpoint, verify_checkpoint

class DeliveryCLITests(RepositoryFixture):
    def write_json(self,name,value):
        path=self.root/name
        path.write_text(json.dumps(value))
        return str(path)

    def call(self,*args,expect=0):
        with contextlib.redirect_stdout(io.StringIO()) as out, contextlib.redirect_stderr(io.StringIO()) as err:
            code=main(list(args))
        self.assertEqual(code,expect,err.getvalue())
        return json.loads(out.getvalue()) if out.getvalue() else json.loads(err.getvalue())

    def spec(self,**changes):
        spec={'objective':'CLI fixture','files_in_scope':['a'],
              'gate_commands':[[sys.executable,'-c','pass']], 'review_policy':'none',
              'authorized_actions':['isolate','port','cleanup'],'actor_id':'fixture','actor_context':'fixture-session'}
        return dict(spec,**changes)

    def context(self,**changes):
        spec=self.write_json('spec.json',self.spec(**changes))
        output=str(self.root/'context.json')
        self.call('delivery','init','--repo',str(self.repo),'--baseline','HEAD','--spec',spec,'--output',output)
        return output

    def test_delivery_fixture_status_and_failed_gate_exit(self):
        ctx=self.context(gate_commands=[[sys.executable,'-c','raise SystemExit(2)']])
        self.call('snapshot','--repo',str(self.repo))
        out=str(self.root/'gates.json')
        self.assertFalse(self.call('gates','run','--context',ctx,'--output',out,expect=1)['passed'])
        self.assertFalse(self.call('delivery','status','--context',ctx,'--gates',out,expect=1)['ready'])
        self.assertTrue(Path(out).exists())
        self.assertEqual(Path(out).stat().st_mode & 0o777,0o600)

    def test_checkpoint_protected_before_gate_or_context_output(self):
        cp=self.root/'checkpoint'
        receipt=create_checkpoint(self.repo,cp)
        ctx=self.context(preservation=[str(cp)])
        self.call('gates','run','--context',ctx,'--output',str(cp/'bad.json'),expect=1)
        self.assertEqual(verify_checkpoint(cp),receipt)
        spec=self.write_json('second-spec.json',self.spec(preservation=[str(cp)]))
        self.call('delivery','init','--repo',str(self.repo),'--baseline','HEAD','--spec',spec,'--output',str(cp/'bad2.json'),expect=1)
        self.assertEqual(verify_checkpoint(cp),receipt)

    def test_output_refuses_overwrite_and_repository(self):
        ctx=self.context()
        self.call('gates','run','--context',ctx,'--output',ctx,expect=1)
        self.call('gates','run','--context',ctx,'--output',str(self.repo/'receipt.json'),expect=1)
        self.assertFalse((self.repo/'receipt.json').exists())

    def test_port_and_cleanup_cli(self):
        (self.repo/'a').write_text('retained')
        cp=self.root/'cp'
        create_checkpoint(self.repo,cp)
        plan=str(self.root/'port-plan.json')
        dest=str(self.root/'ported')
        self.call('port','plan','--repo',str(self.repo),'--checkpoint',str(cp),'--baseline','HEAD','--destination',dest,'--branch','feat/port','--output',plan)
        self.call('port','apply','--plan',plan,'--authorize','port')
        self.assertEqual((Path(dest)/'a').read_text(),'retained')
        cleanup=str(self.root/'cleanup.json')
        self.call('cleanup','plan','--repo',str(self.repo),'--output',cleanup)
        self.assertEqual(self.call('cleanup','apply','--plan',cleanup,'--authorize','cleanup')['completed'],[])

    def test_review_record_cli(self):
        ctx=self.context(review_policy='independent')
        current=self.call('snapshot','--repo',str(self.repo))
        input_path=self.write_json('review.json',{'reviewer':'other','reviewer_context':'other-session',
             'snapshot_sha256':current['snapshot_sha256'],'outcome':'approved','report':'Reviewed independently'})
        receipt=self.call('review','record','--context',ctx,'--input',input_path)
        self.assertEqual(receipt['kind'],'review-receipt')

    def test_github_cli_boundary(self):
        checks=self.write_json('checks.json',['quality'])
        pr={'state':'OPEN','isDraft':False,'reviewDecision':'APPROVED','mergeStateStatus':'CLEAN',
             'statusCheckRollup':[{'name':'quality','status':'COMPLETED','conclusion':'SUCCESS'}]}
        with patch('repo_delivery.github.inspect_pr',return_value=pr):
            self.call('pr','inspect','--repository','owner/repo','--number','1')
            self.assertTrue(self.call('pr','readiness','--repository','owner/repo','--number','1','--required-checks',checks)['ready'])
        with patch('repo_delivery.github.inspect_issue',return_value={'number':1}):
            self.assertEqual(self.call('issue','inspect','--repository','owner/repo','--number','1')['number'],1)

    def test_bad_json_is_typed_error(self):
        path=self.root/'bad.json'
        path.write_text('not json')
        self.assertIn('error',self.call('cleanup','apply','--plan',str(path),'--authorize','cleanup',expect=1))

    def test_json_shape_validation_and_subprocess_gate_exit(self):
        path=self.write_json('array.json',[])
        self.assertIn('error',self.call('cleanup','apply','--plan',path,'--authorize','cleanup',expect=1))
        ctx=self.context(gate_commands=[[sys.executable,'-c','raise SystemExit(1)']])
        import subprocess
        proc=subprocess.run([sys.executable,str(Path(__file__).resolve().parents[1]/'scripts/repo-delivery'),
            'gates','run','--context',ctx],capture_output=True,text=True)
        self.assertEqual(proc.returncode,1)
        self.assertFalse(json.loads(proc.stdout)['passed'])
