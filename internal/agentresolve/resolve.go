package agentresolve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentpolicy"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentvalidate"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

const (
	maxFiles = 512
	maxBytes = 64 << 20
	maxFile  = contracts.MaxDocumentBytes
)

// Resolve captures a bounded corpus, validates only that captured copy, and
// resolves the exact assignment pins. An empty corpusRoot means the assignment
// file's parent directory. No commands, probes, adapters, or network calls run.
func Resolve(assignmentPath, corpusRoot string) Report {
	report := Report{Diagnostics: []contracts.Diagnostic{}}
	capture, err := captureCorpus(assignmentPath, corpusRoot)
	if err != nil {
		report.add("resolve.capture", assignmentPath, "", "error", err.Error())
		report.finish("invalid")
		return report
	}
	defer os.RemoveAll(capture.tempRoot)

	validated := agentvalidate.Validate(capture.tempRoot)
	report.Diagnostics = append(report.Diagnostics, sanitizeDiagnostics(validated.Diagnostics, capture.tempRoot)...)
	if validated.Status == "invalid" {
		report.finish("invalid")
		return report
	}
	corpus, loaded := agentvalidate.Load(capture.tempRoot)
	report.Diagnostics = append(report.Diagnostics, sanitizeDiagnostics(loaded.Diagnostics, capture.tempRoot)...)
	if loaded.Status == "invalid" || corpus == nil {
		report.finish("invalid")
		return report
	}
	resolved, err := resolveCaptured(corpus, capture, &report)
	if err != nil {
		if !hasErrors(report.Diagnostics) {
			report.add("resolve.selection", capture.assignmentRel, "", "error", err.Error())
		}
		report.finish("invalid")
		return report
	}
	report.Snapshot = resolved
	for _, e := range resolved.ReadinessEvidence {
		report.add("resolve.readiness.unverified", e.Source, e.Field, "warning", e.Message)
	}
	for _, e := range resolved.CompatibilityEvidence {
		report.add("resolve.compatibility.unverified", e.Source, e.Field, "warning", e.Message)
	}
	report.finish("blocked")
	return report
}

type capturedCorpus struct {
	tempRoot      string
	assignmentRel string
	sources       []Source
	bytesByPath   map[string][]byte
}

func captureCorpus(assignmentPath, corpusRoot string) (*capturedCorpus, error) {
	if assignmentPath == "" {
		return nil, fmt.Errorf("assignment path is required")
	}
	assignmentAbs, err := filepath.Abs(assignmentPath)
	if err != nil {
		return nil, err
	}
	assignmentInfo, err := os.Lstat(assignmentAbs)
	if err != nil {
		return nil, fmt.Errorf("assignment: %w", err)
	}
	if assignmentInfo.Mode()&os.ModeSymlink != 0 || !assignmentInfo.Mode().IsRegular() {
		return nil, fmt.Errorf("assignment must be a regular non-symlink file")
	}
	if corpusRoot == "" {
		corpusRoot = filepath.Dir(assignmentAbs)
	}
	rootAbs, err := filepath.Abs(corpusRoot)
	if err != nil {
		return nil, err
	}
	rootInfo, err := os.Lstat(rootAbs)
	if err != nil {
		return nil, fmt.Errorf("corpus root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, fmt.Errorf("corpus root must be a real directory")
	}
	root, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return nil, err
	}
	assignmentParent, err := filepath.EvalSymlinks(filepath.Dir(assignmentAbs))
	if err != nil {
		return nil, fmt.Errorf("assignment parent: %w", err)
	}
	assignmentResolved := filepath.Join(assignmentParent, filepath.Base(assignmentAbs))
	assignmentRel, err := filepath.Rel(root, assignmentResolved)
	if err != nil || assignmentRel == ".." || strings.HasPrefix(assignmentRel, ".."+string(filepath.Separator)) || filepath.IsAbs(assignmentRel) {
		return nil, fmt.Errorf("assignment is outside corpus root")
	}
	assignmentRel = filepath.ToSlash(assignmentRel)
	tempRoot, err := os.MkdirTemp("", "aok-resolution-corpus-")
	if err != nil {
		return nil, err
	}
	c := &capturedCorpus{tempRoot: tempRoot, assignmentRel: assignmentRel, bytesByPath: map[string][]byte{}}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(tempRoot)
		}
	}()
	paths := []string{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symbolic links are forbidden: %s", filepath.ToSlash(rel(root, path)))
		}
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("special files are forbidden: %s", filepath.ToSlash(rel(root, path)))
		}
		paths = append(paths, path)
		if len(paths) > maxFiles {
			return fmt.Errorf("corpus exceeds %d files", maxFiles)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	var aggregate int64
	for _, path := range paths {
		rp := filepath.ToSlash(rel(root, path))
		b, e := readBounded(path)
		if e != nil {
			return nil, fmt.Errorf("%s: %w", rp, e)
		}
		aggregate += int64(len(b))
		if aggregate > maxBytes {
			return nil, fmt.Errorf("corpus exceeds %d MiB", maxBytes>>20)
		}
		dest := filepath.Join(tempRoot, filepath.FromSlash(rp))
		if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
			return nil, e
		}
		if e = os.WriteFile(dest, b, 0600); e != nil {
			return nil, e
		}
		digest := sha256.Sum256(b)
		c.sources = append(c.sources, Source{Path: rp, SHA256: hex.EncodeToString(digest[:])})
		c.bytesByPath[rp] = b
	}
	if _, ok := c.bytesByPath[assignmentRel]; !ok {
		return nil, fmt.Errorf("assignment was not captured beneath corpus root")
	}
	ok = true
	return c, nil
}

