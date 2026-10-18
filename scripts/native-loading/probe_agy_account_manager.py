#!/usr/bin/env python3
"""Compare three native /agents controls using a fresh private account profile.

No native logs, account rows, agent selection or model prompt are retained/sent.
The input profile must be AOK-owned, unexpired and prompt-ready. Copies are
removed per case; the original remains under its unchanged expiry for cleanup.
"""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import tempfile

SPEC = importlib.util.spec_from_file_location(
    'agy_account_manager_local', Path(__file__).with_name('probe_agy_local_manager.py'))
local = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(local)
login = local.login


class AccountCapture(local.CaseCapture):
    def manager_rows(self, text, root):
        # An account prompt can include private identity; retain booleans only.
        return []

    def diagnostics(self, root):
        # Synthetic log inspection is expressly forbidden for account mode.
        return []


def cleanup_owned_case(root):
    """Invalidate without following native-written links; always remove the copy."""
    root = Path(root)
    if not root.exists():
        return
    info = root.lstat()
    if (root.resolve(strict=True) != root or not stat.S_ISDIR(info.st_mode) or
            info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700):
        raise RuntimeError('case cleanup root ownership changed')
    try:
        fd = os.open(root / 'owned-session.json', os.O_WRONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        try:
            marker = os.fstat(fd)
            if not stat.S_ISREG(marker.st_mode) or marker.st_uid != os.getuid() or marker.st_nlink != 1:
                raise RuntimeError('unsafe cleanup marker')
            os.ftruncate(fd, 0)
            os.write(fd, b'{"valid":false,"readyPromptObserved":false}\n')
        finally:
            os.close(fd)
    finally:
        shutil.rmtree(root)
    if root.exists():
        raise RuntimeError('case root cleanup incomplete')


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--aok', required=True, type=Path)
    parser.add_argument('--isolated-login-root', required=True, type=Path)
    parser.add_argument('--evidence-out', required=True, type=Path)
    parser.add_argument('--accept-required-terms', required=True, action='store_true')
    parser.add_argument('--disable-optional-sharing', required=True, action='store_true')
    parser.add_argument('--trust-owned-fixtures', required=True, action='store_true')
    args = parser.parse_args(argv)
    source = login.AGY.validate_isolated_login_root(args.isolated_login_root)['root']
    try:
        fd = local.open_evidence_output(args.evidence_out)
    except (ValueError, OSError):
        parser.error('evidence must be a new file in canonical nonsymlink checkout directories')
    original_env, original_profile = login.env_for, login.sandbox_profile
    original_capture, original_enter = login._capture_login, login.may_press_enter
    cases = {}
    try:
        with tempfile.TemporaryDirectory(prefix='aok-agy-account-fixture-', dir=login.PRIVATE_TEMP_ROOT) as td:
            owned_aok, rendered, fixture_hash, description = login.AGY.agy.base.prepare_aok(args.aok, login.REPO, Path(td))
            aok_hash = hashlib.sha256(owned_aok.read_bytes()).hexdigest()
            for case in ('positive', 'absent', 'malformed'):
                capture = AccountCapture()
                copy_metadata = {}
                copied_root = None

                def env(root):
                    nonlocal copy_metadata, copied_root
                    login.AGY.validate_isolated_login_root(source)
                    if copied_root is None:
                        copied_root = Path(root)
                        copy_metadata = login.AGY.copy_isolated_login_home(source, Path(root) / 'home')
                    elif copied_root != Path(root):
                        raise RuntimeError('unexpected root changed within account case')
                    e = original_env(root)
                    e['BROWSER'] = '/usr/bin/false'
                    subprocess.run(['/usr/bin/git', 'init', '--quiet', '--initial-branch=main'], cwd=root,
                        env={'PATH': '/usr/bin:/bin', 'HOME': e['HOME'],
                             'GIT_CONFIG_NOSYSTEM': '1', 'GIT_CONFIG_GLOBAL': '/dev/null'},
                        capture_output=True, check=True, timeout=15)
                    fixture = Path(root) / '.agents/agents/aok-probe/agent.md'
                    if case != 'absent':
                        fixture.parent.mkdir(parents=True, exist_ok=True)
                        fixture.write_bytes(rendered if case == 'positive' else b'---\nname: aok-probe\ndescription: [\n---\n')
                    return e

                login.env_for = env
                login.sandbox_profile = lambda *a: (original_profile(*a) +
                    f'(deny file-read* (subpath {json.dumps(str(source))}))' +
                    '(deny process-exec (literal "/usr/bin/open"))' +
                    '(deny process-exec (literal "/usr/bin/osascript"))')
                login._capture_login = capture.capture
                login.may_press_enter = lambda state: False
                try:
                    result = login.prepare(timeout=35)
                    events = [json.loads(row) for row in (Path(result['auditRoot']) / 'audit.jsonl').read_text().splitlines()]
                    cleaned = any(e['event'] == 'native-login-cleanup' and e.get('cleaned') is True for e in events)
                    state = dict(capture.manager or result.get('observation', {}).get('state', {}))
                    state['commandSent'] = capture.command_sent
                finally:
                    if copied_root is not None:
                        cleanup_owned_case(copied_root)
                root = Path(result['root'])
                cases[case] = {'state': state, 'prompt': capture.prompt, 'actions': capture.actions,
                               'cleanupVerified': cleaned, 'privateRootRemoved': not root.exists(),
                               'profileCopy': copy_metadata, 'auditRoot': result['auditRoot'],
                               'accountRowsRetained': False, 'nativeLogsRead': False}
                print(json.dumps({'case': case, 'commandSent': capture.command_sent,
                                  'managerObserved': state.get('managerSectionObserved'),
                                  'nameObserved': state.get('nameObserved')}), flush=True)
        qualified = all(v['cleanupVerified'] and v['privateRootRemoved'] for v in cases.values()) and login.AGY.has_candidate_manager_evidence({k: v['state'] for k, v in cases.items()})
        report = {'kind': 'aok-agy-account-manager/v1', 'version': '1.4.2', 'scope': 'project',
                  'sourceSHA256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                  'binarySHA256': login.PINNED_BINARY_SHA256, 'aokSHA256': aok_hash,
                  'fixtureSHA256': fixture_hash, 'renderedFixtureUTF8': rendered.decode(),
                  'expectedDescription': description, 'cases': cases,
                  'provider': 'fresh isolated OAuth account profile; not synthetic API key',
                  'network': 'allowed for native metadata and refresh; original home/keychain denied',
                  'backendAuthentication': 'not-independently-verified', 'inference': 'not-run',
                  'nativeLogsRead': False, 'accountRowsRetained': False,
                  'minimumManagerObservationSeconds': 5, 'candidateNativeLoadingEvidence': qualified}
        with os.fdopen(fd, 'w') as out:
            fd = None
            json.dump(report, out, indent=2, sort_keys=True)
            out.write('\n')
        return 0 if qualified else 1
    finally:
        login.env_for, login.sandbox_profile = original_env, original_profile
        login._capture_login, login.may_press_enter = original_capture, original_enter
        if fd is not None:
            os.close(fd)


if __name__ == '__main__':
    raise SystemExit(main())
