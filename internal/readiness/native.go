package readiness

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector/milestones"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/deliverybridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

// HostControlTrust comes from the operator's protected trust store. The host
// policy file cannot establish its own control proof roots.
type HostControlTrust struct {
	Principal      string
	ControlIssuers map[string]string
	OS             string
	Arch           string
	Transport      string
}

// HostControlAdapters wires the existing host policy validator and signed
// control proof verifier into the two host gates. Unconfigured hosts remain
// unknown. A policy's embedded issuer map must match the external trust map.
func HostControlAdapters(trust map[string]HostControlTrust) Adapters {
	out := Adapters{}
	for _, host := range []string{"codex", "claude-code"} {
		t, ok := trust[host]
		if !ok || t.Principal == "" || len(t.ControlIssuers) == 0 {
			continue
		}
		validator := NativeValidatorFunc(func(in ValidationInput) ValidationResult { return validateHostControls(in, host, t) })
		if host == "codex" {
			out["native.host.codex"] = validator
		} else {
			out["native.host.claude"] = validator
		}
	}
	return out
}

func validateHostControls(in ValidationInput, host string, trust HostControlTrust) ValidationResult {
	fail := func(err error) ValidationResult {
		return ValidationResult{Status: "blocked", Reason: err.Error(), EvidenceSHA256: []string{}}
	}
	if in.Route.Host != host || len(in.Evidence) != 1 {
		return fail(errors.New("host control evidence must bind one exact host policy"))
	}
	b := in.Evidence[0]
	if b.Issuer != trust.Principal || b.Role != "host-control-policy" {
		return fail(errors.New("host policy evidence issuer or role is not trusted"))
	}
	p, err := hostbridge.ReadPolicy(filepath.Join(in.EvidenceRoot, filepath.FromSlash(b.Path)))
	if err != nil {
		return fail(fmt.Errorf("native host policy: %w", err))
	}
	if err = hostbridge.ValidatePolicy(p); err != nil {
		return fail(fmt.Errorf("native host policy: %w", err))
	}
	if p.Host != host || p.HostVersion != in.Route.HostVersion || p.ExecutableSHA256 != in.Route.HostSHA256 || p.AccountScope != in.Route.Account || p.AuthMode != in.Route.AuthMode || hostbridge.PolicyHash(p) != in.Route.ControlPolicySHA256 || trust.OS != in.Route.OS || trust.Arch != in.Route.Arch || trust.Transport != in.Route.Transport {
		return fail(errors.New("host policy does not match exact route identity and digest"))
	}
	if !reflect.DeepEqual(p.ControlIssuers, trust.ControlIssuers) {
		return fail(errors.New("host policy control roots differ from protected operator trust"))
	}
	if err = hostbridge.VerifyControls(p, in.AsOf); err != nil {
		return fail(fmt.Errorf("native host controls: %w", err))
	}
	validUntil := time.Time{}
	for _, c := range p.Controls {
		until, e := time.Parse(time.RFC3339Nano, c.ExpiresAt)
		if e != nil {
			return fail(errors.New("native control expiry is invalid"))
		}
		if validUntil.IsZero() || until.Before(validUntil) {
			validUntil = until
		}
	}
	// Controls establish a currently trusted host-control configuration. They do
	// not establish that the native host actually conforms to cancellation,
	// resume, task identity, or other live transport behavior. Keep the P6 gate
	// unknown until an independently qualified conformance validator is wired.
	return ValidationResult{Status: "unknown", Reason: "host controls verified; bounded native conformance remains unqualified", EvidenceSHA256: []string{b.SHA256}, ValidUntil: validUntil.UTC().Format(time.RFC3339)}
}

// AcceptanceTrust supplies acceptance issuers from outside the evidence
// bundle. Keys embedded in the verification policy are never trusted by this
// adapter. The three binding IDs are fixed so evidence cannot be substituted.
type AcceptanceTrust struct {
	Issuers    []verification.TrustedIssuer
	MaxAge     time.Duration
	Projection milestones.CurrentProjectionVerifier
}

