package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReservationWritesWithoutReplacingIntentOrExistingFiles(t *testing.T) {
	parent := realTemp(t)
	out := filepath.Join(parent, "attempt")
	r, err := Reserve(out)
	if err != nil {
		t.Fatal(err)
	}
	intentPath := filepath.Join(out, "launch-intent.json")
	intent, err := os.ReadFile(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(intent) != "{\"status\":\"starting\",\"authorizationGranted\":false,\"executionReady\":false}\n" {
		t.Fatalf("unexpected launch intent: %s", intent)
	}
	if err = r.Write(map[string][]byte{"transcripts/attempt.json": []byte(`{"status":"blocked"}`)}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "transcripts", "attempt.json"))
	if err != nil || string(got) != `{"status":"blocked"}` {
		t.Fatalf("write result %q: %v", got, err)
	}
	if err = r.Write(map[string][]byte{"existing.json": []byte("old")}); err != nil {
		t.Fatal(err)
	}
	if err = r.Write(map[string][]byte{"existing.json": []byte("new")}); err == nil {
		t.Fatal("overwrote an existing evidence file")
	}
	got, err = os.ReadFile(filepath.Join(out, "existing.json"))
	if err != nil || string(got) != "old" {
		t.Fatalf("existing file changed: %q %v", got, err)
	}
}

func TestReservationPrevalidatesIntentAndPortablePathCollisions(t *testing.T) {
	for _, files := range []map[string][]byte{
		{"a.json": []byte("partial"), "launch-intent.json": []byte("replacement")},
		{"a": {}, "a/b": {}},
		{"A": {}, "a": {}},
		{"../escape": {}},
		{"/absolute": {}},
		{"nested/../../escape": {}},
		{"NUL.txt": {}},
		{"bad\\name": {}},
	} {
		root := realTemp(t)
		r, err := Reserve(filepath.Join(root, "attempt"))
		if err != nil {
			t.Fatal(err)
		}
		intentPath := filepath.Join(r.root, "launch-intent.json")
		before, _ := os.ReadFile(intentPath)
		if err = r.Write(files); err == nil {
			t.Fatalf("unsafe reservation write accepted: %v", files)
		}
		if after, e := os.ReadFile(intentPath); e != nil || string(after) != string(before) {
			t.Fatalf("launch intent changed for %v: %v", files, e)
		}
		if _, e := os.Stat(filepath.Join(r.root, "a.json")); !os.IsNotExist(e) {
			t.Fatalf("partial output created for %v: %v", files, e)
		}
	}
}

func TestReservationRejectsChangedDirectoryAndSymlinkRoot(t *testing.T) {
	t.Run("replaced-directory", func(t *testing.T) {
		parent := realTemp(t)
		out := filepath.Join(parent, "attempt")
		r, err := Reserve(out)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(out, out+"-old"); err != nil {
			t.Fatal(err)
		}
		if err = os.Mkdir(out, 0700); err != nil {
			t.Fatal(err)
		}
		if err = r.Write(map[string][]byte{"must-not-exist": []byte("x")}); err == nil {
			t.Fatal("accepted replacement directory")
		}
		if _, err = os.Stat(filepath.Join(out, "must-not-exist")); !os.IsNotExist(err) {
			t.Fatalf("wrote into replacement root: %v", err)
		}
	})
	t.Run("symlink-root", func(t *testing.T) {
		parent := realTemp(t)
		out := filepath.Join(parent, "attempt")
		target := filepath.Join(parent, "target")
		r, err := Reserve(out)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.Rename(out, out+"-old"); err != nil {
			t.Fatal(err)
		}
		if err = os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.Symlink(target, out); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err = r.Write(map[string][]byte{"must-not-exist": []byte("x")}); err == nil {
			t.Fatal("accepted symlink replacement root")
		}
		if _, err = os.Stat(filepath.Join(target, "must-not-exist")); !os.IsNotExist(err) {
			t.Fatalf("followed replacement symlink: %v", err)
		}
	})
}

func TestReservationRejectsNestedSymlinkBeforeWriting(t *testing.T) {
	parent := realTemp(t)
	out := filepath.Join(parent, "attempt")
	target := filepath.Join(parent, "outside")
	r, err := Reserve(out)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, filepath.Join(out, "nested")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err = r.Write(map[string][]byte{"nested/pwned": []byte("x")}); err == nil {
		t.Fatal("followed nested symlink")
	}
	if _, err = os.Stat(filepath.Join(target, "pwned")); !os.IsNotExist(err) {
		t.Fatalf("wrote through nested symlink: %v", err)
	}
}
