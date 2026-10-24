// Package writeback contains offline grant verification and a private durable
// outbox. It deliberately has no provider transport.
package writeback

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"database/sql"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	_ "modernc.org/sqlite"
)

const APIVersion = "aok-writeback/v1"

type Grant struct {
	APIVersion           string                   `json:"apiVersion"`
	Kind                 string                   `json:"kind"`
	ID                   string                   `json:"id"`
	Issuer               string                   `json:"issuer"`
	Principal            string                   `json:"principal"`
	AuthRef              string                   `json:"authRef"`
	IssuedAt             string                   `json:"issuedAt"`
	ProviderOrigin       string                   `json:"providerOrigin"`
	DraftDigest          string                   `json:"draftDigest"`
	SnapshotHash         string                   `json:"snapshotHash"`
	SnapshotObservedAt   string                   `json:"snapshotObservedAt"`
	MappingVersion       string                   `json:"mappingVersion"`
	EvidenceDependencies []EvidenceDependency     `json:"evidenceDependencies"`
	Scope                contracts.RemoteIdentity `json:"scope"`
	Operation            string                   `json:"operation"`
	OperationID          string                   `json:"operationId"`
	OperationDigest      string                   `json:"operationDigest"`
	Capability           contracts.Reference      `json:"capability"`
	Binding              contracts.Reference      `json:"binding"`
	ExpectedRevision     string                   `json:"expectedRevision"`
	Nonce                string                   `json:"nonce"`
	ExpiresAt            string                   `json:"expiresAt"`
	RevocationGeneration uint64                   `json:"revocationGeneration"`
	Signature            string                   `json:"signature"`
}
type TrustPolicy struct {
	APIVersion         string            `json:"apiVersion"`
	Kind               string            `json:"kind"`
	Keys               map[string]string `json:"keys"`
	MaxGrantTTLSeconds int64             `json:"maxGrantTtlSeconds"`
	ProviderOrigins    []string          `json:"providerOrigins"`
	Principals         []string          `json:"principals"`
	AuthRefs           []string          `json:"authRefs"`
	CompletionRules    []CompletionRule  `json:"completionRules"`
	ReadbackIssuers    []string          `json:"readbackIssuers"`
	ProviderRoutes     []ProviderRoute   `json:"providerRoutes"`
	FieldRules         []NativeFieldRule `json:"fieldRules"`
}
type ProviderRoute struct {
	Provider string `json:"provider"`
	Origin   string `json:"origin"`
}
type NativeFieldRule struct {
	Provider           string            `json:"provider"`
	FieldID            string            `json:"fieldId"`
	AllowedValues      []json.RawMessage `json:"allowedValues"`
	CompletionValues   []json.RawMessage `json:"completionValues,omitempty"`
	CompletionAllowed  bool              `json:"completionAllowed"`
	RequiredMilestones []string          `json:"requiredMilestones,omitempty"`
}
type CompletionRule struct {
	Provider           string            `json:"provider"`
	FieldID            string            `json:"fieldId,omitempty"`
	TransitionID       string            `json:"transitionId,omitempty"`
	CompletionValues   []json.RawMessage `json:"completionValues,omitempty"`
	Allowed            bool              `json:"allowed"`
	RequiredMilestones []string          `json:"requiredMilestones"`
}
type EvidenceDependency struct {
	Milestone string `json:"milestone"`
	Kind      string `json:"kind"`
	ID        string `json:"id"`
	Digest    string `json:"digest"`
	Issuer    string `json:"issuer"`
}
type GrantContext struct {
	DraftDigest    string
	SnapshotHash   string
	MappingVersion string
	ObservedAt     time.Time
}
type EvidenceVerifier func(EvidenceDependency) error
type ReconciliationReport struct {
	APIVersion      string                   `json:"apiVersion"`
	Kind            string                   `json:"kind"`
	Issuer          string                   `json:"issuer"`
	OperationID     string                   `json:"operationId"`
	OperationDigest string                   `json:"operationDigest"`
	Identity        contracts.RemoteIdentity `json:"identity"`
	ObservedAt      string                   `json:"observedAt"`
	Disposition     string                   `json:"disposition"`
	Evidence        []contracts.AssetRef     `json:"evidence"`
	Signature       string                   `json:"signature"`
}
type signedReconciliation struct {
	APIVersion      string                   `json:"apiVersion"`
	Kind            string                   `json:"kind"`
	Issuer          string                   `json:"issuer"`
	OperationID     string                   `json:"operationId"`
	OperationDigest string                   `json:"operationDigest"`
	Identity        contracts.RemoteIdentity `json:"identity"`
	ObservedAt      string                   `json:"observedAt"`
	Disposition     string                   `json:"disposition"`
	Evidence        []contracts.AssetRef     `json:"evidence"`
}
type VerifiedReconciliation struct {
	operationID, digest string
	identity            contracts.RemoteIdentity
	disposition, detail string
}

func ReconciliationSigningBytes(r ReconciliationReport) ([]byte, error) {
	return json.Marshal(signedReconciliation{r.APIVersion, r.Kind, r.Issuer, r.OperationID, r.OperationDigest, r.Identity, r.ObservedAt, r.Disposition, r.Evidence})
}
func VerifyReconciliation(r ReconciliationReport, p TrustPolicy, op contracts.SyncOperation, id contracts.RemoteIdentity, now time.Time) (VerifiedReconciliation, error) {
	if r.APIVersion != APIVersion || r.Kind != "ReconciliationReport" || p.APIVersion != APIVersion || p.Kind != "TrustPolicy" {
		return VerifiedReconciliation{}, errors.New("invalid reconciliation version or kind")
	}
	if !contains(p.ReadbackIssuers, r.Issuer) {
		return VerifiedReconciliation{}, errors.New("untrusted readback issuer")
	}
	key, e := hex.DecodeString(p.Keys[r.Issuer])
	if e != nil || len(key) != ed25519.PublicKeySize {
		return VerifiedReconciliation{}, errors.New("invalid readback key")
	}
	b, e := ReconciliationSigningBytes(r)
	if e != nil {
		return VerifiedReconciliation{}, e
	}
	sig, e := hex.DecodeString(r.Signature)
	if e != nil || !ed25519.Verify(key, b, sig) {
		return VerifiedReconciliation{}, errors.New("invalid reconciliation signature")
	}
	observed, e := time.Parse(time.RFC3339Nano, r.ObservedAt)
	if e != nil || now.Before(observed) || now.Sub(observed) > 5*time.Minute {
		return VerifiedReconciliation{}, errors.New("readback evidence is stale or future dated")
	}
	if r.OperationID != op.OperationID || r.OperationDigest != op.OperationDigest || r.Identity != id {
		return VerifiedReconciliation{}, errors.New("readback does not match exact operation and identity")
	}
	if r.Disposition != "readback-verified" && r.Disposition != "conflict" && r.Disposition != "unknown-awaiting-reconciliation" {
		return VerifiedReconciliation{}, errors.New("invalid readback disposition")
	}
	if len(r.Evidence) == 0 {
		return VerifiedReconciliation{}, errors.New("readback requires evidence assets")
	}
	for _, a := range r.Evidence {
		if a.Path == "" || !validDigest(a.SHA256) {
			return VerifiedReconciliation{}, errors.New("invalid readback evidence reference")
		}
	}
	return VerifiedReconciliation{r.OperationID, r.OperationDigest, r.Identity, r.Disposition, DigestBytes(b)}, nil
}

type RevocationSnapshot struct {
	APIVersion      string   `json:"apiVersion"`
	Kind            string   `json:"kind"`
	Issuer          string   `json:"issuer"`
	Generation      uint64   `json:"generation"`
	IssuedAt        string   `json:"issuedAt"`
	RevokedGrantIDs []string `json:"revokedGrantIds"`
	RevokedNonces   []string `json:"revokedNonces"`
	Signature       string   `json:"signature"`
}
type signedRevocation struct {
	APIVersion      string   `json:"apiVersion"`
	Kind            string   `json:"kind"`
	Issuer          string   `json:"issuer"`
	Generation      uint64   `json:"generation"`
	IssuedAt        string   `json:"issuedAt"`
	RevokedGrantIDs []string `json:"revokedGrantIds"`
	RevokedNonces   []string `json:"revokedNonces"`
}
type signedGrant struct {
	APIVersion           string                   `json:"apiVersion"`
	Kind                 string                   `json:"kind"`
	ID                   string                   `json:"id"`
	Issuer               string                   `json:"issuer"`
	Principal            string                   `json:"principal"`
	AuthRef              string                   `json:"authRef"`
	IssuedAt             string                   `json:"issuedAt"`
	ProviderOrigin       string                   `json:"providerOrigin"`
	DraftDigest          string                   `json:"draftDigest"`
	SnapshotHash         string                   `json:"snapshotHash"`
	SnapshotObservedAt   string                   `json:"snapshotObservedAt"`
	MappingVersion       string                   `json:"mappingVersion"`
	EvidenceDependencies []EvidenceDependency     `json:"evidenceDependencies"`
	Scope                contracts.RemoteIdentity `json:"scope"`
	Operation            string                   `json:"operation"`
	OperationID          string                   `json:"operationId"`
	OperationDigest      string                   `json:"operationDigest"`
	Capability           contracts.Reference      `json:"capability"`
	Binding              contracts.Reference      `json:"binding"`
	ExpectedRevision     string                   `json:"expectedRevision"`
	Nonce                string                   `json:"nonce"`
	ExpiresAt            string                   `json:"expiresAt"`
	RevocationGeneration uint64                   `json:"revocationGeneration"`
}

// VerifiedGrant is proof that all protected inputs matched the exact operation.
// Its fields are private so callers cannot manufacture authorization by copying declarations.
type VerifiedGrant struct {
	id, issuer, digest, nonce, principal, authRef, operation string
	grantHash, policyHash, origin                            string
	scope                                                    contracts.RemoteIdentity
	binding, capability                                      contracts.Reference
	expectedRevision                                         string
	generation, sourceGeneration                             uint64
	expires                                                  time.Time
}

