package install

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsureUnixCLIReplacesPythonShim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shim test")
	}
	home := t.TempDir()
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(binDir, "aok")
	target := filepath.Join(t.TempDir(), "scripts", "aok")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("#!/usr/bin/env python3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, shim); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(t.TempDir(), "aok-bin")
	payload := []byte("fake-go-binary-content")
	if err := os.WriteFile(src, payload, 0o755); err != nil {
		t.Fatal(err)
	}

	path, mode, err := ensureUnixCLI(binDir, src, true)
	if err != nil {
		t.Fatal(err)
	}
	if mode != "binary" {
		t.Fatalf("mode=%q", mode)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("binary not installed")
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("PATH entry should be a binary copy, not a symlink")
	}
}

func TestEnsureUnixCLIRefusesWithoutReplace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix shim test")
	}
	binDir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(binDir, "aok")
	if err := os.WriteFile(dest, []byte("other"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "aok-bin")
	if err := os.WriteFile(src, []byte("new"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := ensureUnixCLI(binDir, src, false)
	if err == nil {
		t.Fatal("expected refuse error")
	}
}
