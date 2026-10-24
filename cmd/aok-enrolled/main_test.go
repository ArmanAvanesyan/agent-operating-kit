package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestStockWorkerCommandBlocksBeforeConfigRead(t *testing.T) {
	t.Setenv("PATH", "")
	compiledRootSHA256 = ""
	for _, command := range []string{"worker", "qualify"} {
		err := run([]string{command, "--config", "/no/such/config", "--root", "/no/such/root"}, os.Stdout, os.Stderr)
		if err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Fatalf("stock worker was not blocked before loading config: %v", err)
		}
		var coded *commandError
		if !errors.As(err, &coded) || coded.code != 1 {
			t.Fatalf("blocked execution must use exit code 1: %v", err)
		}
	}
}

func TestInspectRequiresExplicitAsOfAndHelpIsSafe(t *testing.T) {
	t.Setenv("PATH", "")
	err := run([]string{"inspect", "--config", "missing", "--root", "missing"}, os.Stdout, os.Stderr)
	var coded *commandError
	if !errors.As(err, &coded) || coded.code != 2 || !strings.Contains(err.Error(), "--as-of") {
		t.Fatalf("expected command misuse for absent as-of: %v", err)
	}
	if err = run([]string{"inspect", "--help"}, os.Stdout, os.Stderr); err != nil {
		t.Fatalf("help should work without a configured root: %v", err)
	}
}
