import os
import unittest
from test_core import RepositoryFixture
from repo_delivery.core import DeliveryError, create_checkpoint
from repo_delivery.workspace import isolate, plan_port, apply_port, plan_cleanup, apply_cleanup


class WorkspaceTests(RepositoryFixture):
    def test_isolate_preserves_dirty_source_and_requires_permission(self):
        (self.repo / 'a').write_text('dirty')
        dest = self.root / 'isolated'
        with self.assertRaises(DeliveryError):
            isolate(self.repo, 'main', dest, 'feat/task')
        isolate(self.repo, 'main', dest, 'feat/task', ['isolate'])
        self.assertEqual((dest / 'a').read_text(), 'initial\n')
        self.assertEqual((self.repo / 'a').read_text(), 'dirty')
        with self.assertRaises(DeliveryError):
            isolate(self.repo, 'main', self.root / 'bad', '-bad', ['isolate'])

    def test_port_binary_deletion_symlink_modes_and_new_baseline_files(self):
        (self.repo / 'a').unlink()
        (self.repo / 'binary').write_bytes(b'\x00\xff')
        (self.repo / 'exec').write_text('script')
        (self.repo / 'exec').chmod(0o755)
        (self.repo / 'link').symlink_to('/outside/missing')
        cp = self.root / 'cp'
        create_checkpoint(self.repo, cp)
        self.git('reset', '--hard')
        (self.repo / 'baseline-new').write_text('keep')
        self.git('add', 'baseline-new')
        self.git('commit', '-m', 'new baseline')
        plan = plan_port(cp, self.repo, 'main', self.root / 'ported', 'feat/port')
        with self.assertRaises(DeliveryError):
            apply_port(plan)
        apply_port(plan, ['port'])
        out = self.root / 'ported'
        self.assertFalse((out / 'a').exists())
        self.assertEqual((out / 'baseline-new').read_text(), 'keep')
        self.assertEqual((out / 'binary').read_bytes(), b'\x00\xff')
        self.assertEqual(os.readlink(out / 'link'), '/outside/missing')
        self.assertTrue((out / 'exec').stat().st_mode & 0o111)

    def test_port_conflict_and_stale_source_fail_closed(self):
        (self.repo / 'a').write_text('local')
        cp = self.root / 'cp'
        create_checkpoint(self.repo, cp)
        self.git('reset', '--hard')
        (self.repo / 'a').write_text('upstream')
        self.git('add', 'a')
        self.git('commit', '-m', 'upstream')
        plan = plan_port(cp, self.repo, 'main', self.root / 'ported', 'feat/port')
        self.assertEqual(plan['entries'][0]['state'], 'conflict')
        with self.assertRaises(DeliveryError):
            apply_port(plan, ['port'])
        self.assertFalse((self.root / 'ported').exists())
        plan = plan_port(cp, self.repo, 'HEAD~1', self.root / 'other', 'feat/other')
        (self.repo / 'extra').write_text('race')
        with self.assertRaises(DeliveryError):
            apply_port(plan, ['port'])

    def test_cleanup_merged_dirty_ignored_and_stale(self):
        self.git('branch', 'chore/merged')
        plan = plan_cleanup(self.repo, branches=['chore/merged'])
        self.git('branch', '-f', 'chore/merged', 'main')
        with self.assertRaises(DeliveryError):
            apply_cleanup(plan)
        apply_cleanup(plan, ['cleanup'])
        self.assertNotIn(b'chore/merged', self.git('branch'))
        wt = self.root / 'wt'
        self.git('worktree', 'add', '-b', 'chore/work', str(wt))
        (wt / '.gitignore').write_text('private\n')
        (wt / 'private').write_text('secret')
        plan = plan_cleanup(self.repo, worktrees=[str(wt)])
        self.assertEqual(plan['entries'][0]['state'], 'retained-dirty')
        apply_cleanup(plan, ['cleanup'])
        self.assertTrue(wt.exists())
        self.git('branch', 'chore/another')
        plan = plan_cleanup(self.repo, branches=['chore/another'])
        (self.repo / 'a').write_text('changed')
        with self.assertRaises(DeliveryError):
            apply_cleanup(plan, ['cleanup'])

    def test_plan_tampering_and_unsafe_port_path(self):
        cp = self.root / 'cp'
        create_checkpoint(self.repo, cp)
        plan = plan_port(cp, self.repo, 'main', self.root / 'port', 'feat/port')
        plan['destination'] = str(self.repo)
        with self.assertRaises(DeliveryError):
            apply_port(plan, ['port'])
        with self.assertRaises(DeliveryError):
            plan_port(cp, self.repo, 'main', self.root / 'port', 'feat/port', ['../bad'])

    def test_cleanup_clean_worktree_and_unmerged_branch(self):
        wt = self.root / 'work tree\nname'
        self.git('worktree', 'add', '-b', 'chore/clean', str(wt))
        plan = plan_cleanup(self.repo, worktrees=[str(wt)])
        self.assertEqual(plan['entries'][0]['state'], 'delete')
        apply_cleanup(plan, ['cleanup'])
        self.assertFalse(wt.exists())
        self.git('checkout', '-b', 'feat/unmerged')
        (self.repo / 'work').write_text('useful')
        self.git('add', 'work')
        self.git('commit', '-m', 'unmerged')
        self.git('checkout', 'main')
        plan = plan_cleanup(self.repo, branches=['feat/unmerged'])
        self.assertEqual(plan['entries'][0]['state'], 'retained-unmerged')
        apply_cleanup(plan, ['cleanup'])
        self.assertIn(b'feat/unmerged', self.git('branch'))

    def test_ignored_only_worktree_is_retained_and_revalidated(self):
        (self.repo / '.gitignore').write_text('private\n')
        self.git('add', '.gitignore')
        self.git('commit', '-m', 'ignore')
        wt = self.root / 'wt'
        self.git('worktree', 'add', '-b', 'chore/private', str(wt))
        plan = plan_cleanup(self.repo, worktrees=[str(wt)])
        (wt / 'private').write_text('secret')
        with self.assertRaises(DeliveryError):
            apply_cleanup(plan, ['cleanup'])
        plan = plan_cleanup(self.repo, worktrees=[str(wt)])
        self.assertEqual(plan['entries'][0]['state'], 'retained-dirty')

    def test_selected_port_omits_conflicts_and_preserves_superseded(self):
        (self.repo / 'a').write_text('local')
        (self.repo / 'new').write_text('retained')
        cp = self.root / 'cp'
        create_checkpoint(self.repo, cp)
        self.git('reset', '--hard')
        (self.repo / 'a').write_text('upstream')
        self.git('add', 'a')
        self.git('commit', '-m', 'upstream')
        plan = plan_port(cp, self.repo, 'main', self.root / 'port', 'feat/port', ['new'])
        self.assertEqual(plan['entries'][0]['state'], 'omitted')
        apply_port(plan, ['port'])
        self.assertEqual((self.root / 'port/a').read_text(), 'upstream')
        self.assertEqual((self.root / 'port/new').read_text(), 'retained')

    def test_destination_collision_does_not_remove_other_files(self):
        from unittest.mock import patch
        from repo_delivery import workspace
        dest = self.root / 'port'
        original = workspace._clone
        def raced(repo, revision, destination, branch):
            destination.mkdir()
            (destination / 'keep').write_text('other owner')
            return original(repo, revision, destination, branch)
        with patch.object(workspace, '_clone', raced):
            with self.assertRaises(DeliveryError):
                isolate(self.repo, 'main', dest, 'feat/port', ['isolate'])
        self.assertEqual((dest / 'keep').read_text(), 'other owner')

    def test_archive_destination_and_common_git_storage_rejected(self):
        cp = self.root / 'cp'
        create_checkpoint(self.repo, cp)
        with self.assertRaises(DeliveryError):
            plan_port(cp, self.repo, 'main', cp / 'nested', 'feat/port')
        wt = self.root / 'wt'
        self.git('worktree', 'add', '-b', 'chore/linked', str(wt))
        with self.assertRaises(DeliveryError):
            isolate(wt, 'main', self.repo / '.git/new', 'feat/port', ['isolate'])
        plan = plan_cleanup(wt, worktrees=[str(self.repo)])
        self.assertEqual(plan['entries'][0]['state'], 'retained-primary')
