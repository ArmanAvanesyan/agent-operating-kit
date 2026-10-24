// Package staging creates isolated workspaces and content-addressed checkpoints.
// It never removes a workspace: an operator can inspect or recover every attempt.
package staging

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
)

const (
	maxTotalBytes     = 64 << 20
	maxFileBytes      = 4 << 20
	defaultGitTimeout = 30 * time.Second
)

// Options controls the bounded source material copied into a staged attempt.
// Untracked files are excluded unless explicitly listed; secret-like paths are
// rejected even when listed.
type Options struct {
	AllowedUntracked []string
	GitTimeout       time.Duration
}

// Stage creates a detached git worktree and overlays tracked source bytes plus
// explicitly allowlisted, safe untracked files. The source checkout is read-only.
func Stage(ctx context.Context, repository, stagingRoot, attemptID, inputRevision string) (records.Checkpoint, error) {
	return StageWithOptions(ctx, repository, stagingRoot, attemptID, inputRevision, Options{})
}

// StageWithOptions is Stage with an explicit untracked-file allowlist.
func StageWithOptions(ctx context.Context, repository, stagingRoot, attemptID, inputRevision string, options Options) (records.Checkpoint, error) {
	if err := validID(attemptID); err != nil {
		return records.Checkpoint{}, err
	}
	if strings.TrimSpace(inputRevision) == "" {
		return records.Checkpoint{}, errors.New("input revision is required")
	}
	if options.GitTimeout <= 0 {
		options.GitTimeout = defaultGitTimeout
	}
	repo, err := canonicalDir(repository)
	if err != nil {
		return records.Checkpoint{}, fmt.Errorf("repository: %w", err)
	}
	root, err := canonicalRoot(stagingRoot)
	if err != nil {
		return records.Checkpoint{}, fmt.Errorf("staging root: %w", err)
	}
	if overlaps(repo, root) {
		return records.Checkpoint{}, errors.New("repository and staging root must not overlap")
	}
	workspace := filepath.Join(root, attemptID)
	if _, err := os.Lstat(workspace); err == nil {
		return records.Checkpoint{}, fmt.Errorf("attempt workspace already exists: %s", workspace)
	} else if !errors.Is(err, os.ErrNotExist) {
		return records.Checkpoint{}, err
	}
	git := func(args ...string) ([]byte, error) {
		return runGit(ctx, options.GitTimeout, repo, args...)
	}
	out, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return records.Checkpoint{}, err
	}
	gitRoot, err := canonicalDir(strings.TrimSpace(string(out)))
	if err != nil {
		return records.Checkpoint{}, err
	}
	if gitRoot != repo {
		return records.Checkpoint{}, errors.New("repository must be the git worktree root")
	}
	baseOut, err := git("rev-parse", "HEAD")
	if err != nil {
		return records.Checkpoint{}, err
	}
	base := strings.TrimSpace(string(baseOut))
	tracked, err := git("ls-files", "-z")
	if err != nil {
		return records.Checkpoint{}, err
	}
	untracked, err := git("ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return records.Checkpoint{}, err
	}
	for _, p := range splitNUL(untracked) {
		if p != "" && secretPath(p) {
			return records.Checkpoint{}, fmt.Errorf("refusing secret-like source path %q", p)
		}
	}
	allow := map[string]bool{}
	for _, p := range options.AllowedUntracked {
		clean, e := safeRel(p)
		if e != nil {
			return records.Checkpoint{}, e
		}
		allow[clean] = true
	}
	files := map[string]bool{}
	for _, p := range splitNUL(tracked) {
		if p != "" {
			files[p] = true
		}
	}
	for _, p := range splitNUL(untracked) {
		if p != "" && allow[p] {
			files[p] = true
		}
	}
	paths := make([]string, 0, len(files))
	for p := range files {
		if secretPath(p) {
			return records.Checkpoint{}, fmt.Errorf("refusing secret-like source path %q", p)
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)
	if err = os.MkdirAll(root, 0700); err != nil {
		return records.Checkpoint{}, err
	}
	if _, err = git("worktree", "add", "--no-checkout", "--detach", workspace, base); err != nil {
		return records.Checkpoint{}, err
	}
	var total int64
	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return records.Checkpoint{}, err
		}
		src := filepath.Join(repo, filepath.FromSlash(p))
		info, e := os.Lstat(src)
		if errors.Is(e, os.ErrNotExist) {
			if err = os.Remove(filepath.Join(workspace, filepath.FromSlash(p))); err != nil && !errors.Is(err, os.ErrNotExist) {
				return records.Checkpoint{}, err
			}
			continue
		}
		if e != nil {
			return records.Checkpoint{}, e
		}
		if !info.Mode().IsRegular() {
			return records.Checkpoint{}, fmt.Errorf("refusing non-regular source asset %q", p)
		}
		if info.Size() > maxFileBytes {
			return records.Checkpoint{}, fmt.Errorf("source asset exceeds 4 MiB: %q", p)
		}
		total += info.Size()
		if total > maxTotalBytes {
			return records.Checkpoint{}, errors.New("staged source assets exceed 64 MiB")
		}
		data, e := os.ReadFile(src)
		if e != nil {
			return records.Checkpoint{}, e
		}
		dst := filepath.Join(workspace, filepath.FromSlash(p))
		if e = os.MkdirAll(filepath.Dir(dst), 0700); e != nil {
			return records.Checkpoint{}, e
		}
		mode := info.Mode().Perm() & 0755
		if mode == 0 {
			mode = 0600
		}
		if e = os.WriteFile(dst, data, mode); e != nil {
			return records.Checkpoint{}, e
		}
	}
	cp, e := Checkpoint(ctx, workspace, attemptID, inputRevision, true)
	if e != nil {
		return records.Checkpoint{}, e
	}
	cp.Repository = repo
	cp.BaseRevision = base
	cp.Digest = digest(cp)
	return cp, nil
}

