// Package v2 binds existing readiness evidence to a compiled host driver.
// It never upgrades an unqualified v1 route or grants authority by itself.
package v2

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
	legacy "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness"
)

const Version = "aok-readiness/v2"

type Route struct {
	Version                  string               `json:"version"`
	Legacy                   legacy.RouteManifest `json:"legacyRoute"`
	DriverSHA256             string               `json:"driverSha256"`
	Protocol                 string               `json:"protocol"`
	PreparedSHA256           string               `json:"preparedSha256"`
	PolicySHA256             string               `json:"policySha256"`
	LaunchSHA256             string               `json:"launchSha256"`
	InputSHA256              string               `json:"inputSha256"`
	MethodologyRequired      bool                 `json:"methodologyRequired"`
	MethodologyBindingSHA256 string               `json:"methodologyBindingSha256,omitempty"`
	// Completion is opt-in so existing v2 route records and their digests retain
	// their historical interpretation. It binds delivery-specific gates to one
	// exact operation and milestone.
	Completion *CompletionRoute `json:"completion,omitempty"`
}

type CompletionRoute struct {
	Claim                string `json:"claim"` // local-handoff or delivered
	Milestone            string `json:"milestone"`
	PublicationOperation string `json:"publicationOperation,omitempty"`
}
type Admission struct {
	Version              string `json:"version"`
	RouteSHA256          string `json:"routeSha256"`
	DriverSHA256         string `json:"driverSha256"`
	Status               string `json:"status"`
	Reason               string `json:"reason"`
	AuthorizationGranted bool   `json:"authorizationGranted"`
}

type Authority interface {
	VerifyReadinessRoute(Route, legacy.Report, json.RawMessage) error
}
type AuthorityFunc func(Route, legacy.Report, json.RawMessage) error

func (f AuthorityFunc) VerifyReadinessRoute(r Route, report legacy.Report, proof json.RawMessage) error {
	return f(r, report, proof)
}

// Evaluate requires an independently evaluated v1 readiness report and exact
// driver binding. Only existing Codex/Claude P7 transport paths can be eligible.
func Evaluate(r Route, report legacy.Report) (Admission, error) {
	at, _ := time.Parse(time.RFC3339Nano, report.AsOf)
	return admitAt(r, report, nil, nil, at)
}

