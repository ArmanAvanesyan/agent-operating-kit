import contextlib
import io
import json
from pathlib import Path
import subprocess
import sys
from unittest.mock import patch

import jsonschema
from repo_delivery.cli import main
from repo_delivery.core import inspect, compare, create_checkpoint, verify_checkpoint, DeliveryError
from test_core import RepositoryFixture

ROOT = Path(__file__).resolve().parents[1]

class ContractTests(RepositoryFixture):
    def validate(self, name, value):
        schema = json.loads((ROOT / 'config/schemas' / (name + '.json')).read_text())
        jsonschema.Draft202012Validator.check_schema(schema)
        jsonschema.validate(value, schema)
        invalid = dict(value, schema_version=999)
        with self.assertRaises(jsonschema.ValidationError):
            jsonschema.validate(invalid, schema)

    def test_actual_evidence_matches_schemas(self):
        self.validate('inventory', inspect(self.repo))
        self.validate('comparison', compare(self.repo, 'HEAD'))
        out = self.root / 'cp'
        self.validate('preservation-receipt', create_checkpoint(self.repo, out))
        self.validate('checkpoint', json.loads((out / 'manifest.json').read_text()))

    def test_cli_operations(self):
        for args in [
            ['inspect', '--repo', str(self.repo)],
            ['compare', '--repo', str(self.repo), '--baseline', 'HEAD'],
            ['checkpoint', 'create', '--repo', str(self.repo), '--output', str(self.root / 'cp')],
            ['checkpoint', 'verify', str(self.root / 'cp')],
        ]:
            with contextlib.redirect_stdout(io.StringIO()) as output:
                self.assertEqual(main(args), 0)
                self.assertEqual(json.loads(output.getvalue())['schema_version'], 1)
        with contextlib.redirect_stderr(io.StringIO()) as output:
            self.assertEqual(main(['inspect', '--repo', str(self.root)]), 1)
            self.assertIn('error', json.loads(output.getvalue()))
        proc = subprocess.run([sys.executable, str(ROOT/'scripts/repo-delivery'), '--help'], capture_output=True)
        self.assertEqual(proc.returncode, 0)

    def test_source_change_aborts_without_publishing(self):
        from repo_delivery import core
        original = core.source_snapshot
        calls = 0
        def changed(*args):
            nonlocal calls
            calls += 1
            if calls == 2:
                (self.repo / 'a').write_text('concurrent edit')
            return original(*args)
        with patch.object(core, 'source_snapshot', changed):
            with self.assertRaisesRegex(DeliveryError, 'Source changed'):
                create_checkpoint(self.repo, self.root / 'cp')
        self.assertFalse((self.root / 'cp').exists())

    def test_comparison_local_file_already_present_on_baseline(self):
        self.git('branch', 'before')
        (self.repo / 'future').write_text('already merged')
        self.git('add', '.')
        self.git('commit', '-m', 'future')
        self.git('checkout', 'before')
        (self.repo / 'future').write_text('already merged')
        states = {f['path']: f['state'] for f in compare(self.repo, 'main')['files']}
        self.assertEqual(states['future'], 'identical')

    def test_sparse_and_invalid_baseline(self):
        self.git('config', 'core.sparseCheckout', 'true')
        with self.assertRaisesRegex(DeliveryError, 'Sparse'):
            create_checkpoint(self.repo, self.root / 'sparse')
        with self.assertRaises(DeliveryError):
            compare(self.repo, '--help')

    def test_unsafe_archive_path(self):
        from repo_delivery.core import safe_path
        for name in ['../x', '/absolute', '.git/config', 'a/../../b', 'a//b']:
            with self.assertRaises(DeliveryError):
                safe_path(name)

    def test_malformed_archive(self):
        cp = self.root / 'cp'
        cp.mkdir()
        (cp/'manifest.json').write_text('not json')
        with self.assertRaises(DeliveryError):
            verify_checkpoint(cp)

    def test_repository_path_whitespace(self):
        destination = self.root / ' repository with spaces '
        self.repo.rename(destination)
        self.repo = destination
        self.assertEqual(inspect(self.repo)['repository'], str(destination.resolve()))
        create_checkpoint(self.repo, self.root / 'cp')
        verify_checkpoint(self.root / 'cp')

    def test_symlink_record_checksum_mismatch(self):
        from repo_delivery.core import canonical, digest
        (self.repo / 'link').symlink_to('/outside')
        cp = self.root / 'cp'
        create_checkpoint(self.repo, cp)
        manifest = json.loads((cp/'manifest.json').read_bytes())
        next(r for r in manifest['files'] if r['path'] == 'link')['sha256'] = '0' * 64
        (cp/'manifest.json').write_bytes(canonical(manifest))
        receipt = {'schema_version': 1, 'kind': 'preservation-receipt', 'manifest_sha256': digest(canonical(manifest))}
        (cp/'receipt.json').write_bytes(canonical(receipt))
        with self.assertRaises(DeliveryError):
            verify_checkpoint(cp)
