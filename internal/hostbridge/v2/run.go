package v2

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
)

// Scope is the protected exact binding required by the v2 readiness and
// runtime admission layers. It is evidence for the P7 boundary, not a second
// execution authority.
type Scope struct {
	RunID               string `json:"runId"`
	Host                string `json:"host"`
	DescriptorSHA256    string `json:"descriptorSha256"`
	PreparedSHA256      string `json:"preparedSha256"`
	InputSHA256         string `json:"inputSha256"`
	PolicySHA256        string `json:"policySha256"`
	LaunchSHA256        string `json:"launchSha256"`
	ExecutableSHA256    string `json:"executableSha256"`
	Model               string `json:"model"`
	ProfileSHA256       string `json:"profileSha256"`
	ConfigurationSHA256 string `json:"configurationSha256"`
	AccountScope        string `json:"accountScope"`
	AuthMode            string `json:"authMode"`
	Workspace           string `json:"workspace"`
	HostVersion         string `json:"hostVersion"`
	ExpiresAt           string `json:"expiresAt"`
}
type QualificationVerifier interface {
	Verify(context.Context, Scope, json.RawMessage) error
}
type Guard interface {
	Check(context.Context, string) error
}
type RunRequest struct {
	RunID        string
	AttemptID    string
	Scope        Scope
	Proof        json.RawMessage
	Prepared     hostdriver.Prepared
	Launch       hostbridge.Launch
	VersionProbe hostdriver.Probe
	Input        []byte
}
type RunResult struct {
	Observation Observation
	Started     bool
	Cancelled   bool
}

// Run is intentionally disabled. All host processes must execute through
// internal/hostbridge.RunControlled and the existing P7 controller lifecycle.
// Keeping this explicit prevents downstream callers from reviving a parallel
// runner that bypasses claims, P3 holds, fences, acceptance and lifecycle hooks.
func Run(context.Context, RunRequest, hostbridge.Runner, QualificationVerifier, Guard) (RunResult, error) {
	return RunResult{}, errors.New("standalone v2 host execution is disabled; use the P7 hostbridge.RunControlled path")
}
