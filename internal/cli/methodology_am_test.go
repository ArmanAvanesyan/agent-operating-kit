package cli

import (
	"bytes"
	"path/filepath"
	"testing"
)

func TestAMOverlayIsExplicitGuidanceOnly(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("AOK_ROOT", root)
	base := renderMethodologyPolicy(root, "kanban", nil)
	if bytes.Contains([]byte(base), []byte("Lightweight modeling")) {
		t.Fatal("AM guidance leaked into default policy")
	}
	selected := renderMethodologyPolicy(root, "kanban", []any{"am"})
	if !bytes.Contains([]byte(selected), []byte("Lightweight modeling")) || !bytes.Contains([]byte(selected), []byte("supplies no lifecycle transition")) {
		t.Fatalf("AM guidance missing or misleading: %s", selected)
	}
	var out, stderr bytes.Buffer
	if code, err := runWorkItem([]string{"list", "--flow", "kanban", "--overlay", "am", "--json"}, &out, &stderr); err != nil || code != 0 {
		t.Fatalf("explicit AM selection rejected: code=%d err=%v stderr=%s", code, err, stderr.String())
	}
	if _, err := strictReadFile(filepath.Join(root, "methodologies", "am", "templates", "modeling-note.md")); err != nil {
		t.Fatal(err)
	}
}
