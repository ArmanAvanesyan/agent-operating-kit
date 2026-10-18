#!/usr/bin/env python3
"""Observe a versioned discovery cycle; never select agents or send model input.

Consumes only a validated temporary OAuth seed. Each cycle keeps one workspace
and HOME, with a second cold-malformed replicate copied from the original seed.
Account text stays in memory; only controlled-list booleans are retained.
"""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import secrets
import subprocess
import tempfile


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


account = module('agy_cycle_account', 'probe_agy_account_manager.py')
gate = module('agy_cycle_gate', 'agy_discovery_gate.py')
login = account.login


def controlled_list(text, guard_name, guard_description, target_description):
    """Require one complete framed list containing only known exact rows."""
    rows = [row.strip() for row in text.splitlines()]
    result = {'guardEntryVisible': False, 'targetEntryVisible': False, 'completeControlledList': False}
    if rows.count('Available Agents') != 1:
        return result
    start = rows.index('Available Agents') + 1
    ends = [i for i in range(start, len(rows)) if rows[i].startswith('Keyboard: ↑/↓ Navigate')]
    if len(ends) != 1:
        return result
    known = {'default': 'Default agent', guard_name: guard_description, 'aok-probe': target_description}
    found = []
    for row in rows[start:ends[0]]:
        if not row:
            continue
        matches = [name for name, description in known.items()
                   if re.fullmatch(r'[>●○* \t]*' + re.escape(name) + r'\s{2,}' + re.escape(description), row)]
        if len(matches) != 1 or matches[0] in found:
            return result
        found.append(matches[0])
    result.update(guardEntryVisible=guard_name in found, targetEntryVisible='aok-probe' in found,
                  completeControlledList='default' in found and guard_name in found)
    return result


class CycleCapture(account.AccountCapture):
    def __init__(self, guard_name, guard_description, description):
        super().__init__()
        self.minimum_manager_stable_seconds = 5.0
        self.controlled = {}
        self.expected = (guard_name, guard_description, description)

    def manager_rows(self, text, root):
        self.controlled = controlled_list(text, *self.expected)
        return []


def identity(root):
    # Same filesystem identities throughout a cycle; different roots across cycles.
    return gate.digest(repr([(str(p), p.stat().st_dev, p.stat().st_ino) for p in (root, root / '.git')]).encode())

def home_identity(root):
    home = root / 'home'
    info = home.lstat()
    if home.resolve(strict=True) != home or not home.is_dir() or info.st_uid != os.getuid() or info.st_mode & 0o777 != 0o700:
        raise RuntimeError('HOME identity unsafe')
    return gate.digest(repr((str(home), info.st_dev, info.st_ino)).encode())


def validation(aok, staging, content):
    if content is None:
        return {'status': 'absent'}
    with tempfile.TemporaryDirectory(dir=staging, prefix='inspect-') as td:
        root = Path(td)
        target = root / gate.TARGET
        target.parent.mkdir(parents=True)
        target.write_bytes(content)
        (root / 'aok-native-input.json').write_text(json.dumps({'version': 'aok-native-input/v1', 'files': [
            {'path': gate.TARGET, 'format': 'markdown'}]}))
        env = {'PATH': '/usr/bin:/bin', 'HOME': str(staging / 'aok-home')}
        proc = subprocess.run([str(aok), 'agent', 'native', 'inspect', '--target', 'agy', '--scope', 'project',
                               '--input', str(root), '--json'], env=env, capture_output=True, timeout=15)
        data = json.loads(proc.stdout)
        return {'status': data.get('status'), 'exitCode': proc.returncode,
                'inputSHA256': gate.digest(content),
                'diagnosticCodes': sorted({d['code'] for d in data.get('diagnostics', [])})}


