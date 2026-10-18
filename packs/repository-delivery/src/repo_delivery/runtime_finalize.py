"""Explicit live fenced finalization; credentials never become evidence."""
import json
import os
from pathlib import Path
import stat

from .core import DeliveryError
from .evidence import require

FIELDS = {'id': 'string', 'attempt_id': 'string', 'worker': 'string',
          'lease_token': 'string', 'revision': 'string', 'tree': 'string',
          'environment': 'string', 'success': 'boolean'}


def credentials(path, reference):
    """Open a private owner-only regular file without traversing symlinks."""
    path = Path(path)
    require(path.is_absolute() and '..' not in path.parts, 'Absolute credential file required')
    directory = os.open(path.anchor, os.O_RDONLY | os.O_DIRECTORY)
    try:
        for component in path.parts[1:-1]:
            next_directory = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory)
            os.close(directory)
            directory = next_directory
        fd = os.open(path.name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory)
        with os.fdopen(fd, 'rb') as stream:
            info = os.fstat(stream.fileno())
            require(stat.S_ISREG(info.st_mode) and info.st_uid == os.getuid()
                    and stat.S_IMODE(info.st_mode) == 0o600, 'Credentials require owned mode 0600 regular file')
            data = stream.read(16385)
        require(len(data) <= 16384, 'Credentials exceed size bound')
        def unique(pairs):
            value = {}
            for key, item in pairs:
                require(key not in value, 'Duplicate credential field')
                value[key] = item
            return value
        value = json.loads(data, object_pairs_hook=unique)
        require(isinstance(value, dict) and set(value) == {'task_id','attempt_id','worker','lease_token'}, 'Invalid credential fields')
        require(all(value[k] == reference[k] for k in ('task_id','attempt_id','worker')), 'Credential identity mismatch')
        require(isinstance(value['lease_token'], str) and bool(value['lease_token'].strip()), 'Lease token required')
        return value['lease_token']
    except (OSError, ValueError, UnicodeError):
        raise DeliveryError('Cannot read private runtime credentials') from None
    finally:
        os.close(directory)


def finish(client, reference, expected, *, lease_token, success=True, authorized_actions=()):
    from .adapters import validate_attempt_reference
    reference = validate_attempt_reference(reference, expected)
    require(isinstance(authorized_actions, (list, tuple, set)) and 'finalize-attempt' in authorized_actions,
            'Explicit finalize-attempt authorization required')
    require(type(success) is bool, 'Expected success must be boolean')
    require(isinstance(lease_token, str) and bool(lease_token.strip()), 'Lease token required')
    # No peer text, exception context, or returned arbitrary fields can expose credentials.
    try:
        if not client.initialized:
            client.initialize()
        listing = client.transport('tools/list', {})
        require(isinstance(listing, dict) and isinstance(listing.get('tools'), list), 'Missing fenced finalization capability')
        tools = [t for t in listing['tools'] if isinstance(t, dict) and t.get('name') == 'task.finalize']
        require(len(tools) == 1, 'Missing fenced finalization capability')
        schema = tools[0].get('inputSchema', {})
        require(isinstance(schema, dict) and schema.get('type') == 'object'
                and set(schema.get('required', [])) == set(FIELDS)
                and isinstance(schema.get('properties'), dict)
                and all(isinstance(schema['properties'].get(k), dict)
                        and schema['properties'][k].get('type') == v for k,v in FIELDS.items()),
                'Incompatible fenced finalization capability')
    except Exception:
        raise DeliveryError('Cannot establish fenced finalization capability') from None
    arguments = {k:v for k,v in reference.items() if k != 'task_id'}
    arguments.update(id=reference['task_id'], lease_token=lease_token, success=success)
    try:
        result = client.transport('tools/call', {'name':'task.finalize', 'arguments':arguments})
        require(isinstance(result, dict) and result.get('isError') is False, 'Finalization rejected')
        content = result.get('content')
        require(isinstance(content, list) and len(content) == 1 and content[0].get('type') == 'text', 'Invalid finalization response')
        task = json.loads(content[0]['text'])
        outcome = 'complete' if success else 'failed'
        require(isinstance(task, dict) and task.get('id') == reference['task_id']
                and task.get('claimed_by') == reference['worker'] and task.get('status') == outcome,
                'Finalization identity or outcome mismatch')
    except Exception:
        raise DeliveryError('Fenced finalization failed; reconcile runtime state before retrying') from None
    return {'schema_version':1, 'kind':'runtime-finalization', 'reference':reference,
            'outcome':outcome, 'task':{k:task[k] for k in ('id','status','claimed_by')},
            'server':{'name':'swarmd'}}
