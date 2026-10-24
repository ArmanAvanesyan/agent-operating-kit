// Package artifacts implements authenticated, immutable verifier candidate
// transfer. Callers must authenticate before invoking Ingest.
package artifacts

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

const MaxTransferBytes = verification.MaxCandidateCaptureBytes + 2<<20

// Store places candidates under digest-addressed, create-only paths.
type Store struct {
	root         string
	policy       verification.VerificationPolicy
	spec         verification.VerifierSpec
	policyDigest string
	specDigest   string
}

func NewStore(root string, policy verification.VerificationPolicy, verifierID string) (*Store, error) {
	if root == "" {
		return nil, errors.New("artifact root required")
	}
	if err := verification.ValidatePolicy(policy); err != nil {
		return nil, errors.New("protected verifier policy required")
	}
	var selected *verification.VerifierSpec
	for i := range policy.Verifiers {
		if policy.Verifiers[i].ID == verifierID {
			selected = &policy.Verifiers[i]
		}
	}
	if selected == nil {
		return nil, errors.New("protected pinned verifier required")
	}
	encoded, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}
	policy, err = verification.DecodePolicy(encoded)
	if err != nil {
		return nil, err
	}
	var pinned verification.VerifierSpec
	for _, v := range policy.Verifiers {
		if v.ID == verifierID {
			pinned = v
		}
	}
	pd, err := verification.Digest(policy)
	if err != nil {
		return nil, err
	}
	sd, err := verification.Digest(pinned)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = rejectLinks(abs); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(abs, 0700); err != nil {
		return nil, err
	}
	if err = os.Chmod(abs, 0700); err != nil {
		return nil, err
	}
	return &Store{root: abs, policy: policy, spec: pinned, policyDigest: pd, specDigest: sd}, nil
}

// AuthorityMatches ensures that handler and store use the same protected pins.
func (s *Store) AuthorityMatches(policy verification.VerificationPolicy, verifierID string) bool {
	if s == nil || s.spec.ID != verifierID {
		return false
	}
	pd, e := verification.Digest(policy)
	if e != nil || pd != s.policyDigest {
		return false
	}
	var pinned *verification.VerifierSpec
	for i := range policy.Verifiers {
		if policy.Verifiers[i].ID == verifierID {
			pinned = &policy.Verifiers[i]
		}
	}
	if pinned == nil {
		return false
	}
	sd, e := verification.Digest(*pinned)
	return e == nil && sd == s.specDigest
}

// Encode produces a bounded tar stream from a previously validated immutable bundle.
func Encode(c verification.Candidate) ([]byte, error) {
	if c.BundlePath == "" {
		return nil, errors.New("candidate bundle required")
	}
	fresh, err := verification.LoadCandidate(c.BundlePath)
	if err != nil || fresh.Digest != c.Digest || fresh.SnapshotSHA256 != c.SnapshotSHA256 {
		return nil, errors.New("candidate bundle identity changed")
	}
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	manifest, err := os.ReadFile(filepath.Join(c.BundlePath, "manifest.json"))
	if err != nil {
		return nil, err
	}
	if err = writeEntry(w, "manifest.json", manifest, 0400); err != nil {
		return nil, err
	}
	for _, a := range fresh.Manifest.Assets {
		if err = contextError(context.Background()); err != nil {
			return nil, err
		}
		raw, e := os.ReadFile(filepath.Join(c.BundlePath, filepath.FromSlash(a.Path)))
		if e != nil {
			return nil, e
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != a.SHA256 {
			return nil, errors.New("candidate changed during transfer encoding")
		}
		mode := int64(0400)
		if a.Executable {
			mode = 0500
		}
		if err = writeEntry(w, a.Path, raw, mode); err != nil {
			return nil, err
		}
		if b.Len() > MaxTransferBytes {
			return nil, errors.New("candidate transfer exceeds limit")
		}
	}
	if err = w.Close(); err != nil {
		return nil, err
	}
	if b.Len() > MaxTransferBytes {
		return nil, errors.New("candidate transfer exceeds limit")
	}
	return b.Bytes(), nil
}

func writeEntry(w *tar.Writer, name string, data []byte, mode int64) error {
	if !safePath(name) {
		return errors.New("unsafe archive path")
	}
	if err := w.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), Typeflag: tar.TypeReg, Format: tar.FormatUSTAR}); err != nil {
		return err
	}
	_, err := w.Write(data)
	return err
}

// Ingest validates every path and byte against the signed candidate manifest
// before publishing it at <root>/<candidate digest>. Existing objects are never replaced.
func (s *Store) Ingest(ctx context.Context, expectedCandidate, expectedSnapshot string, proof verification.ControlProof, r io.Reader) (verification.Candidate, error) {
	if s == nil || ctx == nil || !validHash(expectedCandidate) || !validHash(expectedSnapshot) {
		return verification.Candidate{}, errors.New("invalid artifact identity")
	}
	if err := verifyTransferAuthority(expectedCandidate, expectedSnapshot, proof, s.policy, s.spec); err != nil {
		return verification.Candidate{}, err
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxTransferBytes+1))
	if err != nil || len(data) > MaxTransferBytes {
		return verification.Candidate{}, errors.New("candidate transfer exceeds limit")
	}
	stage, err := os.MkdirTemp(s.root, ".incoming-")
	if err != nil {
		return verification.Candidate{}, err
	}
	defer os.RemoveAll(stage)
	tr := tar.NewReader(bytes.NewReader(data))
	seen := map[string]bool{}
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return verification.Candidate{}, err
		}
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil {
			return verification.Candidate{}, errors.New("invalid candidate archive")
		}
		if h.Typeflag != tar.TypeReg || h.Linkname != "" || !safePath(h.Name) || seen[h.Name] || h.Size < 0 {
			return verification.Candidate{}, errors.New("unsafe or duplicate candidate archive entry")
		}
		seen[h.Name] = true
		total += h.Size
		if total > verification.MaxCandidateCaptureBytes+verification.MaxPolicyFrameBytes {
			return verification.Candidate{}, errors.New("candidate content exceeds limit")
		}
		p := filepath.Join(stage, filepath.FromSlash(h.Name))
		if !within(stage, p) {
			return verification.Candidate{}, errors.New("candidate path escapes staging root")
		}
		if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return verification.Candidate{}, err
		}
		f, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return verification.Candidate{}, e
		}
		_, e = io.CopyN(f, tr, h.Size)
		closeErr := f.Close()
		if e != nil || closeErr != nil {
			return verification.Candidate{}, errors.New("truncated candidate archive")
		}
	}
	manifestBytes, err := os.ReadFile(filepath.Join(stage, "manifest.json"))
	if err != nil {
		return verification.Candidate{}, errors.New("candidate manifest missing")
	}
	m, err := verification.DecodeManifest(manifestBytes)
	if err != nil {
		return verification.Candidate{}, err
	}
	expected := map[string]bool{"manifest.json": true}
	for _, a := range m.Assets {
		expected[a.Path] = true
		p := filepath.Join(stage, filepath.FromSlash(a.Path))
		mode := os.FileMode(0400)
		if a.Executable {
			mode = 0500
		}
		if e := os.Chmod(p, mode); e != nil {
			return verification.Candidate{}, e
		}
	}
	if len(seen) != len(expected) {
		return verification.Candidate{}, errors.New("candidate archive inventory differs from manifest")
	}
	for n := range seen {
		if !expected[n] {
			return verification.Candidate{}, fmt.Errorf("unlisted candidate archive entry: %s", n)
		}
	}
	if err = ctx.Err(); err != nil {
		return verification.Candidate{}, err
	}
	c, err := verification.LoadCandidate(stage)
	if err != nil || c.Digest != expectedCandidate || c.SnapshotSHA256 != expectedSnapshot {
		return verification.Candidate{}, errors.New("transferred candidate digest or snapshot mismatch")
	}
	dest := filepath.Join(s.root, expectedCandidate)
	if _, err = os.Lstat(dest); err == nil {
		return verification.Candidate{}, errors.New("candidate object already exists")
	} else if !os.IsNotExist(err) {
		return verification.Candidate{}, err
	}
	if err = os.Rename(stage, dest); err != nil {
		return verification.Candidate{}, errors.New("candidate object publish failed")
	}
	return verification.LoadCandidate(dest)
}

