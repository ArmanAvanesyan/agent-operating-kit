package pilot

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

const GrantVersion = "aok-calibration-grant/v1"

// CalibrationGrant authorizes only preregistered disposable trials. PublicKey
// is passed separately from a trusted operator channel and is never selected
// by the grant itself.
type CalibrationGrant struct {
	Version              string      `json:"version"`
	ID                   string      `json:"id"`
	ManifestSHA256       string      `json:"manifestSha256"`
	RepositoryAllowlist  []string    `json:"repositoryAllowlist"`
	AccountAllowlist     []string    `json:"accountAllowlist"`
	InputSHA256Allowlist []string    `json:"inputSha256Allowlist"`
	RouteSHA256Allowlist []string    `json:"routeSha256Allowlist"`
	ConfigurationSHA256  string      `json:"configurationSha256"`
	ProtocolSHA256       string      `json:"protocolSha256"`
	TrialIDs             []string    `json:"trialIds"`
	MaxAttempts          int         `json:"maxAttempts"`
	MaxSeconds           int         `json:"maxSeconds"`
	MaxConsumption       json.Number `json:"maxConsumption"`
	ConsumptionUnit      string      `json:"consumptionUnit"`
	ExpiresAt            string      `json:"expiresAt"`
	Signature            string      `json:"signature"`
}

type GuardReceipt struct {
	Gate           string `json:"gate"`
	EvidenceSHA256 string `json:"evidenceSha256"`
	ScopeSHA256    string `json:"scopeSha256"`
}
type GuardVerifier interface {
	Verify(GuardReceipt, string, time.Time) error
}

// ConsumptionGuard must atomically spend the trial/attempt in an authority
// domain outside cloneable pilot bundles and manifests.
type ConsumptionGuard interface {
	ConsumeCalibrationTrial(grantID, trialID string, attempt int, scopeSHA256 string) error
}
type CalibrationRequest struct {
	TrialID, Repository, Account, InputSHA256        string
	RouteSHA256, ConfigurationSHA256, ProtocolSHA256 string
	Attempt                                          int
	MaxSeconds                                       int
	Consumption                                      json.Number
	ConsumptionUnit                                  string
	Guards                                           []GuardReceipt
}

func grantPayload(g CalibrationGrant) []byte { g.Signature = ""; b, _ := json.Marshal(g); return b }
func SignCalibrationGrant(g CalibrationGrant, key ed25519.PrivateKey) CalibrationGrant {
	g.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, grantPayload(g)))
	return g
}

// AdmitCalibration composes a signed, scoped experiment grant with fresh
// externally verified guard receipts. The guard verifier is supplied by the
// caller; no executor or quality bypass is provided here.
func AdmitCalibration(m Manifest, g CalibrationGrant, tr CalibrationRequest, trustedKey ed25519.PublicKey, verifier GuardVerifier, consumption ConsumptionGuard) error {
	if consumption == nil {
		return errors.New("atomic external consumption guard required")
	}
	scope, err := VerifyCalibration(m, g, tr, trustedKey, verifier, time.Now().UTC())
	if err != nil {
		return err
	}
	return consumption.ConsumeCalibrationTrial(g.ID, tr.TrialID, tr.Attempt, scope)
}

