import json
from pathlib import Path
import sys
import jsonschema
from test_core import RepositoryFixture
from repo_delivery import adapters,evidence,hooks,integration,planning
from repo_delivery.core import digest

class Milestone3SchemaTests(RepositoryFixture):
    def validate(self,value):
        path=Path(__file__).resolve().parents[1]/'config/schemas'/(value['kind']+'.json')
        schema=json.loads(path.read_text())
        jsonschema.Draft202012Validator.check_schema(schema)
        jsonschema.validate(value,schema)
        with self.assertRaises(jsonschema.ValidationError):
            jsonschema.validate(dict(value,schema_version=999),schema)

    def test_generated_proof_and_hook_records(self):
        self.repo=self.repo.resolve();self.root=self.root.resolve()
        ctx=evidence.new_context(self.repo,{'objective':'schema','files_in_scope':['a'],
            'gate_commands':[[sys.executable,'-c','pass']],'review_policy':'none',
            'authorized_actions':['push'],'actor_id':'coder','actor_context':'coding'},'HEAD')
        proof=integration.run_integration(ctx,{'required_mode':'mock','services':[{'name':'fixture','mode':'mock','version':'v1'}]})
        self.validate(proof);self.validate(integration.assert_integration(ctx,proof))
        config={'schema_version':1,'kind':'git-pre-push-config','repository':str(self.repo),
            'context':str(self.root/'context'),'gates':str(self.root/'gates'),'review':None,'fail_mode':'closed'}
        for name,value in [('context',ctx),('gates',evidence.run_gates(ctx)),('config',config)]:
            p=self.root/name;p.write_text(json.dumps(value));p.chmod(0o600)
        self.validate(config);self.validate(hooks.check_hook(self.root/'config'))
        self.validate(hooks.install_hook(self.repo,self.root/'config',['install-hook']))

    def test_actual_contract_reference_and_planning_records(self):
        schema=Path(__file__).parent/'fixtures/aok/task-packet.schema.json'
        packet={'title':'Work','spec':'Scope','status':'blocked','phase':'build','acceptanceCriteria':['observable'],'verification':{'commands':['make quality']}}
        path=self.root/'packet';path.write_text(json.dumps(packet))
        self.validate(adapters.consume_aok(path,schema,digest(schema.read_bytes())))
        ref={'attempt_id':'attempt','task_id':'task','worker':'forge','revision':'a'*40,'tree':'b'*40,'environment':'test'}
        row=dict(id='attempt',task_id='task',worker='forge',state='recorded',environment='test',record_json=json.dumps({'Revision':'a'*40,'Tree':'b'*40,'Environment':'test'}))
        export=self.root/'runtime.ndjson';export.write_text(json.dumps(row))
        self.validate(adapters.consume_swarmd_dump(export,'attempt'))
        self.validate({'schema_version':1,'kind':'attempt-reference-check','reference':ref,'live_lease_verified':False,'managed_finish_supported':False})
        sources=[]
        for role in planning.ROLES:
            (self.repo/role).write_text(role)
            sources.append({'role':role,'path':role,'sha256':digest(role.encode())})
        plan=planning.plan_backlog(self.repo,{'github_repository':'owner/repo','baseline':'HEAD','sources':sources,
            'items':[{'key':'gap','title':'Gap','summary':'Implement observed gap','acceptance_criteria':[{'action':'Run test','expected':'Exit zero'}],
                      'evidence':['implementation'],'depends_on':[]}]})
        self.validate(plan)
        self.validate({'schema_version':1,'kind':'backlog-result','plan_sha256':plan['plan_sha256'],'github_repository':'owner/repo','issues':{'gap':1}})
