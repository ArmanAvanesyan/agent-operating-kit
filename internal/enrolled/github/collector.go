// Package github provides a protected, read-only broker boundary for the
// tracker GitHub capture adapter. It does not enable writeback.
package github

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector/providers"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
)

const pinnedOrigin = "https://api.github.com"

var versionPattern = regexp.MustCompile(`^gh version [0-9]+\.[0-9]+\.[0-9]+([ .()/-][A-Za-z0-9()./ -]+)?$`)

type Config struct {
	Source           tracker.Source
	Policy           *tracker.ReadPolicy
	Executable       string
	ExecutableSHA256 string
	Version          string
	AuthReference    string
}

// Token is returned by an external protected broker. Value remains process
// memory only; source files and task data cannot name its resolver reference.
type Token struct {
	Value              string
	PrincipalPseudonym string
	GrantRef           string
	Scopes             []string
	ValidUntil         time.Time
}

type TokenResolver interface {
	Resolve(context.Context, string) (Token, error)
}
type TokenResolverFunc func(context.Context, string) (Token, error)

func (f TokenResolverFunc) Resolve(ctx context.Context, ref string) (Token, error) {
	return f(ctx, ref)
}

type Authorization struct {
	SourceSHA256 string
	PolicySHA256 string
	Origin       string
	Principal    string
	GrantRef     string
	Scopes       []string
	Method       string
	Path         string
	ItemIDs      []string
}

// Authority checks live origin, identity, grant scope and revocation state.
type Authority interface {
	CheckRead(context.Context, Authorization) error
}
type AuthorityFunc func(context.Context, Authorization) error

func (f AuthorityFunc) CheckRead(ctx context.Context, a Authorization) error { return f(ctx, a) }

type WriteAuthority interface {
	CheckWrite(context.Context, Authorization) error
}
type WriteAuthorityFunc func(context.Context, Authorization) error

func (f WriteAuthorityFunc) CheckWrite(ctx context.Context, a Authorization) error { return f(ctx, a) }

type Collector struct {
	cfg       Config
	policy    tracker.ReadPolicy
	resolver  TokenResolver
	authority Authority
}

func New(cfg Config, resolver TokenResolver, authority Authority) (*Collector, error) {
	if !processGroupSupported() {
		return nil, errors.New("GitHub collector requires enforceable process-group cleanup")
	}
	if resolver == nil || authority == nil || cfg.Policy == nil || cfg.AuthReference == "" || cfg.Version == "" || !versionPattern.MatchString(cfg.Version) || !filepath.IsAbs(cfg.Executable) || filepath.Clean(cfg.Executable) != cfg.Executable || len(cfg.ExecutableSHA256) != 64 {
		return nil, errors.New("invalid enrolled GitHub collector configuration")
	}
	if _, err := hex.DecodeString(cfg.ExecutableSHA256); err != nil {
		return nil, errors.New("invalid enrolled gh executable digest")
	}
	if err := validateReadRoute(cfg.Source, cfg.Policy); err != nil {
		return nil, err
	}
	if cfg.Source.ReadPolicySHA256 != "" && cfg.Source.ReadPolicySHA256 != tracker.ReadPolicyHash(cfg.Policy) {
		return nil, errors.New("source read-policy digest mismatch")
	}
	copyPolicy := *cfg.Policy
	copyPolicy.Rules = append([]tracker.ReadRule(nil), cfg.Policy.Rules...)
	for i := range copyPolicy.Rules {
		if copyPolicy.Rules[i].Deployment != nil {
			x := *copyPolicy.Rules[i].Deployment
			copyPolicy.Rules[i].Deployment = &x
		}
	}
	cfg.Source.Scope.ItemIDs = append([]string(nil), cfg.Source.Scope.ItemIDs...)
	if cfg.Source.Limits != nil {
		limits := *cfg.Source.Limits
		cfg.Source.Limits = &limits
	}
	cfg.Source.ReadPolicySHA256 = tracker.ReadPolicyHash(cfg.Policy)
	cfg.Policy = &copyPolicy
	return &Collector{cfg: cfg, policy: copyPolicy, resolver: resolver, authority: authority}, nil
}

