package writeback

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

func TestDatabaseIdentityVersionAndSidecarsFailClosed(t *testing.T) {
	dir := t.TempDir()
	foreignPath := filepath.Join(dir, "foreign.sqlite")
	foreign, err := sql.Open("sqlite", foreignPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = foreign.Exec(`CREATE TABLE foreign_data(value TEXT); INSERT INTO foreign_data VALUES('preserve-me')`); err != nil {
		t.Fatal(err)
	}
	if err = foreign.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(foreignPath, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(foreignPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Open(foreignPath); err == nil {
		t.Fatal("foreign untagged database accepted")
	}
	after, err := os.ReadFile(foreignPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("foreign database changed after rejected open")
	}

	versionPath := filepath.Join(dir, "version.sqlite")
	o, err := Open(versionPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = o.db.Exec(`PRAGMA user_version=99`); err != nil {
		t.Fatal(err)
	}
	if err = o.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(versionPath); err == nil {
		t.Fatal("unsupported future schema version accepted")
	}
	if _, err = OpenReadOnly(versionPath); err == nil {
		t.Fatal("read-only open accepted unsupported schema version")
	}

	sidecarPath := filepath.Join(dir, "sidecar.sqlite")
	o, err = Open(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = o.Close(); err != nil {
		t.Fatal(err)
	}
	walPath := sidecarPath + "-wal"
	if err = os.Remove(walPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "external-wal")
	if err = os.WriteFile(target, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(target, walPath); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(sidecarPath); err == nil {
		t.Fatal("symlinked WAL sidecar accepted for write")
	}
	if _, err = OpenReadOnly(sidecarPath); err == nil {
		t.Fatal("symlinked WAL sidecar accepted for read")
	}
}

func outboxTestOperation(id string) contracts.SyncOperation {
	binding := contracts.Reference{Kind: "WorkItemBinding", ID: "binding", Version: "1"}
	cap := contracts.Reference{Kind: "ConnectorCapability", ID: "cap", Version: "1"}
	identity := outboxTestIdentity()
	digest, _ := contracts.OperationDigest(binding, identity, cap, "comment", "revision", contracts.SyncChange{})
	return contracts.SyncOperation{OperationID: id, OperationDigest: digest, WorkItemBindingRef: binding, ConnectorCapabilityRef: cap, OperationKind: "comment", ExpectedRevision: "revision"}
}
func outboxTestIdentity() contracts.RemoteIdentity {
	return contracts.RemoteIdentity{Provider: "fixture", TenantID: "tenant", ContainerID: "container", RemoteID: "item", ItemKind: "issue"}
}

func TestSendIntentIsSingleUseUnderRaceAndAmbiguityNeverReopens(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	dbPath := filepath.Join(t.TempDir(), "outbox.sqlite")
	o, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = o.Close() }()
	op := outboxTestOperation("logical-1")
	g := VerifiedGrant{id: "grant-1", digest: op.OperationDigest, nonce: "nonce-1", issuer: "issuer-1", scope: outboxTestIdentity(), generation: 3, expires: now.Add(time.Minute)}
	if _, err = o.db.Exec(`INSERT INTO revocation_state(issuer,generation,issued_at) VALUES(?,?,?)`, "issuer-1", 3, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = o.Enqueue(op, g); err != nil {
		t.Fatal(err)
	}
	claimed, err := o.Claim("logical-1", "owner", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	success := 0
	var mu sync.Mutex
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := o.commitIntentForTest("logical-1", "owner", claimed.Fence, now, 3); e == nil {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("want exactly one send intent; got %d", success)
	}
	if _, err = o.RecordOutcome("logical-1", "owner", claimed.Fence, "unknown-awaiting-reconciliation", "lost response"); err != nil {
		t.Fatal(err)
	}
	if _, err = o.Claim("logical-1", "other", now, time.Minute); err == nil {
		t.Fatal("ambiguous operation became claimable")
	}
	if err = o.Close(); err != nil {
		t.Fatal(err)
	}
	o, err = OpenReadOnly(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := o.Get("logical-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempts != 1 || got.State != "unknown-awaiting-reconciliation" {
		t.Fatalf("bad final state: %+v", got)
	}
}

func TestReplayStaysUnknownAfterAmbiguousDispatch(t *testing.T) {
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	s := ReplayScenario{APIVersion: APIVersion, Kind: "ReplayScenario", Events: []ReplayEvent{{At: now.Format(time.RFC3339), Action: "enqueue", OperationID: "x", Detail: "d"}, {At: now.Format(time.RFC3339), Action: "claim", OperationID: "x", Owner: "a"}, {At: now.Format(time.RFC3339), Action: "send-intent", OperationID: "x", Owner: "a", Fence: 1, Generation: 1}, {At: now.Format(time.RFC3339), Action: "ambiguous", OperationID: "x"}}}
	out, err := Replay(s, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].State != "unknown-awaiting-reconciliation" || out[0].Attempts != 1 {
		t.Fatalf("unexpected replay: %+v", out)
	}
	if _, err = Replay(s, now.Add(-time.Second)); err == nil {
		t.Fatal("future events accepted")
	}
}

func TestSendIntentParsesSubsecondExpiryAndPersistsClockHighWater(t *testing.T) {
	base := time.Date(2026, 10, 24, 10, 0, 1, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "clock.sqlite")
	o, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	op := outboxTestOperation("fractional-expiry")
	g := VerifiedGrant{id: "fractional-grant", digest: op.OperationDigest, nonce: "fractional-nonce", issuer: "fractional-issuer", scope: outboxTestIdentity(), generation: 1, expires: base.Add(time.Second)}
	if _, err = o.db.Exec(`INSERT INTO revocation_state(issuer,generation,issued_at) VALUES(?,?,?)`, g.issuer, 1, base.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = o.Enqueue(op, g); err != nil {
		t.Fatal(err)
	}
	claim, err := o.Claim(op.OperationID, "owner", base, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = o.db.Exec(`UPDATE outbox SET grant_expiry=? WHERE operation_id=?`, base.Format(time.RFC3339Nano), op.OperationID); err != nil {
		t.Fatal(err)
	}
	_, err = o.commitIntentForTest(op.OperationID, "owner", claim.Fence, base.Add(100*time.Millisecond), 1)
	if err == nil {
		t.Fatal("subsecond time after whole-second expiry was accepted")
	}
	if err = o.Close(); err != nil {
		t.Fatal(err)
	}

	// A separate operation establishes a persisted high-water mark. Reopening
	// the database must not permit an earlier timestamp to authorize a send.
	o, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	// Make the first record valid again for an explicit send at base+500ms.
	if _, err = o.db.Exec(`UPDATE outbox SET grant_expiry=? WHERE operation_id=?`, base.Add(2*time.Second).Format(time.RFC3339Nano), op.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err = o.db.Exec(`UPDATE revocation_state SET issued_at=? WHERE issuer=?`, base.Add(400*time.Millisecond).Format(time.RFC3339Nano), g.issuer); err != nil {
		t.Fatal(err)
	}
	if _, err = o.db.Exec(`UPDATE outbox SET state='authorized',owner='owner',fence=1,lease_until=? WHERE operation_id=?`, base.Add(time.Second).Format(time.RFC3339Nano), op.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err = o.commitIntentForTest(op.OperationID, "owner", 1, base.Add(500*time.Millisecond), 1); err != nil {
		t.Fatal(err)
	}
	// Next prepared operation is claimed after the high-water timestamp, then
	// send admission with a regressed clock is rejected even across reopen.
	op2 := outboxTestOperation("clock-regression")
	g2 := VerifiedGrant{id: "clock-grant-2", digest: op2.OperationDigest, nonce: "clock-nonce-2", issuer: g.issuer, scope: outboxTestIdentity(), generation: 1, expires: base.Add(5 * time.Second)}
	if _, err = o.Enqueue(op2, g2); err != nil {
		t.Fatal(err)
	}
	if _, err = o.Claim(op2.OperationID, "owner2", base.Add(600*time.Millisecond), time.Second); err != nil {
		t.Fatal(err)
	}
	if err = o.Close(); err != nil {
		t.Fatal(err)
	}
	o, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if _, err = o.commitIntentForTest(op2.OperationID, "owner2", 1, base.Add(400*time.Millisecond), 1); err == nil {
		t.Fatal("clock rollback after restart was accepted")
	}
}

func TestClosedDecodersRejectUnknownAndDuplicateFields(t *testing.T) {
	for _, input := range []string{
		`{"apiVersion":"aok-writeback/v1","kind":"WriteGrant","unknown":true}`,
		`{"apiVersion":"aok-writeback/v1","apiVersion":"aok-writeback/v1","kind":"WriteGrant"}`,
	} {
		var g Grant
		if err := Decode([]byte(input), &g); err == nil {
			t.Fatalf("accepted non-closed input: %s", input)
		}
	}
}

func TestCrashAfterDurableIntentCannotResend(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.sqlite")
	now := time.Now().UTC().Truncate(time.Second)
	o, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	op := outboxTestOperation("crash-op")
	g := VerifiedGrant{id: "crash-grant", digest: op.OperationDigest, nonce: "crash-nonce", issuer: "crash-issuer", scope: outboxTestIdentity(), generation: 4, expires: now.Add(time.Minute)}
	if _, err = o.db.Exec(`INSERT INTO revocation_state(issuer,generation,issued_at) VALUES(?,?,?)`, g.issuer, g.generation, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = o.Enqueue(op, g); err != nil {
		t.Fatal(err)
	}
	_, err = o.Claim(op.OperationID, "crash-owner", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err = o.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestWritebackCrashWorker$")
	cmd.Env = append(os.Environ(), "AOK_WRITEBACK_CRASH_HELPER=1", "AOK_WRITEBACK_CRASH_DB="+dbPath, "AOK_WRITEBACK_CRASH_NOW="+now.Format(time.RFC3339Nano), "AOK_WRITEBACK_CRASH_FENCE=1")
	err = cmd.Run()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 77 {
		t.Fatalf("crash worker did not exit at injected crash point: %v", err)
	}
	ro, err := OpenReadOnly(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	got, err := ro.Get(op.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "dispatch-intent" || got.Attempts != 1 {
		t.Fatalf("send intent was not durably consumed: %+v", got)
	}
	if _, err = ro.Claim(op.OperationID, "new-owner", now, time.Minute); err == nil {
		t.Fatal("post-crash operation became claimable")
	}
}

func TestWritebackCrashWorker(t *testing.T) {
	if os.Getenv("AOK_WRITEBACK_CRASH_HELPER") != "1" {
		return
	}
	dbPath := os.Getenv("AOK_WRITEBACK_CRASH_DB")
	if dbPath == "" {
		return
	}
	now, e := time.Parse(time.RFC3339Nano, os.Getenv("AOK_WRITEBACK_CRASH_NOW"))
	if e != nil {
		os.Exit(78)
	}
	o, e := Open(dbPath)
	if e != nil {
		os.Exit(79)
	}
	_, e = o.commitIntentForTest("crash-op", "crash-owner", 1, now, 4)
	if e != nil {
		os.Exit(80)
	}
	// Simulates process death after intent fsync and before network dispatch.
	os.Exit(77)
}

func TestPrivatePathURICharactersAndParentSafety(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "outbox?mode=memory#fragment.sqlite")
	o, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = o.Close(); err != nil {
		t.Fatal(err)
	}
	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	ro.Close()
	link := filepath.Join(dir, "linked")
	if err = os.Symlink(dir, link); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(filepath.Join(link, "state.sqlite")); err == nil {
		t.Fatal("symlink parent accepted")
	}
	unsafe := filepath.Join(dir, "unsafe")
	if err = os.Mkdir(unsafe, 0777); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(unsafe, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(filepath.Join(unsafe, "state.sqlite")); err == nil {
		t.Fatal("world-writable parent accepted")
	}
}

func TestRevocationsAreMonotonicAcrossSignedGenerations(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	policy := TrustPolicy{APIVersion: APIVersion, Kind: "TrustPolicy", Keys: map[string]string{"issuer": hex.EncodeToString(pub)}}
	path := filepath.Join(t.TempDir(), "outbox.sqlite")
	o, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	op := outboxTestOperation("revoked-op")
	g := VerifiedGrant{id: "g", issuer: "issuer", digest: op.OperationDigest, nonce: "once", scope: outboxTestIdentity(), generation: 2, expires: now.Add(time.Minute)}
	if _, err = o.Enqueue(op, g); err != nil {
		t.Fatal(err)
	}
	if _, err = o.Claim(op.OperationID, "owner", now, time.Minute); err != nil {
		t.Fatal(err)
	}
	makeRev := func(generation uint64, at time.Time, nonces []string) RevocationSnapshot {
		r := RevocationSnapshot{APIVersion: APIVersion, Kind: "RevocationSnapshot", Issuer: "issuer", Generation: generation, IssuedAt: at.Format(time.RFC3339Nano), RevokedGrantIDs: []string{}, RevokedNonces: nonces}
		b, _ := RevocationSigningBytes(r)
		r.Signature = hex.EncodeToString(ed25519.Sign(priv, b))
		return r
	}
	r1 := makeRev(1, now, []string{"once"})
	if err = o.ImportRevocation(policy, r1, now); err != nil {
		t.Fatal(err)
	}
	r2 := makeRev(2, now.Add(time.Second), nil)
	if err = o.ImportRevocation(policy, r2, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err = o.commitIntentForTest(op.OperationID, "owner", 1, now.Add(time.Second), 2); err == nil {
		t.Fatal("later snapshot reinstated an old revoked nonce")
	}
}

func TestExpiredPreIntentClaimCanBeFencedOver(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	path := filepath.Join(t.TempDir(), "lease.sqlite")
	o, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	op := outboxTestOperation("lease-op")
	g := VerifiedGrant{id: "lease-grant", issuer: "lease-issuer", digest: op.OperationDigest, nonce: "lease-nonce", scope: outboxTestIdentity(), generation: 5, expires: now.Add(time.Minute)}
	if _, err = o.db.Exec(`INSERT INTO revocation_state(issuer,generation,issued_at) VALUES(?,?,?)`, g.issuer, 5, now.Add(time.Second).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = o.Enqueue(op, g); err != nil {
		t.Fatal(err)
	}
	first, err := o.Claim(op.OperationID, "old", now, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	o.Close()
	o, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	second, err := o.Claim(op.OperationID, "new", now.Add(2*time.Second), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if second.Fence <= first.Fence {
		t.Fatal("fence did not increase")
	}
	if _, err = o.commitIntentForTest(op.OperationID, "old", first.Fence, now.Add(2*time.Second), 5); err == nil {
		t.Fatal("stale claimant committed send intent")
	}
	if _, err = o.commitIntentForTest(op.OperationID, "new", second.Fence, now.Add(2*time.Second), 5); err != nil {
		t.Fatalf("current claimant could not commit: %v", err)
	}
}

func TestBoundSendIntentChecksFullImmutableOperation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	o, err := Open(filepath.Join(t.TempDir(), "bound.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	op := outboxTestOperation("bound-op")
	g := VerifiedGrant{id: "bound-grant", issuer: "issuer", digest: op.OperationDigest, nonce: "bound-nonce", scope: outboxTestIdentity(), generation: 6, sourceGeneration: 6, expires: now.Add(time.Minute), grantHash: "gh", policyHash: "ph", origin: "https://tracker.example"}
	if _, err = o.db.Exec(`INSERT INTO revocation_state(issuer,generation,issued_at) VALUES(?,?,?)`, g.issuer, g.generation, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = o.Enqueue(op, g); err != nil {
		t.Fatal(err)
	}
	c, err := o.Claim(op.OperationID, "owner", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	changed := op
	changed.ExpectedRevision = "other"
	if _, err = o.CommitSendIntentBound(op.OperationID, "owner", c.Fence, now, g, changed); err == nil {
		t.Fatal("changed operation was committed")
	}
	if got, err := o.Get(op.OperationID); err != nil || got.State != "authorized" {
		t.Fatalf("mismatch consumed budget: %+v %v", got, err)
	}
	if _, err = o.CommitSendIntentBound(op.OperationID, "owner", c.Fence, now, g, op); err != nil {
		t.Fatalf("exact bound operation rejected: %v", err)
	}
}

func TestCompletionTransitionsNeedExplicitPolicyAndEvidence(t *testing.T) {
	op := contracts.SyncOperation{OperationKind: "transition", Change: contracts.SyncChange{TransitionID: "closed"}}
	g := Grant{}
	if err := validateCompletionGrant(op, TrustPolicy{}, g, "github", false, nil); err == nil {
		t.Fatal("unlisted completion transition was allowed")
	}
	p := TrustPolicy{CompletionRules: []CompletionRule{{Provider: "github", TransitionID: "closed", Allowed: true, RequiredMilestones: []string{"accepted", "delivered"}}}}
	if err := validateCompletionGrant(op, p, g, "github", false, nil); err == nil {
		t.Fatal("completion without external milestone verification was allowed")
	}
}

func TestBoundGrantPinsDraftSnapshotAndMapping(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	op := outboxTestOperation("bound-grant-op")
	id := outboxTestIdentity()
	cap := contracts.ConnectorCapability{DocumentHeader: contracts.DocumentHeader{Kind: "ConnectorCapability", Metadata: contracts.OperationalMetadata{ID: op.ConnectorCapabilityRef.ID, Version: op.ConnectorCapabilityRef.Version}}}
	ctx := GrantContext{DraftDigest: DigestBytes([]byte("draft")), SnapshotHash: DigestBytes([]byte("snapshot")), MappingVersion: "mapping-v7", ObservedAt: now}
	g := Grant{APIVersion: APIVersion, Kind: "WriteGrant", ID: "grant", Issuer: "issuer", Principal: "human", AuthRef: "auth-ref", IssuedAt: now.Format(time.RFC3339Nano), ProviderOrigin: "https://tracker.example", DraftDigest: ctx.DraftDigest, SnapshotHash: ctx.SnapshotHash, SnapshotObservedAt: now.Format(time.RFC3339Nano), MappingVersion: ctx.MappingVersion, Scope: id, Operation: op.OperationKind, OperationID: op.OperationID, OperationDigest: op.OperationDigest, Capability: op.ConnectorCapabilityRef, Binding: op.WorkItemBindingRef, ExpectedRevision: op.ExpectedRevision, Nonce: "n", ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), RevocationGeneration: 1}
	b, _ := GrantSigningBytes(g)
	g.Signature = hex.EncodeToString(ed25519.Sign(priv, b))
	r := RevocationSnapshot{APIVersion: APIVersion, Kind: "RevocationSnapshot", Issuer: "issuer", Generation: 1, IssuedAt: now.Format(time.RFC3339Nano), RevokedGrantIDs: []string{}, RevokedNonces: []string{}}
	rb, _ := RevocationSigningBytes(r)
	r.Signature = hex.EncodeToString(ed25519.Sign(priv, rb))
	p := TrustPolicy{APIVersion: APIVersion, Kind: "TrustPolicy", Keys: map[string]string{"issuer": hex.EncodeToString(pub)}, MaxGrantTTLSeconds: 300, ProviderOrigins: []string{g.ProviderOrigin}, Principals: []string{g.Principal}, AuthRefs: []string{g.AuthRef}}
	p.ProviderRoutes = []ProviderRoute{{Provider: id.Provider, Origin: g.ProviderOrigin}}
	if _, err = VerifyGrantBound(g, p, r, op, cap, id, ctx, nil, now); err != nil {
		t.Fatalf("exact context rejected: %v", err)
	}
	stale := ctx
	stale.SnapshotHash = DigestBytes([]byte("new snapshot"))
	if _, err = VerifyGrantBound(g, p, r, op, cap, id, stale, nil, now); err == nil {
		t.Fatal("grant accepted after snapshot changed")
	}
	stale = ctx
	stale.ObservedAt = now.Add(-6 * time.Minute)
	if _, err = VerifyGrantBound(g, p, r, op, cap, id, stale, nil, now); err == nil {
		t.Fatal("grant accepted stale source observation")
	}

	// A newer signed snapshot that keeps the grant unrevoked must advance the
	// admission generation without changing the grant's original generation.
	o, err := Open(filepath.Join(t.TempDir(), "generation-advance.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if err = o.ImportRevocation(p, r, now); err != nil {
		t.Fatal(err)
	}
	verified1, err := VerifyGrantBound(g, p, r, op, cap, id, ctx, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = o.Enqueue(op, verified1); err != nil {
		t.Fatal(err)
	}
	claim, err := o.Claim(op.OperationID, "advance-owner", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r2 := r
	r2.Generation = 2
	r2.IssuedAt = now.Add(time.Second).Format(time.RFC3339Nano)
	r2.Signature = ""
	rb2, _ := RevocationSigningBytes(r2)
	r2.Signature = hex.EncodeToString(ed25519.Sign(priv, rb2))
	if err = o.ImportRevocation(p, r2, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	verified2, err := VerifyGrantBound(g, p, r2, op, cap, id, ctx, nil, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = o.CommitSendIntentBound(op.OperationID, "advance-owner", claim.Fence, now.Add(time.Second), verified2, op); err != nil {
		t.Fatalf("unrevoked grant blocked by generation advance: %v", err)
	}
	var sourceGeneration, admittedGeneration uint64
	if err = o.db.QueryRow(`SELECT grant_generation,admitted_generation FROM outbox WHERE operation_id=?`, op.OperationID).Scan(&sourceGeneration, &admittedGeneration); err != nil {
		t.Fatal(err)
	}
	if sourceGeneration != 1 || admittedGeneration != 2 {
		t.Fatalf("bad grant generation audit: source=%d admitted=%d", sourceGeneration, admittedGeneration)
	}
}

func TestOnlySignedFreshScopedReadbackCanVerifyReconciliation(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	policy := TrustPolicy{APIVersion: APIVersion, Kind: "TrustPolicy", Keys: map[string]string{"reader": hex.EncodeToString(pub)}, ReadbackIssuers: []string{"reader"}}
	op := outboxTestOperation("readback-op")
	id := outboxTestIdentity()
	report := ReconciliationReport{APIVersion: APIVersion, Kind: "ReconciliationReport", Issuer: "reader", OperationID: op.OperationID, OperationDigest: op.OperationDigest, Identity: id, ObservedAt: now.Format(time.RFC3339Nano), Disposition: "readback-verified", Evidence: []contracts.AssetRef{{Path: "evidence/readback.json", SHA256: DigestBytes([]byte("readback"))}}}
	b, err := ReconciliationSigningBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	report.Signature = hex.EncodeToString(ed25519.Sign(priv, b))
	verified, err := VerifyReconciliation(report, policy, op, id, now)
	if err != nil {
		t.Fatal(err)
	}
	wrong := id
	wrong.TenantID = "other-tenant"
	wrongReport := report
	wrongReport.Identity = wrong
	wrongBytes, _ := ReconciliationSigningBytes(wrongReport)
	wrongReport.Signature = hex.EncodeToString(ed25519.Sign(priv, wrongBytes))
	wrongVerified, err := VerifyReconciliation(wrongReport, policy, op, wrong, now)
	if err != nil {
		t.Fatal(err)
	}
	o, err := Open(filepath.Join(t.TempDir(), "outbox.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	g := VerifiedGrant{id: "readback-grant", issuer: "issuer", digest: op.OperationDigest, nonce: "nonce", scope: id, generation: 1, expires: now.Add(time.Minute)}
	if _, err = o.db.Exec(`INSERT INTO revocation_state(issuer,generation,issued_at) VALUES(?,?,?)`, g.issuer, 1, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = o.Enqueue(op, g); err != nil {
		t.Fatal(err)
	}
	c, err := o.Claim(op.OperationID, "owner", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = o.commitIntentForTest(op.OperationID, "owner", c.Fence, now, 1); err != nil {
		t.Fatal(err)
	}
	if _, err = o.Reconcile(op.OperationID, "fabricated", "readback-verified"); err == nil {
		t.Fatal("raw string verified reconciliation")
	}
	if _, err = o.ReconcileVerified(wrongVerified); err == nil {
		t.Fatal("signed readback for wrong tenant changed persisted operation")
	}
	got, err := o.ReconcileVerified(verified)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "readback-verified" {
		t.Fatalf("wrong reconciliation state: %+v", got)
	}
}

func TestCreateIntentShapeIsValidatedButExecutionRemainsDisabled(t *testing.T) {
	i := CreateIntent{APIVersion: APIVersion, Kind: "CreateIntent", ID: "create-1", Parent: outboxTestIdentity(), TypeID: "issue", SchemaDigest: DigestBytes([]byte("schema")), Fields: json.RawMessage(`{"title":"fixture"}`), ClientCorrelationID: "correlation-1", Budget: 1, GrantID: "grant-1"}
	parsed, err := ParseCreateIntent(i)
	if err != nil {
		t.Fatal(err)
	}
	if err = ExecuteCreate(context.Background(), parsed); !errors.Is(err, ErrCreationDisabled) {
		t.Fatalf("execution should remain explicitly disabled: %v", err)
	}
	i.Budget = 2
	if _, err = ParseCreateIntent(i); err == nil {
		t.Fatal("multi-create budget accepted")
	}
}

func TestCreateGrantRejectsZeroRevocationGenerations(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	i, verified, p, r, priv := createFixture(t, now)
	newGrant := func(generation uint64) CreateGrant {
		g := CreateGrant{APIVersion: APIVersion, Kind: "CreateGrant", ID: verified.grantID, Issuer: verified.issuer, Principal: "human", AuthRef: "auth", IssuedAt: now.Format(time.RFC3339Nano), ProviderOrigin: "https://tracker.example", IntentDigest: verified.intentDigest, Parent: i.Parent, TypeID: i.TypeID, SchemaDigest: i.SchemaDigest, FieldsDigest: DigestBytes(i.Fields), ClientCorrelationID: i.ClientCorrelationID, Budget: 1, Nonce: "create-nonce", ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), RevocationGeneration: generation}
		b, _ := CreateGrantSigningBytes(g)
		g.Signature = hex.EncodeToString(ed25519.Sign(priv, b))
		return g
	}
	if _, err := VerifyCreateGrant(newGrant(0), p, r, i, now); err == nil {
		t.Fatal("zero grant revocation generation accepted")
	}
	r.Generation = 0
	r.IssuedAt = now.Format(time.RFC3339Nano)
	rb, _ := RevocationSigningBytes(r)
	r.Signature = hex.EncodeToString(ed25519.Sign(priv, rb))
	if _, err := VerifyCreateGrant(newGrant(1), p, r, i, now); err == nil {
		t.Fatal("zero current revocation generation accepted")
	}
}

func TestCreateGrantCanDispatchUnderNewerUnrevokedSnapshot(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	i, initial, p, r, priv := createFixture(t, now)
	g := CreateGrant{APIVersion: APIVersion, Kind: "CreateGrant", ID: i.GrantID, Issuer: initial.issuer, Principal: "human", AuthRef: "auth", IssuedAt: now.Format(time.RFC3339Nano), ProviderOrigin: initial.origin, IntentDigest: initial.intentDigest, Parent: i.Parent, TypeID: i.TypeID, SchemaDigest: i.SchemaDigest, FieldsDigest: DigestBytes(i.Fields), ClientCorrelationID: i.ClientCorrelationID, Budget: 1, Nonce: initial.nonce, ExpiresAt: initial.expires.Format(time.RFC3339Nano), RevocationGeneration: 1}
	gb, _ := CreateGrantSigningBytes(g)
	g.Signature = hex.EncodeToString(ed25519.Sign(priv, gb))
	o, err := Open(filepath.Join(t.TempDir(), "create-generation-advance.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if err = o.ImportRevocation(p, r, now); err != nil {
		t.Fatal(err)
	}
	if _, err = o.EnqueueCreate(initial); err != nil {
		t.Fatal(err)
	}
	claim, err := o.ClaimCreate(i.ClientCorrelationID, "creator", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	r2 := r
	r2.Generation = 2
	r2.IssuedAt = now.Add(time.Second).Format(time.RFC3339Nano)
	r2.Signature = ""
	rb, _ := RevocationSigningBytes(r2)
	r2.Signature = hex.EncodeToString(ed25519.Sign(priv, rb))
	if err = o.ImportRevocation(p, r2, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	current, err := VerifyCreateGrant(g, p, r2, i, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = o.CommitCreateIntent(i.ClientCorrelationID, "creator", claim.Fence, now.Add(time.Second), current); err != nil {
		t.Fatalf("unrevoked create grant blocked by generation advance: %v", err)
	}
	var sourceGeneration, admittedGeneration uint64
	if err = o.db.QueryRow(`SELECT grant_generation,admitted_generation FROM create_outbox WHERE correlation_id=?`, i.ClientCorrelationID).Scan(&sourceGeneration, &admittedGeneration); err != nil {
		t.Fatal(err)
	}
	if sourceGeneration != 1 || admittedGeneration != 2 {
		t.Fatalf("bad create generation audit: source=%d admitted=%d", sourceGeneration, admittedGeneration)
	}
}

type fixtureCreator struct {
	response  CreateResponse
	err       error
	calls     int
	permitErr error
}

func (f *fixtureCreator) Create(_ context.Context, permit CreationPermit) (CreateResponse, error) {
	f.calls++
	if _, err := permit.Consume(); err != nil {
		f.permitErr = err
		return CreateResponse{}, err
	}
	if _, err := permit.Consume(); err == nil {
		f.permitErr = errors.New("creation permit was reusable")
		return CreateResponse{}, f.permitErr
	}
	return f.response, f.err
}

func createFixture(t *testing.T, now time.Time) (CreateIntent, VerifiedCreateIntent, TrustPolicy, RevocationSnapshot, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	parent := contracts.RemoteIdentity{Provider: "github", TenantID: "tenant", ContainerID: "project", ItemKind: "project"}
	i := CreateIntent{APIVersion: APIVersion, Kind: "CreateIntent", ID: "create-id", Parent: parent, TypeID: "issue-type", SchemaDigest: DigestBytes([]byte("schema-v1")), Fields: json.RawMessage(`{"title":"new item"}`), ClientCorrelationID: "client-correlation-1", Budget: 1, GrantID: "create-grant"}
	digest, err := CreateIntentDigest(i)
	if err != nil {
		t.Fatal(err)
	}
	policy := TrustPolicy{APIVersion: APIVersion, Kind: "TrustPolicy", Keys: map[string]string{"issuer": hex.EncodeToString(pub)}, MaxGrantTTLSeconds: 300, ProviderOrigins: []string{"https://tracker.example"}, ProviderRoutes: []ProviderRoute{{Provider: "github", Origin: "https://tracker.example"}}, Principals: []string{"human"}, AuthRefs: []string{"auth"}, ReadbackIssuers: []string{"issuer"}}
	g := CreateGrant{APIVersion: APIVersion, Kind: "CreateGrant", ID: i.GrantID, Issuer: "issuer", Principal: "human", AuthRef: "auth", IssuedAt: now.Format(time.RFC3339Nano), ProviderOrigin: "https://tracker.example", IntentDigest: digest, Parent: parent, TypeID: i.TypeID, SchemaDigest: i.SchemaDigest, FieldsDigest: DigestBytes(i.Fields), ClientCorrelationID: i.ClientCorrelationID, Budget: 1, Nonce: "create-nonce", ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), RevocationGeneration: 1}
	b, _ := CreateGrantSigningBytes(g)
	g.Signature = hex.EncodeToString(ed25519.Sign(priv, b))
	r := RevocationSnapshot{APIVersion: APIVersion, Kind: "RevocationSnapshot", Issuer: "issuer", Generation: 1, IssuedAt: now.Format(time.RFC3339Nano), RevokedGrantIDs: []string{}, RevokedNonces: []string{}}
	rb, _ := RevocationSigningBytes(r)
	r.Signature = hex.EncodeToString(ed25519.Sign(priv, rb))
	verified, err := VerifyCreateGrant(g, policy, r, i, now)
	if err != nil {
		t.Fatal(err)
	}
	return i, verified, policy, r, priv
}

func TestCreateOutboxFixtureReadbackAndBindingOnlyAfterAssignment(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	intent, verified, p, rev, priv := createFixture(t, now)
	o, err := Open(filepath.Join(t.TempDir(), "create.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if err = o.ImportRevocation(p, rev, now); err != nil {
		t.Fatal(err)
	}
	queued, err := o.EnqueueCreate(verified)
	if err != nil {
		t.Fatal(err)
	}
	if queued.CandidateIdentity != nil || queued.Identity != nil {
		t.Fatal("identity appeared before creator dispatch")
	}
	claimed, err := o.ClaimCreate(intent.ClientCorrelationID, "creator", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	assigned := contracts.RemoteIdentity{Provider: "github", TenantID: "tenant", ContainerID: "project", RemoteID: "native-381", ItemKind: "issue"}
	creator := &fixtureCreator{response: CreateResponse{Identity: assigned, NativeReceiptID: "receipt"}}
	result, err := o.DispatchCreateOnce(context.Background(), creator, intent.ClientCorrelationID, "creator", claimed.Fence, now, verified)
	if err != nil {
		t.Fatal(err)
	}
	if creator.permitErr != nil {
		t.Fatalf("creator permit invariant failed: %v", creator.permitErr)
	}
	if result.State != "response-observed" || result.Identity != nil || result.CandidateIdentity == nil || *result.CandidateIdentity != assigned {
		t.Fatalf("unexpected unverified result: %+v", result)
	}
	if _, err = o.ClaimCreate(intent.ClientCorrelationID, "retry", now, time.Minute); err == nil {
		t.Fatal("create retried after mutation intent")
	}
	readback := CreateReadbackReport{APIVersion: APIVersion, Kind: "CreateReadbackReport", Issuer: "issuer", ClientCorrelationID: intent.ClientCorrelationID, IntentDigest: verified.intentDigest, Identity: assigned, ObservedAt: now.Format(time.RFC3339Nano), Evidence: []contracts.AssetRef{{Path: "readback.json", SHA256: DigestBytes([]byte("readback"))}}}
	rb, _ := CreateReadbackSigningBytes(readback)
	readback.Signature = hex.EncodeToString(ed25519.Sign(priv, rb))
	trusted, err := VerifyCreateReadback(readback, p, verified, now)
	if err != nil {
		t.Fatal(err)
	}
	final, err := o.ReconcileCreateVerified(trusted)
	if err != nil {
		t.Fatal(err)
	}
	if final.State != "readback-verified" || final.Identity == nil || *final.Identity != assigned {
		t.Fatalf("readback did not establish assigned identity: %+v", final)
	}
	binding := contracts.WorkItemBinding{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "WorkItemBinding", Metadata: contracts.OperationalMetadata{ID: "created-binding", Name: "created-binding", Version: "0.1.0", Description: "verified fixture"}}, Identity: assigned, Packet: contracts.AssetRef{Path: "assets/item.json", SHA256: DigestBytes([]byte("packet"))}, ObservedSourceRevision: "rev-1", MappingVersion: "mapping-v1", FieldMappings: []contracts.FieldMapping{{CanonicalField: "title", ProviderFieldID: "title", Owner: "tracker", Supported: true}}}
	issued, err := o.CreateBinding(intent.ClientCorrelationID, binding)
	if err != nil {
		t.Fatal(err)
	}
	if issued.Binding().Identity != assigned {
		t.Fatal("binding identity mismatch")
	}
}

func TestCreateLostReplyRemainsAmbiguousAndNeverCallsCreatorAgain(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	intent, verified, p, rev, _ := createFixture(t, now)
	o, err := Open(filepath.Join(t.TempDir(), "create-lost.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer o.Close()
	if err = o.ImportRevocation(p, rev, now); err != nil {
		t.Fatal(err)
	}
	if _, err = o.EnqueueCreate(verified); err != nil {
		t.Fatal(err)
	}
	c, err := o.ClaimCreate(intent.ClientCorrelationID, "creator", now, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	creator := &fixtureCreator{err: errors.New("lost reply")}
	entry, err := o.DispatchCreateOnce(context.Background(), creator, intent.ClientCorrelationID, "creator", c.Fence, now, verified)
	if err == nil || entry.State != "unknown-awaiting-reconciliation" || creator.calls != 1 {
		t.Fatalf("lost reply not preserved: %+v %v calls=%d", entry, err, creator.calls)
	}
	if _, err = o.ClaimCreate(intent.ClientCorrelationID, "retry", now.Add(time.Minute), time.Minute); err == nil {
		t.Fatal("ambiguous create was resent")
	}
	if creator.calls != 1 {
		t.Fatalf("creator called %d times", creator.calls)
	}
}
