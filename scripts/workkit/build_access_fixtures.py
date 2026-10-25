#!/usr/bin/env python3
"""Deterministic synthetic WorkKit P3 references; no credentials or signing keys."""
import argparse,hashlib,json,pathlib
root=pathlib.Path(__file__).resolve().parents[2]
p=argparse.ArgumentParser();p.add_argument('--check',action='store_true');args=p.parse_args()
files={}
for provider in ('github','azure-devops','linear','jira'):
 old=json.loads((root/f'kits/workkit/testdata/read/connections/{provider}.json').read_bytes())
 profile=old['profile'];spec=profile['spec'];mode={'github':'token','azure-devops':'pat','linear':'api-key','jira':'basic'}[provider]
 refs={'token':'WK_P3_DISPOSABLE_TOKEN'}
 if mode=='basic':refs['username']='WK_P3_DISPOSABLE_USERNAME'
 c={'version':'workkit-access/v1','kind':'AuthConfiguration','connectionId':old['connectionId'],'profile':profile,'provider':spec['provider'],'providerLookup':old['source']['containerKey'].split('/')[0] if provider=='azure-devops' else '','origin':spec['origin'],'basePath':spec.get('basePath',''),'nativePrincipal':'synthetic-native-principal','tenantId':spec['scope']['tenantId'],'containers':spec['scope']['containerIds'],'mode':mode,'secretReferences':refs}
 files[f'{provider}.json']=c
for name,value in files.items():
 target=root/'kits/workkit/testdata/access'/name
 body=(json.dumps(value,sort_keys=True,indent=2)+'\n').encode()
 if args.check:
  if not target.exists() or target.read_bytes()!=body:raise SystemExit(f'P3 fixture drift: {name}')
 else:target.parent.mkdir(parents=True,exist_ok=True);target.write_bytes(body)
print('WorkKit P3 reference fixture generation/check passed')