// Checkpoint hashes regular workspace files and returns a canonical digest.
// Symlinks, git metadata, unsafe paths, and files over the package limits fail closed.
func Checkpoint(ctx context.Context, workspace, attemptID, inputRevision string, quiescent bool) (records.Checkpoint, error) {
	if err := validID(attemptID); err != nil {
		return records.Checkpoint{}, err
	}
	if strings.TrimSpace(inputRevision) == "" {
		return records.Checkpoint{}, errors.New("input revision is required")
	}
	root, err := canonicalDir(workspace)
	if err != nil {
		return records.Checkpoint{}, err
	}
	assets, _, err := walkAssets(ctx, root)
	if err != nil {
		return records.Checkpoint{}, err
	}
	base := gitBase(ctx, root)
	checkpointRoot := filepath.Join(filepath.Dir(root), ".aok-checkpoints")
	if err = ensurePrivateDirectory(checkpointRoot); err != nil {
		return records.Checkpoint{}, err
	}
	key := sha256.Sum256([]byte(inputRevision))
	nonce := make([]byte, 12)
	if _, err = rand.Read(nonce); err != nil {
		return records.Checkpoint{}, fmt.Errorf("create checkpoint identity: %w", err)
	}
	checkpointID := attemptID + "-" + hex.EncodeToString(key[:8]) + "-" + hex.EncodeToString(nonce)
	snapshotRoot := filepath.Join(checkpointRoot, checkpointID)
	if err = os.Mkdir(snapshotRoot, 0700); err != nil {
		return records.Checkpoint{}, fmt.Errorf("create immutable checkpoint snapshot: %w", err)
	}
	if err = snapshotAssets(ctx, root, snapshotRoot, assets); err != nil {
		return records.Checkpoint{}, err
	}
	cp := records.Checkpoint{Version: records.Version, ID: checkpointID, AttemptID: attemptID, InputRevision: inputRevision, Workspace: root, Repository: root, BaseRevision: base, SnapshotRoot: snapshotRoot, Assets: assets, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Quiescent: quiescent}
	cp.Digest = digest(cp)
	return cp, nil
}

