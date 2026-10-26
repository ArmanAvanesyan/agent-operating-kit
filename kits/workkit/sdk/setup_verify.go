package sdk

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// SetupTrust must be installed by protected bootstrap. Current authenticates the
// externally managed policy pin, issuer, revocation and route qualification.
// Consume is the protected one-use operation boundary, not a provider sender.
type SetupTrust interface {
	Current(context.Context, s.Verification, []byte) error
	Consume(context.Context, s.Verification) error
}

func (svc *Service) WithSetupTrust(t SetupTrust) *Service { v := *svc; v.setupTrust = t; return &v }
func SetupVerificationSigningBytes(v s.Verification) []byte {
	v.Signature = ""
	return append([]byte("workkit-setup/v1:setup-verification\n"), engine.Encode(v)...)
}

type VerifySetupRequest struct {
	Observation, SourceObservation, Desired, Mapping, MappingContext, AsOf, Policy, Verification, Out, Workspace, Root, ExpectedWorkspaceSHA256 string
	WriteBinding                                                                                                                                bool
}

// SetupCandidateSHA256 binds the exact deterministic candidate descriptor before
// signature/receipt bytes exist; it cannot depend on its own signature hash.
func SetupCandidateSHA256(inputs map[string]string, scope s.Scope, verificationID string) string {
	return check.Hash(struct {
		Version, Kind  string
		Scope          s.Scope
		Inputs         map[string]string
		VerificationID string
	}{s.Version, "SetupBindingCandidate", scope, inputs, verificationID})
}
func verifySetupProof(v s.Verification, p s.Policy, policyRaw []byte, inputs map[string]string, o s.Observation, out, workspaceHash, configurationRoot, workspacePath string, now time.Time) error {
	if v.ConfigurationRoot != configurationRoot || v.WorkspacePath != workspacePath {
		return errors.New("local binding target path mismatch")
	}
	if configurationRoot != "" || workspacePath != "" {
		if !filepath.IsAbs(configurationRoot) || filepath.Clean(configurationRoot) != configurationRoot || !filepath.IsAbs(workspacePath) || filepath.Clean(workspacePath) != workspacePath || workspaceHash == "" {
			return errors.New("canonical exact selection paths required")
		}
	}
	if v.CandidateSHA256 != SetupCandidateSHA256(inputs, o.Scope, v.ID) || v.Version != s.Version || v.Kind != "SetupVerification" || v.Purpose != "setup-verification" || v.ID == "" || len(v.ID) > 128 || !v.OperationalMetadata || o.QualificationOnly || v.Scope != o.Scope || v.ImplementationSHA256 != engine.ImplementationSHA256 || v.PolicySHA256 != SHA256(policyRaw) || v.WorkspaceSHA256 != workspaceHash || !filepath.IsAbs(out) || filepath.Clean(out) != out || v.OutputPath != out || check.Hash(v.Inputs) != check.Hash(inputs) || p.Version != s.Version || p.Kind != "SetupPolicy" || p.MaxAgeSeconds < 1 || p.MaxAgeSeconds > 300 {
		return errors.New("setup proof binding mismatch or qualification-only evidence")
	}
	start, e := time.Parse(time.RFC3339Nano, v.IssuedAt)
	end, e2 := time.Parse(time.RFC3339Nano, v.ExpiresAt)
	ps, e3 := time.Parse(time.RFC3339Nano, p.NotBefore)
	pe, e4 := time.Parse(time.RFC3339Nano, p.ExpiresAt)
	if e != nil || e2 != nil || e3 != nil || e4 != nil || now.Before(start) || !now.Before(end) || !end.After(start) || end.Sub(start) > 5*time.Minute || now.Before(ps) || !now.Before(pe) || end.After(pe) {
		return errors.New("stale setup proof or policy")
	}
	allowed := false
	for _, scope := range p.AllowedScopes {
		if scope == o.Scope {
			allowed = true
		}
	}
	if !allowed {
		return errors.New("setup scope not approved")
	}
	for _, id := range p.RevokedIDs {
		if id == v.ID {
			return errors.New("revoked setup proof")
		}
	}
	seen := map[string]bool{}
	var key ed25519.PublicKey
	for _, i := range p.Issuers {
		b, e := base64.StdEncoding.DecodeString(i.PublicKey)
		if e != nil || len(b) != ed25519.PublicKeySize || i.ID == "" || seen[i.ID] {
			return errors.New("invalid setup issuer")
		}
		seen[i.ID] = true
		if i.ID == v.Issuer {
			key = b
		}
	}
	sig, e := base64.StdEncoding.DecodeString(v.Signature)
	if e != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, SetupVerificationSigningBytes(v), sig) {
		return errors.New("invalid setup signature")
	}
	return nil
}
func (svc *Service) VerifySetup(ctx context.Context, q VerifySetupRequest) (s.BindingReceipt, error) {
	receipt := s.BindingReceipt{}
	if e := setupOutput(q.Out, q.Observation, q.SourceObservation); e != nil {
		return receipt, e
	}
	o, assets, e := LoadSetupObservation(ctx, q.Observation)
	if e != nil {
		return receipt, e
	}
	src := o
	sourceAssets := assets
	if q.SourceObservation != "" {
		src, sourceAssets, e = LoadSetupObservation(ctx, q.SourceObservation)
		if e != nil {
			return receipt, e
		}
	}
	desired, e := ReadLocal(ctx, q.Desired)
	if e != nil {
		return receipt, e
	}
	mapping, e := ReadLocal(ctx, q.Mapping)
	if e != nil {
		return receipt, e
	}
	mc, e := ReadLocal(ctx, q.MappingContext)
	if e != nil {
		return receipt, e
	}
	asOf := q.AsOf
	connected := q.Policy != "" || q.Verification != "" || q.WriteBinding
	if connected {
		if q.AsOf != "" || q.Policy == "" || q.Verification == "" {
			return receipt, errors.New("connected verification requires policy/proof and current time")
		}
		asOf = time.Now().UTC().Format(time.RFC3339Nano)
	}
	r, e := EvaluateSetup(o, desired, asOf)
	if e != nil {
		return receipt, e
	}
	r.Kind = "BindingEvaluation"
	mr, e := EvaluateMapping(mapping, mc, src, o, asOf)
	if e != nil {
		return receipt, e
	}
	r.Inputs["mapping"] = SHA256(mapping)
	r.Inputs["mappingContext"] = SHA256(mc)
	r.Inputs["sourceObservation"] = check.Hash(src)
	r.Checks = append(r.Checks, mr.Checks...)
	r.Diagnostics = append(r.Diagnostics, mr.Diagnostics...)
	if mr.Status != "ok" || len(r.Changes) > 0 {
		r.Status = "blocked"
	}
	if src.QualificationOnly {
		check.Block(&r, "binding.source-qualification-only", "sourceObservation", "source catalogs require independent operational metadata")
	}
	if o.QualificationOnly {
		check.Block(&r, "binding.qualification-only", "observation", "qualification captures support planning only")
	}
	var m api.Mapping
	if e = setupDocument(mapping, "Mapping", &m); e != nil {
		return receipt, e
	}
	var desiredDoc api.DesiredSetup
	if e = setupDocument(desired, "DesiredSetup", &desiredDoc); e != nil {
		return receipt, e
	}
	var mappingContext s.MappingContext
	if DecodeRead(mc, &mappingContext) != nil {
		return receipt, errors.New("invalid context")
	}
	parent := mappingContext.DestinationContainerID
	if parent == "" {
		parent = o.Scope.ContainerID
	}
	if parent != desiredDoc.Spec.ContainerId {
		check.Block(&r, "binding.scope", "mappingContext", "mapping destination parent differs from desired container")
	}
	for _, f := range desiredDoc.Spec.Fields {
		found := false
		for _, mf := range m.Spec.Fields {
			if mf.Canonical == string(f.LogicalId) && f.NativeId != nil && mf.TargetFieldId == *f.NativeId {
				found = true
			}
		}
		if !found {
			check.Block(&r, "binding.mapping", string(f.LogicalId), "required desired field lacks exact destination mapping")
		}
	}
	binding := api.WorkBinding{APIVersion: api.Version, Kind: "WorkBinding", Metadata: api.Metadata{Id: "binding-" + check.Hash(r.Inputs)[:24], Revision: 1}, Spec: api.WorkBindingSpec{ConnectionRef: o.ConnectionRef, MappingRef: api.Ref{Kind: "Mapping", Id: m.Metadata.Id, Revision: m.Metadata.Revision, Sha256: api.Sha(SHA256(mapping))}, Scope: api.Scope{TenantId: o.Scope.TenantID, ContainerIds: []string{desiredDoc.Spec.ContainerId}}, Status: "draft", Checks: r.Checks, Evidence: []api.Asset{}}}
	receipt.Report = r
	receipt.Binding = binding
	inputs := map[string][]byte{"observation.json": engine.Encode(o), "source-observation.json": engine.Encode(src), "desired.json": desired, "mapping.json": mapping, "mapping-context.json": mc}
	var p s.Policy
	var v s.Verification
	var policyRaw, proofRaw []byte
	if connected {
		policyRaw, e = ReadLocal(ctx, q.Policy)
		if e != nil {
			return receipt, e
		}
		proofRaw, e = ReadLocal(ctx, q.Verification)
		if e != nil {
			return receipt, e
		}
		if DecodeRead(policyRaw, &p) != nil || DecodeRead(proofRaw, &v) != nil {
			return receipt, errors.New("invalid setup policy/proof")
		}
		sourceApproved := false
		for _, scope := range p.AllowedScopes {
			if scope == src.Scope {
				sourceApproved = true
			}
		}
		if !sourceApproved {
			check.Block(&receipt.Report, "binding.source-scope", "sourceObservation", "source scope is not approved by setup policy")
		}
		targetRoot, targetWorkspace := "", ""
		if q.WriteBinding {
			targetRoot = q.Root
			targetWorkspace = q.Workspace
		}
		check.Fresh(&receipt.Report, o, asOf, p.MaxAgeSeconds)
		check.Fresh(&receipt.Report, src, asOf, p.MaxAgeSeconds)
		if svc.setupTrust == nil {
			check.Block(&receipt.Report, "binding.trust", "proof", "protected trust bootstrap unavailable")
		} else if e = verifySetupProof(v, p, policyRaw, r.Inputs, o, q.Out, q.ExpectedWorkspaceSHA256, targetRoot, targetWorkspace, time.Now().UTC()); e != nil {
			check.Block(&receipt.Report, "binding.proof", "proof", "signature, purpose, freshness or scope invalid")
		} else if e = svc.setupTrust.Current(ctx, v, policyRaw); e != nil {
			check.Block(&receipt.Report, "binding.revocation", "proof", "current protected route/issuer checks unavailable")
		}
		inputs["policy.json"] = policyRaw
		inputs["verification.json"] = proofRaw
		receipt.VerificationSHA256 = SHA256(proofRaw)
		receipt.CandidateSHA256 = v.CandidateSHA256
	} else {
		check.Block(&receipt.Report, "binding.proof", "proof", "independent operational setup verification required")
	}
	if receipt.Report.Status == "ok" && connected {
		receipt.Binding.Metadata.Id = "binding-" + receipt.CandidateSHA256[:24]
		receipt.Binding.Spec.Status = "verified"
		check.OK(&receipt.Report, "protected-setup-verification")
	} else {
		receipt.Binding.Spec.Status = "draft"
	}
	receipt.Binding.Spec.Checks = receipt.Report.Checks
	// Capture original native assets with scoped paths in the emitted evidence.
	for n, b := range sourceAssets {
		inputs["source-native/"+n] = b
	}
	for n, b := range assets {
		inputs["native/"+n] = b
	}
	receipt.Binding.Spec.Evidence = []api.Asset{{Path: "assets/observation.json", Sha256: api.Sha(SHA256(engine.Encode(o)))}}
	if !q.WriteBinding {
		e = writeSetupReport(ctx, q.Out, receipt, inputs)
		return receipt, e
	}
	if receipt.Binding.Spec.Status != "verified" {
		e = writeSetupReport(ctx, q.Out, receipt, inputs)
		return receipt, e
	}
	return svc.selectSetupBinding(ctx, q, receipt, inputs, policyRaw, v, m, o, src)
}
func (svc *Service) selectSetupBinding(ctx context.Context, q VerifySetupRequest, receipt s.BindingReceipt, inputs map[string][]byte, policy []byte, v s.Verification, m api.Mapping, observation, sourceObservation s.Observation) (s.BindingReceipt, error) {
	root, e := engine.SafePath(q.Root)
	if e != nil {
		return receipt, e
	}
	wp, e := engine.SafePath(q.Workspace)
	if e != nil {
		return receipt, e
	}
	rel, e := filepath.Rel(root, wp)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || q.ExpectedWorkspaceSHA256 == "" {
		return receipt, errors.New("exact workspace inside configuration root required")
	}
	if e = setupOutput(q.Out, filepath.Join(root, "resources"), q.Observation, q.SourceObservation); e != nil {
		return receipt, e
	}
	output, e := engine.ReserveBundle(ctx, q.Out)
	if e != nil {
		return receipt, e
	}
	// A create-only lock persists across crashes; no PID/time heuristic releases it.
	lock := filepath.Join(root, ".workkit-binding.lock")
	lf, e := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return receipt, failure("binding.conflict", "binding selection locked; inspect recovery evidence", 4, e)
	}
	_, e = lf.Write(engine.Encode(map[string]string{"verification": receipt.VerificationSHA256, "output": q.Out}))
	if e == nil {
		e = lf.Sync()
	}
	lf.Close()
	if e != nil {
		return receipt, e
	}
	if e = syncDir(root); e != nil {
		return receipt, e
	}
	// Failures after lock creation retain it. An independent recovery must inspect.
	old, e := ReadLocal(ctx, wp)
	if e != nil || SHA256(old) != q.ExpectedWorkspaceSHA256 {
		return receipt, failure("binding.conflict", "workspace changed; lock retained", 4, e)
	}
	snapshot, e := engine.Resolve(ctx, wp, root)
	if e != nil {
		return receipt, e
	}
	var workspace api.Workspace
	if e = setupDocument(old, "Workspace", &workspace); e != nil {
		return receipt, e
	}
	// Exact connection and mapping must already exist under the selected root.
	hasConn, hasMap := false, false
	for _, d := range snapshot.Documents {
		if d.Kind == "ConnectionProfile" && d.ID == receipt.Binding.Spec.ConnectionRef.Id && engine.Hash(d.Raw) == string(receipt.Binding.Spec.ConnectionRef.Sha256) {
			hasConn = true
		}
		if d.Kind == "Mapping" && d.ID == m.Metadata.Id && engine.Hash(d.Raw) == string(receipt.Binding.Spec.MappingRef.Sha256) {
			hasMap = true
		}
	}
	if !hasConn || !hasMap {
		return receipt, errors.New("selected workspace lacks exact connection or mapping bytes")
	}
	if e = svc.setupTrust.Current(ctx, v, policy); e != nil {
		return receipt, e
	}
	if e = svc.setupTrust.Consume(ctx, v); e != nil {
		return receipt, e
	}
	backup := wp + ".before-" + receipt.VerificationSHA256
	f, e := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return receipt, e
	}
	_, e = f.Write(old)
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e != nil {
		return receipt, e
	}
	assetName := "assets/setup-" + receipt.VerificationSHA256
	assetOut := filepath.Join(root, assetName)
	// Durable evidence first; the domain binding references the copied report.
	if e = writeSetupReport(ctx, assetOut, receipt, inputs); e != nil {
		return receipt, e
	}
	evidenceRaw, e := ReadLocal(ctx, filepath.Join(assetOut, "report.json"))
	if e != nil {
		return receipt, e
	}
	receipt.Binding.Spec.Evidence = []api.Asset{{Path: assetName + "/report.json", Sha256: api.Sha(SHA256(evidenceRaw))}}
	bindingRaw := engine.Encode(receipt.Binding)
	path := "resources/setup-" + receipt.VerificationSHA256 + ".json"
	dest := filepath.Join(root, path)
	if _, e = engine.SafePath(filepath.Dir(dest)); e != nil {
		return receipt, e
	}
	f, e = os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return receipt, e
	}
	_, e = f.Write(bindingRaw)
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e != nil {
		return receipt, e
	}
	if e = syncDir(filepath.Dir(dest)); e != nil {
		return receipt, e
	}
	// Preserve all other workspace fields and extensions; only revision and the
	// binding reference selected for this exact connection change.
	var value map[string]any
	workspaceDoc, parseErr := Parse(old, "json")
	if parseErr != nil {
		workspaceDoc, parseErr = Parse(old, "yaml")
	}
	if parseErr != nil {
		return receipt, parseErr
	}
	if e = workspaceDoc.Decode(&value); e != nil {
		return receipt, e
	}
	spec := value["spec"].(map[string]any)
	refs := []any{}
	for _, b := range workspace.Spec.Bindings {
		replace := false
		for _, d := range snapshot.Documents {
			if d.Kind == "WorkBinding" && d.ID == b.Id {
				var existing api.WorkBinding
				if setupDocument(d.Raw, "WorkBinding", &existing) == nil && existing.Spec.ConnectionRef == receipt.Binding.Spec.ConnectionRef {
					replace = true
				}
			}
		}
		if !replace {
			refs = append(refs, b)
		}
	}
	refs = append(refs, api.Ref{Kind: "WorkBinding", Id: receipt.Binding.Metadata.Id, Revision: 1, Sha256: api.Sha(SHA256(bindingRaw))})
	spec["bindings"] = refs
	updated := engine.Encode(value)
	if _, e = engine.Parse(updated, "json"); e != nil {
		return receipt, e
	}
	tmp := wp + ".select-" + receipt.VerificationSHA256
	f, e = os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return receipt, e
	}
	_, e = f.Write(updated)
	if e == nil {
		e = f.Sync()
	}
	f.Close()
	if e != nil {
		return receipt, e
	}
	current, e := ReadLocal(ctx, wp)
	if e != nil || SHA256(current) != q.ExpectedWorkspaceSHA256 {
		return receipt, failure("binding.conflict", "workspace changed before selection", 4, e)
	}
	if e = ctx.Err(); e != nil {
		return receipt, e
	}
	if e = svc.setupTrust.Current(ctx, v, policy); e != nil {
		return receipt, e
	}
	var finalPolicy s.Policy
	if DecodeRead(policy, &finalPolicy) != nil {
		return receipt, errors.New("policy invalid")
	}
	if e = verifySetupProof(v, finalPolicy, policy, receipt.Report.Inputs, observation, q.Out, q.ExpectedWorkspaceSHA256, root, wp, time.Now().UTC()); e != nil {
		return receipt, e
	}
	fresh := check.NewReport("FinalBindingCheck", time.Now().UTC().Format(time.RFC3339Nano))
	check.Fresh(&fresh, observation, fresh.AsOf, finalPolicy.MaxAgeSeconds)
	check.Fresh(&fresh, sourceObservation, fresh.AsOf, finalPolicy.MaxAgeSeconds)
	if fresh.Status != "ok" {
		return receipt, errors.New("metadata expired before selection")
	}
	for i, path := range snapshot.Paths {
		current, e := ReadLocal(ctx, path)
		if e != nil || !bytes.Equal(current, snapshot.Documents[i].Raw) {
			return receipt, errors.New("resolved configuration changed before selection")
		}
	}
	for name, prior := range snapshot.Assets {
		path, e := ConfinedLocal(root, name)
		if e != nil {
			return receipt, e
		}
		current, e := ReadLocal(ctx, path)
		if e != nil || !bytes.Equal(current, prior) {
			return receipt, errors.New("referenced asset changed before selection")
		}
	}
	currentBinding, e := ReadLocal(ctx, dest)
	if e != nil || !bytes.Equal(currentBinding, bindingRaw) {
		return receipt, errors.New("new binding changed before selection")
	}
	if _, e = loadSetupReportBundle(ctx, assetOut); e != nil {
		return receipt, e
	}
	if e = os.Rename(tmp, wp); e != nil {
		return receipt, e
	}
	if e = syncDir(filepath.Dir(wp)); e != nil {
		return receipt, failure("binding.uncertain", "workspace replacement durability uncertain; retain lock", 5, e)
	}
	receipt.Selected = true
	receipt.PreviousWorkspaceSHA256 = SHA256(old)
	receipt.WorkspaceSHA256 = SHA256(updated)
	if e = output.Write(ctx, setupReportFiles(receipt, inputs)); e == nil {
		e = output.Sync(ctx)
	}
	if e != nil {
		return receipt, failure("binding.uncertain", "selected workspace; output receipt incomplete; retain lock", 5, e)
	}
	if e = os.Remove(lock); e != nil {
		return receipt, e
	}
	return receipt, syncDir(root)
}
