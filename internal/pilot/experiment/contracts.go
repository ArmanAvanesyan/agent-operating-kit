// Package experiment contains closed, offline contracts for one bounded
// evidence-collection trial. It contains no executor or production authority.
package experiment

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
)

const (
	ManifestVersion = "aok-experiment-manifest/v1"
	GrantVersion    = "aok-experiment-grant/v1"
	BundleVersion   = "aok-experiment-bundle/v1"
	GrantRole       = "pilot-experiment-authority"
	Purpose         = "controlled-evidence-collection"
	RiskStandard    = "standard"
	grantDomain     = "aok-experiment:grant:v1:\n"
)

type Manifest struct {
	Version             string          `json:"version"`
	ID                  string          `json:"id"`
	Purpose             string          `json:"purpose"`
	Risk                string          `json:"risk"`
	FrozenAt            string          `json:"frozenAt"`
	Route               pilot.Route     `json:"route"`
	Trial               pilot.TrialSpec `json:"trial"`
	ConfigurationSHA256 string          `json:"configurationSha256"`
	ProfileSHA256       string          `json:"profileSha256"`
	ProtocolSHA256      string          `json:"protocolSha256"`
	MaximumAttempts     int             `json:"maximumAttempts"`
	MaximumSteps        int             `json:"maximumSteps"`
	MaximumSeconds      int             `json:"maximumSeconds"`
	MaximumConsumption  json.Number     `json:"maximumConsumption"`
	ConsumptionUnit     string          `json:"consumptionUnit"`
}

type Grant struct {
	Version             string      `json:"version"`
	ID                  string      `json:"id"`
	Issuer              string      `json:"issuer"`
	Role                string      `json:"role"`
	ManifestSHA256      string      `json:"manifestSha256"`
	TrialID             string      `json:"trialId"`
	RouteSHA256         string      `json:"routeSha256"`
	InputSHA256         string      `json:"inputSha256"`
	ConfigurationSHA256 string      `json:"configurationSha256"`
	ProfileSHA256       string      `json:"profileSha256"`
	ProtocolSHA256      string      `json:"protocolSha256"`
	MaximumAttempts     int         `json:"maximumAttempts"`
	MaximumSeconds      int         `json:"maximumSeconds"`
	MaximumConsumption  json.Number `json:"maximumConsumption"`
	ConsumptionUnit     string      `json:"consumptionUnit"`
	IssuedAt            string      `json:"issuedAt"`
	ExpiresAt           string      `json:"expiresAt"`
	Signature           string      `json:"signature"`
}

type EvidenceRef struct {
	Kind        string `json:"kind"`
	SHA256      string `json:"sha256"`
	ScopeSHA256 string `json:"scopeSha256"`
}

type Bundle struct {
	Version  string        `json:"version"`
	Manifest Manifest      `json:"manifest"`
	Grant    Grant         `json:"grant"`
	Evidence []EvidenceRef `json:"evidence"`
}

// TrustPolicy is resolved outside the record through a trusted operator path.
type TrustPolicy struct {
	Principal   string
	Role        string
	PublicKey   ed25519.PublicKey
	MaxTTL      time.Duration
	Revocations RevocationChecker
}

type RevocationChecker interface{ CheckExperimentGrantCurrent(Grant, time.Time) error }

// TrialGuard is an external atomic anti-replay/consumption boundary.
type TrialGuard interface {
	ConsumeExperimentTrial(grantID, trialID, scopeSHA256 string) error
}

type Report struct {
	Version              string        `json:"version"`
	ManifestSHA256       string        `json:"manifestSha256"`
	GrantSHA256          string        `json:"grantSha256"`
	ScopeSHA256          string        `json:"scopeSha256"`
	Evidence             []EvidenceRef `json:"evidence"`
	EvidenceOnly         bool          `json:"evidenceOnly"`
	AuthorizationGranted bool          `json:"authorizationGranted"`
	ExecutionReady       bool          `json:"executionReady"`
	ProductionEligible   bool          `json:"productionEligible"`
	MaximumAttempts      int           `json:"maximumAttempts"`
	MaximumSteps         int           `json:"maximumSteps"`
	MaximumSeconds       int           `json:"maximumSeconds"`
	MaximumConsumption   json.Number   `json:"maximumConsumption"`
	ConsumptionUnit      string        `json:"consumptionUnit"`
	verified             bool
	grantID              string
	trialID              string
	expiresAt            time.Time
	seal                 [32]byte
}

