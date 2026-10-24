//go:build darwin

package toolisolation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunUsesPinnedWorkerAndFixedTarget(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Seatbelt launcher is Darwin-only")
	}
	if os.Getenv("AOK_TOOL_ISOLATION_OS_TESTS") != "1" {
		t.Skip("set AOK_TOOL_ISOLATION_OS_TESTS=1 to run OS sandbox integration tests")
	}
	root := privateTestTempDir(t)
	worker := filepath.Join(root, "aok-tool-worker")
	build := exec.Command("go", "build", "-ldflags=-B=gobuildid", "-o", worker, "../../cmd/aok-tool-worker")
	build.Env = append(os.Environ(), "GOCACHE=/private/tmp/aok-integration-go-cache", "TMPDIR=/private/tmp")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build worker: %v: %s", err, output)
	}
	b, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	// Confirm the staged Go helper starts normally before running the confined
	// operation. Malformed input fails before any workspace access.
	startup := exec.Command(worker)
	startup.Env = []string{}
	startup.Stdin = strings.NewReader("{}\n")
	if err := startup.Run(); err == nil {
		t.Fatal("malformed envelope unexpectedly succeeded")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("worker startup failed unexpectedly: %v", err)
	}
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	profileHash, err := dyldSupportSHA256()
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{Version: Version, Workspace: workspace, Target: "pilot-output.txt", WorkerExecutable: worker, WorkerSHA256: hex.EncodeToString(sum[:]), RuntimeProfileSHA256: profileHash, TimeoutSeconds: 5, ReadPaths: []string{"pilot-output.txt"}}
	result, err := Run(context.Background(), p, Request{Operation: "create-target"})
	if err != nil {
		class := diagnoseFiniteProfileStartup(t, p, worker, workspace)
		t.Fatalf("create-target failed (%s): %v", class, err)
	}
	if result.Path != p.Target || result.Size != len(createFixture) {
		t.Fatalf("unexpected result: %#v", result)
	}
	got, err := os.ReadFile(filepath.Join(workspace, p.Target))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != createFixture {
		t.Fatalf("unexpected target contents: %q", got)
	}
	read, err := Run(context.Background(), p, Request{Operation: "read", Path: p.Target})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(read.Data) != createFixture || read.Size != len(createFixture) {
		t.Fatalf("unexpected read result: %#v", read)
	}
	if _, err := Run(context.Background(), p, Request{Operation: "create-target"}); err == nil {
		t.Fatal("second create unexpectedly succeeded")
	}
}

