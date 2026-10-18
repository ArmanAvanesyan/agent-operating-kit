#!/usr/bin/env python3
"""Verify an actual release installer in a disposable home (Linux/macOS only)."""
import argparse
import json
import os
from pathlib import Path
import platform
import subprocess
import sys
import tarfile
import tempfile
import zipfile


def extract(archive, destination):
    if archive.suffix == '.zip':
        with zipfile.ZipFile(archive) as package:
            for info in package.infolist():
                path = Path(info.filename)
                if path.is_absolute() or '..' in path.parts:
                    raise ValueError('Unsafe archive path')
            package.extractall(destination)
            for info in package.infolist():
                target = destination / info.filename
                mode = (info.external_attr >> 16) & 0o777
                if mode and target.is_file():
                    target.chmod(mode)
    else:
        with tarfile.open(archive) as package:
            for info in package.getmembers():
                path = Path(info.name)
                if path.is_absolute() or '..' in path.parts or not (info.isfile() or info.isdir()):
                    raise ValueError('Unsafe archive member')
            package.extractall(destination)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--release-dir', type=Path, required=True)
    parser.add_argument('--out', type=Path)
    args = parser.parse_args()
    source = Path(__file__).resolve().parents[2]
    suffix = ('macos-apple-silicon' if platform.machine() == 'arm64' else 'macos-intel') if sys.platform == 'darwin' else 'linux-user'
    if sys.platform not in ('darwin', 'linux'):
        parser.error('Delivery certification supports Linux/macOS only')
    archives = list(args.release_dir.resolve().glob('*-' + suffix + '-installer.*'))
    if len(archives) != 1:
        parser.error('Expected exactly one matching host installer')
    with tempfile.TemporaryDirectory(prefix='aok-delivery-installed-') as directory:
        work = Path(directory).resolve()
        extract(archives[0], work)
        installer = next(work.glob('*-installer'))
        home = work / 'home'
        home.mkdir()
        env = dict(os.environ, HOME=str(home), XDG_DATA_HOME=str(home / '.local/share'),
                   CODEX_HOME=str(home / '.codex'), AOK_DELIVERY_PYTHON=sys.executable)
        env.pop('AOK_ROOT', None)
        env.pop('PYTHONPATH', None)
        command = 'install.command' if sys.platform == 'darwin' else 'install.sh'
        subprocess.run(['bash', str(installer / command)], cwd=work, env=env, check=True, capture_output=True)
        installed = home / ('Library/Application Support/Agent Operating Kit/agent-operating-kit' if sys.platform == 'darwin' else '.local/share/agent-operating-kit/agent-operating-kit')
        empty = work / 'empty-path'
        empty.mkdir()
        subprocess.run([str(installed / 'bin/aok'), 'validate'], cwd=work,
                       env=dict(env, PATH=str(empty)), check=True, capture_output=True)
        subprocess.run([sys.executable, '-m', 'unittest', 'discover', '-s', str(source / 'tests'),
                        '-p', 'test_delivery_distribution.py', '-v'], cwd=work,
                       env=dict(env, AOK_DELIVERY_ARTIFACT=str(installed)), check=True)
        report = {'status': 'passed', 'platform': sys.platform, 'installer': suffix,
                  'existingGoValidationWithoutPython': True, 'installedDeliveryFlow': True,
                  'sourceCheckoutRequiredByInstalledHelper': False}
    if args.out:
        args.out.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, sort_keys=True))


if __name__ == '__main__':
    main()
