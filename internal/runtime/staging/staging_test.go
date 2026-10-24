package staging

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v: %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func fixture(t *testing.T) (string, string) {
	t.Helper()
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if e := os.Mkdir(repo, 0700); e != nil {
		t.Fatal(e)
	}
	gitTest(t, repo, "init", "-q")
	gitTest(t, repo, "config", "user.email", "test@example.invalid")
	gitTest(t, repo, "config", "user.name", "Test")
	if e := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("committed\n"), 0600); e != nil {
		t.Fatal(e)
	}
	gitTest(t, repo, "add", "tracked.txt")
	gitTest(t, repo, "commit", "-qm", "initial")
	return repo, filepath.Join(base, "staging")
}

func TestStagePreservesDirtySourceAndCopiesAllowlistedFiles(t *testing.T) {
	repo, root := fixture(t)
	os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("dirty\n"), 0600)
	os.WriteFile(filepath.Join(repo, "new.txt"), []byte("untracked\n"), 0600)
	cp, e := StageWithOptions(context.Background(), repo, root, "attempt-1", "input-7", Options{AllowedUntracked: []string{"new.txt"}})
	if e != nil {
		t.Fatal(e)
	}
	for path, want := range map[string]string{filepath.Join(repo, "tracked.txt"): "dirty\n", filepath.Join(repo, "new.txt"): "untracked\n", filepath.Join(cp.Workspace, "tracked.txt"): "dirty\n", filepath.Join(cp.Workspace, "new.txt"): "untracked\n"} {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if string(b) != want {
			t.Fatalf("%s=%q, want %q", path, b, want)
		}
	}
	if cp.BaseRevision == "" || !cp.Quiescent {
		t.Fatalf("incomplete initial checkpoint: %+v", cp)
	}
}

func TestStageRejectsUnsafeAndExistingAttempt(t *testing.T) {
	repo, root := fixture(t)
	if e := os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET=x"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Stage(context.Background(), repo, root, "attempt-1", "r1"); e == nil {
		t.Fatal("expected secret path rejection")
	}
	if _, e := Stage(context.Background(), repo, root, "../escape", "r1"); e == nil {
		t.Fatal("expected attempt ID rejection")
	}
	os.Remove(filepath.Join(repo, ".env"))
	if _, e := Stage(context.Background(), repo, root, "attempt-1", "r1"); e != nil {
		t.Fatal(e)
	}
	if _, e := Stage(context.Background(), repo, root, "attempt-1", "r2"); e == nil {
		t.Fatal("expected existing workspace rejection")
	}
}