func AcceptanceValidator(trust AcceptanceTrust) NativeValidator {
	return NativeValidatorFunc(func(in ValidationInput) ValidationResult {
		fail := func(err error) ValidationResult {
			return ValidationResult{Status: "blocked", Reason: err.Error(), EvidenceSHA256: []string{}}
		}
		if len(trust.Issuers) == 0 || trust.MaxAge <= 0 {
			return ValidationResult{Status: "unknown", Reason: "external acceptance issuer trust and finite age limit are required", EvidenceSHA256: []string{}}
		}
		if trust.Projection == nil {
			return ValidationResult{Status: "unknown", Reason: "protected current P8 projection and revocation verifier is required", EvidenceSHA256: []string{}}
		}
		byID := map[string]EvidenceBinding{}
		for _, b := range in.Evidence {
			byID[b.ID] = b
		}
		read := func(id string) ([]byte, error) {
			b, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("required evidence %q is missing", id)
			}
			data, ok := in.EvidenceBytes[id]
			if !ok || Hash(data) != b.SHA256 {
				return nil, fmt.Errorf("required evidence %q is unavailable or changed", id)
			}
			return data, nil
		}
		policyBytes, err := read("verification-policy")
		if err != nil {
			return fail(err)
		}
		var p verification.VerificationPolicy
		if err = usage.Decode(policyBytes, &p); err != nil {
			return fail(err)
		}
		if err = verification.ValidatePolicy(p); err != nil {
			return fail(err)
		}
		if p.MaxAgeSeconds < 1 || time.Duration(p.MaxAgeSeconds)*time.Second > trust.MaxAge {
			return fail(errors.New("verification policy acceptance age exceeds external trust limit"))
		}
		manifestBytes, err := read("candidate-manifest")
		if err != nil {
			return fail(err)
		}
		m, err := verification.DecodeManifest(manifestBytes)
		if err != nil {
			return fail(err)
		}
		if m.ConfigurationSHA256 != in.Route.ConfigurationSHA256 || m.ProfileSHA256 != in.Route.ProfileSHA256 {
			return fail(errors.New("accepted candidate configuration/profile differ from exact route"))
		}
		acceptanceBytes, err := read("acceptance")
		if err != nil {
			return fail(err)
		}
		var receipt verification.AcceptanceReceipt
		if err = usage.Decode(acceptanceBytes, &receipt); err != nil {
			return fail(err)
		}
		if err = verification.VerifyAcceptance(receipt, trust.Issuers, in.AsOf, trust.MaxAge); err != nil {
			return fail(fmt.Errorf("native acceptance signature or freshness: %w", err))
		}
		if err = verification.ValidateAcceptanceManifestBinding(receipt, m, receipt.CandidateSHA256, receipt.SnapshotSHA256, p); err != nil {
			return fail(fmt.Errorf("native acceptance binding: %w", err))
		}
		candidate := verification.Candidate{Manifest: m, Digest: receipt.CandidateSHA256, SnapshotSHA256: receipt.SnapshotSHA256}
		ref := milestones.ArtifactRef{Milestone: "accepted", Kind: "acceptance", ID: receipt.ID, Issuer: receipt.Issuer, Digest: byID["acceptance"].SHA256, Scope: milestones.CandidateScope{RunID: m.RunID, StepID: m.StepID, AttemptID: m.AttemptID, Fence: m.Fence, InputSHA256: m.InputSHA256, ConfigurationSHA256: m.ConfigurationSHA256, ProfileSHA256: m.ProfileSHA256}}
		if err = trust.Projection.VerifyCurrentProjection(ref, candidate, receipt, in.AsOf); err != nil {
			return fail(fmt.Errorf("current P8 projection: %w", err))
		}
		refs := []string{byID["acceptance"].SHA256, byID["candidate-manifest"].SHA256, byID["verification-policy"].SHA256}
		validUntil := receipt.ValidUntil
		if validUntil == "" {
			if issued, err := time.Parse(time.RFC3339, receipt.IssuedAt); err == nil {
				validUntil = issued.Add(trust.MaxAge).Format(time.RFC3339)
			}
		}
		return ValidationResult{Status: "passed", Reason: "existing acceptance signature, policy coverage and candidate route binding passed", EvidenceSHA256: refs, ValidUntil: validUntil}
	})
}

func AcceptanceAdapters(trust AcceptanceTrust) Adapters {
	return Adapters{"native.acceptance": AcceptanceValidator(trust)}
}

