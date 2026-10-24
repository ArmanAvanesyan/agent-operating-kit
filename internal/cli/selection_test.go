package cli

import (
	"bytes"
	"testing"
)

func TestSelectionCommandMisuseAndHelp(t *testing.T) {
	for _, args := range [][]string{{"models", "select", "--unknown", "x"}, {"evaluation", "estimate", "--manifest", "x"}, {"evaluation", "replay", "--dataset"}, {"evaluation", "run"}} {
		var out, errout bytes.Buffer
		if got := Run(args, &out, &errout); got != 2 {
			t.Fatalf("%v got%d %s", args, got, errout.String())
		}
	}
	for _, args := range [][]string{{"models", "select", "--help"}, {"evaluation", "--help"}, {"evaluation", "estimate", "--help"}} {
		var out, errout bytes.Buffer
		if got := Run(args, &out, &errout); got != 0 {
			t.Fatalf("%v got%d %s", args, got, errout.String())
		}
	}
}
