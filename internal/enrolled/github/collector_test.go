package github

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector/providers"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
)

type testBroker struct {
	calls int
	token Token
}

func (b *testBroker) Resolve(_ context.Context, ref string) (Token, error) {
	b.calls++
	if ref != "broker-ref" {
		return Token{}, context.Canceled
	}
	return b.token, nil
}

type testAuth struct {
	calls  int
	scopes []Authorization
	deny   bool
}

func (a *testAuth) CheckRead(_ context.Context, s Authorization) error {
	a.calls++
	a.scopes = append(a.scopes, s)
	if a.deny {
		return context.Canceled
	}
	return nil
}

func githubFixture(t *testing.T, script string) (Config, *testBroker, *testAuth) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh-fixture")
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	s := tracker.Source{Version: tracker.Version, Provider: "github", Origin: pinnedOrigin, APIVersion: "2022-11-28", NativeTenantID: "owner-node", ContainerID: "repo-node", ContainerKey: "acme/widgets", PrincipalPseudonym: "principal-a", Auth: tracker.Auth{Mode: "gh-session"}, Scope: tracker.Scope{}, MappingVersion: "mapping-v1"}
	p := &tracker.ReadPolicy{Version: tracker.Version, Rules: []tracker.ReadRule{{Provider: "github", Origin: pinnedOrigin, NativeTenantID: s.NativeTenantID, ContainerID: s.ContainerID, ContainerKey: s.ContainerKey, Auth: s.Auth, PrincipalPseudonym: s.PrincipalPseudonym}}}
	broker := &testBroker{token: Token{Value: "fixture-secret-token", PrincipalPseudonym: s.PrincipalPseudonym, GrantRef: "grant-read-1", Scopes: []string{"issues:read", "metadata:read"}, ValidUntil: time.Now().Add(time.Minute)}}
	return Config{Source: s, Policy: p, Executable: bin, ExecutableSHA256: hex.EncodeToString(sum[:]), Version: "gh version 2.64.0", AuthReference: "broker-ref"}, broker, &testAuth{}
}

func TestCollectUsesPinnedGhAndCapturesBoundSource(t *testing.T) {
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then echo 'gh version 2.64.0'; exit 0; fi
test "$GH_TOKEN" = "fixture-secret-token" || exit 8
test -z "${GITHUB_TOKEN+x}" || exit 9
printf 'HTTP/2 200\r\nContent-Type: application/json\r\n\r\n{"id":123,"node_id":"repo-node","name":"widgets","full_name":"acme/widgets","owner":{"id":44,"node_id":"owner-node","login":"acme"}}'
`
	cfg, broker, auth := githubFixture(t, script)
	c, err := New(cfg, broker, auth)
	if err != nil {
		t.Fatal(err)
	}
	capture, err := c.Collect(context.Background(), "discover")
	if err != nil {
		t.Fatal(err)
	}
	if !capture.Snapshot.Complete {
		t.Fatalf("capture incomplete: %+v", capture.Snapshot.Report.Diagnostics)
	}
	if capture.Snapshot.Source.ReadPolicySHA256 == "" || capture.Snapshot.Source.PrincipalPseudonym != "principal-a" {
		t.Fatalf("capture lost enrolled source binding: %+v", capture.Snapshot.Source)
	}
	if broker.calls != 1 || auth.calls != 1 || len(auth.scopes) != 1 || auth.scopes[0].Path != "/repos/acme/widgets" || auth.scopes[0].Origin != pinnedOrigin {
		t.Fatalf("broker/admission scope mismatch: calls=%d %+v", broker.calls, auth.scopes)
	}
	if strings.Contains(string(capture.Snapshot.Discovery["repository"]), "fixture-secret-token") {
		t.Fatal("credential canary survived into captured evidence")
	}
}

func TestReadRunnerRejectsAccountProjectsMutationsAndAuthFallback(t *testing.T) {
	cfg, broker, auth := githubFixture(t, "#!/bin/sh\necho 'gh version 2.64.0'\n")
	c, err := New(cfg, broker, auth)
	if err != nil {
		t.Fatal(err)
	}
	valid := []string{"api", "--hostname", "github.com", "--include", "--method", "GET", "/repos/acme/widgets", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28"}
	for _, bad := range [][]string{
		append(append([]string(nil), valid[:6]...), append([]string{"/user"}, valid[7:]...)...),
		append(append([]string(nil), valid[:6]...), append([]string{"/graphql"}, valid[7:]...)...),
		append(append([]string(nil), valid[:4]...), append([]string{"POST"}, valid[5:]...)...),
	} {
		if _, err := c.Run(context.Background(), bad, nil, 1024); err == nil {
			t.Fatalf("out-of-scope GitHub call accepted: %v", bad)
		}
	}
	if broker.calls != 0 || auth.calls != 0 {
		t.Fatal("rejected route reached broker or authority")
	}
	cfg.Source.Scope.Projects = true
	if _, err = New(cfg, broker, auth); err == nil {
		t.Fatal("Projects discovery scope accepted")
	}
}

func TestNewWriterUsesExistingConnectorBoundaryWithoutAutomaticDispatch(t *testing.T) {
	cfg, broker, auth := githubFixture(t, "#!/bin/sh\n")
	cfg.Source.Scope.ItemIDs = []string{"17"}
	called := false
	writeAuth := WriteAuthorityFunc(func(_ context.Context, a Authorization) error {
		called = true
		if a.Principal != "principal-a" || a.Path != "/repos/acme/widgets/issues/17/comments" || a.Origin != pinnedOrigin {
			return context.Canceled
		}
		return nil
	})
	writer, err := NewWriter(cfg, broker, auth, writeAuth, providers.WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if writer == nil || broker.calls != 0 || auth.calls != 0 || called {
		t.Fatal("writer construction resolved credentials or authorized an operation")
	}
}

func TestGHCommandOutputAndCancellationAreBounded(t *testing.T) {
	var bounded limitedBuffer
	bounded.limit = 512
	if _, err := bounded.Write(make([]byte, 4096)); err != nil || bounded.err == nil || bounded.Len() != 513 {
		t.Fatal("limited buffer did not cap retained bytes")
	}
	args := []string{"api", "--hostname", "github.com", "--include", "--method", "GET", "/repos/acme/widgets", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28"}
	cfg, broker, auth := githubFixture(t, "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'gh version 2.64.0'; exit 0; fi\nhead -c 4096 /dev/zero\n")
	c, err := New(cfg, broker, auth)
	if err != nil {
		t.Fatal(err)
	}
	if got, runErr := c.Run(context.Background(), args, nil, 512); runErr == nil {
		t.Fatalf("oversized gh output was accepted (%d bytes)", len(got))
	}

	cfg, broker, auth = githubFixture(t, "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'gh version 2.64.0'; exit 0; fi\nsleep 10\n")
	c, err = New(cfg, broker, auth)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err = c.Run(ctx, args, nil, 1024); err == nil {
		t.Fatal("cancelled gh command succeeded")
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("gh process group did not stop promptly after cancellation")
	}
}
