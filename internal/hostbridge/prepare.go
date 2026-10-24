package hostbridge

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentresolve"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"path/filepath"
	"strings"
)

func Prepare(assignment, root string, policy Policy) (Bundle, error) {
	b := Bundle{Files: map[string][]byte{}}
	if e := ValidatePolicy(policy); e != nil {
		return b, e
	}
	if root == "" {
		root = filepath.Dir(assignment)
	}
	resolved, instructions := agentresolve.ResolveWithInstructions(assignment, root)
	if resolved.Snapshot == nil || resolved.Status == "invalid" {
		return b, fmt.Errorf("assignment resolution invalid")
	}
	s := resolved.Snapshot
	if s.Profile.Client.Adapter != policy.Host || s.Profile.Model.Selector == "" || !contains(policy.Models, s.Profile.Model.Selector) {
		return b, fmt.Errorf("assignment host/model is not operator-approved")
	}
	for _, refs := range [][]contracts.CapabilityReference{s.Agent.Capabilities.Tools, s.Agent.Capabilities.Skills, s.Agent.Capabilities.MCPs, s.Agent.Capabilities.Plugins} {
		for _, ref := range refs {
			if ref.Required {
				return b, fmt.Errorf("required capability %s has no verified native binding", ref.ID)
			}
		}
	}
	if Digest(s.Permissions) != Digest(policy.Permissions) {
		return b, fmt.Errorf("operator controls must bind exact resolved permissions")
	}
	if s.Profile.Limits != nil && (s.Profile.Limits.MaxCost != nil || s.Profile.Limits.MaxTurns != nil || s.Profile.Limits.MaxToolCalls != nil) {
		return b, fmt.Errorf("required execution limits lack verified transport enforcement")
	}
	if s.Profile.Model.Generation != nil {
		return b, fmt.Errorf("generation controls are not verified for this transport")
	}
	if s.Profile.Model.Reasoning.Mode != "provider-default" && s.Profile.Model.Reasoning.Mode != "effort" {
		return b, fmt.Errorf("unsupported required reasoning mode")
	}
	if s.Agent.Delegation.Enabled {
		return b, fmt.Errorf("child dispatch not enabled")
	}
	if s.Agent.Instructions.Parameters != nil && len(*s.Agent.Instructions.Parameters) > 0 {
		return b, fmt.Errorf("instruction template expansion unsupported")
	}
	p := Prepared{Version: Version, Host: policy.Host, HostVersion: policy.HostVersion, PolicySHA256: PolicyHash(policy), Model: s.Profile.Model.Selector, Workspace: policy.Workspace, Configuration: policy.Configuration, ConfigurationID: modelcatalog.ConfigurationID(policy.Configuration), SnapshotSHA256: s.Digest, Sources: []Asset{}, NativeTasks: []TaskStep{}}
	if policy.Version == VersionV2 {
		p.Version = VersionV2
		p.HostDriverSHA256 = policy.HostDriverSHA256
		p.HostProtocol = policy.HostProtocol
	}
	if p.Configuration.Host == nil || *p.Configuration.Host != p.Host || p.Configuration.HostVersion == nil || *p.Configuration.HostVersion != p.HostVersion || p.Configuration.ModelID == nil || *p.Configuration.ModelID != p.Model || p.Configuration.AuthMode == nil || *p.Configuration.AuthMode != policy.AuthMode {
		return b, fmt.Errorf("exact configuration identity must match approved host/version/model/authentication")
	}
	for _, src := range s.Sources {
		raw, e := ReadAsset(root, src.Path)
		if e != nil || Hash(raw) != src.SHA256 {
			return b, fmt.Errorf("assignment corpus changed during capture")
		}
		path := "sources/" + src.Path
		b.Files[path] = raw
		p.Sources = append(p.Sources, Asset{path, Hash(raw)})
		if filepath.Clean(filepath.Join(root, src.Path)) == filepath.Clean(assignment) {
			p.AssignmentSHA256 = Hash(raw)
		}
		var obj struct {
			Kind     string `json:"kind"`
			Metadata struct {
				ID string `json:"id"`
			} `json:"metadata"`
		}
		_ = json.Unmarshal(raw, &obj)
		if obj.Kind == "ExecutionProfile" && obj.Metadata.ID == s.Profile.Metadata.ID {
			p.ProfileSHA256 = Hash(raw)
		}
		if obj.Kind == "OutputContract" && obj.Metadata.ID == s.Output.Metadata.ID {
			schemaPath := filepath.ToSlash(filepath.Join(filepath.Dir(src.Path), s.Output.ResultSchema))
			schema, e := ReadAsset(root, schemaPath)
			if e != nil {
				return b, e
			}
			p.OutputSchema = schema
		}
	}
	if p.AssignmentSHA256 == "" {
		raw, e := ReadFile(assignment)
		if e != nil {
			return b, e
		}
		p.AssignmentSHA256 = Hash(raw)
	}
	for _, f := range instructions {
		p.Instructions += "\n" + string(f.Data)
	}
	p.Instructions = strings.TrimSpace(p.Instructions)
	p.Input, _ = json.Marshal(struct {
		TaskPacketSHA256 string `json:"taskPacketSha256"`
		Inputs           any    `json:"inputs"`
	}{s.TaskPacketSHA256, s.Inputs})
	// Include the actual packet as data, never as instruction authority.
	task, e := ReadAsset(root, s.Assignment.TaskPacket)
	if e != nil || Hash(task) != s.TaskPacketSHA256 {
		return b, fmt.Errorf("task packet changed during capture")
	}
	p.Input, _ = json.Marshal(struct {
		Task   json.RawMessage `json:"task"`
		Inputs any             `json:"inputs"`
	}{task, s.Inputs})
	if policy.WorkItemBinding != nil {
		raw, e := assetBytes(policy.root, *policy.WorkItemBinding)
		if e != nil {
			return b, e
		}
		res, _, e := contracts.Parse(raw)
		if e != nil {
			return b, e
		}
		resource, ok := res.(contracts.Resource)
		if !ok || resource.Kind != "WorkItemBinding" {
			return b, fmt.Errorf("WorkItemBinding required")
		}
		var binding contracts.WorkItemBinding
		if json.Unmarshal(raw, &binding) != nil || binding.Packet.SHA256 != s.TaskPacketSHA256 {
			return b, fmt.Errorf("work item packet mismatch")
		}
		p.WorkItemRef = &contracts.Reference{Kind: "WorkItemBinding", ID: binding.Metadata.ID, Version: binding.Metadata.Version}
		path := "bindings/work-item.json"
		b.Files[path] = raw
		p.Sources = append(p.Sources, Asset{path, Hash(raw)})
	}
	if policy.TaskPlan != nil {
		raw, e := assetBytes(policy.root, *policy.TaskPlan)
		if e != nil {
			return b, e
		}
		var plan struct {
			Steps []struct {
				ID          string   `json:"id"`
				Description string   `json:"description"`
				DependsOn   []string `json:"dependsOn"`
			} `json:"steps"`
		}
		if _, _, e = contracts.Parse(raw); e != nil {
			return b, e
		}
		if json.Unmarshal(raw, &plan) != nil {
			return b, fmt.Errorf("invalid task plan")
		}
		if !policy.AllowNativeTasks {
			return b, fmt.Errorf("native task projection not permitted")
		}
		for _, step := range plan.Steps {
			p.NativeTasks = append(p.NativeTasks, TaskStep{ID: step.ID, Subject: step.Description, Description: step.Description, Dependencies: step.DependsOn, AOKState: "pending"})
		}
		path := "bindings/plan.json"
		b.Files[path] = raw
		p.Sources = append(p.Sources, Asset{path, Hash(raw)})
	}
	if e := configurationFidelity(s.Profile, policy); e != nil {
		return b, e
	}
	if e := catalogCompatibility(s.Profile, p.ProfileSHA256, policy); e != nil {
		return b, e
	}
	p.Snapshot, _ = json.Marshal(s)
	p.CatalogSHA256, e = catalogDigest(policy)
	if e != nil {
		return b, e
	}
	if !json.Valid(p.OutputSchema) || p.ProfileSHA256 == "" || p.Instructions == "" {
		return b, fmt.Errorf("captured output schema/profile/instructions required")
	}
	p.Digest = preparedDigest(p)
	b.Prepared = &p
	b.Files["prepared.json"] = encoded(p)
	return b, nil
}
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
