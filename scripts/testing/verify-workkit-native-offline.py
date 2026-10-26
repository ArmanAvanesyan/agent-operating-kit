#!/usr/bin/env python3
"""P6 synthetic offline smoke: empty PATH, isolated HOME, no host transports."""
import json, pathlib, subprocess, sys, tempfile
root=pathlib.Path(__file__).resolve().parents[2]
if len(sys.argv) not in (3,4): raise SystemExit('usage: verify-workkit-native-offline.py AOK WORKKIT')
with tempfile.TemporaryDirectory(prefix='workkit-p6-offline-') as name:
 tmp=pathlib.Path(name).resolve()
 for binary in map(lambda p:pathlib.Path(p).resolve(),sys.argv[1:3]):
  for prefix in ([['work'],['workkit']] if binary.name.startswith('aok') else [[]]):
   env={'PATH':str(tmp),'HOME':str(tmp),'USERPROFILE':str(tmp),'TMPDIR':str(tmp)}
   def run(args,want=0):
    p=subprocess.run([str(binary),*prefix,*map(str,args),'--json'],cwd=root,env=env,capture_output=True,text=True,timeout=25)
    assert p.returncode==want,(args,p.returncode,p.stdout,p.stderr)
    d=json.loads(p.stdout)
    if prefix:d=d['spec']['data']
    assert d['spec']['effects']['provider']=='none' and d['spec']['effects']['native']=='none'
    return d['spec']['data']
   if len(sys.argv)==4:
    flags=json.loads((pathlib.Path(sys.argv[3])/'arguments.json').read_text())
    args=['native','plan']
    for key,value in flags.items():args.extend(['--'+key,value])
    progress=tmp/(binary.name+'-'+('-'.join(prefix) or 'standalone')+'-blocked-progress')
    result=run([*args,'--out',progress],3)
    assert result['status']=='blocked' and not result['authorizationGranted']
   assert len(run(['native','hosts']))==5
   run(['native','apply'],3)
   for host in ('codex','claude-code','opencode','cursor','antigravity'):
    fixture=root/'kits/workkit/testdata/native'/host
    common=['--assignment',fixture/'assignment.json','--profile',fixture/'profile.json','--mapping',fixture/'mapping.json','--handle',fixture/'handle.json']
    time=['--as-of','2026-10-26T12:00:02Z']
    label=binary.name+'-'+('-'.join(prefix) or 'standalone')+'-'+host
    output=tmp/(label+'-projection')
    result=run(['native','plan',*common,*time,'--out',output]);assert not any(result[k] for k in ('authorizationGranted','executionReady','migrationActivated','productionActivated'))
    assert result['implementationSha256'] and result['adapterVersion'] and not result['entries']
    run(['native','inspect','--bundle',output,*time])
    run(['native','plan',*common,*time,'--out',output],2)
    run(['native','validate-binding',*common,*time])
    if host in ('codex','claude-code','opencode'):
     obs=tmp/(label+'-observation')
     result=run(['native','observe',*common,'--evidence',fixture/'evidence.json',*time,'--out',obs])
     assert result['complete'] and result['currentAuthority']=='not-established' and result['liveQualification']=='not-established'
     assert all(not e.get('nativeId') for e in result['entries']) if host!='claude-code' else result['entries'][0]['nativeId']=='synthetic-task'
     run(['native','inspect','--bundle',obs,'--as-of','2026-10-26T12:05:00Z'],4)
     run(['native','observe',*common,'--policy','missing','--enrollment','missing','--out',tmp/(label+'-unenrolled')],3)
print('WorkKit P6 offline smoke passed; no host/model/provider calls; activation gates unchanged')