func validateReadRoute(s tracker.Source, p *tracker.ReadPolicy) error {
	if tracker.ValidateSource(s) != nil || s.Provider != "github" || s.Origin != pinnedOrigin || s.Auth.Mode != "gh-session" || s.Scope.Projects || s.Scope.Relations || s.Scope.Query != "" {
		return errors.New("GitHub enrollment must be pinned to issue-only api.github.com reads")
	}
	if p == nil || tracker.ReadPolicyHash(p) == "" || p.Version != s.Version || len(p.Rules) != 1 {
		return errors.New("one exact GitHub operator read-policy rule required")
	}
	r := p.Rules[0]
	if r.Provider != "github" || r.Origin != pinnedOrigin || r.NativeTenantID != s.NativeTenantID || r.ContainerID != s.ContainerID || r.ContainerKey != s.ContainerKey || r.Auth != s.Auth || r.PrincipalPseudonym != s.PrincipalPseudonym || r.Deployment != nil {
		return errors.New("GitHub read policy does not exactly bind the enrolled source")
	}
	for _, id := range s.Scope.ItemIDs {
		if strings.HasPrefix(id, "pr:") {
			return errors.New("pull-request reads are outside the issue-only enrollment")
		}
	}
	return nil
}

// Collect captures the configured source through AOK's normal pagination,
// parsing, hashing and privacy-preserving snapshot path.
func (c *Collector) Collect(ctx context.Context, mode string) (*tracker.Capture, error) {
	if ctx == nil || (mode != "capture" && mode != "discover") {
		return nil, errors.New("invalid GitHub capture request")
	}
	if err := validateReadRoute(c.cfg.Source, &c.policy); err != nil {
		return nil, errors.New("enrolled GitHub source changed")
	}
	transport := tracker.GitHubTransport{Runner: c, Policy: &c.policy}
	captured, err := tracker.CaptureSource(ctx, c.cfg.Source, mode, nil, transport)
	if err != nil {
		return nil, err
	}
	if captured == nil || captured.Snapshot.Source.ReadPolicySHA256 != tracker.ReadPolicyHash(&c.policy) {
		return nil, errors.New("captured GitHub source binding mismatch")
	}
	return captured, nil
}

// Run implements tracker.Runner. GitHubTransport has already built the argv;
// this supervisor checks every argument against the enrolled read scope.
func (c *Collector) Run(ctx context.Context, args []string, input []byte, max int) ([]byte, error) {
	if ctx == nil || len(input) != 0 || max < 1 || max > (4<<20)+16384 || !validArgs(c.cfg.Source, args) {
		return nil, errors.New("GitHub read request is outside enrolled scope")
	}
	path := args[6]
	if err := verifyExecutable(c.cfg.Executable, c.cfg.ExecutableSHA256); err != nil {
		return nil, errors.New("pinned gh executable unavailable")
	}
	if err := c.checkVersion(ctx); err != nil {
		return nil, errors.New("pinned gh version mismatch")
	}
	token, err := c.resolver.Resolve(ctx, c.cfg.AuthReference)
	if err != nil || token.Value == "" || token.GrantRef == "" || token.PrincipalPseudonym != c.cfg.Source.PrincipalPseudonym || !time.Now().Before(token.ValidUntil) || strings.ContainsAny(token.Value, "\r\n\x00") || !validScopes(token.Scopes) {
		return nil, errors.New("GitHub broker credential unavailable")
	}
	authz := Authorization{SourceSHA256: tracker.ScopeDigest(c.cfg.Source), PolicySHA256: tracker.ReadPolicyHash(&c.policy), Origin: c.cfg.Source.Origin, Principal: token.PrincipalPseudonym, GrantRef: token.GrantRef, Scopes: append([]string(nil), token.Scopes...), Method: "GET", Path: path}
	if err := c.authority.CheckRead(ctx, authz); err != nil {
		return nil, errors.New("GitHub read authorization denied")
	}
	if err := verifyExecutable(c.cfg.Executable, c.cfg.ExecutableSHA256); err != nil {
		return nil, errors.New("gh executable changed before launch")
	}
	return runGH(ctx, c.cfg, token.Value, args, max)
}

