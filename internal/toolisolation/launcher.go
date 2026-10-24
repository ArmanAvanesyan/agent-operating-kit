//go:build darwin

package toolisolation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

const maxLauncherOutput = 8 << 20

// Run executes one policy-bound worker request inside the platform sandbox.
// The policy is supplied by the trusted caller; the request cannot select a
// worker, workspace, environment, or executable arguments.
func Run(ctx context.Context, p Policy, q Request) (Result, error) {
	var zero Result
	if ctx == nil || ctx.Err() != nil {
		return zero, errors.New("request denied")
	}
	if ValidatePolicy(p) != nil || ValidateRequest(q) != nil {
		return zero, errors.New("request denied")
	}
	runtimeHash, err := dyldSupportSHA256()
	if err != nil || p.RuntimeProfileSHA256 == "" || p.RuntimeProfileSHA256 != runtimeHash {
		return zero, errors.New("request denied")
	}
	if q.Operation == "read" && ValidateReadRequest(p, q) != nil {
		return zero, errors.New("request denied")
	}
	workspace, err := filepath.EvalSymlinks(p.Workspace)
	if err != nil || workspace != p.Workspace {
		return zero, errors.New("request denied")
	}
	st, err := os.Stat(workspace)
	if err != nil || !st.IsDir() {
		return zero, errors.New("request denied")
	}
	executable, err := filepath.EvalSymlinks(p.WorkerExecutable)
	if err != nil || executable != p.WorkerExecutable {
		return zero, errors.New("request denied")
	}
	st, err = os.Stat(executable)
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 || st.Mode().Perm()&0022 != 0 || st.Size() > 512<<20 || !ownedByUserOrRoot(st) {
		return zero, errors.New("request denied")
	}
	if !trustedAncestors(executable) {
		return zero, errors.New("request denied")
	}
	if rel, relErr := filepath.Rel(workspace, executable); relErr != nil || rel == "." || (rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return zero, errors.New("request denied")
	}
	rootFile, rootDevice, rootInode, err := openWorkspaceCapability(workspace)
	if err != nil {
		return zero, errors.New("request denied")
	}
	defer rootFile.Close()
	stageDir, stagedWorker, err := stageWorker(executable, p.WorkerSHA256)
	if err != nil {
		return zero, errors.New("request denied")
	}
	defer os.RemoveAll(stageDir)
	policyForWorker := p
	policyForWorker.WorkerExecutable = stagedWorker
	policyForWorker.WorkspaceDevice = rootDevice
	policyForWorker.WorkspaceInode = rootInode
	env := Envelope{Policy: policyForWorker, Request: q}
	input, err := json.Marshal(env)
	if err != nil {
		return zero, errors.New("request denied")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutSeconds)*time.Second)
	defer cancel()
	b, err := runSandboxed(ctx, policyForWorker, input, rootFile)
	if err != nil {
		return zero, errors.New("request denied")
	}
	var result Result
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if _, err := contracts.DecodeJSON(b); err != nil || d.Decode(&result) != nil || d.Decode(new(any)) != io.EOF || result.Operation != q.Operation {
		return zero, errors.New("request denied")
	}
	if q.Operation == "create-target" && result.Path != p.Target {
		return zero, errors.New("request denied")
	}
	if q.Operation == "read" && result.Path != q.Path {
		return zero, errors.New("request denied")
	}
	if result.Size < 0 || result.Size > maxReadBytes || (q.Operation == "create-target" && result.Size != len(createFixture)) {
		return zero, errors.New("request denied")
	}
	var want []byte
	if q.Operation == "read" {
		if result.Size != len(result.Data) {
			return zero, errors.New("request denied")
		}
		want = result.Data
	} else {
		want = []byte(createFixture)
		if len(result.Data) != 0 {
			return zero, errors.New("request denied")
		}
		if result.Size != len(want) {
			return zero, errors.New("request denied")
		}
	}
	sum := sha256.Sum256(want)
	if result.SHA256 != hex.EncodeToString(sum[:]) {
		return zero, errors.New("request denied")
	}
	return result, nil
}

func stageWorker(source, expectedSHA string) (string, string, error) {
	tmp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", "", err
	}
	tmpInfo, err := os.Stat(tmp)
	if err != nil || !tmpInfo.IsDir() || !ownedByUserOrRoot(tmpInfo) || !trustedStageRoot(tmp, tmpInfo) {
		return "", "", errors.New("unsafe staging root")
	}
	dir, err := os.MkdirTemp(tmp, "aok-tool-worker-")
	if err != nil {
		return "", "", err
	}
	cleanup := func(e error) (string, string, error) { _ = os.RemoveAll(dir); return "", "", e }
	if err = os.Chmod(dir, 0700); err != nil {
		return cleanup(err)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil || dirInfo.Mode().Perm() != 0700 || !ownedByUserOrRoot(dirInfo) {
		return cleanup(errors.New("unsafe staging directory"))
	}
	path := filepath.Join(dir, "worker")
	sourceFD, err := syscall.Open(source, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return cleanup(err)
	}
	in := os.NewFile(uintptr(sourceFD), "worker-source")
	info, err := in.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > 512<<20 || info.Mode().Perm()&0022 != 0 || !ownedByUserOrRoot(info) {
		_ = in.Close()
		return cleanup(errors.New("unsafe worker source"))
	}
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		_ = in.Close()
		return cleanup(err)
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(in, (512<<20)+1))
	closeIn := in.Close()
	if copyErr != nil || closeIn != nil || n <= 0 || n > 512<<20 || hex.EncodeToString(h.Sum(nil)) != expectedSHA {
		_ = out.Close()
		return cleanup(errors.New("worker hash mismatch"))
	}
	if err = out.Sync(); err != nil {
		_ = out.Close()
		return cleanup(err)
	}
	if err = out.Chmod(0500); err != nil {
		_ = out.Close()
		return cleanup(err)
	}
	if err = out.Close(); err != nil {
		return cleanup(err)
	}
	return dir, path, nil
}
