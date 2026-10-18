"""Optional consumers: AOK owns contracts; swarmd owns live claims and fences.

No task state is persisted here. Managed finalization requires live capability
discovery and an explicitly supplied fenced lease credential.
"""
import json
import math
import os
import select
from pathlib import Path
import re
import selectors
import subprocess
import time

from .core import DeliveryError, digest
from .evidence import require

MAX_MESSAGE = 4 * 1024 * 1024


def _document(path):
    try:
        with Path(path).open('rb') as stream:
            data = stream.read(MAX_MESSAGE + 1)
        require(len(data) <= MAX_MESSAGE, 'Contract exceeds size bound')
        def unique(pairs):
            result = {}
            for key, value in pairs:
                require(key not in result, 'Duplicate JSON field')
                result[key] = value
            return result
        value = json.loads(data, object_pairs_hook=unique,
                           parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))
        require(isinstance(value, dict), 'Contract must be an object')
        return value, data
    except (OSError, ValueError, UnicodeError) as exc:
        raise DeliveryError('Cannot read JSON contract') from exc


def _validated(document_path, schema_path, expected):
    try:
        from jsonschema import Draft202012Validator
        from importlib.metadata import version
        parts = version("jsonschema").split(".")
        require(int(parts[0]) == 4 and int(parts[1]) >= 26, "Contract consumption requires jsonschema>=4.26,<5 in the selected interpreter")
    except ImportError as exc:
        raise DeliveryError("Contract consumption requires jsonschema>=4.26,<5. Install it into the interpreter selected by AOK_DELIVERY_PYTHON (for example: /path/to/venv/bin/python -m pip install 'jsonschema>=4.26,<5'); no dependency is installed automatically.") from exc
    schema, raw = _document(schema_path)
    require(isinstance(expected, str) and re.fullmatch('[a-f0-9]{64}', expected), 'Pinned schema SHA256 required')
    require(digest(raw) == expected, 'Upstream schema digest mismatch')
    # Offline only: a supplied schema must not trigger remote schema retrieval.
    def references(value):
        if isinstance(value, dict):
            for key, item in value.items():
                if key in ('$ref', '$dynamicRef'):
                    require(isinstance(item, str) and item.startswith('#'), 'External schema references unsupported')
                references(item)
        elif isinstance(value, list):
            for item in value:
                references(item)
    references(schema)
    try:
        Draft202012Validator.check_schema(schema)
        document, data = _document(document_path)
        require(schema.get('$schema', 'https://json-schema.org/draft/2020-12/schema') == 'https://json-schema.org/draft/2020-12/schema', 'Unsupported schema dialect')
        errors = list(Draft202012Validator(schema, format_checker=Draft202012Validator.FORMAT_CHECKER).iter_errors(document))
        require(not errors, 'Upstream contract validation failed')
    except DeliveryError:
        raise
    except Exception as exc:
        raise DeliveryError('Unsupported upstream schema') from exc
    return document, {'schema_id': schema.get('$id'), 'schema_sha256': expected,
                      'document_sha256': digest(data)}


def consume_aok(packet_path, schema_path, schema_sha256, *, policy_path=None,
                policy_schema_path=None, policy_schema_sha256=None):
    """Validate upstream-owned documents and propose a mapping, without executing it.

    AOK's task packet has no version field; schema identity plus its pinned digest
    is the supported version boundary. Policy uses its own caller-supplied schema.
    """
    packet, source = _validated(packet_path, schema_path, schema_sha256)
    require(all(key in packet for key in ('title', 'spec', 'status', 'acceptanceCriteria', 'verification')), 'Not an AOK task packet')
    require(isinstance(packet['verification'], dict) and isinstance(packet['verification'].get('commands'), list), 'Missing upstream verification commands')
    require((policy_path is None) == (policy_schema_path is None) == (policy_schema_sha256 is None), 'Policy requires document, schema and pinned digest')
    policy = policy_source = None
    if policy_path is not None:
        policy, policy_source = _validated(policy_path, policy_schema_path, policy_schema_sha256)
    return {'schema_version': 1, 'kind': 'aok-consumption', 'source': source,
            'packet': packet, 'policy': policy, 'policy_source': policy_source,
            'proposal': {'objective': packet['title'] + '\n\n' + packet['spec'],
                         'acceptance_criteria': packet['acceptanceCriteria'],
                         'verification_commands': packet['verification']['commands']},
            'runtime_status': None,
            'gaps': ['Upstream status ' + str(packet['status']) + ' remains desired state; no runtime transition inferred.',
                     'Verification strings require host-reviewed argv mapping; commands were not executed.',
                     'File scope, authority, preservation, reviewer identity and policy interpretation require host binding.']}


