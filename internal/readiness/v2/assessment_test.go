package v2

import (
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"strings"
	"testing"
	"time"
)

type policyAuthority struct{}

func (policyAuthority) VerifyRoutePolicy(Route, Policy) error { return nil }

type gateValidator struct{}

func (gateValidator) ValidateRouteGate(_ Route, _ Policy, q readiness.GateRequirement, at time.Time) readiness.ValidationResult {
	return readiness.ValidationResult{Status: "passed", EvidenceSHA256: []string{q.Bindings[0].SHA256}, ValidUntil: at.Add(time.Minute).Format(time.RFC3339Nano)}
}
func assessmentFixture() (Route, Policy, time.Time) {
	h := strings.Repeat("a", 64)
	d, _ := hostdriver.Lookup("opencode")
	at := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	r := Route{Version: Version, Legacy: readiness.RouteManifest{Version: readiness.Version, ID: "fixture", Host: "opencode", HostVersion: "1", HostSHA256: h, Transport: d.Protocol, ConfigurationSHA256: h, ProfileSHA256: h, ControlPolicySHA256: h, Account: "account", AuthMode: "native-login", Pool: "pool", OS: "linux", Arch: "amd64", VerifierSHA256: h, RubricSHA256: h, EnvironmentSHA256: h}, DriverSHA256: d.Digest, Protocol: d.Protocol, PreparedSHA256: h, PolicySHA256: h, LaunchSHA256: h, InputSHA256: h}
	p := Policy{Version: Version, ID: "operator", RouteSHA256: usage.Digest(r), MaxEvidenceAgeSeconds: 300, Requirements: []readiness.GateRequirement{}}
	return r, p, at
}
func TestAssessmentDerivesUniversalsAndIsOfflineDeterministic(t *testing.T) {
	r, p, at := assessmentFixture()
	one, e := Assess(r, p, at, nil, nil)
	if e != nil {
		t.Fatal(e)
	}
	two, e := Assess(r, p, at, nil, nil)
	if e != nil || usage.Digest(one) != usage.Digest(two) {
		t.Fatal(e)
	}
	if one.Report.Status != "blocked" || len(one.Report.Gates) != len(UniversalGateIDs(r)) || one.Report.AuthorizationGranted || one.Admission.Status != "blocked" {
		t.Fatal(one)
	}
}
func TestAssessmentCannotDropUniversalOrUseCrossRouteEvidence(t *testing.T) {
	r, p, at := assessmentFixture()
	h := strings.Repeat("b", 64)
	for _, id := range UniversalGateIDs(r) {
		p.Requirements = append(p.Requirements, readiness.GateRequirement{ID: id, Required: true, Validator: "native", Applicability: "applicable", Scope: map[string]string{"routeSha256": p.RouteSHA256}, Bindings: []readiness.EvidenceBinding{{SHA256: h, Origin: "live", ObservedAt: at.Format(time.RFC3339Nano), ValidUntil: at.Add(2 * time.Minute).Format(time.RFC3339Nano)}}})
	}
	out, e := Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}})
	if e != nil || out.Report.Status != "ready-offline" || out.Admission.Status != "blocked" {
		t.Fatal(out, e)
	}
	p.Requirements[0].Scope = map[string]string{"routeSha256": strings.Repeat("c", 64)}
	out, e = Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}})
	if e != nil || out.Report.Status != "blocked" {
		t.Fatal(out, e)
	}
	p.Requirements = nil
	out, e = Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}})
	if e != nil || out.Report.Status != "blocked" {
		t.Fatal("removed mandatory gates")
	}
}
func TestOptionalCustomEvidenceDoesNotRemoveOrBlockUniversalGates(t *testing.T) {
	r, p, at := assessmentFixture()
	h := strings.Repeat("b", 64)
	for _, id := range UniversalGateIDs(r) {
		p.Requirements = append(p.Requirements, readiness.GateRequirement{ID: id, Required: true, Validator: "native", Applicability: "applicable", Scope: map[string]string{"routeSha256": p.RouteSHA256}, Bindings: []readiness.EvidenceBinding{{SHA256: h, Origin: "live", ObservedAt: at.Format(time.RFC3339Nano), ValidUntil: at.Add(2 * time.Minute).Format(time.RFC3339Nano)}}})
	}
	p.Requirements = append(p.Requirements, readiness.GateRequirement{ID: "optional-tool", Required: false, Validator: "missing", Applicability: "applicable"})
	out, e := Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}})
	if e != nil || out.Report.Status != "ready-offline" {
		t.Fatal(out, e)
	}
	for _, g := range out.Report.Gates {
		if g.ID == "optional-tool" && g.Required {
			t.Fatal("optional became mandatory")
		}
	}
	p.Requirements[0].Required = false
	out, e = Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}})
	if e != nil || out.Report.Status != "blocked" {
		t.Fatal("universal optionalized")
	}
}

