"""Exercise only synthetic P2 fixtures, with empty PATH and no credentials."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
ROOT=Path(__file__).resolve().parents[2]
aok=Path(sys.argv[1]).resolve();workkit=Path(sys.argv[2]).resolve()
with tempfile.TemporaryDirectory(prefix='workkit-read-offline-') as temp:
 root=Path(temp).resolve();empty=root/'empty';empty.mkdir();env={'PATH':str(empty)}
 def run(binary,args,code=0):
  p=subprocess.run([str(binary),*args],env=env,cwd=root,capture_output=True,timeout=60)
  if p.returncode!=code:raise AssertionError((args,p.returncode,p.stdout.decode(),p.stderr.decode()))
  return json.loads(p.stdout)
 providers=run(workkit,['providers','list','--json']);suite=run(aok,['work','providers','list','--json']);assert suite['spec']['data']==providers
 assert len(providers['spec']['data'])==5
 bundles=[]
 for provider in ['github','azure-devops','linear','jira']:
  destination=root/provider
  result=run(workkit,['capture','import','--connection-file',str(ROOT/'kits/workkit/testdata/read/connections'/f'{provider}.json'),'--manifest',str(ROOT/'internal/tracker/testdata/offline'/provider/'manifest.json'),'--as-of','2026-10-24T12:00:30Z','--out',str(destination),'--json'])
  manifest=result['spec']['data']['manifest'];assert manifest['liveQualification']=='not-established' and not manifest['migrationActivated'] and not manifest['productionActivated']
  run(aok,['work','capture','validate','--capture',str(destination),'--json'])
  run(workkit,['items','list','--capture',str(destination),'--as-of','2026-10-24T12:00:30Z','--json'])
  run(workkit,['catalog','iterations','--capture',str(destination),'--as-of','2026-10-24T12:00:30Z','--json'],3)
  bundles.append(str(destination))
 args=['browse','--captures',','.join(bundles),'--query',str(ROOT/'kits/workkit/testdata/read/query.json'),'--as-of','2026-10-24T12:00:30Z','--json']
 domain=run(workkit,args);suite=run(aok,['work',*args]);assert suite['spec']['data']==domain
 data=domain['spec']['data'];assert data['returned']==4 and data['exhaustive'] and len(data['coverage'])==4
 assert data['authorizationGranted'] is False and data['executionReady'] is False
 print('WorkKit P2 empty-PATH/no-credential smoke passed: four captures, catalogs, federation, suite parity; no activation.')
