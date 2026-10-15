"""Validate proposal fixtures; optional authoring dependencies, no runtime integration."""
import copy
import json
import re
from pathlib import Path

from jsonschema import Draft202012Validator
from referencing import Registry, Resource

ROOT = Path(__file__).resolve().parent
schemas = {p.stem: json.loads(p.read_text()) for p in (ROOT / 'schemas').glob('*.json')}
registry = Registry().with_resources((s['$id'], Resource.from_contents(s)) for s in schemas.values())
for schema in schemas.values():
    Draft202012Validator.check_schema(schema)
examples = {p.stem: json.loads(p.read_text()) for p in (ROOT / 'examples').glob('*.json')
            if not p.name.endswith('.schema.json')}
for name, data in examples.items():
    validator = Draft202012Validator(schemas[name + '.schema'], registry=registry)
    validator.validate(data)
    extra = copy.deepcopy(data)
    extra['unknownField'] = True
    assert list(validator.iter_errors(extra)), name
    missing = copy.deepcopy(data)
    del missing['kind']
    assert list(validator.iter_errors(missing)), name
Draft202012Validator.check_schema(json.loads((ROOT / 'examples/decision-input.schema.json').read_text()))

def check_plan(plan):
    steps = {s['id']: s for s in plan['steps']}
    assert len(steps) == len(plan['steps']), 'duplicate step'
    visiting, visited = set(), set()
    def visit(id):
        assert id in steps, 'missing dependency'
        assert id not in visiting, 'cycle'
        if id in visited:
            return
        visiting.add(id)
        for dep in steps[id]['dependsOn']:
            visit(dep)
        visiting.remove(id)
        visited.add(id)
    for id in steps:
        visit(id)

def check_experiment(experiment):
    splits = [set(experiment['datasets'][k]) for k in ['train', 'validation', 'test']]
    assert all(splits), 'missing split'
    assert not (splits[0] & splits[1] or splits[0] & splits[2] or splits[1] & splits[2]), 'leakage'
    for mutable in experiment['mutableFields']:
        for protected in experiment['protectedFields']:
            assert not (mutable == protected or mutable.startswith(protected + '.')
                        or protected.startswith(mutable + '.')), 'protected field overlap'

check_plan(examples['plan'])
check_experiment(examples['optimization-experiment'])
assert examples['goal']['planRef'] == examples['plan']['metadata']['id']
assert examples['plan']['goalRef'] == examples['goal']['metadata']['id']
assert examples['plan']['replanning']['maxRevisions'] <= examples['goal']['limits']['maxReplans']
assert examples['decision-result']['decisionRef'] == examples['decision']['metadata']['id']
assert examples['decision-result']['status'] == 'abstained'
assert examples['decision-result']['value'] is None
assert examples['optimization-experiment']['status'] == 'not-run'
assert not examples['optimization-experiment']['results']
step_ids = {s['id'] for s in examples['plan']['steps']}
predicate_ids = {p['id'] for p in examples['goal']['successPredicates']}
for link in examples['traceability']['links']:
    assert link['stepRef'] in step_ids
    assert set(link['predicateRefs']) <= predicate_ids

negative = []
x = copy.deepcopy(examples['plan']); x['steps'][0]['dependsOn'] = ['verify']; negative.append((check_plan, x))
x = copy.deepcopy(examples['plan']); x['steps'][0]['dependsOn'] = ['missing']; negative.append((check_plan, x))
x = copy.deepcopy(examples['plan']); x['steps'].append(x['steps'][0]); negative.append((check_plan, x))
x = copy.deepcopy(examples['optimization-experiment']); x['datasets']['test'] = x['datasets']['train']; negative.append((check_experiment, x))
x = copy.deepcopy(examples['optimization-experiment']); x['mutableFields'] = ['permissions.actions']; negative.append((check_experiment, x))
x = copy.deepcopy(examples['optimization-experiment']); x['mutableFields'] = ['identity']; negative.append((check_experiment, x))
for check, invalid in negative:
    try:
        check(invalid)
    except AssertionError:
        pass
    else:
        raise AssertionError('negative fixture was accepted')
for path in ROOT.rglob('*.md'):
    for link in re.findall(r'\]\(([^)]+)\)', path.read_text()):
        if not link.startswith(('http://', 'https://', '#')):
            assert (path.parent / link.split('#')[0]).exists(), (path, link)
json.loads((ROOT / 'sources.json').read_text())
print(f'PASS: {len(schemas)} schemas, {len(examples)} examples, '
      f'{2 * len(examples) + len(negative)} negative cases, graph/split/reference checks and local links')