func resolveCaptured(corpus *agentvalidate.Corpus, capture *capturedCorpus, report *Report) (*Snapshot, error) {
	var assignment contracts.AgentAssignment
	resourceFound := false
	for _, loaded := range corpus.Resources {
		if loaded.Path != capture.assignmentRel {
			continue
		}
		decoded, err := contracts.DecodeTyped(loaded.Resource)
		if err != nil {
			return nil, fmt.Errorf("decode assignment: %w", err)
		}
		var ok bool
		assignment, ok = decoded.(contracts.AgentAssignment)
		if !ok {
			return nil, fmt.Errorf("assignment file kind is %s, expected AgentAssignment", loaded.Resource.Kind)
		}
		resourceFound = true
		break
	}
	if !resourceFound {
		return nil, fmt.Errorf("assignment file is not a valid AgentAssignment resource")
	}
	byRef := map[string]contracts.Resource{}
	for _, loaded := range corpus.Resources {
		byRef[refKey(loaded.Resource.Kind, loaded.Resource.Envelope.Metadata.ID, loaded.Resource.Envelope.Metadata.Version)] = loaded.Resource
	}
	decode := func(ref contracts.Reference, expected string) (any, error) {
		if ref.Kind != expected {
			return nil, fmt.Errorf("expected %s reference, got %s", expected, ref.Kind)
		}
		r, ok := byRef[refKey(ref.Kind, ref.ID, ref.Version)]
		if !ok {
			return nil, fmt.Errorf("missing exact reference %s %s@%s", ref.Kind, ref.ID, ref.Version)
		}
		return contracts.DecodeTyped(r)
	}
	a, err := decode(assignment.Agent, "Agent")
	if err != nil {
		return nil, err
	}
	agent := a.(contracts.Agent)
	p, err := decode(assignment.ExecutionProfile, "ExecutionProfile")
	if err != nil {
		return nil, err
	}
	profile := p.(contracts.ExecutionProfile)
	outValue, err := decode(assignment.Outputs.Contract, "OutputContract")
	if err != nil {
		return nil, err
	}
	output := outValue.(contracts.OutputContract)
	envValue, err := decode(agent.Environment.Profile, "EnvironmentProfile")
	if err != nil {
		return nil, err
	}
	env := envValue.(contracts.EnvironmentProfile)
	profileEnv := profile.Environment.Profile
	if !sameRef(agent.Environment.Profile, profileEnv) {
		return nil, fmt.Errorf("agent and selected profile name different environment profiles")
	}
	inputs := cloneRawMap(nil)
	if agent.Instructions.Parameters != nil {
		for name, parameter := range *agent.Instructions.Parameters {
			if len(parameter.Default) > 0 {
				inputs[name] = append(json.RawMessage(nil), parameter.Default...)
			}
		}
	}
	for key, value := range assignment.Inputs {
		inputs[key] = append(json.RawMessage(nil), value...)
	}
	outputParameters := cloneRawMap(nil)
	if agent.Outputs.Parameters != nil {
		for key, value := range *agent.Outputs.Parameters {
			outputParameters[key] = append(json.RawMessage(nil), value...)
		}
	}
	if assignment.Outputs.Parameters != nil {
		for key, value := range *assignment.Outputs.Parameters {
			outputParameters[key] = append(json.RawMessage(nil), value...)
		}
	}
	permissions, policyDiagnostics := agentpolicy.Intersect(agent.Permissions, profile.PermissionRestrictions, assignment.PermissionRestrictions)
	report.Diagnostics = append(report.Diagnostics, policyDiagnostics...)
	if hasErrors(policyDiagnostics) {
		return nil, fmt.Errorf("permission intersection failed")
	}
	if env.Network == "denied" {
		permissions.Network = contracts.NetworkPermissions{Mode: "denied"}
	}
	packetPath, err := corpus.ResolveFile(capture.assignmentRel, assignment.TaskPacket)
	if err != nil {
		return nil, fmt.Errorf("task packet: %w", err)
	}
	packetRel := filepath.ToSlash(rel(corpus.Root, packetPath))
	packetBytes, ok := capture.bytesByPath[packetRel]
	if !ok {
		return nil, fmt.Errorf("task packet was not captured")
	}
	packetDigest := sha256.Sum256(packetBytes)
	readinessEvidence, compatibilityEvidence := declaredEvidence(agent, profile, env, capture.assignmentRel)
	snapshot := &Snapshot{
		AssignmentPath:        capture.assignmentRel,
		Assignment:            assignment,
		Agent:                 agent,
		Profile:               profile,
		Output:                output,
		Environment:           env,
		TaskPacketSHA256:      hex.EncodeToString(packetDigest[:]),
		Inputs:                inputs,
		OutputParameters:      outputParameters,
		Permissions:           permissions,
		ReadinessEvidence:     readinessEvidence,
		CompatibilityEvidence: compatibilityEvidence,
		Sources:               append([]Source(nil), capture.sources...),
	}
	b, err := snapshot.digestBytes()
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("AOK-RESOLUTION-SNAPSHOT\x00v1\x00"))
	_, _ = h.Write(b)
	snapshot.Digest = hex.EncodeToString(h.Sum(nil))
	return snapshot, nil
}

