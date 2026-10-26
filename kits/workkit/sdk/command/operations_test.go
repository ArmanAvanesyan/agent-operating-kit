package command

import (
	"bytes"
	"context"
	"encoding/json"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"os"
	"path/filepath"
	"testing"
)

func TestOperationsStrictFlagsAndParity(t *testing.T) {
	svc := sdk.New(nil)
	ctx := context.Background()
	for _, args := range [][]string{{"operations", "apply", "--as-of", "2026-10-26T00:00:00Z"}, {"sync", "run", "--plan", "missing"}, {"sync", "status", "--store", "missing", "--route", "route", "--out", "unused"}, {"items", "plan-comment", "--grant", "invented"}, {"sync", "validate", "--once"}} {
		var out, errout bytes.Buffer
		if code := Run(ctx, args, &out, &errout, svc); code != 2 || out.Len() != 0 {
			t.Fatal(args, code, out.String())
		}
	}
	for _, group := range []string{"operations", "sync"} {
		var out, errout bytes.Buffer
		if code := Run(ctx, []string{group, "--help"}, &out, &errout, svc); code != 0 || out.Len() == 0 {
			t.Fatal(group, code)
		}
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	store := o.Store{Version: o.Version, Kind: "IntegrationJournal", Path: filepath.Join(root, "journal.db"), ID: "synthetic", Namespace: "new", Owner: "workkit"}
	raw, _ := json.Marshal(store)
	path := filepath.Join(root, "store.json")
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	args := []string{"operations", "list", "--store", path, "--json"}
	var domain, wrapper, errout bytes.Buffer
	code := Run(ctx, args, &domain, &errout, svc)
	suiteCode := RunSuite(ctx, args, &wrapper, &errout, svc)
	if code != suiteCode {
		t.Fatal("exit drift")
	}
	var a any
	var b map[string]any
	_ = json.Unmarshal(domain.Bytes(), &a)
	_ = json.Unmarshal(wrapper.Bytes(), &b)
	got, _ := json.Marshal(b["spec"].(map[string]any)["data"])
	want, _ := json.Marshal(a)
	if !bytes.Equal(got, want) {
		t.Fatal("lossy result")
	}
}

func TestOperationsCancellationBeforeInputsIsOperational(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, stderr bytes.Buffer
	code := Run(ctx, []string{"operations", "apply", "--plan", "missing", "--policy", "missing", "--enrollment", "missing", "--out", "missing-output", "--json"}, &out, &stderr, sdk.New(nil))
	if code != 1 {
		t.Fatal("before-send cancellation is operational, not schema misuse", code, out.String())
	}
}
