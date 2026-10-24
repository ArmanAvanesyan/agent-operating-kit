package bootstrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/github"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

type githubWriteRoundTrip func(*http.Request) (*http.Response, error)

func (f githubWriteRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func githubPolicyFixture(s tracker.Source) *tracker.ReadPolicy {
	return &tracker.ReadPolicy{Version: s.Version, Rules: []tracker.ReadRule{{Provider: "github", Origin: s.Origin, NativeTenantID: s.NativeTenantID, ContainerID: s.ContainerID, ContainerKey: s.ContainerKey, Auth: s.Auth, PrincipalPseudonym: s.PrincipalPseudonym}}}
}

func TestLoadGitHubCommentBoundedAndHashVerified(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte("approved comment")
	if err := os.WriteFile(filepath.Join(root, "assets", "comment.md"), body, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := loadGitHubComment(root, "assets/comment.md", tracker.Hash(body))
	if err != nil || string(got) != string(body) {
		t.Fatalf("load = %q, %v", got, err)
	}
	if _, err := loadGitHubComment(root, "assets/comment.md", tracker.Hash([]byte("changed"))); err == nil {
		t.Fatal("changed asset accepted")
	}
	if _, err := loadGitHubComment(root, "../comment.md", tracker.Hash(body)); err == nil {
		t.Fatal("traversal accepted")
	}
}

func TestGitHubIssueNumberMappingRequiresExactSignedScope(t *testing.T) {
	source := tracker.Source{Version: tracker.Version, Provider: "github", Origin: "https://api.github.com", NativeTenantID: "tenant", ContainerID: "repo", ContainerKey: "owner/repo", PrincipalPseudonym: "principal", MappingVersion: "map-1", APIVersion: "2022-11-28", Auth: tracker.Auth{Mode: "gh-session"}, Scope: tracker.Scope{ItemIDs: []string{"issue:7"}}}
	got, err := githubIssueNumberMapping(source, "node-id-42", "7", "node-id-42")
	if err != nil || got["node-id-42"] != "7" {
		t.Fatalf("mapping = %#v, %v", got, err)
	}
	for _, tc := range []struct{ id, number, operationID string }{{"node-id-41", "7", "node-id-42"}, {"node-id-42", "8", "node-id-42"}, {"node-id-42", "0", "node-id-42"}, {"node-id-42", "007", "node-id-42"}} {
		if _, err := githubIssueNumberMapping(source, tc.id, tc.number, tc.operationID); err == nil {
			t.Fatalf("accepted wrong mapping: %+v", tc)
		}
	}
}

func TestLoadGitHubCommentRejectsSymlinkAndEvidenceSinkIsCreateOnly(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	body := []byte("comment")
	if err := os.WriteFile(filepath.Join(outside, "comment.md"), body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := loadGitHubComment(root, "linked/comment.md", tracker.Hash(body)); err == nil {
		t.Fatal("symlink path accepted")
	}

	sink := githubWriteEvidenceSink(root)
	ref, err := sink("dispatch", []byte(`{"id":1}`), "2026-10-24T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sink("dispatch", []byte(`{"id":1}`), "2026-10-24T00:00:00Z"); err == nil {
		t.Fatal("evidence overwrite accepted")
	}
	assetPath := filepath.Join(root, filepath.FromSlash(ref.Asset.Path))
	stored, err := os.ReadFile(assetPath)
	if err != nil || string(stored) != `{"id":1}` {
		t.Fatalf("stored evidence changed: %q, %v", stored, err)
	}
}

func TestConfiguredBootstrapGitHubWriterPostsAndReadsBack(t *testing.T) {
	assetRoot, evidenceRoot := t.TempDir(), t.TempDir()
	comment := []byte("approved fixture comment")
	if err := os.WriteFile(filepath.Join(assetRoot, "comment.md"), comment, 0600); err != nil {
		t.Fatal(err)
	}
	source := tracker.Source{Version: tracker.Version, Provider: "github", Origin: "https://api.github.com", NativeTenantID: "tenant", ContainerID: "repo", ContainerKey: "owner/repo", PrincipalPseudonym: "principal", MappingVersion: "map-1", APIVersion: "2022-11-28", Auth: tracker.Auth{Mode: "gh-session"}, Scope: tracker.Scope{ItemIDs: []string{"issue:7"}}}
	id := contracts.RemoteIdentity{Provider: "github", TenantID: tracker.TenantScopeID(source), ContainerID: source.ContainerID, RemoteID: "node-id-42", ItemKind: "issue"}
	asset := contracts.AssetRef{Path: "comment.md", SHA256: tracker.Hash(comment)}
	bind, capRef := contracts.Reference{Kind: "WorkItemBinding", ID: "binding", Version: "1.0.0"}, contracts.Reference{Kind: "ConnectorCapability", ID: "cap", Version: "1.0.0"}
	op := contracts.SyncOperation{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "SyncOperation", Metadata: contracts.OperationalMetadata{ID: "op-doc", Name: "comment", Version: "1.0.0", Description: "fixture"}}, WorkItemBindingRef: bind, ConnectorCapabilityRef: capRef, OperationID: "github-fixture-op", OperationKind: "comment", ExpectedRevision: "r1", Change: contracts.SyncChange{Comment: &asset}}
	digest, err := contracts.OperationDigest(bind, id, capRef, "comment", op.ExpectedRevision, op.Change)
	if err != nil {
		t.Fatal(err)
	}
	op.OperationDigest = digest
	op.Authority = contracts.ExternalAuthorityRef{Reference: "grant-fixture", Scope: id, Operation: "comment", OperationDigest: digest}
	capability := contracts.ConnectorCapability{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "ConnectorCapability", Metadata: contracts.OperationalMetadata{ID: "cap", Name: "fixture", Version: "1.0.0", Description: "fixture"}}, Provider: "github", AdapterID: "tracker-github", AdapterVersion: "1.0.0", Scope: id, Operations: []contracts.OperationCapability{{Operation: "comment", Supported: true, Concurrency: "none"}}}
	if _, ds := connector.ValidateOperation(op, capability, id); len(ds) != 0 {
		t.Fatalf("operation invalid: %+v", ds)
	}

	policy := githubPolicyFixture(source)
	source.ReadPolicySHA256 = tracker.ReadPolicyHash(policy)
	g := &GitHubWriteRecords{ImmutableIssueID: "node-id-42", IssueNumber: "7", CommentAssetsRoot: assetRoot, EvidenceRoot: evidenceRoot, Collector: github.Config{Source: source, Policy: policy}, Credential: TokenMetadata{AuthReference: "auth", PrincipalPseudonym: "principal", GrantRef: "grant-fixture", Scopes: []string{"issues:write", "metadata:read"}}, Operation: op, Identity: id, Capability: contracts.ConnectorCapability{Scope: id}}
	var requests []*http.Request
	var checked []github.Authorization
	wantedBody := string(comment) + "\n\n<!-- aok-op:github-fixture-op -->"
	rt := githubWriteRoundTrip(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Clone(req.Context()))
		if req.Method == http.MethodPost {
			body, _ := io.ReadAll(req.Body)
			var payload map[string]string
			if json.Unmarshal(body, &payload) != nil || payload["body"] != wantedBody {
				t.Errorf("unexpected POST body: %s", body)
			}
			return githubHTTPResponse(201, `{"id":123,"body":"created"}`), nil
		}
		return githubHTTPResponse(200, `[{"id":123,"body":"`+strings.ReplaceAll(wantedBody, "\n", `\n`)+`","issue_url":"https://api.github.com/repos/owner/repo/issues/7"}]`), nil
	})
	resolver := github.TokenResolverFunc(func(context.Context, string) (github.Token, error) {
		return github.Token{Value: "fixture-token", PrincipalPseudonym: "principal", GrantRef: "grant-fixture", Scopes: []string{"issues:write", "metadata:read"}, ValidUntil: time.Now().Add(time.Hour)}, nil
	})
	authority := github.AuthorityFunc(func(_ context.Context, a github.Authorization) error { checked = append(checked, a); return nil })
	writer, err := buildGitHubWriterWithTransport(g, resolver, authority, github.WriteAuthorityFunc(func(_ context.Context, a github.Authorization) error { checked = append(checked, a); return nil }), rt)
	if err != nil {
		t.Fatal(err)
	}
	entry, op, capability := dispatchGitHubFixture(t, writer, op, capability, id)
	if entry.State != "response-observed" || len(requests) != 1 {
		t.Fatalf("dispatch entry=%+v requests=%d", entry, len(requests))
	}
	if requests[0].Method != http.MethodPost || requests[0].URL.Path != "/repos/owner/repo/issues/7/comments" || requests[0].Header.Get("Authorization") != "Bearer fixture-token" || requests[0].Header.Get("X-GitHub-Api-Version") != source.APIVersion {
		t.Fatalf("unexpected provider request: %s %s headers=%v", requests[0].Method, requests[0].URL, requests[0].Header)
	}
	var receipt contracts.SyncReceipt
	if err := json.Unmarshal([]byte(entry.Detail), &receipt); err != nil {
		t.Fatal(err)
	}
	result, err := connector.Reconcile(context.Background(), writer, op, capability, id, receipt)
	if err != nil || result.Value.Disposition != "applied" || len(requests) != 2 {
		t.Fatalf("readback result=%+v err=%v requests=%d", result, err, len(requests))
	}
	if requests[1].Method != http.MethodGet || requests[1].URL.Path != "/repos/owner/repo/issues/7/comments" {
		t.Fatalf("unexpected readback request %s %s", requests[1].Method, requests[1].URL)
	}
	if len(checked) != 4 || checked[0].Path != "/repos/owner/repo" || checked[0].Scopes[0] != "metadata:read" || checked[1].Path != "/repos/owner/repo/issues/7/comments" || !sameStrings(checked[1].Scopes, []string{"issues:write", "metadata:read"}) || checked[2].Path != checked[0].Path || checked[3].Path != checked[1].Path {
		t.Fatalf("authority checked a different route or scope: %+v", checked)
	}
	if _, err := os.Stat(filepath.Join(evidenceRoot, filepath.FromSlash(result.Value.ReadbackEvidence[0].Asset.Path))); err != nil {
		t.Fatalf("readback evidence was not persisted: %v", err)
	}
}