func TestCheckpointSnapshotSurvivesWorkspaceMutationAndContinue(t *testing.T) {
	repo, root := fixture(t)
	os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("dirty\n"), 0600)
	os.WriteFile(filepath.Join(repo, "extra.txt"), []byte("extra\n"), 0600)
	cp, e := StageWithOptions(context.Background(), repo, root, "attempt-1", "r1", Options{AllowedUntracked: []string{"extra.txt"}})
	if e != nil {
		t.Fatal(e)
	}
	if cp.SnapshotRoot == "" {
		t.Fatal("missing snapshot root")
	}
	if e = os.WriteFile(filepath.Join(cp.Workspace, "tracked.txt"), []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.RemoveAll(cp.Workspace); e != nil {
		t.Fatal(e)
	}
	if e = Verify(cp); e != nil {
		t.Fatalf("immutable snapshot should survive workspace changes: %v", e)
	}
	next, e := Continue(context.Background(), cp, "attempt-2", "r2")
	if e != nil {
		t.Fatal(e)
	}
	for path, want := range map[string]string{filepath.Join(next.Workspace, "tracked.txt"): "dirty\n", filepath.Join(next.Workspace, "extra.txt"): "extra\n"} {
		b, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if string(b) != want {
			t.Fatalf("%s=%q, want %q", path, b, want)
		}
	}
	if !next.Quiescent || next.AttemptID == cp.AttemptID {
		t.Fatalf("bad continuation: %+v", next)
	}
	asset := filepath.Join(cp.SnapshotRoot, "tracked.txt")
	if e = os.Chmod(asset, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(asset, []byte("corrupt"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = Verify(cp); e == nil {
		t.Fatal("expected snapshot tamper detection")
	}
}

func TestContinueRestoresTrackedDeletion(t *testing.T) {
	repo, root := fixture(t)
	if e := os.Remove(filepath.Join(repo, "tracked.txt")); e != nil {
		t.Fatal(e)
	}
	cp, e := Stage(context.Background(), repo, root, "delete-1", "r1")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(cp.Workspace, "tracked.txt")); !os.IsNotExist(e) {
		t.Fatalf("tracked deletion was not staged: %v", e)
	}
	if e = os.RemoveAll(cp.Workspace); e != nil {
		t.Fatal(e)
	}
	next, e := Continue(context.Background(), cp, "delete-2", "r2")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(next.Workspace, "tracked.txt")); !os.IsNotExist(e) {
		t.Fatalf("tracked deletion was not restored: %v", e)
	}
}

func TestCheckpointRejectsSymlinkAndWorkspaceRootOverlap(t *testing.T) {
	repo, _ := fixture(t)
	if _, e := Stage(context.Background(), repo, repo, "attempt", "r"); e == nil {
		t.Fatal("expected overlap rejection")
	}
	w := t.TempDir()
	if e := os.Symlink(filepath.Join(w, "missing"), filepath.Join(w, "link")); e != nil {
		t.Fatal(e)
	}
	if _, e := Checkpoint(context.Background(), w, "a", "r", true); e == nil {
		t.Fatal("expected symlink rejection")
	}
}

func TestRepeatedCheckpointKeepsAttemptIDAndUsesUniqueSnapshot(t *testing.T) {
	repo, root := fixture(t)
	first, err := Stage(context.Background(), repo, root, "same-attempt", "same-input")
	if err != nil {
		t.Fatal(err)
	}
	second, err := Checkpoint(context.Background(), first.Workspace, "same-attempt", "same-input", true)
	if err != nil {
		t.Fatal(err)
	}
	if first.AttemptID != "same-attempt" || second.AttemptID != "same-attempt" {
		t.Fatalf("attempt identity was rewritten: %q / %q", first.AttemptID, second.AttemptID)
	}
	if first.SnapshotRoot == second.SnapshotRoot || first.ID == second.ID {
		t.Fatal("repeated checkpoints reused immutable snapshot identity")
	}
	if err = Verify(first); err != nil {
		t.Fatal(err)
	}
	if err = Verify(second); err != nil {
		t.Fatal(err)
	}
	forged := second
	forged.ObservedAt = forged.ObservedAt + "-tampered"
	if err = Verify(forged); err == nil {
		t.Fatal("observedAt metadata tampering was not detected")
	}
	bound, err := CheckpointFor(context.Background(), first.Workspace, "same-attempt", "same-input", repo, true)
	if err != nil {
		t.Fatal(err)
	}
	if bound.Repository != repo {
		t.Fatalf("source repository not bound: %q", bound.Repository)
	}
	recovered, err := Continue(context.Background(), bound, "continued-attempt", "next-input")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Repository != repo {
		t.Fatalf("continuation lost source repository: %q", recovered.Repository)
	}
}

func TestStageDisablesCheckoutHooksAndSmudgeFilters(t *testing.T) {
	repo, root := fixture(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.txt filter=canary\n"), 0600); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", ".gitattributes")
	gitTest(t, repo, "commit", "-qm", "add filter attribute")
	canary := filepath.Join(filepath.Dir(repo), "git-code-ran")
	script := "touch '" + canary + "'; cat"
	gitTest(t, repo, "config", "filter.canary.smudge", "sh -c \""+script+"\"")
	hooks := filepath.Join(repo, ".git", "hooks")
	if err := os.MkdirAll(hooks, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte("#!/bin/sh\ntouch '"+canary+"'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Stage(context.Background(), repo, root, "attempt-safe-git", "r1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(canary); !os.IsNotExist(err) {
		t.Fatalf("repository-provided hook or smudge filter executed (stat err %v)", err)
	}
	if got, err := os.ReadFile(filepath.Join(repo, "tracked.txt")); err != nil || string(got) != "committed\n" {
		t.Fatalf("source bytes changed: %q, %v", got, err)
	}
}

func TestExecutableBitSurvivesSnapshotAndContinuation(t *testing.T) {
	repo, root := fixture(t)
	script := filepath.Join(repo, "run.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho ok\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0750); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", "run.sh")
	gitTest(t, repo, "commit", "-qm", "add executable script")
	cp, err := Stage(context.Background(), repo, root, "exec-1", "input-exec")
	if err != nil {
		t.Fatal(err)
	}
	var scriptAsset string
	for _, asset := range cp.Assets {
		if asset.Path == "run.sh" {
			scriptAsset = asset.Path
			if !asset.Executable {
				t.Fatal("checkpoint omitted executable metadata")
			}
		}
	}
	if scriptAsset == "" {
		t.Fatal("script absent from checkpoint")
	}
	if err = os.RemoveAll(cp.Workspace); err != nil {
		t.Fatal(err)
	}
	next, err := Continue(context.Background(), cp, "exec-2", "input-next")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(next.Workspace, scriptAsset))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("continued script lost executable mode: %v", info.Mode().Perm())
	}
	if err = Verify(cp); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(filepath.Join(cp.SnapshotRoot, scriptAsset), 0400); err != nil {
		t.Fatal(err)
	}
	if err = Verify(cp); err == nil {
		t.Fatal("expected snapshot executable-mode tamper detection")
	}
}
