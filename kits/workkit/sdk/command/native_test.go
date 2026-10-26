package command

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"path/filepath"
	"testing"
)

func TestNativeCommandsParityAndBlockedApply(t *testing.T) {
	for _, args := range [][]string{{"native", "hosts", "--json"}, {"native", "apply", "--json"}} {
		var a, b, errout bytes.Buffer
		svc := sdk.New(nil)
		code := Run(context.Background(), args, &a, &errout, svc)
		other := RunSuite(context.Background(), args, &b, &errout, svc)
		if code != other || args[1] == "apply" && code != 3 {
			t.Fatal(code, other)
		}
		var domain any
		var wrapper map[string]any
		json.Unmarshal(a.Bytes(), &domain)
		json.Unmarshal(b.Bytes(), &wrapper)
		actual, _ := json.Marshal(wrapper["spec"].(map[string]any)["data"])
		want, _ := json.Marshal(domain)
		if !bytes.Equal(actual, want) {
			t.Fatal("lossy domain wrapper")
		}
	}
	root := "../../testdata/native/opencode"
	canonical, _ := filepath.EvalSymlinks(t.TempDir())
	out := filepath.Join(canonical, "projection")
	args := []string{"native", "plan", "--assignment", root + "/assignment.json", "--profile", root + "/profile.json", "--mapping", root + "/mapping.json", "--handle", root + "/handle.json", "--as-of", "2026-10-26T12:00:02Z", "--out", out, "--json"}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), args, &stdout, &stderr, sdk.New(nil)); code != 0 {
		t.Fatal(code, stdout.String(), stderr.String())
	}
	for _, args := range [][]string{{"native", "hosts", "--as-of", "unused"}, {"native", "apply", "--enrollment", "must-not-read"}, {"native", "plan", "--evidence", "unused"}, {"native", "observe", "--as-of", "2026-10-26T00:00:00Z"}} {
		stdout.Reset()
		if code := Run(context.Background(), args, &stdout, &stderr, sdk.New(nil)); code != 2 || stdout.Len() != 0 {
			t.Fatal("inapplicable flags reached I/O", code, stdout.String())
		}
	}
}
