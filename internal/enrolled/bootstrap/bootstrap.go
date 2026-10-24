// Package bootstrap assembles the narrow protected service entrypoints used by
// enrolled deployments. It does not provision credentials or qualify a host.
package bootstrap

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/anchorserver"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/artifacts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/authority"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/verifier"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

const (
	EnvelopeVersion = "aok-protected-bootstrap/v1"
	ConfigVersion   = "aok-enrolled-service-config/v1"
	maxConfigBytes  = 4 << 20
)

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

var ErrBlocked = errors.New("protected service bootstrap denied")

// Envelope signs the exact bytes in Payload. The root key is supplied in a
// separate file and pinned into the executable at build time.
type Envelope struct {
	Version   string          `json:"version"`
	IssuedAt  string          `json:"issuedAt"`
	ExpiresAt string          `json:"expiresAt"`
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
}

func SigningBytes(e Envelope) []byte {
	return append([]byte(EnvelopeVersion+"\n"+e.IssuedAt+"\n"+e.ExpiresAt+"\n"), e.Payload...)
}

type TLSFiles struct {
	Certificate string `json:"certificate"`
	PrivateKey  string `json:"privateKey"`
}

type AuthorityConfig struct {
	DatabasePath string `json:"databasePath"`
	ServerID     string `json:"serverId"`
	// The authority's monotonic tip lives in a separately retained HTTPS
	// append service. Its public key and stream identity are root-signed; the
	// token itself is resolved lazily only after enrollment validation.
	AnchorEndpoint        string                 `json:"anchorEndpoint"`
	AnchorPublicKey       string                 `json:"anchorPublicKey"`
	AnchorTokenRef        string                 `json:"anchorTokenRef"`
	InitialAnchorSHA256   string                 `json:"initialAnchorSha256"`
	OperatorPublicKey     string                 `json:"operatorPublicKey"`
	ResponsePrivateKeyRef string                 `json:"responsePrivateKeyRef"`
	Issuers               []authority.Issuer     `json:"issuers"`
	Enrollments           []authority.Enrollment `json:"enrollments"`
	TokenRefs             map[string]string      `json:"tokenRefs"`
}

type DockerConfig struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	Host        string `json:"host"`
	ConfigDir   string `json:"configDir"`
	ImageDigest string `json:"imageDigest"`
	MemoryLimit string `json:"memoryLimit"`
	PidsLimit   int    `json:"pidsLimit"`
	MaxCPUs     string `json:"maxCpus"`
}

type VerifierConfig struct {
	ArtifactsRoot string                          `json:"artifactsRoot"`
	Policy        verification.VerificationPolicy `json:"policy"`
	VerifierID    string                          `json:"verifierId"`
	AuthReference string                          `json:"authReference"`
	Docker        DockerConfig                    `json:"docker"`
}

type AnchorServiceConfig struct {
	Directory             string `json:"directory"`
	StreamID              string `json:"streamId"`
	InitialTipSHA256      string `json:"initialTipSha256"`
	ResponsePrivateKeyRef string `json:"responsePrivateKeyRef"`
	TokenRef              string `json:"tokenRef"`
}

type Config struct {
	Qualification *QualificationRecords `json:"qualification,omitempty"`
	Version       string                `json:"version"`
	Service       string                `json:"service"`
	Listen        string                `json:"listen"`
	TLS           TLSFiles              `json:"tls"`
	SecretEnv     map[string]string     `json:"secretEnv"`
	Authority     *AuthorityConfig      `json:"authority,omitempty"`
	Verifier      *VerifierConfig       `json:"verifier,omitempty"`
	Anchor        *AnchorServiceConfig  `json:"anchor,omitempty"`
	Worker        *WorkerConfig         `json:"worker,omitempty"`
}

// Prepared is ready for a TLS listener. Close must be called on shutdown.
type Prepared struct {
	Handler   http.Handler
	TLSConfig *tls.Config
	listener  string
	service   string
	close     func() error
}

func (p *Prepared) Service() string {
	if p == nil {
		return ""
	}
	return p.service
}

func (p *Prepared) Close() error {
	if p == nil || p.close == nil {
		return nil
	}
	return p.close()
}

// Inspect verifies envelope integrity and expiry without resolving secrets or
// opening stores. pinned is false for the stock build, whose root pin is empty.
func Inspect(configPath, rootPath, compiledRootSHA256 string, now time.Time) (map[string]any, error) {
	e, c, root, err := loadAndVerify(configPath, rootPath, compiledRootSHA256, false, now)
	if err != nil {
		return nil, err
	}
	if err = validateServiceConfig(c, now); err != nil {
		return nil, err
	}
	rootSum := sha256.Sum256(root)
	return map[string]any{
		"version": e.Version, "service": c.Service, "listen": c.Listen,
		"issuedAt": e.IssuedAt, "expiresAt": e.ExpiresAt,
		"signatureValid": true, "rootSHA256": hex.EncodeToString(rootSum[:]),
		"rootPinned": compiledRootSHA256 != "", "secretReferenceCount": len(c.SecretEnv),
		"workerAssembly":             workerInspection(c),
		"anchorStorageQualification": anchorInspection(c),
	}, nil
}

