package store

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/methodology"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGovernanceMethodologyCASReplayAndRevocation(t *testing.T) {
	s, ctx, now := fixture(t)
	if e := s.EnableGovernance(ctx); e != nil {
		t.Fatal(e)
	}
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	keys := func(id, role string) (ed25519.PublicKey, bool) {
		return pub, id == "reviewer" && role == "methodology:fdd:design"
	}
	p := methodology.Profile{ID: "fdd", Stages: []string{"plan", "design"}, Gates: map[string][]string{"design": {"inspection"}}, VerifyEvidence: func(methodology.Evidence) error { return nil }}
	tr := methodology.Transition{Version: methodology.LifecycleVersion, ID: "approval-1", Subject: "feature-1", Profile: "fdd", From: "plan", To: "design", Role: "methodology:fdd:design", Signer: "reviewer", Evidence: []methodology.Evidence{{Gate: "inspection", SHA256: strings.Repeat("a", 64)}}}
	b, _ := methodology.CanonicalTransitionBytes(tr)
	tr.Signature = hex.EncodeToString(ed25519.Sign(key, b))
	snap, e := s.RecordMethodology(ctx, p, tr, keys, now)
	if e != nil || snap.Revision != 1 {
		t.Fatal(snap, e)
	}
	if _, e = s.RecordMethodology(ctx, p, tr, keys, now); e == nil {
		t.Fatal("replayed approval")
	}
	snap, e = s.MethodologySnapshot(ctx, "feature-1")
	if e != nil || snap.Stage != "design" {
		t.Fatal(snap, e)
	}

	if _, e = s.VerifiedMethodologySnapshot(ctx, "feature-1", p, keys); e != nil {
		t.Fatal(e)
	}
	stale := tr
	stale.ID = "stale-distinct-approval"
	sb, _ := methodology.CanonicalTransitionBytes(stale)
	stale.Signature = hex.EncodeToString(ed25519.Sign(key, sb))
	if _, e = s.RecordMethodology(ctx, p, stale, keys, now); e == nil {
		t.Fatal("stale expected revision accepted")
	}
	tampered := tr
	tampered.Evidence[0].SHA256 = strings.Repeat("b", 64)
	tb, _ := json.Marshal(tampered)
	if _, e = s.db.ExecContext(ctx, "UPDATE governance_event SET payload=?,digest=? WHERE logical_id=?", tb, usage.Hash(tb), tr.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.VerifiedMethodologySnapshot(ctx, "feature-1", p, keys); e == nil {
		t.Fatal("rehashed unsigned approval accepted")
	}
	tr.ID = "approval-2"
	tr.Subject = "feature-2"
	b, _ = methodology.CanonicalTransitionBytes(tr)
	tr.Signature = hex.EncodeToString(ed25519.Sign(key, b))
	p.VerifyEvidence = func(methodology.Evidence) error { return errors.New("stale or revoked") }
	if _, e = s.RecordMethodology(ctx, p, tr, keys, now); e == nil {
		t.Fatal("revoked approval")
	}
}
func TestProvisionIntentConcurrentOneSendAndReopen(t *testing.T) {
	s, ctx, _ := fixture(t)
	if e := s.EnableGovernance(ctx); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	results := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.Consume(ctx, "same-creation", strings.Repeat("a", 64)) }()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
	if e := s.Record(ctx, "absent", "readback-observed"); e == nil {
		t.Fatal("unconsumed intent accepted")
	}
	if e := s.Record(ctx, "same-creation", "readback-observed"); e != nil {
		t.Fatal(e)
	}
	_ = context.Background()
	_ = time.Second
}
