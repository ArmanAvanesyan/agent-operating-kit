package methodology

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

func TestTransitionRequiresSignedRoleAndAllGates(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	p := Profile{ID: "aup", Stages: []string{"inception", "construction"}, Gates: map[string][]string{"construction": {"plan", "test"}}, VerifyEvidence: func(e Evidence) error {
		if e.SHA256 != Digest([]byte(e.Gate)) {
			return errors.New("wrong evidence hash")
		}
		return nil
	}}
	cur := Snapshot{Subject: "ITER-1", Profile: "aup", Stage: "inception", Revision: 4}
	tr := Transition{Version: LifecycleVersion, ID: "tr-1", Subject: cur.Subject, Profile: "aup", From: cur.Stage, To: "construction", ExpectedRevision: 4, Role: "methodology:aup:construction", Signer: "human", Evidence: []Evidence{{Gate: "plan", SHA256: Digest([]byte("plan"))}}}
	keys := func(s, r string) (ed25519.PublicKey, bool) { return pub, s == "human" && r == tr.Role }
	body, _ := CanonicalTransitionBytes(tr)
	tr.Signature = hex.EncodeToString(ed25519.Sign(priv, body))
	if CheckTransition(p, cur, tr, keys) == nil {
		t.Fatal("missing gate accepted")
	}
	tr.Evidence = append(tr.Evidence, Evidence{Gate: "test", SHA256: Digest([]byte("test"))})
	body, _ = CanonicalTransitionBytes(tr)
	tr.Signature = hex.EncodeToString(ed25519.Sign(priv, body))
	appended := false
	next, err := RecordTransition(p, cur, tr, keys, func(rev uint64, n Snapshot, _ Transition) error {
		appended = true
		if rev != 4 || n.Revision != 5 {
			t.Fatal("bad CAS values")
		}
		return nil
	})
	if err != nil || !appended || next.Stage != "construction" {
		t.Fatalf("record: %#v %v", next, err)
	}
	if _, err = RecordTransition(p, cur, tr, keys, nil); err == nil {
		t.Fatal("missing append authority accepted")
	}
}
