package toolisolation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func testPolicy(root string) Policy {
	root, _ = filepath.EvalSymlinks(root)
	return Policy{Version: Version, Workspace: root, Target: "pilot-output.txt", WorkerExecutable: "/tmp/aok-worker", WorkerSHA256: hex.EncodeToString(make([]byte, sha256.Size)), TimeoutSeconds: 10, ReadPaths: []string{"input.txt", "large"}}
}

func TestExecuteCreateAndRead(t *testing.T) {
	root := t.TempDir()
	p := testPolicy(root)
	r, err := Execute(p, Request{Operation: "create-target"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != p.Target || r.Size != len(createFixture) {
		t.Fatalf("unexpected result: %+v", r)
	}
	b, err := os.ReadFile(filepath.Join(root, p.Target))
	if err != nil || string(b) != createFixture {
		t.Fatalf("created content %q, err %v", b, err)
	}
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	r, err = Execute(p, Request{Operation: "read", Path: "input.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Data) != "fixture" || r.Size != 7 {
		t.Fatalf("unexpected read: %+v", r)
	}
}

func TestExecuteRejectsUnsafeFiles(t *testing.T) {
	root := t.TempDir()
	p := testPolicy(root)
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Execute(p, Request{Operation: "read", Path: "link"}); err == nil {
		t.Fatal("symlink read accepted")
	}
	if err := os.Link(outside, filepath.Join(root, "hardlink")); err == nil {
		if _, err := Execute(p, Request{Operation: "read", Path: "hardlink"}); err == nil {
			t.Fatal("hardlink read accepted")
		}
	}
	if err := os.Symlink(filepath.Dir(outside), filepath.Join(root, "dirlink")); err == nil {
		if _, err := Execute(p, Request{Operation: "read", Path: "dirlink/secret"}); err == nil {
			t.Fatal("symlinked directory read accepted")
		}
	}
	for _, name := range []string{"../secret", "/etc/passwd", ".", "a/../secret"} {
		if _, err := Execute(p, Request{Operation: "read", Path: name}); err == nil {
			t.Errorf("unsafe path accepted: %q", name)
		}
	}
	if _, err := Execute(p, Request{Operation: "read", Path: "unlisted.txt"}); err == nil {
		t.Fatal("unlisted workspace file read accepted")
	}
	if _, err := Execute(p, Request{Operation: "create-target"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(p, Request{Operation: "create-target"}); err == nil {
		t.Fatal("existing target was overwritten")
	}
}

func TestValidatePolicyRequiresPrivateRootAndExternalWorker(t *testing.T) {
	p := testPolicy(t.TempDir())
	p.Workspace = string(filepath.Separator)
	if ValidatePolicy(p) == nil {
		t.Fatal("filesystem root accepted as workspace")
	}
	p = testPolicy(t.TempDir())
	p.WorkerExecutable = filepath.Join(p.Workspace, "worker")
	if ValidatePolicy(p) == nil {
		t.Fatal("worker inside workspace accepted")
	}
}

func TestEmptyReadPathsDenyAllReads(t *testing.T) {
	root := t.TempDir()
	p := testPolicy(root)
	p.ReadPaths = nil
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(p, Request{Operation: "read", Path: "input.txt"}); err == nil {
		t.Fatal("read succeeded with empty allowlist")
	}
}

func TestDecodeEnvelopeRejectsMalformedAndUnknownFields(t *testing.T) {
	for _, input := range []string{
		`{"policy":{},"request":{},"request":{}}`,
		`{"policy":{},"request":{"operation":"read","future":true}}`,
		`{"policy":{},"request":{}} trailing`,
	} {
		if _, _, err := DecodeEnvelope(bytes.NewBufferString(input)); err == nil {
			t.Errorf("accepted malformed envelope: %s", input)
		}
	}
	oversized := bytes.NewReader(bytes.Repeat([]byte("x"), maxRequestBytes+1))
	if _, _, err := DecodeEnvelope(oversized); err == nil {
		t.Fatal("oversized envelope accepted")
	}
}

func TestReadRejectsOversizedFileAndInvalidRequest(t *testing.T) {
	root := t.TempDir()
	p := testPolicy(root)
	if err := os.WriteFile(filepath.Join(root, "large"), bytes.Repeat([]byte{'x'}, maxReadBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Execute(p, Request{Operation: "read", Path: "large"}); err == nil {
		t.Fatal("oversized read accepted")
	}
	if err := ValidateRequest(Request{Operation: "create-target", Data: []byte("other")}); err == nil {
		t.Fatal("arbitrary create bytes accepted")
	}
}
