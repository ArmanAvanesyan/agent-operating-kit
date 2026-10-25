package workkit

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ReadBackend is explicit trusted bootstrap. There is no login, install, token
// refresh, retry, account fallback, mutation API or qualification promotion.
type ReadBackend struct {
	LookupEnv    func(string) (string, bool)
	RoundTripper http.RoundTripper
}

var secretName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (b ReadBackend) OpenRead(ctx context.Context, configRaw, policyRaw, enrollmentRaw []byte) (*sdk.ReadSession, error) {
	cfg, policy, en, e := sdk.ValidateReadEnrollment(configRaw, policyRaw, enrollmentRaw, time.Now().UTC())
	if e != nil {
		return nil, e
	}
	legacy, e := tracker.ReadPolicyFromBytes(policy.LegacyReadPolicy)
	if e != nil {
		return nil, e
	}
	lookup := b.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	secrets := map[string]string{}
	resolve := func(name string) (string, error) {
		if !secretName.MatchString(name) {
			return "", errors.New("invalid opaque secret reference")
		}
		v, ok := lookup(name)
		if !ok || len(v) < 8 || len(v) > 4096 || strings.ContainsAny(v, "\r\n\x00") {
			return "", errors.New("read credential unavailable")
		}
		return v, nil
	}
	limits, _ := readsource.EffectiveLimits(cfg.Source)
	if en.RequestSeconds > limits.RequestSeconds {
		en.RequestSeconds = limits.RequestSeconds
	}
	budget := &readBudget{requests: limits.Requests, bytes: limits.TotalBytes, response: limits.ResponseBytes}
	checkNow := func(ctx context.Context) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		_, _, _, e := sdk.ValidateReadEnrollment(configRaw, policyRaw, enrollmentRaw, time.Now().UTC())
		return e
	}
	var transport readsource.Transport
	var identity func(context.Context) error
	if cfg.Source.Provider == "github" {
		if b.RoundTripper != nil {
			return nil, errors.New("gh transport cannot switch to HTTP")
		}
		child := []string{"GH_PROMPT_DISABLED=1", "GH_PAGER=cat", "GH_HOST=" + githubHost(cfg.Source.Origin)}
		if len(en.SecretEnvironment) != 1 || en.GitHubConfigDirectory != "" || en.GitHubConfigSHA256 != "" {
			return nil, errors.New("one explicit gh credential route required")
		}
		for key, ref := range en.SecretEnvironment {
			want := "GH_TOKEN"
			if githubHost(cfg.Source.Origin) != "github.com" {
				want = "GH_ENTERPRISE_TOKEN"
			}
			if key != want {
				return nil, errors.New("unapproved gh child environment")
			}
			v, e := resolve(ref)
			if e != nil {
				return nil, e
			}
			secrets[ref] = v
			child = append(child, key+"="+v)
		}
		if en.GitHubConfigDirectory != "" {
			if en.GitHubConfigSHA256 == "" {
				return nil, errors.New("native gh configuration pin required")
			}
			child = append(child, "GH_CONFIG_DIR="+en.GitHubConfigDirectory)
		} else if len(en.SecretEnvironment) != 1 || en.GitHubConfigSHA256 != "" {
			return nil, errors.New("missing explicit gh credential route")
		}
		runner := &pinnedReadRunner{en: en, env: child, secrets: secrets, check: checkNow}
		verify := func(ctx context.Context) error {
			if e := runner.pin(ctx); e != nil {
				return e
			}
			out, e := runner.execute(ctx, []string{"--version"}, nil, 16384)
			if e != nil || strings.TrimSpace(string(out)) != en.Transport.Version {
				return errors.New("gh version changed")
			}
			max, e := budget.begin()
			if e != nil {
				return e
			}
			out, e = runner.Run(ctx, []string{"api", "--hostname", githubHost(cfg.Source.Origin), "--method", "GET", "/user"}, nil, max)
			if e != nil {
				return e
			}
			if e = budget.consume(len(out)); e != nil {
				return e
			}
			return checkPrincipal(out, cfg.Source.Provider, en.NativePrincipalID)
		}
		transport = tracker.GitHubTransport{Runner: runner, Policy: legacy}
		identity = verify
	} else {
		if len(en.SecretEnvironment) != 0 || en.GitHubConfigSHA256 != "" {
			return nil, errors.New("HTTP auth must use exact approved source references")
		}
		token, e := resolve(cfg.Source.Auth.TokenEnv)
		if e != nil {
			return nil, e
		}
		secrets[cfg.Source.Auth.TokenEnv] = token
		if cfg.Source.Auth.Mode == "basic" {
			user, ok := lookup(cfg.Source.Auth.UsernameEnv)
			if !ok || user == "" || len(user) > 1024 || strings.ContainsAny(user, "\r\n\x00") {
				return nil, errors.New("basic principal reference unavailable")
			}
			secrets[cfg.Source.Auth.UsernameEnv] = user
		}
		roots := []byte(nil)
		round := b.RoundTripper
		if cfg.Source.Provider == "jira-datacenter" {
			if round != nil || en.TLSRootsPath == "" {
				return nil, errors.New("Data Center requires pinned native TLS trust")
			}
			roots, e = sdk.ReadLocal(ctx, en.TLSRootsPath)
			if e != nil || sdk.SHA256(roots) != cfg.Source.Deployment.TLSTrustSHA256 {
				return nil, errors.New("Data Center trust changed")
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(roots) {
				return nil, errors.New("invalid Data Center TLS roots")
			}
			round = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
		} else if en.TLSRootsPath != "" {
			return nil, errors.New("unexpected TLS trust override")
		}
		get := func(k string) (string, bool) { v, ok := secrets[k]; return v, ok }
		transport = tracker.HTTPTransport{Policy: legacy, LookupEnv: get, RoundTripper: b.RoundTripper, DCRootsPEM: roots}
		identity = func(ctx context.Context) error {
			if e := checkNow(ctx); e != nil {
				return e
			}
			return probeHTTPIdentity(ctx, cfg.Source, en.NativePrincipalID, secrets, round, en.RequestSeconds, budget)
		}
	}
	guarded := readsource.Transport(&frozenReadTransport{sourceSHA: readsource.Digest(cfg.Source), base: transport, check: checkNow, seconds: en.RequestSeconds, budget: budget})
	return sdk.NewReadSession(configRaw, policyRaw, enrollmentRaw, guarded, identity)
}

