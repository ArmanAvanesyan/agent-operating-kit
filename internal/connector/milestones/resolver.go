// Package milestones resolves writeback dependencies to immutable, locally
// revalidated P8 evidence. It does not grant authority or publish anything.
package milestones

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/deliverybridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

// CandidateScope pins the execution lineage acceptance must cover.
type CandidateScope struct {
	RunID, StepID, AttemptID                        string
	Fence                                           uint64
	InputSHA256, ConfigurationSHA256, ProfileSHA256 string
}

// Destination is the protected writeback target bound to both the operation
// and each evidence reference. API and delivery milestone come from the
// operator's protected route configuration.
type Destination struct {
	ProviderOrigin    string                   `json:"providerOrigin"`
	Scope             contracts.RemoteIdentity `json:"scope"`
	Operation         string                   `json:"operation"`
	OperationID       string                   `json:"operationId"`
	OperationDigest   string                   `json:"operationDigest"`
	API               string                   `json:"api"`
	AuthRef           string                   `json:"authRef"`
	Principal         string                   `json:"principal"`
	DeliveryMilestone string                   `json:"deliveryMilestone"`
}

type ArtifactRef struct {
	Milestone       string         `json:"milestone"`
	Kind            string         `json:"kind"`
	ID              string         `json:"id"`
	Digest          string         `json:"digest"`
	Issuer          string         `json:"issuer"`
	EvidencePath    string         `json:"evidencePath"`
	CandidateBundle string         `json:"candidateBundle"`
	Scope           CandidateScope `json:"scope"`
	Destination     Destination    `json:"destination"`
}

// CurrentProjectionVerifier must consult the authoritative current runtime
// projection, including revocation and lineage, on every resolution.
type CurrentProjectionVerifier interface {
	VerifyCurrentProjection(ArtifactRef, verification.Candidate, verification.AcceptanceReceipt, time.Time) error
}

// QualifiedPublicationVerifier is required for delivered milestones. Local
// handoff validation alone is not proof of remote publication/readback.
type QualifiedPublicationVerifier interface {
	VerifyPublished(ArtifactRef, deliverybridge.HandoffManifest, time.Time) error
}

type Config struct {
	Root           string
	Policy         verification.VerificationPolicy
	TrustedIssuers []verification.TrustedIssuer
	MaxAge         time.Duration
	Artifacts      []ArtifactRef
	Target         Destination
	Projection     CurrentProjectionVerifier
	Publication    QualifiedPublicationVerifier
	Clock          func() time.Time
}

type Resolver struct {
	cfg   Config
	index map[string]ArtifactRef
}

func NewResolver(c Config) (*Resolver, error) {
	if c.Root == "" || c.MaxAge <= 0 || c.Projection == nil {
		return nil, errors.New("root, finite age limit, and current projection verifier are required")
	}
	if err := verification.ValidatePolicy(c.Policy); err != nil {
		return nil, fmt.Errorf("external verification policy: %w", err)
	}
	if len(c.TrustedIssuers) == 0 {
		return nil, errors.New("externally supplied trusted issuers are required")
	}
	if err := validateDestination(c.Target); err != nil {
		return nil, fmt.Errorf("protected target: %w", err)
	}
	if !sameIssuers(c.Policy.Issuers, c.TrustedIssuers) {
		return nil, errors.New("policy issuer roots differ from external trusted issuers")
	}
	if time.Duration(c.Policy.MaxAgeSeconds)*time.Second > c.MaxAge {
		return nil, errors.New("policy age exceeds external maximum")
	}
	root, err := filepath.Abs(c.Root)
	if err != nil {
		return nil, err
	}
	c.Root = root
	if c.Clock == nil {
		c.Clock = func() time.Time { return time.Now().UTC() }
	}
	r := &Resolver{cfg: c, index: map[string]ArtifactRef{}}
	for _, a := range c.Artifacts {
		if err := validateRef(a); err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(a.Destination, c.Target) {
			return nil, errors.New("milestone artifact destination differs from protected target")
		}
		k := key(a.Milestone, a.Kind, a.ID, a.Digest, a.Issuer)
		if _, exists := r.index[k]; exists {
			return nil, errors.New("duplicate milestone evidence identity")
		}
		r.index[k] = a
	}
	return r, nil
}

