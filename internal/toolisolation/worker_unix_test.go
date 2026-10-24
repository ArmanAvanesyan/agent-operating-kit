//go:build linux || darwin

package toolisolation

import (
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestExecuteWithRootFDRequiresBoundIdentity(t *testing.T) {
	rootPath, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fd, err := openWorkspace(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		t.Fatal(err)
	}
	p := testPolicy(rootPath)
	p.WorkspaceDevice, p.WorkspaceInode = uint64(st.Dev), st.Ino
	if _, err := ExecuteWithRootFD(p, Request{Operation: "create-target"}, fd); err != nil {
		t.Fatal(err)
	}
	p.WorkspaceInode++
	if _, err := ExecuteWithRootFD(p, Request{Operation: "create-target"}, fd); err == nil {
		t.Fatal("mismatched root descriptor accepted")
	}
}
