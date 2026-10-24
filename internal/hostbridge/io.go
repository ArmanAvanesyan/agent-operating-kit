package hostbridge

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentresolve"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"path/filepath"
	"strings"
)

func Hash(b []byte) string                     { return usage.Hash(b) }
func Digest(v any) string                      { return usage.Digest(v) }
func ReadFile(p string) ([]byte, error)        { return tracker.ReadFile(p) }
func ReadAsset(root, p string) ([]byte, error) { return tracker.ReadAsset(root, p) }
func Decode(b []byte, kind string, v any) error {
	if e := ValidateJSON(b, kind); e != nil {
		return e
	}
	return usage.Decode(b, v)
}
func BaseReport() Report {
	return Report{Version: Version, Status: "valid", Diagnostics: []Diagnostic{}, LiveGate: "not-qualified", RuntimeGate: "pending-p7-p8"}
}
func blocked(code, field, msg string) Report {
	r := BaseReport()
	r.Status = "blocked"
	r.Diagnostics = append(r.Diagnostics, Diagnostic{code, field, msg})
	return r
}
func encoded(v any) []byte             { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }
func preparedDigest(p Prepared) string { p.Digest = ""; return Digest(p) }
func receiptDigest(r Receipt) string   { r.Digest = ""; return Digest(r) }
func LoadPrepared(dir string) (Prepared, error) {
	var p Prepared
	b, e := ReadAsset(dir, "prepared.json")
	if e == nil {
		e = Decode(b, "prepared", &p)
	}
	if e == nil && p.Digest != preparedDigest(p) {
		e = fmt.Errorf("prepared digest changed")
	}
	if e != nil {
		return p, e
	}
	for _, a := range p.Sources {
		b, e := ReadAsset(dir, a.Path)
		if e != nil || Hash(b) != a.SHA256 {
			return p, fmt.Errorf("captured source changed")
		}
	}

	var snapshot agentresolve.Snapshot
	if e = json.Unmarshal(p.Snapshot, &snapshot); e != nil {
		return p, fmt.Errorf("invalid captured resolution")
	}
	corpus := filepath.Join(dir, "sources")
	assignment := filepath.Join(corpus, filepath.FromSlash(snapshot.AssignmentPath))
	resolved, files := agentresolve.ResolveWithInstructions(assignment, corpus)
	if resolved.Snapshot == nil || resolved.Snapshot.Digest != p.SnapshotSHA256 || resolved.Snapshot.Profile.Model.Selector != p.Model {
		return p, fmt.Errorf("prepared resolution differs from captured corpus")
	}
	if Digest(snapshot) != Digest(*resolved.Snapshot) {
		return p, fmt.Errorf("captured snapshot content differs from resolved corpus")
	}
	assignmentBytes, err := ReadAsset(corpus, snapshot.AssignmentPath)
	if err != nil || Hash(assignmentBytes) != p.AssignmentSHA256 {
		return p, fmt.Errorf("assignment digest mismatch")
	}
	profileMatched, schemaMatched := false, false
	for _, src := range resolved.Snapshot.Sources {
		raw, err := ReadAsset(corpus, src.Path)
		if err != nil {
			return p, err
		}
		var header struct {
			Kind     string `json:"kind"`
			Metadata struct {
				ID string `json:"id"`
			} `json:"metadata"`
		}
		_ = json.Unmarshal(raw, &header)
		if header.Kind == "ExecutionProfile" && header.Metadata.ID == snapshot.Profile.Metadata.ID {
			profileMatched = Hash(raw) == p.ProfileSHA256
		}
		if header.Kind == "OutputContract" && header.Metadata.ID == snapshot.Output.Metadata.ID {
			schema, err := ReadAsset(corpus, filepath.ToSlash(filepath.Join(filepath.Dir(src.Path), snapshot.Output.ResultSchema)))
			schemaMatched = err == nil && semanticJSON(schema) == semanticJSON(p.OutputSchema)
		}
	}
	if !profileMatched || !schemaMatched {
		return p, fmt.Errorf("profile or output schema differs from captured corpus")
	}
	instructions := ""
	for _, file := range files {
		instructions += "\n" + string(file.Data)
	}
	if strings.TrimSpace(instructions) != p.Instructions {
		return p, fmt.Errorf("prepared instruction bytes differ from captured sources")
	}
	var input struct {
		Task   json.RawMessage `json:"task"`
		Inputs json.RawMessage `json:"inputs"`
	}
	packet, packetErr := ReadAsset(corpus, snapshot.Assignment.TaskPacket)
	if packetErr != nil || Hash(packet) != snapshot.TaskPacketSHA256 || json.Unmarshal(p.Input, &input) != nil || semanticJSON(input.Task) != semanticJSON(packet) {
		return p, fmt.Errorf("prepared task input differs from captured packet")
	}
	expectedInputs, _ := json.Marshal(snapshot.Inputs)
	if semanticJSON(input.Inputs) != semanticJSON(expectedInputs) {
		return p, fmt.Errorf("prepared input parameters differ from assignment")
	}
	var bindingRef *contracts.Reference
	for _, asset := range p.Sources {
		if asset.Path == "bindings/work-item.json" {
			raw, err := ReadAsset(dir, asset.Path)
			if err != nil {
				return p, err
			}
			var binding contracts.WorkItemBinding
			if _, _, err = contracts.Parse(raw); err != nil {
				return p, err
			}
			if err = json.Unmarshal(raw, &binding); err != nil || binding.Packet.SHA256 != snapshot.TaskPacketSHA256 {
				return p, fmt.Errorf("captured work-item packet mismatch")
			}
			bindingRef = &contracts.Reference{Kind: "WorkItemBinding", ID: binding.Metadata.ID, Version: binding.Metadata.Version}
		}
	}
	if Digest(bindingRef) != Digest(p.WorkItemRef) {
		return p, fmt.Errorf("work-item reference differs from captured binding")
	}
	planSteps := []TaskStep{}
	for _, asset := range p.Sources {
		if asset.Path != "bindings/plan.json" {
			continue
		}
		raw, err := ReadAsset(dir, asset.Path)
		if err != nil {
			return p, err
		}
		var plan struct {
			Steps []struct {
				ID          string   `json:"id"`
				Description string   `json:"description"`
				DependsOn   []string `json:"dependsOn"`
			} `json:"steps"`
		}
		if err = json.Unmarshal(raw, &plan); err != nil {
			return p, err
		}
		for _, step := range plan.Steps {
			planSteps = append(planSteps, TaskStep{ID: step.ID, Subject: step.Description, Description: step.Description, Dependencies: step.DependsOn, AOKState: "pending"})
		}
	}
	if (len(p.NativeTasks) != 0 || len(planSteps) != 0) && Digest(p.NativeTasks) != Digest(planSteps) {
		return p, fmt.Errorf("prepared task steps differ from captured plan")
	}
	return p, nil
}
func semanticJSON(raw []byte) string {
	var v any
	if usage.Decode(raw, &v) != nil {
		return "invalid"
	}
	b, _ := json.Marshal(v)
	return string(b)
}
func LoadReceipt(dir string) (Receipt, error) {
	var r Receipt
	b, e := ReadAsset(dir, "receipt.json")
	if e == nil {
		e = Decode(b, "receipt", &r)
	}
	if e == nil && r.Digest != receiptDigest(r) {
		e = fmt.Errorf("attempt digest changed")
	}
	if e != nil {
		return r, e
	}
	t, e := ReadAsset(dir, "transcript.json")
	if e != nil || Hash(t) != r.TranscriptSHA256 {
		return r, fmt.Errorf("attempt transcript changed")
	}
	var tr Transcript
	if e = Decode(t, "transcript", &tr); e != nil {
		return r, e
	}
	return r, nil
}
func assetBytes(root string, a Asset) ([]byte, error) {
	b, e := ReadAsset(root, a.Path)
	if e != nil || Hash(b) != a.SHA256 {
		return nil, fmt.Errorf("hash-bound asset unavailable or changed: %s", filepath.Base(a.Path))
	}
	return b, nil
}