type Receipt struct {
	GrantID     string `json:"grantId"`
	TrialID     string `json:"trialId"`
	ScopeSHA256 string `json:"scopeSha256"`
}

type Limits struct{ MaxBytes int64 }

func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func digest(v any) string       { b, _ := json.Marshal(v); return hashBytes(b) }
func validHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && strings.ToLower(s) == s
}
func rational(s json.Number) (*big.Rat, error) {
	if s == "" || !json.Valid([]byte(s)) || strings.ContainsAny(string(s), "eE") {
		return nil, errors.New("consumption must be a finite decimal rational")
	}
	r, ok := new(big.Rat).SetString(string(s))
	if !ok {
		return nil, errors.New("invalid rational consumption")
	}
	return r, nil
}

func ValidateManifest(m Manifest) error {
	if m.Version != ManifestVersion || m.ID == "" || m.Purpose != Purpose || m.Risk != RiskStandard {
		return errors.New("invalid experiment identity, purpose, or risk")
	}
	if _, e := time.Parse(time.RFC3339, m.FrozenAt); e != nil {
		return errors.New("experiment freeze time must be RFC3339")
	}
	if m.MaximumAttempts != 1 || m.Trial.MaximumAttempts != 1 || m.MaximumSteps != 1 || m.MaximumSeconds < 1 || m.MaximumSeconds > 60 || m.Trial.MaximumSeconds != m.MaximumSeconds {
		return errors.New("experiment requires one step, one attempt, and at most 60 seconds")
	}
	for _, h := range []string{m.ConfigurationSHA256, m.ProfileSHA256, m.ProtocolSHA256, m.Route.HostSHA256, m.Route.ControlPolicySHA256, m.Route.ConfigurationSHA256, m.Route.ProfileSHA256, m.Route.RouteManifestSHA256, m.Route.VerifierSHA256, m.Route.RubricSHA256, m.Route.EnvironmentSHA256, m.Route.AccountScopeSHA256, m.Trial.InputSHA256, m.Trial.RouteSHA256} {
		if !validHash(h) {
			return errors.New("experiment route and trial require exact SHA-256 identities")
		}
	}
	if m.Route.ConfigurationSHA256 != m.ConfigurationSHA256 || m.Route.ProfileSHA256 != m.ProfileSHA256 || m.Trial.RouteSHA256 != digest(m.Route) || m.Trial.ID == "" || m.Trial.Repository == "" || m.Trial.Account == "" || m.Route.Account != m.Trial.Account || m.Route.Account == "" {
		return errors.New("experiment route and trial bindings mismatch")
	}
	if m.Route.Host == "" || m.Route.HostVersion == "" || m.Route.Provider == "" || m.Route.Platform == "" || m.Route.Transport == "" || m.Route.Operation == "" || m.Route.AuthMode == "" || m.Route.NativeTenant == "" || m.Route.API == "" || m.Route.Origin == "" {
		return errors.New("experiment route identity is incomplete")
	}
	if m.RouteSHA() == "" {
		return errors.New("invalid route")
	}
	if m.Trial.ConsumptionUnit == "" || m.Trial.ConsumptionUnit != m.ConsumptionUnit {
		return errors.New("trial consumption unit mismatch")
	}
	c, e := rational(m.MaximumConsumption)
	tc, te := rational(m.Trial.MaximumConsumption)
	if e != nil || te != nil || c.Sign() <= 0 || tc.Sign() <= 0 || tc.Cmp(c) > 0 {
		return errors.New("invalid finite cumulative trial consumption cap")
	}
	return nil
}