func TestQualificationGitHubWriterUsesSameInstalledPOSTAndCompleteReadback(t *testing.T) {
	assetRoot, evidenceRoot := t.TempDir(), t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	parentID, operationID := "qual-parent-1", "qual-operation-1"
	comment := []byte(qualificationGitHubCommentPrefix + parentID + "/" + operationID + qualificationGitHubCommentClaim)
	if err := os.WriteFile(filepath.Join(assetRoot, "comment.md"), comment, 0600); err != nil {
		t.Fatal(err)
	}
	source := tracker.Source{Version: tracker.Version, Provider: "github", Origin: "https://api.github.com", NativeTenantID: "tenant", ContainerID: "repo", ContainerKey: "owner/repo", PrincipalPseudonym: "principal", MappingVersion: "map-1", APIVersion: "2022-11-28", Auth: tracker.Auth{Mode: "gh-session"}, Scope: tracker.Scope{ItemIDs: []string{"issue:7"}}}
	id := contracts.RemoteIdentity{Provider: "github", TenantID: tracker.TenantScopeID(source), ContainerID: "repo", RemoteID: "node-id-42", ItemKind: "issue"}
	policy := githubPolicyFixture(source)
	source.ReadPolicySHA256 = tracker.ReadPolicyHash(policy)
	asset := contracts.AssetRef{Path: "comment.md", SHA256: tracker.Hash(comment)}
	workRef, capRef := contracts.Reference{Kind: "WorkItemBinding", ID: "qual-binding", Version: "1.0.0"}, contracts.Reference{Kind: "ConnectorCapability", ID: "qual-cap", Version: "1.0.0"}
	op := contracts.SyncOperation{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "SyncOperation", Metadata: contracts.OperationalMetadata{ID: "qual-op-doc", Name: "qualification comment", Version: "1.0.0", Description: "synthetic conformance"}}, WorkItemBindingRef: workRef, ConnectorCapabilityRef: capRef, OperationID: operationID, OperationKind: "comment", ExpectedRevision: "r1", Change: contracts.SyncChange{Comment: &asset}}
	digest, err := contracts.OperationDigest(workRef, id, capRef, "comment", op.ExpectedRevision, op.Change)
	if err != nil {
		t.Fatal(err)
	}
	op.OperationDigest = digest
	op.Authority = contracts.ExternalAuthorityRef{Reference: "grant-qual", Scope: id, Operation: "comment", OperationDigest: digest}
	capability := contracts.ConnectorCapability{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "ConnectorCapability", Metadata: contracts.OperationalMetadata{ID: capRef.ID, Name: "qualification", Version: capRef.Version, Description: "synthetic"}}, Provider: "github", AdapterID: "tracker-github", AdapterVersion: "1.0.0", Scope: id, Operations: []contracts.OperationCapability{{Operation: "comment", Supported: true, Concurrency: "none"}}}
	if _, ds := connector.ValidateOperation(op, capability, id); len(ds) != 0 {
		t.Fatalf("operation invalid: %+v", ds)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	grant := writeback.Grant{APIVersion: writeback.APIVersion, Kind: "WriteGrant", ID: "grant-qual", Issuer: "issuer", Principal: "principal", AuthRef: "auth-qual", IssuedAt: now.Format(time.RFC3339Nano), ProviderOrigin: source.Origin, DraftDigest: tracker.Hash([]byte("draft")), SnapshotHash: tracker.Hash([]byte("snapshot")), SnapshotObservedAt: now.Format(time.RFC3339Nano), MappingVersion: "map-1", Scope: id, Operation: "comment", OperationID: operationID, OperationDigest: digest, Capability: capRef, Binding: workRef, ExpectedRevision: op.ExpectedRevision, Nonce: "qual-nonce", ExpiresAt: now.Add(4 * time.Minute).Format(time.RFC3339Nano), RevocationGeneration: 1}
	grantBytes, _ := writeback.GrantSigningBytes(grant)
	grant.Signature = hex.EncodeToString(ed25519.Sign(priv, grantBytes))
	rev := writeback.RevocationSnapshot{APIVersion: writeback.APIVersion, Kind: "RevocationSnapshot", Issuer: "issuer", Generation: 1, IssuedAt: now.Format(time.RFC3339Nano), RevokedGrantIDs: []string{}, RevokedNonces: []string{}}
	revBytes, _ := writeback.RevocationSigningBytes(rev)
	rev.Signature = hex.EncodeToString(ed25519.Sign(priv, revBytes))
	trust := writeback.TrustPolicy{APIVersion: writeback.APIVersion, Kind: "TrustPolicy", Keys: map[string]string{"issuer": hex.EncodeToString(pub)}, MaxGrantTTLSeconds: 300, ProviderOrigins: []string{source.Origin}, ProviderRoutes: []writeback.ProviderRoute{{Provider: "github", Origin: source.Origin}}, Principals: []string{"principal"}, AuthRefs: []string{"auth-qual"}, CompletionRules: []writeback.CompletionRule{}}
	current := writeback.GrantContext{DraftDigest: grant.DraftDigest, SnapshotHash: grant.SnapshotHash, MappingVersion: grant.MappingVersion, ObservedAt: now}
	g := &GitHubWriteRecords{ImmutableIssueID: id.RemoteID, IssueNumber: "7", CommentAssetsRoot: assetRoot, EvidenceRoot: evidenceRoot, Collector: github.Config{Source: source, Policy: policy, AuthReference: "auth-qual"}, Credential: TokenMetadata{AuthReference: "auth-qual", EnvReference: "gh-token", PrincipalPseudonym: "principal", GrantRef: "grant-qual", Scopes: []string{"issues:write", "metadata:read"}, ValidUntil: now.Add(3 * time.Minute).Format(time.RFC3339Nano)}, Operation: op, Capability: capability, Identity: id, Grant: grant, Policy: trust, Revocation: rev, Current: current, Owner: "qualification-owner", OutboxPath: filepath.Join(t.TempDir(), "qual-outbox.sqlite")}
	q := &QualificationRecords{Version: QualificationVersion, Purpose: "synthetic-conformance-only", ParentID: parentID, OperationID: operationID, Action: "github-comment", IssuedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(4 * time.Minute).Format(time.RFC3339Nano), GitHubWrite: g}
	t.Setenv("AOK_FIXTURE_GH_TOKEN", "fixture-token")
	c := Config{Service: "qualification", SecretEnv: map[string]string{"gh-token": "AOK_FIXTURE_GH_TOKEN"}}
	wantedBody := string(comment) + "\n\n<!-- aok-op:" + operationID + " -->"
	var requests []*http.Request
	rt := githubWriteRoundTrip(func(req *http.Request) (*http.Response, error) {
		requests = append(requests, req.Clone(req.Context()))
		if req.Method == http.MethodPost {
			body, _ := io.ReadAll(req.Body)
			var payload map[string]string
			if json.Unmarshal(body, &payload) != nil || payload["body"] != wantedBody {
				t.Errorf("unexpected qualification POST body: %s", body)
			}
			return nil, errors.New("simulated lost create response")
		}
		return githubHTTPResponse(200, `[{"id":123,"body":"`+strings.ReplaceAll(wantedBody, "\n", `\n`)+`","issue_url":"https://api.github.com/repos/owner/repo/issues/7"}]`), nil
	})
	if err := validateQualificationGitHub(c, q, now); err != nil {
		t.Fatalf("qualification config rejected: %v", err)
	}
	writer, err := buildQualificationGitHubWriterWithTransport(c, q, rt)
	if err != nil {
		t.Fatal(err)
	}
	files, err := runQualificationGitHubWithWriter(context.Background(), c, q, writer)
	if err != nil {
		t.Fatal(err)
	}
	if len(requests) != 2 || requests[0].Method != http.MethodPost || requests[0].URL.Path != "/repos/owner/repo/issues/7/comments" || requests[0].Header.Get("Authorization") != "Bearer fixture-token" || requests[0].Header.Get("X-GitHub-Api-Version") != source.APIVersion || requests[1].Method != http.MethodGet || requests[1].URL.Path != "/repos/owner/repo/issues/7/comments" {
		t.Fatalf("qualification recovery requests=%d: %+v", len(requests), requests)
	}
	if len(files["github-write-observation.json"]) == 0 || !strings.Contains(string(files["github-write-observation.json"]), `"readbackDisposition": "applied"`) {
		t.Fatalf("qualification observation lacks successful readback: %s", files["github-write-observation.json"])
	}
}

func githubHTTPResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func dispatchGitHubFixture(t *testing.T, writer connector.Writer, op contracts.SyncOperation, capability contracts.ConnectorCapability, id contracts.RemoteIdentity) (writeback.Entry, contracts.SyncOperation, contracts.ConnectorCapability) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	grant := writeback.Grant{APIVersion: writeback.APIVersion, Kind: "WriteGrant", ID: "grant-fixture", Issuer: "issuer", Principal: "principal", AuthRef: "auth", IssuedAt: now.Format(time.RFC3339Nano), ProviderOrigin: "https://api.github.com", DraftDigest: tracker.Hash([]byte("draft")), SnapshotHash: tracker.Hash([]byte("snapshot")), SnapshotObservedAt: now.Format(time.RFC3339Nano), MappingVersion: "map-1", Scope: id, Operation: "comment", OperationID: op.OperationID, OperationDigest: op.OperationDigest, Capability: op.ConnectorCapabilityRef, Binding: op.WorkItemBindingRef, ExpectedRevision: op.ExpectedRevision, Nonce: "nonce", ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), RevocationGeneration: 1}
	msg, _ := writeback.GrantSigningBytes(grant)
	grant.Signature = hex.EncodeToString(ed25519.Sign(priv, msg))
	rev := writeback.RevocationSnapshot{APIVersion: writeback.APIVersion, Kind: "RevocationSnapshot", Issuer: "issuer", Generation: 1, IssuedAt: now.Format(time.RFC3339Nano), RevokedGrantIDs: []string{}, RevokedNonces: []string{}}
	rmsg, _ := writeback.RevocationSigningBytes(rev)
	rev.Signature = hex.EncodeToString(ed25519.Sign(priv, rmsg))
	policy := writeback.TrustPolicy{APIVersion: writeback.APIVersion, Kind: "TrustPolicy", Keys: map[string]string{"issuer": hex.EncodeToString(pub)}, MaxGrantTTLSeconds: 300, ProviderOrigins: []string{"https://api.github.com"}, ProviderRoutes: []writeback.ProviderRoute{{Provider: "github", Origin: "https://api.github.com"}}, Principals: []string{"principal"}, AuthRefs: []string{"auth"}}
	current := writeback.GrantContext{DraftDigest: grant.DraftDigest, SnapshotHash: grant.SnapshotHash, MappingVersion: grant.MappingVersion, ObservedAt: now}
	verified, err := writeback.VerifyGrantBound(grant, policy, rev, op, capability, id, current, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	box, err := writeback.Open(filepath.Join(t.TempDir(), "outbox.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = box.Close() })
	if _, err := box.Enqueue(op, verified); err != nil {
		t.Fatal(err)
	}
	entry, err := connector.DispatchOnce(context.Background(), box, writer, op, capability, id, grant, policy, rev, current, nil, "fixture-owner", now)
	if err != nil {
		t.Fatal(err)
	}
	return entry, op, capability
}