// VerifyCalibration validates a calibration request and its fresh guard
// receipts without consuming the trial. The caller must separately use an
// atomic external consumption guard immediately before launch.
func VerifyCalibration(m Manifest, g CalibrationGrant, tr CalibrationRequest, trustedKey ed25519.PublicKey, verifier GuardVerifier, asOf time.Time) (string, error) {
	if err := ValidateManifest(m); err != nil {
		return "", err
	}
	if m.Purpose != Calibration {
		return "", errors.New("production policy pilots cannot use calibration admission")
	}
	if len(trustedKey) != ed25519.PublicKeySize || verifier == nil || asOf.IsZero() {
		return "", errors.New("trusted grant key, guard verifier, and current time required")
	}
	if g.Version != GrantVersion || g.ID == "" || g.ManifestSHA256 != digest(m) {
		return "", errors.New("calibration grant binding invalid")
	}
	if len(g.TrialIDs) == 0 || len(g.RepositoryAllowlist) == 0 || len(g.AccountAllowlist) == 0 || len(g.InputSHA256Allowlist) == 0 || len(g.RouteSHA256Allowlist) == 0 {
		return "", errors.New("grant allowlists must be nonempty")
	}
	for _, h := range g.InputSHA256Allowlist {
		if !validSHA256(h) {
			return "", errors.New("grant input allowlist requires SHA-256 values")
		}
	}
	if g.ConfigurationSHA256 != m.ConfigurationSHA256 || g.ProtocolSHA256 != m.ProtocolSHA256 {
		return "", errors.New("grant configuration or protocol binding mismatch")
	}
	for _, rh := range g.RouteSHA256Allowlist {
		found := false
		for _, route := range m.Routes {
			if digest(route) == rh {
				found = true
			}
		}
		if !found {
			return "", errors.New("grant route outside frozen routes")
		}
	}
	for _, trialID := range g.TrialIDs {
		found := false
		for _, ts := range m.Trials {
			trialGrantCap, ce := parseRational(ts.MaximumConsumption)
			grantCap, ge := parseRational(g.MaxConsumption)
			if ts.ID == trialID && ce == nil && ge == nil && contains(g.RepositoryAllowlist, ts.Repository) && contains(g.AccountAllowlist, ts.Account) && contains(g.InputSHA256Allowlist, ts.InputSHA256) && contains(g.RouteSHA256Allowlist, ts.RouteSHA256) && g.MaxAttempts <= ts.MaximumAttempts && g.MaxSeconds <= ts.MaximumSeconds && grantCap.Cmp(trialGrantCap) <= 0 {
				found = true
			}
		}
		if !found {
			return "", errors.New("grant trial outside frozen campaign")
		}
	}
	for _, input := range g.InputSHA256Allowlist {
		found := false
		for _, ts := range m.Trials {
			if ts.InputSHA256 == input {
				found = true
			}
		}
		if !found {
			return "", errors.New("grant input outside frozen trial allowlist")
		}
	}
	for _, route := range g.RouteSHA256Allowlist {
		found := false
		for _, ts := range m.Trials {
			if ts.RouteSHA256 == route {
				found = true
			}
		}
		if !found {
			return "", errors.New("grant route has no frozen trial")
		}
	}
	for _, repo := range g.RepositoryAllowlist {
		if !contains(m.AllowedRepositories, repo) {
			return "", errors.New("grant repository outside campaign allowlist")
		}
	}
	for _, acct := range g.AccountAllowlist {
		if !contains(m.AllowedAccounts, acct) {
			return "", errors.New("grant account outside campaign allowlist")
		}
	}
	sig, e := base64.StdEncoding.DecodeString(g.Signature)
	if e != nil || !ed25519.Verify(trustedKey, grantPayload(g), sig) {
		return "", errors.New("invalid calibration grant signature")
	}
	expires, e := time.Parse(time.RFC3339, g.ExpiresAt)
	if e != nil || !asOf.Before(expires) {
		return "", errors.New("calibration grant expired")
	}
	if g.MaxAttempts <= 0 || g.MaxAttempts > m.MaximumAttempts || g.MaxSeconds <= 0 || g.MaxSeconds > m.MaximumSeconds || g.ConsumptionUnit == "" || g.ConsumptionUnit != m.ConsumptionUnit {
		return "", errors.New("calibration grant needs finite positive bounds")
	}
	max, e := parseRational(g.MaxConsumption)
	manifestMax, e2 := parseRational(m.MaximumConsumption)
	if e != nil || e2 != nil || max.Sign() <= 0 || max.Cmp(manifestMax) > 0 {
		return "", errors.New("invalid grant consumption bound")
	}
	if tr.TrialID == "" || !validSHA256(tr.InputSHA256) || tr.Attempt < 1 || tr.Attempt > g.MaxAttempts || tr.MaxSeconds < 1 || tr.MaxSeconds > g.MaxSeconds || tr.ConsumptionUnit != g.ConsumptionUnit {
		return "", errors.New("trial exceeds grant attempt/time/unit bounds")
	}
	var spec *TrialSpec
	for i := range m.Trials {
		if m.Trials[i].ID == tr.TrialID {
			spec = &m.Trials[i]
			break
		}
	}
	if spec == nil || spec.Repository != tr.Repository || spec.Account != tr.Account || spec.InputSHA256 != tr.InputSHA256 || spec.RouteSHA256 != tr.RouteSHA256 || tr.ConfigurationSHA256 != m.ConfigurationSHA256 || tr.ProtocolSHA256 != m.ProtocolSHA256 || !contains(g.RouteSHA256Allowlist, tr.RouteSHA256) || !contains(g.TrialIDs, tr.TrialID) {
		return "", errors.New("request differs from frozen trial/route/configuration/protocol")
	}
	if tr.Attempt > spec.MaximumAttempts || tr.MaxSeconds > spec.MaximumSeconds || tr.ConsumptionUnit != spec.ConsumptionUnit {
		return "", errors.New("request exceeds frozen trial bounds")
	}
	used, e := parseRational(tr.Consumption)
	trialCap, e2 := parseRational(spec.MaximumConsumption)
	if e != nil || e2 != nil || used.Sign() < 0 || used.Cmp(max) > 0 || used.Cmp(trialCap) > 0 {
		return "", errors.New("trial exceeds grant consumption bound")
	}
	if !contains(g.TrialIDs, tr.TrialID) || !contains(g.RepositoryAllowlist, tr.Repository) || !contains(g.AccountAllowlist, tr.Account) || !contains(g.InputSHA256Allowlist, tr.InputSHA256) {
		return "", errors.New("trial outside signed grant scope")
	}
	if len(tr.Guards) < 4 {
		return "", errors.New("all non-quality hard-gate receipts are required")
	}
	required := map[string]bool{"controls": false, "capability": false, "capacity": false, "verification": false}
	scope := digest(trialScope{tr.TrialID, tr.Repository, tr.Account, tr.InputSHA256, tr.RouteSHA256, tr.ConfigurationSHA256, tr.ProtocolSHA256, tr.Attempt, tr.MaxSeconds, string(tr.Consumption), tr.ConsumptionUnit})
	for _, r := range tr.Guards {
		if _, ok := required[r.Gate]; !ok || required[r.Gate] {
			return "", errors.New("duplicate or unknown guard receipt")
		}
		if r.ScopeSHA256 != scope || !validSHA256(r.EvidenceSHA256) {
			return "", errors.New("guard receipt scope mismatch")
		}
		if e := verifier.Verify(r, scope, asOf); e != nil {
			return "", e
		}
		required[r.Gate] = true
	}
	for _, ok := range required {
		if !ok {
			return "", errors.New("missing non-quality hard-gate receipt")
		}
	}
	return scope, nil
}

type trialScope struct {
	TrialID             string `json:"trialId"`
	Repository          string `json:"repository"`
	Account             string `json:"account"`
	InputSHA256         string `json:"inputSha256"`
	RouteSHA256         string `json:"routeSha256"`
	ConfigurationSHA256 string `json:"configurationSha256"`
	ProtocolSHA256      string `json:"protocolSha256"`
	Attempt             int    `json:"attempt"`
	MaxSeconds          int    `json:"maxSeconds"`
	Consumption         string `json:"consumption"`
	ConsumptionUnit     string `json:"consumptionUnit"`
}

// CalibrationScope returns the exact canonical scope used by calibration
// guard receipts and the external trial consumption boundary.
func CalibrationScope(tr CalibrationRequest) string {
	return digest(trialScope{tr.TrialID, tr.Repository, tr.Account, tr.InputSHA256, tr.RouteSHA256, tr.ConfigurationSHA256, tr.ProtocolSHA256, tr.Attempt, tr.MaxSeconds, string(tr.Consumption), tr.ConsumptionUnit})
}

func contains(xs []string, s string) bool {
	ys := append([]string(nil), xs...)
	sort.Strings(ys)
	i := sort.SearchStrings(ys, s)
	return i < len(ys) && ys[i] == s
}