func (m Manifest) RouteSHA() string { return digest(m.Route) }
func ManifestDigest(m Manifest) string {
	return hashBytes([]byte("aok-experiment:manifest:v1:\n" + digest(m)))
}
func GrantDigest(g Grant) string {
	b, _ := json.Marshal(g)
	return hashBytes([]byte(grantDomain + string(b)))
}
func grantPayload(g Grant) []byte {
	g.Signature = ""
	b, _ := json.Marshal(g)
	return append([]byte(grantDomain), b...)
}
func SignGrant(g Grant, key ed25519.PrivateKey) Grant {
	g.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, grantPayload(g)))
	return g
}

func Verify(b Bundle, trust TrustPolicy, now time.Time) (Report, error) {
	r := Report{Version: BundleVersion, EvidenceOnly: true, AuthorizationGranted: false, ExecutionReady: false, ProductionEligible: false}
	if b.Version != BundleVersion {
		return r, errors.New("invalid experiment bundle version")
	}
	if e := ValidateManifest(b.Manifest); e != nil {
		return r, e
	}
	if trust.Principal == "" || trust.Role != GrantRole || len(trust.PublicKey) != ed25519.PublicKeySize || trust.MaxTTL <= 0 || trust.Revocations == nil {
		return r, errors.New("trusted experiment issuer, role, key, TTL, and current revocation checker required")
	}
	g := b.Grant
	m := b.Manifest
	if g.Version != GrantVersion || g.ID == "" || g.Issuer != trust.Principal || g.Role != trust.Role || g.ManifestSHA256 != ManifestDigest(m) || g.TrialID != m.Trial.ID || g.RouteSHA256 != m.Trial.RouteSHA256 || g.InputSHA256 != m.Trial.InputSHA256 || g.ConfigurationSHA256 != m.ConfigurationSHA256 || g.ProfileSHA256 != m.ProfileSHA256 || g.ProtocolSHA256 != m.ProtocolSHA256 || g.MaximumAttempts != 1 || g.MaximumAttempts > m.MaximumAttempts || g.MaximumSeconds < 1 || g.MaximumSeconds > m.MaximumSeconds || g.ConsumptionUnit != m.ConsumptionUnit {
		return r, errors.New("experiment grant scope mismatch")
	}
	cap, e := rational(g.MaximumConsumption)
	manifestCap, me := rational(m.MaximumConsumption)
	trialCap, te := rational(m.Trial.MaximumConsumption)
	if e != nil || me != nil || te != nil || cap.Sign() <= 0 || cap.Cmp(manifestCap) > 0 || cap.Cmp(trialCap) > 0 {
		return r, errors.New("experiment grant consumption cap exceeds frozen bounds")
	}
	issued, ie := time.Parse(time.RFC3339, g.IssuedAt)
	expires, ee := time.Parse(time.RFC3339, g.ExpiresAt)
	frozen, fe := time.Parse(time.RFC3339, m.FrozenAt)
	if ie != nil || ee != nil || fe != nil || frozen.After(issued) || frozen.After(now) || issued.After(now) || !expires.After(now) || !expires.After(issued) || expires.Sub(issued) > trust.MaxTTL {
		return r, errors.New("experiment grant issue or expiry invalid")
	}
	sig, se := base64.StdEncoding.DecodeString(g.Signature)
	if se != nil || !ed25519.Verify(trust.PublicKey, grantPayload(g), sig) {
		return r, errors.New("invalid experiment grant signature")
	}
	if e = trust.Revocations.CheckExperimentGrantCurrent(g, now); e != nil {
		return r, fmt.Errorf("experiment grant revoked or status unknown: %w", e)
	}
	for _, x := range b.Evidence {
		if x.Kind == "" || !validHash(x.SHA256) || !validHash(x.ScopeSHA256) {
			return r, errors.New("invalid evidence reference")
		}
	}
	r.ManifestSHA256 = ManifestDigest(m)
	r.GrantSHA256 = GrantDigest(g)
	r.ScopeSHA256 = scopeDigest(m, g)
	r.Evidence = append([]EvidenceRef(nil), b.Evidence...)
	r.MaximumAttempts = g.MaximumAttempts
	r.MaximumSteps = m.MaximumSteps
	r.MaximumSeconds = g.MaximumSeconds
	r.MaximumConsumption = g.MaximumConsumption
	r.ConsumptionUnit = g.ConsumptionUnit
	r.verified = true
	r.grantID = g.ID
	r.trialID = m.Trial.ID
	r.expiresAt = expires
	r.seal = reportSeal(r)
	return r, nil
}