func completionFixture(claim string) (Route, Policy, time.Time) {
	r, p, at := assessmentFixture()
	r.Legacy.Provider = "github"
	r.Legacy.Operation = "issue-comment"
	r.Legacy.DeliveryMilestone = "local-handoff"
	r.Completion = &CompletionRoute{Claim: claim, Milestone: "issue-comment"}
	p.CompletionClaim = claim
	p.CompletionMilestone = r.Completion.Milestone
	if claim == "delivered" {
		r.Legacy.DeliveryMilestone = "delivered"
		r.Completion.PublicationOperation = "issue-comment"
	}
	p.RouteSHA256 = usage.Digest(r)
	return r, p, at
}

func completionRequirements(r Route, p *Policy, at time.Time) {
	h := strings.Repeat("b", 64)
	ids := append([]string{}, UniversalGateIDs(r)...)
	seen := map[string]bool{}
	for _, id := range ids {
		seen[id] = true
	}
	for _, id := range CompletionGateIDs(r) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, id := range ids {
		scope := map[string]string{"routeSha256": p.RouteSHA256}
		if r.Completion != nil {
			scope["milestone"] = r.Completion.Milestone
		}
		if id == "provider-operation" || id == "publication" {
			scope["provider"] = r.Legacy.Provider
			scope["operation"] = r.Legacy.Operation
		}
		p.Requirements = append(p.Requirements, readiness.GateRequirement{ID: id, Required: true, Validator: "native", Applicability: "applicable", Scope: scope, Bindings: []readiness.EvidenceBinding{{SHA256: h, Origin: "live", ObservedAt: at.Format(time.RFC3339Nano), ValidUntil: at.Add(2 * time.Minute).Format(time.RFC3339Nano)}}})
	}
}

func TestCompletionRequiresExactOperationMilestoneAndPublicationForDelivered(t *testing.T) {
	for _, claim := range []string{"local-handoff", "delivered"} {
		r, p, at := completionFixture(claim)
		completionRequirements(r, &p, at)
		out, err := Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}})
		if err != nil || out.Report.Status != "ready-offline" {
			t.Fatalf("valid %s completion blocked: %+v %v", claim, out, err)
		}
	}
	r, p, at := completionFixture("delivered")
	completionRequirements(r, &p, at)
	for i := range p.Requirements {
		if p.Requirements[i].ID == "publication" {
			p.Requirements = append(p.Requirements[:i], p.Requirements[i+1:]...)
			break
		}
	}
	if _, err := Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}}); err == nil {
		t.Fatal("delivered route omitted publication gate")
	}
}

