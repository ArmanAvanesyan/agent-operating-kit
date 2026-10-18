#!/usr/bin/env python3
"""Bounded Cursor ACP / AGY list-surface probes with synthetic fixtures only.

Cursor receives only the documented synthetic invalid API-key argument; no
authenticate or session/prompt request is sent. AGY receives no prompt at all.
Both host binaries are copied before invocation. No user credentials/config are
copied. Native invocations run under a Darwin write/network guard; Cursor also
has the keychain Mach service denied.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile


def sha_bytes(data):
    return hashlib.sha256(data).hexdigest()


def sha_file(path):
    return sha_bytes(Path(path).read_bytes())


def prefer_cumulative(previous, current):
    """TimeoutExpired data is a prefix; communicate() later returns cumulative output."""
    return current if current is not None else previous


def confined(command, *, root, env, timeout=30, stdin=None, block_keychain=False, cwd=None, loopback=False):
    root = Path(root).resolve(strict=True)
    actual_cwd = root if cwd is None else Path(cwd).resolve(strict=True)
    if actual_cwd != root and root not in actual_cwd.parents:
        raise ValueError('native probe cwd must stay inside its temporary root')
    profile = ('(version 1)(allow default)(deny file-write*)'
               '(allow file-write* (subpath ' + json.dumps(str(root)) + ')'
               ' (literal "/dev/null") (literal "/dev/tty"))(deny network*)')
    if block_keychain:
        profile += '(deny mach-lookup (global-name "com.apple.securityd"))'
    if loopback:
        profile += '(allow network-bind (local ip "localhost:*"))(allow network-inbound (local ip "localhost:*"))'
    proc = subprocess.Popen(['/usr/bin/sandbox-exec', '-p', profile, *map(str, command)],
                            cwd=actual_cwd, env=env, stdin=subprocess.PIPE if stdin is not None else subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
    try:
        out, err = proc.communicate(input=stdin, timeout=timeout)
    except subprocess.TimeoutExpired as initial_timeout:
        partial_out = initial_timeout.output or b''
        partial_err = initial_timeout.stderr or b''
        try:
            os.killpg(proc.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            out, err = proc.communicate(timeout=2)
            partial_out = prefer_cumulative(partial_out, out)
            partial_err = prefer_cumulative(partial_err, err)
        except subprocess.TimeoutExpired as term_timeout:
            partial_out = term_timeout.output or partial_out
            partial_err = term_timeout.stderr or partial_err
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            try:
                out, err = proc.communicate(timeout=5)
                partial_out = prefer_cumulative(partial_out, out)
                partial_err = prefer_cumulative(partial_err, err)
            except subprocess.TimeoutExpired as kill_timeout:
                partial_out = kill_timeout.output or partial_out
                partial_err = kill_timeout.stderr or partial_err
                proc.stdout.close()
                proc.stderr.close()
                proc.wait(timeout=2)
        return 124, partial_out, partial_err
    return proc.returncode, out, err


def report_json(out):
    try:
        value = json.loads(out.decode('utf-8'))
    except Exception as exc:
        raise RuntimeError('AOK returned no JSON report') from exc
    return value if isinstance(value, dict) else {}


def sanitized_lines(raw, root, needles=()):
    text = raw.decode('utf-8', 'replace').replace(str(root), '<TEMP_ROOT>')
    for item in needles:
        if item:
            text = text.replace(item, '<SYNTHETIC_KEY>')
    return [line[:500] for line in text.splitlines()[:30]]


def main():
    p = argparse.ArgumentParser()
    p.add_argument('--aok', type=Path, required=True)
    p.add_argument('--cursor-host-dir', type=Path, required=True)
    p.add_argument('--agy-binary', type=Path, required=True)
    p.add_argument('--retain-agy-render', type=Path)
    p.add_argument('--retain-cursor-render', type=Path)
    args = p.parse_args()
    repo = Path(__file__).resolve().parents[2]
    aok = args.aok.resolve(strict=True)
    cursor_source = args.cursor_host_dir.resolve(strict=True)
    agy_source = args.agy_binary.resolve(strict=True)
    if not Path('/usr/bin/sandbox-exec').is_file():
        raise SystemExit('Darwin sandbox-exec is required')
    cursor_wrapper = cursor_source / 'cursor-agent'
    if not cursor_wrapper.is_file() or not (cursor_source / 'index.js').is_file() or not (cursor_source / 'node').is_file():
        raise SystemExit('Cursor host directory is incomplete')
    cursor_before = sha_file(cursor_source / 'index.js')
    agy_before = sha_file(agy_source)

    with tempfile.TemporaryDirectory(prefix='aok-offline-clients.', dir='/tmp') as temp:
        root = Path(temp).resolve(strict=True)
        aok_env = {'PATH': '/usr/bin:/bin', 'HOME': str(root / 'aok-home'), 'TMPDIR': str(root)}

        # Produce actual AOK imported-draft exports for each native host.
        cursor_input = root / 'cursor-input'
        cursor_md = cursor_input / '.cursor/agents/reviewer.md'
        cursor_md.parent.mkdir(parents=True)
        shutil.copyfile(repo / 'internal/nativeadapters/cursor/testdata/reviewer.md', cursor_md)
        (cursor_input / 'aok-native-input.json').write_text(json.dumps({
            'version': 'aok-native-input/v1', 'files': [{'path': '.cursor/agents/reviewer.md', 'format': 'markdown'}]
        }), encoding='utf-8')
        cursor_import = root / 'cursor-import'
        rc, out, _ = confined([aok, 'agent', 'native', 'import', '--target', 'cursor', '--scope', 'project',
                               '--input', cursor_input, '--out', cursor_import, '--json'], root=root, env=aok_env)
        cursor_import_report = report_json(out)
        cursor_bundle = cursor_import / 'aok-native.json'
        cursor_bundle_json = json.loads(cursor_bundle.read_text(encoding='utf-8'))
        if rc != 1 or cursor_import_report.get('status') != 'blocked' or cursor_bundle_json.get('completeness') != 'draft-incomplete':
            raise SystemExit('Cursor fixture import did not produce expected incomplete draft')
        cursor_export = root / 'cursor-export'
        rc, out, _ = confined([aok, 'agent', 'native', 'export', '--target', 'cursor', '--scope', 'project',
                               '--input', cursor_bundle, '--source', cursor_input, '--out', cursor_export, '--json'],
                              root=root, env=aok_env)
        cursor_export_report = report_json(out)
        cursor_file = cursor_export / '.cursor/agents/reviewer.md'
        if rc != 1 or cursor_export_report.get('status') != 'blocked' or not cursor_file.is_file():
            raise SystemExit('Cursor fixture export did not materialize expected draft')
        retained_cursor_render = args.retain_cursor_render or (repo / 'internal/nativeadapters/cursor/testdata/probe-aok-rendered.md')
        retained_cursor_render = retained_cursor_render.resolve()
        if repo not in retained_cursor_render.parents:
            raise SystemExit('retained Cursor render must be inside the repository')
        if retained_cursor_render.exists():
            if retained_cursor_render.read_bytes() != cursor_file.read_bytes():
                raise SystemExit('retained Cursor render exists with different bytes')
        else:
            retained_cursor_render.parent.mkdir(parents=True, exist_ok=True)
            retained_cursor_render.write_bytes(cursor_file.read_bytes())

        agy_fixture = repo / 'internal/nativeadapters/agy/testdata/probe.md'
        agy_input = root / 'agy-input'
        agy_md = agy_input / '.agents/agents/aok-probe/agent.md'
        agy_md.parent.mkdir(parents=True)
        shutil.copyfile(agy_fixture, agy_md)
        (agy_input / 'aok-native-input.json').write_text(json.dumps({
            'version': 'aok-native-input/v1', 'files': [{'path': '.agents/agents/aok-probe/agent.md', 'format': 'markdown'}]
        }), encoding='utf-8')
        agy_import = root / 'agy-import'
        rc, out, _ = confined([aok, 'agent', 'native', 'import', '--target', 'agy', '--scope', 'project',
                               '--input', agy_input, '--out', agy_import, '--json'], root=root, env=aok_env)
        agy_import_report = report_json(out)
        agy_bundle = agy_import / 'aok-native.json'
        if not agy_bundle.is_file():
            details = [{'code': d.get('code'), 'message': d.get('message')}
                       for d in agy_import_report.get('diagnostics', []) if isinstance(d, dict)]
            raise SystemExit('AOK AGY import produced no bundle: status=%s diagnostics=%s' %
                             (agy_import_report.get('status'), details))
        agy_bundle_json = json.loads(agy_bundle.read_text(encoding='utf-8'))
        if rc != 1 or agy_import_report.get('status') != 'blocked' or agy_bundle_json.get('completeness') != 'draft-incomplete':
            raise SystemExit('AGY fixture import did not produce expected incomplete draft')
        agy_export = root / 'agy-export'
        rc, out, _ = confined([aok, 'agent', 'native', 'export', '--target', 'agy', '--scope', 'project',
                               '--input', agy_bundle, '--source', agy_input, '--out', agy_export, '--json'],
                              root=root, env=aok_env)
        agy_export_report = report_json(out)
        agy_file = agy_export / '.agents/agents/aok-probe/agent.md'
        if rc != 1 or agy_export_report.get('status') != 'blocked' or not agy_file.is_file():
            raise SystemExit('AGY fixture export did not materialize expected draft')
        agy_export_hash = sha_file(agy_file)
        retained_agy_render = args.retain_agy_render or (repo / 'internal/nativeadapters/agy/testdata/probe-aok-rendered.md')
        retained_agy_render = retained_agy_render.resolve()
        if repo not in retained_agy_render.parents:
            raise SystemExit('retained AGY render must be inside the repository')
        if retained_agy_render.exists():
            if retained_agy_render.read_bytes() != agy_file.read_bytes():
                raise SystemExit('retained AGY render exists with different bytes')
        else:
            retained_agy_render.parent.mkdir(parents=True, exist_ok=True)
            retained_agy_render.write_bytes(agy_file.read_bytes())

        # Copy both installed host trees/binaries before any invocation.
        cursor_copy = root / 'cursor-host'
        shutil.copytree(cursor_source, cursor_copy, symlinks=True)
        agy_copy = root / 'agy-copy'
        shutil.copyfile(agy_source, agy_copy)
        agy_copy.chmod(0o700)
        if sha_file(agy_copy) != agy_before:
            raise SystemExit('AGY copied binary hash mismatch')
        cursor_env = {
            'PATH': '/usr/bin:/bin', 'HOME': str(root / 'cursor-home'), 'TMPDIR': str(root),
            'XDG_CONFIG_HOME': str(root / 'cursor-home/config'), 'XDG_CACHE_HOME': str(root / 'cursor-home/cache'),
            'CURSOR_CONFIG_DIR': str(root / 'cursor-config'), 'CURSOR_DATA_DIR': str(root / 'cursor-data'),
            'NODE_COMPILE_CACHE': str(root / 'cursor-home/compile-cache'), 'CURSOR_INVOKED_AS': 'cursor-agent',
        }
        (root / 'cursor-home').mkdir()
        cursor_version = '2026.04.17-787b533'
        if cursor_before != 'b229d091e5199812927f0c74918d6b53e61fcc869fe39c2f94bad2ab1c049d2e':
            raise SystemExit('Cursor bundle hash does not match pinned version identity')
        agy_version = '1.3.6'
        if agy_before != '2a6bd844ae3431d35203af44f31bdd5e26f5ffe287d47c62a46862bf44099fc2':
            raise SystemExit('AGY binary hash does not match pinned version identity')

        # Cursor ACP is sent only initialize + initialized + session/new. This
        # carries no authenticate and no session/prompt method.
        synthetic_key = 'aok-invalid-local-probe'
        cursor_project = root / 'cursor-project'
        cursor_project.mkdir()
        cursor_project_agent = cursor_project / '.cursor/agents/reviewer.md'
        cursor_project_agent.parent.mkdir(parents=True)
        shutil.copyfile(cursor_file, cursor_project_agent)
        acp_lines = [
            {'jsonrpc': '2.0', 'id': 1, 'method': 'initialize', 'params': {
                'protocolVersion': 1, 'clientInfo': {'name': 'aok-offline-probe', 'version': '1.0'},
                'clientCapabilities': {'fs': {'readTextFile': False, 'writeTextFile': False}, 'terminal': False}}},
            {'jsonrpc': '2.0', 'method': 'initialized', 'params': {}},
            {'jsonrpc': '2.0', 'id': 2, 'method': 'session/new', 'params': {'cwd': str(cursor_project), 'mcpServers': []}},
        ]
        acp_input = ''.join(json.dumps(x, separators=(',', ':')) + '\n' for x in acp_lines).encode()
        cursor_rc, cursor_out, cursor_err = confined(
            [cursor_copy / 'cursor-agent', '--api-key', synthetic_key, 'acp'], root=root,
            env=cursor_env, timeout=30, stdin=acp_input, block_keychain=True)
        cursor_text = cursor_out.decode('utf-8', 'replace')
        cursor_json_methods = []
        cursor_json_errors = []
        cursor_error_fingerprints = set()
        cursor_id1 = {'response': 'absent'}
        for line in cursor_text.splitlines():
            try:
                obj = json.loads(line)
            except Exception:
                continue
            if isinstance(obj, dict) and isinstance(obj.get('method'), str):
                cursor_json_methods.append(obj['method'])
            if isinstance(obj, dict) and obj.get('id') == 1:
                cursor_id1 = {'response': 'error' if 'error' in obj else 'result' if 'result' in obj else 'other'}
            if isinstance(obj, dict) and 'error' in obj:
                err = obj['error'] if isinstance(obj['error'], dict) else {}
                message = str(err.get('message', '')).lower()
                if any(token in message for token in ('keychain', 'securityd')):
                    error_class = 'keychain'
                elif any(token in message for token in ('network', 'connect', 'socket')):
                    error_class = 'network'
                elif any(token in message for token in ('auth', 'credential', 'api key', 'apikey')):
                    error_class = 'authentication'
                else:
                    error_class = 'backend-or-unknown'
                fingerprint = (obj.get('id'), err.get('code'), error_class,
                               'present' if err.get('message') else 'absent')
                cursor_json_errors.append({'id': obj.get('id'), 'code': err.get('code'),
                                           'messageClass': fingerprint[3], 'class': error_class,
                                           'sameAsPrevious': fingerprint in cursor_error_fingerprints})
                cursor_error_fingerprints.add(fingerprint)
        if sha_file(cursor_source / 'index.js') != cursor_before:
            raise SystemExit('Cursor original bundle changed during probe')
        if sha_file(agy_source) != agy_before:
            raise SystemExit('AGY original binary changed during probe')

        # AGY help/list probes only. Headless -p is deliberately excluded:
        # official docs state it sends a model prompt and uses cached auth.
        agy_env = {'PATH': '/usr/bin:/bin', 'HOME': str(root / 'agy-home'), 'TMPDIR': str(root)}
        (root / 'agy-home').mkdir()
        agy_positive = root / 'agy-project-positive'
        agy_positive.mkdir()
        shutil.copyfile(agy_file, agy_positive / '.agents-placeholder') if False else None
        native_positive = agy_positive / '.agents/agents/aok-probe/agent.md'
        native_positive.parent.mkdir(parents=True)
        shutil.copyfile(agy_file, native_positive)
        agy_empty = root / 'agy-project-empty'
        agy_empty.mkdir()
        agy_bad = root / 'agy-project-malformed'
        bad_native = agy_bad / '.agents/agents/bad/agent.md'
        bad_native.parent.mkdir(parents=True)
        bad_native.write_text('not valid frontmatter\n', encoding='utf-8')
        help_results = {}
        for label, argv in [('root', ['--help']), ('agent', ['agent', '--help']), ('agents', ['agents', '--help'])]:
            rc, so, se = confined([agy_copy, *argv], root=root, env=agy_env, timeout=15)
            help_results[label] = {'exitCode': rc, 'stdoutBytes': len(so), 'stderrBytes': len(se),
                                   'mentionsPrint': b'--print' in so or b'--print' in se,
                                   'mentionsAgentSelector': b'--agent' in so or b'--agent' in se,
                                   'mentionsOutputFormat': b'--output-format' in so or b'--output-format' in se}
        list_results = {}
        for label, cwd in [('positive', agy_positive), ('absent', agy_empty), ('malformed', agy_bad)]:
            rc, so, se = confined([agy_copy, 'agents'], root=root, env=agy_env, timeout=30, cwd=cwd, loopback=True)
            # Isolate the process cwd using a duplicated guard rooted at the
            # same owned temp root; this helper intentionally supports cwd.
            list_results[label] = {'exitCode': rc, 'stdoutBytes': len(so), 'stderrBytes': len(se),
                                   'stderrSummary': sanitized_lines(se, root)}

        result = {
            'observedAt': datetime.now(timezone.utc).isoformat(),
            'probeScriptSha256': sha_file(Path(__file__).resolve()),
            'aokBinarySha256': sha_file(aok),
            'cursor': {
                'hostVersion': cursor_version, 'copiedIndexSha256': cursor_before,
                'versionIdentity': 'pinned installed bundle hash; --version is not run under the offline guard',
                'aokImport': cursor_import_report.get('status'), 'aokBundleCompleteness': cursor_bundle_json.get('completeness'),
                'aokExport': cursor_export_report.get('status'), 'renderedFixtureSha256': sha_file(cursor_file),
                'retainedRenderPath': str(retained_cursor_render.relative_to(repo)),
                'retainedRenderSha256': sha_file(retained_cursor_render),
                'acp': {'argv': ['cursor-agent', '--api-key', '<SYNTHETIC_INVALID_KEY>', 'acp'],
                        'requestsSent': ['initialize', 'initialized', 'session/new'], 'initializeResponse': cursor_id1,
                        'authenticateSent': False,
                        'sessionPromptSent': False, 'exitCode': cursor_rc, 'responseMethods': cursor_json_methods,
                        'errors': cursor_json_errors, 'stdoutBytes': len(cursor_out), 'stderrBytes': len(cursor_err),
                        'stderrSummary': sanitized_lines(cursor_err, root, [synthetic_key]),
                        'discoveryEvidence': False, 'keychainMachDenied': True, 'remoteNetworkDenied': True},
                'nativeLoading': 'not-verified'
            },
            'agy': {
                'hostVersion': agy_version, 'copiedBinarySha256': sha_file(agy_copy),
                'versionIdentity': 'pinned installed binary SHA-256',
                'fixture': {'path': '.agents/agents/aok-probe/agent.md', 'sha256': agy_export_hash,
                            'sourceTestFixtureSha256': sha_file(agy_fixture), 'retainedPath': str(retained_agy_render.relative_to(repo)),
                            'retainedSha256': sha_file(retained_agy_render)},
                'help': help_results, 'agentsAlias': list_results,
                'headless': {'documentedLocalSlashPathIdentified': False,
                             'reason': 'official docs define -p/--print as sending a single prompt; no local-only slash command path documented; not run'},
                'nativeLoading': 'not-verified'
            },
            'runtimeEnforcement': 'unverified', 'modelInvocation': 'not-run',
            'sandbox': {'writesOutsideTempRoot': 'denied',
                        'network': 'remote denied; AGY listing allows localhost bind/inbound',
                        'processTimeoutSeconds': 30}
        }
        print(json.dumps(result, sort_keys=True, indent=2))


if __name__ == '__main__':
    main()
