#!/usr/bin/env python3
"""Bounded Hermes profile discovery and loader probe on macOS.

Requires an explicit AOK binary and existing Hermes environment. Writes are
confined to a fresh temporary root; no launcher, authentication, model, or
profile-provided command is invoked.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import tempfile


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def run(command, *, root, env, timeout=30):
    root = root.resolve(strict=True)
    profile = ('(version 1)(allow default)(deny file-write*)'
               '(allow file-write* (subpath ' + json.dumps(str(root)) + ')'
               ' (literal "/dev/null") (literal "/dev/tty"))(deny network*)')
    proc = subprocess.Popen(['/usr/bin/sandbox-exec', '-p', profile, *map(str, command)],
                            env=env, cwd=root, stdin=subprocess.DEVNULL,
                            stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            start_new_session=True)
    try:
        out, err = proc.communicate(timeout=timeout)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(proc.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            proc.communicate(timeout=2)
        except subprocess.TimeoutExpired:
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            try:
                proc.communicate(timeout=5)
            except subprocess.TimeoutExpired:
                proc.stdout.close()
                proc.stderr.close()
                proc.wait(timeout=2)
        raise RuntimeError('confined probe timed out')
    return proc.returncode, out, err


def output_report(out):
    try:
        value = json.loads(out.decode('utf-8'))
    except Exception as exc:
        raise SystemExit('AOK emitted no parseable JSON report (output withheld)') from exc
    if not isinstance(value, dict):
        raise SystemExit('AOK report has an unexpected shape')
    return value


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--environment', required=True, type=Path,
                        help='installed Hermes environment containing venv/ and workspace/')
    parser.add_argument('--aok', required=True, type=Path, help='explicit built AOK CLI binary')
    args = parser.parse_args()
    envroot = args.environment.resolve(strict=True)
    aok = args.aok.resolve(strict=True)
    python = envroot / 'venv/bin/python'
    workspace = envroot / 'workspace'
    fixture_source = Path(__file__).resolve().parents[2] / 'internal/nativeadapters/hermes/testdata'
    required = [python, workspace / 'hermes_cli/main.py', workspace / 'agent/prompt_builder.py', aok]
    if any(not item.exists() for item in required):
        raise SystemExit('installed environment or AOK binary is missing required files')
    if (workspace / '.git').exists() or (workspace / 'install-stamp.json').exists():
        raise SystemExit('refusing CLI probe: installed workspace has bootstrap identity metadata')
    if not Path('/usr/bin/sandbox-exec').exists():
        raise SystemExit('Darwin sandbox-exec is required')

    with tempfile.TemporaryDirectory(prefix='aok-hermes-installed.') as tmp:
        root = Path(tmp).resolve(strict=True)
        input_dir = root / 'fixture-input'
        shutil.copytree(fixture_source, input_dir)
        import_dir = root / 'aok-import'
        rendered_dir = root / 'aok-rendered'
        aok_env = {'PATH': '/usr/bin:/bin', 'HOME': str(root / 'aok-home'), 'TMPDIR': str(root)}
        import_rc, import_out, _ = run(
            [aok, 'agent', 'native', 'import', '--target', 'hermes-agent', '--scope', 'profile',
             '--input', input_dir, '--out', import_dir, '--json'],
            root=root, env=aok_env, timeout=30)
        imported = output_report(import_out)
        bundle_path = import_dir / 'aok-native.json'
        if import_rc != 1 or imported.get('status') != 'blocked' or not bundle_path.is_file():
            raise SystemExit('AOK import did not produce the expected blocked draft (output withheld)')
        bundle = json.loads(bundle_path.read_text(encoding='utf-8'))
        if bundle.get('completeness') != 'draft-incomplete':
            raise SystemExit('AOK import bundle was not marked draft-incomplete')
        export_rc, export_out, _ = run(
            [aok, 'agent', 'native', 'export', '--target', 'hermes-agent', '--scope', 'profile',
             '--input', bundle_path, '--source', input_dir, '--out', rendered_dir, '--json'],
            root=root, env=aok_env, timeout=30)
        exported = output_report(export_out)
        if export_rc != 1 or exported.get('status') != 'blocked':
            raise SystemExit('AOK export did not remain a blocked draft (output withheld)')
        rendered = {name: rendered_dir / name for name in ('config.yaml', 'profile.yaml', 'SOUL.md')}
        if any(not path.is_file() for path in rendered.values()):
            raise SystemExit('AOK export did not materialize all three Hermes profile files')
        render_hashes = {name: sha256(path) for name, path in rendered.items()}

        positive_home = root / 'positive-home/.hermes'
        positive_home.mkdir(parents=True)
        for name, path in rendered.items():
            shutil.copyfile(path, positive_home / name)
        def hermes_env(home):
            return {'PATH': '/usr/bin:/bin', 'HOME': str(home.parent), 'TMPDIR': str(root),
                    'HERMES_HOME': str(home), 'PYTHONPATH': str(workspace),
                    'PYTHONDONTWRITEBYTECODE': '1'}

        version_rc, version_out, _ = run([python, '-B', '-m', 'hermes_cli.main', '--version'],
                                         root=root, env=hermes_env(positive_home), timeout=15)
        list_rc, listing, _ = run([python, '-B', '-m', 'hermes_cli.main', 'profile', 'list'],
                                  root=root, env=hermes_env(positive_home), timeout=30)
        listing_text = listing.decode('utf-8', 'replace')
        if version_rc or list_rc or 'Builder' not in listing_text or 'anthropic/claude-sonnet-4' not in listing_text:
            raise SystemExit('native Hermes profile list did not show the generated profile (output withheld)')
        if sha256(positive_home / 'SOUL.md') != render_hashes['SOUL.md']:
            raise SystemExit('Hermes profile-list startup changed the rendered SOUL file')

        empty_home = root / 'empty-home/.hermes'
        empty_home.mkdir(parents=True)
        empty_rc, empty_out, _ = run([python, '-B', '-m', 'hermes_cli.main', 'profile', 'list'],
                                     root=root, env=hermes_env(empty_home), timeout=30)
        empty_text = empty_out.decode('utf-8', 'replace')
        expected_model = 'anthropic/claude-sonnet-4'
        if empty_rc or 'default' not in empty_text.lower() or '—' not in empty_text or expected_model in empty_text:
            raise SystemExit('empty-profile control did not show the expected default with no model (output withheld)')

        malformed_home = root / 'malformed-home/.hermes'
        malformed_home.mkdir(parents=True)
        shutil.copyfile(rendered['profile.yaml'], malformed_home / 'profile.yaml')
        shutil.copyfile(rendered['SOUL.md'], malformed_home / 'SOUL.md')
        (malformed_home / 'config.yaml').write_text('model:\n  default: [broken\n', encoding='utf-8')
        malformed_rc, malformed_out, malformed_err = run(
            [python, '-B', '-m', 'hermes_cli.main', 'profile', 'list'],
            root=root, env=hermes_env(malformed_home), timeout=30)
        malformed_text = (malformed_out + malformed_err).decode('utf-8', 'replace')
        warning_prefixes = ('your settings file (', 'has a formatting error')
        fallback_phrase = 'hermes is running on default settings until it is fixed'
        if (malformed_rc or 'default' not in malformed_text.lower() or '—' not in malformed_text
                or expected_model in malformed_text
                or not all(phrase in malformed_text.lower() for phrase in warning_prefixes)
                or fallback_phrase not in malformed_text.lower()):
            raise SystemExit('malformed-config control lacked expected fallback and parse warning (output withheld)')

        component_code = r'''import hashlib,json,os
from pathlib import Path
from agent.prompt_builder import load_soul_md
from hermes_cli.profiles import read_profile_meta
fixture=Path(os.environ['HERMES_FIXTURE'])
body=(fixture/'SOUL.md').read_text(encoding='utf-8').strip()
loaded=load_soul_md(home_override=fixture)
meta=read_profile_meta(fixture)
missing=fixture.parent/'missing'
missing_soul=load_soul_md(home_override=missing)
missing_meta=read_profile_meta(missing)
actual=hashlib.sha256((loaded or '').encode()).hexdigest()
print(json.dumps({'loadedMatchesFixture':loaded==body,'loadedSha256':actual,'description':meta.get('description'),'missingSoulReturnsNone':missing_soul is None,'missingDescriptionEmpty':missing_meta.get('description','')==''}))
if loaded!=body or meta.get('description')!='Focused software engineering assistant.' or missing_soul is not None or missing_meta.get('description','')!='': raise SystemExit(1)'''
        component_env = dict(hermes_env(positive_home), HERMES_FIXTURE=str(positive_home))
        loader_rc, loader_out, _ = run([python, '-B', '-c', component_code], root=root,
                                       env=component_env, timeout=30)
        if loader_rc:
            raise SystemExit('installed profile metadata or SOUL loader control failed (output withheld)')
        loader_result = json.loads(loader_out.decode('utf-8'))
        if loader_result.get('loadedMatchesFixture') is not True or loader_result.get('description') != 'Focused software engineering assistant.' or loader_result.get('missingSoulReturnsNone') is not True or loader_result.get('missingDescriptionEmpty') is not True:
            raise SystemExit('installed profile metadata or SOUL loader result mismatch')

        print(json.dumps({
            'cliVersionLine': version_out.decode('utf-8', 'replace').splitlines()[0].strip(),
            'aokBinarySha256': sha256(aok),
            'aokImportStatus': imported.get('status'),
            'bundleCompleteness': bundle.get('completeness'),
            'aokExportStatus': exported.get('status'),
            'aokRenderedFileHashes': render_hashes,
            'renderedFileSha256': render_hashes,
            'positiveProfileList': 'Builder and configured model found',
            'emptyControl': 'default profile listed with no model',
            'malformedControl': 'fallback/default listed and parse/config warning found',
            'profileMetadataComponent': 'expected description loaded; missing profile returned empty description',
            'soulComponent': 'expected body hash loaded; missing SOUL returned None',
            'profileListConsumedPurposeOrSoul': False,
            'network': 'denied', 'writesOutsideTempRoot': 'denied',
            'modelInvocation': 'not-run', 'runtimeEnforcement': 'unverified',
            'launcherSelectionEquivalence': 'unverified',
        }, sort_keys=True))


if __name__ == '__main__':
    main()
