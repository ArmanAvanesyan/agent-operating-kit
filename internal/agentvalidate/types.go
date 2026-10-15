package agentvalidate

import "github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"

// Report describes corpus validation. A valid corpus has passed static checks;
// it is not a readiness or authorization decision.
type Report struct {
	Status      string                 `json:"status"`
	Diagnostics []contracts.Diagnostic `json:"diagnostics"`
}

// Corpus is a loaded snapshot of one explicit directory. Its exported members
// are mutable so callers should treat it as read-only after Load returns.
type Corpus struct {
	Root      string
	Resources []LoadedResource
	Files     map[string]struct{}
}

// LoadedResource pairs an accepted document with its root-relative source.
type LoadedResource struct {
	Resource contracts.Resource
	Path     string
	Object   map[string]any
}

func (r *Report) add(code, source, field, severity, message string) {
	r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{Code: code, Source: source, Field: field, Severity: severity, Message: message})
}

func (r *Report) finish() {
	if r.Diagnostics == nil {
		r.Diagnostics = []contracts.Diagnostic{}
	}
	r.Status = "valid"
	for _, d := range r.Diagnostics {
		if d.Severity == "error" {
			r.Status = "invalid"
			return
		}
	}
}
