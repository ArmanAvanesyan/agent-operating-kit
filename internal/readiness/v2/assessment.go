package v2

import (
	"fmt"
	"sort"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

// Policy is selected through protected operator authority, independently of the
// route and its evidence. It cannot remove universal or driver-required gates.
type Policy struct {
	Version                  string                      `json:"version"`
	ID                       string                      `json:"id"`
	RouteSHA256              string                      `json:"routeSha256"`
	MethodologyBindingSHA256 string                      `json:"methodologyBindingSha256"`
	CompletionClaim          string                      `json:"completionClaim,omitempty"`
	CompletionMilestone      string                      `json:"completionMilestone,omitempty"`
	Requirements             []readiness.GateRequirement `json:"requirements"`
	MaxEvidenceAgeSeconds    int64                       `json:"maxEvidenceAgeSeconds"`
}
type PolicyAuthority interface{ VerifyRoutePolicy(Route, Policy) error }
type Validator interface {
	ValidateRouteGate(Route, Policy, readiness.GateRequirement, time.Time) readiness.ValidationResult
}
type Report struct {
	Version                string           `json:"version"`
	RouteSHA256            string           `json:"routeSha256"`
	DriverSHA256           string           `json:"driverSha256"`
	Report                 readiness.Report `json:"report"`
	Admission              Admission        `json:"admission"`
	Implementation         string           `json:"implementation"`
	FixtureConformance     string           `json:"fixtureConformance"`
	LiveRouteQualification string           `json:"liveRouteQualification"`
	PlatformCertification  string           `json:"platformCertification"`
	ProductionActivation   string           `json:"productionActivation"`
}

func UniversalGateIDs(r Route) []string {
	gates := []string{"identity", "controls", "capability", "capacity", "quality", "verification", "authorization", "runtime", "driver-discovery", "driver-cancellation", "driver-accounting", "platform"}
	if r.Legacy.Provider != "" {
		gates = append(gates, "provider-read", "provider-operation")
	}
	if r.MethodologyRequired {
		gates = append(gates, "methodology")
	}
	sort.Strings(gates)
	return gates
}

// CompletionGateIDs returns the route-specific protected delivery gates. The
// provider operation gate is universal when a provider is selected; the
// additional gates distinguish independent acceptance, local handoff and an
// optional publication claim.
func CompletionGateIDs(r Route) []string {
	if r.Completion == nil {
		return nil
	}
	ids := []string{"acceptance", "local-handoff", "provider-operation"}
	if r.Completion.Claim == "delivered" {
		ids = append(ids, "publication")
	}
	sort.Strings(ids)
	return ids
}

func completionRequirementMatches(r Route, routeHash string, q readiness.GateRequirement) bool {
	if r.Completion == nil {
		return true
	}
	if !q.Required || q.Applicability != "applicable" || q.Scope["routeSha256"] != routeHash || q.Scope["milestone"] != r.Completion.Milestone {
		return false
	}
	if q.ID == "provider-operation" || q.ID == "publication" {
		if q.Scope["provider"] != r.Legacy.Provider || q.Scope["operation"] != r.Legacy.Operation {
			return false
		}
	}
	if q.ID == "publication" && (r.Completion.Claim != "delivered" || r.Completion.PublicationOperation != r.Legacy.Operation) {
		return false
	}
	return true
}
func Assess(r Route, p Policy, at time.Time, authority PolicyAuthority, validators map[string]Validator) (Report, error) {
	out := Report{Version: Version, RouteSHA256: usage.Digest(r), DriverSHA256: r.DriverSHA256, Implementation: "registered", FixtureConformance: "route-specific", LiveRouteQualification: "blocked", PlatformCertification: "pending", ProductionActivation: "disabled"}
	base, e := readiness.Digest(r.Legacy)
	if e != nil {
		return out, e
	}
	out.Report = readiness.Report{Version: readiness.Version, ManifestSHA256: base, PolicySHA256: usage.Digest(p), AsOf: at.UTC().Format(time.RFC3339Nano), Status: "blocked", PermittedClaim: "offline evidence only", Gates: []readiness.GateResult{}, InputSHA256s: []string{usage.Digest(r), usage.Digest(p)}, NextActions: []string{}}
	if p.Version != Version || p.ID == "" || p.RouteSHA256 != out.RouteSHA256 || at.IsZero() || p.MaxEvidenceAgeSeconds < 1 || p.MaxEvidenceAgeSeconds > 300 || len(p.Requirements) > 100 {
		return out, fmt.Errorf("invalid exact-route v2 readiness policy")
	}
	if p.MethodologyBindingSHA256 != r.MethodologyBindingSHA256 || (p.MethodologyBindingSHA256 != "") != r.MethodologyRequired {
		return out, fmt.Errorf("methodology policy/binding mismatch")
	}
	if r.Completion == nil {
		if p.CompletionClaim != "" || p.CompletionMilestone != "" {
			return out, fmt.Errorf("completion policy supplied for an unscoped route")
		}
	} else if p.CompletionClaim != r.Completion.Claim || p.CompletionMilestone != r.Completion.Milestone {
		return out, fmt.Errorf("protected completion policy claim or milestone mismatch")
	}
	// Reuse structural route/driver checks. Missing qualification remains blocked.
	if _, e = Evaluate(r, out.Report); e != nil {
		return out, e
	}
	reqs := map[string]readiness.GateRequirement{}
	for _, q := range p.Requirements {
		if q.ID == "" || reqs[q.ID].ID != "" || len(q.Bindings) > 100 || q.Validator == "" {
			return out, fmt.Errorf("invalid or duplicate readiness requirement")
		}
		reqs[q.ID] = q
	}
	trusted := authority != nil && authority.VerifyRoutePolicy(r, p) == nil
	ids := UniversalGateIDs(r)
	seen := map[string]bool{}
	present := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
		present[id] = true
	}
	for id := range reqs {
		q := reqs[id]
		if r.Completion != nil && q.Scope["provider"] != "" && q.Scope["provider"] != r.Legacy.Provider {
			continue
		}
		if !present[id] {
			present[id] = true
			ids = append(ids, id)
		}
	}
	for _, id := range CompletionGateIDs(r) {
		q, ok := reqs[id]
		if !ok || !completionRequirementMatches(r, out.RouteSHA256, q) {
			return out, fmt.Errorf("completion readiness policy omits or mis-scopes mandatory gate %q", id)
		}
	}
	sort.Strings(ids)
	all := true
	earliest := time.Time{}
	for _, id := range ids {
		gate := readiness.GateResult{ID: id, Required: true, Status: "unknown", Reason: "required protected scoped evidence unavailable", EvidenceSHA256: []string{}}
		q, exists := reqs[id]
		gate.Required = seen[id] || q.Required
		if exists && trusted && q.Required && q.Applicability == "applicable" && validators[q.Validator] != nil {
			valid := len(q.Bindings) > 0 && q.Scope["routeSha256"] == out.RouteSHA256
			for _, b := range q.Bindings {
				observed, e := time.Parse(time.RFC3339Nano, b.ObservedAt)
				until, e2 := time.Parse(time.RFC3339Nano, b.ValidUntil)
				if e != nil || e2 != nil || at.Before(observed) || !at.Before(until) || at.Sub(observed) > time.Duration(p.MaxEvidenceAgeSeconds)*time.Second || b.Revoked || !isHash(b.SHA256) || b.Origin != "live" {
					valid = false
				}
				if earliest.IsZero() || until.Before(earliest) {
					earliest = until
				}
			}
			if valid {
				v := validators[q.Validator].ValidateRouteGate(r, p, q, at)
				gate.Status = v.Status
				gate.Reason = v.Reason
				gate.EvidenceSHA256 = v.EvidenceSHA256
				if v.Status != "passed" && v.Status != "failed" && v.Status != "unknown" {
					gate.Status = "unknown"
					gate.Reason = "invalid native validator result"
				}
				until, ue := time.Parse(time.RFC3339Nano, v.ValidUntil)
				if ue != nil || !at.Before(until) {
					gate.Status = "unknown"
					gate.Reason = "native evidence expiry unknown or expired"
				} else if earliest.IsZero() || until.Before(earliest) {
					earliest = until
				}
				approved := map[string]bool{}
				for _, b := range q.Bindings {
					approved[b.SHA256] = true
				}
				for _, hash := range v.EvidenceSHA256 {
					if !isHash(hash) || !approved[hash] {
						gate.Status = "unknown"
						gate.Reason = "native result not bound to policy evidence"
					}
				}
				if gate.Status == "passed" && len(v.EvidenceSHA256) == 0 {
					gate.Status = "unknown"
					gate.Reason = "native validator supplied no verified evidence"
				}
			}
		}
		if gate.Required && gate.Status != "passed" {
			all = false
			out.Report.NextActions = append(out.Report.NextActions, id+": "+gate.Reason)
		}
		out.Report.Gates = append(out.Report.Gates, gate)
	}
	if !earliest.IsZero() {
		out.Report.EarliestValidUntil = earliest.UTC().Format(time.RFC3339Nano)
	}
	if all {
		out.Report.Status = "ready-offline"
		out.LiveRouteQualification = "evidence-verified"
	}
	if d, ok := hostdriver.Lookup(r.Legacy.Host); !ok || d.Support != hostdriver.Supported {
		out.Report.Status = "blocked"
		out.LiveRouteQualification = "unsupported"
	}
	out.Admission, e = Evaluate(r, out.Report)
	return out, e
}
