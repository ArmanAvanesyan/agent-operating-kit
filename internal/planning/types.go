package planning

import "github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"

// Report separates malformed contract corpora from plans blocked on current
// evidence. It never dispatches work or claims execution success.
type Report struct {
	Status        string                 `json:"status"`
	Diagnostics   []contracts.Diagnostic `json:"diagnostics"`
	EligibleSteps []string               `json:"eligibleSteps,omitempty"`
	GoalStatus    string                 `json:"goalStatus,omitempty"`
	ContextSHA256 string                 `json:"contextSHA256,omitempty"`
	PlanHashes    map[string]string      `json:"planHashes,omitempty"`
}

func (r *Report) add(code, source, field, severity, message string) {
	r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{Code: code, Source: source, Field: field, Severity: severity, Message: message})
}
