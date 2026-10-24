package pilot

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

const PromotionGrantVersion = "aok-pilot-promotion-grant/v1"
const DrillReceiptVersion = "aok-pilot-operational-drill/v1"

func promotionPayload(g PromotionGrant) []byte { g.Signature = ""; b, _ := json.Marshal(g); return b }
func SignPromotionGrant(g PromotionGrant, key ed25519.PrivateKey) PromotionGrant {
	g.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, promotionPayload(g)))
	return g
}

// VerifyPromotionGrant validates a narrow human-signed grant against the exact
// review report. It requires every hard gate to have passed already and never
// mutates the assessment into execution readiness.
func VerifyPromotionGrant(m Manifest, a Assessment, b Bundle, g PromotionGrant, trust PromotionTrust) (*VerifiedPromotionGrant, error) {
	if err := ValidateManifest(m); err != nil {
		return nil, err
	}
	if trust.Principal == "" || trust.Role != "pilot-promotion-authority" || len(trust.PublicKey) != ed25519.PublicKeySize || trust.MaxTTL <= 0 || trust.RevocationVerifier == nil {
		return nil, errors.New("trusted promotion principal, role, key, finite TTL and current revocation verifier required")
	}
	if !validAssessment(a) || a.Version != Version || a.Status != "eligible-for-review" || a.ManifestSHA256 != digest(m) || a.BundleSHA256 != digest(b) || b.ManifestSHA256 != digest(m) || a.ProductionEligible || a.AuthorizationGranted || a.ExecutionReady || a.ShadowEligible < m.MinimumShadowOpportunities || len(a.Reasons) > 0 {
		return nil, errors.New("assessment is not a complete review-only report for this campaign")
	}
	if b.UnauthorizedRequests != 0 || b.HardGateBypasses != 0 || b.FalseAcceptances != 0 || b.OverAdmissions != 0 || b.AmbiguousResends != 0 || b.ConsequentialMisses != 0 {
		return nil, errors.New("hard safety counts must be zero")
	}
	if len(a.Metrics) != len(m.Metrics) {
		return nil, errors.New("assessment omitted preregistered metrics")
	}
	for _, r := range a.Metrics {
		if !r.Supported || !r.Pass || r.Missing != 0 || r.Censored != 0 || r.Unknown != 0 {
			return nil, errors.New("every metric must be supported and pass with complete data")
		}
	}
	if g.Version != PromotionGrantVersion || g.ID == "" || g.ApprovedBy != trust.Principal || g.ManifestSHA256 != digest(m) || g.AssessmentSHA256 != digest(a) || g.BundleSHA256 != digest(b) || g.ConsumptionUnit != m.ConsumptionUnit || g.MaximumAttempts < 1 || g.MaximumAttempts > m.MaximumAttempts || g.MaximumSeconds < 1 || g.MaximumSeconds > m.MaximumSeconds {
		return nil, errors.New("promotion grant scope or budget mismatch")
	}
	cap, e := parseRational(g.MaximumConsumption)
	mcap, me := parseRational(m.MaximumConsumption)
	if e != nil || me != nil || cap.Sign() <= 0 || cap.Cmp(mcap) > 0 {
		return nil, errors.New("promotion consumption cap exceeds campaign cap")
	}
	if len(g.RouteSHA256Allowlist) == 0 {
		return nil, errors.New("promotion grant needs exact routes")
	}
	seen := map[string]bool{}
	for _, rh := range g.RouteSHA256Allowlist {
		found := false
		for _, r := range m.Routes {
			if digest(r) == rh {
				found = true
			}
		}
		if !found || seen[rh] {
			return nil, errors.New("promotion grant route outside campaign or duplicated")
		}
		seen[rh] = true
	}
	issued, ie := time.Parse(time.RFC3339, g.IssuedAt)
	expires, ee := time.Parse(time.RFC3339, g.ExpiresAt)
	now := time.Now().UTC()
	if ie != nil || ee != nil || issued.After(now) || !expires.After(now) || !expires.After(issued) || expires.Sub(issued) > trust.MaxTTL {
		return nil, errors.New("promotion grant issue/expiry time invalid or exceeds trusted maximum TTL")
	}
	sig, e := base64.StdEncoding.DecodeString(g.Signature)
	if e != nil || !ed25519.Verify(trust.PublicKey, promotionPayload(g), sig) {
		return nil, errors.New("invalid promotion signature")
	}
	if e = trust.RevocationVerifier.VerifyPromotionGrantCurrent(g, now); e != nil {
		return nil, errors.New("promotion grant is revoked or current status is unknown")
	}
	return &VerifiedPromotionGrant{grant: g}, nil
}

func drillPayload(r OperationalDrillReceipt) []byte {
	r.Signature = ""
	b, _ := json.Marshal(r)
	return b
}
func SignOperationalDrill(r OperationalDrillReceipt, key ed25519.PrivateKey) OperationalDrillReceipt {
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, drillPayload(r)))
	return r
}
func VerifyOperationalDrill(m Manifest, r OperationalDrillReceipt, issuer string, key ed25519.PublicKey) error {
	if r.Version != DrillReceiptVersion || r.ID == "" || r.Issuer != issuer || r.ManifestSHA256 != digest(m) || r.Outcome != "passed" || r.Drill == "" || len(key) != ed25519.PublicKeySize {
		return errors.New("operational drill receipt binding invalid")
	}
	route := false
	for _, x := range m.Routes {
		if digest(x) == r.RouteSHA256 {
			route = true
		}
	}
	if !route {
		return errors.New("drill route outside campaign")
	}
	observed, e := time.Parse(time.RFC3339, r.ObservedAt)
	if e != nil || observed.After(time.Now().UTC()) {
		return errors.New("drill observation timestamp invalid or future")
	}
	measured, e := parseFinite(r.MeasuredSeconds)
	if e != nil || measured < 0 || r.MaximumSeconds <= 0 || measured > float64(r.MaximumSeconds) {
		return errors.New("drill exceeded its declared recovery bound")
	}
	sig, e := base64.StdEncoding.DecodeString(r.Signature)
	if e != nil {
		return errors.New("invalid drill signature encoding")
	}
	if !ed25519.Verify(key, drillPayload(r), sig) {
		return errors.New("invalid operational drill signature")
	}
	return nil
}