// CheckpointFor captures immutable workspace bytes and binds the checkpoint to
// the canonical source repository that can recreate its detached worktree.
func CheckpointFor(ctx context.Context, workspace, attemptID, inputRevision, repository string, quiescent bool) (records.Checkpoint, error) {
	repo, err := canonicalDir(repository)
	if err != nil {
		return records.Checkpoint{}, fmt.Errorf("repository: %w", err)
	}
	cp, err := Checkpoint(ctx, workspace, attemptID, inputRevision, quiescent)
	if err != nil {
		return records.Checkpoint{}, err
	}
	if overlaps(repo, filepath.Dir(cp.Workspace)) {
		return records.Checkpoint{}, errors.New("repository and checkpoint staging root must not overlap")
	}
	gitRoot, err := gitOutput(ctx, repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return records.Checkpoint{}, fmt.Errorf("repository is not a git worktree: %w", err)
	}
	canonicalGitRoot, err := canonicalDir(strings.TrimSpace(string(gitRoot)))
	if err != nil || canonicalGitRoot != repo {
		return records.Checkpoint{}, errors.New("repository must be its canonical git worktree root")
	}
	if cp.BaseRevision != "" {
		if _, err = gitOutput(ctx, repo, "cat-file", "-e", cp.BaseRevision+"^{commit}"); err != nil {
			return records.Checkpoint{}, errors.New("checkpoint base revision is absent from source repository")
		}
	}
	cp.Repository = repo
	cp.Digest = digest(cp)
	return cp, nil
}

// Verify confirms checkpoint metadata and the immutable snapshot bytes match.
// The original mutable workspace may have changed or been removed.
func Verify(cp records.Checkpoint) error {
	if cp.Version != records.Version || cp.Digest == "" || digest(cp) != cp.Digest {
		return errors.New("checkpoint metadata digest mismatch")
	}
	if cp.SnapshotRoot == "" {
		return errors.New("checkpoint snapshot root is missing")
	}
	key := sha256.Sum256([]byte(cp.InputRevision))
	prefix := cp.AttemptID + "-" + hex.EncodeToString(key[:8]) + "-"
	if !strings.HasPrefix(cp.ID, prefix) || len(strings.TrimPrefix(cp.ID, prefix)) != 24 || !isLowerHex(strings.TrimPrefix(cp.ID, prefix)) {
		return errors.New("checkpoint ID is invalid")
	}
	expectedRoot := filepath.Join(filepath.Dir(cp.Workspace), ".aok-checkpoints", cp.ID)
	if filepath.Clean(cp.SnapshotRoot) != filepath.Clean(expectedRoot) {
		return errors.New("checkpoint snapshot root is not the expected private sibling")
	}
	info, err := os.Lstat(cp.SnapshotRoot)
	if err != nil {
		return fmt.Errorf("checkpoint snapshot: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return errors.New("checkpoint snapshot directory is not private")
	}
	root, err := canonicalDir(cp.SnapshotRoot)
	if err != nil {
		return fmt.Errorf("checkpoint snapshot: %w", err)
	}
	assets, _, err := walkAssets(context.Background(), root)
	if err != nil {
		return err
	}
	if len(assets) != len(cp.Assets) {
		return errors.New("checkpoint snapshot asset set changed")
	}
	for i := range assets {
		if assets[i] != cp.Assets[i] {
			return fmt.Errorf("checkpoint snapshot asset changed: %s", cp.Assets[i].Path)
		}
	}
	return nil
}

// Continue creates a fresh worktree at the checkpoint base and restores its
// immutable snapshot. It never reads from or writes to the prior workspace.
func Continue(ctx context.Context, cp records.Checkpoint, newAttemptID, inputRevision string) (records.Checkpoint, error) {
	if err := Verify(cp); err != nil {
		return records.Checkpoint{}, fmt.Errorf("prior checkpoint is not recoverable: %w", err)
	}
	if !cp.Quiescent {
		return records.Checkpoint{}, errors.New("checkpoint is not quiescent")
	}
	if err := validID(newAttemptID); err != nil {
		return records.Checkpoint{}, err
	}
	if strings.TrimSpace(inputRevision) == "" {
		return records.Checkpoint{}, errors.New("input revision is required")
	}
	repo, err := canonicalDir(cp.Repository)
	if err != nil {
		return records.Checkpoint{}, fmt.Errorf("checkpoint repository: %w", err)
	}
	stagingRoot, err := canonicalDir(filepath.Dir(cp.Workspace))
	if err != nil {
		return records.Checkpoint{}, fmt.Errorf("checkpoint staging root: %w", err)
	}
	if overlaps(repo, stagingRoot) {
		return records.Checkpoint{}, errors.New("repository and staging root must not overlap")
	}
	workspace := filepath.Join(stagingRoot, newAttemptID)
	if _, err = os.Lstat(workspace); err == nil {
		return records.Checkpoint{}, fmt.Errorf("attempt workspace already exists: %s", workspace)
	} else if !errors.Is(err, os.ErrNotExist) {
		return records.Checkpoint{}, err
	}
	if cp.BaseRevision == "" {
		return records.Checkpoint{}, errors.New("checkpoint base revision is missing")
	}
	cctx, cancel := context.WithTimeout(ctx, defaultGitTimeout)
	defer cancel()
	if out, e := runGit(cctx, defaultGitTimeout, repo, "worktree", "add", "--no-checkout", "--detach", workspace, cp.BaseRevision); e != nil {
		return records.Checkpoint{}, fmt.Errorf("restore git worktree: %w: %s", e, strings.TrimSpace(string(out)))
	}
	present := make(map[string]bool, len(cp.Assets))
	for _, a := range cp.Assets {
		p, e := safeRel(a.Path)
		if e != nil || secretPath(p) {
			return records.Checkpoint{}, fmt.Errorf("unsafe checkpoint asset path %q", a.Path)
		}
		present[p] = true
	}
	// Baseline tracked files absent from the snapshot represent deletions.
	tracked, err := gitOutput(ctx, repo, "ls-tree", "-r", "--name-only", "-z", cp.BaseRevision)
	if err != nil {
		return records.Checkpoint{}, err
	}
	for _, p := range splitNUL(tracked) {
		if p == "" || present[p] {
			continue
		}
		if err = os.Remove(filepath.Join(workspace, filepath.FromSlash(p))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return records.Checkpoint{}, fmt.Errorf("restore deletion %q: %w", p, err)
		}
	}
	for _, a := range cp.Assets {
		if err = ctx.Err(); err != nil {
			return records.Checkpoint{}, err
		}
		src := filepath.Join(cp.SnapshotRoot, filepath.FromSlash(a.Path))
		dst := filepath.Join(workspace, filepath.FromSlash(a.Path))
		if e := ensureNoSymlinkPath(workspace, dst); e != nil {
			return records.Checkpoint{}, e
		}
		if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return records.Checkpoint{}, err
		}
		data, e := os.ReadFile(src)
		if e != nil {
			return records.Checkpoint{}, e
		}
		mode := fs.FileMode(0600)
		if a.Executable {
			mode = 0700
		}
		if e = os.WriteFile(dst, data, mode); e != nil {
			return records.Checkpoint{}, e
		}
	}
	next, err := Checkpoint(ctx, workspace, newAttemptID, inputRevision, true)
	if err != nil {
		return records.Checkpoint{}, err
	}
	next.Repository = repo
	next.BaseRevision = cp.BaseRevision
	next.Digest = digest(next)
	return next, nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("checkpoint storage root must be a real directory")
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("checkpoint storage root permissions must be private")
	}
	return nil
}