func readBounded(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular non-symlink file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	if !os.SameFile(before, info) {
		return nil, fmt.Errorf("file changed while opening")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFile {
		return nil, fmt.Errorf("file exceeds 4 MiB")
	}
	return b, nil
}

func rel(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}
func refKey(kind, id, version string) string { return kind + "\x00" + id + "\x00" + version }
func sameRef(a, b contracts.Reference) bool {
	return refKey(a.Kind, a.ID, a.Version) == refKey(b.Kind, b.ID, b.Version)
}
func cloneRawMap(source contracts.ParameterValues) contracts.ParameterValues {
	out := contracts.ParameterValues{}
	for key, value := range source {
		out[key] = append(json.RawMessage(nil), value...)
	}
	return out
}

func (r *Report) add(code, source, field, severity, message string) {
	r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{Code: code, Source: source, Field: field, Severity: severity, Message: message})
}
func (r *Report) finish(status string) {
	sort.Slice(r.Diagnostics, func(i, j int) bool {
		a, b := r.Diagnostics[i], r.Diagnostics[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	if r.Diagnostics == nil {
		r.Diagnostics = []contracts.Diagnostic{}
	}
	r.Status = status
}

func hasErrors(ds []contracts.Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

func sanitizeDiagnostics(ds []contracts.Diagnostic, privateRoot string) []contracts.Diagnostic {
	result := append([]contracts.Diagnostic(nil), ds...)
	for i := range result {
		result[i].Source = strings.ReplaceAll(result[i].Source, filepath.ToSlash(privateRoot), ".")
		result[i].Source = strings.ReplaceAll(result[i].Source, privateRoot, ".")
		result[i].Message = strings.ReplaceAll(result[i].Message, privateRoot, "<corpus>")
	}
	return result
}

func declaredEvidence(agent contracts.Agent, profile contracts.ExecutionProfile, env contracts.EnvironmentProfile, source string) ([]Evidence, []Evidence) {
	readiness := []Evidence{{
		Check: "environment-enforcement", Status: "unverified", Source: source, Field: "environment",
		Message: "isolation, filesystem roots, and declared permission enforcement require trusted runtime attestation",
	}}
	readiness = append(readiness,
		Evidence{Check: "environment-isolation", Status: "unverified", Source: source, Field: "environmentProfile.isolation", Message: "declared isolation mode " + env.Isolation + " is not enforced by static resolution"},
		Evidence{Check: "environment-network", Status: "unverified", Source: source, Field: "environmentProfile.network", Message: "declared environment network mode " + env.Network + " is not enforced by static resolution"},
		Evidence{Check: "effective-permissions", Status: "unverified", Source: source, Field: "effectivePermissions", Message: "permission intersection is descriptive until a trusted executor confirms enforcement"},
	)
	if agent.Environment.ReadinessRequired {
		readiness = append(readiness, Evidence{Check: "readiness-required", Status: "unverified", Source: source, Field: "agent.environment.readinessRequired", Message: "agent requires environment readiness evidence, which static resolution cannot produce"})
	}
	for i, requirement := range env.Requirements {
		if requirement.Required {
			readiness = append(readiness, Evidence{Check: "environment-requirement", Status: "unverified", Source: source, Field: fmt.Sprintf("environmentProfile.requirements[%d].ref", i), Message: "required runtime requirement " + requirement.Ref + " is not probed during static resolution"})
		}
	}
	for i, check := range env.ReadinessChecks {
		readiness = append(readiness, Evidence{Check: "readiness-check", Status: "unverified", Source: source, Field: fmt.Sprintf("environmentProfile.readinessChecks[%d]", i), Message: "declared readiness check " + check + " is not executed during static resolution"})
	}
	for i, rule := range agent.Permissions.Actions.Rules {
		readiness = append(readiness, Evidence{Check: "action-enforcement", Status: "unverified", Source: source, Field: fmt.Sprintf("agent.permissions.actions.rules[%d]", i), Message: fmt.Sprintf("action %s on %s is assigned to enforcement owner %s; that owner is not attested", rule.Action, rule.Resource, rule.EnforcementOwner)})
	}
	compatibility := []Evidence{{Check: "adapter", Status: "unverified", Source: source, Field: "executionProfile.client.adapter", Message: "adapter " + profile.Client.Adapter + " support is unknown without a compatibility catalog"},
		{Check: "adapter-interface", Status: "unverified", Source: source, Field: "executionProfile.client.interface", Message: "adapter interface " + profile.Client.Interface + " support is unknown without a compatibility catalog"},
		{Check: "model", Status: "unverified", Source: source, Field: "executionProfile.model.selector", Message: "model selector " + profile.Model.Selector + " support and availability are unknown without a compatibility catalog"},
		{Check: "reasoning", Status: "unverified", Source: source, Field: "executionProfile.model.reasoning.mode", Message: "reasoning mode " + profile.Model.Reasoning.Mode + " support is unknown for the selected adapter and model"},
	}
	if profile.Client.VersionConstraint != nil {
		compatibility = append(compatibility, Evidence{Check: "adapter-version", Status: "unverified", Source: source, Field: "executionProfile.client.versionConstraint", Message: "adapter version constraint " + *profile.Client.VersionConstraint + " cannot be checked without a compatibility catalog"})
	}
	if profile.Model.Provider != nil {
		compatibility = append(compatibility, Evidence{Check: "model-provider", Status: "unverified", Source: source, Field: "executionProfile.model.provider", Message: "provider " + *profile.Model.Provider + " availability is unknown without a compatibility catalog"})
	}
	if profile.Model.Reasoning.Effort != nil {
		compatibility = append(compatibility, Evidence{Check: "reasoning-effort", Status: "unverified", Source: source, Field: "executionProfile.model.reasoning.effort", Message: "reasoning effort " + *profile.Model.Reasoning.Effort + " support is unknown for the selected adapter and model"})
	}
	if profile.Model.Reasoning.BudgetTokens != nil {
		compatibility = append(compatibility, Evidence{Check: "reasoning-budget", Status: "unverified", Source: source, Field: "executionProfile.model.reasoning.budgetTokens", Message: "token-budget reasoning support is unknown for the selected adapter and model"})
	}
	for _, group := range []struct {
		name string
		refs []contracts.CapabilityReference
	}{
		{"tools", agent.Capabilities.Tools}, {"skills", agent.Capabilities.Skills}, {"mcps", agent.Capabilities.MCPs}, {"plugins", agent.Capabilities.Plugins},
	} {
		for i, ref := range group.refs {
			compatibility = append(compatibility, Evidence{Check: "capability", Status: "unverified", Source: source, Field: fmt.Sprintf("agent.capabilities.%s[%d]", group.name, i), Message: fmt.Sprintf("capability %s@%s (required=%t) native scope and support have not been verified", ref.ID, ref.Version, ref.Required)})
		}
	}
	for _, control := range []struct {
		field   string
		present bool
	}{
		{"limits", profile.Limits != nil}, {"model.generation", profile.Model.Generation != nil}, {"extensions", profile.Extensions != nil},
	} {
		if control.present {
			compatibility = append(compatibility, Evidence{Check: "profile-control", Status: "unverified", Source: source, Field: "executionProfile." + control.field, Message: "requested profile control requires adapter compatibility and enforcement evidence"})
		}
	}
	return readiness, compatibility
}
