#!/usr/bin/env python3
"""Repeat Codex project-registered custom-role checks without model turns.

The script requires an AOK binary, a copied Codex CLI 0.162.1 binary, and
Darwin sandbox-exec. Both are invoked with an environment allow-list. Host
network and writes outside one disposable root are denied. It sends no turn.
"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import signal
import subprocess
import tempfile
import time


def sha(data):
    return hashlib.sha256(data).hexdigest()


def run(command, *, root, cwd, env, timeout=30):
    root = Path(root).resolve(strict=True)
    cwd = Path(cwd).resolve(strict=True)
    if cwd != root and root not in cwd.parents:
        raise ValueError("probe working directory escaped its temporary root")
    profile = ('(version 1)(allow default)(deny network*)(deny mach-lookup (global-name "com.apple.securityd"))' +
               '(deny file-write*)(allow file-write* (subpath ' + json.dumps(str(root)) +
               ') (literal "/dev/null") (literal "/dev/tty"))')
    proc = subprocess.Popen(['/usr/bin/sandbox-exec', '-p', profile, *map(str, command)],
                            cwd=cwd, env=env, stdin=subprocess.PIPE,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            start_new_session=True)
    try:
        out, err = proc.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        os.killpg(proc.pid, signal.SIGTERM)
        try:
            out, err = proc.communicate(timeout=4)
        except subprocess.TimeoutExpired:
            os.killpg(proc.pid, signal.SIGKILL)
            out, err = proc.communicate(timeout=2)
        raise TimeoutError(f"bounded command timed out: {Path(command[0]).name}")
    if len(out) > 8 * 1024 * 1024 or len(err) > 8 * 1024 * 1024:
        raise ValueError("probe output exceeded cap")
    return proc.returncode, out, err


def aok_call(aok, args, *, root, env):
    code, out, err = run([aok, *args], root=root, cwd=root, env=env, timeout=30)
    try:
        report = json.loads(out)
    except Exception as exc:
        raise RuntimeError(f"AOK returned non-JSON output (exit {code})") from exc
    expected = 0 if report.get('status') == 'valid' else 1
    if code != expected:
        raise RuntimeError(f"AOK exit/status mismatch for {args[2]}: {code}/{report.get('status')}")
    return report


def rpc(proc, request, timeout=10):
    proc.stdin.write(json.dumps(request, separators=(',', ':')).encode() + b'\n')
    proc.stdin.flush()
    request_id = request.get('id')
    records = []
    selector = selectors.DefaultSelector()
    selector.register(proc.stdout, selectors.EVENT_READ)
    deadline = time.monotonic() + timeout
    fd = proc.stdout.fileno()
    os.set_blocking(fd, False)
    buffer = getattr(proc, '_aok_rpc_buffer', bytearray())
    while time.monotonic() < deadline:
        while b'\n' in buffer:
            line, _, remainder = buffer.partition(b'\n')
            buffer = bytearray(remainder)
            if len(line) > 1024 * 1024:
                raise ValueError("app-server response line exceeded cap")
            try:
                message = json.loads(line)
            except json.JSONDecodeError:
                continue
            records.append(message)
            if request_id is not None and message.get('id') == request_id:
                setattr(proc, '_aok_rpc_buffer', buffer)
                return message, records
        ready = selector.select(max(0, deadline - time.monotonic()))
        if not ready:
            break
        chunk = os.read(fd, 65536)
        if not chunk:
            break
        buffer.extend(chunk)
        if len(buffer) > 8 * 1024 * 1024:
            raise ValueError("app-server buffered response exceeded cap")
    setattr(proc, '_aok_rpc_buffer', buffer)
    raise TimeoutError(f"app-server did not answer {request.get('method')}")


def host_case(codex, root, project, *, role_bytes, config_bytes, case):
    project.mkdir(parents=True, exist_ok=True)
    codex_dir = project / '.codex'
    (codex_dir / 'agents').mkdir(parents=True, exist_ok=True)
    if role_bytes is not None:
        (codex_dir / 'agents/aok-probe.toml').write_bytes(role_bytes)
    (codex_dir / 'config.toml').write_bytes(config_bytes)
    home = root / (case + '-home')
    codex_home = home / '.codex'
    home.mkdir(parents=True)
    codex_home.mkdir(parents=True)
    trust = (f'[projects.{json.dumps(str(project.resolve()))}]\n'
             'trust_level = "trusted"\n').encode()
    (codex_home / 'config.toml').write_bytes(trust)
    for p in (home / '.cache', home / '.config', root / 'tmp'):
        p.mkdir(parents=True, exist_ok=True)
    env = {'PATH': '/usr/bin:/bin', 'HOME': str(home), 'CODEX_HOME': str(codex_home),
           'XDG_CONFIG_HOME': str(home / '.config'), 'XDG_CACHE_HOME': str(home / '.cache'),
           'TMPDIR': str(root / 'tmp')}
    proc = subprocess.Popen(['/usr/bin/sandbox-exec', '-p',
        '(version 1)(allow default)(deny network*)(deny mach-lookup (global-name "com.apple.securityd"))(deny file-write*)(allow file-write* (subpath ' +
        json.dumps(str(root)) + ') (literal "/dev/null") (literal "/dev/tty"))',
        str(codex), 'app-server', '--listen', 'stdio://'], cwd=project, env=env,
        stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
        start_new_session=True)
    try:
        _, init_records = rpc(proc, {'id': 1, 'method': 'initialize', 'params': {
            'clientInfo': {'name': 'aok-native-probe', 'title': 'AOK synthetic registration check', 'version': '1'}}})
        proc.stdin.write(b'{"method":"initialized","params":{}}\n')
        proc.stdin.flush()
        config_response, config_records = rpc(proc, {'id': 2, 'method': 'config/read', 'params': {
            'cwd': str(project.resolve()), 'includeLayers': True}})
        start_response, start_records = rpc(proc, {'id': 3, 'method': 'thread/start', 'params': {
            'cwd': str(project.resolve()), 'ephemeral': True, 'model': 'gpt-6-luna',
            'approvalPolicy': 'never', 'sandbox': 'read-only'}}, timeout=15)
        warnings = []
        all_records = init_records + config_records + start_records
        for record in all_records:
            data = json.dumps(record, sort_keys=True).lower()
            if 'configwarning' in data or 'malformed agent role' in data or 'config_file must point' in data:
                warnings.append(record)
        cfg = config_response.get('result', {})
        return {
            'configReadError': config_response.get('error'),
            'agentsValue': cfg.get('config', {}).get('agents') if isinstance(cfg.get('config'), dict) else cfg.get('agents'),
            'projectLayer': cfg.get('layers', {}).get('project') if isinstance(cfg.get('layers'), dict) else None,
            'threadStartError': start_response.get('error'),
            'threadStartSucceeded': 'result' in start_response,
            'configWarningCount': len(warnings),
            'warningCodes': sorted({str(r.get('method', r.get('params', {}).get('type', 'warning'))) for r in warnings}),
            'warningSummaries': [re.sub(re.escape(str(root)), '<PROBE_ROOT>', json.dumps(w, sort_keys=True))[:1600] for w in warnings],
        }
    finally:
        try:
            proc.stdin.close()
        except Exception:
            pass
        try:
            os.killpg(proc.pid, signal.SIGTERM)
            proc.wait(timeout=3)
        except Exception:
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            proc.wait(timeout=2)
        proc.stdout.close()
        proc.stderr.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--aok', type=Path, required=True)
    parser.add_argument('--codex-copy', type=Path, required=True,
                        help='local copy of the installed Codex CLI 0.162.1 binary')
    args = parser.parse_args()
    if not Path('/usr/bin/sandbox-exec').is_file():
        raise SystemExit('Darwin sandbox-exec is required')
    aok = args.aok.resolve(strict=True)
    codex_source = args.codex_copy.resolve(strict=True)
    repo = Path(__file__).resolve().parents[2]
    with tempfile.TemporaryDirectory(prefix='aok-codex-registered-') as temp:
        root = Path(temp).resolve(strict=True)
        codex = root / 'codex'
        shutil.copyfile(codex_source, codex)
        os.chmod(codex, 0o700)
        source = (repo / 'internal/nativeadapters/codex/testdata/probe.toml').read_bytes()
        (root / 'input').mkdir()
        native_config = b'[agents.aok-probe]\ndescription = "Synthetic read-only discovery fixture"\nconfig_file = "agents/aok-probe.toml"\n'
        (root / 'input/.codex/agents').mkdir(parents=True)
        (root / 'input/.codex/agents/aok-probe.toml').write_bytes(source)
        (root / 'input/.codex/config.toml').write_bytes(native_config)
        (root / 'input/aok-native-input.json').write_text(json.dumps({'version':'aok-native-input/v1','files':[
            {'path':'.codex/config.toml','format':'toml'},
            {'path':'.codex/agents/aok-probe.toml','format':'toml'}]}))
        home = root / 'aok-home'
        (home / '.codex').mkdir(parents=True)
        (root / 'tmp').mkdir()
        aok_env = {'PATH':'/usr/bin:/bin', 'HOME':str(home), 'CODEX_HOME':str(home/'.codex'), 'TMPDIR':str(root/'tmp')}
        imported = aok_call(aok, ['agent','native','import','--target','codex','--scope','project-registered',
                                  '--input',str(root/'input'),'--out',str(root/'draft'),'--json'],root=root,env=aok_env)
        exported = aok_call(aok, ['agent','native','export','--target','codex','--scope','project-registered',
                                  '--input',str(root/'draft/aok-native.json'),'--source',str(root/'input'),
                                  '--out',str(root/'export'),'--json'],root=root,env=aok_env)
        role_bytes = (root / 'export/.codex/agents/aok-probe.toml').read_bytes()
        config_bytes = (root / 'export/.codex/config.toml').read_bytes()
        if not role_bytes or b'[agents.aok-probe]' not in config_bytes:
            raise RuntimeError('fresh AOK export did not contain the selected registered role')
        version_code, version_out, _ = run([str(codex), '--version'], root=root, cwd=root,
                                           env={'PATH':'/usr/bin:/bin','HOME':str(root),'TMPDIR':str(root/'tmp')}, timeout=10)
        if version_code != 0 or version_out.decode().strip() != 'codex-cli 0.162.1':
            raise RuntimeError('copied Codex binary did not match the audited 0.162.1 version')
        cases = {}
        cases['positive'] = host_case(codex, root, root/'positive', role_bytes=role_bytes,
            config_bytes=config_bytes, case='positive')
        cases['absentRegistration'] = host_case(codex, root, root/'absent', role_bytes=role_bytes,
            config_bytes=b'', case='absent')
        cases['malformedConfig'] = host_case(codex, root, root/'malformed-config', role_bytes=role_bytes,
            config_bytes=b'[agents.aok-probe\n', case='malformed-config')
        cases['malformedRole'] = host_case(codex, root, root/'malformed-role', role_bytes=b'name = [\n',
            config_bytes=config_bytes, case='malformed-role')
        missing_config = b'[agents.aok-probe]\ndescription = "Synthetic read-only discovery fixture"\nconfig_file = "agents/missing.toml"\n'
        cases['missingRole'] = host_case(codex, root, root/'missing-role', role_bytes=None,
            config_bytes=missing_config, case='missing-role')
        positive_agents = cases['positive'].get('agentsValue')
        absent_agents = cases['absentRegistration'].get('agentsValue')
        if not cases['positive']['threadStartSucceeded'] or not isinstance(positive_agents, dict) or 'aok-probe' not in positive_agents or cases['positive']['configWarningCount']:
            raise RuntimeError('positive AOK-registered export did not load cleanly')
        if not cases['absentRegistration']['threadStartSucceeded'] or isinstance(absent_agents, dict) and 'aok-probe' in absent_agents:
            raise RuntimeError('unregistered standalone role was unexpectedly loaded')
        if cases['malformedConfig']['configReadError'] is None:
            raise RuntimeError('malformed project config was not rejected')
        if not cases['malformedRole']['configWarningCount'] or not cases['missingRole']['configWarningCount']:
            raise RuntimeError('malformed/missing selected role controls did not warn')
        result = {
            'target':'codex','scope':'project-registered','hostVersion':version_out.decode().strip(),
            'observedAt':datetime.now(timezone.utc).isoformat(),
            'nativeLoading':'verified for project-registered config discovery and selected role startup validation',
            'runtimeEnforcement':'unverified','modelExecution':'not-run',
            'probeScript':'scripts/native-loading/verify_codex_project_registered.py',
            'probeScriptSHA256':sha(Path(__file__).read_bytes()),
            'aokImportStatus': imported.get('status'),'aokExportStatus':exported.get('status'),
            'aokBinarySHA256':sha(aok.read_bytes()),'codexBinarySHA256':sha(codex.read_bytes()),
            'sourceRoleSHA256':sha(source),'exportedRoleSHA256':sha(role_bytes),
            'exportedConfigSHA256':sha(config_bytes),'exportedRole':role_bytes.decode(),
            'exportedConfig':config_bytes.decode(),'cases':cases,
            'safety':{'homeAndProject':'disposable temporary root','environment':'allow-list; no credential variables',
                      'hostNetwork':'sandbox-exec denied network*','writes':'sandbox-exec denied writes outside temporary root',
                      'keychain':'sandbox-exec denied mach-lookup to com.apple.securityd',
                      'requests':['app-server initialize','config/read','ephemeral read-only thread/start'],
                      'turns':'none','inference':'none','authentication':'none'},
        }
        rendered = json.dumps(result, indent=2, sort_keys=True).replace(str(root), '<PROBE_ROOT>')
        print(rendered)


if __name__ == '__main__':
    main()
