package command

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"testing"
)

func TestAccessFlagsRejectBeforeEffects(t *testing.T) {
	for _, args := range [][]string{
		{"auth", "login", "fixture", "--configuration-file", "missing", "--policy", "missing", "--grant", "missing", "--out", "missing", "--artifact", "unexpected"},
		{"tools", "install", "--plan", "missing", "--policy", "missing", "--grant", "missing", "--out", "missing", "--configuration-file", "unexpected"},
		{"tools", "doctor", "--tool", "missing", "--as-of", "2026-10-26T00:00:00Z", "--grant", "unexpected"},
	} {
		var out, err bytes.Buffer
		if code := Run(context.Background(), args, &out, &err, sdk.New(nil)); code != 2 || out.Len() != 0 {
			t.Fatalf("unused flag reached IO: %d %s", code, out.String())
		}
	}
}
func TestAccessStandaloneSuiteAndInactiveRouteInventory(t *testing.T) {
	s := sdk.New(nil)
	var out, err bytes.Buffer
	if code := Run(context.Background(), []string{"auth", "status", "--json"}, &out, &err, s); code != 0 {
		t.Fatal(code)
	}
	domain := out.Bytes()
	out.Reset()
	if code := RunSuite(context.Background(), []string{"auth", "status", "--json"}, &out, &err, s); code != 0 {
		t.Fatal(code)
	}
	var d, e map[string]any
	json.Unmarshal(domain, &d)
	json.Unmarshal(out.Bytes(), &e)
	nested := e["spec"].(map[string]any)["data"]
	b, _ := json.Marshal(nested)
	a, _ := json.Marshal(d)
	if string(a) != string(b) {
		t.Fatal("lossy suite wrapping")
	}
	if e["spec"].(map[string]any)["effects"] != "none" {
		t.Fatal("route inventory has effects")
	}
}
