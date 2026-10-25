package sdk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ReadBackend is installed by trusted bootstrap, never selected by workspace data.
type ReadBackend interface {
	OpenRead(context.Context, []byte, []byte, []byte) (*ReadSession, error)
}
type ReadSession struct {
	mu                                  sync.Mutex
	used                                bool
	configRaw, policyRaw, enrollmentRaw []byte
	configuration                       r.Configuration
	enrollment                          r.ReadEnrollment
	transport                           readsource.Transport
	check                               func(context.Context) error
}

func EnrollmentSigningBytes(e r.ReadEnrollment) []byte {
	e.Signature = ""
	b, _ := json.Marshal(e)
	return append([]byte("workkit-read-enrollment/v1\n"), b...)
}
func ValidateReadEnrollment(configuration, policy, enrollment []byte, asOf time.Time) (r.Configuration, r.AccessPolicy, r.ReadEnrollment, error) {
	return validateReadEnrollment(configuration, policy, enrollment, asOf, true)
}
func validateReadEnrollment(configuration, policy, enrollment []byte, asOf time.Time, current bool) (r.Configuration, r.AccessPolicy, r.ReadEnrollment, error) {
	var c r.Configuration
	var p r.AccessPolicy
	var e r.ReadEnrollment
	if DecodeRead(configuration, &c) != nil || validateReadConfiguration(c) != nil || DecodeRead(policy, &p) != nil || p.Version != r.Version || len(p.Issuers) == 0 || len(p.Issuers) > 32 || DecodeRead(enrollment, &e) != nil {
		return c, p, e, errors.New("invalid external read enrollment")
	}
	limits, _ := readsource.EffectiveLimits(c.Source)
	start, se := time.Parse(time.RFC3339Nano, e.IssuedAt)
	end, ee := time.Parse(time.RFC3339Nano, e.ValidUntil)
	if (current && e.ImplementationSHA256 != engine.ImplementationSHA256) || !readDigestOrEmpty(e.ImplementationSHA256) || e.ImplementationSHA256 == "" || (e.Action != "capture" && e.Action != "discover") || e.RequestSeconds > limits.RequestSeconds || e.CommandSeconds > limits.CommandSeconds || e.Version != r.Version || se != nil || ee != nil || start.After(asOf) || !asOf.Before(end) || !end.After(start) || end.Sub(start) > 5*time.Minute || !e.QualificationOnly || e.ConfigurationSHA256 != SHA256(configuration) || e.ReadPolicySHA256 != SHA256(policy) || e.Transport.Principal != c.Source.PrincipalPseudonym || !readDigestOrEmpty(e.PreviousBundleSHA256) || e.NativePrincipalID == "" || !filepath.IsAbs(e.OutputPath) || filepath.Clean(e.OutputPath) != e.OutputPath || e.RequestSeconds < 1 || e.RequestSeconds > 30 || e.CommandSeconds < 1 || e.CommandSeconds > 300 {
		return c, p, e, errors.New("expired or mismatched qualification-only read enrollment")
	}
	if c.Source.Provider == "github" {
		if e.Transport.Kind != "gh" || e.Transport.Version == "" || len(e.Transport.ExecutableSHA256) != 64 || e.Executable == "" || e.Transport.AuthMode != "gh-token" || e.GitHubConfigDirectory != "" || e.GitHubConfigSHA256 != "" {
			return c, p, e, errors.New("exact gh transport binding required")
		}
	} else if e.Transport.Kind != "http" || e.Transport.AuthMode != c.Source.Auth.Mode || e.Executable != "" || e.GitHubConfigDirectory != "" || e.Transport.ExecutableSHA256 != "" || e.Transport.Version != "go-http/v1" {
		return c, p, e, errors.New("HTTP read transport binding mismatch")
	}
	var legacy readsource.ReadPolicy
	if readsource.Decode(p.LegacyReadPolicy, "read-policy", &legacy) != nil || legacy.Version != c.Source.Version || c.Source.ReadPolicySHA256 != "" && c.Source.ReadPolicySHA256 != SHA256(p.LegacyReadPolicy) {
		return c, p, e, errors.New("legacy read policy binding invalid")
	}
	scopeMatched := false
	for _, rule := range legacy.Rules {
		if rule.Provider == c.Source.Provider && rule.Origin == c.Source.Origin && rule.NativeTenantID == c.Source.NativeTenantID && rule.ContainerID == c.Source.ContainerID && rule.ContainerKey == c.Source.ContainerKey && rule.PrincipalPseudonym == c.Source.PrincipalPseudonym && readsource.Digest(rule.Auth) == readsource.Digest(c.Source.Auth) && readsource.Digest(rule.Deployment) == readsource.Digest(c.Source.Deployment) {
			scopeMatched = true
		}
	}
	if !scopeMatched {
		return c, p, e, errors.New("read scope is not approved")
	}
	sig, err := base64.StdEncoding.DecodeString(e.Signature)
	if err != nil {
		return c, p, e, errors.New("invalid enrollment signature")
	}
	seen := map[string]bool{}
	valid := false
	for _, issuer := range p.Issuers {
		if issuer.ID == "" || seen[issuer.ID] {
			return c, p, e, errors.New("duplicate or empty issuer")
		}
		seen[issuer.ID] = true
		key, err := base64.StdEncoding.DecodeString(issuer.PublicKey)
		if err != nil || len(key) != ed25519.PublicKeySize {
			return c, p, e, errors.New("invalid enrollment issuer key")
		}
		if issuer.ID == e.Issuer && ed25519.Verify(ed25519.PublicKey(key), EnrollmentSigningBytes(e), sig) {
			valid = true
		}
	}
	if !valid {
		return c, p, e, errors.New("untrusted read enrollment signature")
	}
	return c, p, e, nil
}

