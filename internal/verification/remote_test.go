package verification

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type remoteRT func(*http.Request) (*http.Response, error)

func (f remoteRT) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func remoteResp(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}
func testRemoteControls(t *testing.T, rt http.RoundTripper) *RemoteControls {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	spec := VerifierSpec{ID: "v", Kind: "command", ImplementationSHA256: hash([]byte("impl")), RubricSHA256: hash([]byte("rubric")), Argv: []string{"check"}, Environment: map[string]string{}, TimeoutSeconds: 1, MaxOutputBytes: 128}
	specDigest, _ := Digest(spec)
	proof := ControlProof{Version: Version, VerifierSHA256: specDigest, CandidateSHA256: hash([]byte("candidate")), SnapshotSHA256: hash([]byte("snapshot")), Issuer: "external", Signature: "fixture-signature", ObservedAt: now.Add(-time.Minute).Format(time.RFC3339), ValidUntil: now.Add(time.Minute).Format(time.RFC3339)}
	c, e := NewRemoteControls(RemoteRoute{Endpoint: "https://verify.example.test/v1/execute", AuthReference: "secret-ref", Resolver: RemoteSecretResolverFunc(func(context.Context, string) (string, error) { return "fixture-auth", nil }), RoundTripper: rt}, proof, spec)
	if e != nil {
		t.Fatal(e)
	}
	return c.(*RemoteControls)
}
func TestRemoteControlsBindsProofAndBoundsOutput(t *testing.T) {
	called := 0
	c := testRemoteControls(t, remoteRT(func(r *http.Request) (*http.Response, error) {
		called++
		if r.URL.String() != "https://verify.example.test/v1/execute" || r.Header.Get("Authorization") != "Bearer fixture-auth" {
			t.Fatalf("unexpected request %s", r.URL)
		}
		var req remoteExecutionRequest
		if json.NewDecoder(r.Body).Decode(&req) != nil {
			t.Fatal("invalid request")
		}
		digest, _ := Digest(req.Proof)
		if req.Protocol != RemoteProtocol || req.ProofSHA256 != digest || req.CandidateSHA256 != req.Proof.CandidateSHA256 || req.TimeoutMillis != 1000 || req.MaxOutputBytes != 128 {
			t.Fatalf("request lost proof/bounds: %+v", req)
		}
		body, _ := json.Marshal(remoteExecutionResponse{Protocol: RemoteProtocol, ProofSHA256: digest, ExitCode: 0, StdoutBase64: base64.StdEncoding.EncodeToString([]byte("ok"))})
		return remoteResp(200, string(body)), nil
	}))
	code, out, err := c.Execute(context.Background(), []string{"check"}, map[string]string{}, time.Second, 128)
	if err != nil || code != 0 || string(out) != "ok" || called != 1 {
		t.Fatalf("code=%d out=%q err=%v calls=%d", code, out, err, called)
	}
}

func TestRemoteControlsRejectsCommandMismatchBeforeResolvingOrSending(t *testing.T) {
	calls := 0
	spec := VerifierSpec{ID: "v", Kind: "command", ImplementationSHA256: hash([]byte("impl")), RubricSHA256: hash([]byte("rubric")), Argv: []string{"check"}, Environment: map[string]string{}, TimeoutSeconds: 1, MaxOutputBytes: 128}
	specDigest, _ := Digest(spec)
	now := time.Now().UTC().Truncate(time.Second)
	proof := ControlProof{Version: Version, VerifierSHA256: specDigest, CandidateSHA256: hash([]byte("c")), SnapshotSHA256: hash([]byte("s")), Signature: "fixture", ObservedAt: now.Add(-time.Minute).Format(time.RFC3339), ValidUntil: now.Add(time.Minute).Format(time.RFC3339)}
	c, err := NewRemoteControls(RemoteRoute{Endpoint: "https://verify.example.test/v1/execute", AuthReference: "ref", Resolver: RemoteSecretResolverFunc(func(context.Context, string) (string, error) { calls++; return "secret", nil }), RoundTripper: remoteRT(func(*http.Request) (*http.Response, error) { t.Fatal("request sent"); return nil, nil })}, proof, spec)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.Execute(context.Background(), []string{"other"}, map[string]string{}, time.Second, 128); err == nil {
		t.Fatal("command differing from pinned spec accepted")
	}
	if calls != 0 {
		t.Fatal("credential resolved before rejecting command mismatch")
	}
}

func TestRemoteControlsDeepCopiesProofAndSpecification(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	spec := VerifierSpec{ID: "v", Kind: "command", ImplementationSHA256: hash([]byte("i")), RubricSHA256: hash([]byte("r")), Argv: []string{"check"}, Environment: map[string]string{}, TimeoutSeconds: 1, MaxOutputBytes: 128}
	specDigest, _ := Digest(spec)
	proof := ControlProof{Version: Version, VerifierSHA256: specDigest, CandidateSHA256: hash([]byte("c")), SnapshotSHA256: hash([]byte("s")), Controls: []string{"network-deny"}, Signature: "sig", ObservedAt: now.Add(-time.Minute).Format(time.RFC3339), ValidUntil: now.Add(time.Minute).Format(time.RFC3339)}
	digest, _ := Digest(proof)
	rt := remoteRT(func(*http.Request) (*http.Response, error) {
		body, _ := json.Marshal(remoteExecutionResponse{Protocol: RemoteProtocol, ProofSHA256: digest, ExitCode: 0, StdoutBase64: base64.StdEncoding.EncodeToString([]byte("ok"))})
		return remoteResp(200, string(body)), nil
	})
	c, err := NewRemoteControls(RemoteRoute{Endpoint: "https://verify.example.test/v1/execute", AuthReference: "ref", Resolver: RemoteSecretResolverFunc(func(context.Context, string) (string, error) { return "secret", nil }), RoundTripper: rt}, proof, spec)
	if err != nil {
		t.Fatal(err)
	}
	proof.Controls[0] = "credential-isolation"
	spec.Argv[0] = "changed"
	spec.Environment["MODE"] = "changed"
	returned := c.Proof()
	returned.Controls[0] = "tampered"
	if c.Proof().Controls[0] != "network-deny" {
		t.Fatal("proof controls slice was shared")
	}
	if _, _, err = c.Execute(context.Background(), []string{"check"}, map[string]string{}, time.Second, 128); err != nil {
		t.Fatalf("constructor did not pin specification: %v", err)
	}
}

