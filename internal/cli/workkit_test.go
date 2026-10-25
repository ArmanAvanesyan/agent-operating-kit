package cli

import (
	"bytes"
	"context"
	"encoding/json"
	legacy "github.com/ArmanAvanesyan/agent-operating-kit/integrations/legacy-aok/workkit"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk/command"
	"path/filepath"
	"testing"
)

func TestWorkkitStandaloneUmbrellaParityAndNoAuthority(t *testing.T) {
	root, e := filepath.Abs("../../kits/workkit/testdata/corpora/native-only")
	if e != nil {
		t.Fatal(e)
	}
	args := []string{"workspace", "inspect", "--workspace", filepath.Join(root, "workspace.json"), "--json"}
	var out, errout, umbrella, umbrellaErr bytes.Buffer
	c := command.Run(context.Background(), args, &out, &errout, sdk.New(&legacy.Reader{}))
	u := Run(append([]string{"work"}, args...), &umbrella, &umbrellaErr)
	if c != 0 || u != c || errout.Len() != 0 || umbrellaErr.Len() != 0 {
		t.Fatalf("code %d/%d %s %s", c, u, errout.String(), umbrellaErr.String())
	}
	var suite struct {
		APIVersion string `json:"apiVersion"`
		Spec       struct {
			Data json.RawMessage `json:"data"`
			Exit int             `json:"domainExitCode"`
		} `json:"spec"`
	}
	if e = json.Unmarshal(umbrella.Bytes(), &suite); e != nil {
		t.Fatal(e)
	}
	if suite.APIVersion != "aok-suite/v1alpha1" || suite.Spec.Exit != c || !bytes.Equal(bytes.TrimSpace(out.Bytes()), suite.Spec.Data) {
		t.Fatal("suite domain result changed")
	}
	if bytes.Contains(out.Bytes(), []byte(`"executionReady":true`)) {
		t.Fatal("static readiness")
	}
}
func TestWorkkitOnlyAdvertisesOfflineCommandsAndRedactsFailures(t *testing.T) {
	for _, args := range [][]string{{"work", "auth", "login"}, {"work", "workspace", "validate"}, {"work", "migrate", "inspect-aok", "--source", "P1_PRIVATE_CANARY", "--format", "unknown", "--as-of", "2026-10-25T12:00:00Z", "--json"}} {
		var out, stderr bytes.Buffer
		code := Run(args, &out, &stderr)
		if code == 0 {
			t.Fatal("invalid/live command accepted")
		}
		if bytes.Contains(out.Bytes(), []byte("P1_PRIVATE_CANARY")) || bytes.Contains(stderr.Bytes(), []byte("P1_PRIVATE_CANARY")) {
			t.Fatal("private diagnostics")
		}
	}
	var out, stderr bytes.Buffer
	if Run([]string{"work", "--help"}, &out, &stderr) != 0 || !bytes.Contains(out.Bytes(), []byte("P1 offline")) {
		t.Fatal("help")
	}
}
