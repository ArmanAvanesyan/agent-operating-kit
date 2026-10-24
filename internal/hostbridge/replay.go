package hostbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"path/filepath"
)

func Replay(path string) (Bundle, error) {
	var m ReplayManifest
	raw, e := ReadFile(path)
	if e != nil {
		return Bundle{}, e
	}
	if e = Decode(raw, "replay", &m); e != nil {
		return Bundle{}, e
	}
	if m.Prepared.Digest != preparedDigest(m.Prepared) {
		return Bundle{}, fmt.Errorf("replay prepared hash mismatch")
	}
	frames := [][]byte{}
	total := 0
	for _, a := range m.Frames {
		b, e := assetBytes(filepath.Dir(path), a)
		if e != nil {
			return Bundle{}, e
		}
		total += len(b)
		if total > MaxCaptureBytes {
			return Bundle{}, fmt.Errorf("replay exceeds capture limit")
		}
		frames = append(frames, b)
	}
	var t Transcript
	if m.Prepared.Host == "codex" {
		t, e = ParseCodexFrames(m.Prepared, m.Previous, frames)
	} else if m.Prepared.Host == "claude-code" {
		t, e = ParseClaudeFrames(m.Prepared, m.Previous, frames)
	} else {
		return Bundle{}, fmt.Errorf("unsupported replay host")
	}
	if e != nil {
		return Bundle{}, e
	}
	p := m.Prepared
	r := Receipt{Version: Version, AttemptID: "replay-" + Hash(raw)[:24], PreparedSHA256: p.Digest, PolicySHA256: p.PolicySHA256, Host: p.Host, HostVersion: p.HostVersion, Workspace: p.Workspace, Model: t.Model, ConfigurationID: p.ConfigurationID, SessionID: t.SessionID, ThreadID: t.ThreadID, TurnID: t.TurnID, HostOutcome: t.HostOutcome, Report: BaseReport()}
	r.Report.LiveGate = "fixture-only"
	if t.HostOutcome == "completed" && t.Model == p.Model && validateOutput(p, t.Output) == nil {
		r.OutputValidated = true
	} else {
		r.Report = blocked("replay-unqualified", "transcript", "fixture cannot establish successful exact-model output")
	}
	bundle := finishAttempt(Bundle{Files: map[string][]byte{}}, p, r, t)
	for _, a := range p.Sources {
		data, err := assetBytes(filepath.Dir(path), a)
		if err != nil {
			return Bundle{}, err
		}
		bundle.Files[a.Path] = data
	}
	return bundle, nil
}
func ValidateBundle(root string) (Report, error) {
	if _, e := ReadAsset(root, "receipt.json"); e != nil {
		p, e := LoadPrepared(root)
		if e != nil {
			return Report{}, e
		}
		if e = validateOutputSchema(p.OutputSchema); e != nil {
			return Report{}, e
		}
		return BaseReport(), nil
	}
	r, e := LoadReceipt(root)
	if e != nil {
		return Report{}, e
	}
	p, e := LoadPreparedReceipt(root)
	if e != nil {
		return Report{}, e
	}
	if r.PreparedSHA256 != p.Digest {
		return Report{}, fmt.Errorf("attempt prepared identity mismatch")
	}
	return r.Report, nil
}
func LoadPreparedReceipt(root string) (Prepared, error) {
	var p Prepared
	b, e := ReadAsset(root, "prepared.json")
	if e != nil {
		return p, e
	}
	if e = Decode(b, "prepared", &p); e != nil {
		return p, e
	}
	if p.Digest != preparedDigest(p) {
		return p, fmt.Errorf("prepared digest changed")
	}
	if len(p.Sources) > 0 {
		return LoadPrepared(root)
	}
	return p, nil
}
func validateOutputSchema(raw []byte) error {
	if !json.Valid(raw) {
		return fmt.Errorf("invalid output schema")
	}
	return nil
}
func PlanTasks(r Receipt, planPath string) (TaskProjection, error) {
	raw, e := ReadFile(planPath)
	p := TaskProjection{Version: Version, AttemptSHA256: r.Digest, PlanSHA256: Hash(raw), Steps: []TaskStep{}, Report: BaseReport()}
	if e != nil {
		return p, e
	}
	res, _, e := contracts.Parse(raw)
	if e != nil {
		return p, e
	}
	resource, ok := res.(contracts.Resource)
	if !ok || resource.Kind != "PlanDefinition" {
		return p, fmt.Errorf("validated PlanDefinition required")
	}
	var doc struct {
		Steps []struct {
			ID          string   `json:"id"`
			Description string   `json:"description"`
			DependsOn   []string `json:"dependsOn"`
		} `json:"steps"`
	}
	if e = json.Unmarshal(raw, &doc); e != nil {
		return p, e
	}
	seen := map[string]bool{}
	for _, s := range doc.Steps {
		if s.ID == "" || seen[s.ID] {
			return p, fmt.Errorf("duplicate or empty plan step")
		}
		seen[s.ID] = true
		p.Steps = append(p.Steps, TaskStep{ID: s.ID, Subject: s.Description, Description: s.Description, Dependencies: s.DependsOn, AOKState: "pending"})
	}
	for _, s := range p.Steps {
		for _, dep := range s.Dependencies {
			if !seen[dep] {
				return p, fmt.Errorf("unresolved plan dependency")
			}
		}
	}
	p.Report.Diagnostics = append(p.Report.Diagnostics, Diagnostic{"draft-only", "nativeTasks", "native task IDs and availability require observed host tool calls; QA/review state remains AOK-owned"})
	return p, nil
}

// DenyBroker handles known approval requests conservatively. Permission grants
// and interactive approval UI are deliberately not synthesized from model text.
type DenyBroker struct{}

func (DenyBroker) Decide(_ context.Context, a Approval) (json.RawMessage, error) {
	switch a.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		return json.RawMessage(`{"decision":"decline"}`), nil
	case "item/permissions/requestApproval":
		return json.RawMessage(`{"permissions":{}}`), nil
	}
	return nil, fmt.Errorf("unknown approval request")
}
