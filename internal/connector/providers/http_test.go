package providers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
)

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func source() tracker.Source {
	return tracker.Source{Version: tracker.Version, Provider: "linear", Origin: "https://api.linear.app", APIVersion: "v1", NativeTenantID: "tenant", ContainerID: "team-id", ContainerKey: "team", PrincipalPseudonym: "pseudonym", Auth: tracker.Auth{Mode: "api-key", TokenEnv: "TOKEN"}, Scope: tracker.Scope{ItemIDs: []string{"123e4567-e89b-42d3-a456-426614174000"}}, MappingVersion: "1"}
}
func response(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: nil}
}
func route(t *testing.T, rt http.RoundTripper) *httpTransport {
	t.Helper()
	tr, e := newTransport(Route{Source: source(), AuthReference: "opaque-auth-1", Resolver: CredentialResolverFunc(func(context.Context, string) (Credential, error) { return Credential{Token: "fixture-token"}, nil }), RoundTripper: rt})
	if e != nil {
		t.Fatal(e)
	}
	return tr
}

func TestTransportPinsOriginAuthAndResponseDigest(t *testing.T) {
	called := 0
	tr := route(t, rtFunc(func(r *http.Request) (*http.Response, error) {
		called++
		if r.URL.String() != "https://api.linear.app/graphql" || r.Header.Get("Authorization") != "fixture-token" {
			t.Fatalf("unexpected request destination/auth: %s %q", r.URL, r.Header.Get("Authorization"))
		}
		return response(200, `{"data":{"ok":true},"access_token":"private"}`), nil
	}))
	got, err := tr.Dispatch(context.Background(), source(), tracker.WriteRequest{Method: "POST", Path: "/graphql", ContentType: "application/json", Body: []byte(`{"query":"mutation AOKComment($input: CommentCreateInput!) { commentCreate(input: $input) { success comment { id body issue { id } } } }"}`)})
	sum := sha256.Sum256([]byte(`{"data":{"ok":true},"access_token":"private"}`))
	if err != nil || got.Status != 200 || strings.Contains(string(got.Body), "access_token") || got.Headers["X-AOK-Response-SHA256"] != hex.EncodeToString(sum[:]) || called != 1 {
		t.Fatalf("got=%+v err=%v calls=%d", got, err, called)
	}
}
func TestTransportFailsClosedWithoutAuthAndOnWrongRoute(t *testing.T) {
	s := source()
	if _, err := newTransport(Route{Source: s}); err == nil {
		t.Fatal("missing auth resolver accepted")
	}
	tr := route(t, rtFunc(func(*http.Request) (*http.Response, error) { t.Fatal("wrong route sent"); return nil, nil }))
	s.Origin = "https://attacker.invalid"
	if _, err := tr.Dispatch(context.Background(), s, tracker.WriteRequest{Method: "POST", Path: "/graphql"}); err == nil {
		t.Fatal("wrong route accepted")
	}
}
func TestTransportRejectsRedirectAndCredentialEcho(t *testing.T) {
	calls := 0
	tr := route(t, rtFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			v := response(302, "")
			v.Header.Set("Location", "https://attacker.invalid/")
			return v, nil
		}
		return response(200, `{"token":"fixture-token"}`), nil
	}))
	valid := tracker.WriteRequest{Method: "POST", Path: "/graphql", ContentType: "application/json", Body: []byte(`{"query":"mutation AOKComment($input: CommentCreateInput!) { commentCreate(input: $input) { success comment { id body issue { id } } } }"}`)}
	got, err := tr.Dispatch(context.Background(), source(), valid)
	if err != nil || got.Status != 302 {
		t.Fatalf("redirect should be returned without following: %+v %v", got, err)
	}
	if _, err = tr.Dispatch(context.Background(), source(), valid); err == nil {
		t.Fatal("credential echo accepted")
	}
	if calls != 2 {
		t.Fatalf("unexpected retries: %d", calls)
	}
}