// EvidenceVerifierFor creates a closure pinned to one exact signed operation
// and grant. The returned verifier refuses dependencies prepared for another
// target even when the dependency identifiers happen to match.
func (r *Resolver) EvidenceVerifierFor(op contracts.SyncOperation, grant writeback.Grant) (writeback.EvidenceVerifier, error) {
	if r == nil {
		return nil, errors.New("milestone resolver is unavailable")
	}
	t := r.cfg.Target
	if op.OperationID != t.OperationID || op.OperationDigest != t.OperationDigest || op.OperationKind != t.Operation ||
		grant.OperationID != t.OperationID || grant.OperationDigest != t.OperationDigest || grant.Operation != t.Operation ||
		grant.ID != op.Authority.Reference || grant.Scope != t.Scope || op.Authority.Scope != t.Scope ||
		grant.ProviderOrigin != t.ProviderOrigin || grant.AuthRef != t.AuthRef || grant.Principal != t.Principal ||
		op.Authority.Operation != t.Operation || op.Authority.OperationDigest != t.OperationDigest {
		return nil, errors.New("writeback operation/grant does not match protected milestone destination")
	}
	return func(dep writeback.EvidenceDependency) error { return r.verifyForTarget(dep, t) }, nil
}

// EvidenceVerifier intentionally cannot provide an unbound closure.
func (r *Resolver) EvidenceVerifier() writeback.EvidenceVerifier {
	return func(writeback.EvidenceDependency) error {
		return errors.New("operation-bound milestone verifier is required")
	}
}

func (r *Resolver) Verify(dep writeback.EvidenceDependency) error {
	return errors.New("operation-bound milestone verifier is required")
}

func (r *Resolver) verifyForTarget(dep writeback.EvidenceDependency, target Destination) error {
	if r == nil {
		return errors.New("milestone resolver is unavailable")
	}
	a, ok := r.index[key(dep.Milestone, dep.Kind, dep.ID, dep.Digest, dep.Issuer)]
	if !ok {
		return errors.New("milestone dependency does not match a trusted immutable evidence reference")
	}
	if !reflect.DeepEqual(a.Destination, target) {
		return errors.New("milestone evidence destination does not match protected operation target")
	}
	now := r.cfg.Clock().UTC()
	if now.IsZero() {
		return errors.New("trusted current time is required")
	}
	if err := noSymlinkPath(r.cfg.Root, a.EvidencePath); err != nil {
		return err
	}
	if a.Kind == "acceptance" {
		return r.verifyAcceptance(a, now)
	}
	if a.Kind == "delivery" {
		if r.cfg.Publication == nil {
			return errors.New("qualified remote publication/readback verifier is unavailable")
		}
		return r.verifyDelivery(a, now)
	}
	return errors.New("unsupported milestone evidence kind")
}

func (r *Resolver) verifyAcceptance(a ArtifactRef, now time.Time) error {
	b, err := modelcatalog.ReadAsset(r.cfg.Root, a.EvidencePath)
	if err != nil {
		return err
	}
	if hash(b) != a.Digest {
		return errors.New("acceptance evidence digest changed")
	}
	var receipt verification.AcceptanceReceipt
	if err := strictJSON(b, &receipt); err != nil {
		return err
	}
	if receipt.ID != a.ID || receipt.Issuer != a.Issuer {
		return errors.New("acceptance identity or issuer mismatch")
	}
	if err := verification.VerifyAcceptance(receipt, r.cfg.TrustedIssuers, now, r.cfg.MaxAge); err != nil {
		return fmt.Errorf("acceptance signature or freshness: %w", err)
	}
	if err := noSymlinkPath(r.cfg.Root, a.CandidateBundle); err != nil {
		return err
	}
	candidate, err := verification.LoadCandidate(filepath.Join(r.cfg.Root, filepath.FromSlash(a.CandidateBundle)))
	if err != nil {
		return err
	}
	if err = matchScope(candidate.Manifest, a.Scope); err != nil {
		return err
	}
	if err = verification.ValidateAcceptanceManifestBinding(receipt, candidate.Manifest, candidate.Digest, candidate.SnapshotSHA256, r.cfg.Policy); err != nil {
		return fmt.Errorf("acceptance candidate binding: %w", err)
	}
	if err = r.cfg.Projection.VerifyCurrentProjection(a, candidate, receipt, now); err != nil {
		return fmt.Errorf("current runtime projection: %w", err)
	}
	return nil
}

