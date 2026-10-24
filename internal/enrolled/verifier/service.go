// Package verifier hosts the protected side of aok-verifier-transport/v1.
// A successful execution response is invocation evidence only; acceptance is
// deliberately handled by the separate verification acceptance workflow.
package verifier

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/artifacts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

const requestLimit = 1 << 20
const responseOverhead = 64 << 10

type SecretResolver interface {
	Resolve(context.Context, string) (string, error)
}
type SecretResolverFunc func(context.Context, string) (string, error)

func (f SecretResolverFunc) Resolve(c context.Context, r string) (string, error) { return f(c, r) }

// Executor is a protected, fixed backend. Request data is checked against the
// protected policy before this interface is called.
type Executor interface {
	Execute(context.Context, verification.Candidate, []string, map[string]string, time.Duration, int64) (int, []byte, error)
}

type Config struct {
	Store         *artifacts.Store
	Policy        verification.VerificationPolicy
	VerifierID    string
	AuthReference string
	Resolver      SecretResolver
	Executor      Executor
	// AllowInsecureForTests permits direct in-memory handler fixtures. Production
	// callers must leave it false and serve this handler behind TLS.
	AllowInsecureForTests bool
	// AllowFixtureExecutorForTests is the only bypass for a measured executor.
	// Never set this in a deployed handler.
	AllowFixtureExecutorForTests bool
}

type Handler struct {
	c            Config
	spec         verification.VerifierSpec
	policyDigest string
}

func NewHandler(c Config) (http.Handler, error) {
	if c.Store == nil || c.Resolver == nil || c.AuthReference == "" || c.Executor == nil {
		return nil, errors.New("protected store, opaque auth resolver, and executor are required")
	}
	if err := verification.ValidatePolicy(c.Policy); err != nil {
		return nil, err
	}
	if !c.Store.AuthorityMatches(c.Policy, c.VerifierID) {
		return nil, errors.New("artifact store authority does not match protected verifier policy")
	}
	var spec *verification.VerifierSpec
	for i := range c.Policy.Verifiers {
		if c.Policy.Verifiers[i].ID == c.VerifierID {
			spec = &c.Policy.Verifiers[i]
		}
	}
	if spec == nil || spec.Kind != "command" || len(spec.Argv) == 0 {
		return nil, errors.New("protected pinned command verifier required")
	}
	if hasSecretEnv(spec.Environment) {
		return nil, errors.New("pinned verifier environment contains credential-shaped key")
	}
	pd, err := verification.Digest(c.Policy)
	if err != nil {
		return nil, err
	}
	copySpec := *spec
	copySpec.Argv = append([]string(nil), spec.Argv...)
	copySpec.Environment = clone(spec.Environment)
	if bound, ok := c.Executor.(interface{ IdentitySHA256() (string, error) }); ok {
		identity, e := bound.IdentitySHA256()
		if e != nil {
			return nil, errors.New("executor identity could not be established")
		}
		approved := false
		for _, issuer := range c.Policy.Issuers {
			if issuer.Role == "controls" && issuer.ExecutorSHA256 == identity {
				approved = true
			}
		}
		if !approved {
			return nil, errors.New("control issuer does not pin configured executor and isolation resources")
		}
	} else if !c.AllowFixtureExecutorForTests {
		return nil, errors.New("verifier executor must expose measured identity")
	}
	return &Handler{c: c, spec: copySpec, policyDigest: pd}, nil
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h == nil || r == nil {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if r.TLS == nil && !h.c.AllowInsecureForTests {
		http.Error(w, "TLS required", http.StatusBadRequest)
		return
	}
	ok := false
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	secret, err := h.c.Resolver.Resolve(ctx, h.c.AuthReference)
	if err == nil && secret != "" && !strings.ContainsAny(secret, "\r\n") {
		want := []byte("Bearer " + secret)
		got := []byte(r.Header.Get("Authorization"))
		// Compare fixed-length hashes so token length does not short-circuit comparison.
		wh := sha256.Sum256(want)
		gh := sha256.Sum256(got)
		ok = subtle.ConstantTimeCompare(wh[:], gh[:]) == 1 && subtle.ConstantTimeEq(int32(len(want)), int32(len(got))) == 1
	}
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.URL.Path == "/v1/candidates" {
		h.upload(w, r.WithContext(ctx))
		return
	}
	if r.URL.Path != "/v1/execute" {
		http.NotFound(w, r)
		return
	}
	h.execute(w, r.WithContext(ctx))
}

// Candidate upload accepts the bounded tar format from artifacts.Encode.
// Candidate digest is the address; the service computes and returns its snapshot.
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	d := r.Header.Get("X-AOK-Candidate-SHA256")
	snapshot := strings.TrimSpace(r.Header.Get("X-AOK-Snapshot-SHA256"))
	if !isHash(d) || !isHash(snapshot) {
		http.Error(w, "invalid candidate digest", http.StatusBadRequest)
		return
	}
	proofBytes, err := base64.StdEncoding.DecodeString(r.Header.Get("X-AOK-Control-Proof"))
	var proof verification.ControlProof
	if err != nil || len(proofBytes) > 16<<10 || strictJSON(proofBytes, &proof) != nil || proof.CandidateSHA256 != d || proof.SnapshotSHA256 != snapshot {
		http.Error(w, "candidate control proof rejected", http.StatusForbidden)
		return
	}
	specDigest, e := verification.Digest(h.spec)
	if e != nil || verifyProof(proof, h.policyDigest, specDigest, h.c.Policy.Issuers, time.Now().UTC()) != nil {
		http.Error(w, "candidate control proof rejected", http.StatusForbidden)
		return
	}
	c, err := h.c.Store.Ingest(r.Context(), d, snapshot, proof, http.MaxBytesReader(w, r.Body, artifacts.MaxTransferBytes))
	if err != nil {
		if existing, getErr := h.c.Store.Get(r.Context(), d, snapshot); getErr == nil {
			writeJSON(w, http.StatusConflict, map[string]string{"candidateSha256": existing.Digest, "snapshotSha256": existing.SnapshotSHA256})
			return
		}
		http.Error(w, "candidate transfer rejected", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"candidateSha256": c.Digest, "snapshotSha256": c.SnapshotSHA256})
}

