#!/usr/bin/env python3
"""P7 synthetic offline smoke with empty PATH and isolated credential-free home."""
import json, pathlib, subprocess, sys, tempfile
root=pathlib.Path(__file__).resolve().parents[2]
if len(sys.argv)!=3: raise SystemExit('usage: verify-workkit-integration-offline.py AOK WORKKIT')
with tempfile.TemporaryDirectory(prefix='workkit-p7-offline-') as name:
 tmp=pathlib.Path(name).resolve()
 empty=tmp/'empty.json';empty.write_text('{}\n')
 for binary in (pathlib.Path(p).resolve() for p in sys.argv[1:]):
  for prefix in ([['work'],['workkit']] if binary.name.startswith('aok') else [[]]):
   env={'PATH':str(tmp),'HOME':str(tmp),'USERPROFILE':str(tmp),'TMPDIR':str(tmp)}
   def run(args,want=0):
    p=subprocess.run([str(binary),*prefix,*map(str,args),'--json'],cwd=root,env=env,capture_output=True,text=True,timeout=30)
    assert p.returncode==want,(args,p.returncode,p.stdout,p.stderr)
    d=json.loads(p.stdout)
    if prefix:d=d['spec']['data']
    assert d['spec']['effects']['provider']=='none' and d['spec']['effects']['native']=='none',d
    return d['spec']['data']
   for case,expected in [('forward',0),('reverse',0),('conflict',4),('equal',0)]:
    fixture=root/'kits/workkit/testdata/integration'/case
    assets=fixture/'assets'
    flags={'configuration':assets/'configuration.json','state':assets/'state.json','source':assets/'source','destination':assets/'destination','source-observation':assets/'source-observation','observation':assets/'destination-observation','forward-mapping':assets/'forward-mapping.json','reverse-mapping':assets/'reverse-mapping.json','forward-context':assets/'forward-context.json','reverse-context':assets/'reverse-context.json','source-binding':assets/'source-binding.json','binding':assets/'destination-binding.json','source-query':assets/'source-query.json','destination-query':assets/'destination-query.json','as-of':'2026-10-26T06:00:01Z'}
    args=[]
    for key,value in flags.items():args+=['--'+key,value]
    label=binary.name+'-'+('-'.join(prefix) or 'standalone')+'-'+case
    output=tmp/label
    result=run(['sync','preview','--v2',*args,'--out',output],expected)
    assert not any(result[k] for k in ('authorizationGranted','executionReady','migrationActivated','productionActivated'))
    assert result['status']==('conflict' if case=='conflict' else 'ready')
    run(['sync','preview','--v2',*args,'--out',output],2)
    if case=='conflict':
     resolution=tmp/(label+'-resolution.json')
     import hashlib
     digest=hashlib.sha256((output/'report.json').read_bytes()).hexdigest()
     resolution.write_text(json.dumps({'version':'workkit-sync/v2','kind':'ConflictResolution','planSha256':digest,'choices':[{'conflictId':result['conflicts'][0]['id'],'choice':'source'}]}))
     resolved=run(['sync','resolve','--plan',output,'--resolution',resolution,'--as-of','2026-10-26T06:00:01Z','--out',tmp/(label+'-resolved')])
     assert resolved['plan']['changes'][0]['direction']=='forward'
    if case=='forward':
     run(['sync','run','--v2','--once','--plan',output,'--invocation',empty,'--policy',empty,'--enrollment',empty,'--out',tmp/(label+'-no-bootstrap')],3)
   run(['native','apply'],3)
print('WorkKit P7 offline smoke passed: zero provider/native/model calls; activation gates unchanged')