def file_hash(path):
    login._private_regular(path, max_size=1024 * 1024)
    return gate.digest(path.read_bytes())


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--aok', required=True, type=Path)
    parser.add_argument('--isolated-login-root', required=True, type=Path)
    parser.add_argument('--evidence-out', required=True, type=Path)
    for flag in ('accept-required-terms', 'disable-optional-sharing', 'trust-owned-fixtures', 'consume-isolated-profile'):
        parser.add_argument('--' + flag, required=True, action='store_true')
    args = parser.parse_args(argv)
    seed = login.AGY.validate_isolated_login_root(args.isolated_login_root)
    source = seed['root']
    fd = account.local.open_evidence_output(args.evidence_out)
    old_env, old_profile = login.env_for, login.sandbox_profile
    old_capture, old_enter = login._capture_login, login.may_press_enter
    report = {'kind': 'aok-agy-discovery-cycle/v1', 'gateContract': gate.CONTRACT, 'version': '1.4.2',
              'scope': 'project', 'binarySHA256': gate.PIN, 'provider': 'isolated-oauth-account',
              'terminalGeometry': {'columns': 160, 'rows': 80}, 'cycles': [],
              'fixtureRelativePath': gate.TARGET, 'modelInvocation': 'not-run', 'agentSelection': 'not-run',
              'nativeParserErrorReporting': 'not-verified', 'accountRowsRetained': False, 'logContentRetained': False}
    try:
        with tempfile.TemporaryDirectory(prefix='aok-agy-cycle-fixture-', dir=login.PRIVATE_TEMP_ROOT) as td:
            staging = Path(td)
            aok, rendered, _, description = login.AGY.agy.base.prepare_aok(args.aok, login.REPO, staging)
            malformed = gate.corrupt(rendered)
            nonce = secrets.token_hex(4)
            guard_name, guard_description = 'aok-cycle-guard-' + nonce, 'Synthetic cycle guard ' + nonce
            guard = ('---\nname: ' + guard_name + '\ndescription: ' + guard_description +
                     '\nmodel: flash\nsubagent: true\n---\nInert discovery control.\n').encode()
            report.update(aokSHA256=gate.digest(aok.read_bytes()), renderedFixtureUTF8=rendered.decode(),
                          malformedFixtureUTF8=malformed.decode(), guardSHA256=gate.digest(guard),
                          guardName=guard_name, guardDescription=guard_description, guardFixtureUTF8=guard.decode())
            for order in gate.ORDERS:
                root = login._owned_dir('aok-agy-session-').resolve(strict=True)
                cycle = {'phases': [], 'freshSeedCopy': False}
                report['cycles'].append(cycle)
                try:
                    marker = dict(seed['marker'], valid=False, readyPromptObserved=False)
                    login._atomic_json(root / 'owned-session.json', marker)
                    login.AGY.copy_isolated_login_home(source, root / 'home')
                    cycle['freshSeedCopy'] = True
                    cli = root / 'antigravity'
                    shutil.copyfile(login.PINNED_BINARY, cli)
                    cli.chmod(0o500)
                    if login.file_sha(cli) != gate.PIN:
                        raise RuntimeError('binary pin')
                    def env(where):
                        login.AGY.validate_isolated_login_root(source)
                        e = old_env(where)
                        e['BROWSER'] = '/usr/bin/false'
                        return e
                    login.env_for = env
                    login.sandbox_profile = lambda *a: (old_profile(*a) +
                        f'(deny file-read* (subpath {json.dumps(str(source))}))' +
                        '(deny process-exec (literal "/usr/bin/open"))' +
                        '(deny process-exec (literal "/usr/bin/osascript"))' +
                        f'(deny file-write* (subpath {json.dumps(str(root / ".agents"))}))' +
                        f'(deny file-write* (subpath {json.dumps(str(root / ".git"))}))' +
                        f'(deny file-write* (literal {json.dumps(str(root / "owned-session.json"))}))')
                    subprocess.run(['/usr/bin/git', 'init', '--quiet', '--initial-branch=main'], cwd=root,
                        env={'PATH': '/usr/bin:/bin', 'HOME': str(root / 'home'), 'GIT_CONFIG_NOSYSTEM': '1',
                             'GIT_CONFIG_GLOBAL': '/dev/null'}, check=True, capture_output=True, timeout=15)
                    cycle['workspaceIdentity'] = identity(root)
                    cycle['homeIdentity'] = home_identity(root)
                    target = root / gate.TARGET
                    guard_path = root / '.agents/agents' / guard_name / 'agent.md'
                    guard_path.parent.mkdir(parents=True)
                    guard_path.write_bytes(guard)
                    for phase in order:
                        login.AGY.validate_isolated_login_root(source)
                        content = rendered if phase in ('valid', 'repaired') else None if phase == 'absent' else malformed
                        if content is None:
                            target.unlink(missing_ok=True)
                        else:
                            target.parent.mkdir(parents=True, exist_ok=True)
                            target.write_bytes(content)
                        check = validation(aok, staging, content)
                        audit = login._owned_dir('aok-agy-cycle-audit-')
                        audit_path = audit / 'audit.jsonl'
                        for mod in (login, login.AGY, login.CURSOR):
                            mod.AUDIT_PATH = audit_path
                        sources = login._record_dependencies(audit)
                        for helper in (Path(__file__), Path(gate.__file__), Path(account.__file__)):
                            shutil.copyfile(helper, audit / helper.name)
                            sources[helper.name] = gate.digest(helper.read_bytes())
                        login.append_audit(audit_path, 'cycle-prepared', sourceHashes=sources, phase=phase)
                        login.reset_resume_sentinels(root)
                        capture = CycleCapture(guard_name, guard_description, description)
                        login._capture_login = capture.capture
                        login.may_press_enter = lambda state: False
                        login._preflight(root, audit_path, login.sandbox_profile(root, cli, '/dev/tty'))
                        result = login.run_login(root, audit_path, cli, timeout=45)
                        events = [json.loads(row) for row in audit_path.read_text().splitlines()]
                        cleaned = any(e['event'] == 'native-login-cleanup' and e.get('cleaned') is True for e in events)
                        state = dict(capture.manager or result.get('state', {}), **capture.controlled)
                        state['commandSent'] = capture.command_sent
                        item = {'phase': phase, 'workspaceIdentity': identity(root), 'homeIdentity': home_identity(root),
                            'runnerStarted': result.get('runnerStarted'), 'nativeExecAttempted': result.get('nativeExecAttempted'), 'fixtureRelativePath': gate.TARGET,
                            'inputSHA256': gate.digest(content) if content is not None else None,
                            'afterSHA256': file_hash(target) if target.exists() else None,
                            'guardSHA256': file_hash(guard_path), 'guardUnchanged': file_hash(guard_path) == gate.digest(guard),
                            'fixtureValidation': check, 'state': state, 'cleanupVerified': cleaned,
                            'managerStableSeconds': capture.manager_stable_seconds,
                            'managerObservationSeconds': capture.manager_observation_seconds,
                            'auditRoot': str(audit), 'sourceHashes': sources, 'actions': capture.actions}
                        cycle['phases'].append(item)
                        login.remove_native_log_link(root / 'home')
                        print(json.dumps({'phase': phase, 'state': state, 'fixtureValidation': check}), flush=True)
                        if not cleaned or not state.get('managerSectionObserved'):
                            raise RuntimeError('native observation incomplete')
                finally:
                    account.cleanup_owned_case(root)
                    cycle['privateRootRemoved'] = not root.exists()
    except Exception as exc:
        # Exception messages may contain native/account output. Retain only type.
        report['failureType'] = type(exc).__name__
    finally:
        login.env_for, login.sandbox_profile = old_env, old_profile
        login._capture_login, login.may_press_enter = old_capture, old_enter
        try:
            account.cleanup_owned_case(source)
            report['sourceProfileRemoved'] = not source.exists()
        finally:
            report['diagnosticCodes'] = gate.evaluate(report)
            report['candidateNativeLoadingEvidence'] = not report['diagnosticCodes'] and 'failureType' not in report
            with os.fdopen(fd, 'w') as out:
                json.dump(report, out, indent=2, sort_keys=True)
                out.write('\n')
    return 0 if report['candidateNativeLoadingEvidence'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