func validScopes(scopes []string) bool {
	seen := map[string]bool{}
	for _, s := range scopes {
		if s != "issues:read" && s != "metadata:read" || seen[s] {
			return false
		}
		seen[s] = true
	}
	return seen["issues:read"] && seen["metadata:read"]
}

func validWriteScopes(scopes []string) bool {
	seen := map[string]bool{}
	for _, s := range scopes {
		if s != "issues:write" && s != "metadata:read" || seen[s] {
			return false
		}
		seen[s] = true
	}
	return seen["issues:write"] && seen["metadata:read"]
}

func validArgs(s tracker.Source, args []string) bool {
	if len(args) != 11 || args[0] != "api" || args[1] != "--hostname" || args[2] != "github.com" || args[3] != "--include" || args[4] != "--method" || args[5] != "GET" || args[7] != "-H" || args[8] != "Accept: application/vnd.github+json" || args[9] != "-H" || args[10] != "X-GitHub-Api-Version: "+s.APIVersion {
		return false
	}
	p := args[6]
	ownerRepo := strings.Split(s.ContainerKey, "/")
	if len(ownerRepo) != 2 {
		return false
	}
	root := "/repos/" + ownerRepo[0] + "/" + ownerRepo[1]
	if p == root {
		return true
	}
	if p == root+"/issues?state=all&sort=updated&direction=asc&per_page=100" {
		return len(s.Scope.ItemIDs) == 0
	}
	if strings.HasPrefix(p, root+"/issues/") {
		tail := strings.TrimPrefix(p, root+"/issues/")
		parts := strings.Split(tail, "/")
		if len(parts) < 1 || parts[0] == "" {
			return false
		}
		itemAllowed := false
		for _, id := range s.Scope.ItemIDs {
			if id == parts[0] || id == "issue:"+parts[0] {
				itemAllowed = true
				break
			}
		}
		if !itemAllowed {
			return false
		}
		if len(parts) == 1 {
			return !strings.Contains(parts[0], "?")
		}
		return len(parts) == 2 && parts[1] == "comments?per_page=100" && s.Scope.Comments
	}
	return false
}

func verifyExecutable(path, expected string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0111 == 0 || st.Size() > 512<<20 {
		return errors.New("invalid executable")
	}
	h := sha256.New()
	if _, err = io.Copy(h, io.LimitReader(f, (512<<20)+1)); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != expected {
		return errors.New("executable digest mismatch")
	}
	return nil
}

func (c *Collector) checkVersion(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "aok-gh-version-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, c.cfg.Executable, "--version")
	cmd.Env = cleanEnv(dir, "")
	cmd.Dir = dir
	if err := configureProcessGroup(cmd); err != nil {
		return err
	}
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	output, capped, err := runBoundedProcess(ctx, cmd, 1024)
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if err != nil || capped || len(lines) == 0 || strings.TrimSpace(lines[0]) != c.cfg.Version {
		return errors.New("version probe failed")
	}
	return nil
}

type limitedBuffer struct {
	buf   bytes.Buffer
	limit int
	err   error
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buf.Len() {
		remaining := b.limit - b.buf.Len() + 1
		if remaining > 0 {
			_, _ = b.buf.Write(p[:remaining])
		}
		b.err = errors.New("output limit")
		// Continue draining the child's pipe so it cannot block on a full pipe;
		// retain only the first byte beyond the cap and report after process exit.
		return len(p), nil
	}
	return b.buf.Write(p)
}
func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }
func (b *limitedBuffer) Len() int      { return b.buf.Len() }

func runGH(parent context.Context, cfg Config, token string, args []string, max int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	dir, err := os.MkdirTemp("", "aok-gh-config-")
	if err != nil {
		return nil, errors.New("isolated gh configuration unavailable")
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, cfg.Executable, args...)
	cmd.Dir = dir
	cmd.Env = cleanEnv(dir, token)
	cmd.Stdin = bytes.NewReader(nil)
	if err := configureProcessGroup(cmd); err != nil {
		return nil, err
	}
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.Stderr = io.Discard
	output, capped, err := runBoundedProcess(ctx, cmd, max)
	if capped {
		return nil, errors.New("GitHub response exceeded byte limit")
	}
	if bytes.Contains(output, []byte(token)) {
		return nil, errors.New("GitHub response contained authentication material")
	}
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("GitHub read command failed")
	}
	return output, nil
}

