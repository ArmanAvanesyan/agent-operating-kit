#!/usr/bin/env python3
"""P3 credential-free, empty-PATH smoke. Never starts tools or authentication."""
import json, os, pathlib, subprocess, sys, tempfile
binaries=[pathlib.Path(p).resolve() for p in sys.argv[1:]]
if len(binaries)!=2: raise SystemExit('usage: verify-workkit-access-offline.py AOK WORKKIT')
with tempfile.TemporaryDirectory(prefix='workkit-p3-offline-') as tmp:
 for binary in binaries:
  prefix=['work'] if binary.name.startswith('aok') else []
  env={'PATH':tmp,'HOME':tmp,'USERPROFILE':tmp,'TMPDIR':tmp}
  result=subprocess.run([str(binary),*prefix,'auth','status','--json'],env=env,capture_output=True,text=True,timeout=10)
  assert result.returncode==0,result.stderr
  data=json.loads(result.stdout)
  if prefix: data=data['spec']['data']
  assert data['spec']['effects']=={'local':'none','provider':'none','native':'none'}
  routes=data['spec']['data'];assert routes and all(r['liveQualification']=='pending' for r in routes)
  assert any(r['mode']=='gh-native' and r['implementation']=='implemented-driver' for r in routes)
  for group in ('tools','auth','connections'):
   r=subprocess.run([str(binary),*prefix,group,'--help'],env=env,capture_output=True,text=True,timeout=10)
   assert r.returncode==0,(group,r.stderr)
  r=subprocess.run([str(binary),*prefix,'auth','login','fixture','--non-interactive'],env=env,capture_output=True,text=True,timeout=10)
  assert r.returncode==2,'unsupported login inputs must not prompt or use ambient credentials'
print('WorkKit P3 empty-PATH/no-credential smoke passed')
