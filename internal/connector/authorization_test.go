package connector

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

type controlledWriter struct{ calls int }

func (w *controlledWriter) Apply(ctx context.Context, v ValidatedOperation, a AuthorizationContext) (Result[contracts.SyncReceipt], error) {
	if !AuthorizationValid(a, v, time.Now()) {
		return Result[contracts.SyncReceipt]{Disposition: Unsupported}, nil
	}
	w.calls++
	return Result[contracts.SyncReceipt]{Disposition: Unknown}, nil
}
func (w *controlledWriter) Reconcile(context.Context, ValidatedOperation, contracts.SyncReceipt) (Result[contracts.SyncReceipt], error) {
	return Result[contracts.SyncReceipt]{Disposition: Unknown}, nil
}
func signedFixture(t *testing.T) (contracts.SyncOperation, contracts.ConnectorCapability, contracts.RemoteIdentity, writeback.Grant, writeback.TrustPolicy, writeback.RevocationSnapshot) {
	t.Helper()
	op, cap, id, _ := fixture()
	now := time.Now().UTC()
	pub, key, e := ed25519.GenerateKey(nil)
	if e != nil {
		t.Fatal(e)
	}
	g := writeback.Grant{APIVersion: writeback.APIVersion, Kind: "WriteGrant", ID: op.Authority.Reference, Issuer: "operator", Principal: "principal", AuthRef: "credential-ref", IssuedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ProviderOrigin: "https://api.github.com", SnapshotObservedAt: now.Format(time.RFC3339Nano), DraftDigest: writeback.DigestBytes([]byte("draft")), SnapshotHash: writeback.DigestBytes([]byte("snapshot")), MappingVersion: "map-1", Scope: id, Operation: op.OperationKind, OperationID: op.OperationID, OperationDigest: op.OperationDigest, Capability: op.ConnectorCapabilityRef, Binding: op.WorkItemBindingRef, ExpectedRevision: op.ExpectedRevision, Nonce: "unique-nonce", ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), RevocationGeneration: 1}
	b, _ := writeback.GrantSigningBytes(g)
	g.Signature = hex.EncodeToString(ed25519.Sign(key, b))
	p := writeback.TrustPolicy{APIVersion: writeback.APIVersion, Kind: "TrustPolicy", Keys: map[string]string{"operator": hex.EncodeToString(pub)}, MaxGrantTTLSeconds: 300, ProviderOrigins: []string{g.ProviderOrigin}, ProviderRoutes: []writeback.ProviderRoute{{Provider: id.Provider, Origin: g.ProviderOrigin}}, FieldRules: []writeback.NativeFieldRule{{Provider: id.Provider, FieldID: "title", AllowedValues: []json.RawMessage{json.RawMessage(`"updated"`)}}}, Principals: []string{g.Principal}, AuthRefs: []string{g.AuthRef}}
	r := writeback.RevocationSnapshot{APIVersion: writeback.APIVersion, Kind: "RevocationSnapshot", Issuer: g.Issuer, Generation: 1, IssuedAt: now.Format(time.RFC3339Nano), RevokedGrantIDs: []string{}, RevokedNonces: []string{}}
	b, _ = writeback.RevocationSigningBytes(r)
	r.Signature = hex.EncodeToString(ed25519.Sign(key, b))
	return op, cap, id, g, p, r
}
func TestSignedContextCannotBeFabricatedOrReused(t *testing.T) {
	op, cap, id, g, p, r := signedFixture(t)
	v, ds := ValidateOperation(op, cap, id)
	if len(ds) > 0 {
		t.Fatal(ds)
	}
	if AuthorizationValid(AuthorizationContext{}, v, time.Now()) {
		t.Fatal("fabricated auth")
	}
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	box, e := writeback.Open(filepath.Join(root, "auth.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer box.Close()
	approved, e := writeback.VerifyGrant(g, p, r, op, cap, id, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = box.ImportRevocation(p, r, time.Now()); e != nil {
		t.Fatal(e)
	}
	if _, e = box.Enqueue(op, approved); e != nil {
		t.Fatal(e)
	}
	entry, e := box.Claim(op.OperationID, "owner", time.Now(), time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = authorize(op, cap, id, g, approved, time.Now(), box, "owner", entry.Fence); e == nil {
		t.Fatal("preintent authorization")
	}
	entry, e = box.CommitSendIntentBound(op.OperationID, "owner", entry.Fence, time.Now(), approved, op)
	if e != nil {
		t.Fatal(e)
	}
	a, e := authorize(op, cap, id, g, approved, time.Now(), box, "owner", entry.Fence)
	if e != nil {
		t.Fatal(e)
	}
	if !AuthorizationValid(a, v, time.Now()) || AuthorizationValid(a, v, time.Now()) {
		t.Fatal("transport token must be single use")
	}
	g.Principal = "other"
	if _, e = writeback.VerifyGrantBound(g, p, r, op, cap, id, writeback.GrantContext{DraftDigest: g.DraftDigest, SnapshotHash: g.SnapshotHash, MappingVersion: g.MappingVersion, ObservedAt: parseFixtureTime(g.SnapshotObservedAt)}, nil, time.Now()); e == nil {
		t.Fatal("tampered grant accepted")
	}

}
func TestDispatchLostReplyCannotAutomaticallyResend(t *testing.T) {
	op, cap, id, g, p, r := signedFixture(t)
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	box, e := writeback.Open(filepath.Join(root, "outbox.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	defer box.Close()
	approved, e := writeback.VerifyGrant(g, p, r, op, cap, id, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if e = box.ImportRevocation(p, r, time.Now()); e != nil {
		t.Fatal(e)
	}
	if _, e = box.Enqueue(op, approved); e != nil {
		t.Fatal(e)
	}
	writer := &controlledWriter{}
	entry, e := DispatchOnce(context.Background(), box, writer, op, cap, id, g, p, r, writeback.GrantContext{DraftDigest: g.DraftDigest, SnapshotHash: g.SnapshotHash, MappingVersion: g.MappingVersion, ObservedAt: parseFixtureTime(g.SnapshotObservedAt)}, nil, "owner", time.Now())
	if e != nil {
		t.Fatal(e)
	}
	if entry.State != "unknown-awaiting-reconciliation" || writer.calls != 1 {
		t.Fatalf("%+v calls=%d", entry, writer.calls)
	}
	if _, e = DispatchOnce(context.Background(), box, writer, op, cap, id, g, p, r, writeback.GrantContext{DraftDigest: g.DraftDigest, SnapshotHash: g.SnapshotHash, MappingVersion: g.MappingVersion, ObservedAt: parseFixtureTime(g.SnapshotObservedAt)}, nil, "owner2", time.Now()); e == nil {
		t.Fatal("ambiguous mutation retried")
	}
	if writer.calls != 1 {
		t.Fatal("duplicate request")
	}
}

func parseFixtureTime(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }
