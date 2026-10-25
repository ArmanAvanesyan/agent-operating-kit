"""Generate synthetic read configurations from existing immutable AOK fixtures."""
from pathlib import Path
import json,argparse,tempfile
ROOT=Path(__file__).resolve().parents[2]
p=argparse.ArgumentParser();p.add_argument('--check',action='store_true');args=p.parse_args()
files={}
for provider in ['github','azure-devops','linear','jira']:
 source=json.loads((ROOT/'internal/tracker/testdata/offline'/provider/'source.json').read_text())
 spec={'provider':'jira-cloud' if provider=='jira' else provider,'origin':source['origin'],'basePath':'','transport':'gh' if provider=='github' else 'graphql' if provider=='linear' else 'rest','authRef':'credentials/'+provider,'scope':{'tenantId':source['nativeTenantId'],'containerIds':[source['containerId']]},'apiVersion':source['apiVersion']}
 profile={'apiVersion':'workkit/v1alpha1','kind':'ConnectionProfile','metadata':{'id':provider,'revision':1},'spec':spec}
 files['connections/'+provider+'.json']={'version':'workkit-read/v1','connectionId':provider,'profile':profile,'source':source}
files['query.json']={'version':'workkit-read/v1','dialect':'portable','connectionId':'','expression':'title != "not-a-fixture-title"','fields':{'title':{'type':'string','providerFields':{'github':'title','azure-devops':'System.Title','linear':'title','jira':'summary'}}},'limit':100,'maxAgeSeconds':60}
for name,value in files.items():
 content=(json.dumps(value,indent=2)+'\n').encode();target=ROOT/'kits/workkit/testdata/read'/name
 if args.check:
  if not target.exists() or target.read_bytes()!=content:raise SystemExit('read fixture drift: '+name)
 else:target.write_bytes(content)
print('WorkKit read fixture generation/check passed')
