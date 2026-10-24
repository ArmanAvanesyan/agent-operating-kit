package verification

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

// RemoteProtocol is a project-defined fixture protocol. It asserts no vendor
// authentication scheme and does not imply that any live remote service is qualified.
const RemoteProtocol = "aok-verifier-transport/v1"
const remoteRequestLimit = 1 << 20
const remoteResponseOverhead = 64 << 10
const remoteMaxResponse = 6 << 20

// RemoteSecretResolver resolves an opaque protected authorization reference.
type RemoteSecretResolver interface {
	Resolve(context.Context, string) (string, error)
}
type RemoteSecretResolverFunc func(context.Context, string) (string, error)

func (f RemoteSecretResolverFunc) Resolve(ctx context.Context, ref string) (string, error) {
	return f(ctx, ref)
}

// RemoteRoute pins one HTTPS service origin and fixed endpoint. Credential
// resolution and service qualification remain external operator responsibilities.
// The service protocol is project-defined; passing fixtures does not qualify a
// live service, verifier isolation, or a provider authentication implementation.
type RemoteRoute struct {
	Endpoint      string
	AuthReference string
	Resolver      RemoteSecretResolver
	RoundTripper  http.RoundTripper
}

type RemoteControls struct {
	route RemoteRoute
	proof ControlProof
	spec  VerifierSpec
}

// NewRemoteControls accepts an externally signed proof; it never signs or
// manufactures control evidence. RunCommand verifies that proof against the
// candidate, policy, verifier and trusted issuer before Execute is reached.
func NewRemoteControls(route RemoteRoute, proof ControlProof, spec VerifierSpec) (Controls, error) {
	u, err := url.Parse(route.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/v1/execute" {
		return nil, errors.New("invalid pinned remote verifier endpoint")
	}
	if route.AuthReference == "" || route.Resolver == nil {
		return nil, errors.New("remote verifier auth resolver required")
	}
	if proof.Signature == "" || proof.CandidateSHA256 == "" || proof.SnapshotSHA256 == "" {
		return nil, errors.New("externally signed candidate-bound control proof required")
	}
	if spec.Kind != "command" || len(spec.Argv) == 0 || spec.TimeoutSeconds < 1 || spec.TimeoutSeconds > MaxVerifierTimeoutSeconds || spec.MaxOutputBytes < 1 || spec.MaxOutputBytes > MaxVerifierOutputBytes {
		return nil, errors.New("pinned command verifier specification required")
	}
	if hasCredentialEnvironmentKey(spec.Environment) {
		return nil, errors.New("verifier environment contains a credential-shaped variable name")
	}
	specDigest, err := Digest(spec)
	if err != nil || proof.VerifierSHA256 != specDigest {
		return nil, errors.New("control proof does not bind the pinned verifier specification")
	}
	observed, err := time.Parse(time.RFC3339, proof.ObservedAt)
	if err != nil || observed.After(time.Now().UTC()) {
		return nil, errors.New("control proof observation time is invalid")
	}
	validUntil, err := time.Parse(time.RFC3339, proof.ValidUntil)
	if err != nil || time.Now().UTC().After(validUntil) || !validUntil.After(observed) {
		return nil, errors.New("control proof is expired or invalid")
	}
	spec.Argv = append([]string(nil), spec.Argv...)
	spec.Environment = cloneStringMap(spec.Environment)
	proof.Controls = append([]string(nil), proof.Controls...)
	return &RemoteControls{route: route, proof: proof, spec: spec}, nil
}
func (r *RemoteControls) Proof() ControlProof {
	p := r.proof
	p.Controls = append([]string(nil), r.proof.Controls...)
	return p
}

type remoteExecutionRequest struct {
	Protocol        string            `json:"protocol"`
	Proof           ControlProof      `json:"proof"`
	ProofSHA256     string            `json:"proofSha256"`
	CandidateSHA256 string            `json:"candidateSha256"`
	SnapshotSHA256  string            `json:"snapshotSha256"`
	Argv            []string          `json:"argv"`
	Environment     map[string]string `json:"environment"`
	TimeoutMillis   int64             `json:"timeoutMillis"`
	MaxOutputBytes  int64             `json:"maxOutputBytes"`
}
type remoteExecutionResponse struct {
	Protocol     string `json:"protocol"`
	ProofSHA256  string `json:"proofSha256"`
	ExitCode     int    `json:"exitCode"`
	StdoutBase64 string `json:"stdoutBase64"`
}