// NewReadSession validates the externally approved enrollment. It is not a live
// qualification grant. Bootstrap must provide pinned transport and identity checks.
func NewReadSession(configuration, policy, enrollment []byte, t readsource.Transport, check func(context.Context) error) (*ReadSession, error) {
	c, _, e, err := ValidateReadEnrollment(configuration, policy, enrollment, time.Now().UTC())
	if err != nil || t == nil || check == nil {
		return nil, errors.New("read session unavailable")
	}
	return &ReadSession{configRaw: append([]byte(nil), configuration...), policyRaw: append([]byte(nil), policy...), enrollmentRaw: append([]byte(nil), enrollment...), configuration: c, enrollment: e, transport: t, check: check}, nil
}
func (s *ReadSession) capture(ctx context.Context, mode string, previous *ReadBundle, reservation *engine.BundleReservation) (r.Manifest, error) {
	s.mu.Lock()
	if s.used {
		s.mu.Unlock()
		return r.Manifest{}, failure("read.session-consumed", "a read session is single-use", 3, nil)
	}
	s.used = true
	s.mu.Unlock()
	if _, _, _, e := ValidateReadEnrollment(s.configRaw, s.policyRaw, s.enrollmentRaw, time.Now().UTC()); e != nil {
		return r.Manifest{}, failure("read.enrollment", "read enrollment expired or changed", 3, e)
	}
	cfg := s.configuration
	var prior *readsource.Capture
	priorHash := ""
	if previous != nil {
		if !bytes.Equal(previous.configBytes, s.configRaw) || s.enrollment.PreviousBundleSHA256 != previous.digest || previous.manifest.ReadPolicySHA256 != SHA256(s.policyRaw) || readsource.Digest(previous.manifest.Transport) != readsource.Digest(s.enrollment.Transport) {
			return r.Manifest{}, failure("read.checkpoint-drift", "resume configuration, policy or transport changed", 4, nil)
		}
		prior = previous.capture
		priorHash = previous.digest
	}
	until, _ := time.Parse(time.RFC3339Nano, s.enrollment.ValidUntil)
	deadline := time.Now().Add(time.Duration(s.enrollment.CommandSeconds) * time.Second)
	if until.Before(deadline) {
		deadline = until
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	if e := s.check(ctx); e != nil {
		return r.Manifest{}, failure("read.identity", "current transport/account identity could not be verified", 3, e)
	}
	captured, e := readsource.CaptureRead(ctx, cfg.Source, mode, prior, s.transport)
	if e != nil {
		return r.Manifest{}, readFailure(e)
	}
	m, files, e := buildReadBundle(s.configRaw, captured, s.enrollment.Transport, SHA256(s.policyRaw), priorHash, readEvidence{Policy: s.policyRaw, Enrollment: s.enrollmentRaw, Lineage: readParentEvidence(previous)})
	if e == nil {
		preserve, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		e = reservation.Write(preserve, files)
	}
	if e != nil {
		failureFiles := map[string][]byte{}
		pins := []map[string]any{}
		total := 0
		names := []string{}
		for name := range captured.Files {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			raw := captured.Files[name]
			retained := len(raw) <= engine.MaxDocumentBytes && total+len(raw) <= engine.MaxTotalBytes-engine.MaxDocumentBytes
			if retained {
				failureFiles["assets/failed-source/"+name] = raw
				total += len(raw)
			}
			pins = append(pins, map[string]any{"path": name, "sha256": SHA256(raw), "retained": retained})
		}
		failureFiles["capture-failure.json"] = encodedRead(map[string]any{"version": r.Version, "status": "unsealed-output-failure", "sourceAssets": pins, "authorizationGranted": false, "executionReady": false})
		preserve, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = reservation.Write(preserve, failureFiles)
		return m, readFailure(e)
	}
	return m, nil
}
func (s *Service) WithReadBackend(b ReadBackend) *Service {
	return &Service{legacy: s.legacy, reads: b}
}
func (s *Service) CaptureRead(ctx context.Context, configuration, policy, enrollment, mode, previous, out string) (r.Manifest, error) {
	if s.reads == nil {
		return r.Manifest{}, failure("read.unavailable", "protected read bootstrap unavailable", 3, nil)
	}
	if mode != "capture" && mode != "discover" {
		return r.Manifest{}, readFailure(errors.New("invalid read operation"))
	}
	_, cfg, e := LoadReadConfiguration(ctx, configuration)
	if e != nil {
		return r.Manifest{}, readFailure(e)
	}
	p, e := ReadLocal(ctx, policy)
	if e != nil {
		return r.Manifest{}, readFailure(e)
	}
	en, e := ReadLocal(ctx, enrollment)
	if e != nil {
		return r.Manifest{}, readFailure(e)
	}
	var prior *ReadBundle
	if previous != "" {
		prior, e = LoadReadBundle(ctx, previous)
		if e != nil {
			return r.Manifest{}, e
		}
		if prior.manifest.Complete {
			return r.Manifest{}, failure("read.complete", "a complete capture cannot be resumed", 3, nil)
		}
	}
	_, _, approved, e := ValidateReadEnrollment(cfg, p, en, time.Now().UTC())
	if e != nil {
		return r.Manifest{}, failure("read.enrollment", "read enrollment is invalid", 3, e)
	}
	if prior != nil {
		if !bytes.Equal(prior.configBytes, cfg) || prior.manifest.ImplementationSHA256 != engine.ImplementationSHA256 || prior.manifest.Mode != mode || prior.digest != approved.PreviousBundleSHA256 || prior.manifest.ReadPolicySHA256 != SHA256(p) || readsource.Digest(prior.manifest.Transport) != readsource.Digest(approved.Transport) {
			return r.Manifest{}, failure("read.checkpoint-drift", "resume inputs differ from signed prior bundle", 4, nil)
		}
	} else if approved.PreviousBundleSHA256 != "" {
		return r.Manifest{}, failure("read.checkpoint-drift", "signed enrollment requires exact prior bundle", 4, nil)
	}
	if approved.Action != mode {
		return r.Manifest{}, failure("read.operation", "signed read operation mismatch", 3, nil)
	}
	absolute, e := filepath.Abs(out)
	if e != nil || absolute != approved.OutputPath {
		return r.Manifest{}, failure("read.output", "output differs from signed enrollment", 3, e)
	}
	if prior != nil {
		parent, e := filepath.Abs(prior.root)
		if e != nil {
			return r.Manifest{}, readFailure(e)
		}
		rel, e := filepath.Rel(parent, absolute)
		if e == nil && (rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return r.Manifest{}, failure("read.output-overlap", "output overlaps immutable prior evidence", 4, nil)
		}
	}
	reservation, e := engine.ReserveBundle(ctx, out)
	if e != nil {
		return r.Manifest{}, failure("read.output", "fresh private output could not be reserved", 1, e)
	}
	completed := false
	defer func() {
		if !completed {
			preserve, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_ = reservation.Write(preserve, map[string][]byte{"failure.json": encodedRead(map[string]any{"version": r.Version, "status": "blocked-or-incomplete", "configurationSha256": SHA256(cfg), "enrollmentSha256": SHA256(en), "authorizationGranted": false, "executionReady": false, "liveQualification": "not-established", "productionActivated": false})})
		}
	}()
	session, e := s.reads.OpenRead(ctx, cfg, p, en)
	if e != nil {
		return r.Manifest{}, failure("read.blocked", "external read enrollment, credentials or transport unavailable", 3, e)
	}
	result, e := session.capture(ctx, mode, prior, reservation)
	completed = e == nil
	return result, e
}

func ReadImplementationSHA256() string { return engine.ImplementationSHA256 }
