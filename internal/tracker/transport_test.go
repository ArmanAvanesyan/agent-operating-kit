package tracker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func transportAzure() Source {
	return Source{Version: Version, Provider: "azure-devops", Origin: "https://dev.azure.com", APIVersion: "7.1", NativeTenantID: "tenant-guid", ContainerID: "project-guid", ContainerKey: "org/project", PrincipalPseudonym: "principal", Auth: Auth{Mode: "bearer", TokenEnv: "ADO_TOKEN"}, MappingVersion: "mapping-v1"}
}
func transportGitHub() Source {
	return Source{Version: Version, Provider: "github", Origin: "https://api.github.com", APIVersion: "2022-11-28", NativeTenantID: "owner-node", ContainerID: "repo-node", ContainerKey: "owner/repo", PrincipalPseudonym: "principal", Auth: Auth{Mode: "gh-session"}, MappingVersion: "mapping-v1"}
}
func transportJiraCloud() Source {
	return Source{Version: Version, Provider: "jira", Origin: "https://api.atlassian.com", APIVersion: "3", NativeTenantID: "cloud-guid", ContainerID: "project-guid", ContainerKey: "ABC", PrincipalPseudonym: "principal", Auth: Auth{Mode: "basic", TokenEnv: "JIRA_TOKEN", UsernameEnv: "JIRA_USER"}, MappingVersion: "mapping-v1"}
}
func policyFor(s Source) *ReadPolicy {
	return &ReadPolicy{Version: Version, Rules: []ReadRule{{Provider: s.Provider, Origin: s.Origin, NativeTenantID: s.NativeTenantID, ContainerID: s.ContainerID, ContainerKey: s.ContainerKey, Auth: s.Auth, PrincipalPseudonym: s.PrincipalPseudonym}}}
}

func TestHTTPTransportRejectsRedirectAndKeepsAuthOutOfErrors(t *testing.T) {
	s := transportAzure()
	secret := "very-secret-token"
	calls := 0
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+secret {
			t.Errorf("authorization not set from injected lookup")
		}
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://attacker.invalid/"}}, Body: http.NoBody, Request: r}, nil
	})
	got, err := (HTTPTransport{Policy: policyFor(s), RoundTripper: tr, LookupEnv: func(k string) (string, bool) { return secret, true }}).Do(context.Background(), s, Request{Method: "GET", Path: "/org/project/_apis/wit/workitems/42?api-version=7.1", Kind: "azure-item"})
	if err != nil || got.Status != 302 || calls != 1 {
		t.Fatalf("redirect should be returned without following: %#v %v calls=%d", got, err, calls)
	}

	tr = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("connection leaked %s", secret) })
	_, err = (HTTPTransport{Policy: policyFor(s), RoundTripper: tr, LookupEnv: func(string) (string, bool) { return secret, true }}).Do(context.Background(), s, Request{Method: "GET", Path: "/org/project/_apis/wit/workitems/42?api-version=7.1", Kind: "azure-item"})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("transport error exposed credential: %v", err)
	}
}

func TestHTTPTransportDropsErrorPayloadAndEnforcesResponseBytes(t *testing.T) {
	s := transportAzure()
	secret := "provider-secret-diagnostic"
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 503, Header: http.Header{"Retry-After": []string{"2"}}, Body: http.NoBody, Request: r}, nil
	})
	got, err := (HTTPTransport{Policy: policyFor(s), RoundTripper: tr, LookupEnv: func(string) (string, bool) { return "token", true }}).Do(context.Background(), s, Request{Method: "GET", Path: "/org/project/_apis/wit/workitems/42", Kind: "azure-item"})
	if err != nil || got.Status != 503 || len(got.Body) != 0 || got.Headers["retry-after"] != "2" {
		t.Fatalf("provider error should retain only status/allowlisted headers: %#v %v", got, err)
	}

	limits := DefaultLimits()
	limits.RequestSeconds = 2
	limits.CommandSeconds = 2
	limits.Requests = 1
	limits.Items = 1
	limits.ResponseBytes = 16
	limits.TotalBytes = 16
	s.Limits = &limits
	// Use a concrete reader so the limit check sees 17 bytes.
	tr = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: ioNopCloserString(strings.Repeat("x", 17)), Request: r}, nil
	})
	_, err = (HTTPTransport{Policy: policyFor(s), RoundTripper: tr, LookupEnv: func(string) (string, bool) { return "token", true }}).Do(context.Background(), s, Request{Method: "GET", Path: "/org/project/_apis/wit/workitems/42", Kind: "azure-item"})
	var te *TransportError
	if !errors.As(err, &te) || te.Disposition != "incomplete" || strings.Contains(err.Error(), secret) {
		t.Fatalf("response bound not enforced safely: %v", err)
	}
}

