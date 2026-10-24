package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareAndPreflightStayOfflineAndBlocked(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("AOK_GH_WRITE_TOKEN", "private-canary-not-for-enrollment")
	root, pathErr := filepath.EvalSymlinks(t.TempDir())
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	dir := filepath.Join(root, "enrollment")
	var out, errout bytes.Buffer
	if err := runEnrollment("prepare", []string{"--out", dir}, &out, &errout); err != nil {
		t.Fatal(err)
	}
	var prepared map[string]any
	if json.Unmarshal(out.Bytes(), &prepared) != nil || prepared["executionReady"] != false || prepared["status"] != "drafts-prepared" {
		t.Fatal("preparation implied readiness")
	}
	out.Reset()
	err := runEnrollment("preflight", []string{"--bundle", dir, "--as-of", "2026-10-24T12:00:00Z", "--json"}, &out, &errout)
	var coded *commandError
	if !errors.As(err, &coded) || coded.code != 1 {
		t.Fatalf("missing inputs must block: %v", err)
	}
	var report map[string]any
	if json.Unmarshal(out.Bytes(), &report) != nil || report["authorizationGranted"] != false || report["executionReady"] != false || report["productionEligible"] != false {
		t.Fatal("preflight implied permission")
	}
	if bytes.Contains(out.Bytes(), []byte("private-canary-not-for-enrollment")) {
		t.Fatal("environment credential leaked")
	}
	if _, err := os.Stat(filepath.Join(dir, "signed")); !os.IsNotExist(err) {
		t.Fatal("preparation manufactured signed authority")
	}
	if err := runEnrollment("prepare", []string{"--out", dir}, &out, &errout); err == nil {
		t.Fatal("preparation overwrote existing files")
	}
}

func TestEnrollmentCommandMisuseDoesNotCreateFiles(t *testing.T) {
	var out, errout bytes.Buffer
	for _, args := range [][]string{{"--bundle", "missing"}, {"--bundle", "missing", "--as-of", "yesterday"}} {
		err := runEnrollment("preflight", args, &out, &errout)
		var coded *commandError
		if !errors.As(err, &coded) || coded.code != 2 {
			t.Fatalf("expected command misuse: %v", err)
		}
	}
}
