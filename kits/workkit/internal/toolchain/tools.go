// Package toolchain verifies explicitly pinned tools. It never searches PATH,
// installs extensions during inspection, or executes manifest-supplied argv.
package toolchain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const MaxBinaryBytes = 256 << 20

func Platform() string { return runtime.GOOS + "-" + runtime.GOARCH }
func HashFile(ctx context.Context, path string, max int64) (string, error) {
	if !filepath.IsAbs(path) || max < 1 || max > MaxBinaryBytes {
		return "", errors.New("absolute bounded file required")
	}
	p, e := engine.SafePath(path)
	if e != nil {
		return "", errors.New("unsafe file")
	}
	before, e := os.Stat(p)
	if e != nil || !before.Mode().IsRegular() || before.Size() > max {
		return "", errors.New("invalid file identity or size")
	}
	f, e := os.Open(p)
	if e != nil {
		return "", errors.New("file unavailable")
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(before, opened) {
		return "", errors.New("file changed")
	}
	h := sha256.New()
	buf := make([]byte, 64<<10)
	var total int64
	for {
		if e = ctx.Err(); e != nil {
			return "", e
		}
		n, re := f.Read(buf)
		total += int64(n)
		if total > max {
			return "", errors.New("file limit")
		}
		h.Write(buf[:n])
		if re == io.EOF {
			break
		}
		if re != nil {
			return "", errors.New("file read failed")
		}
	}
	after, e := os.Stat(p)
	if e != nil || !os.SameFile(opened, after) || after.Size() != opened.Size() || !after.ModTime().Equal(opened.ModTime()) {
		return "", errors.New("file changed")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func Verify(ctx context.Context, p a.ToolPin) error {
	if p.Platform != Platform() || !Known(p.ID) || len(p.SHA256) != 64 || p.VersionOutput == "" || len(p.VersionOutput) > 16384 {
		return errors.New("invalid tool pin")
	}
	hash, e := HashFile(ctx, p.Path, MaxBinaryBytes)
	if e != nil || hash != p.SHA256 {
		return errors.New("tool pin mismatch")
	}
	if p.Runtime != nil {
		rel, e := filepath.Rel(p.Runtime.Path, p.Path)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			return errors.New("runtime does not contain executable")
		}
		hash, e := HashTree(ctx, p.Runtime.Path)
		if e != nil || hash != p.Runtime.SHA256 {
			return errors.New("runtime tree drift")
		}
	}
	for _, ext := range p.Extensions {
		if ext.ID != "azure-devops" || p.ID != "az" || ext.Version == "" {
			return errors.New("unsupported extension")
		}
		var hash string
		var e error
		if ext.Layout == "tree-v1" {
			hash, e = HashTree(ctx, ext.Path)
		} else {
			hash, e = HashFile(ctx, ext.Path, MaxBinaryBytes)
		}
		if e != nil || hash != ext.SHA256 {
			return errors.New("extension pin mismatch")
		}
	}
	return nil
}
func Known(id string) bool {
	return id == "gh" || id == "az" || id == "acli" || id == "workkit-secret-broker" || id == "workkit-keychain-helper"
}

type bounded struct {
	mu       sync.Mutex
	b        []byte
	max      int
	overflow bool
}

func (b *bounded) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.b)+len(p) > b.max {
		b.overflow = true
		return 0, errors.New("output bound")
	}
	b.b = append(b.b, p...)
	return len(p), nil
}

// Run is only used with compiled argv. The input and output may contain secrets;
// callers must not expose either. Errors deliberately discard subprocess text.
func Run(ctx context.Context, p a.ToolPin, args []string, input []byte, env []string, dir string, max int, checks ...func() error) ([]byte, error) {
	return runOwned(ctx, p, args, input, env, dir, max, false, checks...)
}

// RunNested retains the broker's process group so its parent can stop the full
// chain even if the broker dies while calling its single, non-spawning helper.
func RunNested(ctx context.Context, p a.ToolPin, args []string, input []byte, env []string, dir string, max int, checks ...func() error) ([]byte, error) {
	return runOwned(ctx, p, args, input, env, dir, max, true, checks...)
}
func runOwned(ctx context.Context, p a.ToolPin, args []string, input []byte, env []string, dir string, max int, nested bool, checks ...func() error) ([]byte, error) {
	if e := Verify(ctx, p); e != nil {
		return nil, e
	}
	if max < 1 || max > 4<<20 {
		return nil, errors.New("output bound")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.Path, args...)
	cmd.Env = append([]string{}, env...)
	cmd.Dir = dir
	cmd.WaitDelay = 2 * time.Second
	cmd.Stdin = strings.NewReader(string(input))
	out := &bounded{max: max}
	errout := &bounded{max: 16384}
	cmd.Stdout = out
	cmd.Stderr = errout
	if e := func() error {
		if nested {
			return nil
		}
		return configureProcess(cmd)
	}(); e != nil {
		return nil, errors.New("process controls unavailable")
	}
	var handle uintptr
	cmd.Cancel = func() error {
		if nested {
			return cmd.Process.Kill()
		}
		return stopProcess(cmd, processHandle(cmd))
	}
	for _, check := range checks {
		if e := check(); e != nil {
			return nil, e
		}
	}
	e := cmd.Start()
	if e == nil {
		if attachErr := func() error {
			if nested {
				return nil
			}
			return attachProcess(cmd)
		}(); attachErr != nil {
			_ = stopProcess(cmd, 0)
			_ = cmd.Wait()
			return nil, errors.New("process ownership unavailable")
		}
		handle = processHandle(cmd)
		e = cmd.Wait()
		if !nested {
			_ = stopProcess(cmd, handle)
			finishProcess(cmd, handle)
		}
	}
	if e != nil || out.overflow || errout.overflow {
		return nil, errors.New("bounded tool operation failed or cancelled")
	}
	if e = Verify(ctx, p); e != nil {
		return nil, e
	}
	return out.b, nil
}
func Observe(ctx context.Context, p a.ToolPin, at time.Time, checks ...func() error) (a.Observation, error) {
	o := a.Observation{Version: a.Version, Kind: "ToolObservation", Tool: p, ObservedAt: at.UTC().Format(time.RFC3339Nano), LiveQualification: "pending"}
	if at.IsZero() {
		return o, errors.New("explicit observation time required")
	}
	if e := Verify(ctx, p); e != nil {
		return o, e
	}
	args := []string{"--version"}
	if p.ID == "az" {
		args = []string{"version", "--output", "json"}
	}
	if p.ID == "workkit-secret-broker" || p.ID == "workkit-keychain-helper" {
		args = []string{"version"}
	}
	dir, e := os.MkdirTemp("", "workkit-tool-probe-")
	if e != nil {
		return o, e
	}
	defer os.RemoveAll(dir)
	out, e := Run(ctx, p, args, nil, []string{"HOME=" + dir, "USERPROFILE=" + dir, "GH_CONFIG_DIR=" + dir, "AZURE_CONFIG_DIR=" + dir, "AZURE_EXTENSION_USE_DYNAMIC_INSTALL=no", "DO_NOT_TRACK=1"}, dir, 16384, checks...)
	if e != nil || strings.TrimSpace(string(out)) != p.VersionOutput {
		return o, errors.New("tool version probe mismatch")
	}
	// Azure extension loading must not use an unpinned tree. P3 records supplied
	// file pins but does not claim that one file authenticates an extension tree.
	for _, extension := range p.Extensions {
		if extension.Layout != "tree-v1" {
			return o, errors.New("extension tree attestation required")
		}
	}
	o.Verified = true
	return o, nil
}