// diagnoseFiniteProfileStartup is used only by this synthetic OS integration
// test. It runs a separately staged worker with the production finite profile,
// captures bounded stderr, and reports only a coarse failure class.
func diagnoseFiniteProfileStartup(t *testing.T, p Policy, worker, workspace string) string {
	t.Helper()
	stageDir, staged, err := stageWorker(worker, p.WorkerSHA256)
	if err != nil {
		return "stage-failed"
	}
	defer os.RemoveAll(stageDir)
	root, dev, ino, err := openWorkspaceCapability(workspace)
	if err != nil {
		return "workspace-capability-failed"
	}
	defer root.Close()
	p.WorkerExecutable, p.WorkspaceDevice, p.WorkspaceInode = staged, dev, ino
	profile, err := seatbeltProfile(p)
	if err != nil {
		return "profile-generation-failed"
	}
	input, err := json.Marshal(Envelope{Policy: p, Request: Request{Operation: "create-target"}})
	if err != nil {
		return "envelope-failed"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.Command("/usr/bin/sandbox-exec", "-p", profile, staged)
	cmd.Dir = workspace
	cmd.Env = []string{}
	cmd.Stdin = bytes.NewReader(input)
	cmd.ExtraFiles = []*os.File{root}
	var stderr cappedOutput
	stderr.limit = 4096
	cmd.Stderr = &stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err = cmd.Start(); err != nil {
		return classifySandboxFailure(err, nil, false)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err = <-done:
	case <-ctx.Done():
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		select {
		case err = <-done:
		case <-time.After(2 * time.Second):
			return "child-timeout"
		}
	}
	if err == nil {
		return "worker-completed"
	}
	b, overflow := stderr.bytes()
	return classifySandboxFailure(err, b, overflow)
}

func classifySandboxFailure(err error, stderr []byte, overflow bool) string {
	if overflow {
		return "stderr-overflow"
	}
	text := strings.ToLower(string(stderr))
	switch {
	case strings.Contains(text, "profile") && (strings.Contains(text, "parse") || strings.Contains(text, "syntax") || strings.Contains(text, "invalid")):
		return "profile-rejected"
	case strings.Contains(text, "sandbox_apply") || strings.Contains(text, "sandbox_init"):
		return "sandbox-attach-denied"
	case strings.Contains(text, "execvp"):
		return "execvp-denied"
	case strings.Contains(text, "sandbox-exec") && (strings.Contains(text, "denied") || strings.Contains(text, "not permitted")):
		return "sandbox-exec-denied"
	case strings.Contains(text, "dyld"):
		return "dyld-startup"
	case strings.Contains(text, "permission denied") || strings.Contains(text, "operation not permitted"):
		return "permission-denied"
	case strings.Contains(text, "tool request denied"):
		return "worker-request-denied"
	default:
		if exit, ok := err.(*exec.ExitError); ok {
			if status, ok := exit.Sys().(syscall.WaitStatus); ok && status.Signaled() {
				return "child-signal-" + status.Signal().String()
			}
			return fmt.Sprintf("child-exit-%d", exit.ExitCode())
		}
		return "launch-error"
	}
}

func TestClassifySandboxFailure(t *testing.T) {
	cases := []struct {
		name   string
		stderr string
		want   string
	}{
		{"profile", "sandbox-exec: invalid profile syntax", "profile-rejected"},
		{"sandbox attach", "sandbox_apply: Operation not permitted", "sandbox-attach-denied"},
		{"exec transition", "execvp: Operation not permitted", "execvp-denied"},
		{"dyld", "dyld: startup failure", "dyld-startup"},
		{"worker operation", "tool request denied", "worker-request-denied"},
		{"permission", "Operation not permitted", "permission-denied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifySandboxFailure(errors.New("synthetic child failure"), []byte(tc.stderr), false); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	if got := classifySandboxFailure(errors.New("synthetic child failure"), nil, true); got != "stderr-overflow" {
		t.Fatalf("overflow class = %q", got)
	}
}

func TestRunDenyDefaultProbe(t *testing.T) {
	if runtime.GOOS != "darwin" || os.Getenv("AOK_TOOL_ISOLATION_OS_TESTS") != "1" {
		t.Skip("set AOK_TOOL_ISOLATION_OS_TESTS=1 on Darwin to run OS sandbox integration tests")
	}
	root := privateTestTempDir(t)
	var err error
	workspace := filepath.Join(root, "workspace")
	if err = os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(workspace, "approved.txt"), []byte("approved"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(workspace, "unapproved.txt"), []byte("workspace private marker"), 0600); err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(root, "private.txt")
	if err = os.WriteFile(privatePath, []byte("external private marker"), 0600); err != nil {
		t.Fatal(err)
	}
	otherWrite := filepath.Join(root, "outside-write.txt")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	addr := listener.Addr().String()
	worker := filepath.Join(root, "probe-worker")
	source := fmt.Sprintf(`package main
import("crypto/sha256";"encoding/hex";"encoding/json";"net";"os";"time")
const privatePath=%q
const unapprovedPath=%q
const otherWrite=%q
const networkAddr=%q
type env struct { Request struct { Operation string; Path string } }
type result struct { Operation string; Path string; Size int; SHA256 string; Data []byte }
func main(){ var e env; if json.NewDecoder(os.Stdin).Decode(&e)!=nil {os.Exit(2)}
 _,privateErr:=os.ReadFile(privatePath); _,unapprovedErr:=os.ReadFile(unapprovedPath); _,approvedErr:=os.ReadFile(%q)
 writeErr:=os.WriteFile(otherWrite,[]byte("forbidden"),0600)
 conn,networkErr:=net.DialTimeout("tcp",networkAddr,100*time.Millisecond); if conn!=nil {conn.Close()}
 raw,_:=json.Marshal(map[string]bool{"private_read":privateErr==nil,"unapproved_read":unapprovedErr==nil,"approved_read":approvedErr==nil,"outside_write":writeErr==nil,"network":networkErr==nil,"environment":os.Getenv("AOK_TEST_PARENT_AUTH_CANARY")!=""})
 sum:=sha256.Sum256(raw); _=json.NewEncoder(os.Stdout).Encode(result{Operation:e.Request.Operation,Path:e.Request.Path,Size:len(raw),SHA256:hex.EncodeToString(sum[:]),Data:raw})
}`, privatePath, filepath.Join(workspace, "unapproved.txt"), filepath.Join(workspace, "approved.txt"), otherWrite, addr)
	sourcePath := filepath.Join(root, "probe-worker.go")
	if err = os.WriteFile(sourcePath, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-ldflags=-B=gobuildid", "-o", worker, sourcePath)
	build.Env = append(os.Environ(), "GOCACHE=/private/tmp/aok-integration-go-cache", "TMPDIR=/private/tmp")
	if output, buildErr := build.CombinedOutput(); buildErr != nil {
		t.Fatalf("build probe: %v: %s", buildErr, output)
	}
	b, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	profileHash, err := dyldSupportSHA256()
	if err != nil {
		t.Fatal(err)
	}
	p := Policy{Version: Version, Workspace: workspace, Target: "pilot-output.txt", WorkerExecutable: worker, WorkerSHA256: hex.EncodeToString(sum[:]), RuntimeProfileSHA256: profileHash, TimeoutSeconds: 5, ReadPaths: []string{"approved.txt"}}
	t.Setenv("AOK_TEST_PARENT_AUTH_CANARY", "synthetic-parent-canary")
	out, err := Run(context.Background(), p, Request{Operation: "read", Path: "approved.txt"})
	if err != nil {
		t.Fatalf("probe run (%s): %v", diagnoseFiniteProfileStartup(t, p, worker, workspace), err)
	}
	var observed map[string]bool
	if err = json.Unmarshal(out.Data, &observed); err != nil {
		t.Fatal(err)
	}
	if !observed["approved_read"] {
		t.Error("sandbox denied the approved synthetic read")
	}
	for _, probe := range []string{"private_read", "unapproved_read", "outside_write", "network", "environment"} {
		if observed[probe] {
			t.Errorf("sandbox allowed %s", probe)
		}
	}
	if len(observed) != 6 {
		t.Fatalf("incomplete probe results: %#v", observed)
	}
	if _, err = os.Stat(otherWrite); !os.IsNotExist(err) {
		t.Fatalf("outside write artifact exists or stat failed: %v", err)
	}
}

func privateTestTempDir(t *testing.T) string {
	t.Helper()
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(cache, "aok-tool-isolation-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	canonical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func TestSeatbeltProfileIsDenyByDefault(t *testing.T) {
	p := Policy{Workspace: "/tmp/aok-work", WorkerExecutable: "/tmp/aok-worker", Target: "pilot-output.txt", ReadPaths: []string{"approved.txt"}}
	profile, err := seatbeltProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{"(deny default)", "(deny network*)", "(allow file-write* (literal \"/tmp/aok-work/pilot-output.txt\")"} {
		if !strings.Contains(profile, needle) {
			t.Fatalf("profile missing %q", needle)
		}
	}
	for _, forbidden := range []string{
		"(allow default)", "(allow syscall-unix)", "(allow syscall*)",
		"(allow process-exec)", "(allow process-exec*)", "(allow process-exec-interpreter)", "with no-sandbox",
		"(subpath \"/tmp\")", "HOME",
	} {
		if strings.Contains(profile, forbidden) {
			t.Fatalf("profile contains broad or ambient grant %q", forbidden)
		}
	}
	for _, expected := range []string{
		"(allow syscall-unix (syscall-number SYS_execve SYS___mac_execve SYS_posix_spawn))",
		"(allow process-exec (literal \"/tmp/aok-worker\"))",
		"(allow process-exec-interpreter (literal \"/usr/lib/dyld\"))",
		"(allow file-read-metadata file-test-existence (literal \"/tmp\"))",
		"(allow file-read* file-test-existence (literal \"/tmp/aok-worker\")",
		"(allow file-read-data file-read-metadata file-test-existence (literal \"/tmp/aok-work/approved.txt\"))",
	} {
		if !strings.Contains(profile, expected) {
			t.Fatalf("profile missing finite launch grant %q", expected)
		}
	}
}
