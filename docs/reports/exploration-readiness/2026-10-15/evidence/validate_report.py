from pathlib import Path
import hashlib,json,re,subprocess
r=Path('docs/reports/exploration-readiness/2026-10-15')
for p in r.glob('*.json'):
 if p.name!='integrity.json':json.loads(p.read_text())
for p in list(r.glob('*.md'))+[Path('docs/project-exploration-and-readiness.md')]:
 for _,link in re.findall(r'\[([^\]]+)\]\(([^)]+)\)',p.read_text()):
  if link.startswith(('http://','https://')):continue
  dest,_,anchor=link.partition('#');target=p.parent/dest if dest else p
  assert target.exists(),(p,link)
  if anchor:
   heads=re.findall(r'^#+ (.+)$',target.read_text(),re.M)
   assert anchor in {re.sub(r'[^\w\- ]','',h.lower()).replace(' ','-') for h in heads}
records=json.loads((r/'commands.json').read_text());ids=[x['id'] for x in records]
assert len(ids)==len(set(ids))
for record in records:
 for key in ['stdout','stderr']:assert (r/record[key]).is_file()
assert [x['id'] for x in records if not x['passed']]==['python-tests']
assert json.loads((r/'readiness-assessment.json').read_text())['verdict']=='ready'
for check in json.loads((r/'readiness-input.json').read_text())['checks']:
 for link in check['evidence']:assert (r/link).exists(),link
for path,digest in json.loads((r/'source-state.json').read_text())['sourceFiles'].items():
 assert hashlib.sha256(Path(path).read_bytes()).hexdigest()==digest,path
for p in r.rglob('*'):
 if p.is_file() and p.suffix!='.patch':
  res=subprocess.run(['git','diff','--no-index','--check','/dev/null',str(p)],capture_output=True,text=True)
  assert not res.stdout and not res.stderr,(p,res.stdout,res.stderr)
print('PASS: JSON, report links, source fingerprints, evidence paths, final readiness and retained historical failure')