func DeliveryEvidenceValidator(trust AcceptanceTrust) NativeValidator {
	return NativeValidatorFunc(func(in ValidationInput) ValidationResult {
		fail := func(err error) ValidationResult {
			return ValidationResult{Status: "blocked", Reason: err.Error(), EvidenceSHA256: []string{}}
		}
		if len(trust.Issuers) == 0 || trust.MaxAge <= 0 {
			return ValidationResult{Status: "unknown", Reason: "external delivery issuer trust and finite age limit are required", EvidenceSHA256: []string{}}
		}
		if trust.Projection == nil {
			return ValidationResult{Status: "unknown", Reason: "protected current P8 projection and revocation verifier is required", EvidenceSHA256: []string{}}
		}
		if len(in.Evidence) != 1 || in.Evidence[0].ID != "delivery-handoff" {
			return fail(errors.New("one scoped delivery handoff is required"))
		}
		var handoff deliverybridge.HandoffManifest
		b := in.Evidence[0]
		raw := in.EvidenceBytes[b.ID]
		if err := usage.Decode(raw, &handoff); err != nil {
			return fail(fmt.Errorf("delivery handoff: %w", err))
		}
		if handoff.Candidate.Manifest.ConfigurationSHA256 != in.Route.ConfigurationSHA256 || handoff.Candidate.Manifest.ProfileSHA256 != in.Route.ProfileSHA256 {
			return fail(errors.New("delivery candidate differs from route configuration/profile"))
		}
		if !trustedIssuerSet(handoff.Policy.Issuers, trust.Issuers) {
			return fail(errors.New("delivery policy issuers differ from protected operator trust"))
		}
		if handoff.Policy.MaxAgeSeconds < 1 || time.Duration(handoff.Policy.MaxAgeSeconds)*time.Second > trust.MaxAge {
			return fail(errors.New("delivery acceptance age exceeds external trust limit"))
		}
		handoff.AsOf = in.AsOf
		bundle := handoff.Candidate.BundlePath
		if !safeRel(bundle) {
			return fail(errors.New("delivery candidate bundle path must be relative and safe"))
		}
		abs, e := filepath.Abs(filepath.Join(in.EvidenceRoot, filepath.FromSlash(bundle)))
		if e != nil {
			return fail(e)
		}
		if e = checkNoSymlinkPath(abs); e != nil {
			return fail(e)
		}
		st, e := os.Stat(abs)
		if e != nil || !st.IsDir() {
			return fail(errors.New("delivery candidate bundle directory is unavailable"))
		}
		handoff.Candidate.BundlePath = abs
		draft, report := deliverybridge.Validate(handoff)
		_ = draft
		if !report.Valid {
			return fail(fmt.Errorf("native delivery evidence failed: %s", strings.Join(report.Diagnostics, "; ")))
		}
		ref := milestones.ArtifactRef{Milestone: "delivered", Kind: "delivery", ID: handoff.ID, Issuer: handoff.Acceptance.Issuer, Digest: b.SHA256, Scope: milestones.CandidateScope{RunID: handoff.Candidate.Manifest.RunID, StepID: handoff.Candidate.Manifest.StepID, AttemptID: handoff.Candidate.Manifest.AttemptID, Fence: handoff.Candidate.Manifest.Fence, InputSHA256: handoff.Candidate.Manifest.InputSHA256, ConfigurationSHA256: handoff.Candidate.Manifest.ConfigurationSHA256, ProfileSHA256: handoff.Candidate.Manifest.ProfileSHA256}}
		if e = trust.Projection.VerifyCurrentProjection(ref, handoff.Candidate, handoff.Acceptance, in.AsOf); e != nil {
			return fail(fmt.Errorf("current P8 projection: %w", e))
		}
		validUntil := handoff.Acceptance.ValidUntil
		if validUntil == "" {
			if issued, err := time.Parse(time.RFC3339, handoff.Acceptance.IssuedAt); err == nil {
				validUntil = issued.Add(trust.MaxAge).Format(time.RFC3339)
			}
		}
		return ValidationResult{Status: "passed", Reason: "existing P8 local repository handoff validates acceptance, independent review and snapshots; it does not establish remote publication or readback", EvidenceSHA256: []string{b.SHA256}, ValidUntil: validUntil}
	})
}

func trustedIssuerSet(policy, external []verification.TrustedIssuer) bool {
	if len(policy) != len(external) {
		return false
	}
	byID := map[string]verification.TrustedIssuer{}
	for _, i := range external {
		if _, ok := byID[i.ID]; ok {
			return false
		}
		byID[i.ID] = i
	}
	for _, i := range policy {
		if ext, ok := byID[i.ID]; !ok || ext != i {
			return false
		}
	}
	return true
}

func checkNoSymlinkPath(path string) error {
	for cur := path; ; cur = filepath.Dir(cur) {
		st, e := os.Lstat(cur)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return errors.New("delivery path has symlink ancestor")
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	return nil
}

type NativeTrust struct {
	Hosts      map[string]HostControlTrust
	Acceptance AcceptanceTrust
}

func NativeAdapters(trust NativeTrust) Adapters {
	out := HostControlAdapters(trust.Hosts)
	for k, v := range AcceptanceAdapters(trust.Acceptance) {
		out[k] = v
	}
	if len(trust.Acceptance.Issuers) > 0 && trust.Acceptance.MaxAge > 0 {
		out["native.delivery-evidence"] = DeliveryEvidenceValidator(trust.Acceptance)
	}
	return out
}

// ReadEvidenceJSON is a utility for adapter authors who need a direct disk
// read. It retains the package's no-symlink and size bounds.
func ReadEvidenceJSON(root, rel string, max int64) (json.RawMessage, error) {
	if max <= 0 || max > MaxEvidenceBytes {
		return nil, errors.New("invalid evidence bound")
	}
	if !safeRel(rel) {
		return nil, errors.New("unsafe evidence path")
	}
	path := filepath.Join(root, filepath.FromSlash(rel))
	b, e := readSafe(path, max)
	if e != nil {
		return nil, e
	}
	if !json.Valid(b) {
		return nil, errors.New("evidence is not JSON")
	}
	return append(json.RawMessage(nil), b...), nil
}