type executeRequest struct {
	Protocol        string                    `json:"protocol"`
	Proof           verification.ControlProof `json:"proof"`
	ProofSHA256     string                    `json:"proofSha256"`
	CandidateSHA256 string                    `json:"candidateSha256"`
	SnapshotSHA256  string                    `json:"snapshotSha256"`
	Argv            []string                  `json:"argv"`
	Environment     map[string]string         `json:"environment"`
	TimeoutMillis   int64                     `json:"timeoutMillis"`
	MaxOutputBytes  int64                     `json:"maxOutputBytes"`
}
type executeResponse struct {
	Protocol     string `json:"protocol"`
	ProofSHA256  string `json:"proofSha256"`
	ExitCode     int    `json:"exitCode"`
	StdoutBase64 string `json:"stdoutBase64"`
}

func (h *Handler) execute(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, requestLimit)
	b, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "request exceeds bound", http.StatusRequestEntityTooLarge)
		return
	}
	var q executeRequest
	if strictJSON(b, &q) != nil || q.Protocol != verification.RemoteProtocol {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	proofDigest, err := verification.Digest(q.Proof)
	if err != nil || proofDigest != q.ProofSHA256 || q.CandidateSHA256 != q.Proof.CandidateSHA256 || q.SnapshotSHA256 != q.Proof.SnapshotSHA256 || q.Proof.PolicySHA256 != h.policyDigest {
		http.Error(w, "proof binding rejected", http.StatusForbidden)
		return
	}
	specDigest, err := verification.Digest(h.spec)
	if err != nil || q.Proof.VerifierSHA256 != specDigest {
		http.Error(w, "verifier pin rejected", http.StatusForbidden)
		return
	}
	if verifyProof(q.Proof, h.policyDigest, specDigest, h.c.Policy.Issuers, time.Now().UTC()) != nil {
		http.Error(w, "control proof rejected", http.StatusForbidden)
		return
	}
	if !reflect.DeepEqual(q.Argv, h.spec.Argv) || !reflect.DeepEqual(q.Environment, h.spec.Environment) || q.TimeoutMillis != int64(h.spec.TimeoutSeconds)*1000 || q.MaxOutputBytes != h.spec.MaxOutputBytes {
		http.Error(w, "request differs from protected verifier pin", http.StatusForbidden)
		return
	}
	if h.spec.TimeoutSeconds > verification.MaxVerifierTimeoutSeconds || h.spec.MaxOutputBytes > verification.MaxVerifierOutputBytes || hasSecretEnv(q.Environment) {
		http.Error(w, "verifier bound rejected", http.StatusBadRequest)
		return
	}
	c, err := h.c.Store.Get(r.Context(), q.CandidateSHA256, q.SnapshotSHA256)
	if err != nil {
		http.Error(w, "candidate unavailable", http.StatusNotFound)
		return
	}
	deadline, cancel := context.WithTimeout(r.Context(), time.Duration(h.spec.TimeoutSeconds)*time.Second)
	defer cancel()
	code, out, runErr := h.c.Executor.Execute(deadline, c, h.spec.Argv, clone(h.spec.Environment), time.Duration(h.spec.TimeoutSeconds)*time.Second, h.spec.MaxOutputBytes)
	// Reload after execution to ensure the object remained byte-identical.
	post, loadErr := h.c.Store.Get(context.Background(), c.Digest, c.SnapshotSHA256)
	if loadErr != nil || post.Digest != c.Digest || post.SnapshotSHA256 != c.SnapshotSHA256 {
		http.Error(w, "candidate changed during verification", http.StatusConflict)
		return
	}
	if runErr != nil || deadline.Err() != nil || int64(len(out)) > h.spec.MaxOutputBytes || credentialOutput(out) {
		http.Error(w, "verifier execution failed", http.StatusUnprocessableEntity)
		return
	}
	writeJSON(w, http.StatusOK, executeResponse{Protocol: verification.RemoteProtocol, ProofSHA256: proofDigest, ExitCode: code, StdoutBase64: base64.StdEncoding.EncodeToString(out)})
}

