package pilot

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

// CustodyWitness is an independently protected chronology authority. It must
// bind these receipts outside the campaign/bundle backup domain.
type CustodyWitness interface {
	VerifyCampaignChronology(campaignSHA256, freezeReceiptSHA256, auditReceiptSHA256 string, frozenAt, openedAt time.Time) error
}

func SignFreezeReceipt(r FreezeReceipt, key ed25519.PrivateKey) FreezeReceipt {
	r.Signature = ""
	b, _ := json.Marshal(r)
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, b))
	return r
}
func SignAuditOpenReceipt(r AuditOpenReceipt, key ed25519.PrivateKey) AuditOpenReceipt {
	r.Signature = ""
	b, _ := json.Marshal(r)
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, b))
	return r
}
func receiptDigest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// VerifyCampaignCustody verifies distinct curation and audit-custodian roles,
// exact frozen dataset/campaign bindings, and witness-confirmed chronology.
func VerifyCampaignCustody(m Manifest, p Policy, curatorID string, curatorKey ed25519.PublicKey, auditorID string, auditorKey ed25519.PublicKey, w CustodyWitness) error {
	if err := ValidatePolicy(p); err != nil {
		return err
	}
	if p.ManifestSHA256 != digest(m) {
		return errors.New("policy is not bound to campaign")
	}
	if w == nil || curatorID == "" || auditorID == "" || curatorID == auditorID || len(curatorKey) != ed25519.PublicKeySize || len(auditorKey) != ed25519.PublicKeySize {
		return errors.New("distinct trusted curation, audit, and chronology witness authorities required")
	}
	f := p.FreezeReceipt
	a := p.AuditOpenReceipt
	if f.Version != "aok-pilot-freeze/v1" || a.Version != "aok-pilot-audit-open/v1" || f.Issuer != curatorID || a.Issuer != auditorID {
		return errors.New("campaign custody roles or versions invalid")
	}
	if f.CampaignSHA256 != digest(m) || a.CampaignSHA256 != digest(m) || f.DatasetSHA256 != m.DatasetSHA256 || a.DatasetSHA256 != m.DatasetSHA256 || a.FreezeReceiptSHA256 != receiptDigest(f) {
		return errors.New("campaign custody bindings mismatch")
	}
	frozen, e := time.Parse(time.RFC3339, f.FrozenAt)
	if e != nil || f.FrozenAt != m.FrozenAt {
		return errors.New("freeze receipt time differs from frozen manifest")
	}
	opened, e := time.Parse(time.RFC3339, a.OpenedAt)
	if e != nil || !opened.After(frozen) {
		return errors.New("audit must open after freeze")
	}
	fsig, e := base64.StdEncoding.DecodeString(f.Signature)
	if e != nil {
		return errors.New("invalid freeze signature encoding")
	}
	unsignedF := f
	unsignedF.Signature = ""
	fb, _ := json.Marshal(unsignedF)
	if !ed25519.Verify(curatorKey, fb, fsig) {
		return errors.New("invalid curation signature")
	}
	asig, e := base64.StdEncoding.DecodeString(a.Signature)
	if e != nil {
		return errors.New("invalid audit signature encoding")
	}
	unsignedA := a
	unsignedA.Signature = ""
	ab, _ := json.Marshal(unsignedA)
	if !ed25519.Verify(auditorKey, ab, asig) {
		return errors.New("invalid audit signature")
	}
	if e = w.VerifyCampaignChronology(digest(m), receiptDigest(f), receiptDigest(a), frozen, opened); e != nil {
		return e
	}
	return nil
}
