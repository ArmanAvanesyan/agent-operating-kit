package milestones

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/deliverybridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

type projectionOK struct{ err error }

func (p projectionOK) VerifyCurrentProjection(ArtifactRef, verification.Candidate, verification.AcceptanceReceipt, time.Time) error {
	return p.err
}

func protectedTarget() Destination {
	return Destination{ProviderOrigin: "https://tracker.example", Scope: contracts.RemoteIdentity{Provider: "github", TenantID: "org", ContainerID: "repo", RemoteID: "42", ItemKind: "issue"}, Operation: "update-fields", OperationID: "op-1", OperationDigest: strings.Repeat("a", 64), API: "v1", AuthRef: "credential-1", Principal: "operator", DeliveryMilestone: "merge"}
}
func bindOperation(tgt Destination) (contracts.SyncOperation, writeback.Grant) {
	const grantID = "grant-1"
	op := contracts.SyncOperation{OperationID: tgt.OperationID, OperationDigest: tgt.OperationDigest, OperationKind: tgt.Operation, Authority: contracts.ExternalAuthorityRef{Reference: grantID, Scope: tgt.Scope, Operation: tgt.Operation, OperationDigest: tgt.OperationDigest}}
	g := writeback.Grant{ID: grantID, Scope: tgt.Scope, ProviderOrigin: tgt.ProviderOrigin, AuthRef: tgt.AuthRef, Principal: tgt.Principal, Operation: tgt.Operation, OperationID: tgt.OperationID, OperationDigest: tgt.OperationDigest}
	return op, g
}
func boundVerifier(t *testing.T, r *Resolver, tgt Destination) writeback.EvidenceVerifier {
	t.Helper()
	op, g := bindOperation(tgt)
	v, e := r.EvidenceVerifierFor(op, g)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestAcceptanceEvidenceRevalidatedAgainstExactScope(t *testing.T) {
	root := t.TempDir()
	bundleDir := filepath.Join(root, "candidate")
	src := filepath.Join(root, "src")
	_ = os.Mkdir(src, 0700)
	_ = os.WriteFile(filepath.Join(src, "result"), []byte("ok"), 0600)
	sha := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "trusted-policy", MaxAgeSeconds: 600, Criteria: []verification.Criterion{{ID: "present", Kind: "asset-present", Required: true, Expected: "result"}}, Issuers: []verification.TrustedIssuer{{ID: "acceptor", Role: "acceptance", PublicKey: base64.StdEncoding.EncodeToString(pub)}}}
	m := verification.CandidateManifest{Version: verification.Version, ID: "cand", Origin: "manual", Repository: "org/repo", BaseRevision: "base", SourceRevision: "head", RunID: "run", StepID: "step", AttemptID: "attempt", Fence: 3, InputSHA256: sha([]byte("input")), ConfigurationSHA256: sha([]byte("cfg")), ProfileSHA256: sha([]byte("profile")), Assets: []verification.Asset{{Path: "result", SHA256: sha([]byte("ok")), Role: "deliverable"}}}
	candidate, err := verification.ImportCandidate(m, src, bundleDir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	receipt, err := verification.IssueAcceptance(candidate, policy, nil, nil, "acceptor", now, "", priv)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "acceptance.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
	scope := CandidateScope{RunID: m.RunID, StepID: m.StepID, AttemptID: m.AttemptID, Fence: m.Fence, InputSHA256: m.InputSHA256, ConfigurationSHA256: m.ConfigurationSHA256, ProfileSHA256: m.ProfileSHA256}
	target := protectedTarget()
	ref := ArtifactRef{Milestone: "accepted", Kind: "acceptance", ID: receipt.ID, Digest: sha(b), Issuer: receipt.Issuer, EvidencePath: "acceptance.json", CandidateBundle: "candidate", Scope: scope, Destination: target}
	r, err := NewResolver(Config{Root: root, Policy: policy, TrustedIssuers: policy.Issuers, MaxAge: 10 * time.Minute, Artifacts: []ArtifactRef{ref}, Target: target, Projection: projectionOK{}})
	if err != nil {
		t.Fatal(err)
	}
	dep := writeback.EvidenceDependency{Milestone: ref.Milestone, Kind: ref.Kind, ID: ref.ID, Digest: ref.Digest, Issuer: ref.Issuer}
	verify := boundVerifier(t, r, target)
	otherTarget := target
	otherTarget.Scope.RemoteID = "43"
	otherOp, otherGrant := bindOperation(otherTarget)
	if _, err = r.EvidenceVerifierFor(otherOp, otherGrant); err == nil {
		t.Fatal("operation-bound verifier was reusable for another destination")
	}
	wrongRef := ref
	wrongRef.Destination = otherTarget
	if _, err = NewResolver(Config{Root: root, Policy: policy, TrustedIssuers: policy.Issuers, MaxAge: 10 * time.Minute, Artifacts: []ArtifactRef{wrongRef}, Target: target, Projection: projectionOK{}}); err == nil {
		t.Fatal("evidence bound to a different destination was accepted")
	}
	if err = verify(dep); err != nil {
		t.Fatalf("valid signed evidence rejected: %v", err)
	}
	denied, err := NewResolver(Config{Root: root, Policy: policy, TrustedIssuers: policy.Issuers, MaxAge: 10 * time.Minute, Artifacts: []ArtifactRef{ref}, Target: target, Projection: projectionOK{err: errors.New("projection revoked")}})
	if err != nil {
		t.Fatal(err)
	}
	if err = boundVerifier(t, denied, target)(dep); err == nil || !strings.Contains(err.Error(), "revoked") {
		t.Fatalf("revoked current projection was not enforced: %v", err)
	}
	stale, err := NewResolver(Config{Root: root, Policy: policy, TrustedIssuers: policy.Issuers, MaxAge: 10 * time.Minute, Artifacts: []ArtifactRef{ref}, Target: target, Projection: projectionOK{}, Clock: func() time.Time { return now.Add(time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	if err = boundVerifier(t, stale, target)(dep); err == nil {
		t.Fatal("stale acceptance passed")
	}
	bad := dep
	bad.ID = "other"
	if err = verify(bad); err == nil {
		t.Fatal("unknown evidence reference passed")
	}
	if err = os.WriteFile(filepath.Join(root, "acceptance.json"), append(b, []byte(" ")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err = verify(dep); err == nil {
		t.Fatal("tampered evidence passed")
	}
}

func TestWrongDestinationDeliveryRemainsBlocked(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	_ = os.Mkdir(src, 0700)
	_ = os.WriteFile(filepath.Join(src, "x"), []byte("x"), 0600)
	sha := func(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	issuer := verification.TrustedIssuer{ID: "issuer", Role: "acceptance", PublicKey: base64.StdEncoding.EncodeToString(pub)}
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "p", MaxAgeSeconds: 600, Criteria: []verification.Criterion{{ID: "x", Kind: "asset-present", Required: true, Expected: "x"}}, Issuers: []verification.TrustedIssuer{issuer}}
	m := verification.CandidateManifest{Version: verification.Version, ID: "c", Origin: "manual", Repository: "repo", BaseRevision: "base", SourceRevision: "head", RunID: "r", StepID: "s", AttemptID: "a", Fence: 1, InputSHA256: sha([]byte("i")), ConfigurationSHA256: sha([]byte("c")), ProfileSHA256: sha([]byte("p")), Assets: []verification.Asset{{Path: "x", SHA256: sha([]byte("x")), Role: "output"}}}
	c, err := verification.ImportCandidate(m, src, filepath.Join(root, "candidate"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// A malformed handoff with conflicting destination hashes is sufficient to
	// prove the resolver reaches and preserves the native destination check.
	h := deliverybridge.HandoffManifest{Version: deliverybridge.Version, ID: "d", Candidate: c, Policy: policy, Acceptance: verification.AcceptanceReceipt{Issuer: "issuer"}, SourceSnapshotSHA256: sha([]byte("source")), DestinationSnapshotSHA256: sha([]byte("other")), AsOf: now}
	h.AcceptanceBytes, _ = json.Marshal(h.Acceptance)
	b, _ := json.Marshal(h)
	_ = os.WriteFile(filepath.Join(root, "delivery.json"), b, 0600)
	target := protectedTarget()
	ref := ArtifactRef{Milestone: "delivered", Kind: "delivery", ID: "d", Digest: hash(b), Issuer: "issuer", EvidencePath: "delivery.json", CandidateBundle: "candidate", Scope: CandidateScope{RunID: m.RunID, StepID: m.StepID, AttemptID: m.AttemptID, Fence: m.Fence, InputSHA256: m.InputSHA256, ConfigurationSHA256: m.ConfigurationSHA256, ProfileSHA256: m.ProfileSHA256}, Destination: target}
	resolver, err := NewResolver(Config{Root: root, Policy: policy, TrustedIssuers: []verification.TrustedIssuer{issuer}, MaxAge: 10 * time.Minute, Artifacts: []ArtifactRef{ref}, Target: target, Projection: projectionOK{}, Publication: publishOK{}})
	if err != nil {
		t.Fatal(err)
	}
	dep := writeback.EvidenceDependency{Milestone: ref.Milestone, Kind: ref.Kind, ID: ref.ID, Digest: ref.Digest, Issuer: ref.Issuer}
	err = boundVerifier(t, resolver, target)(dep)
	if err == nil || !strings.Contains(err.Error(), "local delivery evidence invalid") {
		t.Fatalf("wrong destination was not rejected: %v", err)
	}
}

type publishOK struct{}

func (publishOK) VerifyPublished(ArtifactRef, deliverybridge.HandoffManifest, time.Time) error {
	return nil
}

func TestResolverRequiresCurrentProjectionAndPublicationQualification(t *testing.T) {
	if _, err := NewResolver(Config{}); err == nil {
		t.Fatal("resolver accepted missing current projection verifier")
	}
	// A delivery dependency is never satisfied by local handoff checks alone.
	root := t.TempDir()
	raw := []byte(`{}`)
	_ = os.WriteFile(filepath.Join(root, "delivery.json"), raw, 0600)
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	issuer := verification.TrustedIssuer{ID: "i", Role: "acceptance", PublicKey: base64.StdEncoding.EncodeToString(pub)}
	target := protectedTarget()
	ref := ArtifactRef{Milestone: "delivered", Kind: "delivery", ID: "d", Digest: hash(raw), Issuer: "i", EvidencePath: "delivery.json", CandidateBundle: "candidate", Scope: CandidateScope{RunID: "r", StepID: "s", AttemptID: "a", Fence: 1, InputSHA256: hash([]byte("i")), ConfigurationSHA256: hash([]byte("c")), ProfileSHA256: hash([]byte("p"))}, Destination: target}
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "p", MaxAgeSeconds: 10, Criteria: []verification.Criterion{{ID: "x", Kind: "asset-present", Required: true}}, Issuers: []verification.TrustedIssuer{issuer}}
	r, err := NewResolver(Config{Root: root, Policy: policy, TrustedIssuers: []verification.TrustedIssuer{issuer}, MaxAge: 10 * time.Second, Artifacts: []ArtifactRef{ref}, Target: target, Projection: projectionOK{}})
	if err != nil {
		t.Fatal(err)
	}
	err = boundVerifier(t, r, target)(writeback.EvidenceDependency{Milestone: ref.Milestone, Kind: ref.Kind, ID: ref.ID, Digest: ref.Digest, Issuer: ref.Issuer})
	if err == nil || !strings.Contains(err.Error(), "publication/readback verifier is unavailable") {
		t.Fatalf("delivery wasn't blocked on publication qualification: %v", err)
	}
}
