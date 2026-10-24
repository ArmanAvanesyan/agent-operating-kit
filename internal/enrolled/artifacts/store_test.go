package artifacts

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

func fixtureCandidate(t *testing.T) verification.Candidate {
	t.Helper()
	src := t.TempDir()
	body := []byte("bounded fixture\n")
	sum := sha256.Sum256(body)
	if e := os.WriteFile(filepath.Join(src, "README.txt"), body, 0600); e != nil {
		t.Fatal(e)
	}
	m := verification.CandidateManifest{Version: verification.Version, ID: "fixture-candidate", Origin: "fixture", Repository: "local/fixture", BaseRevision: "base", SourceRevision: "candidate", Assets: []verification.Asset{{Path: "README.txt", SHA256: hex.EncodeToString(sum[:]), Role: "source"}}}
	c, e := verification.ImportCandidate(m, src, filepath.Join(t.TempDir(), "bundle"))
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func fixtureAuthority(t *testing.T, c verification.Candidate) (verification.ControlProof, verification.VerificationPolicy, verification.VerifierSpec) {
	t.Helper()
	pub, priv, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	spec := verification.VerifierSpec{ID: "tests", Kind: "command", ImplementationSHA256: hash("impl"), RubricSHA256: hash("rubric"), Argv: []string{"check"}, Environment: map[string]string{}, TimeoutSeconds: 3, MaxOutputBytes: 256}
	issuer := verification.TrustedIssuer{ID: "controls", Role: "controls", ExecutorSHA256: hash("executor"), PublicKey: base64.StdEncoding.EncodeToString(pub)}
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "policy", Issuers: []verification.TrustedIssuer{issuer}, Verifiers: []verification.VerifierSpec{spec}, Criteria: []verification.Criterion{{ID: "criterion", Kind: "command", Required: true, VerifierID: spec.ID}}, MaxAgeSeconds: 300}
	pd, _ := verification.Digest(policy)
	sd, _ := verification.Digest(spec)
	now := time.Now().UTC().Truncate(time.Second)
	proof, e := verification.SignControlProof(verification.ControlProof{PolicySHA256: pd, VerifierSHA256: sd, CandidateSHA256: c.Digest, SnapshotSHA256: c.SnapshotSHA256, Scope: "candidate-offline", ExecutorSHA256: issuer.ExecutorSHA256, ObservedAt: now.Add(-time.Minute).Format(time.RFC3339), ValidUntil: now.Add(time.Minute).Format(time.RFC3339), Controls: []string{"filesystem-isolation", "readonly-candidate", "network-deny", "credential-isolation", "time-bound", "output-bound"}, Issuer: issuer.ID}, priv)
	if e != nil {
		t.Fatal(e)
	}
	return proof, policy, spec
}
func hash(s string) string { x := sha256.Sum256([]byte(s)); return hex.EncodeToString(x[:]) }

func TestImmutableTransferRoundTripAndNoOverwrite(t *testing.T) {
	c := fixtureCandidate(t)
	proof, policy, spec := fixtureAuthority(t, c)
	payload, e := Encode(c)
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewStore(filepath.Join(t.TempDir(), "store"), policy, spec.ID)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.Ingest(context.Background(), c.Digest, c.SnapshotSHA256, proof, bytes.NewReader(payload))
	if e != nil {
		t.Fatal(e)
	}
	if got.Digest != c.Digest || got.SnapshotSHA256 != c.SnapshotSHA256 {
		t.Fatal("transfer identity changed")
	}
	if _, e = s.Ingest(context.Background(), c.Digest, c.SnapshotSHA256, proof, bytes.NewReader(payload)); e == nil {
		t.Fatal("existing immutable candidate overwritten")
	}
	if _, e = s.Get(context.Background(), c.Digest, c.SnapshotSHA256); e != nil {
		t.Fatal(e)
	}
}

func TestTransferRejectsTamperedBytesAndWrongSnapshot(t *testing.T) {
	c := fixtureCandidate(t)
	proof, policy, spec := fixtureAuthority(t, c)
	payload, e := Encode(c)
	if e != nil {
		t.Fatal(e)
	}
	s, e := NewStore(filepath.Join(t.TempDir(), "store"), policy, spec.ID)
	if e != nil {
		t.Fatal(e)
	}
	bad := append([]byte(nil), payload...)
	bad[len(bad)-1] ^= 1
	if _, e = s.Ingest(context.Background(), c.Digest, c.SnapshotSHA256, proof, bytes.NewReader(bad)); e == nil {
		t.Fatal("tampered archive accepted")
	}
	if _, e = s.Ingest(context.Background(), c.Digest, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", proof, bytes.NewReader(payload)); e == nil {
		t.Fatal("wrong snapshot accepted")
	}
	proof.Signature = ""
	if _, e = s.Ingest(context.Background(), c.Digest, c.SnapshotSHA256, proof, bytes.NewReader(payload)); e == nil {
		t.Fatal("unsigned candidate transfer accepted")
	}
}

func TestStoreRejectsSymlinkRoot(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if e := os.Mkdir(real, 0700); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(base, "link")
	if e := os.Symlink(real, link); e != nil {
		t.Fatal(e)
	}
	_, policy, spec := fixtureAuthority(t, fixtureCandidate(t))
	if _, e := NewStore(filepath.Join(link, "store"), policy, spec.ID); e == nil {
		t.Fatal("symlink store path accepted")
	}
}
