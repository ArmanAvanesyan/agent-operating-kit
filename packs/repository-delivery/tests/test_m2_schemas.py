import json
from pathlib import Path
import sys
import jsonschema
from test_core import RepositoryFixture
from repo_delivery.core import create_checkpoint
from repo_delivery.evidence import snapshot,new_context,run_gates,record_review,delivery_status
from repo_delivery.workspace import isolate,plan_port,apply_port,plan_cleanup,apply_cleanup

class Milestone2SchemaTests(RepositoryFixture):
    def validate(self, record):
        name=record['kind']
        path=Path(__file__).resolve().parents[1]/'config/schemas'/(name+'.json')
        schema=json.loads(path.read_text())
        jsonschema.Draft202012Validator.check_schema(schema)
        jsonschema.validate(record,schema)
        with self.assertRaises(jsonschema.ValidationError):
            jsonschema.validate(dict(record,schema_version=999),schema)

    def test_actual_records_validate(self):
        self.validate(snapshot(self.repo))
        spec={'objective':'schema fixture','files_in_scope':['a'],'gate_commands':[[sys.executable,'-c','pass']],
              'review_policy':'independent','authorized_actions':[], 'actor_id':'coder','actor_context':'coding'}
        context=new_context(self.repo,spec,'HEAD')
        self.validate(context)
        gates=run_gates(context)
        self.validate(gates)
        review=record_review(context,{'reviewer':'reviewer','reviewer_context':'review-session',
                'snapshot_sha256':snapshot(self.repo)['snapshot_sha256'],'outcome':'approved','report':'Reviewed'})
        self.validate(review)
        self.validate(delivery_status(context,gates,review))
        self.validate(isolate(self.repo,'HEAD',self.root/'isolate','feat/schema',['isolate']))
        cp=self.root/'checkpoint'
        (self.repo/'a').write_text('port')
        create_checkpoint(self.repo,cp)
        port=plan_port(cp,self.repo,'HEAD',self.root/'port','feat/port')
        self.validate(port)
        self.validate(apply_port(port,['port']))
        cleanup=plan_cleanup(self.repo)
        self.validate(cleanup)
        self.validate(apply_cleanup(cleanup,['cleanup']))