func verifyTransferAuthority(candidate, snapshot string, proof verification.ControlProof, policy verification.VerificationPolicy, spec verification.VerifierSpec) error {
	if err := verification.ValidatePolicy(policy); err != nil {
		return errors.New("artifact transfer policy is invalid")
	}
	pd, err := verification.Digest(policy)
	if err != nil {
		return err
	}
	sd, err := verification.Digest(spec)
	if err != nil {
		return err
	}
	var pinned *verification.VerifierSpec
	for i := range policy.Verifiers {
		if policy.Verifiers[i].ID == spec.ID {
			pinned = &policy.Verifiers[i]
		}
	}
	expectedSpec, err := verification.Digest(spec)
	if err != nil || pinned == nil {
		return errors.New("artifact transfer verifier is not pinned")
	}
	pinnedDigest, _ := verification.Digest(*pinned)
	if pinnedDigest != expectedSpec || proof.CandidateSHA256 != candidate || proof.SnapshotSHA256 != snapshot || proof.Scope != "candidate-offline" {
		return errors.New("artifact transfer control proof binding mismatch")
	}
	observed, err := time.Parse(time.RFC3339, proof.ObservedAt)
	if err != nil {
		return errors.New("artifact transfer proof timestamp invalid")
	}
	until, err := time.Parse(time.RFC3339, proof.ValidUntil)
	now := time.Now().UTC()
	if err != nil || observed.After(now) || now.After(until) || !until.After(observed) || until.Sub(observed) > 5*time.Minute {
		return errors.New("artifact transfer proof is expired or overlong")
	}
	if err = verification.VerifyControlProof(proof, pd, sd, policy.Issuers, now); err != nil {
		return errors.New("artifact transfer control proof is not trusted")
	}
	return nil
}

func (s *Store) Get(ctx context.Context, candidateDigest, snapshotDigest string) (verification.Candidate, error) {
	if s == nil || ctx == nil || ctx.Err() != nil || !validHash(candidateDigest) || !validHash(snapshotDigest) {
		return verification.Candidate{}, errors.New("invalid artifact reference")
	}
	p := filepath.Join(s.root, candidateDigest)
	if err := rejectLinks(p); err != nil {
		return verification.Candidate{}, err
	}
	c, err := verification.LoadCandidate(p)
	if err != nil || c.Digest != candidateDigest || c.SnapshotSHA256 != snapshotDigest {
		return verification.Candidate{}, errors.New("immutable candidate reference unavailable")
	}
	return c, nil
}

func safePath(p string) bool {
	return p != "" && !strings.Contains(p, "\\") && !filepath.IsAbs(p) && filepath.ToSlash(filepath.Clean(p)) == p && p != "." && !strings.HasPrefix(p, "../")
}
func validHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func within(root, p string) bool {
	rel, e := filepath.Rel(root, p)
	return e == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func rejectLinks(p string) error {
	abs, e := filepath.Abs(p)
	if e != nil {
		return e
	}
	cur := filepath.VolumeName(abs) + string(filepath.Separator)
	rest := strings.TrimPrefix(abs, cur)
	for _, part := range strings.Split(rest, string(filepath.Separator)) {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		st, e := os.Lstat(cur)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink in artifact store path")
		}
	}
	return nil
}
func contextError(ctx context.Context) error { return ctx.Err() }