type frozenReadTransport struct {
	sourceSHA string
	base      readsource.Transport
	check     func(context.Context) error
	seconds   int
	budget    *readBudget
}

func (t *frozenReadTransport) Do(ctx context.Context, s readsource.Source, req readsource.Request) (readsource.Response, error) {
	if readsource.Digest(s) != t.sourceSHA || readsource.ValidateRequest(s, req) != nil {
		return readsource.Response{}, errors.New("frozen read scope mismatch")
	}
	if e := t.check(ctx); e != nil {
		return readsource.Response{}, e
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(t.seconds)*time.Second)
	defer cancel()
	max, e := t.budget.begin()
	if e != nil {
		return readsource.Response{}, &readsource.TransportError{Disposition: "incomplete", Message: "shared read budget exhausted"}
	}
	limits, _ := readsource.EffectiveLimits(s)
	limits.ResponseBytes = max
	s.Limits = &limits
	response, e := t.base.Do(ctx, s, req)
	if budgetErr := t.budget.consume(len(response.Body)); budgetErr != nil {
		return readsource.Response{}, budgetErr
	}
	return response, e
}
func githubHost(origin string) string {
	u, _ := url.Parse(origin)
	if u.Host == "api.github.com" {
		return "github.com"
	}
	return u.Host
}

type pinnedReadRunner struct {
	en      r.ReadEnrollment
	env     []string
	secrets map[string]string
	check   func(context.Context) error
}
type boundedReadOutput struct {
	bytes.Buffer
	max int
}

