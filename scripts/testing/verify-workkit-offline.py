"""Smoke built WorkKit binaries with no inherited credentials or executable discovery."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

ROOT=Path(__file__).resolve().parents[2]
aok=Path(sys.argv[1]).resolve();workkit=Path(sys.argv[2]).resolve()
with tempfile.TemporaryDirectory(prefix='workkit-offline-') as directory:
 root=Path(directory).resolve();empty=root/'empty';empty.mkdir()
 env={'PATH':str(empty)}
 def run(binary,args,want=0):
  p=subprocess.run([str(binary),*args],env=env,cwd=root,capture_output=True,timeout=30)
  if p.returncode!=want:raise AssertionError((args,p.returncode,p.stdout.decode(),p.stderr.decode()))
  return json.loads(p.stdout) if '--json' in args else p.stdout.decode()
 for name in ['offline','one-provider','multi-provider','native-only']:
  args=['workspace','validate','--workspace',str(ROOT/'kits/workkit/testdata/corpora'/name/'workspace.json'),'--json']
  domain=run(workkit,args);suite=run(aok,['work',*args])
  assert suite['spec']['data']==domain
  assert domain['spec']['data']['authorizationGranted'] is False
  assert domain['spec']['data']['executionReady'] is False
 output=root/'import'
 args=['migrate','import-aok','--source',str(ROOT/'internal/tracker/testdata/offline/github'),'--format','aok-tracker/v1','--as-of','2026-10-25T12:00:00Z','--out',str(output),'--json']
 result=run(workkit,args,6);assert result['spec']['data']['activate'] is False
 run(workkit,['migrate','verify',str(output),'--json'],6)
 run(workkit,args,1)
 assert 'offline' in run(workkit,['version'])
 print('WorkKit empty-PATH/credential-free offline smoke passed; domain/suite parity and immutable inactive import verified.')
