package tracker

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"time"
)

func ReadDesired(path string) (Desired, error) {
	var d Desired
	b, e := ReadFile(path)
	if e != nil {
		return d, e
	}
	if e = Decode(b, "desired", &d); e != nil {
		return d, e
	}
	if d.Version != Version && d.Version != Version2 {
		return d, fmt.Errorf("unsupported desired version")
	}
	return d, nil
}
func PlanUpdate(c *Capture, m Mapping, d Desired, asOf time.Time) (TrackerUpdatePlan, error) {
	p := TrackerUpdatePlan{Version: d.Version, Identity: d.Identity, AsOf: asOf.UTC().Format(time.RFC3339Nano), ExpectedRevision: d.ExpectedRevision, ConditionalWrite: "unverified-read-only", Changes: d, Prerequisites: []string{"P7/P8 authorization and fresh read before dispatch", "verify provider-specific conditional update support", "readback and reconcile; native completion is not acceptance"}, Report: BaseReport()}
	p.Report.Version = d.Version
	if !ValidCapture(c) {
		return p, fmt.Errorf("unvalidated or mutated capture")
	}
	if asOf.IsZero() {
		return p, fmt.Errorf("as-of required")
	}
	if e := validateMapping(m); e != nil {
		return p, e
	}
	if d.Version != c.Snapshot.Version || m.Version != c.Snapshot.Version || m.Provider != c.Snapshot.Source.Provider || m.MappingVersion != c.Snapshot.Source.MappingVersion {
		return p, fmt.Errorf("desired/mapping/source mismatch")
	}
	p.SnapshotSHA256 = c.Snapshot.Digest
	p.MappingSHA256 = Digest(m)
	p.DesiredSHA256 = Digest(d)
	block := func(code, field, msg string) {
		p.Report.Status = "blocked"
		p.Report.Diagnostics = append(p.Report.Diagnostics, Diagnostic{code, field, msg})
	}
	observed, e := time.Parse(time.RFC3339Nano, c.Snapshot.ObservedAt)
	if e != nil || asOf.Before(observed) || asOf.Sub(observed) >= 5*time.Minute {
		block("refresh-needed", "snapshot.observedAt", "obtain fresh observations before preparing a current update draft")
	}
	if !c.Snapshot.Complete {
		block("incomplete", "snapshot", "complete requested reads before planning changes")
	}
	var item *Item
	for _, i := range c.Snapshot.Items {
		if Digest(i.Identity) == Digest(d.Identity) {
			v := i
			item = &v
			break
		}
	}
	if item == nil {
		block("unknown-identity", "desired.identity", "exact item identity not present in snapshot")
	} else {
		p.RevisionKind = item.RevisionKind
		if d.ExpectedRevision == "" || item.Revision == "" || d.ExpectedRevision != item.Revision {
			block("conflict", "expectedRevision", "desired plan does not match the exact observed source revision")
		}
		ownership := map[string]string{}
		for _, r := range m.Rules {
			if r.ProviderFieldID != "" {
				if old, ok := ownership[r.ProviderFieldID]; ok && old != r.Owner {
					block("ambiguous-ownership", r.ProviderFieldID, "mapping disagrees on field ownership")
				}
				ownership[r.ProviderFieldID] = r.Owner
			}
		}
		for field, raw := range d.Fields {
			owner, known := ownership[field]
			if !known {
				block("unmapped-field", field, "changes require an explicit native field mapping")
				continue
			}
			if owner == "provider" {
				block("provider-owned", field, "provider-owned fields cannot be proposed as editable values")
			}
			if owner == "human" && !has(d.HumanOwnedFields, field) {
				block("human-owned", field, "explicit human-owned-field change intent is required")
			}
			if len(raw) == 0 {
				block("invalid-value", field, "field value is absent")
			}
			if _, exists := mappedNativeField(*item, field); !exists {
				block("unknown-current-value", field, "current native field was not captured")
			}
			for _, lossy := range item.LossyFields {
				if lossy == field {
					block("lossy-conversion", field, "unsupported source content must not be overwritten")
				}
			}
			if (c.Snapshot.Source.Provider == "jira" || c.Snapshot.Source.Provider == "jira-datacenter") && (field == "status" || field == "statusId") {
				block("transition-required", field, "Jira state changes require a discovered transition ID")
			}
			if c.Snapshot.Source.Provider == "github" && (field == "merged" || field == "merged_at") {
				block("merge-separate", field, "PR merge is not a tracker field update or issue acceptance")
			}
		}
		if d.TransitionID != "" {
			found := false
			for _, t := range item.Transitions {
				if t.ID != d.TransitionID {
					continue
				}
				found = true
				if !t.RequirementsKnown {
					block("unknown-transition-fields", t.ID, "required transition fields were not established")
				}
				for _, f := range t.RequiredFields {
					if _, provided := d.Fields[f]; !provided {
						block("transition-required-field", f, "desired plan is missing a required transition input")
					}
				}
			}
			if !found {
				block("unsupported-transition", "transitionId", "transition was not discovered for this item")
			}
		}
		for _, r := range d.Relations {
			known := has(m.RelationTypes, r.Type)
			for _, existing := range item.Relations {
				if existing.Type == r.Type && existing.Supported {
					known = true
				}
			}
			if r.TargetID == "" || r.Type == "" || !known {
				block("unsupported-relation", "relations", "relation semantics need an explicit supported mapping")
			}
		}
	}
	if len(d.Fields) == 0 && d.TransitionID == "" && d.Comment == nil && len(d.Relations) == 0 {
		block("empty-change", "desired", "no explicit requested change")
	}
	if d.Comment != nil {
		if !hashPattern.MatchString(d.Comment.SHA256) || d.Comment.Path == "" {
			block("invalid-comment", "comment", "comment must be a captured hash-bound local asset")
		}
		p.Prerequisites = append(p.Prerequisites, "validate and capture comment asset bytes before write authorization")
	}
	stableDiagnostics(&p.Report)
	p.Digest = Digest(p)
	return p, nil
}
func has(a []string, v string) bool {
	for _, s := range a {
		if s == v {
			return true
		}
	}
	return false
}
func Capability(s Source) contracts.ConnectorCapability {
	ops := []contracts.OperationCapability{}
	for _, name := range []string{"update-fields", "transition", "comment", "link"} {
		ops = append(ops, contracts.OperationCapability{Operation: name, Supported: false, ConditionalWrite: false, Idempotent: false, Concurrency: "none"})
	}
	return contracts.ConnectorCapability{DocumentHeader: contracts.DocumentHeader{APIVersion: workflowVersion(s), Kind: "ConnectorCapability", Metadata: contracts.OperationalMetadata{ID: "tracker-" + s.Provider, Name: s.Provider + " read adapter", Version: AdapterVersion, Description: "Offline fixture support; live read requires separate conformance. Remote writes unavailable."}}, Provider: s.Provider, AdapterID: "aok-tracker-" + s.Provider, AdapterVersion: AdapterVersion, Scope: identity(s, "scope", "container"), Operations: ops}
}
func Inspect(c *Capture, at time.Time) (map[string]any, error) {
	if !ValidCapture(c) || at.IsZero() {
		return nil, fmt.Errorf("validated capture and as-of required")
	}
	t, e := time.Parse(time.RFC3339Nano, c.Snapshot.ObservedAt)
	freshness := "expired"
	if e == nil && !at.Before(t) && at.Sub(t) < 5*time.Minute {
		freshness = "fresh"
	}
	return map[string]any{"version": c.Snapshot.Version, "snapshot": c.Snapshot, "capability": Capability(c.Snapshot.Source), "asOf": at.UTC().Format(time.RFC3339Nano), "freshness": freshness, "authorizationGranted": false, "executionReady": false}, nil
}

var _ json.RawMessage