func TestHTTPTransportHonorsCallerCancellation(t *testing.T) {
	s := transportAzure()
	started := make(chan struct{})
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := (HTTPTransport{Policy: policyFor(s), RoundTripper: tr, LookupEnv: func(string) (string, bool) { return "token", true }}).Do(ctx, s, Request{Method: "GET", Path: "/org/project/_apis/wit/workitems/42", Kind: "azure-item"})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request never reached injected transport")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("transport ignored cancellation")
	}
}

func TestReadPolicyMismatchStopsBeforeCredentialLookupOrRunner(t *testing.T) {
	s := transportAzure()
	policy := policyFor(s)
	s.PrincipalPseudonym = "different-principal"
	lookups, requests := 0, 0
	tr := roundTripFunc(func(*http.Request) (*http.Response, error) { requests++; return nil, errors.New("must not reach HTTP") })
	_, err := (HTTPTransport{Policy: policy, RoundTripper: tr, LookupEnv: func(string) (string, bool) { lookups++; return "token", true }}).Do(context.Background(), s, Request{Method: "GET", Path: "/org/project/_apis/wit/workitems/42", Kind: "azure-item"})
	var te *TransportError
	if !errors.As(err, &te) || te.Disposition != "authentication" || lookups != 0 || requests != 0 {
		t.Fatalf("policy mismatch reached credentials or transport: err=%v lookups=%d requests=%d", err, lookups, requests)
	}

	gh := transportGitHub()
	ghPolicy := policyFor(gh)
	gh.PrincipalPseudonym = "different-principal"
	runner := &recordingRunner{}
	_, err = (GitHubTransport{Policy: ghPolicy, Runner: runner}).Do(context.Background(), gh, Request{Method: "GET", Path: "/repos/owner/repo/issues", Kind: "github-issues"})
	if !errors.As(err, &te) || te.Disposition != "authentication" || len(runner.args) != 0 {
		t.Fatalf("GitHub policy mismatch invoked gh: err=%v args=%v", err, runner.args)
	}
}

func TestValidateRequestRejectsMutationAndOriginEscapes(t *testing.T) {
	gh := transportGitHub()
	for _, method := range []string{"PUT", "PATCH", "DELETE"} {
		if err := ValidateRequest(gh, Request{Method: method, Path: "/repos/owner/repo/issues/1", Kind: "github-item"}); err == nil {
			t.Errorf("%s accepted", method)
		}
	}
	for _, query := range []string{`mutation { viewer { login } }`, `subscription { viewer { login } }`} {
		body := []byte(`{"query":` + fmt.Sprintf("%q", query) + `}`)
		if err := ValidateRequest(gh, Request{Method: "POST", Path: "/graphql", Body: body, Kind: "github-projects"}); err == nil {
			t.Errorf("GraphQL operation accepted: %s", query)
		}
	}
	gh.Scope.Projects = true
	projectQuery := githubProjectsRequest("owner", "repo", "")
	if err := ValidateRequest(gh, projectQuery); err != nil {
		t.Fatalf("adapter's fixed scoped GitHub projects query rejected: %v", err)
	}
	read := Request{Method: "POST", Path: "/graphql", Body: []byte(`{"query":"query Safe { viewer { login } }"}`), Kind: "github-projects"}
	if err := ValidateRequest(gh, read); err == nil {
		t.Fatal("undeclared read-only GraphQL query was accepted")
	}
	checkpoint := finishCheckpoint(Checkpoint{Version: Version, ScopeSHA256: ScopeDigest(gh), Pending: []Request{read}})
	if err := ValidateCheckpoint(checkpoint, gh); err == nil {
		t.Fatal("arbitrary read GraphQL request was accepted through a checkpoint")
	}

	gh.Scope.ItemIDs = []string{"issue:1"}
	otherItem := Request{Method: "GET", Path: "/repos/owner/repo/issues/2", Kind: "github-item", ItemID: "issue:2"}
	checkpoint = finishCheckpoint(Checkpoint{Version: Version, ScopeSHA256: ScopeDigest(gh), Pending: []Request{otherItem}})
	if err := ValidateCheckpoint(checkpoint, gh); err == nil {
		t.Fatal("checkpoint selected a GitHub item outside explicit itemIds")
	}
	if err := ValidateRequest(gh, otherItem); err == nil {
		t.Fatal("explicit GitHub item scope was not enforced")
	}
	for _, path := range []string{"https://evil.invalid/org/project/_apis/wit/workitems/1", "//evil.invalid/path", "/org/project/../../evil", "/org/project/_apis/wit/workitems/1#fragment"} {
		if err := ValidateRequest(transportAzure(), Request{Method: "GET", Path: path, Kind: "azure-item"}); err == nil {
			t.Errorf("unsafe path accepted: %q", path)
		}
	}
	if err := ValidateRequest(transportAzure(), Request{Method: "GET", Path: "/org/other/_apis/wit/workitems/42?api-version=7.1", Kind: "azure-item"}); err == nil {
		t.Fatal("Azure request to another project in the same organization was accepted")
	}
	if err := ValidateRequest(transportJiraCloud(), Request{Method: "GET", Path: "/ex/jira/cloud-guid/rest/api/3/project/ABC", Kind: "jira-project"}); err != nil {
		t.Fatalf("Atlassian Cloud tenant-prefixed Jira path rejected: %v", err)
	}
	for _, origin := range []string{"https://user:pass@dev.azure.com", "https://dev.azure.com?token=secret", "https://dev.azure.com/path"} {
		s := transportAzure()
		s.Origin = origin
		if err := ValidateSource(s); err == nil {
			t.Errorf("unsafe origin accepted: %s", origin)
		}
	}
}

