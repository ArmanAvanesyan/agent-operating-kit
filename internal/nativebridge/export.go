// Package nativebridge projects a validated AOK assignment into native candidate
// fields. It preserves source hashes and never promotes syntax to runtime safety.
package nativebridge

import (
	"bytes"
	"encoding/json"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentresolve"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeadapter"
)

func Export(engine *nativeadapter.Engine, target, scope, assignment, root string, extra []nativeadapter.Control) ([]nativeadapter.File, nativeadapter.Bundle, nativeadapter.Report) {
	r := nativeadapter.Report{Operation: "export", Target: target, Scope: scope, Status: "blocked", RuntimeEnforcement: "unverified", HostLoading: nativeadapter.HostLoadingEvidence{Status: "not-run"}, Diagnostics: []contracts.Diagnostic{}}
	fail := func(code, field, message string) {
		r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{Code: code, Field: field, Source: assignment, Severity: "error", Message: message})
	}
	resolved, instructions := agentresolve.ResolveWithInstructions(assignment, root)
	if resolved.Snapshot == nil || resolved.Status == "invalid" {
		fail("native.canonical.invalid", "assignment", "canonical assignment failed validation or resolution")
		r.Status = "invalid"
		return nil, nativeadapter.Bundle{}, r
	}
	s := resolved.Snapshot
	if s.Profile.Client.Adapter != target {
		fail("native.canonical.target", "executionProfile.client.adapter", "explicit target differs from selected profile")
		return nil, nativeadapter.Bundle{}, r
	}
	if s.Agent.Instructions.Parameters != nil && len(*s.Agent.Instructions.Parameters) > 0 {
		fail("native.canonical.template", "agent.instructions.parameters", "native export requires literal instruction sources; template expansion is not implemented")
		return nil, nativeadapter.Bundle{}, r
	}
	descriptor, ok := engine.Describe(target)
	if !ok {
		fail("native.target.unregistered", "target", "target has no registered native adapter")
		return nil, nativeadapter.Bundle{}, r
	}
	fields := map[string]json.RawMessage{}
	set := func(path string, value any) { fields[path], _ = json.Marshal(value) }
	set("agent.metadata.id", s.Agent.Metadata.ID)
	set("agent.identity.purpose", s.Agent.Identity.Purpose)
	set("agent.instructions.sources", []contracts.InstructionSource{{Path: "instructions.md"}})
	set("executionProfile.model.selector", s.Profile.Model.Selector)
	controls := append([]nativeadapter.Control{}, extra...)
	required := func(path string) { controls = append(controls, nativeadapter.Control{Path: path, Required: true}) }
	for _, path := range []string{"agent.metadata.id", "agent.identity.purpose", "agent.instructions.sources", "executionProfile.model.selector"} {
		required(path)
	}
	if s.Profile.Model.Reasoning.Mode != "provider-default" {
		set("executionProfile.model.reasoning", s.Profile.Model.Reasoning)
		required("executionProfile.model.reasoning")
	}
	if g := s.Profile.Model.Generation; g != nil {
		if g.Temperature != nil {
			set("executionProfile.model.generation.temperature", g.Temperature)
			required("executionProfile.model.generation.temperature")
		}
		if g.TopP != nil {
			set("executionProfile.model.generation.topP", g.TopP)
			required("executionProfile.model.generation.topP")
		}
		if g.MaxOutputTokens != nil {
			set("executionProfile.model.generation.maxOutputTokens", g.MaxOutputTokens)
			required("executionProfile.model.generation.maxOutputTokens")
		}
	}
	for _, path := range []string{"effectivePermissions.filesystem", "effectivePermissions.process", "effectivePermissions.network", "effectivePermissions.actions", "agent.identity.boundaries", "agent.roles", "agent.verification", "agent.outputs", "agent.environment", "agent.invocationContract", "agent.contextPolicy", "agent.lifecyclePolicy", "agent.evaluationContract", "agent.migrationContract", "agent.delegation"} {
		required(path)
	}
	for _, group := range []struct {
		name string
		refs []contracts.CapabilityReference
	}{{"tools", s.Agent.Capabilities.Tools}, {"skills", s.Agent.Capabilities.Skills}, {"mcps", s.Agent.Capabilities.MCPs}, {"plugins", s.Agent.Capabilities.Plugins}} {
		if len(group.refs) == 0 {
			continue
		}
		req := false
		for _, ref := range group.refs {
			req = req || ref.Required
		}
		controls = append(controls, nativeadapter.Control{Path: "agent.capabilities." + group.name, Required: req})
	}
	if s.Profile.Model.Provider != nil {
		required("executionProfile.model.provider")
	}
	if s.Profile.Limits != nil {
		required("executionProfile.limits")
	}
	if s.Profile.Extensions != nil && len(*s.Profile.Extensions) > 0 {
		required("executionProfile.extensions")
	}
	if s.Profile.Client.VersionConstraint != nil {
		required("executionProfile.client.versionConstraint")
	}
	// Keep only explicitly declared native projections. Missing required fields
	// remain controls and become blocked compatibility diagnostics, never guesses.
	supported := map[string]bool{}
	for _, cap := range descriptor.Fields {
		supported[cap.NeutralPath] = cap.Support == "native"
	}
	for path := range fields {
		if !supported[path] {
			delete(fields, path)
		}
	}
	chunks := [][]byte{}
	for _, instruction := range instructions {
		chunks = append(chunks, instruction.Data)
	}
	doc := nativeadapter.Document{Fields: fields, Unknown: map[string]json.RawMessage{}, Assets: map[string][]byte{"instructions.md": bytes.Join(chunks, []byte("\n\n"))}}
	provenance := &nativeadapter.CanonicalProvenance{SnapshotDigest: s.Digest, Agent: s.Assignment.Agent, Profile: s.Assignment.ExecutionProfile, Assignment: contracts.Reference{Kind: "AgentAssignment", ID: s.Assignment.Metadata.ID, Version: s.Assignment.Metadata.Version}}
	for _, source := range s.Sources {
		provenance.Sources = append(provenance.Sources, nativeadapter.CanonicalSource{Path: source.Path, SHA256: source.SHA256})
	}
	files, bundle, result := engine.Render(target, scope, doc, controls, provenance)
	result.Diagnostics = append(result.Diagnostics, contracts.Diagnostic{Code: "native.canonical.readiness", Source: assignment, Severity: "warning", Message: "native candidate preserves canonical selection hashes; runtime readiness and full policy enforcement remain unverified"})
	if result.Status == "valid" {
		result.Status = "blocked"
	}
	return files, bundle, result
}