func anchorInspection(c Config) any {
	if c.Service != "anchor" {
		return "not-selected"
	}
	return map[string]string{"storage": "separate-append-only-endpoint", "genesis": "pre-provisioned-required", "qualification": "not-established"}
}

func workerInspection(c Config) any {
	if c.Service != "worker" {
		return "not-selected"
	}
	return map[string]string{"calibrationExperiment": "one-shot-assembly-implemented", "verifierControls": "on-demand-assembly-implemented", "githubCapture": "protected-entrypoint-implemented", "codexQuota": "protected-entrypoint-implemented", "githubDispatch": "exact-operation-entrypoint-implemented", "liveQualification": "not-established", "productionPilot": "disabled"}
}

// Build verifies the externally pinned root and fresh signed configuration,
// validates service enrollment, and only then resolves secret references.
func Build(configPath, rootPath, compiledRootSHA256 string, now time.Time) (*Prepared, error) {
	_, c, _, err := loadAndVerify(configPath, rootPath, compiledRootSHA256, true, now)
	if err != nil {
		return nil, err
	}
	if err = validateServiceConfig(c, now); err != nil {
		return nil, err
	}
	certPEM, err := readSafe(c.TLS.Certificate, 1<<20, false)
	if err != nil {
		return nil, ErrBlocked
	}
	keyPEM, err := readSafe(c.TLS.PrivateKey, 1<<20, true)
	if err != nil {
		return nil, ErrBlocked
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, ErrBlocked
	}
	p := &Prepared{TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, listener: c.Listen, service: c.Service}
	switch c.Service {
	case "authority":
		ac := c.Authority
		op, err := decodeKey(ac.OperatorPublicKey, ed25519.PublicKeySize)
		if err != nil {
			return nil, ErrBlocked
		}
		for _, enrollment := range ac.Enrollments {
			if err := authority.ValidateEnrollment(enrollment, ed25519.PublicKey(op), ac.Issuers, now); err != nil {
				return nil, ErrBlocked
			}
		}
		response, err := resolveSecret(c, ac.ResponsePrivateKeyRef)
		if err != nil {
			return nil, ErrBlocked
		}
		responseKey, err := decodeKey(response, ed25519.PrivateKeySize)
		if err != nil {
			return nil, ErrBlocked
		}
		tokens := make(map[string]string, len(ac.TokenRefs))
		for principal, ref := range ac.TokenRefs {
			token, err := resolveSecret(c, ref)
			if err != nil || token == "" {
				return nil, ErrBlocked
			}
			tokens[principal] = token
		}
		anchorKey, err := decodeKey(ac.AnchorPublicKey, ed25519.PublicKeySize)
		if err != nil {
			return nil, ErrBlocked
		}
		anchor := authority.AnchorClient{
			Endpoint: ac.AnchorEndpoint, StreamID: ac.ServerID,
			PublicKey: ed25519.PublicKey(anchorKey),
			Token:     func(ctx context.Context) (string, error) { return resolveSecret(c, ac.AnchorTokenRef) },
		}
		store, err := authority.Open(authority.Config{Anchor: anchor, InitialAnchorSHA256: ac.InitialAnchorSHA256, Database: ac.DatabasePath, ServerID: ac.ServerID, OperatorKey: ed25519.PublicKey(op), ResponseKey: ed25519.PrivateKey(responseKey), Issuers: ac.Issuers, Enrollments: ac.Enrollments, Tokens: tokens})
		if err != nil {
			return nil, ErrBlocked
		}
		p.Handler = store.Handler()
		p.close = store.Close
	case "verifier":
		vc := c.Verifier
		d := verifier.DockerExecutor{DockerPath: vc.Docker.Path, DockerSHA256: vc.Docker.SHA256, DockerHost: vc.Docker.Host, DockerConfigDir: vc.Docker.ConfigDir, ImageDigest: vc.Docker.ImageDigest, MemoryLimit: vc.Docker.MemoryLimit, PidsLimit: vc.Docker.PidsLimit, MaxCPUs: vc.Docker.MaxCPUs}
		if _, err = d.IdentitySHA256(); err != nil {
			return nil, ErrBlocked
		}
		resolver := environmentResolver{references: c.SecretEnv}
		store, err := artifacts.NewStore(vc.ArtifactsRoot, vc.Policy, vc.VerifierID)
		if err != nil {
			return nil, ErrBlocked
		}
		h, err := verifier.NewHandler(verifier.Config{Store: store, Policy: vc.Policy, VerifierID: vc.VerifierID, AuthReference: vc.AuthReference, Resolver: resolver, Executor: d})
		if err != nil {
			return nil, ErrBlocked
		}
		p.Handler = h
	case "anchor":
		ac := c.Anchor
		response, err := resolveSecret(c, ac.ResponsePrivateKeyRef)
		if err != nil {
			return nil, ErrBlocked
		}
		key, err := decodeKey(response, ed25519.PrivateKeySize)
		if err != nil {
			return nil, ErrBlocked
		}
		server, err := anchorserver.New(anchorserver.Config{Directory: ac.Directory, StreamID: ac.StreamID, InitialTipSHA256: ac.InitialTipSHA256, ResponsePrivateKey: ed25519.PrivateKey(key), TokenRef: ac.TokenRef, ResolveToken: func(ctx context.Context, ref string) (string, error) {
			if ref != ac.TokenRef {
				return "", ErrBlocked
			}
			return resolveSecret(c, ref)
		}})
		if err != nil {
			return nil, ErrBlocked
		}
		p.Handler = server.Handler()
	case "worker":
		return nil, errors.New("worker assembly is not installed in this binary")
	default:
		return nil, ErrBlocked
	}
	return p, nil
}

