#!/usr/bin/env python3
"""P5 compiled command smoke with empty PATH, no credentials and synthetic inputs."""
import json,pathlib,subprocess,sys,tempfile
if len(sys.argv) not in (3,4):raise SystemExit('usage: verify-workkit-operations-offline.py AOK WORKKIT [SYNTHETIC-SYNC-BUNDLE]')
root=pathlib.Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix='workkit-p5-offline-') as name:
 tmp=pathlib.Path(name).resolve();env={'PATH':str(tmp),'HOME':str(tmp),'USERPROFILE':str(tmp),'TMPDIR':str(tmp)}
 for binary in map(lambda p:pathlib.Path(p).resolve(),sys.argv[1:3]):
  for prefix in ([['work'],['workkit']] if binary.name.startswith('aok') else [[]]):
   def run(args,want=0,envelope=True):
    p=subprocess.run([str(binary),*prefix,*map(str,args)],cwd=root,env=env,capture_output=True,text=True,timeout=30)
    assert p.returncode==want,(args,p.returncode,p.stdout,p.stderr)
    if not envelope:return p.stdout
    d=json.loads(p.stdout)
    if prefix:d=d['spec']['data']
    assert d['spec']['effects']['provider']=='none' and d['spec']['effects']['native']=='none'
    return d
   for group in ('operations','sync'):assert run([group,'--help'],envelope=False)
   run(['sync','run','--plan','missing','--json'],2,False)
   run(['operations','apply','--plan','missing','--as-of','2026-10-26T00:00:00Z','--json'],2,False)
   if len(sys.argv)==4:
    fixture=pathlib.Path(sys.argv[3]).resolve();assets=fixture/'assets'
    result=run(['sync','conflicts','--plan',fixture,'--json']);assert result['spec']['data']==[]
    output=tmp/(binary.name+'-'+('-'.join(prefix) or 'standalone')+'-preview')
    args=['sync','preview','--configuration',assets/'configuration.json','--state',assets/'state.json','--source',assets/'source','--destination',assets/'destination','--source-observation',assets/'source-observation','--observation',assets/'destination-observation','--mapping',assets/'mapping.json','--mapping-context',assets/'mapping-context.json','--source-binding',assets/'source-binding.json','--binding',assets/'destination-binding.json','--query',assets/'query.json','--as-of',json.loads((fixture/'report.json').read_text())['asOf'],'--out',output,'--json']
    result=run(args,3);plan=result['spec']['data'];assert plan['status']=='baseline-candidate' and not plan['authorizationGranted'] and not plan['productionActivated'];run(args,2)
print('WorkKit P5 empty-PATH/credential-free smoke passed; no provider calls or activation')
