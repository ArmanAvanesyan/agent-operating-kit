"""Validate documentation fixtures only; requires the optional jsonschema package."""
import copy
import json
import hashlib
import re
from pathlib import Path

from jsonschema import Draft202012Validator, FormatChecker
from referencing import Registry, Resource

ROOT = Path(__file__).resolve().parent
REPO = ROOT.parents[2]
SCHEMAS = {p.stem: json.loads(p.read_text()) for p in (ROOT / 'schemas').glob('*.json')}
registry = Registry().with_resources(
    (s['$id'], Resource.from_contents(s)) for s in SCHEMAS.values()
)
for schema in SCHEMAS.values():
    Draft202012Validator.check_schema(schema)

pairs = {
    'agent': 'agent',
    'execution-profile-opencode': 'execution-profile',
    'execution-profile-agy': 'execution-profile',
    'output-contract': 'output-contract',
    'assignment': 'assignment',
    'adapter-report': 'adapter-report',
    'resolved-manifest': 'resolved-manifest',
    **{name: name for name in [
        'invocation-contract', 'context-policy', 'capability-registry',
        'environment-profile', 'result-envelope', 'run-record', 'lifecycle-policy',
        'adapter-conformance', 'evaluation-contract', 'migration-contract',
    ]},
}
for example, schema_name in pairs.items():
    schema = SCHEMAS[f'{schema_name}.schema']
    validator = Draft202012Validator(schema, registry=registry, format_checker=FormatChecker())
    data = json.loads((ROOT / 'examples' / f'{example}.json').read_text())
    validator.validate(data)
    invalid = copy.deepcopy(data)
    invalid['unknownField'] = True
    assert list(validator.iter_errors(invalid)), example
    invalid = copy.deepcopy(data)
    del invalid['kind']
    assert list(validator.iter_errors(invalid)), example

for name in ['inventory-output', 'invocation-input']:
    Draft202012Validator.check_schema(json.loads((ROOT / f'examples/{name}.schema.json').read_text()))
packet_schema = json.loads((REPO / 'schemas/task-packet.schema.json').read_text())
Draft202012Validator(packet_schema).validate(
    json.loads((ROOT / 'examples/task-packet.json').read_text())
)
for path in ROOT.rglob('*.md'):
    for target in re.findall(r'\]\(([^)]+)\)', path.read_text()):
        if target.startswith(('https://', 'http://', '#')):
            continue
        target = target.split('#', 1)[0]
        assert (path.parent / target).exists(), (path, target)
# Fixture consistency checks do not implement a production resolver.
def fixture(name):
    return json.loads((ROOT / f'examples/{name}.json').read_text())

agent = fixture('agent')
profiles = [fixture('execution-profile-opencode'), fixture('execution-profile-agy')]
assert agent['execution']['defaultProfile'] in agent['execution']['allowedProfiles']
assert all(p['metadata']['id'] in agent['execution']['allowedProfiles'] for p in profiles)
for field, name in [
    ('invocationContract', 'invocation-contract'), ('contextPolicy', 'context-policy'),
    ('lifecyclePolicy', 'lifecycle-policy'), ('evaluationContract', 'evaluation-contract'),
    ('migrationContract', 'migration-contract'),
]:
    assert agent[field] == fixture(name)['metadata']['id']
assignment = fixture('assignment')
input_schema = json.loads((ROOT / fixture('invocation-contract')['inputSchema']).read_text())
Draft202012Validator(input_schema).validate(assignment['inputs'])
for deliverable in fixture('output-contract')['deliverables']:
    for field in ['schema', 'template']:
        if field in deliverable:
            assert (ROOT / deliverable[field]).is_file()
assert (ROOT / fixture('output-contract')['resultSchema']).is_file()
manifest = fixture('resolved-manifest')
for source in manifest['snapshot']['documents']:
    path = ROOT / source['path']
    assert hashlib.sha256(path.read_bytes()).hexdigest() == source['sha256']
    assert json.loads(path.read_text()) == source['content']
for source in manifest['snapshot']['instructionSources']:
    path = ROOT / source['path']
    assert hashlib.sha256(path.read_bytes()).hexdigest() == source['sha256']
    assert path.read_text() == source['content']
run = fixture('run-record')
assert run['configuration']['resolvedManifestSha256'] == hashlib.sha256(
    (ROOT / run['manifest']).read_bytes()
).hexdigest()
assert run['fixture'] and run['state'] == 'blocked' and run['result']['status'] == 'blocked'
assert run['configuration']['observed'] is None
assert all(v is None for v in run['usage'].values())
lifecycle = fixture('lifecycle-policy')
assert all(t['from'] in lifecycle['states'] and t['to'] in lifecycle['states']
           for t in lifecycle['transitions'])
assert fixture('adapter-conformance')['certification'] == 'draft'
assert all(result['status'] == 'not-run' for result in fixture('evaluation-contract')['results'])

# Meaningful negative examples for newly enforceable shape constraints.
negative_cases = []
result = fixture('result-envelope')
result['status'] = 'completed'  # Verification and unresolved items contradict completion.
negative_cases.append(('result-envelope', result))
active = fixture('run-record')
active['state'] = 'running'  # Active records cannot contain a terminal result.
negative_cases.append(('run-record', active))
terminal = fixture('run-record')
terminal['result'] = None  # Terminal records need a result.
negative_cases.append(('run-record', terminal))
permissive = fixture('agent')
permissive['permissions']['actions']['default'] = 'allow'
negative_cases.append(('agent', permissive))
unsafe_migration = fixture('migration-contract')
unsafe_migration['rollback']['reverseExternalEffects'] = True
negative_cases.append(('migration-contract', unsafe_migration))
for name, data in negative_cases:
    validator = Draft202012Validator(SCHEMAS[f'{name}.schema'], registry=registry)
    assert list(validator.iter_errors(data)), name
print(f'PASS: {len(SCHEMAS)} draft schemas, {len(pairs)} examples, '
      f'{2 * len(pairs) + len(negative_cases)} negative cases, '
      'input/task schemas, snapshot hashes, fixture consistency and local links')