func (r *Resolver) verifyDelivery(a ArtifactRef, now time.Time) error {
	b, err := modelcatalog.ReadAsset(r.cfg.Root, a.EvidencePath)
	if err != nil {
		return err
	}
	if hash(b) != a.Digest {
		return errors.New("delivery evidence digest changed")
	}
	var handoff deliverybridge.HandoffManifest
	if err := strictJSON(b, &handoff); err != nil {
		return err
	}
	if handoff.ID != a.ID || handoff.Acceptance.Issuer != a.Issuer {
		return errors.New("delivery identity or issuer mismatch")
	}
	// Evidence cannot select its own policy or candidate path.
	policyDigest, err := verification.Digest(handoff.Policy)
	if err != nil {
		return err
	}
	trustedDigest, err := verification.Digest(r.cfg.Policy)
	if err != nil {
		return err
	}
	if policyDigest != trustedDigest {
		return errors.New("delivery embedded policy differs from external policy")
	}
	handoff.Policy = r.cfg.Policy
	handoff.AsOf = now
	if err := noSymlinkPath(r.cfg.Root, a.CandidateBundle); err != nil {
		return err
	}
	handoff.Candidate, err = verification.LoadCandidate(filepath.Join(r.cfg.Root, filepath.FromSlash(a.CandidateBundle)))
	if err != nil {
		return err
	}
	if err = matchScope(handoff.Candidate.Manifest, a.Scope); err != nil {
		return err
	}
	_, report := deliverybridge.Validate(handoff)
	if len(report.Diagnostics) != 0 {
		return fmt.Errorf("local delivery evidence invalid: %s", strings.Join(report.Diagnostics, "; "))
	}
	if err = verification.VerifyAcceptance(handoff.Acceptance, r.cfg.TrustedIssuers, now, r.cfg.MaxAge); err != nil {
		return fmt.Errorf("delivery acceptance signature or freshness: %w", err)
	}
	if err = r.cfg.Projection.VerifyCurrentProjection(a, handoff.Candidate, handoff.Acceptance, now); err != nil {
		return fmt.Errorf("current runtime projection: %w", err)
	}
	if err = r.cfg.Publication.VerifyPublished(a, handoff, now); err != nil {
		return fmt.Errorf("publication/readback evidence: %w", err)
	}
	return nil
}

func matchScope(m verification.CandidateManifest, s CandidateScope) error {
	if s.RunID == "" || s.StepID == "" || s.AttemptID == "" || s.Fence == 0 || !isHash(s.InputSHA256) || !isHash(s.ConfigurationSHA256) || !isHash(s.ProfileSHA256) {
		return errors.New("candidate scope is incomplete")
	}
	if m.RunID != s.RunID || m.StepID != s.StepID || m.AttemptID != s.AttemptID || m.Fence != s.Fence || m.InputSHA256 != s.InputSHA256 || m.ConfigurationSHA256 != s.ConfigurationSHA256 || m.ProfileSHA256 != s.ProfileSHA256 {
		return errors.New("candidate lineage does not match exact milestone scope")
	}
	return nil
}

func validateRef(a ArtifactRef) error {
	if a.Milestone == "" || a.ID == "" || a.Issuer == "" || !isHash(a.Digest) {
		return errors.New("incomplete milestone evidence reference")
	}
	if a.Kind != "acceptance" && a.Kind != "delivery" {
		return errors.New("unsupported milestone evidence kind")
	}
	if (a.Kind == "acceptance" && a.Milestone != "accepted") || (a.Kind == "delivery" && a.Milestone != "delivered") {
		return errors.New("milestone name does not match evidence kind")
	}
	for _, p := range []string{a.EvidencePath, a.CandidateBundle} {
		if p == "" || filepath.IsAbs(p) || filepath.Clean(p) != p || p == "." || p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
			return errors.New("evidence paths must be safe relative paths")
		}
	}
	return nil
}

func validateDestination(d Destination) error {
	if d.ProviderOrigin == "" || d.Scope.Provider == "" || d.Scope.TenantID == "" || d.Scope.ContainerID == "" || d.Scope.RemoteID == "" || d.Scope.ItemKind == "" || d.Operation == "" || d.OperationID == "" || !isHash(d.OperationDigest) || d.API == "" || d.AuthRef == "" || d.Principal == "" || d.DeliveryMilestone == "" {
		return errors.New("provider origin, full remote identity, operation, API, auth, principal, and delivery milestone are required")
	}
	return nil
}

func noSymlinkPath(root, rel string) error {
	if filepath.IsAbs(rel) || filepath.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("unsafe evidence path")
	}
	for p := filepath.Join(root, rel); ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return errors.New("evidence path contains symlink")
		}
		if p == root || filepath.Dir(p) == p {
			break
		}
	}
	return nil
}
func strictJSON(b []byte, v any) error {
	if err := contracts.ValidateJSON(b); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
func sameIssuers(a, b []verification.TrustedIssuer) bool { return reflect.DeepEqual(a, b) }
func key(parts ...string) string                         { return strings.Join(parts, "\x00") }
func hash(b []byte) string                               { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
