package toolisolation

import (
	"runtime"
	"time"
)

// Inspection is an offline structural report. In particular it must never
// suggest that Codex's built-in tools use the separate worker implemented here.
type Inspection struct {
	Version                 string   `json:"version"`
	AsOf                    string   `json:"asOf"`
	PolicyValid             bool     `json:"policyValid"`
	Status                  string   `json:"status"`
	ComponentImplementation string   `json:"componentImplementation"`
	Platform                string   `json:"platform"`
	OSConformance           string   `json:"osConformance"`
	EnforcementBackend      string   `json:"enforcementBackend"`
	NativeCodexTransport    string   `json:"nativeCodexTransport"`
	Blockers                []string `json:"blockers"`
	AuthorizationGranted    bool     `json:"authorizationGranted"`
	ExecutionReady          bool     `json:"executionReady"`
	ProductionEligible      bool     `json:"productionEligible"`
}

func Inspect(p Policy, at time.Time) Inspection {
	r := Inspection{Version: Version, AsOf: at.UTC().Format(time.RFC3339Nano), PolicyValid: ValidatePolicy(p) == nil, Status: "blocked", OSConformance: "not-established", ComponentImplementation: "separate-tool-worker", Platform: runtime.GOOS + "-" + runtime.GOARCH, EnforcementBackend: "unsupported", NativeCodexTransport: "unsupported-pinned-0.162.1-tool-delegation", Blockers: []string{"Pinned Codex 0.162.1 has no verified delegated built-in tool transport; this component is not connected to its native tools.", "Final exact-root OS controls, parent native authentication boundary, issuer trust and fresh signed proofs require independent enrollment and live qualification.", "Runtime admission, capacity, independent verification/acceptance, provider conformance and production gates remain separate."}}
	if runtime.GOOS == "darwin" {
		r.EnforcementBackend = "darwin-seatbelt"
	}
	if !r.PolicyValid {
		r.Blockers = append(r.Blockers, "Tool isolation policy is invalid or incomplete.")
	}
	if at.IsZero() {
		r.PolicyValid = false
		r.Blockers = append(r.Blockers, "An inspection instant is required.")
	}
	return r
}