class StdioTransport:
    """Bounded newline JSON-RPC transport for an explicitly supplied host command."""
    def __init__(self, argv, timeout=10):
        require(isinstance(argv, list) and bool(argv) and all(isinstance(a, str) and a and '\0' not in a for a in argv), 'Host command must be argv')
        require(type(timeout) in (int, float) and math.isfinite(timeout) and 0 < timeout <= 300, 'Invalid transport timeout')
        self.timeout = timeout
        self.counter = 0
        self.buffer = b''
        try:
            self.process = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
        except OSError as exc:
            raise DeliveryError('Cannot start host MCP command') from exc
        os.set_blocking(self.process.stdin.fileno(), False)
        self.selector = selectors.DefaultSelector()
        self.selector.register(self.process.stdout, selectors.EVENT_READ)

    def __enter__(self):
        return self

    def __exit__(self, *_):
        self.close()

    def close(self):
        if self.process.poll() is None:
            self.process.terminate()
            try:
                self.process.wait(timeout=1)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()
        self.selector.close()
        self.process.stdin.close()
        self.process.stdout.close()

    def __call__(self, method, params):
        self.counter += 1
        notification = method.startswith('notifications/')
        request = {'jsonrpc': '2.0', 'method': method, 'params': params}
        if not notification:
            request['id'] = self.counter
        payload = json.dumps(request).encode() + b'\n'
        require(len(payload) <= 65536, 'MCP request exceeds bound')
        deadline = time.monotonic() + self.timeout
        offset = 0
        try:
            while offset < len(payload):
                remaining = deadline - time.monotonic()
                require(remaining > 0 and select.select([], [self.process.stdin], [], remaining)[1], 'MCP input timeout')
                try:
                    offset += os.write(self.process.stdin.fileno(), payload[offset:])
                except BlockingIOError:
                    continue
        except (OSError, ValueError) as exc:
            raise DeliveryError('MCP peer closed input') from exc
        if notification:
            return None
        deadline = time.monotonic() + self.timeout
        while True:
            while b'\n' not in self.buffer:
                remaining = deadline - time.monotonic()
                require(remaining > 0 and self.selector.select(remaining), 'MCP response timeout')
                chunk = os.read(self.process.stdout.fileno(), 65536)
                require(bool(chunk), 'MCP peer closed output')
                self.buffer += chunk
                require(len(self.buffer) <= MAX_MESSAGE, 'MCP response exceeds bound')
            line, self.buffer = self.buffer.split(b'\n', 1)
            try:
                response = json.loads(line)
            except (ValueError, UnicodeError) as exc:
                raise DeliveryError('Malformed MCP response') from exc
            require(isinstance(response, dict) and response.get('jsonrpc') == '2.0', 'Malformed MCP envelope')
            require(response.get('id') == self.counter, 'Unexpected MCP response identity')
            require('error' not in response, 'MCP RPC error')
            require('result' in response, 'Missing MCP result')
            return response['result']


def validate_attempt_reference(reference, expected):
    """Check host-exported runtime references, without asserting live lease ownership."""
    fields = {'attempt_id', 'task_id', 'worker', 'revision', 'tree', 'environment'}
    for value in (reference, expected):
        require(isinstance(value, dict) and set(value) == fields, 'Complete attempt reference required')
        require(all(isinstance(v, str) and v.strip() for v in value.values()), 'Missing attempt reference identity')
        for field in ('revision', 'tree'):
            require(re.fullmatch('[a-f0-9]{40}|[a-f0-9]{64}', value[field]), 'Invalid attempt revision')
    require(reference == expected, 'Attempt or revision mismatch')
    return dict(reference)


