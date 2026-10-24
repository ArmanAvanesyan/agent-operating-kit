package bootstrap

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/verifier"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

// VerifierConformance is exclusively an operator-enrolled synthetic fixture,
// never an alternate entrypoint for accepting a runtime task candidate.
type VerifierConformance struct {
	CandidatePath   string                          `json:"candidatePath"`
	CandidateSHA256 string                          `json:"candidateSha256"`
	SnapshotSHA256  string                          `json:"snapshotSha256"`
	Endpoint        string                          `json:"endpoint"`
	AuthReference   string                          `json:"authReference"`
	Policy          verification.VerificationPolicy `json:"policy"`
	VerifierID      string                          `json:"verifierId"`
	Proof           verification.ControlProof       `json:"proof"`
}

func validateQualificationVerifier(c Config, q *QualificationRecords, at time.Time) error {
	v := q.Verifier
	if v == nil || q.Host != nil || q.CodexQuota != nil || q.GitHubRead != nil || q.GitHubWrite != nil || c.SecretEnv[v.AuthReference] == "" || !validHTTPSPath(v.Endpoint, "/v1/execute") || !validHash(v.CandidateSHA256) || !validHash(v.SnapshotSHA256) || verification.ValidatePolicy(v.Policy) != nil {
		return ErrBlocked
	}
	candidate, e := verification.LoadCandidate(v.CandidatePath)
	if e != nil || candidate.Manifest.Origin != "fixture" || candidate.Digest != v.CandidateSHA256 || candidate.SnapshotSHA256 != v.SnapshotSHA256 || v.Proof.CandidateSHA256 != candidate.Digest || v.Proof.SnapshotSHA256 != candidate.SnapshotSHA256 {
		return ErrBlocked
	}
	for _, spec := range v.Policy.Verifiers {
		if spec.ID == v.VerifierID && spec.Kind == "command" {
			pd, e := verification.Digest(v.Policy)
			if e != nil {
				return ErrBlocked
			}
			sd, e := verification.Digest(spec)
			if e != nil {
				return ErrBlocked
			}
			if verification.VerifyControlProof(v.Proof, pd, sd, v.Policy.Issuers, at) != nil {
				return ErrBlocked
			}
			return nil
		}
	}
	return ErrBlocked
}
func runQualificationVerifier(ctx context.Context, c Config, q QualificationRecords, transport http.RoundTripper) (map[string][]byte, error) {
	if ctx == nil || validateQualificationVerifier(c, &q, time.Now().UTC()) != nil {
		return nil, ErrBlocked
	}
	v := q.Verifier
	candidate, e := verification.LoadCandidate(v.CandidatePath)
	if e != nil {
		return nil, e
	}
	var spec verification.VerifierSpec
	for _, s := range v.Policy.Verifiers {
		if s.ID == v.VerifierID {
			spec = s
		}
	}
	resolver := verification.RemoteSecretResolverFunc(func(ctx context.Context, ref string) (string, error) {
		if ctx.Err() != nil || ref != v.AuthReference {
			return "", ErrBlocked
		}
		return resolveSecret(c, ref)
	})
	controls, e := verifier.NewUploadingControls(verification.RemoteRoute{Endpoint: v.Endpoint, AuthReference: v.AuthReference, Resolver: resolver, RoundTripper: transport}, v.Proof, spec, candidate)
	if e != nil {
		return nil, e
	}
	output, code, e := verification.RunCommand(ctx, candidate, v.Policy, v.VerifierID, controls)
	observation := map[string]any{"version": QualificationVersion, "candidateSha256": candidate.Digest, "snapshotSha256": candidate.SnapshotSHA256, "policySha256": usage.Digest(v.Policy), "verifierId": v.VerifierID, "exitCode": code, "outputSha256": usage.Hash(output), "outputBytes": len(output), "observationOnly": true, "authorizationGranted": false, "executionReady": false, "acceptanceGranted": false}
	b, _ := json.MarshalIndent(observation, "", "  ")
	return map[string][]byte{"verifier-observation.json": append(b, '\n')}, e
}
