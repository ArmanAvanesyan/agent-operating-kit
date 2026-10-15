//go:build linux || darwin

package agentvalidate

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestSchemaFIFORejected(t *testing.T) {
	root := validCorpus(t, nil)
	fifo := filepath.Join(root, "assets", "pipe.schema.json")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	report := Validate(root)
	if report.Status != "invalid" || !hasCode(report, "schema.compile") {
		t.Fatalf("FIFO schema accepted: %+v", report)
	}
}