func (v VerifiedGrant) ID() string                      { return v.id }
func (v VerifiedGrant) OperationDigest() string         { return v.digest }
func (v VerifiedGrant) Issuer() string                  { return v.issuer }
func (v VerifiedGrant) Principal() string               { return v.principal }
func (v VerifiedGrant) AuthRef() string                 { return v.authRef }
func (v VerifiedGrant) Scope() contracts.RemoteIdentity { return v.scope }
func (v VerifiedGrant) Operation() string               { return v.operation }
func (v VerifiedGrant) Binding() contracts.Reference    { return v.binding }
func (v VerifiedGrant) Capability() contracts.Reference { return v.capability }
func (v VerifiedGrant) ExpectedRevision() string        { return v.expectedRevision }
func (v VerifiedGrant) ExpiresAt() time.Time            { return v.expires }
func (v VerifiedGrant) RevocationGeneration() uint64    { return v.generation }
func (v VerifiedGrant) GrantHash() string               { return v.grantHash }
func (v VerifiedGrant) PolicyHash() string              { return v.policyHash }
func (v VerifiedGrant) ProviderOrigin() string          { return v.origin }

func GrantSigningBytes(g Grant) ([]byte, error) {
	return json.Marshal(signedGrant{APIVersion: g.APIVersion, Kind: g.Kind, ID: g.ID, Issuer: g.Issuer, Principal: g.Principal, AuthRef: g.AuthRef, IssuedAt: g.IssuedAt, ProviderOrigin: g.ProviderOrigin, DraftDigest: g.DraftDigest, SnapshotHash: g.SnapshotHash, SnapshotObservedAt: g.SnapshotObservedAt, MappingVersion: g.MappingVersion, EvidenceDependencies: g.EvidenceDependencies, Scope: g.Scope, Operation: g.Operation, OperationID: g.OperationID, OperationDigest: g.OperationDigest, Capability: g.Capability, Binding: g.Binding, ExpectedRevision: g.ExpectedRevision, Nonce: g.Nonce, ExpiresAt: g.ExpiresAt, RevocationGeneration: g.RevocationGeneration})
}
func RevocationSigningBytes(r RevocationSnapshot) ([]byte, error) {
	return json.Marshal(signedRevocation{r.APIVersion, r.Kind, r.Issuer, r.Generation, r.IssuedAt, r.RevokedGrantIDs, r.RevokedNonces})
}

func VerifyGrant(g Grant, p TrustPolicy, r RevocationSnapshot, op contracts.SyncOperation, cap contracts.ConnectorCapability, id contracts.RemoteIdentity, now time.Time) (VerifiedGrant, error) {
	return verifyGrant(g, p, r, op, cap, id, GrantContext{}, nil, now, false)
}

// VerifyGrantBound additionally binds fresh draft, source snapshot and mapping inputs,
// and verifies every milestone reference against an external protected evidence store.
func VerifyGrantBound(g Grant, p TrustPolicy, r RevocationSnapshot, op contracts.SyncOperation, cap contracts.ConnectorCapability, id contracts.RemoteIdentity, current GrantContext, verifyEvidence EvidenceVerifier, now time.Time) (VerifiedGrant, error) {
	return verifyGrant(g, p, r, op, cap, id, current, verifyEvidence, now, true)
}

func verifyGrant(g Grant, p TrustPolicy, r RevocationSnapshot, op contracts.SyncOperation, cap contracts.ConnectorCapability, id contracts.RemoteIdentity, current GrantContext, verifyEvidence EvidenceVerifier, now time.Time, bound bool) (VerifiedGrant, error) {
	if g.APIVersion != APIVersion || g.Kind != "WriteGrant" || p.APIVersion != APIVersion || p.Kind != "TrustPolicy" || r.APIVersion != APIVersion || r.Kind != "RevocationSnapshot" {
		return VerifiedGrant{}, errors.New("unknown or malformed writeback version/kind")
	}
	if g.ID == "" || g.Issuer == "" || g.Principal == "" || g.AuthRef == "" || g.Nonce == "" || g.RevocationGeneration == 0 || g.ProviderOrigin == "" || g.IssuedAt == "" {
		return VerifiedGrant{}, errors.New("grant required fields missing")
	}
	keyText, ok := p.Keys[g.Issuer]
	if !ok {
		return VerifiedGrant{}, errors.New("grant issuer is not trusted")
	}
	key, e := hex.DecodeString(keyText)
	if e != nil || len(key) != ed25519.PublicKeySize {
		return VerifiedGrant{}, errors.New("invalid trusted issuer key")
	}
	msg, e := GrantSigningBytes(g)
	if e != nil {
		return VerifiedGrant{}, e
	}
	sig, e := hex.DecodeString(g.Signature)
	if e != nil || !ed25519.Verify(key, msg, sig) {
		return VerifiedGrant{}, errors.New("invalid grant signature")
	}
	grantHash := DigestBytes(msg)
	policyBytes, e := json.Marshal(p)
	if e != nil {
		return VerifiedGrant{}, e
	}
	policyHash := DigestBytes(policyBytes)
	rkeyText, ok := p.Keys[r.Issuer]
	if !ok {
		return VerifiedGrant{}, errors.New("revocation issuer is not trusted")
	}
	rkey, e := hex.DecodeString(rkeyText)
	rm, _ := RevocationSigningBytes(r)
	rs, re := hex.DecodeString(r.Signature)
	if e != nil || len(rkey) != ed25519.PublicKeySize || re != nil || !ed25519.Verify(rkey, rm, rs) {
		return VerifiedGrant{}, errors.New("invalid revocation signature")
	}
	issued, e := time.Parse(time.RFC3339Nano, r.IssuedAt)
	if e != nil || now.Before(issued) || now.Sub(issued) > 5*time.Minute || r.Generation < g.RevocationGeneration {
		return VerifiedGrant{}, errors.New("revocation state is stale, regressed, or unavailable")
	}
	if r.Issuer != g.Issuer {
		return VerifiedGrant{}, errors.New("revocation issuer mismatch")
	}
	for _, x := range r.RevokedGrantIDs {
		if x == g.ID {
			return VerifiedGrant{}, errors.New("grant revoked")
		}
	}
	for _, x := range r.RevokedNonces {
		if x == g.Nonce {
			return VerifiedGrant{}, errors.New("grant nonce revoked")
		}
	}
	expires, e := time.Parse(time.RFC3339Nano, g.ExpiresAt)
	if e != nil || !now.Before(expires) {
		return VerifiedGrant{}, errors.New("grant expired")
	}
	issuedGrant, ie := time.Parse(time.RFC3339Nano, g.IssuedAt)
	if ie != nil || issuedGrant.After(now) || p.MaxGrantTTLSeconds <= 0 || expires.Sub(issuedGrant) > time.Duration(p.MaxGrantTTLSeconds)*time.Second {
		return VerifiedGrant{}, errors.New("grant exceeds policy TTL")
	}
	if !contains(p.ProviderOrigins, g.ProviderOrigin) || !routeAllowed(p.ProviderRoutes, id.Provider, g.ProviderOrigin) || !contains(p.Principals, g.Principal) || !contains(p.AuthRefs, g.AuthRef) {
		return VerifiedGrant{}, errors.New("grant origin, principal, or auth reference is outside protected policy")
	}
	if bound {
		observed, oe := time.Parse(time.RFC3339Nano, g.SnapshotObservedAt)
		if !validDigest(g.DraftDigest) || !validDigest(g.SnapshotHash) || g.MappingVersion == "" || g.DraftDigest != current.DraftDigest || g.SnapshotHash != current.SnapshotHash || g.MappingVersion != current.MappingVersion || oe != nil || current.ObservedAt.IsZero() || !observed.Equal(current.ObservedAt) || now.Before(observed) || now.Sub(observed) > 5*time.Minute {
			return VerifiedGrant{}, errors.New("grant is stale or does not bind the current draft, snapshot, and mapping")
		}
	} else if isCompletionOperation(op, p, id.Provider) {
		return VerifiedGrant{}, errors.New("completion operation requires VerifyGrantBound and externally validated milestone evidence")
	}
	if err := validateCompletionGrant(op, p, g, id.Provider, bound, verifyEvidence); err != nil {
		return VerifiedGrant{}, err
	}
	want, e := contracts.OperationDigest(op.WorkItemBindingRef, id, op.ConnectorCapabilityRef, op.OperationKind, op.ExpectedRevision, op.Change)
	if e != nil {
		return VerifiedGrant{}, e
	}
	if g.Principal == "" || g.Scope != id || g.Operation != op.OperationKind || g.OperationID != op.OperationID || g.OperationDigest != want || op.OperationDigest != want || g.ExpectedRevision != op.ExpectedRevision || g.Binding != op.WorkItemBindingRef || g.Capability != op.ConnectorCapabilityRef || cap.Metadata.ID != g.Capability.ID || cap.Metadata.Version != g.Capability.Version {
		return VerifiedGrant{}, errors.New("grant does not cover exact operation scope and precondition")
	}
	return VerifiedGrant{id: g.ID, issuer: g.Issuer, digest: want, nonce: g.Nonce, principal: g.Principal, authRef: g.AuthRef, operation: g.Operation, scope: g.Scope, binding: g.Binding, capability: g.Capability, expectedRevision: g.ExpectedRevision, generation: r.Generation, sourceGeneration: g.RevocationGeneration, expires: expires, grantHash: grantHash, policyHash: policyHash, origin: g.ProviderOrigin}, nil
}