func snapshotAssets(ctx context.Context, sourceRoot, snapshotRoot string, assets []records.Asset) error {
	for _, a := range assets {
		if err := ctx.Err(); err != nil {
			return err
		}
		p, err := safeRel(a.Path)
		if err != nil || secretPath(p) {
			return fmt.Errorf("unsafe checkpoint asset path %q", a.Path)
		}
		src := filepath.Join(sourceRoot, filepath.FromSlash(p))
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		h := sha256.Sum256(data)
		if hex.EncodeToString(h[:]) != a.SHA256 {
			return fmt.Errorf("workspace changed during checkpoint capture: %s", p)
		}
		dst := filepath.Join(snapshotRoot, filepath.FromSlash(p))
		if err = os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return err
		}
		mode := fs.FileMode(0400)
		if a.Executable {
			mode = 0500
		}
		if err = os.WriteFile(dst, data, mode); err != nil {
			return err
		}
	}
	return nil
}

func ensureNoSymlinkPath(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errors.New("checkpoint asset escapes workspace")
	}
	current := root
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink in restore path %q", current)
		}
	}
	return nil
}

func gitOutput(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return runGit(ctx, defaultGitTimeout, dir, args...)
}

// runGit disables hooks, fsmonitor, and recursive submodule operations for
// every helper invocation. The empty hooks directory cannot contain repository
// supplied code; worktree creation also skips checkout so smudge filters never
// run before files are copied as inert bytes.
func runGit(ctx context.Context, timeout time.Duration, dir string, args ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	hooks, err := os.MkdirTemp("", "aok-disabled-git-hooks-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(hooks)
	allArgs := []string{"-c", "core.hooksPath=" + hooks, "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false"}
	allArgs = append(allArgs, args...)
	cmd := exec.CommandContext(cctx, "git", allArgs...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("git %v: %s", args, strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

func walkAssets(ctx context.Context, root string) ([]records.Asset, int64, error) {
	var assets []records.Asset
	var total int64
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e := ctx.Err(); e != nil {
			return e
		}
		if path == root {
			return nil
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if rel == ".git" || strings.HasPrefix(rel, ".git/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(rel, ".aok-checkpoints/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing symlink %q", rel)
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("refusing non-regular asset %q", rel)
		}
		if secretPath(rel) {
			return fmt.Errorf("refusing secret-like asset %q", rel)
		}
		if info.Size() > maxFileBytes {
			return fmt.Errorf("asset exceeds 4 MiB: %q", rel)
		}
		total += info.Size()
		if total > maxTotalBytes {
			return errors.New("checkpoint assets exceed 64 MiB")
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		h := sha256.New()
		_, e = ioCopyContext(ctx, h, f)
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		assets = append(assets, records.Asset{Path: rel, SHA256: hex.EncodeToString(h.Sum(nil)), Executable: info.Mode()&0111 != 0})
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	sort.Slice(assets, func(i, j int) bool { return assets[i].Path < assets[j].Path })
	return assets, total, nil
}

func ioCopyContext(ctx context.Context, dst interface{ Write([]byte) (int, error) }, src interface{ Read([]byte) (int, error) }) (int64, error) {
	buf := make([]byte, 32*1024)
	var n int64
	for {
		if e := ctx.Err(); e != nil {
			return n, e
		}
		r, e := src.Read(buf)
		if r > 0 {
			w, we := dst.Write(buf[:r])
			n += int64(w)
			if we != nil {
				return n, we
			}
			if w != r {
				return n, errors.New("short write")
			}
		}
		if e != nil {
			if errors.Is(e, io.EOF) {
				return n, nil
			}
			return n, e
		}
	}
}

func gitBase(ctx context.Context, dir string) string {
	b, e := runGit(ctx, defaultGitTimeout, dir, "rev-parse", "HEAD")
	if e != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func digest(cp records.Checkpoint) string {
	cp.Digest = ""
	b, err := json.Marshal(cp)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func isLowerHex(s string) bool {
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func validID(s string) error {
	if s == "" || len(s) > 80 {
		return errors.New("attempt ID must be 1-80 characters")
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return errors.New("attempt ID contains unsafe characters")
		}
	}
	return nil
}
func safeRel(p string) (string, error) {
	p = filepath.ToSlash(filepath.Clean(p))
	if p == "." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") {
		return "", fmt.Errorf("unsafe relative path %q", p)
	}
	return p, nil
}
func secretPath(p string) bool {
	q := strings.ToLower(filepath.Base(p))
	return q == ".env" || strings.HasPrefix(q, ".env.") || strings.Contains(q, "credentials") || strings.Contains(q, "secret") || strings.HasSuffix(q, ".pem") || strings.HasSuffix(q, ".key")
}
func splitNUL(b []byte) []string { return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00") }
func canonicalDir(path string) (string, error) {
	a, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	a, e = filepath.EvalSymlinks(a)
	if e != nil {
		return "", e
	}
	i, e := os.Stat(a)
	if e != nil {
		return "", e
	}
	if !i.IsDir() {
		return "", errors.New("path is not a directory")
	}
	return filepath.Clean(a), nil
}
func canonicalRoot(path string) (string, error) {
	a, e := filepath.Abs(path)
	if e != nil {
		return "", e
	}
	if e = os.MkdirAll(a, 0700); e != nil {
		return "", e
	}
	return canonicalDir(a)
}
func overlaps(a, b string) bool {
	rel, e := filepath.Rel(a, b)
	if e != nil {
		return true
	}
	if rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != "..") {
		return true
	}
	rel, e = filepath.Rel(b, a)
	return e == nil && (rel == "." || (!strings.HasPrefix(rel, ".."+string(os.PathSeparator)) && rel != ".."))
}