func (b *boundedReadOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("bounded gh output exceeded")
	}
	return b.Buffer.Write(p)
}
func (r *pinnedReadRunner) pin(ctx context.Context) error {
	if !filepath.IsAbs(r.en.Executable) {
		return errors.New("absolute pinned executable required")
	}
	b, e := sdk.ReadLocal(ctx, r.en.Executable)
	if e != nil {
		// Executables may exceed the 4MiB document bound; stream a fixed-size regular
		// file after safe-path checks, without changing document/asset limits.
		return r.pinLargeExecutable(ctx)
	}
	if sdk.SHA256(b) != r.en.Transport.ExecutableSHA256 {
		return errors.New("executable pin changed")
	}
	return r.pinConfig(ctx)
}
func (r *pinnedReadRunner) pinLargeExecutable(ctx context.Context) error {
	p := r.en.Executable
	for cur := p; ; cur = filepath.Dir(cur) {
		st, e := os.Lstat(cur)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe gh executable path")
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	f, e := os.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 256<<20 {
		return errors.New("unsafe gh executable")
	}
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(f, 256<<20+1))
	after, ae := os.Stat(p)
	if e != nil || ae != nil || n != st.Size() || !os.SameFile(st, after) || st.Size() != after.Size() || !st.ModTime().Equal(after.ModTime()) || hex.EncodeToString(h.Sum(nil)) != r.en.Transport.ExecutableSHA256 {
		return errors.New("gh executable identity changed")
	}
	if e = r.check(ctx); e != nil {
		return e
	}
	return r.pinConfig(ctx)
}
func (r *pinnedReadRunner) pinConfig(ctx context.Context) error {
	if r.en.GitHubConfigDirectory != "" {
		b, e := sdk.ReadLocal(ctx, filepath.Join(r.en.GitHubConfigDirectory, "hosts.yml"))
		if e != nil || sdk.SHA256(b) != r.en.GitHubConfigSHA256 {
			return errors.New("gh native account configuration changed")
		}
	}
	return r.check(ctx)
}
func (r *pinnedReadRunner) execute(ctx context.Context, args []string, input []byte, max int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(r.en.RequestSeconds)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.en.Executable, args...)
	cmd.Env = append([]string(nil), r.env...)
	cmd.Dir = filepath.Dir(r.en.Executable)
	cmd.Stdin = bytes.NewReader(input)
	out := &boundedReadOutput{max: max}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	e := cmd.Run()
	for _, v := range r.secrets {
		if bytes.Contains(out.Bytes(), []byte(v)) {
			return nil, errors.New("credential reflected by child")
		}
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if e != nil && out.Len() == 0 {
		return nil, errors.New("bounded gh read failed")
	}
	return append([]byte(nil), out.Bytes()...), e
}
func (r *pinnedReadRunner) Run(ctx context.Context, args []string, input []byte, max int) ([]byte, error) {
	if e := r.pin(ctx); e != nil {
		return nil, e
	}
	return r.execute(ctx, args, input, max)
}
func probeHTTPIdentity(ctx context.Context, s readsource.Source, want string, secrets map[string]string, round http.RoundTripper, seconds int, budget *readBudget) error {
	path := ""
	method := "GET"
	var body io.Reader
	switch s.Provider {
	case "azure-devops":
		parts := strings.Split(s.ContainerKey, "/")
		path = "/" + parts[0] + "/_apis/connectionData?connectOptions=0"
	case "linear":
		path = "/graphql"
		method = "POST"
		body = strings.NewReader(`{"query":"query WorkKitIdentity { viewer { id } organization { id } }"}`)
	case "jira":
		path = "/rest/api/3/myself"
		if s.Origin == "https://api.atlassian.com" {
			path = "/ex/jira/" + s.NativeTenantID + path
		}
	case "jira-datacenter":
		path = s.Deployment.BasePath + "/rest/api/2/myself"
	default:
		return errors.New("identity probe unsupported")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, method, s.Origin+path, body)
	if e != nil {
		return e
	}
	token := secrets[s.Auth.TokenEnv]
	switch s.Auth.Mode {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+token)
	case "api-key":
		req.Header.Set("Authorization", token)
	case "pat":
		req.SetBasicAuth("", token)
	case "basic":
		req.SetBasicAuth(secrets[s.Auth.UsernameEnv], token)
	default:
		return errors.New("identity auth unsupported")
	}
	req.Header.Set("Accept", "application/json")
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	max, e := budget.begin()
	if e != nil {
		return e
	}
	client := http.Client{Transport: round, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, e := client.Do(req)
	if e != nil {
		return errors.New("identity request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return errors.New("identity request blocked")
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, int64(max)+1))
	if e != nil || len(raw) > max {
		return errors.New("identity response bounds")
	}
	if e = budget.consume(len(raw)); e != nil {
		return e
	}
	for name, v := range secrets {
		if name == s.Auth.UsernameEnv {
			continue
		}
		if bytes.Contains(raw, []byte(v)) {
			return errors.New("identity response reflected credential")
		}
	}
	if s.Provider == "linear" {
		var doc map[string]json.RawMessage
		if sdk.DecodeRead(raw, &doc) != nil {
			return errors.New("invalid identity response")
		}
		var data map[string]json.RawMessage
		if len(doc["errors"]) > 0 && string(doc["errors"]) != "[]" && string(doc["errors"]) != "null" || sdk.DecodeRead(doc["data"], &data) != nil {
			return errors.New("partial identity response")
		}
		var org struct {
			ID string `json:"id"`
		}
		if sdk.DecodeRead(data["organization"], &org) != nil || org.ID != s.NativeTenantID {
			return errors.New("identity tenant mismatch")
		}
	}
	return checkPrincipal(raw, s.Provider, want)
}
func checkPrincipal(raw []byte, provider, want string) error {
	var doc map[string]json.RawMessage
	if sdk.DecodeRead(raw, &doc) != nil {
		return errors.New("invalid identity response")
	}
	key := "id"
	switch provider {
	case "azure-devops":
		if sdk.DecodeRead(doc["authenticatedUser"], &doc) != nil {
			return errors.New("missing native principal")
		}
	case "linear":
		var data map[string]json.RawMessage
		if sdk.DecodeRead(doc["data"], &data) != nil || sdk.DecodeRead(data["viewer"], &doc) != nil {
			return errors.New("missing native principal")
		}
	case "jira":
		key = "accountId"
	case "jira-datacenter":
		key = "key"
	}
	id := ""
	if e := json.Unmarshal(doc[key], &id); e != nil {
		var n json.Number
		if e = json.Unmarshal(doc[key], &n); e == nil {
			id = string(n)
		}
	}
	if id == "" || id != want {
		return fmt.Errorf("native principal does not match enrolled account")
	}
	return nil
}

var _ sdk.ReadBackend = ReadBackend{}

type readBudget struct {
	mu                        sync.Mutex
	requests, bytes, response int
}

func (b *readBudget) begin() (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.requests < 1 || b.bytes < 1 {
		return 0, errors.New("shared read budget exhausted")
	}
	b.requests--
	limit := b.response
	if b.bytes < limit {
		limit = b.bytes
	}
	return limit, nil
}
func (b *readBudget) consume(n int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > b.bytes {
		b.bytes = 0
		return errors.New("shared read byte budget exceeded")
	}
	b.bytes -= n
	return nil
}
