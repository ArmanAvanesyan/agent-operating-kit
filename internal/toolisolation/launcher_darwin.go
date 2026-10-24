//go:build darwin

package toolisolation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const dyldSupportProfile = "/System/Library/Sandbox/Profiles/dyld-support.sb"

type cappedOutput struct {
	mu       sync.Mutex
	b        bytes.Buffer
	overflow bool
	limit    int
}

func (w *cappedOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	remain := w.limit - w.b.Len()
	if remain > 0 {
		if remain > n {
			remain = n
		}
		_, _ = w.b.Write(p[:remain])
	}
	if remain < n {
		w.overflow = true
	}
	return n, nil
}
func (w *cappedOutput) bytes() ([]byte, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.b.Bytes()...), w.overflow
}

func runSandboxed(ctx context.Context, p Policy, input []byte, root *os.File) ([]byte, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, errors.New("request denied")
	}
	const sandboxExec = "/usr/bin/sandbox-exec"
	if _, err := os.Stat(sandboxExec); err != nil {
		return nil, errors.New("unavailable")
	}
	profile, err := seatbeltProfile(p)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(sandboxExec, "-p", profile, p.WorkerExecutable)
	cmd.Dir = p.Workspace
	cmd.Env = []string{}
	cmd.Stdin = bytes.NewReader(input)
	cmd.ExtraFiles = []*os.File{root}
	out := &cappedOutput{limit: maxLauncherOutput}
	stderr := &cappedOutput{limit: 64 << 10}
	cmd.Stdout = out
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if ctx.Err() != nil {
		return nil, errors.New("request denied")
	}
	if err := cmd.Start(); err != nil {
		return nil, errors.New("start denied")
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
		b, overflow := out.bytes()
		_, stderrOverflow := stderr.bytes()
		if err != nil || overflow || stderrOverflow {
			return nil, errors.New("worker failed")
		}
		return b, nil
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		return nil, errors.New("timeout")
	}
}

func seatbeltProfile(p Policy) (string, error) {
	workspace, err := sbplString(p.Workspace)
	if err != nil {
		return "", err
	}
	executable, err := sbplString(p.WorkerExecutable)
	if err != nil {
		return "", err
	}
	target, err := sbplString(filepath.Join(p.Workspace, p.Target))
	if err != nil {
		return "", err
	}
	reads := make([]string, 0, len(p.ReadPaths)*4)
	for _, relative := range p.ReadPaths {
		absolute := filepath.Join(p.Workspace, filepath.FromSlash(relative))
		file, err := sbplString(absolute)
		if err != nil {
			return "", err
		}
		reads = append(reads, fmt.Sprintf("(allow file-read-data file-read-metadata file-test-existence (literal %s))", file))
		for parent := filepath.Dir(absolute); parent != p.Workspace && strings.HasPrefix(parent, p.Workspace+string(filepath.Separator)); parent = filepath.Dir(parent) {
			dir, e := sbplString(parent)
			if e != nil {
				return "", e
			}
			reads = append(reads, fmt.Sprintf("(allow file-read-metadata (literal %s))", dir))
		}
	}
	stageMeta := make([]string, 0, 8)
	for dir := filepath.Dir(p.WorkerExecutable); ; dir = filepath.Dir(dir) {
		literal, e := sbplString(dir)
		if e != nil {
			return "", e
		}
		stageMeta = append(stageMeta, fmt.Sprintf("(allow file-read-metadata file-test-existence (literal %s))", literal))
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	// Deny by default. User data is visible only through finite read literals;
	// the workspace root is inherited as an already-open directory capability.
	return fmt.Sprintf(`(version 3)
(deny default)
(import %s)
(allow syscall-unix (syscall-number SYS_execve SYS___mac_execve SYS_posix_spawn))
(allow process-fork)
(allow process-exec (literal %s))
(allow process-exec-interpreter (literal "/usr/lib/dyld"))
(allow process-info* (target self))
(allow sysctl-read)
(allow file-read* file-test-existence (literal %s)
  (subpath "/System/Library") (subpath "/usr/lib")
  (subpath "/System/Volumes/Preboot/Cryptexes/OS/System/Library")
  (subpath "/System/Volumes/Preboot/Cryptexes/OS/System/Library/dyld")
  (subpath "/System/Volumes/Preboot/Cryptexes/OS/usr/lib")
(literal "/dev/null") (literal "/dev/urandom"))
(allow file-read-metadata (literal %s))
%s
%s
(allow file-map-executable (literal %s)
  (subpath "/System/Library") (subpath "/usr/lib")
  (subpath "/System/Volumes/Preboot/Cryptexes/OS/System/Library")
  (subpath "/System/Volumes/Preboot/Cryptexes/OS/System/Library/dyld")
  (subpath "/System/Volumes/Preboot/Cryptexes/OS/usr/lib"))
(allow file-write* (literal %s) (literal "/dev/null"))
(deny network*)
`, mustSBPLString(dyldSupportProfile), executable, executable, workspace, strings.Join(reads, "\n"), strings.Join(stageMeta, "\n"), executable, target), nil
}

func dyldSupportSHA256() (string, error) {
	b, err := os.ReadFile(dyldSupportProfile)
	if err != nil || len(b) == 0 || len(b) > 1<<20 {
		return "", errors.New("runtime profile unavailable")
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func mustSBPLString(s string) string {
	v, _ := sbplString(s)
	return v
}

func sbplString(s string) (string, error) {
	if s == "" || strings.ContainsAny(s, "\x00\r\n") {
		return "", errors.New("invalid profile path")
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`, nil
}

func openWorkspaceCapability(workspace string) (*os.File, uint64, uint64, error) {
	fd, err := openWorkspace(workspace)
	if err != nil {
		return nil, 0, 0, err
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return nil, 0, 0, errors.New("invalid workspace")
	}
	return os.NewFile(uintptr(fd), "workspace-root"), uint64(st.Dev), st.Ino, nil
}

func ownedByUserOrRoot(st os.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && (s.Uid == uint32(os.Getuid()) || s.Uid == 0)
}

// Refuse worker paths whose directory chain could be replaced by another
// account before the verified copy is staged.
func trustedAncestors(name string) bool {
	for dir := filepath.Dir(name); ; dir = filepath.Dir(dir) {
		st, err := os.Stat(dir)
		if err != nil || !st.IsDir() || st.Mode().Perm()&0022 != 0 || !ownedByUserOrRoot(st) {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return true
		}
	}
}

func trustedStageRoot(name string, st os.FileInfo) bool {
	mode := st.Mode().Perm()
	if mode&0022 != 0 {
		s, ok := st.Sys().(*syscall.Stat_t)
		if !ok || s.Uid != 0 || s.Mode&syscall.S_ISVTX == 0 {
			return false
		}
	}
	for dir := filepath.Dir(name); ; dir = filepath.Dir(dir) {
		parentInfo, err := os.Stat(dir)
		if err != nil || !parentInfo.IsDir() || parentInfo.Mode().Perm()&0022 != 0 || !ownedByUserOrRoot(parentInfo) {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return true
		}
	}
}
