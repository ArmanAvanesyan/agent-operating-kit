#!/usr/bin/env python3
"""P4 fixture-only, empty-PATH smoke; no authentication, native stores or sends."""
import json, pathlib, subprocess, sys, tempfile
root=pathlib.Path(__file__).resolve().parents[2]
if len(sys.argv)!=3: raise SystemExit('usage: verify-workkit-setup-offline.py AOK WORKKIT')
with tempfile.TemporaryDirectory(prefix='workkit-p4-smoke-') as name:
 tmp=pathlib.Path(name).resolve()
 for binary in map(lambda p:pathlib.Path(p).resolve(),sys.argv[1:]):
  prefixes=[['work'],['workkit']] if binary.name.startswith('aok') else [[]]
  for prefix in prefixes:
   env={'PATH':str(tmp),'HOME':str(tmp),'USERPROFILE':str(tmp),'TMPDIR':str(tmp)}
   def run(args,code=0):
    p=subprocess.run([str(binary),*prefix,*map(str,args),'--json'],cwd=root,env=env,capture_output=True,text=True,timeout=20)
    assert p.returncode==code,(args,p.returncode,p.stdout,p.stderr)
    d=json.loads(p.stdout)
    if prefix: d=d['spec']['data']
    assert d['spec']['effects']['provider']=='none' and d['spec']['effects']['native']=='none'
    return d
   for provider in ('github','azure-devops','jira-cloud','jira-datacenter','linear'):
    fixture=root/'kits/workkit/testdata/setup'/provider
    stamp=binary.name+'-'+str(len(prefix))+'-'+('-'.join(prefix) or 'standalone')+'-'+provider
    plan=tmp/(stamp+'-plan')
    d=run(['setup','plan','--observation',fixture,'--desired',fixture/'desired.json','--as-of','2026-10-26T00:00:01Z','--out',plan])
    assert not d['spec']['data']['authorizationGranted'] and not d['spec']['data']['productionActivated']
    run(['setup','validate-plan','--plan',plan,'--as-of','2026-10-26T00:00:01Z'])
    run(['mappings','validate','--mapping',fixture/'mapping.json','--mapping-context',fixture/'mapping-context.json','--source-observation',fixture,'--observation',fixture,'--as-of','2026-10-26T00:00:01Z'])
    receipt=run(['setup','verify','--observation',fixture,'--desired',fixture/'desired.json','--mapping',fixture/'mapping.json','--mapping-context',fixture/'mapping-context.json','--as-of','2026-10-26T00:00:01Z','--out',tmp/(stamp+'-candidate')],3)
    assert not receipt['spec']['data']['selected'] and receipt['spec']['data']['binding']['spec']['status']=='draft'
print('WorkKit P4 fixture/empty-PATH smoke passed; migration blocked, live pending, production unauthorized')
