"""Python renderer parity: entrypoint links, safe support trees and relocation."""
import importlib.util
from importlib.machinery import SourceFileLoader
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
loader = SourceFileLoader('aok_resource_pack_test', str(ROOT / 'scripts/aok'))
spec = importlib.util.spec_from_loader(loader.name, loader)
aok = importlib.util.module_from_spec(spec)
sys.modules[loader.name] = aok
loader.exec_module(aok)


class ResourcePackTests(unittest.TestCase):
    def test_all_targets_resolve_resources_after_relocation(self):
        with tempfile.TemporaryDirectory() as directory:
            out = Path(directory) / 'first'
            for target in ('codex', 'claude-code', 'cursor', 'opencode', 'gemini', 'openclaw', 'hermes-agent'):
                bundle = aok.render_target_bundle('repository-delivery', target, out)
                relocated = (Path(directory) / ('relocated-' + target)).resolve()
                shutil.move(str(bundle), relocated)
                launcher = relocated / 'sources/packs/repository-delivery/scripts/repo-delivery'
                p = subprocess.run([sys.executable, '-I', str(launcher), 'inspect', '--help'],
                                   cwd=directory, capture_output=True, text=True)
                self.assertEqual(p.returncode, 0, p.stderr)
                if target == 'codex':
                    plugin = json.loads((relocated / '.codex-plugin/plugin.json').read_text())
                    self.assertIsInstance(plugin['skills'], str)
                    self.assertTrue((relocated / plugin['skills']).is_dir())
                for hostdir in ('skills', '.cursor/skills'):
                    for wrapper in (relocated / hostdir).glob('*/SKILL.md'):
                        links = re.findall(r'\]\(([^)]+)\)', wrapper.read_text())
                        self.assertTrue(links)
                        for link in links:
                            guide = (wrapper.parent / link).resolve()
                            self.assertTrue(guide.is_file(), str(guide))
                            self.assertTrue(guide.is_relative_to(relocated))
                metadata = json.loads((relocated / 'aok-bundle.json').read_text())
                for path in metadata['files']:
                    self.assertTrue((relocated / path).exists(), (target, path))

    def test_ship_pipeline_includes_canonical_support(self):
        with tempfile.TemporaryDirectory() as directory:
            bundle = aok.render_target_bundle('ship-pipeline', 'cursor', Path(directory))
            self.assertTrue((bundle / 'sources/packs/repository-delivery/scripts/repo-delivery').is_file())
            for path in json.loads((bundle / 'aok-bundle.json').read_text())['files']:
                self.assertTrue((bundle / path).exists(), path)

    def test_unsafe_skill_roots_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            (root / 'skills').mkdir()
            (root / 'alias').symlink_to(root / 'skills')
            for rel in ('', '.', '..', '../outside', '/absolute', 'a\\b', 'C:/outside', 'alias'):
                self.assertFalse(aok._valid_skill_root(root, rel), rel)
            self.assertTrue(aok._valid_skill_root(root, 'skills'))

    def test_support_copy_filters_cache_and_refuses_symlinks(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            base = root / 'pack'
            base.mkdir()
            (base / 'safe').write_text('safe')
            (base / '.coverage').write_text('private cache')
            (base / '__pycache__').mkdir()
            (base / '__pycache__/ignored.pyc').write_bytes(b'cache')
            bundle = root / 'bundle'
            copied = []
            original_root = aok.KIT_ROOT
            aok.KIT_ROOT = root
            self.addCleanup(setattr, aok, "KIT_ROOT", original_root)
            aok.copy_bundle_source(base, '.', bundle, copied)
            self.assertFalse((bundle / 'sources/pack/.coverage').exists())
            self.assertFalse((bundle / 'sources/pack/__pycache__').exists())
            (base / 'escape').symlink_to(root / 'outside')
            with self.assertRaises(aok.AokError):
                aok.copy_bundle_source(base, '.', root / 'bad', [])

    def test_guide_reference_escape_is_rejected_before_rendering(self):
        import copy
        from unittest.mock import patch
        manifest, pack = aok.load_pack_manifest('repository-delivery')
        content, path = aok.load_pack_content('repository-delivery', pack, manifest)
        content = copy.deepcopy(content)
        content['skills'][0]['sourcePath'] = '../../../../outside'
        with tempfile.TemporaryDirectory() as directory:
            with patch.object(aok, 'load_pack_content', return_value=(content, path)):
                with self.assertRaises(aok.AokError):
                    aok.render_target_bundle('repository-delivery', 'cursor', Path(directory))
