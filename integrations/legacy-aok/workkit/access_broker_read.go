package workkit

import (
	"context"
	"errors"
	a "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/accessv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"net/http"
	"net/url"
	"time"
)

type credentialReadSnapshot struct {
	configuration           r.Configuration
	raw, policy, enrollment []byte
}

func (b AccessBackend) captureWithCredential(ctx context.Context, c a.Configuration, token string, check func() error) error {
	cfg, raw, p, en, e := loadCredentialRead(ctx, c)
	if e != nil {
		return e
	}
	return b.captureWithCredentialSnapshot(ctx, c, token, check, &credentialReadSnapshot{cfg, raw, p, en})
}
func (b AccessBackend) captureWithCredentialSnapshot(ctx context.Context, c a.Configuration, token string, check func() error, snapshot *credentialReadSnapshot, usernames ...string) error {
	if c.Read == nil {
		return errors.New("read inputs required")
	}
	if snapshot == nil {
		return errors.New("captured read inputs required")
	}
	cfg, raw, policy, en := snapshot.configuration, snapshot.raw, snapshot.policy, snapshot.enrollment
	var e error

	transport := b.RoundTripper
	if transport == nil && cfg.Source.Provider != "github" && cfg.Source.Provider != "jira-datacenter" {
		tr := http.DefaultTransport.(*http.Transport).Clone()
		tr.Proxy = nil
		defer tr.CloseIdleConnections()
		transport = tr
	}
	if c.Provider == "jira-cloud" && c.Mode == "oauth2" {
		base := transport
		if base == nil {
			tr := http.DefaultTransport.(*http.Transport).Clone()
			tr.Proxy = nil
			defer tr.CloseIdleConnections()
			base = tr
		}
		transport = nativeRoundTrip(func(req *http.Request) (*http.Response, error) {
			if req.URL.Scheme != "https" || req.URL.Host != mustHost(c.Origin) {
				return nil, errors.New("read gateway source origin")
			}
			if e := check(); e != nil {
				return nil, e
			}
			copy := req.Clone(req.Context())
			target, _ := url.Parse("https://api.atlassian.com/ex/jira/" + url.PathEscape(c.TenantID) + req.URL.EscapedPath())
			target.RawQuery = req.URL.RawQuery
			copy.URL = target
			copy.Host = "api.atlassian.com"
			return base.RoundTrip(copy)
		})
	}
	username := ""
	if cfg.Source.Auth.UsernameEnv != "" {
		if cfg.Source.Auth.UsernameEnv != c.SecretReferences["username"] {
			return errors.New("read username reference mismatch")
		}
		if len(usernames) != 1 || usernames[0] == "" {
			return errors.New("resolved read username required")
		}
		username = usernames[0]
	}
	lookup := func(ref string) (string, bool) {
		if ref == cfg.Source.Auth.UsernameEnv && username != "" {
			return username, true
		}
		if ref == cfg.Source.Auth.TokenEnv {
			return token, true
		}
		return "", false
	}
	// The exact signed access request and independent P2 enrollment bind both
	// credential scope and capture. No refresh or plaintext export is performed.
	if e = check(); e != nil {
		return e
	}
	manifest, e := sdk.New(nil).WithReadBackend(ReadBackend{LookupEnv: lookup, RoundTripper: transport}).CaptureReadSnapshot(ctx, raw, policy, en, "capture", "", c.Read.Output)
	if e != nil {
		return e
	}
	if !manifest.Complete {
		return &sdk.Error{Code: "read.partial", Message: "partial capture retained; scoped connection remains unverified", ExitCode: 6}
	}
	return nil
}
func mustHost(origin string) string { u, _ := url.Parse(origin); return u.Host }

func loadCredentialRead(ctx context.Context, c a.Configuration) (r.Configuration, []byte, []byte, []byte, error) {
	var cfg r.Configuration
	fail := func(e error) (r.Configuration, []byte, []byte, []byte, error) { return cfg, nil, nil, nil, e }
	if c.Read == nil {
		return fail(errors.New("exact credential read inputs required"))
	}
	pins := []a.LocalPin{c.Read.Configuration, c.Read.Policy, c.Read.Enrollment}
	captured := make([][]byte, 3)
	for i, pin := range pins {
		raw, e := sdk.ReadLocal(ctx, pin.Path)
		if e != nil || sdk.SHA256(raw) != pin.SHA256 {
			return fail(errors.New("read input drift"))
		}
		captured[i] = raw
	}
	cfg, _, enrollment, e := sdk.ValidateReadEnrollment(captured[0], captured[1], captured[2], time.Now().UTC())
	if e != nil || enrollment.NativePrincipalID != c.NativePrincipal || enrollment.OutputPath != c.Read.Output {
		return fail(errors.New("read enrollment identity mismatch"))
	}
	if cfg.Source.Auth.UsernameEnv != "" && cfg.Source.Auth.UsernameEnv != c.SecretReferences["username"] {
		return fail(errors.New("explicit read username binding required"))
	}
	provider := cfg.Source.Provider
	if provider == "jira" {
		provider = "jira-cloud"
	}
	if provider != c.Provider || cfg.ConnectionID != c.ConnectionID || cfg.Source.Origin != c.Origin || cfg.Source.NativeTenantID != c.TenantID || len(c.Containers) != 1 || cfg.Source.ContainerID != c.Containers[0] {
		return fail(errors.New("credential read scope mismatch"))
	}
	left, e := sdk.Parse(cfg.Profile, "json")
	if e != nil {
		return fail(e)
	}
	right, e := sdk.Parse(c.Profile, "json")
	if e != nil || left.ContentSHA256() != right.ContentSHA256() {
		return fail(errors.New("credential profile/deployment mismatch"))
	}
	return cfg, captured[0], captured[1], captured[2], nil
}