// Admit applies a protected readiness decision. The ordinary offline v1
// report cannot authorize execution; an external verifier must bind proof to
// the route, report, and compiled driver identity.
func Admit(r Route, report legacy.Report, proof json.RawMessage, authority Authority) (Admission, error) {
	return admitAt(r, report, proof, authority, time.Now().UTC())
}
func admitAt(r Route, report legacy.Report, proof json.RawMessage, authority Authority, now time.Time) (Admission, error) {
	a := Admission{Version: Version, Status: "blocked", AuthorizationGranted: false}
	if r.Version != Version || r.Legacy.Host == "" || !isHash(r.DriverSHA256) || r.Protocol == "" || !isHash(r.PreparedSHA256) || !isHash(r.PolicySHA256) || !isHash(r.LaunchSHA256) || !isHash(r.InputSHA256) || (r.MethodologyRequired && !isHash(r.MethodologyBindingSHA256)) || (!r.MethodologyRequired && r.MethodologyBindingSHA256 != "") || !validCompletionRoute(r) {
		return a, errors.New("invalid v2 route binding")
	}
	baseHash, err := legacy.Digest(r.Legacy)
	if err != nil {
		return a, err
	}
	b, _ := json.Marshal(r)
	sum := sha256.Sum256(b)
	a.RouteSHA256 = hex.EncodeToString(sum[:])
	a.DriverSHA256 = r.DriverSHA256
	d, ok := hostdriver.Lookup(r.Legacy.Host)
	if !ok || d.Support != hostdriver.Supported {
		a.Reason = "host driver is not registered as supported"
		return a, nil
	}
	if d.Digest != r.DriverSHA256 || d.Protocol != r.Protocol {
		a.Reason = "compiled driver identity or protocol changed"
		return a, nil
	}
	if d.Execution != "p7-v1" && d.Execution != "p7-v2-controlled" {
		a.Reason = "registered protocol has no P7 controller transport integration"
		return a, nil
	}
	if d.Execution == "p7-v1" {
		if err := legacy.ValidateManifest(r.Legacy); err != nil {
			return a, errors.New("invalid legacy readiness route")
		}
	} else if r.Legacy.Version == "" || r.Legacy.ID == "" || r.Legacy.HostVersion == "" || r.Legacy.HostSHA256 == "" || r.Legacy.Transport != d.Protocol || r.Legacy.Account == "" || r.Legacy.AuthMode == "" || !isHash(r.Legacy.HostSHA256) || !isHash(r.Legacy.ConfigurationSHA256) || !isHash(r.Legacy.ProfileSHA256) || !isHash(r.Legacy.ControlPolicySHA256) {
		return a, errors.New("invalid legacy readiness route")
	}
	if report.ManifestSHA256 != baseHash || report.Status != "ready-offline" {
		a.Reason = "current readiness report is missing or blocked"
		return a, nil
	}
	if !requiredGatesPassedAt(r, report, now) {
		a.Reason = "mandatory universal readiness gates are absent, stale or not passed"
		return a, nil
	}
	if authority == nil || !json.Valid(proof) {
		a.Reason = "external protected readiness authority is unavailable"
		return a, nil
	}
	if err := authority.VerifyReadinessRoute(r, report, proof); err != nil {
		a.Reason = "protected readiness authority rejected exact route"
		return a, nil
	}
	a.Status = "eligible"
	a.Reason = "existing P7 route may be considered after exact-scope proof verification"
	return a, nil
}

func validCompletionRoute(r Route) bool {
	c := r.Completion
	if c == nil {
		return r.Legacy.DeliveryMilestone == ""
	}
	if c.Milestone == "" || r.Legacy.Provider == "" || r.Legacy.Operation == "" {
		return false
	}
	switch c.Claim {
	case "local-handoff":
		return c.PublicationOperation == "" && r.Legacy.DeliveryMilestone != "publication" && r.Legacy.DeliveryMilestone != "delivered"
	case "delivered":
		return c.PublicationOperation != "" && c.PublicationOperation == r.Legacy.Operation
	default:
		return false
	}
}

func requiredGatesPassedAt(route Route, report legacy.Report, now time.Time) bool {
	asOf, err := time.Parse(time.RFC3339Nano, report.AsOf)
	if err != nil || now.IsZero() || asOf.After(now) || now.Sub(asOf) > 5*time.Minute {
		return false
	}
	validUntil, err := time.Parse(time.RFC3339Nano, report.EarliestValidUntil)
	if err != nil || !now.Before(validUntil) {
		return false
	}
	if !isHash(report.PolicySHA256) {
		return false
	}
	gates := map[string]legacy.GateResult{}
	for _, gate := range report.Gates {
		if _, exists := gates[gate.ID]; exists || gate.ID == "" {
			return false
		}
		gates[gate.ID] = gate
	}
	for _, id := range append(UniversalGateIDs(route), CompletionGateIDs(route)...) {
		gate, ok := gates[id]
		if !ok || !gate.Required || gate.Status != "passed" || len(gate.EvidenceSHA256) == 0 {
			return false
		}
		for _, evidence := range gate.EvidenceSHA256 {
			if !isHash(evidence) {
				return false
			}
		}
	}
	return true
}

func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func DecodeStrict(raw []byte, dst any) error {
	if err := contracts.ValidateJSON(raw); err != nil {
		return fmt.Errorf("invalid strict JSON: %w", err)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON")
		}
		return err
	}
	return nil
}