func TestRemoteControlsRejectsSecretEnvironmentKeysBeforeResolution(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	spec := VerifierSpec{ID: "v", Kind: "command", ImplementationSHA256: hash([]byte("i")), RubricSHA256: hash([]byte("r")), Argv: []string{"check"}, Environment: map[string]string{"API_KEY": "secret"}, TimeoutSeconds: 1, MaxOutputBytes: 128}
	digest, _ := Digest(spec)
	proof := ControlProof{Version: Version, VerifierSHA256: digest, CandidateSHA256: hash([]byte("c")), SnapshotSHA256: hash([]byte("s")), Signature: "sig", ObservedAt: now.Add(-time.Minute).Format(time.RFC3339), ValidUntil: now.Add(time.Minute).Format(time.RFC3339)}
	resolved := false
	_, err := NewRemoteControls(RemoteRoute{Endpoint: "https://verify.example.test/v1/execute", AuthReference: "ref", Resolver: RemoteSecretResolverFunc(func(context.Context, string) (string, error) { resolved = true; return "secret", nil })}, proof, spec)
	if err == nil || resolved {
		t.Fatal("secret-like environment key accepted or resolved")
	}
}
func TestRemoteControlsRejectsBadRouteProofAndOutput(t *testing.T) {
	for _, endpoint := range []string{"http://verify.test/v1/execute", "https://verify.test/other", "https://user:pass@verify.test/v1/execute"} {
		if _, err := NewRemoteControls(RemoteRoute{Endpoint: endpoint, AuthReference: "x", Resolver: RemoteSecretResolverFunc(func(context.Context, string) (string, error) { return "s", nil })}, ControlProof{CandidateSHA256: "x", SnapshotSHA256: "y", Signature: "sig"}, VerifierSpec{}); err == nil {
			t.Fatalf("unsafe endpoint accepted: %s", endpoint)
		}
	}
	c := testRemoteControls(t, remoteRT(func(*http.Request) (*http.Response, error) {
		return remoteResp(200, `{"protocol":"aok-verifier-transport/v1","proofSha256":"wrong","exitCode":0,"stdoutBase64":"b2s="}`), nil
	}))
	if _, _, err := c.Execute(context.Background(), []string{"check"}, map[string]string{}, time.Second, 128); err == nil {
		t.Fatal("wrong proof response accepted")
	}
	c = testRemoteControls(t, remoteRT(func(*http.Request) (*http.Response, error) {
		return remoteResp(200, `{"protocol":"aok-verifier-transport/v1","proofSha256":"x","proofSha256":"x","exitCode":0,"stdoutBase64":"b2s="}`), nil
	}))
	if _, _, err := c.Execute(context.Background(), []string{"check"}, map[string]string{}, time.Second, 128); err == nil {
		t.Fatal("duplicate response fields accepted")
	}
}
func TestRemoteControlsNoRetryRedirectTimeoutAndSecretRefusal(t *testing.T) {
	calls := 0
	c := testRemoteControls(t, remoteRT(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("fixture lost reply") }))
	if _, _, err := c.Execute(context.Background(), []string{"check"}, map[string]string{}, time.Second, 128); err == nil || calls != 1 {
		t.Fatalf("request retried or accepted: err=%v calls=%d", err, calls)
	}
	c = testRemoteControls(t, remoteRT(func(*http.Request) (*http.Response, error) {
		v := remoteResp(302, "")
		v.Header.Set("Location", "https://elsewhere.invalid")
		return v, nil
	}))
	if _, _, err := c.Execute(context.Background(), []string{"check"}, map[string]string{}, time.Second, 128); err == nil {
		t.Fatal("redirect accepted")
	}
	c = testRemoteControls(t, remoteRT(func(r *http.Request) (*http.Response, error) { <-r.Context().Done(); return nil, r.Context().Err() }))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, _, err := c.Execute(ctx, []string{"check"}, map[string]string{}, time.Second, 128); err == nil {
		t.Fatal("timeout accepted")
	}
	c = testRemoteControls(t, remoteRT(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		var req remoteExecutionRequest
		_ = json.Unmarshal(body, &req)
		dig, _ := Digest(req.Proof)
		payload, _ := json.Marshal(remoteExecutionResponse{Protocol: RemoteProtocol, ProofSHA256: dig, StdoutBase64: base64.StdEncoding.EncodeToString([]byte(`{"apiKey":"leaked","client_secret":"x"}`))})
		return remoteResp(200, string(payload)), nil
	}))
	if _, _, err := c.Execute(context.Background(), []string{"check"}, map[string]string{}, time.Second, 128); err == nil {
		t.Fatal("credential shaped output accepted")
	}
}
