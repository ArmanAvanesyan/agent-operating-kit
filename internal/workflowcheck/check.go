// Package workflowcheck validates cross-record P1 workflow semantics. It is
// intentionally pure: asset access is supplied by the caller.
package workflowcheck

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

type Document struct {
	Resource contracts.Resource
	Path     string
}

// AssetResolver returns exact asset bytes after enforcing the caller's safe,
// corpus-relative resolution policy. ownerPath identifies the document that
// declared the asset; refPath is the corpus-relative path in AssetRef.
type AssetResolver func(ownerPath, refPath string) ([]byte, error)

type key struct{ kind, id, version string }
type indexes struct {
	docs map[key]Document
}
type attemptIdentity struct {
	host, session, attempt string
}
type hostBindingIdentity struct {
	host, session, attempt                                          string
	itemKind, itemID, itemVersion                                   string
	assignmentKind, assignmentID, assignmentVersion, assignmentHash string
}
type boundAttempt struct {
	itemKind, itemID, itemVersion                   string
	assignmentKind, assignmentID, assignmentVersion string
}
type operationIdentity struct {
	adapter, tenant, operationID string
}

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Check returns deterministic structured diagnostics for the supplied parsed
// resources. It never consults wall-clock time or performs I/O except through
// resolve.
func Check(resources []Document, resolve AssetResolver) []contracts.Diagnostic {
	var ds []contracts.Diagnostic
	add := func(code, source, field, message string) {
		ds = append(ds, contracts.Diagnostic{Code: code, Source: source, Field: field, Severity: "error", Message: message})
	}
	ix := indexes{docs: map[key]Document{}}
	for _, d := range resources {
		k := key{d.Resource.Kind, d.Resource.Metadata.ID, d.Resource.Metadata.Version}
		if _, ok := ix.docs[k]; ok {
			add("workflow.resource.duplicate", d.Path, "metadata", "duplicate exact resource snapshot")
		} else {
			ix.docs[k] = d
		}
	}
	asset := func(owner, field string, ref contracts.AssetRef) []byte {
		if !hashPattern.MatchString(ref.SHA256) {
			add("workflow.asset.hash", owner, field+".sha256", "asset hash must be lowercase SHA-256 hex")
		}
		if strings.TrimSpace(ref.Path) == "" {
			add("workflow.asset.path", owner, field+".path", "asset path is required")
			return nil
		}
		if resolve == nil {
			add("workflow.asset.unresolved", owner, field, "asset resolver is unavailable")
			return nil
		}
		b, err := resolve(owner, ref.Path)
		if err != nil {
			add("workflow.asset.read", owner, field, fmt.Sprintf("cannot resolve asset: %v", err))
			return nil
		}
		sum := sha256.Sum256(b)
		if hex.EncodeToString(sum[:]) != ref.SHA256 {
			add("workflow.asset.hash-mismatch", owner, field+".sha256", "asset bytes do not match declared SHA-256")
		}
		return b
	}
	lookup := func(source, field string, ref contracts.Reference, kind string) (Document, bool) {
		if ref.Kind != kind {
			add("workflow.reference.kind", source, field+".kind", "reference kind must be "+kind)
			return Document{}, false
		}
		d, ok := ix.docs[key{ref.Kind, ref.ID, ref.Version}]
		if !ok {
			add("workflow.reference.missing", source, field, "exact referenced snapshot does not exist")
			return Document{}, false
		}
		return d, true
	}
	identityKey := func(i contracts.RemoteIdentity) string { b, _ := json.Marshal(i); return string(b) }
	bindings := map[string]contracts.Reference{}
	bindingIdentity := map[string]string{}
	hostSnapshotIdentity := map[string]hostBindingIdentity{}
	attempts := map[attemptIdentity]boundAttempt{}
	operations := map[operationIdentity]string{}
	receiptIDs := map[string]bool{}
	// Validate each binding snapshot and its packet.
	for _, d := range resources {
		if d.Resource.Kind != "WorkItemBinding" {
			continue
		}
		v, err := contracts.DecodeTyped(d.Resource)
		if err != nil {
			add("workflow.decode", d.Path, "", "cannot decode work item binding: "+err.Error())
			continue
		}
		w := v.(contracts.WorkItemBinding)
		ik := identityKey(w.Identity)
		stableBinding := d.Resource.Kind + "\x00" + d.Resource.Metadata.ID
		if old, ok := bindingIdentity[stableBinding]; ok && old != ik {
			add("workflow.identity.binding-mutated", d.Path, "identity", "a binding metadata ID cannot change immutable remote identity across versions")
		} else {
			bindingIdentity[stableBinding] = ik
		}
		if prev, ok := bindings[ik]; ok && (prev.ID != d.Resource.Metadata.ID || prev.Kind != d.Resource.Kind) {
			add("workflow.identity.binding-conflict", d.Path, "identity", "one remote identity cannot have distinct binding IDs")
		} else if !ok {
			bindings[ik] = contracts.Reference{Kind: "WorkItemBinding", ID: d.Resource.Metadata.ID, Version: d.Resource.Metadata.Version}
		}
		seen := map[string]bool{}
		canonicalSeen := map[string]bool{}
		for n, m := range w.FieldMappings {
			if m.CanonicalField == "" || m.ProviderFieldID == "" {
				add("workflow.mapping.required", d.Path, fmt.Sprintf("fieldMappings[%d]", n), "canonicalField and providerFieldId are required")
			}
			if seen[m.ProviderFieldID] {
				add("workflow.mapping.duplicate", d.Path, fmt.Sprintf("fieldMappings[%d].providerFieldId", n), "provider field IDs must be unique")
			}
			seen[m.ProviderFieldID] = true
			if canonicalSeen[m.CanonicalField] {
				add("workflow.mapping.canonical-duplicate", d.Path, fmt.Sprintf("fieldMappings[%d].canonicalField", n), "canonical field keys must be unique")
			}
			canonicalSeen[m.CanonicalField] = true
			if m.Owner != "tracker" && m.Owner != "aok" && m.Owner != "shared" {
				add("workflow.mapping.owner", d.Path, fmt.Sprintf("fieldMappings[%d].owner", n), "owner must be tracker, aok, or shared")
			}
		}
		asset(d.Path, "packet", w.Packet)
	}
	// Host attempts bind immutable item/assignment identity within host/session/attempt.
	for _, d := range resources {
		if d.Resource.Kind != "HostTaskBinding" {
			continue
		}
		v, e := contracts.DecodeTyped(d.Resource)
		if e != nil {
			add("workflow.decode", d.Path, "", e.Error())
			continue
		}
		h := v.(contracts.HostTaskBinding)
		stableHostID := d.Resource.Kind + "\x00" + d.Resource.Metadata.ID
		hostIdentity := hostBindingIdentity{host: h.Host, session: h.SessionID, attempt: h.AttemptID, itemKind: h.WorkItemRef.Kind, itemID: h.WorkItemRef.ID, itemVersion: h.WorkItemRef.Version, assignmentKind: h.AssignmentRef.Kind, assignmentID: h.AssignmentRef.ID, assignmentVersion: h.AssignmentRef.Version, assignmentHash: h.AssignmentSHA256}
		if old, ok := hostSnapshotIdentity[stableHostID]; ok && old != hostIdentity {
			add("workflow.attempt.snapshot-mutated", d.Path, "metadata.id", "a host task binding ID cannot change attempt, item, or assignment across versions")
		} else {
			hostSnapshotIdentity[stableHostID] = hostIdentity
		}
		_, itemOK := lookup(d.Path, "workItemRef", h.WorkItemRef, "WorkItemBinding")
		ass, assOK := lookup(d.Path, "assignmentRef", h.AssignmentRef, "AgentAssignment")
		if assOK {
			b := ass.Resource.Document
			sum := sha256.Sum256(b)
			if hex.EncodeToString(sum[:]) != h.AssignmentSHA256 {
				add("workflow.assignment.hash-mismatch", d.Path, "assignmentSha256", "assignment bytes do not match declared SHA-256")
			}
		}
		attemptKey := attemptIdentity{host: h.Host, session: h.SessionID, attempt: h.AttemptID}
		if itemOK && assOK {
			want := boundAttempt{itemKind: h.WorkItemRef.Kind, itemID: h.WorkItemRef.ID, itemVersion: h.WorkItemRef.Version, assignmentKind: h.AssignmentRef.Kind, assignmentID: h.AssignmentRef.ID, assignmentVersion: h.AssignmentRef.Version}
			if old, ok := attempts[attemptKey]; ok && old != want {
				add("workflow.attempt.conflict", d.Path, "attemptId", "host/session/attempt identity is already bound differently")
			} else {
				attempts[attemptKey] = want
			}
		}
		if h.PlanStepRef != nil {
			plan, ok := lookup(d.Path, "planStepRef.planRef", h.PlanStepRef.PlanRef, "PlanDefinition")
			if ok {
				pv, err := contracts.DecodeTyped(plan.Resource)
				if err == nil {
					found := false
					for _, s := range pv.(contracts.PlanDefinition).Steps {
						if s.ID == h.PlanStepRef.StepID {
							found = true
							if s.AssignmentRef != h.AssignmentRef {
								add("workflow.plan.assignment-mismatch", d.Path, "planStepRef", "plan step selects a different assignment")
							}
							break
						}
					}
					if !found {
						add("workflow.plan.step-missing", d.Path, "planStepRef.stepId", "plan step does not exist")
					}
				}
			}
		}
	}
	// Index typed records used by later cross checks.
	decode := func(d Document) any {
		v, e := contracts.DecodeTyped(d.Resource)
		if e != nil {
			add("workflow.decode", d.Path, "", e.Error())
			return nil
		}
		return v
	}
	// Run snapshots must remain consistent with the selected host binding and assignment/profile.
	for _, d := range resources {
		if d.Resource.Kind != "RunRecord" {
			continue
		}
		v := decode(d)
		if v == nil {
			continue
		}
		r := v.(contracts.RunRecord)
		hd, ok := lookup(d.Path, "hostTaskBindingRef", r.HostTaskBindingRef, "HostTaskBinding")
		if !ok {
			continue
		}
		hv := decode(hd)
		if hv == nil {
			continue
		}
		h := hv.(contracts.HostTaskBinding)
		if r.AttemptID != h.AttemptID {
			add("workflow.run.attempt-mismatch", d.Path, "attemptId", "run attempt must match host task binding")
		}
		if r.AssignmentSHA256 != h.AssignmentSHA256 {
			add("workflow.run.assignment-mismatch", d.Path, "assignmentSha256", "run assignment hash must match host task binding")
		}
		ad, aok := lookup(d.Path, "hostTaskBindingRef.assignmentRef", h.AssignmentRef, "AgentAssignment")
		if aok {
			av := decode(ad)
			if av != nil && r.ExecutionProfileRef != av.(contracts.AgentAssignment).ExecutionProfile {
				add("workflow.run.profile-mismatch", d.Path, "executionProfileRef", "run profile must match assignment profile")
			}
		}
		if (r.LeaseID == nil) != (r.Fence == nil) {
			add("workflow.run.fence-pair", d.Path, "leaseId", "leaseId and fence must be present together")
		}
		if r.Fence != nil {
			n, err := r.Fence.Int64()
			if err != nil || n < 0 || n > 2147483647 {
				add("workflow.run.fence", d.Path, "fence", "fence must be a nonnegative bounded integer")
			}
		}
		for i, ref := range r.PolicyRefs {
			if ref.Kind != "ContextPolicy" && ref.Kind != "LifecyclePolicy" {
				add("workflow.run.policy-kind", d.Path, fmt.Sprintf("policyRefs[%d].kind", i), "run policy references must name ContextPolicy or LifecyclePolicy")
				continue
			}
			if _, ok := ix.docs[key{ref.Kind, ref.ID, ref.Version}]; !ok {
				add("workflow.reference.missing", d.Path, fmt.Sprintf("policyRefs[%d]", i), "exact policy snapshot does not exist")
			}
		}
		validTime(d.Path, "observedAt", r.ObservedAt, add)
		switch r.Disposition {
		case "pending", "running", "blocked", "cancelled", "exhausted", "failed", "completed":
		default:
			add("workflow.run.disposition", d.Path, "disposition", "unsupported run disposition")
		}
		allEvidence := append(append([]contracts.EvidenceRef{}, r.CheckpointRefs...), r.OutputRefs...)
		for i, e := range allEvidence {
			asset(d.Path, fmt.Sprintf("evidence[%d].asset", i), e.Asset)
			validTime(d.Path, fmt.Sprintf("evidence[%d].observedAt", i), e.ObservedAt, add)
			if e.Purpose == "" {
				add("workflow.evidence.purpose", d.Path, fmt.Sprintf("evidence[%d].purpose", i), "evidence purpose is required")
			}
		}
	}
	for _, d := range resources {
		if d.Resource.Kind != "ConnectorCapability" {
			continue
		}
		v := decode(d)
		if v == nil {
			continue
		}
		c := v.(contracts.ConnectorCapability)
		seen := map[string]bool{}
		for i, o := range c.Operations {
			if seen[o.Operation] {
				add("workflow.capability.duplicate", d.Path, fmt.Sprintf("operations[%d].operation", i), "operation entries must be unique")
			}
			seen[o.Operation] = true
			if !o.Supported && (o.ConditionalWrite || o.Idempotent || o.Concurrency != "none") {
				add("workflow.capability.unsupported-guarantee", d.Path, fmt.Sprintf("operations[%d]", i), "unsupported operation cannot claim guarantees")
			}
			if o.Concurrency != "none" && o.Concurrency != "conditional" && o.Concurrency != "serialized" {
				add("workflow.capability.concurrency", d.Path, fmt.Sprintf("operations[%d].concurrency", i), "concurrency must be none, conditional, or serialized")
			}
			if o.ConditionalWrite && o.Concurrency == "none" {
				add("workflow.capability.guarantee-conflict", d.Path, fmt.Sprintf("operations[%d]", i), "conditional write support requires conditional or serialized concurrency")
			}
			if o.Concurrency == "conditional" && !o.ConditionalWrite {
				add("workflow.capability.guarantee-conflict", d.Path, fmt.Sprintf("operations[%d]", i), "conditional concurrency requires conditional write support")
			}
		}
	}
	// Operations are inert declarations; validate scope, digest, action support, expected revision, and replay identity.
	for _, d := range resources {
		if d.Resource.Kind != "SyncOperation" {
			continue
		}
		v := decode(d)
		if v == nil {
			continue
		}
		o := v.(contracts.SyncOperation)
		wd, wok := lookup(d.Path, "workItemBindingRef", o.WorkItemBindingRef, "WorkItemBinding")
		cd, cok := lookup(d.Path, "connectorCapabilityRef", o.ConnectorCapabilityRef, "ConnectorCapability")
		if !wok || !cok {
			continue
		}
		wv := decode(wd)
		cv := decode(cd)
		if wv == nil || cv == nil {
			continue
		}
		w := wv.(contracts.WorkItemBinding)
		c := cv.(contracts.ConnectorCapability)
		if c.Provider != w.Identity.Provider || c.Scope != w.Identity {
			add("workflow.operation.scope", d.Path, "connectorCapabilityRef", "capability provider and scope must match bound item")
		}
		if o.ExpectedRevision != w.ObservedSourceRevision {
			add("workflow.operation.revision", d.Path, "expectedRevision", "expected revision must match captured binding revision")
		}
		supported := false
		conditional := false
		for _, cap := range c.Operations {
			if cap.Operation == o.OperationKind && cap.Supported {
				supported = true
				conditional = cap.ConditionalWrite
			}
		}
		if !supported {
			add("workflow.operation.unsupported", d.Path, "operationKind", "connector capability does not support operation")
		}
		if (o.OperationKind == "update-fields" || o.OperationKind == "transition") && supported && !conditional {
			add("workflow.operation.conditional-write", d.Path, "operationKind", "operation requires conditional write support")
		}
		for i, f := range o.Change.Fields {
			supportedField := false
			for _, m := range w.FieldMappings {
				if m.ProviderFieldID == f.ProviderFieldID && m.Supported {
					supportedField = true
				}
			}
			if !supportedField {
				add("workflow.operation.field", d.Path, fmt.Sprintf("change.fields[%d].providerFieldId", i), "field change is unmapped or unsupported")
			}
			if !scalarOrScalarArray(f.Value) {
				add("workflow.operation.value", d.Path, fmt.Sprintf("change.fields[%d].value", i), "field value must be a scalar or array of scalars")
			}
		}
		if o.Change.Comment != nil {
			asset(d.Path, "change.comment", *o.Change.Comment)
		}
		if err := validatePayload(o); err != nil {
			add("workflow.operation.payload", d.Path, "change", err.Error())
		}
		digest, err := contracts.OperationDigest(o.WorkItemBindingRef, w.Identity, o.ConnectorCapabilityRef, o.OperationKind, o.ExpectedRevision, o.Change)
		if err != nil {
			add("workflow.operation.digest", d.Path, "change", err.Error())
		} else if digest != o.OperationDigest {
			add("workflow.operation.digest-mismatch", d.Path, "operationDigest", "operation digest does not match canonical operation")
		}
		if o.Authority.Scope != w.Identity || o.Authority.Operation != o.OperationKind || o.Authority.OperationDigest != o.OperationDigest {
			add("workflow.operation.authority-mismatch", d.Path, "authority", "authority declaration must match operation digest, action, and scope")
		}
		if o.Authority.Reference == "" {
			add("workflow.operation.authority-required", d.Path, "authority.reference", "external authority reference is required")
		}
		logical := operationIdentity{adapter: c.AdapterID, tenant: w.Identity.TenantID, operationID: o.OperationID}
		if old, ok := operations[logical]; ok && old != o.OperationDigest {
			add("workflow.operation.replay-conflict", d.Path, "operationId", "scoped operation ID was reused with a different digest")
		} else {
			operations[logical] = o.OperationDigest
		}
	}
	for _, d := range resources {
		if d.Resource.Kind != "SyncReceipt" {
			continue
		}
		v := decode(d)
		if v == nil {
			continue
		}
		r := v.(contracts.SyncReceipt)
		if receiptIDs[r.ReceiptID] {
			add("workflow.receipt.duplicate", d.Path, "receiptId", "receipt ID must be unique")
		}
		receiptIDs[r.ReceiptID] = true
		od, ok := lookup(d.Path, "syncOperationRef", r.SyncOperationRef, "SyncOperation")
		if !ok {
			continue
		}
		ov := decode(od)
		if ov == nil {
			continue
		}
		o := ov.(contracts.SyncOperation)
		if r.OperationID != o.OperationID || r.OperationDigest != o.OperationDigest {
			add("workflow.receipt.correlation", d.Path, "operationDigest", "receipt must exactly correlate to operation ID and digest")
		}
		validTime(d.Path, "observedAt", r.ObservedAt, add)
		for i, e := range r.DispatchEvidence {
			asset(d.Path, fmt.Sprintf("dispatchEvidence[%d].asset", i), e.Asset)
			validTime(d.Path, fmt.Sprintf("dispatchEvidence[%d].observedAt", i), e.ObservedAt, add)
		}
		for i, e := range r.ReadbackEvidence {
			if e.Purpose != "readback" {
				add("workflow.receipt.readback-purpose", d.Path, fmt.Sprintf("readbackEvidence[%d].purpose", i), "readback evidence purpose must be readback")
			}
			asset(d.Path, fmt.Sprintf("readbackEvidence[%d].asset", i), e.Asset)
			validTime(d.Path, fmt.Sprintf("readbackEvidence[%d].observedAt", i), e.ObservedAt, add)
		}
		switch r.Disposition {
		case "applied":
			for _, e := range r.DispatchEvidence {
				if e.Purpose != "dispatch" {
					add("workflow.receipt.dispatch-purpose", d.Path, "dispatchEvidence", "applied receipt dispatch evidence purpose must be dispatch")
				}
			}
			if len(r.DispatchEvidence) == 0 || len(r.ReadbackEvidence) == 0 {
				add("workflow.receipt.evidence", d.Path, "disposition", "applied requires dispatch and matching readback evidence")
			} else {
				if bd, ok := lookup(od.Path, "workItemBindingRef", o.WorkItemBindingRef, "WorkItemBinding"); ok {
					if bv := decode(bd); bv != nil {
						for _, evidence := range r.ReadbackEvidence {
							verifyReadback(od.Path, d.Path, o, bv.(contracts.WorkItemBinding).Identity, r.Disposition, evidence, asset, add)
						}
					}
				}
			}
		case "not-applied":
			if len(r.ReadbackEvidence) == 0 {
				add("workflow.receipt.evidence", d.Path, "readbackEvidence", "not-applied requires readback evidence")
			} else {
				if bd, ok := lookup(od.Path, "workItemBindingRef", o.WorkItemBindingRef, "WorkItemBinding"); ok {
					if bv := decode(bd); bv != nil {
						for _, evidence := range r.ReadbackEvidence {
							verifyReadback(od.Path, d.Path, o, bv.(contracts.WorkItemBinding).Identity, r.Disposition, evidence, asset, add)
						}
					}
				}
			}
		case "conflict", "rejected":
			if len(r.DispatchEvidence) == 0 && len(r.ReadbackEvidence) == 0 {
				add("workflow.receipt.evidence", d.Path, "disposition", "conflict and rejected require diagnostic evidence")
			}
			for _, e := range append(append([]contracts.EvidenceRef{}, r.DispatchEvidence...), r.ReadbackEvidence...) {
				if e.Purpose != "diagnostic" {
					add("workflow.receipt.diagnostic-purpose", d.Path, "disposition", "conflict and rejected evidence purpose must be diagnostic")
				}
			}
		case "unknown":
			if len(r.ReadbackEvidence) != 0 {
				add("workflow.receipt.unknown-readback", d.Path, "readbackEvidence", "unknown receipts may retain dispatch evidence only")
			}
			for _, e := range r.DispatchEvidence {
				if e.Purpose != "dispatch" {
					add("workflow.receipt.dispatch-purpose", d.Path, "dispatchEvidence", "unknown receipt dispatch evidence purpose must be dispatch")
				}
			}
		default:
			add("workflow.receipt.disposition", d.Path, "disposition", "unsupported receipt disposition")
		}
	}
	sort.Slice(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		return a.Message < b.Message
	})
	return ds
}