// verifyProof validates the external control signature and all bindings. No
// signing key or default fixture issuer is installed by this package.
func verifyProof(p verification.ControlProof, pd, sd string, issuers []verification.TrustedIssuer, now time.Time) error {
	if p.Version != verification.Version || p.PolicySHA256 != pd || p.VerifierSHA256 != sd || p.Scope != "candidate-offline" || !isHash(p.CandidateSHA256) || !isHash(p.SnapshotSHA256) {
		return errors.New("proof binding mismatch")
	}
	observed, e := time.Parse(time.RFC3339, p.ObservedAt)
	if e != nil || observed.After(now) {
		return errors.New("invalid proof observation")
	}
	until, e := time.Parse(time.RFC3339, p.ValidUntil)
	if e != nil || now.After(until) || !until.After(observed) || until.Sub(observed) > 5*time.Minute {
		return errors.New("expired or overlong proof")
	}
	required := map[string]bool{"filesystem-isolation": false, "readonly-candidate": false, "network-deny": false, "credential-isolation": false, "time-bound": false, "output-bound": false}
	for _, v := range p.Controls {
		if _, ok := required[v]; ok {
			required[v] = true
		}
	}
	for _, ok := range required {
		if !ok {
			return errors.New("required control absent")
		}
	}
	return verification.VerifyControlProof(p, pd, sd, issuers, now)
}
func strictJSON(b []byte, out any) error {
	if len(b) > requestLimit || duplicateJSON(b) != nil {
		return errors.New("invalid JSON")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}
func duplicateJSON(b []byte) error {
	var x any
	d := json.NewDecoder(bytes.NewReader(b))
	if e := d.Decode(&x); e != nil {
		return e
	} // Re-marshal does not detect duplicate keys; token walk below does.
	d = json.NewDecoder(bytes.NewReader(b))
	return scanJSON(d)
}
func scanJSON(d *json.Decoder) error {
	t, e := d.Token()
	if e != nil {
		return e
	}
	del, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch del {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := k.(string)
			if !ok || seen[s] {
				return errors.New("duplicate key")
			}
			seen[s] = true
			if e = scanJSON(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	case '[':
		for d.More() {
			if e := scanJSON(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	}
	return errors.New("invalid JSON")
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	b, e := json.Marshal(v)
	if e != nil {
		http.Error(w, "encoding failure", http.StatusInternalServerError)
		return
	}
	if len(b) > verification.MaxVerifierOutputBytes*2+responseOverhead {
		http.Error(w, "response exceeds bound", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
func clone(m map[string]string) map[string]string {
	x := make(map[string]string, len(m))
	for k, v := range m {
		x[k] = v
	}
	return x
}
func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}
func hasSecretEnv(m map[string]string) bool {
	for k := range m {
		n := strings.ToLower(strings.NewReplacer("_", "", "-", "", " ", "").Replace(k))
		for _, x := range []string{"token", "secret", "password", "authorization", "apikey", "credential", "privatekey"} {
			if strings.Contains(n, x) {
				return true
			}
		}
	}
	return false
}

var secretOutput = []byte("-----BEGIN ")

func credentialOutput(b []byte) bool {
	lower := bytes.ToLower(b)
	for _, p := range [][]byte{secretOutput, []byte("api_key"), []byte("\"apikey\""), []byte("access_token"), []byte("client_secret"), []byte("password"), []byte("authorization"), []byte("bearer ")} {
		if bytes.Contains(lower, bytes.ToLower(p)) {
			return true
		}
	}
	return false
}

// DockerExecutor invokes one protected Docker executable and one immutable
// image digest. Docker must itself be independently qualified by operators.
type DockerExecutor struct {
	DockerPath      string
	DockerSHA256    string
	DockerHost      string
	DockerConfigDir string
	ImageDigest     string
	MemoryLimit     string
	PidsLimit       int
	MaxCPUs         string
}

// IdentitySHA256 derives the controls executor identity from the measured
// Docker binary, immutable image, and enforced isolation/resource pins.
func (d DockerExecutor) IdentitySHA256() (string, error) {
	if !validImageDigest(d.ImageDigest) || !validMemory(d.MemoryLimit) || !validCPU(d.MaxCPUs) || d.PidsLimit < 1 || d.PidsLimit > 1024 || validateDockerContext(d.DockerHost, d.DockerConfigDir) != nil {
		return "", errors.New("invalid Docker isolation identity configuration")
	}
	binarySHA, binaryPath, err := dockerBinaryIdentity(d.DockerPath, d.DockerSHA256)
	if err != nil {
		return "", err
	}
	identity := struct {
		Version           string `json:"version"`
		BinaryPath        string `json:"binaryPath"`
		BinarySHA256      string `json:"binarySha256"`
		ImageDigest       string `json:"imageDigest"`
		DockerHost        string `json:"dockerHost"`
		DockerConfigDir   string `json:"dockerConfigDir"`
		Memory            string `json:"memory"`
		Pids              int    `json:"pids"`
		CPUs              string `json:"cpus"`
		Network           string `json:"network"`
		RootFS            string `json:"rootfs"`
		CandidateMount    string `json:"candidateMount"`
		Capabilities      string `json:"capabilities"`
		NoNewPrivileges   bool   `json:"noNewPrivileges"`
		User              string `json:"user"`
		Tmpfs             string `json:"tmpfs"`
		TimeoutMaxSeconds int    `json:"timeoutMaxSeconds"`
		OutputMaxBytes    int64  `json:"outputMaxBytes"`
	}{Version: "aok-docker-controls/v1", BinaryPath: binaryPath, BinarySHA256: binarySHA, ImageDigest: d.ImageDigest, DockerHost: d.DockerHost, DockerConfigDir: d.DockerConfigDir, Memory: d.MemoryLimit, Pids: d.PidsLimit, CPUs: d.MaxCPUs,
		Network: "none", RootFS: "read-only", CandidateMount: "read-only", Capabilities: "drop-all", NoNewPrivileges: true, User: "65532:65532", Tmpfs: "/tmp:rw,noexec,nosuid,nodev,size=64m", TimeoutMaxSeconds: verification.MaxVerifierTimeoutSeconds, OutputMaxBytes: verification.MaxVerifierOutputBytes}
	return verification.Digest(identity)
}

func (d DockerExecutor) Execute(ctx context.Context, c verification.Candidate, argv []string, env map[string]string, timeout time.Duration, limit int64) (int, []byte, error) {
	if ctx == nil || c.BundlePath == "" || len(argv) == 0 || timeout <= 0 || timeout > 30*time.Second || limit <= 0 || limit > verification.MaxVerifierOutputBytes || !isHash(d.DockerSHA256) || !validImageDigest(d.ImageDigest) || d.PidsLimit < 1 || d.PidsLimit > 1024 || !validMemory(d.MemoryLimit) || !validCPU(d.MaxCPUs) || validateDockerContext(d.DockerHost, d.DockerConfigDir) != nil {
		return 0, nil, errors.New("invalid protected Docker configuration")
	}
	fresh, e := verification.LoadCandidate(c.BundlePath)
	if e != nil || fresh.Digest != c.Digest || fresh.SnapshotSHA256 != c.SnapshotSHA256 {
		return 0, nil, errors.New("Docker backend requires exact validated candidate snapshot")
	}
	_, abs, e := dockerBinaryIdentity(d.DockerPath, d.DockerSHA256)
	if e != nil {
		return 0, nil, e
	}
	var nonce [12]byte
	if _, e = rand.Read(nonce[:]); e != nil {
		return 0, nil, e
	}
	name := "aok-verifier-" + hex.EncodeToString(nonce[:])
	args := []string{"create", "--name", name, "--pull=never", "--network=none", "--read-only", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--user", "65532:65532", "--pids-limit", strconv.Itoa(d.PidsLimit), "--memory", d.MemoryLimit, "--memory-swap", d.MemoryLimit, "--cpus", d.MaxCPUs, "--mount", "type=bind,src=" + c.BundlePath + ",dst=/candidate,readonly", "--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=64m", "--workdir", "/candidate", "--env", "HOME=/nonexistent"}
	keys := make([]string, 0, len(env))
	for k := range env {
		if strings.ContainsAny(k, "=\x00\r\n") || strings.ContainsAny(env[k], "\x00\r\n") {
			return 0, nil, errors.New("unsafe verifier environment")
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--env", k+"="+env[k])
	}
	args = append(args, d.ImageDigest)
	args = append(args, argv...)
	createCtx, createCancel := context.WithTimeout(ctx, 5*time.Second)
	createOutput, createErr := runDocker(createCtx, abs, d, args, 256)
	createCancel()
	containerID := strings.TrimSpace(string(createOutput))
	if createErr != nil || !validContainerID(containerID) {
		if cleanupContainer(abs, d, name, true) != nil {
			return 0, nil, errors.New("container creation was ambiguous and cleanup could not be confirmed")
		}
		return 0, nil, errors.New("container creation result was ambiguous")
	}
	// Keep the daemon-returned immutable container ID for every lifecycle action.
	name = containerID
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var out capBuffer
	out.limit = limit
	out.cancel = cancel
	_, startErr := runDockerWithWriter(bounded, abs, d, []string{"start", "--attach", "--sig-proxy=false", containerID}, &out)
	var code int
	if startErr == nil && bounded.Err() == nil && !out.overflow {
		waitCtx, waitCancel := context.WithTimeout(bounded, 5*time.Second)
		waitOutput, waitErr := runDocker(waitCtx, abs, d, []string{"wait", containerID}, 64)
		waitCancel()
		if waitErr == nil {
			parsed, parseErr := strconv.Atoi(strings.TrimSpace(string(waitOutput)))
			if parseErr == nil && parsed >= 0 && parsed <= 255 {
				code = parsed
			} else {
				startErr = errors.New("invalid container exit status")
			}
		} else {
			startErr = errors.New("container exit status unavailable")
		}
	} else if startErr == nil {
		startErr = errors.New("container execution exceeded bound")
	}
	// stop/wait/remove plus a daemon query for absence are required regardless of
	// start outcome. This covers output cancellation and response loss.
	uncertainLifecycle := startErr != nil || bounded.Err() != nil || out.overflow
	if cleanupContainer(abs, d, containerID, uncertainLifecycle) != nil {
		return 0, nil, errors.New("sandbox stop or cleanup could not be confirmed")
	}
	if bounded.Err() != nil {
		return 0, nil, bounded.Err()
	}
	if out.overflow {
		return 0, nil, errors.New("verifier output limit exceeded")
	}
	if startErr != nil {
		return 0, nil, errors.New("sandbox execution outcome is uncertain")
	}
	return code, out.b, nil

}

func dockerEnv(d DockerExecutor) []string {
	return []string{"PATH=/usr/bin:/bin", "DOCKER_HOST=" + d.DockerHost, "DOCKER_CONFIG=" + d.DockerConfigDir}
}
func runDocker(ctx context.Context, binary string, d DockerExecutor, args []string, limit int64) ([]byte, error) {
	bounded, cancel := context.WithCancel(ctx)
	defer cancel()
	var out capBuffer
	out.limit = limit
	out.cancel = cancel
	full := append([]string{"--host", d.DockerHost, "--config", d.DockerConfigDir}, args...)
	cmd := exec.CommandContext(bounded, binary, full...)
	cmd.Env = dockerEnv(d)
	cmd.Stdout = &out
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if bounded.Err() != nil {
		return nil, bounded.Err()
	}
	if out.overflow {
		return nil, errors.New("Docker response exceeded limit")
	}
	if err != nil {
		return nil, errors.New("Docker lifecycle operation failed")
	}
	return out.b, nil
}
func runDockerWithWriter(ctx context.Context, binary string, d DockerExecutor, args []string, w io.Writer) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("Docker operation context required")
	}
	full := append([]string{"--host", d.DockerHost, "--config", d.DockerConfigDir}, args...)
	cmd := exec.CommandContext(ctx, binary, full...)
	cmd.Env = dockerEnv(d)
	if w == nil {
		w = io.Discard
	}
	cmd.Stdout = w
	cmd.Stderr = w
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, errors.New("Docker lifecycle operation failed")
	}
	return nil, nil
}

func cleanupContainer(binary string, d DockerExecutor, id string, watchLate bool) error {
	cleanCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Stop and wait by immutable ID even if create/start returned ambiguously.
	stopCtx, stopCancel := context.WithTimeout(cleanCtx, 4*time.Second)
	_, _ = runDocker(stopCtx, binary, d, []string{"stop", "--time", "0", id}, 256)
	stopCancel()
	waitCtx, waitCancel := context.WithTimeout(cleanCtx, 4*time.Second)
	_, _ = runDocker(waitCtx, binary, d, []string{"wait", id}, 64)
	waitCancel()
	rmCtx, rmCancel := context.WithTimeout(cleanCtx, 4*time.Second)
	_, _ = runDocker(rmCtx, binary, d, []string{"rm", "--force", id}, 256)
	rmCancel()
	checks := 1
	if watchLate {
		checks = 8
	}
	for i := 0; i < checks; i++ {
		filter, format := "id="+id, "{{.ID}}"
		if !validContainerID(id) {
			filter = "name=^/" + id + "$"
			format = "{{.Names}}"
		}
		psCtx, psCancel := context.WithTimeout(cleanCtx, 3*time.Second)
		out, err := runDocker(psCtx, binary, d, []string{"ps", "--all", "--filter", filter, "--format", format}, 512)
		psCancel()
		listed := strings.TrimSpace(string(out))
		if err != nil || listed != "" {
			return errors.New("container absence was not confirmed")
		}
		if i+1 < checks {
			select {
			case <-cleanCtx.Done():
				return cleanCtx.Err()
			case <-time.After(250 * time.Millisecond):
			}
		}
	}
	return nil
}

func validContainerID(s string) bool {
	if len(s) < 12 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func dockerBinaryIdentity(path, expected string) (string, string, error) {
	if !isHash(expected) {
		return "", "", errors.New("pinned Docker executable digest is invalid")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	st, err := os.Lstat(abs)
	if err != nil || !st.Mode().IsRegular() || st.Mode()&0111 == 0 {
		return "", "", errors.New("pinned Docker executable unavailable")
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	if got != expected {
		return "", "", errors.New("Docker executable digest mismatch")
	}
	return got, abs, nil
}

func validateDockerContext(host, configDir string) error {
	if !strings.HasPrefix(host, "unix://") {
		return errors.New("Docker host must be a pinned Unix socket")
	}
	socketPath := strings.TrimPrefix(host, "unix://")
	if !filepath.IsAbs(socketPath) || strings.ContainsAny(socketPath, "\x00\r\n") {
		return errors.New("invalid Docker socket path")
	}
	st, err := os.Lstat(socketPath)
	if err == nil && (st.Mode()&os.ModeSocket == 0 || st.Mode()&os.ModeSymlink != 0) {
		return errors.New("pinned Docker socket path is unsafe")
	}
	if err != nil && !os.IsNotExist(err) {
		return errors.New("pinned Docker socket path is unavailable")
	}
	abs, err := filepath.Abs(configDir)
	if err != nil {
		return err
	}
	st, err = os.Lstat(abs)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0022 != 0 {
		return errors.New("isolated Docker config directory is unavailable or writable by others")
	}
	entries, err := os.ReadDir(abs)
	if err != nil || len(entries) != 0 {
		return errors.New("Docker config directory must be empty")
	}
	return nil
}

func validImageDigest(s string) bool {
	p := strings.Split(s, "@sha256:")
	if len(p) != 2 || p[0] == "" || !isHash(p[1]) {
		return false
	}
	for _, r := range p[0] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("./:_-", r)) {
			return false
		}
	}
	return true
}
func validMemory(s string) bool {
	if len(s) < 2 {
		return false
	}
	unit := s[len(s)-1]
	n, e := strconv.ParseInt(s[:len(s)-1], 10, 64)
	if e != nil || n < 1 {
		return false
	}
	switch unit {
	case 'm', 'M':
		return n <= 16384
	case 'g', 'G':
		return n <= 16
	default:
		return false
	}
}
func validCPU(s string) bool {
	n, e := strconv.ParseFloat(s, 64)
	return e == nil && n >= 0.1 && n <= 8
}

type capBuffer struct {
	b        []byte
	limit    int64
	overflow bool
	cancel   context.CancelFunc
}

func (c *capBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if int64(len(c.b))+int64(n) > c.limit {
		remain := int(c.limit) - len(c.b)
		if remain > 0 {
			c.b = append(c.b, p[:remain]...)
		}
		c.overflow = true
		if c.cancel != nil {
			c.cancel()
		}
		return n, nil
	}
	c.b = append(c.b, p...)
	return n, nil
}
