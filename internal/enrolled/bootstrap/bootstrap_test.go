package bootstrap

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	githubcollector "github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/github"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/testutil"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

func testConfig(t *testing.T) (string, string, string) {
	t.Helper()
	dir := testTempDir(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(dir, "root.pub")
	if err := os.WriteFile(rootPath, pub, 0600); err != nil {
		t.Fatal(err)
	}
	issuer, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256([]byte("pin"))
	spec := verification.VerifierSpec{ID: "tests", Kind: "command", ImplementationSHA256: hex.EncodeToString(h[:]), RubricSHA256: hex.EncodeToString(h[:]), Argv: []string{"/opt/check"}, TimeoutSeconds: 2, MaxOutputBytes: 1024}
	policy := verification.VerificationPolicy{Version: verification.Version, ID: "policy", Issuers: []verification.TrustedIssuer{{ID: "controls", Role: "controls", ExecutorSHA256: hex.EncodeToString(h[:]), PublicKey: base64.StdEncoding.EncodeToString(issuer)}}, Verifiers: []verification.VerifierSpec{spec}, Criteria: []verification.Criterion{{ID: "check", Kind: "command", Required: true, VerifierID: spec.ID}}, MaxAgeSeconds: 60}
	cfg := Config{Version: ConfigVersion, Service: "verifier", Listen: "127.0.0.1:0", TLS: TLSFiles{Certificate: filepath.Join(dir, "server.crt"), PrivateKey: filepath.Join(dir, "server.key")}, SecretEnv: map[string]string{"verifier-auth": "AOK_TEST_SECRET"}, Verifier: &VerifierConfig{ArtifactsRoot: filepath.Join(dir, "artifacts"), Policy: policy, VerifierID: spec.ID, AuthReference: "verifier-auth"}}
	payload, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	e := Envelope{Version: EnvelopeVersion, IssuedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano), Payload: payload}
	e.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, SigningBytes(e)))
	body, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "service.json")
	if err := os.WriteFile(configPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	rootHash := sha256.Sum256(pub)
	return configPath, rootPath, hex.EncodeToString(rootHash[:])
}

func testTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp(testutil.CanonicalTempRoot(t), "aok-bootstrap-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func TestInspectWorksWithoutPATHAndRedactsCredentialMaterial(t *testing.T) {
	config, root, pin := testConfig(t)
	secret := "highly-sensitive-fixture-credential"
	t.Setenv("PATH", "")
	t.Setenv("AOK_TEST_SECRET", secret)
	result, err := Inspect(config, root, pin, time.Now().UTC())
	if err != nil {
		e, c, _, le := loadAndVerify(config, root, pin, false, time.Now().UTC())
		_, re := readSafe(root, ed25519.PublicKeySize, false)
		t.Fatalf("inspect %v (load=%v root=%v path=%s envelope=%+v service=%s policy=%+v)", err, le, re, root, e, c.Service, c.Verifier)
	}
	b, err := RedactedInspectJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), secret) || strings.Contains(string(b), "AOK_TEST_SECRET") {
		t.Fatalf("inspection exposed credential material: %s", b)
	}
	if result["signatureValid"] != true || result["rootPinned"] != true {
		t.Fatalf("inspection did not report verified pin: %#v", result)
	}
}

func TestBuildDefaultRootPinBlocksBeforeReadingConfigOrResolvingSecrets(t *testing.T) {
	if _, err := Build(filepath.Join(t.TempDir(), "missing.json"), filepath.Join(t.TempDir(), "missing.pub"), "", time.Now()); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("empty compiled root pin did not block service: %v", err)
	}
}

