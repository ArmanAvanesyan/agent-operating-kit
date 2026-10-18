import json
import os
import subprocess
import sys

from test_core import RepositoryFixture
from repo_delivery.core import DeliveryError
from repo_delivery.evidence import new_context, run_gates
from repo_delivery.hooks import check_hook, install_hook


class HookTests(RepositoryFixture):
    def setUp(self):
        super().setUp()
        self.root = self.root.resolve()
        self.repo = self.repo.resolve()

    def config(self):
        directory = self.root / "private 'hooks; $(touch injected)"
        directory.mkdir(mode=0o700)
        spec = {'objective':'fixture', 'files_in_scope':['a'],
                'gate_commands':[[sys.executable,'-c','pass']], 'review_policy':'none',
                'authorized_actions':['push'], 'actor_id':'actor', 'actor_context':'context'}
        context = new_context(self.repo,spec,'HEAD')
        self.write(directory/'context.json',context)
        self.write(directory/'gates.json',run_gates(context))
        config = {'schema_version':1,'kind':'git-pre-push-config','repository':str(self.repo),
                  'context':str(directory/'context.json'),'gates':str(directory/'gates.json'),
                  'review':None,'fail_mode':'closed'}
        path=directory/'hook.json'
        self.write(path,config)
        return path

    def write(self,path,value):
        path.write_text(json.dumps(value))
        path.chmod(0o600)

    def test_installed_hook_checks_only_and_blocks_stale_evidence(self):
        config=self.config()
        before=self.git('status','--porcelain=v1','-z')
        receipt=install_hook(self.repo,config,['install-hook'])
        hook=self.repo/'.git/hooks/pre-push'
        self.assertEqual(receipt['hook'],str(hook))
        result=subprocess.run([str(hook)],cwd=self.repo,capture_output=True,
                              input=b'refs/heads/main '+self.git('rev-parse','HEAD').strip()+b' refs/heads/main '+b'0'*40+b'\n')
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertEqual(before,self.git('status','--porcelain=v1','-z'))
        self.assertFalse((self.repo/'injected').exists())
        (self.repo/'a').write_text('changed')
        result=subprocess.run([str(hook)],cwd=self.repo,capture_output=True,
                              input=b'refs/heads/main '+self.git('rev-parse','HEAD').strip()+b' refs/heads/main '+b'0'*40+b'\n')
        self.assertNotEqual(result.returncode,0)
        self.assertIn(b'clean',result.stderr)

    def test_authorization_existing_hook_and_custom_hook_path(self):
        config=self.config()
        with self.assertRaisesRegex(DeliveryError,'author'):
            install_hook(self.repo,config,[])
        hook=self.repo/'.git/hooks/pre-push'
        hook.write_text('keep')
        with self.assertRaisesRegex(DeliveryError,'existing'):
            install_hook(self.repo,config,['install-hook'])
        self.assertEqual(hook.read_text(),'keep')
        hook.unlink()
        self.git('config','core.hooksPath','custom')
        with self.assertRaisesRegex(DeliveryError,'hooksPath'):
            install_hook(self.repo,config,['install-hook'])

    def test_private_external_config_and_symlinks_required(self):
        config=self.config()
        config.chmod(0o644)
        with self.assertRaisesRegex(DeliveryError,'private'):
            check_hook(config)
        config.chmod(0o600)
        link=config.parent/'link'
        link.symlink_to(config)
        with self.assertRaisesRegex(DeliveryError,'symlink'):
            check_hook(link)
        inside=self.repo/'hook.json'
        self.write(inside,json.loads(config.read_text()))
        with self.assertRaisesRegex(DeliveryError,'outside'):
            install_hook(self.repo,inside,['install-hook'])

    def test_missing_invalid_or_unauthorized_evidence_fails_closed(self):
        config=self.config()
        record=json.loads(config.read_text())
        record['fail_mode']='open'
        self.write(config,record)
        with self.assertRaisesRegex(DeliveryError,'configuration'):
            check_hook(config)
        record['fail_mode']='closed'
        self.write(config,record)
        (config.parent/'gates.json').unlink()
        with self.assertRaises(DeliveryError):
            check_hook(config)

    def test_hook_directory_symlink_refused(self):
        config=self.config()
        hooks=self.repo/'.git/hooks'
        import shutil
        shutil.rmtree(hooks)
        target=self.root/'elsewhere'
        target.mkdir()
        hooks.symlink_to(target,target_is_directory=True)
        with self.assertRaisesRegex(DeliveryError,'symlink'):
            install_hook(self.repo,config,['install-hook'])

    def test_push_updates_require_current_head(self):
        config=self.config()
        install_hook(self.repo,config,['install-hook'])
        hook=self.repo/'.git/hooks/pre-push'
        for updates in (b'', b'refs/heads/other '+b'0'*40+b' refs/heads/other '+b'0'*40+b'\n'):
            result=subprocess.run([str(hook)],cwd=self.repo,capture_output=True,input=updates)
            self.assertNotEqual(result.returncode,0)
            self.assertIn(b'current HEAD',result.stderr)

    def test_push_authority_is_not_inferred(self):
        config=self.config()
        spec={'objective':'fixture','files_in_scope':['a'],'gate_commands':[[sys.executable,'-c','pass']],
              'review_policy':'none','authorized_actions':[], 'actor_id':'actor','actor_context':'ctx'}
        context=new_context(self.repo,spec,'HEAD')
        self.write(config.parent/'context.json',context)
        self.write(config.parent/'gates.json',run_gates(context))
        with self.assertRaisesRegex(DeliveryError,'authorization'):
            check_hook(config)

    def test_invalid_json_and_linked_worktree_installation(self):
        config=self.config()
        config.write_text('{')
        with self.assertRaisesRegex(DeliveryError,'Cannot read'):
            check_hook(config)
        config.unlink()
        config.parent.rmdir() if not list(config.parent.iterdir()) else None

    def test_linked_worktree_installation_refused(self):
        linked=self.root/'linked'
        self.git('worktree','add','-b','linked',str(linked))
        self.repo=linked
        config=self.config()
        with self.assertRaisesRegex(DeliveryError,'Linked worktree'):
            install_hook(self.repo,config,['install-hook'])

    def test_bad_evidence_and_changed_repository_are_rejected(self):
        config=self.config()
        record=json.loads(config.read_text())
        record['review']=str(config.parent/'nonexistent')
        self.write(config,record)
        with self.assertRaises(DeliveryError):
            check_hook(config)
        record['review']=None
        record['repository']=str(self.root)
        self.write(config,record)
        with self.assertRaises(DeliveryError):
            check_hook(config)

    def test_fresh_dirty_evidence_cannot_certify_push_head(self):
        for mode in ('working', 'staged', 'untracked'):
            with self.subTest(mode=mode):
                directory=self.root/('evidence-'+mode)
                directory.mkdir(mode=0o700)
                if mode == 'untracked':
                    (self.repo/'a').write_text('initial\n')
                    (self.repo/'new').write_text('new')
                else:
                    (self.repo/'a').write_text('changed')
                    if mode == 'staged':
                        self.git('add','a')
                spec={'objective':'fixture','files_in_scope':['a','new'],
                      'gate_commands':[[sys.executable,'-c','pass']], 'review_policy':'none',
                      'authorized_actions':['push'], 'actor_id':'actor','actor_context':'ctx'}
                context=new_context(self.repo,spec,'HEAD')
                self.write(directory/'context.json',context)
                self.write(directory/'gates.json',run_gates(context))
                config={'schema_version':1,'kind':'git-pre-push-config','repository':str(self.repo),
                        'context':str(directory/'context.json'),'gates':str(directory/'gates.json'),
                        'review':None,'fail_mode':'closed'}
                self.write(directory/'hook.json',config)
                with self.assertRaisesRegex(DeliveryError,'clean'):
                    check_hook(directory/'hook.json')
                with self.assertRaisesRegex(DeliveryError,'clean'):
                    install_hook(self.repo,directory/'hook.json',['install-hook'])
                self.git('reset','--hard','HEAD')
                if (self.repo/'new').exists():
                    (self.repo/'new').unlink()

    def test_repository_path_with_trailing_whitespace(self):
        previous=self.repo
        self.repo=self.root/'repository \n'
        previous.rename(self.repo)
        config=self.config()
        receipt=install_hook(self.repo,config,['install-hook'])
        self.assertEqual(receipt['hook'],str(self.repo/'.git/hooks/pre-push'))
        self.assertTrue(check_hook(config)['ready'])


    def test_relocated_installation_hook_fails_closed_without_ambient_fallback(self):
        import shutil
        from pathlib import Path
        from unittest.mock import patch
        import repo_delivery.hooks as hooks
        installed = self.root / "installed/src"
        shutil.copytree(Path(hooks.__file__).resolve().parents[1], installed)
        config = self.config()
        with patch.object(hooks, "__file__", str(installed / "repo_delivery/hooks.py")):
            install_hook(self.repo, config, ["install-hook"])
        hook = self.repo / ".git/hooks/pre-push"
        updates = b"refs/heads/main " + self.git("rev-parse", "HEAD").strip() + b" refs/heads/main " + b"0" * 40 + b"\n"
        first = subprocess.run([str(hook)], cwd=self.repo, capture_output=True, input=updates)
        self.assertEqual(first.returncode, 0, first.stderr)
        installed.rename(installed.parent / "moved")
        env = dict(os.environ, PYTHONPATH=str(Path(hooks.__file__).resolve().parents[1]))
        second = subprocess.run([str(hook)], cwd=self.repo, capture_output=True, input=updates, env=env)
        self.assertNotEqual(second.returncode, 0)
        self.assertIn(b"helper is missing", second.stderr)