// Serve runs the assembled service with TLS, bounded headers and no redirects.
func Serve(ctx context.Context, p *Prepared) error {
	if ctx == nil || p == nil || p.Handler == nil || p.TLSConfig == nil || p.listener == "" {
		return ErrBlocked
	}
	ln, err := net.Listen("tcp", p.listener)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: p.Handler, TLSConfig: p.TLSConfig, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 35 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	go func() { <-ctx.Done(); _ = srv.Shutdown(context.Background()) }()
	err = srv.Serve(tls.NewListener(ln, p.TLSConfig))
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func loadAndVerify(configPath, rootPath, pin string, requirePin bool, now time.Time) (Envelope, Config, []byte, error) {
	var e Envelope
	var c Config
	if requirePin && !validHash(pin) {
		return e, c, nil, errors.New("protected service is disabled: executable root pin is empty or invalid")
	}
	root, err := readSafe(rootPath, ed25519.PublicKeySize, false)
	if err != nil || len(root) != ed25519.PublicKeySize {
		return e, c, nil, errors.New("protected root file rejected")
	}
	rootHash := sha256.Sum256(root)
	if pin != "" && subtleEqualHex(pin, rootHash[:]) != true {
		return e, c, nil, ErrBlocked
	}
	if requirePin && pin == "" {
		return e, c, nil, ErrBlocked
	}
	b, err := readSafe(configPath, maxConfigBytes, false)
	if err != nil {
		return e, c, nil, errors.New("protected config file rejected")
	}
	if usage.Decode(b, &e) != nil {
		return e, c, nil, errors.New("protected config encoding rejected")
	}
	if e.Version != EnvelopeVersion || len(e.Payload) == 0 {
		return e, c, nil, errors.New("protected config version rejected")
	}
	issued, ie := time.Parse(time.RFC3339Nano, e.IssuedAt)
	expires, ee := time.Parse(time.RFC3339Nano, e.ExpiresAt)
	if ie != nil || ee != nil || issued.After(now.Add(time.Second)) || !now.Before(expires) || expires.Sub(issued) > 24*time.Hour {
		return e, c, nil, errors.New("protected bootstrap enrollment expired or invalid")
	}
	sig, err := base64.StdEncoding.DecodeString(e.Signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(root), SigningBytes(e), sig) {
		return e, c, nil, errors.New("protected bootstrap signature invalid")
	}
	if usage.Decode(e.Payload, &c) != nil {
		return e, c, nil, errors.New("protected payload encoding rejected")
	}
	return e, c, root, nil
}

func validateServiceConfig(c Config, now time.Time) error {
	if c.Version != ConfigVersion || len(c.SecretEnv) > 32 || (c.Qualification != nil && c.Service != "qualification") {
		return ErrBlocked
	}
	for ref, env := range c.SecretEnv {
		if ref == "" || len(ref) > 256 || envName.MatchString(env) == false {
			return ErrBlocked
		}
	}
	switch c.Service {
	case "qualification":
		return validateQualification(c, now)
	case "authority":
		if c.Listen == "" || c.TLS.Certificate == "" || c.TLS.PrivateKey == "" || c.Authority == nil || c.Verifier != nil || c.Anchor != nil || c.Worker != nil || c.Authority.DatabasePath == "" || c.Authority.ServerID == "" || c.Authority.AnchorEndpoint == "" || c.Authority.AnchorTokenRef == "" || c.Authority.InitialAnchorSHA256 == "" || !validHash(c.Authority.InitialAnchorSHA256) || len(c.Authority.Enrollments) == 0 || len(c.Authority.TokenRefs) == 0 {
			return ErrBlocked
		}
		anchorKey, err := decodeKey(c.Authority.AnchorPublicKey, ed25519.PublicKeySize)
		if err != nil || !validHTTPSPath(c.Authority.AnchorEndpoint, "/v1/anchor") {
			return ErrBlocked
		}
		_ = anchorKey
		for _, ref := range append([]string{c.Authority.ResponsePrivateKeyRef, c.Authority.AnchorTokenRef}, mapValues(c.Authority.TokenRefs)...) {
			if _, ok := c.SecretEnv[ref]; !ok {
				return ErrBlocked
			}
		}
		op, err := decodeKey(c.Authority.OperatorPublicKey, ed25519.PublicKeySize)
		if err != nil {
			return ErrBlocked
		}
		for _, en := range c.Authority.Enrollments {
			if authority.ValidateEnrollment(en, ed25519.PublicKey(op), c.Authority.Issuers, now) != nil {
				return errors.New("protected authority enrollment invalid or expired")
			}
		}
	case "verifier":
		if c.Listen == "" || c.TLS.Certificate == "" || c.TLS.PrivateKey == "" || c.Verifier == nil || c.Authority != nil || c.Anchor != nil || c.Worker != nil || c.Verifier.AuthReference == "" || c.Verifier.ArtifactsRoot == "" {
			return ErrBlocked
		}
		if _, ok := c.SecretEnv[c.Verifier.AuthReference]; !ok {
			return ErrBlocked
		}
		if err := verification.ValidatePolicy(c.Verifier.Policy); err != nil {
			return ErrBlocked
		}
		found := false
		for _, spec := range c.Verifier.Policy.Verifiers {
			if spec.ID == c.Verifier.VerifierID && spec.Kind == "command" {
				found = true
			}
		}
		if !found {
			return ErrBlocked
		}
	case "anchor":
		if c.Listen == "" || c.TLS.Certificate == "" || c.TLS.PrivateKey == "" || c.Anchor == nil || c.Authority != nil || c.Verifier != nil || c.Worker != nil || c.Anchor.Directory == "" || c.Anchor.StreamID == "" || !validHash(c.Anchor.InitialTipSHA256) || c.Anchor.TokenRef == "" || c.Anchor.ResponsePrivateKeyRef == "" {
			return ErrBlocked
		}
		for _, ref := range []string{c.Anchor.TokenRef, c.Anchor.ResponsePrivateKeyRef} {
			if _, ok := c.SecretEnv[ref]; !ok {
				return ErrBlocked
			}
		}
	case "worker":
		return validateWorkerConfig(c, now)
	default:
		return ErrBlocked
	}
	return nil
}

func resolveSecret(c Config, ref string) (string, error) {
	env, ok := c.SecretEnv[ref]
	if !ok {
		return "", ErrBlocked
	}
	v, ok := os.LookupEnv(env)
	if !ok || v == "" || strings.ContainsAny(v, "\x00\r\n") {
		return "", ErrBlocked
	}
	return v, nil
}

type environmentResolver struct{ references map[string]string }

func (r environmentResolver) Resolve(_ context.Context, ref string) (string, error) {
	env, ok := r.references[ref]
	if !ok || !envName.MatchString(env) {
		return "", ErrBlocked
	}
	v, ok := os.LookupEnv(env)
	if !ok || v == "" || strings.ContainsAny(v, "\x00\r\n") {
		return "", ErrBlocked
	}
	return v, nil
}

func readSafe(path string, limit int64, private bool) ([]byte, error) {
	if path == "" {
		return nil, ErrBlocked
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	for p := abs; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return nil, ErrBlocked
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	st, err := os.Lstat(abs)
	if err != nil || !st.Mode().IsRegular() || st.Size() > limit || (private && st.Mode().Perm()&0077 != 0) {
		return nil, ErrBlocked
	}
	b, err := os.ReadFile(abs)
	if err != nil || int64(len(b)) > limit {
		return nil, ErrBlocked
	}
	return b, nil
}

func decodeKey(s string, want int) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != want {
		return nil, ErrBlocked
	}
	return b, nil
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

func validHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && strings.ToLower(s) == s
}

func validHTTPSPath(raw, path string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != "" && u.Path == path && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

func subtleEqualHex(s string, hash []byte) bool {
	want, err := hex.DecodeString(s)
	if err != nil || len(want) != len(hash) {
		return false
	}
	var diff byte
	for i := range hash {
		diff |= want[i] ^ hash[i]
	}
	return diff == 0
}

func RedactedInspectJSON(out map[string]any) ([]byte, error) {
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("inspect output unavailable")
	}
	return append(b, '\n'), nil
}