func TestInspectRejectsTamperExpiredAndUnpinnedRoot(t *testing.T) {
	config, root, pin := testConfig(t)
	body, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	var e Envelope
	if err = json.Unmarshal(body, &e); err != nil {
		t.Fatal(err)
	}
	e.Payload[len(e.Payload)-2] ^= 1
	tampered, _ := json.Marshal(e)
	if err = os.WriteFile(config, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Inspect(config, root, pin, time.Now()); err == nil {
		t.Fatal("tampered envelope accepted")
	}
	config, root, pin = testConfig(t)
	body, _ = os.ReadFile(config)
	if err = json.Unmarshal(body, &e); err != nil {
		t.Fatal(err)
	}
	e.ExpiresAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	// The signature is intentionally not refreshed: expiry is denied before any
	// service or secret material is examined.
	expired, _ := json.Marshal(e)
	if err = os.WriteFile(config, expired, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Inspect(config, root, pin, time.Now()); err == nil {
		t.Fatal("expired enrollment accepted")
	}
	config, root, pin = testConfig(t)
	if _, err = Inspect(config, root, strings.Repeat("0", 64), time.Now()); err == nil {
		t.Fatal("root mismatch accepted")
	}
}

func TestSafeFileRejectsSymlinkAncestors(t *testing.T) {
	dir := testTempDir(t)
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(real, "config")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readSafe(filepath.Join(link, "config"), 10, false); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
}

func TestWorkerRequiresExactEnrolledRecordDigests(t *testing.T) {
	worker := &WorkerConfig{RunID: "run-one", Mode: "calibration", Calibration: &CalibrationRecords{Manifest: structManifest(), Grant: structGrant(), Request: structRequest(), Bounds: json.RawMessage(`{"bounds":[]}`)}}
	worker.Authority.Bindings = map[string]string{
		"run-id":               usage.Digest(worker.RunID),
		"calibration-manifest": usage.Digest(worker.Calibration.Manifest),
		"calibration-grant":    usage.Digest(worker.Calibration.Grant),
		"calibration-request":  usage.Digest(worker.Calibration.Request),
		"calibration-bounds":   usage.Hash(worker.Calibration.Bounds),
		"verifier-policy":      usage.Digest(worker.Verifier.Policy),
		"verifier-proof":       usage.Digest(worker.Verifier.Proof),
		"verifier-endpoint":    usage.Digest(worker.Verifier.Endpoint),
		"witness-proof":        usage.Hash(nil),
	}
	worker.Authority.Enrollment.Bindings = cloneStringMap(worker.Authority.Bindings)
	if err := checkWorkerRecordBindings(worker); err != nil {
		t.Fatalf("matching enrolled record hashes rejected: %v", err)
	}
	worker.Authority.Bindings["calibration-grant"] = strings.Repeat("0", 64)
	if err := checkWorkerRecordBindings(worker); err == nil {
		t.Fatal("mismatched grant digest accepted")
	}
}

func TestGitHubWriteRequiresBothExactScopedAuthorityBindings(t *testing.T) {
	source := tracker.Source{Version: tracker.Version, Provider: "github", Origin: "https://api.github.com", NativeTenantID: "tenant", ContainerID: "repo", ContainerKey: "owner/repo", PrincipalPseudonym: "principal", MappingVersion: "map-1", APIVersion: "2022-11-28", Auth: tracker.Auth{Mode: "gh-session"}, Scope: tracker.Scope{ItemIDs: []string{"issue:17"}}}
	policy := githubPolicyFixture(source)
	source.ReadPolicySHA256 = tracker.ReadPolicyHash(policy)
	assets, evidenceRoot := t.TempDir(), t.TempDir()
	comment := []byte("fixture comment")
	if err := os.WriteFile(filepath.Join(assets, "comment.md"), comment, 0600); err != nil {
		t.Fatal(err)
	}
	id := contracts.RemoteIdentity{Provider: "github", RemoteID: "node-id-17", ItemKind: "issue"}
	g := &GitHubWriteRecords{ImmutableIssueID: "node-id-17", IssueNumber: "17", CommentAssetsRoot: assets, EvidenceRoot: evidenceRoot, Collector: githubcollector.Config{Source: source, Policy: policy}, Credential: TokenMetadata{PrincipalPseudonym: "principal", GrantRef: "grant", Scopes: []string{"issues:write", "metadata:read"}}, Identity: id, Capability: contracts.ConnectorCapability{Scope: id}, Operation: contracts.SyncOperation{OperationKind: "comment", Authority: contracts.ExternalAuthorityRef{Scope: id}, Change: contracts.SyncChange{Comment: &contracts.AssetRef{Path: "comment.md", SHA256: tracker.Hash(comment)}}}}
	base := githubcollector.Authorization{SourceSHA256: tracker.ScopeDigest(source), PolicySHA256: tracker.ReadPolicyHash(policy), Origin: source.Origin, Principal: "principal", GrantRef: "grant", ItemIDs: []string{"issue:17"}}
	read := base
	read.Method = "GET"
	read.Path = "/repos/owner/repo"
	read.Scopes = []string{"metadata:read"}
	write := base
	write.Method = "POST"
	write.Path = "/repos/owner/repo/issues/17/comments"
	write.Scopes = []string{"issues:write", "metadata:read"}
	bindings := map[string]string{}
	bindings["github-read:"+usage.Digest(read)] = usage.Digest(read)
	bindings["github-write:"+usage.Digest(write)] = usage.Digest(write)
	bindings["github-operation-config"] = usage.Digest(*g)
	a := WorkerAuthority{Bindings: bindings}
	if err := validateGitHubWriteBindings(a, g); err != nil {
		t.Fatalf("exact scoped route rejected: %v", err)
	}
	delete(bindings, "github-write:"+usage.Digest(write))
	if err := validateGitHubWriteBindings(a, g); err == nil {
		t.Fatal("write route without its enrolled authority binding accepted")
	}
}

func TestAnchorServiceRequiresSignedTokenRefsAndPreprovisionedDescriptor(t *testing.T) {
	h := sha256.Sum256([]byte("anchor-tip"))
	c := Config{Version: ConfigVersion, Service: "anchor", Listen: "127.0.0.1:0", TLS: TLSFiles{Certificate: "cert", PrivateKey: "key"}, SecretEnv: map[string]string{"anchor-response": "AOK_RESPONSE", "anchor-token": "AOK_TOKEN"}, Anchor: &AnchorServiceConfig{Directory: "/private/preprovisioned/anchor", StreamID: "pilot-stream", InitialTipSHA256: hex.EncodeToString(h[:]), ResponsePrivateKeyRef: "anchor-response", TokenRef: "anchor-token"}}
	if err := validateServiceConfig(c, time.Now()); err != nil {
		t.Fatalf("valid separately provisioned anchor descriptor rejected: %v", err)
	}
	delete(c.SecretEnv, "anchor-token")
	if err := validateServiceConfig(c, time.Now()); err == nil {
		t.Fatal("anchor without an opaque bearer-token reference accepted")
	}
}

func structManifest() pilot.Manifest      { return pilot.Manifest{ID: "manifest-fixture"} }
func structGrant() pilot.CalibrationGrant { return pilot.CalibrationGrant{ID: "grant-fixture"} }
func structRequest() pilot.CalibrationRequest {
	return pilot.CalibrationRequest{TrialID: "trial-fixture"}
}
