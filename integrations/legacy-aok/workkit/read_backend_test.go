package workkit

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type readRoundTrip func(*http.Request) (*http.Response, error)

func (f readRoundTrip) RoundTrip(q *http.Request) (*http.Response, error) { return f(q) }
func backendDir(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func backendJSON(v any) []byte { b, _ := json.Marshal(v); return append(b, '\n') }
func backendEnrollment(t *testing.T, s readsource.Source, transport r.TransportIdentity, executable, out string) (string, string, string) {
	t.Helper()
	dir := backendDir(t)
	provider := s.Provider
	if provider == "jira" {
		provider = "jira-cloud"
	}
	kind := "rest"
	if s.Provider == "github" {
		kind = "gh"
	}
	profile := backendJSON(map[string]any{"apiVersion": "workkit/v1alpha1", "kind": "ConnectionProfile", "metadata": map[string]any{"id": "fixture", "revision": 1}, "spec": map[string]any{"provider": provider, "origin": s.Origin, "basePath": "", "transport": kind, "authRef": "credentials/fixture", "scope": map[string]any{"tenantId": s.NativeTenantID, "containerIds": []string{s.ContainerID}}, "apiVersion": s.APIVersion}})
	cfg := backendJSON(r.Configuration{Version: r.Version, ConnectionID: "fixture", Profile: profile, Source: s})
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	rule := readsource.ReadRule{Provider: s.Provider, Origin: s.Origin, NativeTenantID: s.NativeTenantID, ContainerID: s.ContainerID, ContainerKey: s.ContainerKey, PrincipalPseudonym: s.PrincipalPseudonym, Auth: s.Auth}
	policy := backendJSON(r.AccessPolicy{Version: r.Version, LegacyReadPolicy: backendJSON(readsource.ReadPolicy{Version: s.Version, Rules: []readsource.ReadRule{rule}}), Issuers: []r.Issuer{{ID: "fixture", PublicKey: base64.StdEncoding.EncodeToString(pub)}}})
	now := time.Now().UTC()
	en := r.ReadEnrollment{Version: r.Version, ImplementationSHA256: sdk.ReadImplementationSHA256(), Action: "capture", ConfigurationSHA256: sdk.SHA256(cfg), ReadPolicySHA256: sdk.SHA256(policy), Transport: transport, Executable: executable, SecretEnvironment: map[string]string{}, IssuedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ValidUntil: now.Add(time.Minute).Format(time.RFC3339Nano), RequestSeconds: 5, CommandSeconds: 15, QualificationOnly: true, Issuer: "fixture", NativePrincipalID: "7", OutputPath: out}
	if s.Provider == "github" {
		en.SecretEnvironment = map[string]string{"GH_TOKEN": "WK_P2_TOKEN"}
	}
	en.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, sdk.EnrollmentSigningBytes(en)))
	paths := []string{}
	for name, body := range map[string][]byte{"config.json": cfg, "policy.json": policy, "enrollment.json": backendJSON(en)} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, body, 0600)
		paths = append(paths, path)
	}
	return filepath.Join(dir, "config.json"), filepath.Join(dir, "policy.json"), filepath.Join(dir, "enrollment.json")
}
func backendHTTPSource() readsource.Source {
	return readsource.Source{Version: readsource.Version, Provider: "jira", Origin: "https://fixture.atlassian.net", APIVersion: "3", NativeTenantID: "cloud", ContainerID: "project", ContainerKey: "ENG", PrincipalPseudonym: "operator", MappingVersion: "1", Auth: readsource.Auth{Mode: "basic", TokenEnv: "WK_P2_TOKEN", UsernameEnv: "WK_P2_USER"}, Scope: readsource.Scope{ItemIDs: []string{"1"}}}
}
func TestP2IdentityAndCaptureShareBudgetsAndRedactTokens(t *testing.T) {
	for _, mode := range []string{"one-request", "wrong-principal", "token-reflection", "redirect", "normal-basic"} {
		t.Run(mode, func(t *testing.T) {
			s := backendHTTPSource()
			if mode == "one-request" {
				l := readsource.DefaultLimits()
				l.Requests = 1
				s.Limits = &l
			}
			var calls atomic.Int32
			tr := readRoundTrip(func(req *http.Request) (*http.Response, error) {
				n := calls.Add(1)
				if req.Header.Get("Authorization") == "" {
					t.Error("missing explicit auth")
				}
				status := 200
				body := `{"accountId":"7","emailAddress":"u"}`
				headers := http.Header{}
				if n > 1 {
					status = 403
					body = "discard private errors"
				}
				switch mode {
				case "wrong-principal":
					body = `{"accountId":"8"}`
				case "token-reflection":
					body = `{"accountId":"7","debug":"fixture-private-token-canary"}`
				case "redirect":
					status = 302
					headers.Set("Location", "https://unapproved.test/")
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			backend := ReadBackend{LookupEnv: func(name string) (string, bool) {
				if name == "WK_P2_TOKEN" {
					return "fixture-private-token-canary", true
				}
				if name == "WK_P2_USER" {
					return "u", true
				}
				return "", false
			}, RoundTripper: tr}
			out := filepath.Join(backendDir(t), "capture")
			cfg, p, en := backendEnrollment(t, s, r.TransportIdentity{Kind: "http", Version: "go-http/v1", Principal: s.PrincipalPseudonym, AuthMode: s.Auth.Mode}, "", out)
			m, e := sdk.New(nil).WithReadBackend(backend).CaptureRead(context.Background(), cfg, p, en, "capture", "", out)
			if mode == "wrong-principal" || mode == "token-reflection" || mode == "redirect" {
				if e == nil || calls.Load() != 1 {
					t.Fatal("identity failure did not block", e, calls.Load())
				}
			} else {
				if e != nil || m.Complete {
					t.Fatal("partial fixture failed", e, errors.Unwrap(e))
				}
				if mode == "one-request" && calls.Load() != 1 {
					t.Fatal("identity request escaped total budget", calls.Load())
				}
				if mode == "normal-basic" && calls.Load() < 2 {
					t.Fatal("non-secret username reflection blocked valid identity")
				}
				if _, e = sdk.LoadReadBundle(context.Background(), out); e != nil {
					t.Fatal(e)
				}
			}
			filepath.WalkDir(out, func(path string, d os.DirEntry, e error) error {
				if e == nil && !d.IsDir() {
					raw, _ := os.ReadFile(path)
					if strings.Contains(string(raw), "fixture-private-token-canary") {
						t.Error("credential entered evidence")
					}
				}
				return nil
			})
		})
	}
}
func TestP2PinnedGhUsesExplicitEnvironmentAndImmutableOutput(t *testing.T) {
	dir := backendDir(t)
	binary := filepath.Join(dir, "gh")
	count := filepath.Join(dir, "calls")
	script := fmt.Sprintf("#!/bin/sh\n[ -z \"${P2_PARENT_CANARY+x}\" ] || exit 88\nprintf '%%s\\n' \"$*\" >> '%s'\ncase \"$*\" in\n--version) printf 'gh fixture 1\\n';;\n*'/user'*) printf '{\"id\":7}';;\n*'/issues/1'*) printf 'HTTP/1.1 200 OK\\r\\n\\r\\n{\"node_id\":\"item\",\"number\":1,\"title\":\"fixture\"}';;\n*) printf 'HTTP/1.1 200 OK\\r\\n\\r\\n{\"node_id\":\"repo\",\"owner\":{\"node_id\":\"tenant\"}}';;\nesac\n", count)
	os.WriteFile(binary, []byte(script), 0700)
	t.Setenv("P2_PARENT_CANARY", "must-not-reach-child")
	s := readsource.Source{Version: readsource.Version, Provider: "github", Origin: "https://api.github.com", APIVersion: "2022-11-28", NativeTenantID: "tenant", ContainerID: "repo", ContainerKey: "owner/repo", PrincipalPseudonym: "operator", MappingVersion: "1", Auth: readsource.Auth{Mode: "gh-session"}, Scope: readsource.Scope{ItemIDs: []string{"1"}}}
	out := filepath.Join(dir, "capture")
	cfg, p, en := backendEnrollment(t, s, r.TransportIdentity{Kind: "gh", Version: "gh fixture 1", ExecutableSHA256: sdk.SHA256([]byte(script)), Principal: s.PrincipalPseudonym, AuthMode: "gh-token"}, binary, out)
	var lookups atomic.Int32
	backend := ReadBackend{LookupEnv: func(name string) (string, bool) {
		lookups.Add(1)
		return "fixture-private-token-canary", name == "WK_P2_TOKEN"
	}}
	service := sdk.New(nil).WithReadBackend(backend)
	m, e := service.CaptureRead(context.Background(), cfg, p, en, "capture", "", out)
	if e != nil || !m.Complete {
		t.Fatal("pinned synthetic gh fixture failed", e)
	}
	if _, e = sdk.LoadReadBundle(context.Background(), out); e != nil {
		t.Fatal(e)
	}
	first, _ := os.ReadFile(count)
	if strings.Count(string(first), "/user") != 1 || strings.Contains(string(first), "fixture-private-token-canary") {
		t.Fatal("identity or argv privacy")
	}
	before := lookups.Load()
	if _, e = service.CaptureRead(context.Background(), cfg, p, en, "capture", "", out); e == nil || lookups.Load() != before {
		t.Fatal("output reuse resolved credential or succeeded")
	}
	last, _ := os.ReadFile(count)
	if string(first) != string(last) {
		t.Fatal("output reuse invoked gh")
	}
}