func scopeDigest(m Manifest, g Grant) string {
	return hashBytes([]byte("aok-experiment:trial-scope:v1:\n" + ManifestDigest(m) + ":" + GrantDigest(g)))
}

// Consume performs only the external atomic trial claim after verification.
func Consume(r Report, guard TrialGuard) (Receipt, error) {
	return consumeAt(r, guard, time.Now().UTC())
}

// consumeAt is injectable only inside the package so callers cannot choose a
// past clock to revive an expired grant.
func consumeAt(r Report, guard TrialGuard, now time.Time) (Receipt, error) {
	if !r.verified || r.seal != reportSeal(r) || now.IsZero() || !now.Before(r.expiresAt) || !r.EvidenceOnly || r.AuthorizationGranted || r.ExecutionReady || r.ProductionEligible || guard == nil {
		return Receipt{}, errors.New("verified evidence-only report and external trial guard required")
	}
	if e := guard.ConsumeExperimentTrial(r.grantID, r.trialID, r.ScopeSHA256); e != nil {
		return Receipt{}, e
	}
	return Receipt{GrantID: r.grantID, TrialID: r.trialID, ScopeSHA256: r.ScopeSHA256}, nil
}

func reportSeal(r Report) [32]byte {
	v := struct {
		Version, ManifestSHA256, GrantSHA256, ScopeSHA256                      string
		Evidence                                                               []EvidenceRef
		EvidenceOnly, AuthorizationGranted, ExecutionReady, ProductionEligible bool
		MaximumAttempts, MaximumSteps, MaximumSeconds                          int
		MaximumConsumption                                                     json.Number
		ConsumptionUnit                                                        string
		GrantID, TrialID                                                       string
		ExpiresAt                                                              string
	}{r.Version, r.ManifestSHA256, r.GrantSHA256, r.ScopeSHA256, r.Evidence, r.EvidenceOnly, r.AuthorizationGranted, r.ExecutionReady, r.ProductionEligible, r.MaximumAttempts, r.MaximumSteps, r.MaximumSeconds, r.MaximumConsumption, r.ConsumptionUnit, r.grantID, r.trialID, r.expiresAt.UTC().Format(time.RFC3339Nano)}
	b, _ := json.Marshal(v)
	return sha256.Sum256(b)
}

func ReadFile(path string, limits Limits) (Bundle, error) {
	var b Bundle
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = 1 << 20
	}
	abs, e := filepath.Abs(path)
	if e != nil {
		return b, e
	}
	for cur := abs; ; cur = filepath.Dir(cur) {
		st, se := os.Lstat(cur)
		if se != nil {
			return b, se
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return b, errors.New("symlink path component forbidden")
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	st, e := os.Lstat(abs)
	if e != nil {
		return b, e
	}
	if !st.Mode().IsRegular() {
		return b, errors.New("experiment document must be a regular file")
	}
	f, e := os.Open(abs)
	if e != nil {
		return b, e
	}
	defer f.Close()
	after, e := f.Stat()
	if e != nil || !os.SameFile(st, after) {
		return b, errors.New("experiment document changed while opening")
	}
	data, e := io.ReadAll(io.LimitReader(f, limits.MaxBytes+1))
	if e != nil {
		return b, e
	}
	if int64(len(data)) > limits.MaxBytes {
		return b, errors.New("experiment document exceeds size limit")
	}
	if e = modelcatalog.Decode(data, &b); e != nil {
		return b, e
	}
	return b, nil
}