func validTime(source, field, value string, add func(string, string, string, string)) {
	if _, err := time.Parse(time.RFC3339, value); err != nil {
		add("workflow.time", source, field, "time must be RFC3339")
	}
}
func scalarOrScalarArray(raw json.RawMessage) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return false
	}
	switch x := v.(type) {
	case nil, bool, string, json.Number:
		return true
	case []any:
		for _, e := range x {
			switch e.(type) {
			case nil, bool, string, json.Number:
			default:
				return false
			}
		}
		return true
	}
	return false
}
func validatePayload(o contracts.SyncOperation) error {
	switch o.OperationKind {
	case "update-fields":
		if len(o.Change.Fields) == 0 || o.Change.TransitionID != "" || o.Change.Comment != nil || o.Change.RelationType != "" || o.Change.RelationIdentity != nil {
			return fmt.Errorf("update-fields requires fields only")
		}
	case "transition":
		if o.Change.TransitionID == "" || len(o.Change.Fields) != 0 || o.Change.Comment != nil || o.Change.RelationType != "" || o.Change.RelationIdentity != nil {
			return fmt.Errorf("transition requires transitionId only")
		}
	case "comment":
		if o.Change.Comment == nil || len(o.Change.Fields) != 0 || o.Change.TransitionID != "" || o.Change.RelationType != "" || o.Change.RelationIdentity != nil {
			return fmt.Errorf("comment requires a comment asset only")
		}
	case "link":
		if o.Change.RelationType == "" || o.Change.RelationIdentity == nil || len(o.Change.Fields) != 0 || o.Change.TransitionID != "" || o.Change.Comment != nil {
			return fmt.Errorf("link requires relationType and relationIdentity only")
		}
	default:
		return fmt.Errorf("unsupported operation kind %q", o.OperationKind)
	}
	if o.OperationKind == "update-fields" {
		seen := map[string]bool{}
		for _, f := range o.Change.Fields {
			if seen[f.ProviderFieldID] {
				return fmt.Errorf("duplicate provider field ID %q", f.ProviderFieldID)
			}
			seen[f.ProviderFieldID] = true
		}
	}
	return nil
}
func verifyReadback(operationSource, source string, o contracts.SyncOperation, expectedIdentity contracts.RemoteIdentity, disposition string, e contracts.EvidenceRef, asset func(string, string, contracts.AssetRef) []byte, add func(string, string, string, string)) {
	if e.Purpose != "readback" {
		add("workflow.receipt.readback-purpose", source, "readbackEvidence", "readback evidence purpose must be readback")
	}
	b := asset(source, "readbackEvidence.asset", e.Asset)
	if len(b) == 0 {
		add("workflow.receipt.readback-empty", source, "readbackEvidence", "readback evidence must contain a structured nonempty claim")
		return
	}
	if err := validateUniqueJSON(b); err != nil {
		add("workflow.receipt.readback-json", source, "readbackEvidence", err.Error())
		return
	}
	var claim struct {
		OperationDigest  string                     `json:"operationDigest"`
		Identity         contracts.RemoteIdentity   `json:"identity"`
		Revision         string                     `json:"revision"`
		Applied          *bool                      `json:"applied"`
		Fields           map[string]json.RawMessage `json:"fields"`
		TransitionID     string                     `json:"transitionId"`
		CommentDigest    string                     `json:"commentDigest"`
		RelationType     string                     `json:"relationType"`
		RelationIdentity *contracts.RemoteIdentity  `json:"relationIdentity"`
	}
	if err := json.Unmarshal(b, &claim); err != nil && err != io.EOF {
		add("workflow.receipt.readback", source, "readbackEvidence", "readback evidence must be structured JSON")
		return
	}
	if claim.OperationDigest != o.OperationDigest {
		add("workflow.receipt.readback-digest", source, "readbackEvidence", "readback claim operation digest does not match")
	}
	if claim.Identity != expectedIdentity {
		add("workflow.receipt.readback-identity", source, "readbackEvidence", "readback identity does not match bound remote item")
	}
	if claim.Revision == "" {
		add("workflow.receipt.readback-revision", source, "readbackEvidence", "readback must include the observed remote revision")
	}
	if disposition == "applied" && (claim.Applied == nil || !*claim.Applied) {
		add("workflow.receipt.readback-outcome", source, "readbackEvidence", "applied receipt requires readback that confirms the change")
	}
	if disposition == "not-applied" && (claim.Applied == nil || *claim.Applied) {
		add("workflow.receipt.readback-outcome", source, "readbackEvidence", "not-applied receipt requires readback that confirms no change")
	}
	if disposition == "applied" && o.OperationKind == "update-fields" {
		for _, f := range o.Change.Fields {
			got, ok := claim.Fields[f.ProviderFieldID]
			if !ok || !jsonEqual(got, f.Value) {
				add("workflow.receipt.readback-fields", source, "readbackEvidence", "readback does not match desired field values")
				break
			}
		}
	}
	if disposition == "applied" && o.OperationKind == "transition" && claim.TransitionID != o.Change.TransitionID {
		add("workflow.receipt.readback-transition", source, "readbackEvidence", "readback transition does not match desired transition")
	}
	if disposition == "applied" && o.OperationKind == "link" && (claim.RelationType != o.Change.RelationType || claim.RelationIdentity == nil || o.Change.RelationIdentity == nil || *claim.RelationIdentity != *o.Change.RelationIdentity) {
		add("workflow.receipt.readback-link", source, "readbackEvidence", "readback relation does not match desired relation")
	}
	if disposition == "applied" && o.OperationKind == "comment" && o.Change.Comment != nil {
		bb := asset(operationSource, "change.comment", *o.Change.Comment)
		sum := sha256.Sum256(bb)
		if claim.CommentDigest != hex.EncodeToString(sum[:]) {
			add("workflow.receipt.readback-comment", source, "readbackEvidence", "readback comment digest does not match")
		}
	}
}
func jsonEqual(a, b json.RawMessage) bool {
	var x, y any
	dx := json.NewDecoder(bytes.NewReader(a))
	dx.UseNumber()
	dy := json.NewDecoder(bytes.NewReader(b))
	dy.UseNumber()
	if dx.Decode(&x) != nil || dy.Decode(&y) != nil {
		return false
	}
	aa, _ := json.Marshal(x)
	bb, _ := json.Marshal(y)
	return bytes.Equal(aa, bb)
}

func validateUniqueJSON(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var value func(int) error
	value = func(depth int) error {
		if depth > contracts.MaxDepth {
			return fmt.Errorf("readback JSON exceeds nesting limit")
		}
		tok, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := tok.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				keyToken, err := d.Token()
				if err != nil {
					return err
				}
				k, ok := keyToken.(string)
				if !ok {
					return fmt.Errorf("readback object key is not a string")
				}
				if seen[k] {
					return fmt.Errorf("duplicate readback JSON key %q", k)
				}
				seen[k] = true
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return fmt.Errorf("malformed readback object")
			}
		case '[':
			for d.More() {
				if err := value(depth + 1); err != nil {
					return err
				}
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return fmt.Errorf("malformed readback array")
			}
		}
		return nil
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("readback contains trailing JSON data")
	}
	return nil
}