func cleanEnv(dir, token string) []string {
	env := []string{"HOME=" + dir, "GH_CONFIG_DIR=" + dir, "TMPDIR=" + dir, "GH_PROMPT_DISABLED=1", "NO_COLOR=1", "GIT_TERMINAL_PROMPT=0", "PATH=/usr/bin:/bin"}
	if token != "" {
		env = append(env, "GH_TOKEN="+token)
	}
	return env
}

// NewWriter reuses the existing protected ProviderWriter/HTTP transport. The
// same broker identity is resolved and checked on every authorized operation;
// construction alone executes no request and grants no connector permit.
func NewWriter(cfg Config, resolver TokenResolver, authority Authority, writeAuthority WriteAuthority, options providers.WriterOptions) (connector.Writer, error) {
	if resolver == nil || authority == nil || writeAuthority == nil || cfg.Policy == nil {
		return nil, errors.New("GitHub read/write enrollment authorities required")
	}
	if err := validateReadRoute(cfg.Source, cfg.Policy); err != nil {
		return nil, err
	}
	if cfg.Source.ReadPolicySHA256 != "" && cfg.Source.ReadPolicySHA256 != tracker.ReadPolicyHash(cfg.Policy) {
		return nil, errors.New("source read-policy digest mismatch")
	}
	cfg.Source.Scope.ItemIDs = append([]string(nil), cfg.Source.Scope.ItemIDs...)
	if cfg.Source.Limits != nil {
		limits := *cfg.Source.Limits
		cfg.Source.Limits = &limits
	}
	policyHash := tracker.ReadPolicyHash(cfg.Policy)
	cfg.Source.ReadPolicySHA256 = policyHash
	if len(cfg.Source.Scope.ItemIDs) != 1 {
		return nil, errors.New("GitHub writer helper requires one explicitly enrolled issue")
	}
	issueID := strings.TrimPrefix(cfg.Source.Scope.ItemIDs[0], "issue:")
	if !allDigits(issueID) {
		return nil, errors.New("GitHub writer helper requires a numeric issue scope")
	}
	resolverAdapter := providers.CredentialResolverFunc(func(ctx context.Context, ref string) (providers.Credential, error) {
		if ref != cfg.AuthReference {
			return providers.Credential{}, errors.New("GitHub auth reference mismatch")
		}
		t, err := resolver.Resolve(ctx, ref)
		if err != nil || t.Value == "" || t.GrantRef == "" || t.PrincipalPseudonym != cfg.Source.PrincipalPseudonym || !time.Now().Before(t.ValidUntil) || !validWriteScopes(t.Scopes) {
			return providers.Credential{}, errors.New("GitHub broker credential unavailable")
		}
		a := Authorization{SourceSHA256: tracker.ScopeDigest(cfg.Source), PolicySHA256: policyHash, Origin: cfg.Source.Origin, Principal: t.PrincipalPseudonym, GrantRef: t.GrantRef, Scopes: append([]string(nil), t.Scopes...), Method: "POST", Path: "/repos/" + cfg.Source.ContainerKey + "/issues/" + issueID + "/comments", ItemIDs: append([]string(nil), cfg.Source.Scope.ItemIDs...)}
		identity := a
		identity.Method = "GET"
		identity.Path = "/repos/" + cfg.Source.ContainerKey
		identity.Scopes = []string{"metadata:read"}
		if err := authority.CheckRead(ctx, identity); err != nil {
			return providers.Credential{}, errors.New("GitHub source identity authorization denied")
		}
		if err := writeAuthority.CheckWrite(ctx, a); err != nil {
			return providers.Credential{}, errors.New("GitHub write scope authorization denied")
		}
		return providers.Credential{Token: t.Value}, nil
	})
	return providers.NewWriter(providers.Route{Source: cfg.Source, AuthReference: cfg.AuthReference, Resolver: resolverAdapter}, options)
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var _ tracker.Runner = (*Collector)(nil)