func TestNewWriterHidesRawCredentialTransportAndPathAllowlistIsNarrow(t *testing.T) {
	writer, err := NewWriter(Route{Source: source(), AuthReference: "opaque", Resolver: CredentialResolverFunc(func(context.Context, string) (Credential, error) { return Credential{Token: "x"}, nil })}, WriterOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := writer.(interface {
		Dispatch(context.Context, tracker.Source, tracker.WriteRequest) (tracker.Response, error)
	}); ok {
		t.Fatal("public writer exposes raw credentialed Dispatch")
	}
	s := source()
	valid := []byte(`{"query":"mutation AOKComment($input: CommentCreateInput!) { commentCreate(input: $input) { success comment { id body issue { id } } } }"}`)
	if !allowedProviderRequest(s, tracker.WriteRequest{Method: "POST", Path: "/graphql", Body: valid}) {
		t.Fatal("typed Linear mutation rejected")
	}
	for _, r := range []tracker.WriteRequest{{Method: "POST", Path: "/arbitrary"}, {Method: "GET", Path: "/graphql"}, {Method: "POST", Path: "/graphql?url=https://attacker.invalid"}, {Method: "POST", Path: "/graphql", Body: []byte(`{"query":"mutation { viewer { id } }"}`)}} {
		if allowedProviderRequest(s, r) {
			t.Fatalf("untyped route accepted: %+v", r)
		}
	}
}

func TestRedactionNormalizesCredentialAliases(t *testing.T) {
	got, err := redactJSON([]byte(`{"api_key":"a","clientSecret":"b","access-token":"c","kept":"d"}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"api_key", "clientSecret", "access-token"} {
		if strings.Contains(string(got), key) {
			t.Fatalf("credential field %q survived: %s", key, got)
		}
	}
	if !strings.Contains(string(got), `"kept":"d"`) {
		t.Fatalf("nonsecret response field dropped: %s", got)
	}
	if _, err := redactJSON([]byte(`{"x":1,"x":2}`)); err == nil {
		t.Fatal("duplicate-key response accepted")
	}
}

func TestRedactionPreservesLargeIDsAndExactDecimalLexemesInNestedData(t *testing.T) {
	input := `{"data":[{"id":900719925474099312345678901,"nested":{"amount":0.123456789012345678901}}]}`
	got, err := redactJSON([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	for _, number := range []string{"900719925474099312345678901", "0.123456789012345678901"} {
		if !strings.Contains(string(got), number) {
			t.Fatalf("numeric lexeme %s changed: %s", number, got)
		}
	}
}

func TestTransportBindsAllProviderSourceFields(t *testing.T) {
	tr := route(t, rtFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("changed source reached transport")
		return nil, nil
	}))
	s := source()
	s.Scope.ItemIDs = append(s.Scope.ItemIDs, "another")
	if _, err := tr.Dispatch(context.Background(), s, tracker.WriteRequest{Method: "POST", Path: "/graphql", Body: []byte(`{"query":"mutation AOKComment($input: CommentCreateInput!) { commentCreate(input: $input) { success comment { id body issue { id } } } }"}`)}); err == nil {
		t.Fatal("changed immutable source scope accepted")
	}
}
func TestTransportTimeoutAndLostReplyAreAmbiguousErrors(t *testing.T) {
	tr := route(t, rtFunc(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := tr.Dispatch(ctx, source(), tracker.WriteRequest{Method: "POST", Path: "/graphql"}); err == nil {
		t.Fatal("timeout not surfaced")
	}
	tr = route(t, rtFunc(func(*http.Request) (*http.Response, error) { return nil, io.ErrUnexpectedEOF }))
	if _, err := tr.Dispatch(context.Background(), source(), tracker.WriteRequest{Method: "POST", Path: "/graphql"}); err == nil {
		t.Fatal("lost reply not surfaced")
	}
}