type recordingRunner struct {
	args   []string
	input  []byte
	output []byte
	err    error
}

func (r *recordingRunner) Run(_ context.Context, args []string, input []byte, _ int) ([]byte, error) {
	r.args = append([]string(nil), args...)
	r.input = append([]byte(nil), input...)
	return append([]byte(nil), r.output...), r.err
}

func TestGitHubTransportUsesArgumentVectorAndParsesRawHTTP(t *testing.T) {
	runner := &recordingRunner{output: []byte("HTTP/2 429 Too Many Requests\r\nRetry-After: 3\r\nX-RateLimit-Remaining: 0\r\nSet-Cookie: secret=value\r\n\r\nprivate body"), err: errors.New("gh stderr secret")}
	req := Request{Method: "GET", Path: "/repos/owner/repo/issues?per_page=10", Kind: "github-issues"}
	s := transportGitHub()
	got, err := (GitHubTransport{Runner: runner, Policy: policyFor(s)}).Do(context.Background(), s, req)
	if err != nil || got.Status != 429 || len(got.Body) != 0 || got.Headers["retry-after"] != "3" || got.Headers["set-cookie"] != "" {
		t.Fatalf("raw HTTP error parse or header filtering failed: %#v %v", got, err)
	}
	want := []string{"api", "--hostname", "github.com", "--include", "--method", "GET", req.Path, "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28"}
	if !reflect.DeepEqual(runner.args, want) {
		t.Fatalf("unexpected gh argv (shell must not be involved): %#v", runner.args)
	}
}

func TestGitHubTransportDoesNotExposeRunnerDiagnostics(t *testing.T) {
	secret := "token-from-gh-stderr"
	runner := &recordingRunner{output: []byte(secret), err: errors.New(secret)}
	s := transportGitHub()
	_, err := (GitHubTransport{Runner: runner, Policy: policyFor(s)}).Do(context.Background(), s, Request{Method: "GET", Path: "/repos/owner/repo/issues", Kind: "github-issues"})
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("gh error leaked stderr/diagnostic: %v", err)
	}
}

func TestCommandRunnerDiscardsStderr(t *testing.T) {
	dir := t.TempDir()
	secret := "command-stderr-secret"
	script := filepath.Join(dir, "gh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' '"+secret+"' >&2\nexit 9\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, err := (CommandRunner{}).Run(context.Background(), []string{"api", "--hostname", "example.invalid"}, nil, 1024)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("command runner exposed stderr: %v", err)
	}
}

// Keep the tests independent of network and avoid importing another helper package.
type stringReadCloser struct{ *strings.Reader }

func (stringReadCloser) Close() error             { return nil }
func ioNopCloserString(s string) stringReadCloser { return stringReadCloser{strings.NewReader(s)} }

func TestTransportEndpointsCannotReadUnrelatedResources(t *testing.T) {
	for _, test := range []struct{ provider, path, kind string }{
		{"github", "/repos/acme/widgets/actions/secrets", "github-item"},
		{"jira", "/rest/api/3/myself", "jira-item"},
		{"azure-devops", "/acme/project/_apis/git/repositories", "azure-fields"},
		{"linear", "/graphql", "linear-items"},
	} {
		s := integrationSources()[test.provider]
		if ValidateRequest(s, Request{Method: "GET", Path: test.path, Kind: test.kind}) == nil {
			t.Fatalf("unrelated endpoint accepted %s", test.path)
		}
	}
}
func TestHTTPTransportRejectsReflectedCredentialCanary(t *testing.T) {
	s := transportAzure()
	secret := "aok-credential-privacy-canary"
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{\"echo\":\"" + secret + "\"}")), Request: r}, nil
	})
	got, e := (HTTPTransport{Policy: policyFor(s), RoundTripper: tr, LookupEnv: func(string) (string, bool) { return secret, true }}).Do(context.Background(), s, Request{Method: "GET", Path: "/org/project/_apis/wit/workitems/42", Kind: "azure-item"})
	if e == nil || strings.Contains(e.Error(), secret) || len(got.Body) > 0 {
		t.Fatal("reflected credential escaped")
	}
}
