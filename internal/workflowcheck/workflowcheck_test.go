package workflowcheck

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

func loadCorpus(t *testing.T) ([]Document, AssetResolver) {
	t.Helper()
	root := filepath.Join("testdata", "corpus")
	var docs []Document
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".json" || filepath.Dir(path) != root {
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		v, _, e := contracts.Parse(b)
		if e != nil {
			return e
		}
		docs = append(docs, Document{Resource: v.(contracts.Resource), Path: strings.TrimPrefix(filepath.ToSlash(path), filepath.ToSlash(root)+"/")})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := func(owner, ref string) ([]byte, error) {
		if filepath.IsAbs(ref) {
			return nil, os.ErrInvalid
		}
		clean := filepath.Clean(filepath.FromSlash(ref))
		if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return nil, os.ErrInvalid
		}
		return os.ReadFile(filepath.Join(root, clean))
	}
	return docs, resolver
}
func TestFixtureCorpusPasses(t *testing.T) {
	docs, res := loadCorpus(t)
	if got := Check(docs, res); len(got) != 0 {
		t.Fatalf("unexpected diagnostics: %+v", got)
	}
}
func TestExactReferenceAndAssignmentHashAreChecked(t *testing.T) {
	docs, res := loadCorpus(t)
	for i := range docs {
		if docs[i].Resource.Kind == "HostTaskBinding" {
			var m map[string]any
			_ = json.Unmarshal(docs[i].Resource.Document, &m)
			m["assignmentSha256"] = strings.Repeat("0", 64)
			b, _ := json.Marshal(m)
			parsed, _, err := contracts.Parse(b)
			if err != nil {
				t.Fatal(err)
			}
			docs[i].Resource = parsed.(contracts.Resource)
			break
		}
	}
	got := Check(docs, res)
	if !hasCode(got, "workflow.assignment.hash-mismatch") {
		t.Fatalf("expected assignment hash diagnostic, got %+v", got)
	}
}
func TestOperationDigestAndReadbackAreChecked(t *testing.T) {
	docs, res := loadCorpus(t)
	for i := range docs {
		if docs[i].Resource.Kind == "SyncOperation" {
			var m map[string]any
			_ = json.Unmarshal(docs[i].Resource.Document, &m)
			m["operationDigest"] = strings.Repeat("0", 64)
			auth := m["authority"].(map[string]any)
			auth["operationDigest"] = strings.Repeat("0", 64)
			b, _ := json.Marshal(m)
			docs[i].Resource.Document = b
			break
		}
	}
	got := Check(docs, res)
	if !hasCode(got, "workflow.operation.digest-mismatch") {
		t.Fatalf("expected digest mismatch, got %+v", got)
	}
}
func TestBindingMetadataIDCannotChangeRemoteIdentityAcrossVersions(t *testing.T) {
	docs, res := loadCorpus(t)
	for _, d := range docs {
		if d.Resource.Kind != "WorkItemBinding" {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal(d.Resource.Document, &m)
		m["metadata"].(map[string]any)["version"] = "0.2.0"
		m["identity"].(map[string]any)["remoteId"] = "issue-999"
		b, _ := json.Marshal(m)
		r, _, err := contracts.Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, Document{Resource: r.(contracts.Resource), Path: "binding-v2.json"})
		break
	}
	if got := Check(docs, res); !hasCode(got, "workflow.identity.binding-mutated") {
		t.Fatalf("expected immutable binding identity diagnostic, got %+v", got)
	}
}
func TestReadbackMustMatchRemoteIdentityAndAppliedOutcome(t *testing.T) {
	docs, res := loadCorpus(t)
	mutated := func(owner, ref string) ([]byte, error) {
		b, e := res(owner, ref)
		if e != nil {
			return nil, e
		}
		if ref == "assets/readback.json" {
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			m["identity"].(map[string]any)["remoteId"] = "different"
			m["applied"] = false
			return json.Marshal(m)
		}
		return b, nil
	}
	got := Check(docs, mutated)
	if !hasCode(got, "workflow.receipt.readback-identity") || !hasCode(got, "workflow.receipt.readback-outcome") {
		t.Fatalf("expected readback identity and outcome diagnostics, got %+v", got)
	}
}
func TestDuplicateCapabilityOperationsAndWrongPinFail(t *testing.T) {
	docs, res := loadCorpus(t)
	for i := range docs {
		switch docs[i].Resource.Kind {
		case "ConnectorCapability":
			var m map[string]any
			_ = json.Unmarshal(docs[i].Resource.Document, &m)
			ops := m["operations"].([]any)
			m["operations"] = append(ops, ops[0])
			b, _ := json.Marshal(m)
			r, _, e := contracts.Parse(b)
			if e != nil {
				t.Fatal(e)
			}
			docs[i].Resource = r.(contracts.Resource)
		case "HostTaskBinding":
			if docs[i].Resource.Metadata.ID == "host-attempt-1" {
				var m map[string]any
				_ = json.Unmarshal(docs[i].Resource.Document, &m)
				m["workItemRef"].(map[string]any)["version"] = "9.9.9"
				b, _ := json.Marshal(m)
				r, _, e := contracts.Parse(b)
				if e != nil {
					t.Fatal(e)
				}
				docs[i].Resource = r.(contracts.Resource)
			}
		}
	}
	got := Check(docs, res)
	if !hasCode(got, "workflow.capability.duplicate") || !hasCode(got, "workflow.reference.missing") {
		t.Fatalf("expected duplicate operation and wrong pin diagnostics, got %+v", got)
	}
}
func TestAssetHashDriftFails(t *testing.T) {
	docs, res := loadCorpus(t)
	drift := func(owner, ref string) ([]byte, error) {
		b, e := res(owner, ref)
		if e == nil && ref == "assets/task.json" {
			b = append(b, []byte("changed")...)
		}
		return b, e
	}
	if got := Check(docs, drift); !hasCode(got, "workflow.asset.hash-mismatch") {
		t.Fatalf("expected asset hash mismatch, got %+v", got)
	}
}
func TestEmptyReadbackWithValidHashFails(t *testing.T) {
	docs, res := loadCorpus(t)
	emptyHash := sha256.Sum256(nil)
	for i := range docs {
		if docs[i].Resource.Kind == "SyncReceipt" {
			var m map[string]any
			_ = json.Unmarshal(docs[i].Resource.Document, &m)
			ev := m["readbackEvidence"].([]any)[0].(map[string]any)
			asset := ev["asset"].(map[string]any)
			asset["path"] = "assets/empty.json"
			asset["sha256"] = fmt.Sprintf("%x", emptyHash)
			b, _ := json.Marshal(m)
			r, _, e := contracts.Parse(b)
			if e != nil {
				t.Fatal(e)
			}
			docs[i].Resource = r.(contracts.Resource)
		}
	}
	resolver := func(owner, ref string) ([]byte, error) {
		if ref == "assets/empty.json" {
			return []byte{}, nil
		}
		return res(owner, ref)
	}
	if got := Check(docs, resolver); !hasCode(got, "workflow.receipt.readback-empty") {
		t.Fatalf("expected empty readback failure, got %+v", got)
	}
}
func TestEveryReadbackMustAgree(t *testing.T) {
	docs, res := loadCorpus(t)
	bad := []byte(`{"operationDigest":"wrong","identity":{"provider":"github","tenantId":"org","containerId":"repo","remoteId":"issue-17","itemKind":"issue"},"revision":"r9","applied":false}`)
	sum := sha256.Sum256(bad)
	for i := range docs {
		if docs[i].Resource.Kind == "SyncReceipt" {
			var m map[string]any
			_ = json.Unmarshal(docs[i].Resource.Document, &m)
			first := m["readbackEvidence"].([]any)[0]
			second := map[string]any{}
			raw, _ := json.Marshal(first)
			_ = json.Unmarshal(raw, &second)
			a := second["asset"].(map[string]any)
			a["path"] = "assets/contradictory.json"
			a["sha256"] = fmt.Sprintf("%x", sum)
			m["readbackEvidence"] = append(m["readbackEvidence"].([]any), second)
			b, _ := json.Marshal(m)
			r, _, e := contracts.Parse(b)
			if e != nil {
				t.Fatal(e)
			}
			docs[i].Resource = r.(contracts.Resource)
		}
	}
	resolver := func(owner, ref string) ([]byte, error) {
		if ref == "assets/contradictory.json" {
			return bad, nil
		}
		return res(owner, ref)
	}
	got := Check(docs, resolver)
	if !hasCode(got, "workflow.receipt.readback-digest") {
		t.Fatalf("expected second readback contradiction to fail, got %+v", got)
	}
}
func TestDuplicateKeysInReadbackFail(t *testing.T) {
	docs, res := loadCorpus(t)
	bad := []byte(`{"operationDigest":"x","operationDigest":"y"}`)
	sum := sha256.Sum256(bad)
	for i := range docs {
		if docs[i].Resource.Kind == "SyncReceipt" {
			var m map[string]any
			_ = json.Unmarshal(docs[i].Resource.Document, &m)
			ev := m["readbackEvidence"].([]any)[0].(map[string]any)
			a := ev["asset"].(map[string]any)
			a["path"] = "assets/duplicate-keys.json"
			a["sha256"] = fmt.Sprintf("%x", sum)
			b, _ := json.Marshal(m)
			r, _, e := contracts.Parse(b)
			if e != nil {
				t.Fatal(e)
			}
			docs[i].Resource = r.(contracts.Resource)
		}
	}
	resolver := func(owner, ref string) ([]byte, error) {
		if ref == "assets/duplicate-keys.json" {
			return bad, nil
		}
		return res(owner, ref)
	}
	if got := Check(docs, resolver); !hasCode(got, "workflow.receipt.readback-json") {
		t.Fatalf("expected duplicate readback key failure, got %+v", got)
	}
}
func TestNestedCommentAssetUsesOperationOwner(t *testing.T) {
	comment := []byte("comment body")
	sum := sha256.Sum256(comment)
	ref := contracts.AssetRef{Path: "assets/comment.md", SHA256: fmt.Sprintf("%x", sum)}
	operation := contracts.SyncOperation{OperationKind: "comment", OperationDigest: "digest", Change: contracts.SyncChange{Comment: &ref}}
	identity := contracts.RemoteIdentity{Provider: "github", TenantID: "org", ContainerID: "repo", RemoteID: "issue-17", ItemKind: "issue"}
	seen := map[string]bool{}
	resolver := func(owner, path string) ([]byte, error) {
		seen[owner+"|"+path] = true
		if path == "assets/readback.json" {
			return []byte(`{"operationDigest":"digest","identity":{"provider":"github","tenantId":"org","containerId":"repo","remoteId":"issue-17","itemKind":"issue"},"revision":"r1","applied":true,"commentDigest":"` + fmt.Sprintf("%x", sum) + `"}`), nil
		}
		return comment, nil
	}
	e := contracts.EvidenceRef{Asset: contracts.AssetRef{Path: "assets/readback.json", SHA256: "unused"}, Purpose: "readback", ObservedAt: "2026-10-23T10:00:00Z"}
	// The callback deliberately returns structured evidence independently of its declared hash.
	asset := func(owner, field string, r contracts.AssetRef) []byte {
		seen[owner+"|"+r.Path] = true
		if r.Path == "assets/readback.json" {
			b, _ := resolver(owner, r.Path)
			return b
		}
		return comment
	}
	verifyReadback("nested/operation.json", "nested/receipt.json", operation, identity, "applied", e, asset, func(code, source, field, message string) {})
	if !seen["nested/operation.json|assets/comment.md"] {
		t.Fatalf("comment asset was resolved relative to receipt instead of operation: %#v", seen)
	}
}
func TestAttemptIdentityConflict(t *testing.T) {
	docs, res := loadCorpus(t)
	var w contracts.WorkItemBinding
	for _, d := range docs {
		if d.Resource.Kind == "WorkItemBinding" {
			v, _ := contracts.DecodeTyped(d.Resource)
			w = v.(contracts.WorkItemBinding)
		}
	}
	w.DocumentHeader.Metadata.ID = "other-item"
	w.Identity.RemoteID = "issue-18"
	w.DocumentHeader.Metadata.Name = "other-item"
	wb, _ := json.Marshal(w)
	wr, _, err := contracts.Parse(wb)
	if err != nil {
		t.Fatal(err)
	}
	docs = append(docs, Document{Resource: wr.(contracts.Resource), Path: "other-binding.json"})
	for i := range docs {
		if docs[i].Resource.Kind == "HostTaskBinding" && docs[i].Resource.Metadata.ID == "host-attempt-2" {
			var m map[string]any
			_ = json.Unmarshal(docs[i].Resource.Document, &m)
			m["workItemRef"].(map[string]any)["id"] = "other-item"
			m["sessionId"] = "sess-1"
			m["attemptId"] = "attempt-1"
			b, _ := json.Marshal(m)
			rr, _, e := contracts.Parse(b)
			if e != nil {
				t.Fatal(e)
			}
			docs[i].Resource = rr.(contracts.Resource)
		}
	}
	if got := Check(docs, res); !hasCode(got, "workflow.attempt.conflict") {
		t.Fatalf("expected attempt conflict, got %+v", got)
	}
}
func TestOpaqueNULComponentsDoNotCollideInAttemptIdentity(t *testing.T) {
	docs, res := loadCorpus(t)
	var binding map[string]any
	for _, d := range docs {
		if d.Resource.Kind == "WorkItemBinding" {
			_ = json.Unmarshal(d.Resource.Document, &binding)
			break
		}
	}
	binding["metadata"].(map[string]any)["id"] = "other-item"
	binding["identity"].(map[string]any)["remoteId"] = "issue-18"
	raw, _ := json.Marshal(binding)
	parsed, _, err := contracts.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	docs = append(docs, Document{Resource: parsed.(contracts.Resource), Path: "other-binding.json"})
	for i := range docs {
		if docs[i].Resource.Kind != "HostTaskBinding" {
			continue
		}
		var m map[string]any
		_ = json.Unmarshal(docs[i].Resource.Document, &m)
		if docs[i].Resource.Metadata.ID == "host-attempt-1" {
			m["host"] = "x\u0000y"
			m["sessionId"] = "z"
			m["attemptId"] = "q"
		} else if docs[i].Resource.Metadata.ID == "host-attempt-2" {
			m["host"] = "x"
			m["sessionId"] = "y\u0000z"
			m["attemptId"] = "q"
			m["workItemRef"].(map[string]any)["id"] = "other-item"
		}
		b, _ := json.Marshal(m)
		r, _, e := contracts.Parse(b)
		if e != nil {
			t.Fatal(e)
		}
		docs[i].Resource = r.(contracts.Resource)
	}
	if got := Check(docs, res); hasCode(got, "workflow.attempt.conflict") {
		t.Fatalf("distinct opaque identity tuples collided: %+v", got)
	}
}
func TestOpaqueConnectorTenantOperationTupleIsStructural(t *testing.T) {
	a := operationIdentity{adapter: "connector\x00tenant", tenant: "scope", operationID: "op"}
	b := operationIdentity{adapter: "connector", tenant: "tenant\x00scope", operationID: "op"}
	legacyA := a.adapter + "\x00" + a.tenant + "\x00" + a.operationID
	legacyB := b.adapter + "\x00" + b.tenant + "\x00" + b.operationID
	if legacyA != legacyB {
		t.Fatalf("test tuples must collide under delimiter encoding: %q != %q", legacyA, legacyB)
	}
	seen := map[operationIdentity]bool{a: true, b: true}
	if len(seen) != 2 {
		t.Fatalf("structural connector/tenant/operation key collided: %#v", seen)
	}
}
func TestRemainingReferenceFenceIdentityAndReceiptGates(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(*testing.T, []Document) []Document
	}{
		{"plan step", "workflow.plan.step-missing", func(t *testing.T, ds []Document) []Document {
			return mutateOne(t, ds, "HostTaskBinding", "host-attempt-1", func(m map[string]any) { m["planStepRef"].(map[string]any)["stepId"] = "missing" })
		}},
		{"profile", "workflow.run.profile-mismatch", func(t *testing.T, ds []Document) []Document {
			return mutateOne(t, ds, "RunRecord", "run-1", func(m map[string]any) { m["executionProfileRef"].(map[string]any)["kind"] = "AgentAssignment" })
		}},
		{"policy ref", "workflow.reference.missing", func(t *testing.T, ds []Document) []Document {
			return mutateOne(t, ds, "RunRecord", "run-1", func(m map[string]any) {
				m["policyRefs"] = []any{map[string]any{"kind": "ContextPolicy", "id": "missing", "version": "1.0.0"}}
			})
		}},
		{"fence pair", "workflow.run.fence-pair", func(t *testing.T, ds []Document) []Document {
			return mutateOne(t, ds, "RunRecord", "run-1", func(m map[string]any) { m["leaseId"] = "lease-1" })
		}},
		{"distinct binding IDs", "workflow.identity.binding-conflict", func(t *testing.T, ds []Document) []Document {
			for _, d := range ds {
				if d.Resource.Kind == "WorkItemBinding" {
					var m map[string]any
					_ = json.Unmarshal(d.Resource.Document, &m)
					m["metadata"].(map[string]any)["id"] = "alternate-binding"
					b, _ := json.Marshal(m)
					r, _, e := contracts.Parse(b)
					if e != nil {
						t.Fatal(e)
					}
					return append(ds, Document{Resource: r.(contracts.Resource), Path: "alternate-binding.json"})
				}
			}
			return ds
		}},
		{"duplicate receipts", "workflow.receipt.duplicate", func(t *testing.T, ds []Document) []Document {
			for _, d := range ds {
				if d.Resource.Kind == "SyncReceipt" {
					var m map[string]any
					_ = json.Unmarshal(d.Resource.Document, &m)
					m["metadata"].(map[string]any)["id"] = "another-receipt"
					b, _ := json.Marshal(m)
					r, _, e := contracts.Parse(b)
					if e != nil {
						t.Fatal(e)
					}
					return append(ds, Document{Resource: r.(contracts.Resource), Path: "another-receipt.json"})
				}
			}
			return ds
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ds, res := loadCorpus(t)
			ds = tt.mutate(t, ds)
			if got := Check(ds, res); !hasCode(got, tt.code) {
				t.Fatalf("expected %s, got %+v", tt.code, got)
			}
		})
	}
}
func TestFenceSchemaMaximumPassesSemanticCheck(t *testing.T) {
	docs, res := loadCorpus(t)
	docs = mutateOne(t, docs, "RunRecord", "run-1", func(m map[string]any) { m["leaseId"] = "lease-1"; m["fence"] = int64(2147483647) })
	if got := Check(docs, res); hasCode(got, "workflow.run.fence") {
		t.Fatalf("schema-maximum fence should pass semantic bounds: %+v", got)
	}
}
func mutateOne(t *testing.T, docs []Document, kind, id string, fn func(map[string]any)) []Document {
	t.Helper()
	for i := range docs {
		if docs[i].Resource.Kind == kind && docs[i].Resource.Metadata.ID == id {
			var m map[string]any
			if e := json.Unmarshal(docs[i].Resource.Document, &m); e != nil {
				t.Fatal(e)
			}
			fn(m)
			b, e := json.Marshal(m)
			if e != nil {
				t.Fatal(e)
			}
			r, _, e := contracts.Parse(b)
			if e != nil {
				t.Fatal(e)
			}
			docs[i].Resource = r.(contracts.Resource)
			return docs
		}
	}
	t.Fatalf("missing %s %s", kind, id)
	return docs
}
func hasCode(ds []contracts.Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}