func (r *RemoteControls) Execute(ctx context.Context, argv []string, env map[string]string, timeout time.Duration, limit int64) (int, []byte, error) {
	if r == nil || r.route.Resolver == nil || ctx == nil || len(argv) == 0 || timeout <= 0 || timeout > MaxVerifierTimeoutSeconds*time.Second || limit <= 0 || limit > MaxVerifierOutputBytes {
		return 0, nil, errors.New("invalid bounded remote verifier request")
	}
	if !reflect.DeepEqual(argv, r.spec.Argv) || !reflect.DeepEqual(env, r.spec.Environment) || timeout != time.Duration(r.spec.TimeoutSeconds)*time.Second || limit != r.spec.MaxOutputBytes {
		return 0, nil, errors.New("remote verifier request differs from the proof-bound specification")
	}
	if hasCredentialEnvironmentKey(env) {
		return 0, nil, errors.New("verifier environment contains a credential-shaped variable name")
	}
	observed, err := time.Parse(time.RFC3339, r.proof.ObservedAt)
	validUntil, untilErr := time.Parse(time.RFC3339, r.proof.ValidUntil)
	now := time.Now().UTC()
	if err != nil || untilErr != nil || observed.After(now) || now.After(validUntil) || !validUntil.After(observed) {
		return 0, nil, errors.New("control proof is expired or invalid")
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	proofDigest, err := Digest(r.proof)
	if err != nil {
		return 0, nil, errors.New("control proof digest failed")
	}
	request := remoteExecutionRequest{Protocol: RemoteProtocol, Proof: r.proof, ProofSHA256: proofDigest, CandidateSHA256: r.proof.CandidateSHA256, SnapshotSHA256: r.proof.SnapshotSHA256, Argv: append([]string(nil), argv...), Environment: cloneStringMap(env), TimeoutMillis: timeout.Milliseconds(), MaxOutputBytes: limit}
	body, err := json.Marshal(request)
	if err != nil || len(body) > remoteRequestLimit {
		return 0, nil, errors.New("remote verifier request exceeds bound")
	}
	secret, err := r.route.Resolver.Resolve(bounded, r.route.AuthReference)
	if err != nil || secret == "" || strings.ContainsAny(secret, "\r\n") {
		return 0, nil, errors.New("remote verifier credential unavailable")
	}
	req, err := http.NewRequestWithContext(bounded, http.MethodPost, r.route.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, nil, errors.New("invalid remote verifier request")
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := http.Client{Transport: r.route.RoundTripper, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, errors.New("remote verifier request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, nil, errors.New("remote verifier rejected request")
	}
	max := int64(base64.StdEncoding.EncodedLen(int(limit))) + remoteResponseOverhead
	if max > remoteMaxResponse {
		max = remoteMaxResponse
	}
	responseBytes, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil || int64(len(responseBytes)) > max {
		return 0, nil, errors.New("remote verifier response exceeded bound")
	}
	if bytes.Contains(responseBytes, []byte(secret)) || bytes.Contains(responseBytes, []byte(req.Header.Get("Authorization"))) {
		return 0, nil, errors.New("remote verifier response reflected credentials")
	}
	if err := decodeStrict(responseBytes, new(remoteExecutionResponse)); err != nil {
		return 0, nil, errors.New("remote verifier response is invalid")
	}
	var result remoteExecutionResponse
	if json.Unmarshal(responseBytes, &result) != nil || result.Protocol != RemoteProtocol || result.ProofSHA256 != proofDigest {
		return 0, nil, errors.New("remote verifier response does not match proof")
	}
	out, err := base64.StdEncoding.DecodeString(result.StdoutBase64)
	if err != nil || int64(len(out)) > limit {
		return 0, nil, errors.New("remote verifier output exceeded bound")
	}
	// Refuse common credential-shaped output in case the service accidentally
	// included an authorization value from its execution environment.
	if hasCredentialOutput(out) {
		return 0, nil, errors.New("remote verifier output contains credential material")
	}
	return result.ExitCode, out, nil
}
func cloneStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

var credentialPattern = regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|client[_-]?secret|password|authorization)\s*["']?\s*:\s*["']?[^\s,"'}]+|bearer\s+[a-z0-9._~-]{12,}|-----BEGIN (RSA |EC |OPENSSH )?PRIVATE KEY-----`)

func normalizedSecurityName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
func credentialName(s string) bool {
	k := normalizedSecurityName(s)
	for _, part := range []string{"token", "secret", "password", "auth", "apikey", "privatekey", "credential", "accesskey", "clientkey"} {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}
func hasCredentialEnvironmentKey(env map[string]string) bool {
	for k := range env {
		if credentialName(k) {
			return true
		}
	}
	return false
}
func hasCredentialOutput(out []byte) bool {
	if credentialPattern.Match(out) {
		return true
	}
	if !json.Valid(out) {
		return false
	}
	if contracts.ValidateJSON(out) != nil {
		return true
	}
	var value any
	if json.Unmarshal(out, &value) != nil {
		return true
	}
	var scan func(any) bool
	scan = func(v any) bool {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				if credentialName(k) || scan(child) {
					return true
				}
			}
		case []any:
			for _, child := range x {
				if scan(child) {
					return true
				}
			}
		}
		return false
	}
	return scan(value)
}

// RemoteResponseDigest is useful to an external service receipt implementation;
// it is informational and does not itself qualify the service.
func RemoteResponseDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
