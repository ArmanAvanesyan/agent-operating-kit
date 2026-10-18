"""Declared real/mock/mixed integration evidence with observable service versions."""
import json
import re
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, Request, build_opener

from .core import DeliveryError, canonical, digest
from .evidence import (environment, in_scope, require, run_gates, seal, snapshot,
                       validate_context, validate_seal)


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def validate_spec(spec):
    require(isinstance(spec, dict) and not set(spec) - {'required_mode', 'services', 'timeout_seconds'}, 'Invalid integration specification')
    require(spec.get('required_mode') in ('real', 'mock', 'mixed'), 'Explicit required integration mode required')
    timeout = spec.get('timeout_seconds', 5)
    require(type(timeout) in (int, float) and 0 < timeout <= 60, 'Invalid service probe timeout')
    services = spec.get('services')
    require(isinstance(services, list) and 0 < len(services) <= 32, 'Explicit service list required')
    names = set()
    for service in services:
        require(isinstance(service, dict), 'Invalid service specification')
        name = service.get('name')
        require(isinstance(name, str) and re.fullmatch(r'[A-Za-z0-9_.-]{1,100}', name) and name not in names, 'Invalid or duplicate service name')
        names.add(name)
        if service.get('mode') == 'real':
            require(set(service) == {'name', 'mode', 'endpoint', 'version_field', 'expected_version'}, 'Invalid real service fields')
            require(isinstance(service['endpoint'], str), 'Invalid service endpoint')
            endpoint = urlsplit(service['endpoint'])
            require(endpoint.scheme in ('http', 'https') and bool(endpoint.hostname) and not endpoint.username and not endpoint.password and not endpoint.query and not endpoint.fragment, 'Service endpoints must be HTTP(S) without credentials, query or fragment')
            require(isinstance(service['version_field'], str) and bool(service['version_field']), 'Version field required')
            version = service['expected_version']
        else:
            require(service.get('mode') == 'mock' and set(service) == {'name', 'mode', 'version'}, 'Invalid mock service fields')
            version = service['version']
        require(isinstance(version, str) and 0 < len(version.strip()) <= 200, 'Explicit service version required')
    return {'required_mode': spec['required_mode'], 'services': services, 'timeout_seconds': timeout}


def probe_services(spec):
    observations, blockers = [], []
    for service in spec['services']:
        result = {'name': service['name'], 'mode': service['mode'],
                  'endpoint': service.get('endpoint'), 'observed_version': None,
                  'response_sha256': None, 'available': False}
        if service['mode'] == 'mock':
            result.update(observed_version=service['version'], available=True)
        else:
            try:
                request = Request(service['endpoint'], headers={'Accept': 'application/json'})
                with build_opener(NoRedirect()).open(request, timeout=spec['timeout_seconds']) as response:
                    require(response.status == 200, 'Service probe requires HTTP 200')
                    data = response.read(65537)
                    require(len(data) <= 65536, 'Service version response too large')
                    value = json.loads(data)[service['version_field']]
                    require(isinstance(value, str) and 0 < len(value.strip()) <= 200, 'Invalid observed service version')
                    result.update(observed_version=value, response_sha256=digest(data), available=True)
                    if value != service['expected_version']:
                        blockers.append(service['name'] + ': service version mismatch')
            except (OSError, ValueError, KeyError, TypeError, DeliveryError, HTTPError, URLError):
                blockers.append(service['name'] + ': service unavailable or invalid version response')
        observations.append(result)
    return observations, blockers


def _mode(services):
    modes = {service['mode'] for service in services}
    return 'mixed' if len(modes) > 1 else modes.pop()


def run_integration(context, specification):
    validate_context(context)
    in_scope(context)
    spec = validate_spec(specification)
    before = snapshot(context['repository'], context['include_ignored'])
    env = environment(context)
    mode = _mode(spec['services'])
    services, blockers = probe_services(spec)
    if context.get('integration_required_mode') is not None and spec['required_mode'] != context['integration_required_mode']:
        blockers.append('Integration mode does not match the task context requirement')
    if mode != spec['required_mode']:
        blockers.append('Declared ' + mode + ' evidence cannot satisfy required ' + spec['required_mode'] + ' coverage')
    gates = None
    if not blockers:
        gates = run_gates(context)
        if not gates['passed']:
            blockers.append('Repository integration gates did not pass unchanged')
        # Verify that service versions remained available across the test execution.
        _, after_blockers = probe_services(spec)
        blockers.extend(after_blockers)
    after = snapshot(context['repository'], context['include_ignored'])
    unchanged = before['snapshot_sha256'] == after['snapshot_sha256']
    if not unchanged or env != environment(context):
        blockers.append('Source or selected environment changed during integration proof')
    return seal({'schema_version': 1, 'kind': 'integration-proof',
                 'context_sha256': context['context_sha256'], 'snapshot_sha256': before['snapshot_sha256'],
                 'environment': env, 'specification': spec, 'mode': mode,
                 'services': services, 'gates_executed': gates is not None, 'gates': gates,
                 'source_unchanged': unchanged, 'passed': not blockers, 'blockers': blockers}, 'receipt_sha256')


def assert_integration(context, proof):
    validate_context(context)
    in_scope(context)
    validate_seal(proof, 'receipt_sha256')
    require(proof.get('kind') == 'integration-proof' and proof.get('schema_version') == 1, 'Invalid integration proof')
    require(proof.get('context_sha256') == context['context_sha256'], 'Integration context mismatch')
    require(proof.get('snapshot_sha256') == snapshot(context['repository'], context['include_ignored'])['snapshot_sha256'], 'Integration proof is stale')
    require(proof.get('environment') == environment(context), 'Integration environment is stale')
    require(proof.get('passed') is True and proof.get('source_unchanged') is True and proof.get('blockers') == [], 'Integration proof did not pass')
    spec = validate_spec(proof.get('specification'))
    mode = _mode(spec['services'])
    require(proof.get('mode') == mode == spec['required_mode'], 'Integration evidence mode mismatch')
    services, blockers = probe_services(spec)
    require(not blockers, 'Integration services changed or unavailable')
    require(isinstance(proof.get('services'), list) and len(proof['services']) == len(services), 'Integration service evidence missing')
    for current, recorded in zip(services, proof['services']):
        require(all(recorded.get(k) == current[k] for k in ('name', 'mode', 'endpoint', 'observed_version', 'available')), 'Integration service evidence changed')
    gates = proof.get('gates')
    require(proof.get('gates_executed') is True and isinstance(gates, dict), 'Integration gates required')
    validate_seal(gates, 'receipt_sha256')
    require(gates.get('kind') == 'gate-receipt' and gates.get('context_sha256') == context['context_sha256'], 'Integration gate context mismatch')
    require(gates.get('snapshot_sha256') == proof['snapshot_sha256'] and gates.get('environment') == proof['environment'], 'Integration gate evidence is stale')
    require(gates.get('passed') is True and gates.get('source_unchanged') is True, 'Integration gates did not pass')
    results = gates.get('results')
    require(isinstance(results, list) and [r.get('argv') for r in results] == context['gate_commands'], 'Integration gate profile mismatch')
    require(all(r.get('returncode') == 0 and r.get('timed_out') is False for r in results), 'Integration gate failed')
    return {'schema_version': 1, 'kind': 'integration-status', 'ready': True,
            'context_sha256': context['context_sha256'], 'snapshot_sha256': proof['snapshot_sha256'],
            'receipt_sha256': proof['receipt_sha256'], 'mode': mode}
