from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]


def load(name, relative):
    spec = importlib.util.spec_from_file_location(name, ROOT / relative)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


inventory = load('inventory', 'skills/project-exploration/scripts/inventory.py')
structure = load('structure', 'skills/project-exploration/scripts/structure.py')
readiness = load('readiness', 'skills/environment-readiness/scripts/assess.py')


class ExplorationTests(unittest.TestCase):
    def test_inventory_limits_hidden_config_and_no_symlink_following(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / '.github/workflows').mkdir(parents=True)
            (root / '.github/workflows/ci.yml').write_text('name: ci')
            (root / 'src/deep').mkdir(parents=True)
            (root / 'src/main.py').write_text('print(1)')
            (root / 'src/deep/hidden.py').write_text('print(2)')
            (root / 'node_modules').mkdir()
            (root / 'node_modules/vendor.js').write_text('secret')
            (root / 'link.py').symlink_to(root / 'src/main.py')
            result = inventory.inventory(root, max_depth=2)
            files = {x['path'] for x in result['files']}
            self.assertIn('.github/workflows/ci.yml', files)
            self.assertNotIn('link.py', files)
            self.assertNotIn('node_modules/vendor.js', files)
            shallow = inventory.inventory(root, max_depth=1)
            self.assertNotIn('src/deep/hidden.py', {x['path'] for x in shallow['files']})
            limited = inventory.inventory(root, max_entries=2)
            self.assertEqual(limited['coverage'], 'partial')
            self.assertIn('entry-budget', limited['omissions'])
            self.assertLessEqual(len(inventory.inventory(root, max_files=1)['files']), 1)

    def test_missing_backend_does_not_claim_empty_success(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'a.py').write_text('def example(): pass')
            with patch.object(structure.shutil, 'which', return_value=None):
                report = structure.query(root, 'a.py', 'pattern', '$A()', 'python')
            self.assertEqual(report['status'], 'unavailable')
            self.assertNotIn('evidence', report)

    def test_ast_patterns_exclude_comments_in_supported_pilot_languages(self):
        if not structure.shutil.which('ast-grep'):
            self.skipTest('ast-grep unavailable')
        examples = [
            ('example.py', 'python', 'target(1)\n# target(2)\n'),
            ('example.go', 'go', 'package main\nfunc main() { target(1) } // target(2)\n'),
            ('example.ts', 'typescript', 'target(1); // target(2)\n'),
        ]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name, language, source in examples:
                with self.subTest(language=language):
                    (root / name).write_text(source)
                    report = structure.query(root, name, 'pattern', 'target($$$ARGS)', language)
                    self.assertEqual(report['status'], 'observed', report)
                    self.assertEqual(len(report['evidence']), 1)
                    self.assertEqual(report['evidence'][0]['file'], name)
                    self.assertEqual((root / name).read_text(), source)

    def test_tokei_stats_preserve_original_filename(self):
        if not structure.shutil.which('tokei'):
            self.skipTest('tokei unavailable')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'example.py').write_text('print(1)\n')
            report = structure.query(root, 'example.py', 'stats')
            self.assertEqual(report['status'], 'observed', report)
            self.assertIn('Python', report['evidence'])
            self.assertEqual(len(report['sourceSha256']), 64)
            self.assertNotIn('aok-explore-', json.dumps(report['evidence']))

    def test_extensionless_entrypoints_require_known_shebang(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            cases = [('cli', '#!/usr/bin/env python3\nprint(1)\n', '.py'),
                     ('setup', '#!/bin/sh\necho ready\n', '.sh')]
            for name, source, suffix in cases:
                path = root / name
                path.write_text(source)
                self.assertEqual(structure.selected_file(root, name, 100), path.resolve())
                self.assertEqual(structure.source_suffix(path), suffix)
            for name, source in [('unknown', 'print(1)'), ('.env', '#!/bin/sh\nSECRET=value'),
                                 ('config.txt', '#!/bin/sh\necho x')]:
                (root / name).write_text(source)
                with self.assertRaises(ValueError):
                    structure.selected_file(root, name, 100)

    def test_extensionless_query_uses_staged_language_suffix(self):
        if not structure.shutil.which('rg'):
            self.skipTest('ripgrep unavailable')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / 'cli'
            original = '#!/usr/bin/env python3\nprint(1)\n'
            path.write_text(original)
            report = structure.query(root, 'cli', 'text', 'print')
            self.assertEqual(report['status'], 'observed', report)
            self.assertEqual(report['evidence'][0]['path']['text'], 'cli')
            self.assertEqual(path.read_text(), original)

    def test_source_scope_rejects_symlinks_traversal_and_size(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'a.py').write_text('print(1)')
            (root / 'b.py').symlink_to(root / 'a.py')
            (root / '.env').write_text('SECRET=do-not-read')
            for relative in ['../a.py', 'b.py', '.env']:
                with self.subTest(relative=relative), self.assertRaises(ValueError):
                    structure.selected_file(root, relative, 100)
            with self.assertRaises(ValueError):
                structure.selected_file(root, 'a.py', 1)

    def test_process_limits_timeout_and_output(self):
        result = structure.run_bounded([sys.executable, '-c', 'print("x" * 100000)'], '.', limit=100)
        self.assertTrue(result['outputLimited'])
        self.assertLessEqual(len(result['stdout']), 100)
        result = structure.run_bounded([sys.executable, '-c', 'import time; time.sleep(3)'], '.', timeout=0.05)
        self.assertTrue(result['timedOut'])

    def test_invalid_backend_json_and_unsupported_outline(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'a.py').write_text('print(1)')
            version = {'stdout': 'fake 1', 'returncode': 0}
            invalid = {'stdout': 'bad-json', 'returncode': 0, 'timedOut': False, 'outputLimited': False}
            with patch.object(structure.shutil, 'which', side_effect=lambda name: 'fake' if name != 'git' else None), patch.object(
                    structure, 'run_bounded', side_effect=[version, invalid]):
                report = structure.query(root, 'a.py', 'stats')
            self.assertEqual(report['reason'], 'invalid-json-output')
            with patch.object(structure.shutil, 'which', side_effect=lambda name: 'fake' if name != 'git' else None), patch.object(
                    structure, 'run_bounded', side_effect=[version, {'returncode': 2}]):
                report = structure.query(root, 'a.py', 'outline', language='python')
            self.assertEqual(report['reason'], 'outline-not-supported')

    def test_text_backend_extracts_matches_and_preserves_source(self):
        if not structure.shutil.which('rg'):
            self.skipTest('ripgrep unavailable')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            path = root / 'a.py'
            original = 'def example():\n    return 1\n'
            path.write_text(original)
            report = structure.query(root, 'a.py', 'text', 'example')
            self.assertEqual(report['status'], 'observed')
            self.assertEqual(len(report['evidence']), 1)
            self.assertEqual(report['evidence'][0]['path']['text'], 'a.py')
            self.assertEqual(path.read_text(), original)
            self.assertEqual(structure.query(root, 'a.py', 'text', 'absent')['evidence'], [])


class ReadinessTests(unittest.TestCase):
    def report(self, profile='inspection'):
        dimensions = ['context'] if profile == 'inspection' else ['context', 'verification', 'feedback']
        return {'task': 'Inspect project', 'profile': profile, 'revision': 'abc',
                'environment': 'fixture', 'checks': [
                    {'id': x, 'dimension': x, 'required': True, 'outcome': 'pass',
                     'evidence': ['fixture evidence']} for x in dimensions]}

    def test_ready_requires_actual_required_records_and_revision_match(self):
        report = self.report('local-development')
        self.assertEqual(readiness.assess(report, 'abc')['verdict'], 'ready')
        self.assertEqual(readiness.assess(report, 'changed')['verdict'], 'blocked')
        report['checks'].pop()
        self.assertEqual(readiness.assess(report)['verdict'], 'blocked')
        report = self.report()
        report['checks'][0]['evidence'] = []
        self.assertEqual(readiness.assess(report)['verdict'], 'blocked')

    def test_required_failure_unexecuted_and_na_never_ready(self):
        for outcome, verdict in [('fail', 'not-ready'), ('blocked', 'blocked'),
                                 ('not-checked', 'blocked'), ('not-applicable', 'blocked')]:
            report = self.report()
            report['checks'][0].update(outcome=outcome, rationale='Not available')
            self.assertEqual(readiness.assess(report)['verdict'], verdict)

    def test_optional_failures_are_retained_and_invalid_records_rejected(self):
        report = self.report()
        report['checks'].append({'id': 'browser', 'dimension': 'verification', 'required': False,
                                 'outcome': 'fail', 'evidence': []})
        self.assertEqual(readiness.assess(report)['verdict'], 'ready')
        self.assertEqual(len(readiness.assess(report)['checks']), 2)
        report['checks'][0]['required'] = 'yes'
        with self.assertRaises(ValueError):
            readiness.assess(report)
        with self.assertRaises(ValueError):
            readiness.assess(self.report('unknown'))
        for malformed in [{'profile': []}, {'profile': None}]:
            with self.assertRaises(ValueError):
                readiness.assess(malformed)
        report = self.report()
        report['checks'] *= 2
        with self.assertRaises(ValueError):
            readiness.assess(report)

    def test_cli_returns_nonzero_for_unexecuted_check_without_running_it(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            report = self.report()
            report['checks'][0]['outcome'] = 'not-checked'
            report['checks'][0]['evidence'] = ['touch SHOULD_NOT_EXIST']
            path = root / 'report.json'
            path.write_text(json.dumps(report))
            result = subprocess.run([sys.executable, str(ROOT / 'skills/environment-readiness/scripts/assess.py'),
                                     str(path)], capture_output=True, text=True, cwd=root)
            self.assertEqual(result.returncode, 1)
            self.assertEqual(json.loads(result.stdout)['verdict'], 'blocked')
            self.assertFalse((root / 'SHOULD_NOT_EXIST').exists())


if __name__ == '__main__':
    unittest.main()
