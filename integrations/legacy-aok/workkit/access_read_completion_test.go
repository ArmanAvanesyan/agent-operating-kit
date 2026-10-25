package workkit

import (
	"context"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrokerReadImmutableSnapshotAndPartialCapture(t *testing.T) {
	root := backendDir(t)
	out := filepath.Join(root, "capture")
	source := backendHTTPSource()
	source.Auth = readsource.Auth{Mode: "bearer", TokenEnv: "WK_P2_TOKEN"}
	configPath, policyPath, enPath := backendEnrollment(t, source, r.TransportIdentity{Kind: "http", Version: "go-http/v1", AuthMode: "bearer", Principal: source.PrincipalPseudonym}, "", out)
	cfgRaw, _ := os.ReadFile(configPath)
	var cfg r.Configuration
	sdk.DecodeRead(cfgRaw, &cfg)
	c := a.Configuration{Version: a.Version, Kind: "AuthConfiguration", ConnectionID: cfg.ConnectionID, Provider: "jira-cloud", Origin: source.Origin, TenantID: source.NativeTenantID, Containers: []string{source.ContainerID}, NativePrincipal: "7", Profile: cfg.Profile, Mode: "oauth2", Read: &a.ReadInputs{Output: out}}
	for i, p := range []string{configPath, policyPath, enPath} {
		b, _ := os.ReadFile(p)
		pin := a.LocalPin{Path: p, SHA256: sdk.SHA256(b)}
		switch i {
		case 0:
			c.Read.Configuration = pin
		case 1:
			c.Read.Policy = pin
		case 2:
			c.Read.Enrollment = pin
		}
	}
	captured, raw, policy, en, e := loadCredentialRead(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	// Replace the source only after pinning; the accepted snapshot must remain used.
	os.WriteFile(configPath, []byte(`{"tampered":true}`), 0600)
	if _, _, _, _, e = loadCredentialRead(context.Background(), c); e == nil {
		t.Fatal("changed pinned input accepted")
	}
	calls := 0
	b := AccessBackend{RoundTripper: readRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.Host != "api.atlassian.com" || !strings.HasPrefix(req.URL.Path, "/ex/jira/cloud/") {
			t.Fatal("gateway route mismatch", req.URL)
		}
		if _, ok := req.Context().Deadline(); !ok {
			t.Fatal("gateway widened P2 deadline")
		}
		status := 429
		body := `{"message":"rate limited"}`
		if strings.HasSuffix(req.URL.Path, "/myself") {
			status = 200
			body = `{"accountId":"7"}`
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: req}, nil
	})}
	e = b.captureWithCredentialSnapshot(context.Background(), c, "synthetic-access-token", func() error { return nil }, &credentialReadSnapshot{captured, raw, policy, en})
	var partial *sdk.Error
	if !errors.As(e, &partial) || partial.ExitCode != 6 {
		t.Fatalf("partial read falsely complete: %v", e)
	}
	if calls < 2 {
		t.Fatal("fixture did not reach P2 capture")
	}
	bundle, e := sdk.LoadReadBundle(context.Background(), out)
	if e != nil || bundle.Manifest().Complete {
		t.Fatal("partial evidence lost", e)
	}
}

func TestBasicCredentialBrokerReadCompletesWithoutAmbientFallback(t *testing.T) {
	raw, e := os.ReadFile("../../../internal/tracker/testdata/offline/jira/manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	var original readsource.Manifest
	if sdk.DecodeRead(raw, &original) != nil {
		t.Fatal("fixture manifest")
	}
	source := original.Source
	source.Auth = readsource.Auth{Mode: "basic", TokenEnv: "WK_P2_TOKEN", UsernameEnv: "WK_P2_USER"}
	out := filepath.Join(backendDir(t), "basic-read")
	config, policy, enrollment := backendEnrollment(t, source, r.TransportIdentity{Kind: "http", Version: "go-http/v1", Principal: source.PrincipalPseudonym, AuthMode: "basic"}, "", out)
	raw, _ = os.ReadFile(config)
	var readConfig r.Configuration
	sdk.DecodeRead(raw, &readConfig)
	c := a.Configuration{ConnectionID: readConfig.ConnectionID, Provider: "jira-cloud", Origin: source.Origin, NativePrincipal: "7", TenantID: source.NativeTenantID, Containers: []string{source.ContainerID}, Profile: readConfig.Profile, Mode: "basic", SecretReferences: map[string]string{"username": "WK_P2_USER"}, Read: &a.ReadInputs{Output: out}}
	for i, path := range []string{config, policy, enrollment} {
		b, _ := os.ReadFile(path)
		pin := a.LocalPin{Path: path, SHA256: sdk.SHA256(b)}
		switch i {
		case 0:
			c.Read.Configuration = pin
		case 1:
			c.Read.Policy = pin
		case 2:
			c.Read.Enrollment = pin
		}
	}
	cfg, cfgBytes, pol, en, e := loadCredentialRead(context.Background(), c)
	if e != nil {
		t.Fatal(e)
	}
	b := AccessBackend{RoundTripper: readRoundTrip(func(req *http.Request) (*http.Response, error) {
		user, token, ok := req.BasicAuth()
		if !ok || user != "fixture-user" || token != "synthetic-token" {
			t.Fatal("wrong explicit credentials")
		}
		var body []byte
		if strings.HasSuffix(req.URL.Path, "/myself") {
			body = []byte(`{"accountId":"7"}`)
		} else if strings.HasSuffix(req.URL.Path, "/transitions") {
			body, _ = os.ReadFile("../../../internal/tracker/testdata/offline/jira/assets/0001.json")
		} else {
			body, _ = os.ReadFile("../../../internal/tracker/testdata/offline/jira/assets/0000.json")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header), Request: req}, nil
	})}
	if e = b.captureWithCredentialSnapshot(context.Background(), c, "synthetic-token", func() error { return nil }, &credentialReadSnapshot{cfg, cfgBytes, pol, en}, "fixture-user"); e != nil {
		t.Fatal("basic read failed", e)
	}
	bundle, e := sdk.LoadReadBundle(context.Background(), out)
	if e != nil || !bundle.Manifest().Complete {
		t.Fatal("basic capture incomplete", e)
	}
}