func TestCompletionAdmissionCannotOmitPassedMilestones(t *testing.T) {
	for _, claim := range []string{"local-handoff", "delivered"} {
		r, p, at := completionFixture(claim)
		completionRequirements(r, &p, at)
		out, e := Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}})
		if e != nil {
			t.Fatal(e)
		}
		if !requiredGatesPassedAt(r, out.Report, at) {
			t.Fatal("complete fixture should pass structural gates")
		}
		for _, id := range CompletionGateIDs(r) {
			report := out.Report
			report.Gates = append([]readiness.GateResult(nil), report.Gates...)
			for i, g := range report.Gates {
				if g.ID == id {
					report.Gates = append(report.Gates[:i], report.Gates[i+1:]...)
					break
				}
			}
			if requiredGatesPassedAt(r, report, at) {
				t.Fatalf("%s omitted %s at admission", claim, id)
			}
		}
	}
}

func TestCompletionRejectsOmittedStaleRevokedWrongScopeAndDowngrade(t *testing.T) {
	r, p, at := completionFixture("local-handoff")
	completionRequirements(r, &p, at)
	for i := range p.Requirements {
		if p.Requirements[i].ID == "acceptance" {
			p.Requirements = append(p.Requirements[:i], p.Requirements[i+1:]...)
			break
		}
	}
	if _, err := Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}}); err == nil {
		t.Fatal("acceptance omission accepted")
	}
	r, p, at = completionFixture("local-handoff")
	completionRequirements(r, &p, at)
	for i := range p.Requirements {
		if p.Requirements[i].ID == "local-handoff" {
			p.Requirements[i].Bindings[0].Revoked = true
			break
		}
	}
	out, err := Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}})
	if err != nil || out.Report.Status != "blocked" {
		t.Fatal("revoked evidence passed", out, err)
	}
	r, p, at = completionFixture("local-handoff")
	completionRequirements(r, &p, at)
	for i := range p.Requirements {
		if p.Requirements[i].ID == "acceptance" {
			p.Requirements[i].Bindings[0].ObservedAt = at.Add(-10 * time.Minute).Format(time.RFC3339Nano)
			break
		}
	}
	out, err = Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}})
	if err != nil || out.Report.Status != "blocked" {
		t.Fatal("stale evidence passed", out, err)
	}
	r, p, at = completionFixture("local-handoff")
	completionRequirements(r, &p, at)
	for i := range p.Requirements {
		if p.Requirements[i].ID == "provider-operation" {
			p.Requirements[i].Scope["operation"] = "other-operation"
			break
		}
	}
	if _, err = Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}}); err == nil {
		t.Fatal("wrong operation scope accepted")
	}
	r, p, at = completionFixture("local-handoff")
	completionRequirements(r, &p, at)
	for i := range p.Requirements {
		if p.Requirements[i].ID == "local-handoff" {
			p.Requirements[i].Scope["routeSha256"] = strings.Repeat("c", 64)
			break
		}
	}
	out, err = Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}})
	if err == nil && out.Report.Status != "blocked" {
		t.Fatal("wrong route scope passed", out, err)
	}
	// A required gate for a different provider is not part of this route's
	// readiness decision.
	r, p, at = completionFixture("local-handoff")
	completionRequirements(r, &p, at)
	p.Requirements = append(p.Requirements, readiness.GateRequirement{ID: "provider-operation:linear", Required: true, Validator: "native", Applicability: "applicable", Scope: map[string]string{"provider": "linear"}})
	out, err = Assess(r, p, at, policyAuthority{}, map[string]Validator{"native": gateValidator{}})
	if err != nil || out.Report.Status != "ready-offline" {
		t.Fatal("foreign provider blocked route", out, err)
	}
	// An enrolled delivered milestone cannot be downgraded to local handoff.
	r, _, _ = completionFixture("delivered")
	r.Completion.Claim = "local-handoff"
	r.Completion.PublicationOperation = ""
	if validCompletionRoute(r) {
		t.Fatal("delivered-to-local claim downgrade accepted")
	}
	r, p, at = completionFixture("delivered")
	r.Completion.Claim = "local-handoff"
	r.Completion.PublicationOperation = ""
	p.RouteSHA256 = usage.Digest(r)
	if _, err = Assess(r, p, at, policyAuthority{}, map[string]Validator{}); err == nil {
		t.Fatal("policy claim downgrade accepted")
	}
}