func validDigest(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == sha256.Size && s == strings.ToLower(s)
}
func isCompletionOperation(op contracts.SyncOperation, p TrustPolicy, provider string) bool {
	if op.OperationKind == "transition" {
		return true
	}
	if op.OperationKind != "update-fields" {
		return false
	}
	for _, f := range op.Change.Fields {
		for _, r := range p.FieldRules {
			if r.Provider == provider && r.FieldID == f.ProviderFieldID && isCompletionValue(f.Value, r.CompletionValues) {
				return true
			}
		}
	}
	return false
}
func isCompletionValue(value json.RawMessage, values []json.RawMessage) bool {
	var a bytes.Buffer
	if json.Compact(&a, value) != nil {
		return false
	}
	for _, v := range values {
		var b bytes.Buffer
		if json.Compact(&b, v) == nil && bytes.Equal(a.Bytes(), b.Bytes()) {
			return true
		}
	}
	return false
}
func validateCompletionGrant(op contracts.SyncOperation, p TrustPolicy, g Grant, provider string, bound bool, verify EvidenceVerifier) error {
	var match *CompletionRule
	if op.OperationKind == "transition" {
		for i := range p.CompletionRules {
			r := &p.CompletionRules[i]
			if r.Provider == provider && r.TransitionID == op.Change.TransitionID {
				match = r
				break
			}
		}
		if match == nil {
			return errors.New("transition lacks an exact native completion policy")
		}
	}
	if op.OperationKind == "update-fields" {
		for _, f := range op.Change.Fields {
			var rule *NativeFieldRule
			for i := range p.FieldRules {
				r := &p.FieldRules[i]
				if r.Provider == provider && r.FieldID == f.ProviderFieldID {
					rule = r
					break
				}
			}
			if rule == nil {
				return fmt.Errorf("native field %q is not explicitly mapped by write policy", f.ProviderFieldID)
			}
			if isCompletionValue(f.Value, rule.CompletionValues) {
				if !rule.CompletionAllowed {
					return errors.New("completion field value is disabled by policy")
				}
				if err := verifyMilestones(rule.RequiredMilestones, g, bound, verify); err != nil {
					return err
				}
			} else if !isCompletionValue(f.Value, rule.AllowedValues) {
				return fmt.Errorf("native field %q value is not allowed by policy", f.ProviderFieldID)
			}
		}
	}
	if match == nil {
		return nil
	}
	if !match.Allowed {
		return errors.New("completion transition is disabled by policy")
	}
	return verifyMilestones(match.RequiredMilestones, g, bound, verify)
}
func routeAllowed(routes []ProviderRoute, provider, origin string) bool {
	for _, r := range routes {
		if r.Provider == provider && r.Origin == origin {
			return true
		}
	}
	return false
}
func verifyMilestones(required []string, g Grant, bound bool, verify EvidenceVerifier) error {
	if !bound || verify == nil || len(required) == 0 {
		return errors.New("completion requires bound authorization and external milestone validation")
	}
	seen := map[string]bool{}
	for _, d := range g.EvidenceDependencies {
		if d.Milestone == "" || d.Kind == "" || d.ID == "" || !validDigest(d.Digest) || d.Issuer == "" || seen[d.Milestone] {
			return errors.New("invalid or duplicate evidence dependency")
		}
		seen[d.Milestone] = true
		if err := verify(d); err != nil {
			return fmt.Errorf("milestone %s evidence rejected: %w", d.Milestone, err)
		}
	}
	for _, m := range required {
		if !seen[m] {
			return fmt.Errorf("required completion milestone %q lacks validated evidence", m)
		}
	}
	return nil
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// Decode* accept only closed records and reject duplicate keys at every depth.
func DecodeGrant(b []byte) (Grant, error) { var v Grant; e := decodeClosed(b, &v); return v, e }
func DecodeTrustPolicy(b []byte) (TrustPolicy, error) {
	var v TrustPolicy
	e := decodeClosed(b, &v)
	return v, e
}
func DecodeRevocationSnapshot(b []byte) (RevocationSnapshot, error) {
	var v RevocationSnapshot
	e := decodeClosed(b, &v)
	return v, e
}
func DecodeReconciliationReport(b []byte) (ReconciliationReport, error) {
	var v ReconciliationReport
	e := decodeClosed(b, &v)
	return v, e
}
func DecodeReplayScenario(b []byte) (ReplayScenario, error) {
	var v ReplayScenario
	e := decodeClosed(b, &v)
	return v, e
}
func Decode(b []byte, target any) error { return decodeClosed(b, target) }
func decodeClosed(b []byte, dst any) error {
	if len(b) == 0 {
		return errors.New("empty record")
	}
	var raw any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if e := dec.Decode(&raw); e != nil {
		return e
	}
	if e := ensureEOF(dec); e != nil {
		return e
	}
	// Decode into RawMessage maps recursively to detect duplicate object members.
	var check func(*json.Decoder) error
	check = func(d *json.Decoder) error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		switch t {
		case json.Delim('{'):
			seen := map[string]bool{}
			for d.More() {
				kt, e := d.Token()
				if e != nil {
					return e
				}
				k, ok := kt.(string)
				if !ok || seen[k] {
					return errors.New("duplicate or invalid JSON object key")
				}
				seen[k] = true
				if e = check(d); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		case json.Delim('['):
			for d.More() {
				if e = check(d); e != nil {
					return e
				}
			}
			_, e = d.Token()
			return e
		default:
			return nil
		}
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if e := check(d); e != nil {
		return e
	}
	strict := json.NewDecoder(bytes.NewReader(b))
	strict.DisallowUnknownFields()
	if e := strict.Decode(dst); e != nil {
		return e
	}
	return ensureEOF(strict)
}
func ensureEOF(d *json.Decoder) error {
	var extra any
	if e := d.Decode(&extra); e == nil {
		return errors.New("trailing JSON value")
	} else if e != io.EOF {
		return e
	}
	return nil
}

type Entry struct {
	OperationID    string `json:"operationId"`
	Digest         string `json:"digest"`
	GrantID        string `json:"grantId"`
	State          string `json:"state"`
	Fence          int64  `json:"fence"`
	Owner          string `json:"owner,omitempty"`
	Attempts       int    `json:"attempts"`
	Detail         string `json:"detail,omitempty"`
	GrantHash      string `json:"grantHash,omitempty"`
	PolicyHash     string `json:"policyHash,omitempty"`
	ProviderOrigin string `json:"providerOrigin,omitempty"`
	LeaseUntil     string `json:"leaseUntil,omitempty"`
}
type Outbox struct{ db *sql.DB }

const (
	writebackApplicationID = 1095715671 // ASCII "AOKW"
	writebackSchemaVersion = 1
)

// checkAndAdvanceTrustedClock fences send-intent admission against wall-clock
// rollback across processes and restarts. It must run in the same transaction
// as the intent CAS; failed transactions roll the high-water update back.
func checkAndAdvanceTrustedClock(tx *sql.Tx, now time.Time) error {
	var lastText string
	err := tx.QueryRow(`SELECT last_at FROM trusted_clock WHERE singleton=1`).Scan(&lastText)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil {
		last, parseErr := time.Parse(time.RFC3339Nano, lastText)
		if parseErr != nil || now.Before(last) {
			return errors.New("trusted clock moved backwards")
		}
	}
	_, err = tx.Exec(`INSERT INTO trusted_clock(singleton,last_at) VALUES(1,?) ON CONFLICT(singleton) DO UPDATE SET last_at=excluded.last_at`, now.UTC().Format(time.RFC3339Nano))
	return err
}

func Open(path string) (*Outbox, error) {
	if path == "" || strings.Contains(path, "\x00") {
		return nil, errors.New("outbox path required")
	}
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if e = checkPrivateParent(filepath.Dir(abs)); e != nil {
		return nil, e
	}
	if e = checkPrivateSidecars(abs); e != nil {
		return nil, e
	}
	if st, se := os.Lstat(abs); se == nil {
		if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
			return nil, errors.New("outbox must be a private regular file")
		}
	} else if os.IsNotExist(se) {
		f, ce := os.OpenFile(abs, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if ce != nil {
			return nil, ce
		}
		if ce = f.Close(); ce != nil {
			return nil, ce
		}
	} else {
		return nil, se
	}
	dsnURL := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	db, e := sql.Open("sqlite", dsnURL.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if e = db.Ping(); e != nil {
		db.Close()
		return nil, e
	}
	if e = validateStoreIdentity(db, true); e != nil {
		db.Close()
		return nil, e
	}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000; CREATE TABLE IF NOT EXISTS outbox(operation_id TEXT PRIMARY KEY,digest TEXT NOT NULL,grant_id TEXT NOT NULL,state TEXT NOT NULL,fence INTEGER NOT NULL DEFAULT 0,owner TEXT NOT NULL DEFAULT '',lease_until TEXT NOT NULL DEFAULT '',attempts INTEGER NOT NULL DEFAULT 0,detail TEXT NOT NULL DEFAULT '',grant_expiry TEXT NOT NULL,grant_generation INTEGER NOT NULL,admitted_generation INTEGER NOT NULL DEFAULT 0,grant_nonce TEXT NOT NULL,grant_issuer TEXT NOT NULL,operation_payload BLOB NOT NULL,grant_hash TEXT NOT NULL,policy_hash TEXT NOT NULL,provider_origin TEXT NOT NULL,scope_payload BLOB NOT NULL DEFAULT X'', UNIQUE(grant_id), UNIQUE(grant_issuer,grant_nonce)); CREATE TABLE IF NOT EXISTS create_outbox(correlation_id TEXT PRIMARY KEY,intent_id TEXT NOT NULL UNIQUE,intent_digest TEXT NOT NULL,grant_id TEXT NOT NULL UNIQUE,state TEXT NOT NULL,fence INTEGER NOT NULL DEFAULT 0,owner TEXT NOT NULL DEFAULT '',lease_until TEXT NOT NULL DEFAULT '',attempts INTEGER NOT NULL DEFAULT 0,detail TEXT NOT NULL DEFAULT '',grant_expiry TEXT NOT NULL,grant_generation INTEGER NOT NULL,admitted_generation INTEGER NOT NULL DEFAULT 0,grant_nonce TEXT NOT NULL,grant_issuer TEXT NOT NULL,intent_payload BLOB NOT NULL,grant_hash TEXT NOT NULL,policy_hash TEXT NOT NULL,provider_origin TEXT NOT NULL,response_identity BLOB NOT NULL DEFAULT X'',UNIQUE(grant_issuer,grant_nonce)); CREATE TABLE IF NOT EXISTS authorization_uses(issuer TEXT NOT NULL,nonce TEXT NOT NULL,grant_id TEXT NOT NULL UNIQUE,intent_digest TEXT NOT NULL,PRIMARY KEY(issuer,nonce)); CREATE TABLE IF NOT EXISTS revocation_state(issuer TEXT PRIMARY KEY,generation INTEGER NOT NULL,issued_at TEXT NOT NULL); CREATE TABLE IF NOT EXISTS revoked(issuer TEXT NOT NULL,value TEXT NOT NULL,kind TEXT NOT NULL,PRIMARY KEY(issuer,value,kind)); CREATE TABLE IF NOT EXISTS trusted_clock(singleton INTEGER PRIMARY KEY CHECK(singleton=1),last_at TEXT NOT NULL);`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Outbox{db}, nil
}

// OpenReadOnly opens an existing database without permitting creation or writes.
func OpenReadOnly(path string) (*Outbox, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	st, e := os.Lstat(abs)
	if e != nil {
		return nil, e
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		return nil, errors.New("outbox must be a private regular file")
	}
	if e = checkPrivateParent(filepath.Dir(abs)); e != nil {
		return nil, e
	}
	if e = checkPrivateSidecars(abs); e != nil {
		return nil, e
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	q := u.Query()
	q.Set("mode", "ro")
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if e = db.Ping(); e != nil {
		db.Close()
		return nil, e
	}
	if e = validateStoreIdentity(db, false); e != nil {
		db.Close()
		return nil, e
	}
	return &Outbox{db}, nil
}

func checkPrivateSidecars(path string) error {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		st, err := os.Lstat(path + suffix)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
			return errors.New("outbox SQLite sidecar must be a private regular file")
		}
	}
	return nil
}

func validateStoreIdentity(db *sql.DB, initializeEmpty bool) error {
	var appID, version int
	if err := db.QueryRow(`PRAGMA application_id`).Scan(&appID); err != nil {
		return err
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if appID == writebackApplicationID && version == writebackSchemaVersion {
		return nil
	}
	if appID != 0 || version != 0 {
		return errors.New("unsupported writeback database identity or schema version")
	}
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`).Scan(&tables); err != nil {
		return err
	}
	if !initializeEmpty || tables != 0 {
		return errors.New("refusing nonempty untagged SQLite database")
	}
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA application_id=%d; PRAGMA user_version=%d`, writebackApplicationID, writebackSchemaVersion)); err != nil {
		return err
	}
	return nil
}
func checkPrivateParent(path string) error {
	st, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
		return errors.New("outbox parent must be a real directory")
	}
	if st.Mode().Perm()&0022 != 0 {
		return errors.New("outbox parent must not be group or world writable")
	}
	return nil
}
func (o *Outbox) Close() error { return o.db.Close() }

// ImportRevocation verifies the protected snapshot before monotonically replacing
// the local generation. Equal generations may only replay byte-identical state.
func (o *Outbox) ImportRevocation(p TrustPolicy, r RevocationSnapshot, now time.Time) error {
	if p.APIVersion != APIVersion || p.Kind != "TrustPolicy" || r.APIVersion != APIVersion || r.Kind != "RevocationSnapshot" {
		return errors.New("invalid revocation snapshot")
	}
	keyText, ok := p.Keys[r.Issuer]
	if !ok {
		return errors.New("untrusted revocation issuer")
	}
	key, e := hex.DecodeString(keyText)
	if e != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("invalid revocation key")
	}
	b, e := RevocationSigningBytes(r)
	if e != nil {
		return e
	}
	sig, e := hex.DecodeString(r.Signature)
	if e != nil || !ed25519.Verify(key, b, sig) {
		return errors.New("invalid revocation signature")
	}
	issued, e := time.Parse(time.RFC3339Nano, r.IssuedAt)
	if e != nil || now.Before(issued) || now.Sub(issued) > 5*time.Minute {
		return errors.New("revocation state is stale or future dated")
	}
	tx, e := o.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var gen uint64
	e = tx.QueryRow(`SELECT generation FROM revocation_state WHERE issuer=?`, r.Issuer).Scan(&gen)
	if e != nil && e != sql.ErrNoRows {
		return e
	}
	if r.Generation < gen {
		return errors.New("revocation generation regressed")
	}
	if r.Generation == gen && gen != 0 {
		var oldAt string
		if e = tx.QueryRow(`SELECT issued_at FROM revocation_state WHERE issuer=?`, r.Issuer).Scan(&oldAt); e != nil {
			return e
		}
		if oldAt != r.IssuedAt {
			return errors.New("same revocation generation has different issuance time")
		}
		rows, qe := tx.Query(`SELECT value,kind FROM revoked WHERE issuer=? ORDER BY kind,value`, r.Issuer)
		if qe != nil {
			return qe
		}
		var existing []string
		for rows.Next() {
			var v, k string
			if qe = rows.Scan(&v, &k); qe != nil {
				rows.Close()
				return qe
			}
			existing = append(existing, k+":"+v)
		}
		if qe = rows.Err(); qe != nil {
			rows.Close()
			return qe
		}
		rows.Close()
		want := make([]string, 0, len(r.RevokedGrantIDs)+len(r.RevokedNonces))
		for _, v := range r.RevokedGrantIDs {
			want = append(want, "grant:"+v)
		}
		for _, v := range r.RevokedNonces {
			want = append(want, "nonce:"+v)
		}
		sort.Strings(want)
		if len(want) != len(existing) {
			return errors.New("same revocation generation has changed contents")
		}
		for i := range want {
			if want[i] != existing[i] {
				return errors.New("same revocation generation has changed contents")
			}
		}
		return tx.Commit()
	}
	_, e = tx.Exec(`INSERT INTO revocation_state(issuer,generation,issued_at) VALUES(?,?,?) ON CONFLICT(issuer) DO UPDATE SET generation=excluded.generation,issued_at=excluded.issued_at`, r.Issuer, r.Generation, r.IssuedAt)
	if e != nil {
		return e
	}
	// Revocations are monotonic. A newer snapshot cannot reinstate an old grant or nonce.
	for _, v := range r.RevokedGrantIDs {
		if v == "" {
			return errors.New("empty revoked grant id")
		}
		if _, e = tx.Exec(`INSERT OR IGNORE INTO revoked(issuer,value,kind) VALUES(?,?,'grant')`, r.Issuer, v); e != nil {
			return e
		}
	}
	for _, v := range r.RevokedNonces {
		if v == "" {
			return errors.New("empty revoked nonce")
		}
		if _, e = tx.Exec(`INSERT OR IGNORE INTO revoked(issuer,value,kind) VALUES(?,?,'nonce')`, r.Issuer, v); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (o *Outbox) Enqueue(op contracts.SyncOperation, g VerifiedGrant) (Entry, error) {
	if g.id == "" || g.digest != op.OperationDigest || g.expires.IsZero() {
		return Entry{}, errors.New("verified grant mismatch")
	}
	payload, e := contracts.CanonicalOperationBytes(op.WorkItemBindingRef, g.scope, op.ConnectorCapabilityRef, op.OperationKind, op.ExpectedRevision, op.Change)
	if e != nil {
		return Entry{}, e
	}
	if DigestBytes(payload) != g.digest {
		return Entry{}, errors.New("operation payload digest mismatch")
	}
	scopePayload, e := json.Marshal(g.scope)
	if e != nil {
		return Entry{}, e
	}
	tx, e := o.db.Begin()
	if e != nil {
		return Entry{}, e
	}
	defer tx.Rollback()
	_, e = tx.Exec(`INSERT OR IGNORE INTO authorization_uses(issuer,nonce,grant_id,intent_digest) VALUES(?,?,?,?)`, g.issuer, g.nonce, g.id, g.digest)
	if e != nil {
		return Entry{}, e
	}
	var usedGrant, usedDigest string
	if e = tx.QueryRow(`SELECT grant_id,intent_digest FROM authorization_uses WHERE issuer=? AND nonce=?`, g.issuer, g.nonce).Scan(&usedGrant, &usedDigest); e != nil {
		return Entry{}, e
	}
	if usedGrant != g.id || usedDigest != g.digest {
		return Entry{}, errors.New("grant nonce already consumed by different operation")
	}
	sourceGeneration := g.sourceGeneration
	if sourceGeneration == 0 {
		sourceGeneration = g.generation
	} // package test fixtures may construct opaque values directly
	res, e := tx.Exec(`INSERT OR IGNORE INTO outbox(operation_id,digest,grant_id,state,grant_expiry,grant_generation,admitted_generation,grant_nonce,grant_issuer,operation_payload,grant_hash,policy_hash,provider_origin,scope_payload) VALUES(?,?,?,'prepared',?,?,?,?,?,?,?,?,?,?)`, op.OperationID, g.digest, g.id, g.expires.UTC().Format(time.RFC3339Nano), sourceGeneration, g.generation, g.nonce, g.issuer, payload, g.grantHash, g.policyHash, g.origin, scopePayload)
	if e != nil {
		return Entry{}, e
	}
	n, _ := res.RowsAffected()
	if e = tx.Commit(); e != nil {
		return Entry{}, e
	}
	x, e := o.Get(op.OperationID)
	if e != nil {
		return Entry{}, e
	}
	if n == 0 && (x.Digest != g.digest || x.GrantID != g.id) {
		return Entry{}, errors.New("logical operation key conflicts with existing bytes")
	}
	return x, nil
}
func (o *Outbox) Get(id string) (Entry, error) {
	var x Entry
	e := o.db.QueryRow(`SELECT operation_id,digest,grant_id,state,fence,owner,attempts,detail,grant_hash,policy_hash,provider_origin FROM outbox WHERE operation_id=?`, id).Scan(&x.OperationID, &x.Digest, &x.GrantID, &x.State, &x.Fence, &x.Owner, &x.Attempts, &x.Detail, &x.GrantHash, &x.PolicyHash, &x.ProviderOrigin)
	return x, e
}
func (o *Outbox) Claim(id, owner string, now time.Time, lease time.Duration) (Entry, error) {
	if owner == "" || lease <= 0 {
		return Entry{}, errors.New("owner and positive lease required")
	}
	tx, e := o.db.Begin()
	if e != nil {
		return Entry{}, e
	}
	defer tx.Rollback()
	var x Entry
	var expiry, leaseUntil string
	e = tx.QueryRow(`SELECT operation_id,digest,grant_id,state,fence,owner,attempts,detail,grant_expiry,lease_until FROM outbox WHERE operation_id=?`, id).Scan(&x.OperationID, &x.Digest, &x.GrantID, &x.State, &x.Fence, &x.Owner, &x.Attempts, &x.Detail, &expiry, &leaseUntil)
	if e != nil {
		return Entry{}, e
	}
	previousState := x.State
	previousFence := x.Fence
	previousOwner := x.Owner
	if x.State != "prepared" && x.State != "authorized" {
		return Entry{}, fmt.Errorf("state %s is not claimable", x.State)
	}
	if x.State == "authorized" {
		oldLease, pe := time.Parse(time.RFC3339Nano, leaseUntil)
		if pe != nil || now.Before(oldLease) {
			return Entry{}, errors.New("active pre-intent claim cannot be taken over")
		}
	}
	ex, _ := time.Parse(time.RFC3339Nano, expiry)
	if !now.Before(ex) {
		return Entry{}, errors.New("grant expired")
	}
	x.Fence++
	x.Owner = owner
	x.State = "authorized"
	x.LeaseUntil = now.Add(lease).UTC().Format(time.RFC3339Nano)
	res, e := tx.Exec(`UPDATE outbox SET state=?,fence=?,owner=?,lease_until=? WHERE operation_id=? AND fence=? AND owner=? AND state=?`, x.State, x.Fence, owner, x.LeaseUntil, id, previousFence, previousOwner, previousState)
	if e != nil {
		return Entry{}, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return Entry{}, errors.New("claim CAS rejected")
	}
	if e = tx.Commit(); e != nil {
		return Entry{}, e
	}
	return x, nil
}

// commitSendIntent is the lower-level CAS retained for package-internal crash
// tests. Callers must use CommitSendIntentBound with opaque verified authority.
func (o *Outbox) commitIntentForTest(id, owner string, fence int64, now time.Time, generation uint64) (Entry, error) {
	return o.commitSendIntentCAS(id, owner, fence, now, generation, nil, nil)
}

func (o *Outbox) CommitSendIntentBound(id, owner string, fence int64, now time.Time, grant VerifiedGrant, op contracts.SyncOperation) (Entry, error) {
	if grant.id == "" {
		return Entry{}, errors.New("verified grant required")
	}
	if op.OperationID != id || op.OperationDigest != grant.digest {
		return Entry{}, errors.New("operation identity does not match verified grant")
	}
	payload, e := contracts.CanonicalOperationBytes(op.WorkItemBindingRef, grant.scope, op.ConnectorCapabilityRef, op.OperationKind, op.ExpectedRevision, op.Change)
	if e != nil {
		return Entry{}, e
	}
	if DigestBytes(payload) != grant.digest {
		return Entry{}, errors.New("operation payload does not match verified digest")
	}
	return o.commitSendIntentCAS(id, owner, fence, now, grant.generation, &grant, payload)
}

func (o *Outbox) commitSendIntentCAS(id, owner string, fence int64, now time.Time, generation uint64, grant *VerifiedGrant, payload []byte) (Entry, error) {
	tx, e := o.db.Begin()
	if e != nil {
		return Entry{}, e
	}
	defer tx.Rollback()
	var issuer, nonce, grantID, digest, grantHash, policyHash, origin string
	var sourceGeneration uint64
	var storedPayload []byte
	e = tx.QueryRow(`SELECT grant_issuer,grant_nonce,grant_id,digest,grant_hash,policy_hash,provider_origin,operation_payload,grant_generation FROM outbox WHERE operation_id=?`, id).Scan(&issuer, &nonce, &grantID, &digest, &grantHash, &policyHash, &origin, &storedPayload, &sourceGeneration)
	if e != nil {
		return Entry{}, e
	}
	if grant != nil && (grantID != grant.id || digest != grant.digest || grantHash != grant.grantHash || policyHash != grant.policyHash || origin != grant.origin || issuer != grant.issuer || nonce != grant.nonce || sourceGeneration != grant.sourceGeneration || !bytes.Equal(storedPayload, payload)) {
		return Entry{}, errors.New("send-intent authorization does not match immutable outbox entry")
	}
	var current uint64
	var issuedText string
	if e = tx.QueryRow(`SELECT generation,issued_at FROM revocation_state WHERE issuer=?`, issuer).Scan(&current, &issuedText); e != nil {
		return Entry{}, errors.New("current trusted revocation generation unavailable")
	}
	issued, pe := time.Parse(time.RFC3339Nano, issuedText)
	if pe != nil || now.Before(issued) || now.Sub(issued) > 5*time.Minute || current != generation {
		return Entry{}, errors.New("revocation generation is stale or changed")
	}
	var revoked int
	if e = tx.QueryRow(`SELECT count(*) FROM revoked WHERE issuer=? AND ((value=? AND kind='nonce') OR (value=(SELECT grant_id FROM outbox WHERE operation_id=?) AND kind='grant'))`, issuer, nonce, id).Scan(&revoked); e != nil {
		return Entry{}, e
	}
	if revoked != 0 {
		return Entry{}, errors.New("grant revoked before send intent")
	}
	var expiryText, leaseText string
	if e = tx.QueryRow(`SELECT grant_expiry,lease_until FROM outbox WHERE operation_id=?`, id).Scan(&expiryText, &leaseText); e != nil {
		return Entry{}, e
	}
	expires, expiryErr := time.Parse(time.RFC3339Nano, expiryText)
	leaseUntil, leaseErr := time.Parse(time.RFC3339Nano, leaseText)
	if expiryErr != nil || leaseErr != nil || !now.Before(expires) || !now.Before(leaseUntil) {
		return Entry{}, errors.New("grant or claim lease expired")
	}
	if e = checkAndAdvanceTrustedClock(tx, now); e != nil {
		return Entry{}, e
	}
	if grant != nil && grant.sourceGeneration > generation {
		return Entry{}, errors.New("current revocation generation predates grant authorization")
	}
	res, e := tx.Exec(`UPDATE outbox SET state='dispatch-intent',attempts=attempts+1,admitted_generation=? WHERE operation_id=? AND state='authorized' AND owner=? AND fence=? AND grant_generation<=? AND attempts=0`, generation, id, owner, fence, generation)
	if e != nil {
		return Entry{}, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return Entry{}, errors.New("send-intent CAS rejected: stale fence, grant, generation, expiry, or consumed budget")
	}
	if e = tx.Commit(); e != nil {
		return Entry{}, e
	}
	return o.Get(id)
}
func (o *Outbox) RecordOutcome(id, owner string, fence int64, state, detail string) (Entry, error) {
	if state != "unknown-awaiting-reconciliation" && state != "response-observed" && state != "conflict" && state != "rejected" {
		return Entry{}, errors.New("invalid outcome state")
	}
	res, e := o.db.Exec(`UPDATE outbox SET state=?,detail=? WHERE operation_id=? AND owner=? AND fence=? AND state IN ('dispatch-intent','response-observed','unknown-awaiting-reconciliation')`, state, detail, id, owner, fence)
	if e != nil {
		return Entry{}, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return Entry{}, errors.New("outcome CAS rejected")
	}
	return o.Get(id)
}

// Reconcile records read-only evidence. It never makes an operation claimable again.
func (o *Outbox) Reconcile(id, detail, disposition string) (Entry, error) {
	if disposition == "readback-verified" {
		return Entry{}, errors.New("verified readback report required")
	}
	if disposition != "conflict" && disposition != "unknown-awaiting-reconciliation" {
		return Entry{}, errors.New("invalid reconciliation disposition")
	}
	res, e := o.db.Exec(`UPDATE outbox SET state=?,detail=? WHERE operation_id=? AND state IN ('dispatch-intent','unknown-awaiting-reconciliation','response-observed')`, disposition, detail, id)
	if e != nil {
		return Entry{}, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return Entry{}, errors.New("entry not reconcilable")
	}
	return o.Get(id)
}
func (o *Outbox) ReconcileVerified(r VerifiedReconciliation) (Entry, error) {
	if r.operationID == "" || r.digest == "" || r.detail == "" {
		return Entry{}, errors.New("verified reconciliation report required")
	}
	identityBytes, e := json.Marshal(r.identity)
	if e != nil {
		return Entry{}, e
	}
	tx, e := o.db.Begin()
	if e != nil {
		return Entry{}, e
	}
	defer tx.Rollback()
	var storedScope []byte
	if e = tx.QueryRow(`SELECT scope_payload FROM outbox WHERE operation_id=? AND digest=?`, r.operationID, r.digest).Scan(&storedScope); e != nil {
		return Entry{}, e
	}
	if !bytes.Equal(storedScope, identityBytes) {
		return Entry{}, errors.New("verified readback identity does not match persisted operation scope")
	}
	res, e := tx.Exec(`UPDATE outbox SET state=?,detail=? WHERE operation_id=? AND digest=? AND scope_payload=? AND state IN ('dispatch-intent','unknown-awaiting-reconciliation','response-observed')`, r.disposition, "signed readback:"+r.detail, r.operationID, r.digest, identityBytes)
	if e != nil {
		return Entry{}, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return Entry{}, errors.New("verified reconciliation CAS rejected")
	}
	if e = tx.Commit(); e != nil {
		return Entry{}, e
	}
	return o.Get(r.operationID)
}
func (o *Outbox) Inspect(ctx context.Context) ([]Entry, error) {
	rows, e := o.db.QueryContext(ctx, `SELECT operation_id,digest,grant_id,state,fence,owner,attempts,detail,grant_hash,policy_hash,provider_origin,lease_until FROM outbox ORDER BY operation_id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var x Entry
		if e = rows.Scan(&x.OperationID, &x.Digest, &x.GrantID, &x.State, &x.Fence, &x.Owner, &x.Attempts, &x.Detail, &x.GrantHash, &x.PolicyHash, &x.ProviderOrigin, &x.LeaseUntil); e != nil {
			return nil, e
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

type ReplayScenario struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	Events     []ReplayEvent `json:"events"`
}
type ReplayEvent struct {
	At          string `json:"at"`
	Action      string `json:"action"`
	OperationID string `json:"operationId"`
	Owner       string `json:"owner,omitempty"`
	Fence       int64  `json:"fence,omitempty"`
	Generation  uint64 `json:"generation,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// Replay is pure: it interprets a closed event sequence and does not open a DB or contact providers.
func Replay(s ReplayScenario, asOf time.Time) ([]Entry, error) {
	if s.APIVersion != APIVersion || s.Kind != "ReplayScenario" {
		return nil, errors.New("invalid replay scenario version/kind")
	}
	m := map[string]Entry{}
	for _, ev := range s.Events {
		at, e := time.Parse(time.RFC3339Nano, ev.At)
		if e != nil || at.After(asOf) {
			return nil, errors.New("invalid or future replay event")
		}
		x := m[ev.OperationID]
		x.OperationID = ev.OperationID
		switch ev.Action {
		case "enqueue":
			if x.State != "" {
				return nil, errors.New("duplicate replay enqueue")
			}
			x.Digest = ev.Detail
			x.State = "prepared"
		case "claim":
			if x.State != "prepared" || ev.Owner == "" {
				return nil, errors.New("invalid replay claim")
			}
			x.Fence++
			x.Owner = ev.Owner
			x.State = "authorized"
		case "send-intent":
			if x.State != "authorized" || x.Fence != ev.Fence || ev.Generation == 0 {
				return nil, errors.New("invalid replay send-intent")
			}
			x.State = "dispatch-intent"
			x.Attempts++
		case "ambiguous":
			if x.State != "dispatch-intent" {
				return nil, errors.New("ambiguous event without intent")
			}
			x.State = "unknown-awaiting-reconciliation"
		case "reconcile":
			if x.State != "dispatch-intent" && x.State != "unknown-awaiting-reconciliation" {
				return nil, errors.New("invalid replay reconciliation")
			}
			x.State = ev.Detail
		default:
			return nil, errors.New("unknown replay action")
		}
		m[ev.OperationID] = x
	}
	out := make([]Entry, 0, len(m))
	for _, x := range m {
		out = append(out, x)
	}
	return out, nil
}

// CreateIntent is an explicitly separate envelope; creation is disabled in this package.
type CreateIntent struct {
	APIVersion          string                   `json:"apiVersion"`
	Kind                string                   `json:"kind"`
	ID                  string                   `json:"id"`
	Parent              contracts.RemoteIdentity `json:"parent"`
	TypeID              string                   `json:"typeId"`
	SchemaDigest        string                   `json:"schemaDigest"`
	Fields              json.RawMessage          `json:"fields"`
	ClientCorrelationID string                   `json:"clientCorrelationId"`
	Budget              int                      `json:"budget"`
	GrantID             string                   `json:"grantId"`
}

type CreateGrant struct {
	APIVersion           string                   `json:"apiVersion"`
	Kind                 string                   `json:"kind"`
	ID                   string                   `json:"id"`
	Issuer               string                   `json:"issuer"`
	Principal            string                   `json:"principal"`
	AuthRef              string                   `json:"authRef"`
	IssuedAt             string                   `json:"issuedAt"`
	ProviderOrigin       string                   `json:"providerOrigin"`
	IntentDigest         string                   `json:"intentDigest"`
	Parent               contracts.RemoteIdentity `json:"parent"`
	TypeID               string                   `json:"typeId"`
	SchemaDigest         string                   `json:"schemaDigest"`
	FieldsDigest         string                   `json:"fieldsDigest"`
	ClientCorrelationID  string                   `json:"clientCorrelationId"`
	Budget               int                      `json:"budget"`
	Nonce                string                   `json:"nonce"`
	ExpiresAt            string                   `json:"expiresAt"`
	RevocationGeneration uint64                   `json:"revocationGeneration"`
	Signature            string                   `json:"signature"`
}
type signedCreateGrant struct {
	APIVersion           string                   `json:"apiVersion"`
	Kind                 string                   `json:"kind"`
	ID                   string                   `json:"id"`
	Issuer               string                   `json:"issuer"`
	Principal            string                   `json:"principal"`
	AuthRef              string                   `json:"authRef"`
	IssuedAt             string                   `json:"issuedAt"`
	ProviderOrigin       string                   `json:"providerOrigin"`
	IntentDigest         string                   `json:"intentDigest"`
	Parent               contracts.RemoteIdentity `json:"parent"`
	TypeID               string                   `json:"typeId"`
	SchemaDigest         string                   `json:"schemaDigest"`
	FieldsDigest         string                   `json:"fieldsDigest"`
	ClientCorrelationID  string                   `json:"clientCorrelationId"`
	Budget               int                      `json:"budget"`
	Nonce                string                   `json:"nonce"`
	ExpiresAt            string                   `json:"expiresAt"`
	RevocationGeneration uint64                   `json:"revocationGeneration"`
}

func CreateGrantSigningBytes(g CreateGrant) ([]byte, error) {
	return json.Marshal(signedCreateGrant{g.APIVersion, g.Kind, g.ID, g.Issuer, g.Principal, g.AuthRef, g.IssuedAt, g.ProviderOrigin, g.IntentDigest, g.Parent, g.TypeID, g.SchemaDigest, g.FieldsDigest, g.ClientCorrelationID, g.Budget, g.Nonce, g.ExpiresAt, g.RevocationGeneration})
}
func CreateIntentDigest(i CreateIntent) (string, error) {
	b, e := CreateIntentBytes(i)
	if e != nil {
		return "", e
	}
	return DigestBytes(b), nil
}
func CreateIntentBytes(i CreateIntent) ([]byte, error) {
	v, e := ParseCreateIntent(i)
	if e != nil {
		return nil, e
	}
	x := v.intent
	var compact bytes.Buffer
	if e = json.Compact(&compact, x.Fields); e != nil {
		return nil, e
	}
	x.Fields = compact.Bytes()
	return json.Marshal(x)
}

type VerifiedCreateIntent struct {
	intent                                                              CreateIntent
	grantID, issuer, nonce, intentDigest, grantHash, policyHash, origin string
	generation, sourceGeneration                                        uint64
	expires                                                             time.Time
}

func (v VerifiedCreateIntent) Intent() CreateIntent { return v.intent }
func (v VerifiedCreateIntent) GrantID() string      { return v.grantID }
func VerifyCreateGrant(g CreateGrant, p TrustPolicy, r RevocationSnapshot, i CreateIntent, now time.Time) (VerifiedCreateIntent, error) {
	validated, e := ParseCreateIntent(i)
	if e != nil {
		return VerifiedCreateIntent{}, e
	}
	i = validated.intent
	if g.APIVersion != APIVersion || g.Kind != "CreateGrant" || p.APIVersion != APIVersion || p.Kind != "TrustPolicy" || r.APIVersion != APIVersion || r.Kind != "RevocationSnapshot" {
		return VerifiedCreateIntent{}, errors.New("invalid create grant policy or revocation version")
	}
	if g.ID == "" || g.Issuer == "" || g.Principal == "" || g.AuthRef == "" || g.Nonce == "" || g.Budget != 1 || g.RevocationGeneration == 0 || r.Generation == 0 {
		return VerifiedCreateIntent{}, errors.New("invalid create grant identity or budget")
	}
	key, e := hex.DecodeString(p.Keys[g.Issuer])
	if e != nil || len(key) != ed25519.PublicKeySize {
		return VerifiedCreateIntent{}, errors.New("untrusted create grant issuer")
	}
	b, e := CreateGrantSigningBytes(g)
	if e != nil {
		return VerifiedCreateIntent{}, e
	}
	sig, e := hex.DecodeString(g.Signature)
	if e != nil || !ed25519.Verify(key, b, sig) {
		return VerifiedCreateIntent{}, errors.New("invalid create grant signature")
	}
	rkey, e := hex.DecodeString(p.Keys[r.Issuer])
	rb, _ := RevocationSigningBytes(r)
	rs, re := hex.DecodeString(r.Signature)
	if e != nil || len(rkey) != ed25519.PublicKeySize || re != nil || !ed25519.Verify(rkey, rb, rs) {
		return VerifiedCreateIntent{}, errors.New("invalid create revocation signature")
	}
	at, e := time.Parse(time.RFC3339Nano, r.IssuedAt)
	if e != nil || r.Issuer != g.Issuer || now.Before(at) || now.Sub(at) > 5*time.Minute || r.Generation < g.RevocationGeneration {
		return VerifiedCreateIntent{}, errors.New("create revocation state is stale or mismatched")
	}
	for _, v := range r.RevokedGrantIDs {
		if v == g.ID {
			return VerifiedCreateIntent{}, errors.New("create grant revoked")
		}
	}
	for _, v := range r.RevokedNonces {
		if v == g.Nonce {
			return VerifiedCreateIntent{}, errors.New("create nonce revoked")
		}
	}
	issued, e := time.Parse(time.RFC3339Nano, g.IssuedAt)
	if e != nil || issued.After(now) {
		return VerifiedCreateIntent{}, errors.New("create grant issuance invalid")
	}
	expiry, e := time.Parse(time.RFC3339Nano, g.ExpiresAt)
	if e != nil || !now.Before(expiry) || p.MaxGrantTTLSeconds <= 0 || expiry.Sub(issued) > time.Duration(p.MaxGrantTTLSeconds)*time.Second {
		return VerifiedCreateIntent{}, errors.New("create grant expired or exceeds policy TTL")
	}
	if !contains(p.Principals, g.Principal) || !contains(p.AuthRefs, g.AuthRef) || !contains(p.ProviderOrigins, g.ProviderOrigin) || !routeAllowed(p.ProviderRoutes, i.Parent.Provider, g.ProviderOrigin) {
		return VerifiedCreateIntent{}, errors.New("create grant route or principal outside protected policy")
	}
	digest, e := CreateIntentDigest(i)
	if e != nil {
		return VerifiedCreateIntent{}, e
	}
	fieldHash := DigestBytes(i.Fields)
	if g.ID != i.GrantID || g.IntentDigest != digest || g.Parent != i.Parent || g.TypeID != i.TypeID || g.SchemaDigest != i.SchemaDigest || g.FieldsDigest != fieldHash || g.ClientCorrelationID != i.ClientCorrelationID || g.Budget != i.Budget {
		return VerifiedCreateIntent{}, errors.New("create grant does not bind exact intent")
	}
	policyBytes, _ := json.Marshal(p)
	return VerifiedCreateIntent{intent: i, grantID: g.ID, issuer: g.Issuer, nonce: g.Nonce, intentDigest: digest, grantHash: DigestBytes(b), policyHash: DigestBytes(policyBytes), origin: g.ProviderOrigin, generation: r.Generation, sourceGeneration: g.RevocationGeneration, expires: expiry}, nil
}

type CreateResponse struct {
	Identity        contracts.RemoteIdentity `json:"identity"`
	NativeReceiptID string                   `json:"nativeReceiptId"`
}
type Creator interface {
	Create(context.Context, CreationPermit) (CreateResponse, error)
}

// CreationPermit is an opaque one-shot authority minted only after the
// durable create send-intent CAS. Its zero value and copied declarations carry
// no authority. An injected creator must consume it immediately before use.
type CreationPermit struct {
	outbox             *Outbox
	intent             VerifiedCreateIntent
	correlation, owner string
	fence              int64
	used               *atomic.Bool
}

// Consume returns the verified create intent only once, after rechecking the
// committed durable intent, owner, fence and exact immutable payload.
func (p CreationPermit) Consume() (VerifiedCreateIntent, error) {
	if p.outbox == nil || p.used == nil || !p.used.CompareAndSwap(false, true) {
		return VerifiedCreateIntent{}, errors.New("creation permit is absent or already consumed")
	}
	var state, owner, grantID, digest, grantHash, policyHash, origin, issuer, nonce string
	var fence int64
	var attempts int
	var payload []byte
	err := p.outbox.db.QueryRow(`SELECT state,owner,fence,attempts,grant_id,intent_digest,grant_hash,policy_hash,provider_origin,grant_issuer,grant_nonce,intent_payload FROM create_outbox WHERE correlation_id=?`, p.correlation).Scan(&state, &owner, &fence, &attempts, &grantID, &digest, &grantHash, &policyHash, &origin, &issuer, &nonce, &payload)
	if err != nil {
		return VerifiedCreateIntent{}, err
	}
	want, err := CreateIntentBytes(p.intent.intent)
	if err != nil {
		return VerifiedCreateIntent{}, err
	}
	if state != "dispatch-intent" || owner != p.owner || fence != p.fence || attempts != 1 || grantID != p.intent.grantID || digest != p.intent.intentDigest || grantHash != p.intent.grantHash || policyHash != p.intent.policyHash || origin != p.intent.origin || issuer != p.intent.issuer || nonce != p.intent.nonce || !bytes.Equal(payload, want) {
		return VerifiedCreateIntent{}, errors.New("creation permit no longer matches durable send intent")
	}
	return p.intent, nil
}

type CreateEntry struct {
	CorrelationID     string                    `json:"correlationId"`
	IntentID          string                    `json:"intentId"`
	IntentDigest      string                    `json:"intentDigest"`
	GrantID           string                    `json:"grantId"`
	State             string                    `json:"state"`
	Fence             int64                     `json:"fence"`
	Owner             string                    `json:"owner,omitempty"`
	LeaseUntil        string                    `json:"leaseUntil,omitempty"`
	Attempts          int                       `json:"attempts"`
	Detail            string                    `json:"detail,omitempty"`
	CandidateIdentity *contracts.RemoteIdentity `json:"candidateIdentity,omitempty"`
	Identity          *contracts.RemoteIdentity `json:"identity,omitempty"`
}

func (v VerifiedCreateIntent) IntentDigest() string { return v.intentDigest }
func (v VerifiedCreateIntent) ExpiresAt() time.Time { return v.expires }
func (v VerifiedCreateIntent) Issuer() string       { return v.issuer }

func (o *Outbox) EnqueueCreate(v VerifiedCreateIntent) (CreateEntry, error) {
	if v.grantID == "" || v.intentDigest == "" || v.expires.IsZero() {
		return CreateEntry{}, errors.New("verified create intent required")
	}
	payload, e := CreateIntentBytes(v.intent)
	if e != nil {
		return CreateEntry{}, e
	}
	tx, e := o.db.Begin()
	if e != nil {
		return CreateEntry{}, e
	}
	defer tx.Rollback()
	_, e = tx.Exec(`INSERT OR IGNORE INTO authorization_uses(issuer,nonce,grant_id,intent_digest) VALUES(?,?,?,?)`, v.issuer, v.nonce, v.grantID, v.intentDigest)
	if e != nil {
		return CreateEntry{}, e
	}
	var usedGrant, usedDigest string
	if e = tx.QueryRow(`SELECT grant_id,intent_digest FROM authorization_uses WHERE issuer=? AND nonce=?`, v.issuer, v.nonce).Scan(&usedGrant, &usedDigest); e != nil {
		return CreateEntry{}, e
	}
	if usedGrant != v.grantID || usedDigest != v.intentDigest {
		return CreateEntry{}, errors.New("grant nonce already consumed by a different intent")
	}
	sourceGeneration := v.sourceGeneration
	if sourceGeneration == 0 {
		sourceGeneration = v.generation
	}
	_, e = tx.Exec(`INSERT OR IGNORE INTO create_outbox(correlation_id,intent_id,intent_digest,grant_id,state,grant_expiry,grant_generation,admitted_generation,grant_nonce,grant_issuer,intent_payload,grant_hash,policy_hash,provider_origin) VALUES(?,?,?,?,'prepared',?,?,?,?,?,?,?,?,?)`, v.intent.ClientCorrelationID, v.intent.ID, v.intentDigest, v.grantID, v.expires.UTC().Format(time.RFC3339Nano), sourceGeneration, v.generation, v.nonce, v.issuer, payload, v.grantHash, v.policyHash, v.origin)
	if e != nil {
		return CreateEntry{}, e
	}
	x, e := getCreateTx(tx, v.intent.ClientCorrelationID)
	if e != nil {
		return x, e
	}
	if x.IntentDigest != v.intentDigest || x.GrantID != v.grantID {
		return CreateEntry{}, errors.New("create correlation key conflicts with different intent")
	}
	return x, tx.Commit()
}
func getCreateTx(q interface{ QueryRow(string, ...any) *sql.Row }, id string) (CreateEntry, error) {
	var x CreateEntry
	var candidate []byte
	e := q.QueryRow(`SELECT correlation_id,intent_id,intent_digest,grant_id,state,fence,owner,lease_until,attempts,detail,response_identity FROM create_outbox WHERE correlation_id=?`, id).Scan(&x.CorrelationID, &x.IntentID, &x.IntentDigest, &x.GrantID, &x.State, &x.Fence, &x.Owner, &x.LeaseUntil, &x.Attempts, &x.Detail, &candidate)
	if e != nil {
		return x, e
	}
	if len(candidate) > 0 {
		var ident contracts.RemoteIdentity
		if json.Unmarshal(candidate, &ident) == nil {
			x.CandidateIdentity = &ident
			if x.State == "readback-verified" {
				x.Identity = &ident
			}
		}
	}
	return x, nil
}
func (o *Outbox) GetCreate(id string) (CreateEntry, error) { return getCreateTx(o.db, id) }
func (o *Outbox) ClaimCreate(id, owner string, now time.Time, lease time.Duration) (CreateEntry, error) {
	if owner == "" || lease <= 0 {
		return CreateEntry{}, errors.New("owner and positive lease required")
	}
	tx, e := o.db.Begin()
	if e != nil {
		return CreateEntry{}, e
	}
	defer tx.Rollback()
	x, e := getCreateTx(tx, id)
	if e != nil {
		return x, e
	}
	if x.State != "prepared" && x.State != "authorized" {
		return x, fmt.Errorf("create state %s is not claimable", x.State)
	}
	if x.State == "authorized" {
		old, pe := time.Parse(time.RFC3339Nano, x.LeaseUntil)
		if pe != nil || now.Before(old) {
			return x, errors.New("active create claim cannot be taken over")
		}
	}
	var expiry string
	if e = tx.QueryRow(`SELECT grant_expiry FROM create_outbox WHERE correlation_id=?`, id).Scan(&expiry); e != nil {
		return x, e
	}
	ex, e := time.Parse(time.RFC3339Nano, expiry)
	if e != nil || !now.Before(ex) {
		return x, errors.New("create grant expired")
	}
	previousState, previousFence, previousOwner := x.State, x.Fence, x.Owner
	x.Fence++
	x.Owner = owner
	x.State = "authorized"
	x.LeaseUntil = now.Add(lease).UTC().Format(time.RFC3339Nano)
	res, e := tx.Exec(`UPDATE create_outbox SET state='authorized',fence=?,owner=?,lease_until=? WHERE correlation_id=? AND state=? AND fence=? AND owner=?`, x.Fence, owner, x.LeaseUntil, id, previousState, previousFence, previousOwner)
	if e != nil {
		return x, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return x, errors.New("create claim CAS rejected")
	}
	if e = tx.Commit(); e != nil {
		return x, e
	}
	return o.GetCreate(id)
}
func (o *Outbox) CommitCreateIntent(id, owner string, fence int64, now time.Time, v VerifiedCreateIntent) (CreateEntry, error) {
	tx, e := o.db.Begin()
	if e != nil {
		return CreateEntry{}, e
	}
	defer tx.Rollback()
	x, e := getCreateTx(tx, id)
	if e != nil {
		return x, e
	}
	var gen uint64
	if e = tx.QueryRow(`SELECT grant_generation FROM create_outbox WHERE correlation_id=?`, id).Scan(&gen); e != nil {
		return x, e
	}
	var storedDigest, storedGrantHash, storedPolicyHash, storedOrigin, storedIssuer, storedNonce string
	var storedPayload []byte
	if e = tx.QueryRow(`SELECT intent_digest,grant_hash,policy_hash,provider_origin,grant_issuer,grant_nonce,intent_payload FROM create_outbox WHERE correlation_id=?`, id).Scan(&storedDigest, &storedGrantHash, &storedPolicyHash, &storedOrigin, &storedIssuer, &storedNonce, &storedPayload); e != nil {
		return x, e
	}
	intentPayload, e := CreateIntentBytes(v.intent)
	if e != nil {
		return x, e
	}
	if x.IntentID != v.intent.ID || x.IntentDigest != v.intentDigest || x.GrantID != v.grantID || gen != v.sourceGeneration || storedDigest != v.intentDigest || storedGrantHash != v.grantHash || storedPolicyHash != v.policyHash || storedOrigin != v.origin || storedIssuer != v.issuer || storedNonce != v.nonce || !bytes.Equal(storedPayload, intentPayload) {
		return x, errors.New("create intent does not match verified grant")
	}
	var current uint64
	var issued string
	if e = tx.QueryRow(`SELECT generation,issued_at FROM revocation_state WHERE issuer=?`, v.issuer).Scan(&current, &issued); e != nil {
		return x, errors.New("current create revocation state unavailable")
	}
	at, pe := time.Parse(time.RFC3339Nano, issued)
	if pe != nil || now.Before(at) || now.Sub(at) > 5*time.Minute || current != v.generation {
		return x, errors.New("create revocation generation stale")
	}
	var revoked int
	if e = tx.QueryRow(`SELECT count(*) FROM revoked WHERE issuer=? AND ((value=? AND kind='nonce') OR (value=? AND kind='grant'))`, v.issuer, v.nonce, v.grantID).Scan(&revoked); e != nil {
		return x, e
	}
	if revoked > 0 {
		return x, errors.New("create grant revoked")
	}
	var expiryText, leaseText string
	if e = tx.QueryRow(`SELECT grant_expiry,lease_until FROM create_outbox WHERE correlation_id=?`, id).Scan(&expiryText, &leaseText); e != nil {
		return x, e
	}
	expires, expiryErr := time.Parse(time.RFC3339Nano, expiryText)
	leaseUntil, leaseErr := time.Parse(time.RFC3339Nano, leaseText)
	if expiryErr != nil || leaseErr != nil || !now.Before(expires) || !now.Before(leaseUntil) {
		return x, errors.New("create grant or claim lease expired")
	}
	if e = checkAndAdvanceTrustedClock(tx, now); e != nil {
		return x, e
	}
	if v.sourceGeneration > v.generation {
		return x, errors.New("current create revocation generation predates authorization")
	}
	res, e := tx.Exec(`UPDATE create_outbox SET state='dispatch-intent',attempts=1,admitted_generation=? WHERE correlation_id=? AND state='authorized' AND owner=? AND fence=? AND grant_generation<=? AND attempts=0`, v.generation, id, owner, fence, v.generation)
	if e != nil {
		return x, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return x, errors.New("create send-intent CAS rejected")
	}
	if e = tx.Commit(); e != nil {
		return x, e
	}
	return o.GetCreate(id)
}
func (o *Outbox) DispatchCreateOnce(ctx context.Context, creator Creator, id, owner string, fence int64, now time.Time, v VerifiedCreateIntent) (CreateEntry, error) {
	if creator == nil {
		return CreateEntry{}, errors.New("injected creator required")
	}
	if ctx.Err() != nil {
		return CreateEntry{}, ctx.Err()
	}
	entry, e := o.CommitCreateIntent(id, owner, fence, now, v)
	if e != nil {
		return entry, e
	}
	permit := CreationPermit{outbox: o, intent: v, correlation: id, owner: owner, fence: fence, used: &atomic.Bool{}}
	response, sendErr := creator.Create(ctx, permit)
	if sendErr != nil {
		_, _ = o.db.Exec(`UPDATE create_outbox SET state='unknown-awaiting-reconciliation',detail='creator returned ambiguous outcome' WHERE correlation_id=? AND state='dispatch-intent' AND fence=?`, id, fence)
		x, _ := o.GetCreate(id)
		return x, sendErr
	}
	parent := v.intent.Parent
	if !validRemote(response.Identity) || response.Identity.Provider != parent.Provider || response.Identity.TenantID != parent.TenantID || response.Identity.ContainerID != parent.ContainerID || response.NativeReceiptID == "" {
		_, _ = o.db.Exec(`UPDATE create_outbox SET state='conflict',detail='creator response identity/scope invalid' WHERE correlation_id=? AND state='dispatch-intent' AND fence=?`, id, fence)
		x, _ := o.GetCreate(id)
		return x, errors.New("creator returned invalid assigned identity")
	}
	raw, _ := json.Marshal(response.Identity)
	res, e := o.db.Exec(`UPDATE create_outbox SET state='response-observed',detail=?,response_identity=? WHERE correlation_id=? AND state='dispatch-intent' AND fence=? AND owner=? AND attempts=1`, response.NativeReceiptID, raw, id, fence, owner)
	if e != nil {
		return entry, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return entry, errors.New("create response CAS rejected")
	}
	return o.GetCreate(id)
}

type CreateReadbackReport struct {
	APIVersion          string                   `json:"apiVersion"`
	Kind                string                   `json:"kind"`
	Issuer              string                   `json:"issuer"`
	ClientCorrelationID string                   `json:"clientCorrelationId"`
	IntentDigest        string                   `json:"intentDigest"`
	Identity            contracts.RemoteIdentity `json:"identity"`
	ObservedAt          string                   `json:"observedAt"`
	Evidence            []contracts.AssetRef     `json:"evidence"`
	Signature           string                   `json:"signature"`
}
type signedCreateReadback struct {
	APIVersion          string                   `json:"apiVersion"`
	Kind                string                   `json:"kind"`
	Issuer              string                   `json:"issuer"`
	ClientCorrelationID string                   `json:"clientCorrelationId"`
	IntentDigest        string                   `json:"intentDigest"`
	Identity            contracts.RemoteIdentity `json:"identity"`
	ObservedAt          string                   `json:"observedAt"`
	Evidence            []contracts.AssetRef     `json:"evidence"`
}
type VerifiedCreateReadback struct {
	correlation, digest string
	identity            contracts.RemoteIdentity
	reportHash          string
}

func CreateReadbackSigningBytes(r CreateReadbackReport) ([]byte, error) {
	return json.Marshal(signedCreateReadback{r.APIVersion, r.Kind, r.Issuer, r.ClientCorrelationID, r.IntentDigest, r.Identity, r.ObservedAt, r.Evidence})
}
func VerifyCreateReadback(r CreateReadbackReport, p TrustPolicy, v VerifiedCreateIntent, now time.Time) (VerifiedCreateReadback, error) {
	if r.APIVersion != APIVersion || r.Kind != "CreateReadbackReport" || p.APIVersion != APIVersion || p.Kind != "TrustPolicy" || !contains(p.ReadbackIssuers, r.Issuer) {
		return VerifiedCreateReadback{}, errors.New("invalid or untrusted create readback")
	}
	key, e := hex.DecodeString(p.Keys[r.Issuer])
	if e != nil || len(key) != ed25519.PublicKeySize {
		return VerifiedCreateReadback{}, errors.New("invalid create readback key")
	}
	b, e := CreateReadbackSigningBytes(r)
	if e != nil {
		return VerifiedCreateReadback{}, e
	}
	sig, e := hex.DecodeString(r.Signature)
	if e != nil || !ed25519.Verify(key, b, sig) {
		return VerifiedCreateReadback{}, errors.New("invalid create readback signature")
	}
	observed, e := time.Parse(time.RFC3339Nano, r.ObservedAt)
	if e != nil || now.Before(observed) || now.Sub(observed) > 5*time.Minute {
		return VerifiedCreateReadback{}, errors.New("create readback is stale or future dated")
	}
	if r.ClientCorrelationID != v.intent.ClientCorrelationID || r.IntentDigest != v.intentDigest {
		return VerifiedCreateReadback{}, errors.New("create readback does not match approved correlation and intent")
	}
	parent := v.intent.Parent
	if !validRemote(r.Identity) || r.Identity.Provider != parent.Provider || r.Identity.TenantID != parent.TenantID || r.Identity.ContainerID != parent.ContainerID {
		return VerifiedCreateReadback{}, errors.New("create readback identity is outside approved parent scope")
	}
	if len(r.Evidence) == 0 {
		return VerifiedCreateReadback{}, errors.New("create readback evidence is required")
	}
	for _, a := range r.Evidence {
		if a.Path == "" || !validDigest(a.SHA256) {
			return VerifiedCreateReadback{}, errors.New("invalid create readback evidence")
		}
	}
	return VerifiedCreateReadback{r.ClientCorrelationID, r.IntentDigest, r.Identity, DigestBytes(b)}, nil
}
func (o *Outbox) ReconcileCreateVerified(r VerifiedCreateReadback) (CreateEntry, error) {
	if r.correlation == "" || r.digest == "" || !validRemote(r.identity) {
		return CreateEntry{}, errors.New("verified create readback required")
	}
	raw, _ := json.Marshal(r.identity)
	res, e := o.db.Exec(`UPDATE create_outbox SET state='readback-verified',detail=?,response_identity=? WHERE correlation_id=? AND intent_digest=? AND state IN ('response-observed','unknown-awaiting-reconciliation') AND (length(response_identity)=0 OR response_identity=?)`, "signed readback:"+r.reportHash, raw, r.correlation, r.digest, raw)
	if e != nil {
		return CreateEntry{}, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return CreateEntry{}, errors.New("create readback CAS rejected or conflicts with observed identity")
	}
	return o.GetCreate(r.correlation)
}

type VerifiedCreatedBinding struct{ binding contracts.WorkItemBinding }

func (v VerifiedCreatedBinding) Binding() contracts.WorkItemBinding { return v.binding }
func (o *Outbox) CreateBinding(correlation string, b contracts.WorkItemBinding) (VerifiedCreatedBinding, error) {
	x, e := o.GetCreate(correlation)
	if e != nil {
		return VerifiedCreatedBinding{}, e
	}
	if x.State != "readback-verified" || x.Identity == nil || !validRemote(b.Identity) || b.Identity != *x.Identity {
		return VerifiedCreatedBinding{}, errors.New("binding can be issued only from exact verified assigned identity")
	}
	raw, e := json.Marshal(b)
	if e != nil {
		return VerifiedCreatedBinding{}, e
	}
	parsed, _, e := contracts.Parse(raw)
	if e != nil {
		return VerifiedCreatedBinding{}, fmt.Errorf("created binding fails contract validation: %w", e)
	}
	if _, e = contracts.DecodeTyped(parsed.(contracts.Resource)); e != nil {
		return VerifiedCreatedBinding{}, e
	}
	return VerifiedCreatedBinding{b}, nil
}
func validRemote(id contracts.RemoteIdentity) bool {
	return id.Provider != "" && id.TenantID != "" && id.ContainerID != "" && id.RemoteID != "" && id.ItemKind != ""
}

var ErrCreationDisabled = errors.New("creation is disabled unless an injected qualified creator boundary is explicitly used")

type ValidatedCreateIntent struct{ intent CreateIntent }

func (v ValidatedCreateIntent) Record() CreateIntent { return v.intent }
func ParseCreateIntent(i CreateIntent) (ValidatedCreateIntent, error) {
	if i.APIVersion != APIVersion || i.Kind != "CreateIntent" || i.ID == "" || i.TypeID == "" || !validDigest(i.SchemaDigest) || i.ClientCorrelationID == "" || i.Budget != 1 || i.GrantID == "" {
		return ValidatedCreateIntent{}, errors.New("invalid create intent identity, schema, correlation, budget, or grant")
	}
	if i.Parent.Provider == "" || i.Parent.TenantID == "" || i.Parent.ContainerID == "" || i.Parent.ItemKind == "" {
		return ValidatedCreateIntent{}, errors.New("create intent requires an exact parent/container scope")
	}
	if len(i.Fields) == 0 || !json.Valid(i.Fields) {
		return ValidatedCreateIntent{}, errors.New("create intent fields must be valid JSON object")
	}
	var fields map[string]json.RawMessage
	if e := json.Unmarshal(i.Fields, &fields); e != nil || fields == nil {
		return ValidatedCreateIntent{}, errors.New("create intent fields must be an object")
	}
	return ValidatedCreateIntent{i}, nil
}
func ValidateCreateIntent(i CreateIntent) error                  { _, e := ParseCreateIntent(i); return e }
func ExecuteCreate(context.Context, ValidatedCreateIntent) error { return ErrCreationDisabled }

func DigestBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