class SwarmdClient:
    def __init__(self, transport):
        self.transport = transport
        self.initialized = False
        self.server = None

    def initialize(self):
        result = self.transport('initialize', {'protocolVersion': '2024-11-05', 'capabilities': {},
                    'clientInfo': {'name': 'repo-delivery', 'version': '0.1.0'}})
        require(isinstance(result, dict) and result.get('protocolVersion') == '2024-11-05', 'Unsupported MCP protocol version')
        require(isinstance(result.get('serverInfo'), dict) and result['serverInfo'].get('name') == 'swarmd', 'Unexpected MCP server')
        self.transport('notifications/initialized', {})
        self.server = result['serverInfo']
        self.initialized = True

    def task(self, task_id):
        require(isinstance(task_id, str) and bool(task_id.strip()), 'Task ID required')
        if not self.initialized:
            self.initialize()
        result = self.transport('tools/call', {'name': 'task.tree', 'arguments': {'id': task_id}})
        require(isinstance(result, dict) and result.get('isError') is False, 'swarmd tool failed')
        content = result.get('content')
        require(isinstance(content, list) and len(content) == 1 and isinstance(content[0], dict) and content[0].get('type') == 'text', 'Unexpected swarmd tool content')
        try:
            task = json.loads(content[0]['text'])
        except (KeyError, TypeError, ValueError) as exc:
            raise DeliveryError('Malformed swarmd task response') from exc
        require(isinstance(task, dict) and isinstance(task.get('task'), dict) and task['task'].get('id') == task_id, 'Task response identity mismatch')
        return task

    def finish(self, reference, expected, *, lease_token=None, success=True, authorized_actions=()):
        from .runtime_finalize import finish
        return finish(self, reference, expected, lease_token=lease_token, success=success,
                      authorized_actions=authorized_actions)


def consume_swarmd_dump(path, attempt_id):
    """Consume host-exported `swarmd dump --table execution_attempts` NDJSON.

    No database is opened here. A recorded checkpoint identifies revision/tree;
    a running attempt has no revision-bound handoff yet and must be rejected.
    Exported rows are observations, not live lease or completion authorization.
    """
    try:
        with Path(path).open('rb') as stream:
            raw = stream.read(MAX_MESSAGE + 1)
        require(len(raw) <= MAX_MESSAGE, 'Runtime export exceeds size bound')
        rows = [json.loads(line) for line in raw.splitlines() if line.strip()]
    except (OSError, ValueError, UnicodeError) as exc:
        raise DeliveryError('Malformed runtime export') from exc
    require(all(isinstance(row, dict) for row in rows), 'Invalid runtime export rows')
    matches = [row for row in rows if row.get('id') == attempt_id]
    require(len(matches) == 1, 'Missing or ambiguous durable attempt')
    row = matches[0]
    require(row.get('_table', 'execution_attempts') == 'execution_attempts', 'Wrong runtime export table')
    require(row.get('state') in ('recorded', 'complete', 'failed'), 'Attempt has no durable revision checkpoint')
    try:
        checkpoint = json.loads(row['record_json'])
    except (KeyError, TypeError, ValueError) as exc:
        raise DeliveryError('Invalid runtime attempt checkpoint') from exc
    require(isinstance(checkpoint, dict), 'Invalid runtime checkpoint shape')
    reference = {'attempt_id': row.get('id'), 'task_id': row.get('task_id'),
                 'worker': row.get('worker'), 'revision': checkpoint.get('Revision'),
                 'tree': checkpoint.get('Tree'), 'environment': checkpoint.get('Environment')}
    validate_attempt_reference(reference, reference)
    require(reference['environment'] == row.get('environment'), 'Runtime environment mismatch')
    return {'schema_version': 1, 'kind': 'swarmd-reference', 'reference': reference,
            'export_sha256': digest(raw), 'observed_state': row['state'],
            'capabilities': {'read_task': True, 'managed_finish': None},
            'gaps': ['Export is not a live lease check.',
                     'Finalization capability and lease authority require a live runtime check.']}
