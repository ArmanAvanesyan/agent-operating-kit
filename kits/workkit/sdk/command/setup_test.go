package command

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"os"
	"path/filepath"
	"testing"
)

func TestSetupCommandsParityAndStrictFlags(t *testing.T) {
	root := "../../testdata/setup/github"
	svc := sdk.New(nil)
	args := []string{"mappings", "validate", "--mapping", root + "/mapping.json", "--mapping-context", root + "/mapping-context.json", "--source-observation", root, "--observation", root, "--as-of", "2026-10-26T00:00:01Z", "--json"}
	var stdout, stderr bytes.Buffer
	if c := Run(context.Background(), args, &stdout, &stderr, svc); c != 0 {
		t.Fatalf("%d %s %s", c, &stdout, &stderr)
	}
	var domain any
	json.Unmarshal(stdout.Bytes(), &domain)
	stdout.Reset()
	if c := RunSuite(context.Background(), args, &stdout, &stderr, svc); c != 0 {
		t.Fatal(c)
	}
	var wrapper map[string]any
	json.Unmarshal(stdout.Bytes(), &wrapper)
	actual, _ := json.Marshal(wrapper["spec"].(map[string]any)["data"])
	expected, _ := json.Marshal(domain)
	if !bytes.Equal(actual, expected) {
		t.Fatal("lossy envelope")
	}
	for _, args := range [][]string{{"setup", "plan", "--grant", "bad"}, {"mappings", "validate", "--mapping", "missing", "--out", "unused"}, {"setup", "verify", "--write-binding", "--as-of", "2026-10-26T00:00:01Z"}} {
		stdout.Reset()
		if c := Run(context.Background(), args, &stdout, &stderr, svc); c != 2 || stdout.Len() != 0 {
			t.Fatal("unused flags reached IO", c)
		}
	}
	for _, op := range []string{"apply", "reconcile"} {
		stdout.Reset()
		if c := Run(context.Background(), []string{"setup", op, "--json"}, &stdout, &stderr, svc); c != 2 {
			t.Fatal("new P5 dispatch requires exact arguments", c)
		}
	}
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	out := filepath.Join(tmp, "candidate")
	stdout.Reset()
	args = []string{"setup", "verify", "--observation", root, "--desired", root + "/desired.json", "--mapping", root + "/mapping.json", "--mapping-context", root + "/mapping-context.json", "--as-of", "2026-10-26T00:00:01Z", "--out", out, "--json"}
	if c := Run(context.Background(), args, &stdout, &stderr, svc); c != 3 {
		t.Fatal("fixture qualified binding", c, stdout.String())
	}
	if _, e := os.Stat(filepath.Join(out, "report.json")); e != nil {
		t.Fatal(e)
	}
}
