package evidence

import (
	"os"
	"path/filepath"
	"testing"
)

func realTemp(t *testing.T) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestWriteReportAndNoOverwrite(t *testing.T) {
	root := realTemp(t)
	out := filepath.Join(root, "report")
	value := map[string]string{"status": "blocked"}
	if err := WriteJSON(out, "resolution.json", value); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(out, "resolution.json"))
	if err := WriteJSON(out, "resolution.json", map[string]string{"status": "valid"}); err == nil {
		t.Fatal("overwrote output")
	}
	after, _ := os.ReadFile(filepath.Join(out, "resolution.json"))
	if string(before) != string(after) {
		t.Fatal("existing output changed")
	}
}
func TestRejectProtectedAndSymlinkOutputs(t *testing.T) {
	root := realTemp(t)
	corpus := filepath.Join(root, "corpus")
	if err := os.Mkdir(corpus, 0700); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(filepath.Join(corpus, "out"), "report.json", nil, corpus); err == nil {
		t.Fatal("wrote into corpus")
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(corpus, link); err != nil {
		t.Skip(err)
	}
	if err := WriteJSON(filepath.Join(link, "out"), "report.json", nil); err == nil {
		t.Fatal("followed symlink")
	}
	if err := WriteJSON(filepath.Join(root, "bad"), "../escape", nil); err == nil {
		t.Fatal("escaped output")
	}
}
