package tracker

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

type fixtureWriteTransport struct {
	dispatch     Response
	read         Response
	err          error
	dispatched   []WriteRequest
	readRequests []WriteRequest
}

func (f *fixtureWriteTransport) Dispatch(_ context.Context, _ Source, r WriteRequest) (Response, error) {
	f.dispatched = append(f.dispatched, r)
	return f.dispatch, f.err
}
func (f *fixtureWriteTransport) Readback(_ context.Context, _ Source, r WriteRequest) (Response, error) {
	f.readRequests = append(f.readRequests, r)
	return f.read, f.err
}

func fixtureEvidenceSink() (EvidenceSink, map[string][]byte) {
	assets := map[string][]byte{}
	sink := func(stage string, b []byte, observed string) (contracts.EvidenceRef, error) {
		path := "fixture-evidence/" + stage + "-" + Hash(b) + ".json"
		assets[path] = append([]byte(nil), b...)
		purpose := "provider-dispatch-response"
		if stage == "readback" {
			purpose = "provider-readback-observation"
		}
		return contracts.EvidenceRef{Asset: contracts.AssetRef{Path: path, SHA256: Hash(b)}, Purpose: purpose, ObservedAt: observed}, nil
	}
	return sink, assets
}

func assertReceiptSchema(t *testing.T, r contracts.SyncReceipt) {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _, err := contracts.Parse(b)
	if err != nil {
		t.Fatalf("invalid SyncReceipt: %v\n%s", err, b)
	}
	resource, ok := parsed.(contracts.Resource)
	if !ok {
		t.Fatal("parsed receipt was not operational resource")
	}
	if _, err := contracts.DecodeTyped(resource); err != nil {
		t.Fatalf("typed receipt decode: %v", err)
	}
}

func authorizedFixture(t *testing.T, s Source, id contracts.RemoteIdentity) (contracts.SyncOperation, contracts.ConnectorCapability, connector.ValidatedOperation, writeback.Grant, writeback.TrustPolicy, writeback.RevocationSnapshot, time.Time) {
	t.Helper()
	asset := contracts.AssetRef{Path: "evidence/comment.txt", SHA256: Hash([]byte("verified evidence"))}
	change := contracts.SyncChange{Comment: &asset}
	bind := contracts.Reference{Kind: "WorkItemBinding", ID: "binding-1", Version: "1.0.0"}
	capRef := contracts.Reference{Kind: "ConnectorCapability", ID: "tracker-cap", Version: "1.0.0"}
	op := contracts.SyncOperation{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "SyncOperation", Metadata: contracts.OperationalMetadata{ID: "operation-doc", Name: "comment", Version: "1.0.0", Description: "fixture"}}, WorkItemBindingRef: bind, ConnectorCapabilityRef: capRef, OperationID: "op-fixture-1", OperationKind: "comment", ExpectedRevision: "r1", Change: change}
	digest, err := contracts.OperationDigest(bind, id, capRef, "comment", op.ExpectedRevision, change)
	if err != nil {
		t.Fatal(err)
	}
	op.OperationDigest = digest
	op.Authority = contracts.ExternalAuthorityRef{Reference: "grant-1", Scope: id, Operation: "comment", OperationDigest: digest}
	capability := contracts.ConnectorCapability{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "ConnectorCapability", Metadata: contracts.OperationalMetadata{ID: capRef.ID, Name: "fixture", Version: capRef.Version, Description: "fixture"}}, Provider: s.Provider, AdapterID: "tracker-" + s.Provider, AdapterVersion: "1.0.0", Scope: id, Operations: []contracts.OperationCapability{{Operation: "comment", Supported: true, Idempotent: false, Concurrency: "none"}}}
	validated, ds := connector.ValidateOperation(op, capability, id)
	if len(ds) > 0 {
		t.Fatalf("operation invalid: %+v", ds)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	grant := writeback.Grant{APIVersion: writeback.APIVersion, Kind: "WriteGrant", ID: "grant-1", Issuer: "test-issuer", Principal: s.PrincipalPseudonym, AuthRef: "auth-fixture", IssuedAt: now.Format(time.RFC3339Nano), ProviderOrigin: s.Origin, DraftDigest: Hash([]byte("draft")), SnapshotHash: Hash([]byte("snapshot")), SnapshotObservedAt: now.Format(time.RFC3339Nano), MappingVersion: s.MappingVersion, Scope: id, Operation: "comment", OperationID: op.OperationID, OperationDigest: digest, Capability: capRef, Binding: bind, ExpectedRevision: op.ExpectedRevision, Nonce: "nonce-1", ExpiresAt: now.Add(time.Minute).Format(time.RFC3339Nano), RevocationGeneration: 1}
	msg, _ := writeback.GrantSigningBytes(grant)
	grant.Signature = hex.EncodeToString(ed25519.Sign(priv, msg))
	rev := writeback.RevocationSnapshot{APIVersion: writeback.APIVersion, Kind: "RevocationSnapshot", Issuer: "test-issuer", Generation: 1, IssuedAt: now.Format(time.RFC3339Nano), RevokedGrantIDs: []string{}, RevokedNonces: []string{}}
	rmsg, _ := writeback.RevocationSigningBytes(rev)
	rev.Signature = hex.EncodeToString(ed25519.Sign(priv, rmsg))
	policy := writeback.TrustPolicy{APIVersion: writeback.APIVersion, Kind: "TrustPolicy", Keys: map[string]string{"test-issuer": hex.EncodeToString(pub)}, MaxGrantTTLSeconds: 300, ProviderOrigins: []string{s.Origin}, ProviderRoutes: []writeback.ProviderRoute{{Provider: s.Provider, Origin: s.Origin}}, Principals: []string{s.PrincipalPseudonym}, AuthRefs: []string{"auth-fixture"}}
	return op, capability, validated, grant, policy, rev, now
}

func dispatchFixture(t *testing.T, w connector.Writer, op contracts.SyncOperation, cap contracts.ConnectorCapability, id contracts.RemoteIdentity, grant writeback.Grant, policy writeback.TrustPolicy, rev writeback.RevocationSnapshot, now time.Time) (writeback.Entry, error) {
	t.Helper()
	box, err := writeback.Open(t.TempDir() + "/outbox.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = box.Close() })
	observed, err := time.Parse(time.RFC3339Nano, grant.SnapshotObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	current := writeback.GrantContext{DraftDigest: grant.DraftDigest, SnapshotHash: grant.SnapshotHash, MappingVersion: grant.MappingVersion, ObservedAt: observed}
	verified, err := writeback.VerifyGrantBound(grant, policy, rev, op, cap, id, current, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = box.Enqueue(op, verified); err != nil {
		t.Fatal(err)
	}
	return connector.DispatchOnce(context.Background(), box, w, op, cap, id, grant, policy, rev, current, nil, "fixture-owner", now)
}

// writerSource builds fixture-only P5 identities and API versions. It asserts
// neither live provider conformance nor production authentication qualification.
func writerSource(provider, remote, kind string) (Source, contracts.RemoteIdentity) {
	s := Source{Version: Version, Provider: provider, NativeTenantID: "tenant-1", ContainerID: "container-1", PrincipalPseudonym: "fixture", MappingVersion: "map-1", Scope: Scope{ItemIDs: []string{remote}}, Auth: Auth{Mode: "bearer", TokenEnv: "TOKEN"}, APIVersion: "v1"}
	id := contracts.RemoteIdentity{Provider: provider, TenantID: TenantScopeID(s), ContainerID: s.ContainerID, RemoteID: remote, ItemKind: kind}
	switch provider {
	case "github":
		s.Origin = "https://api.github.com"
		s.ContainerKey = "owner/repo"
		// Fixture uses a supported P5 version; this records no live conformance.
		s.APIVersion = "2022-11-28"
		s.Auth = Auth{Mode: "gh-session"}
	case "azure-devops":
		s.Origin = "https://dev.azure.com"
		s.ContainerKey = "org/project"
		s.APIVersion = "7.1"
	case "linear":
		s.Origin = "https://api.linear.app"
		s.ContainerKey = "workspace/team"
		s.APIVersion = "graphql"
	case "jira":
		s.Origin = "https://example.atlassian.net"
		s.ContainerKey = "PROJ"
		s.APIVersion = "3"
	}
	id.TenantID = TenantScopeID(s)
	return s, id
}

func commentOperation(id contracts.RemoteIdentity) contracts.SyncOperation {
	asset := contracts.AssetRef{Path: "evidence/comment.txt", SHA256: Hash([]byte("verified evidence"))}
	return contracts.SyncOperation{OperationID: "op-fixture-1", OperationKind: "comment", ExpectedRevision: "r1", Authority: contracts.ExternalAuthorityRef{Scope: id}, Change: contracts.SyncChange{Comment: &asset}}
}

func TestTypedProviderCommentRequests(t *testing.T) {
	cases := []struct {
		provider, item, kind string
		wantPath             string
	}{
		{"github", "node-id-7", "issue", "/repos/owner/repo/issues/7/comments"},
		{"linear", "550e8400-e29b-41d4-a716-446655440000", "issue", "/graphql"},
		{"jira", "10007", "issue", "/rest/api/3/issue/10007/comment"},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			s, identity := writerSource(tc.provider, tc.item, tc.kind)
			if tc.provider == "github" {
				s.Scope.ItemIDs = []string{"issue:7"}
			}
			w := NewProviderWriter(WriterConfig{Source: s, GitHubIssueNumbers: map[string]string{tc.item: "7"}, CommentLoader: func(path, sha string) ([]byte, error) {
				if path != "evidence/comment.txt" {
					t.Fatalf("unexpected asset path %q", path)
				}
				return []byte("verified evidence"), nil
			}})
			r, err := w.requestFor(commentOperation(identity))
			if err != nil {
				t.Fatal(err)
			}
			if r.Method != "POST" || r.Path != tc.wantPath {
				t.Fatalf("request %s %s", r.Method, r.Path)
			}
			if !strings.Contains(string(r.Body), "aok-op:op-fixture-1") {
				t.Fatalf("stable marker missing: %s", r.Body)
			}
			if tc.provider == "jira" {
				var b struct {
					Body struct {
						Type    string `json:"type"`
						Version int    `json:"version"`
					} `json:"body"`
				}
				if json.Unmarshal(r.Body, &b) != nil || b.Body.Type != "doc" || b.Body.Version != 1 {
					t.Fatalf("Jira comment is not ADF: %s", r.Body)
				}
			}
		})
	}
}

func TestTypedAzureRevisionConditionalPatch(t *testing.T) {
	s, id := writerSource("azure-devops", "42", "task")
	op := contracts.SyncOperation{OperationID: "op-azure-1", OperationKind: "update-fields", ExpectedRevision: "9", Authority: contracts.ExternalAuthorityRef{Scope: id}, Change: contracts.SyncChange{Fields: []contracts.FieldChange{{ProviderFieldID: "System.Title", Value: json.RawMessage(`"new title"`)}}}}
	r, err := (ProviderWriter{Source: s}).requestFor(op)
	if err != nil {
		t.Fatal(err)
	}
	if r.Method != "PATCH" || r.ContentType != "application/json-patch+json" {
		t.Fatalf("unexpected Azure request: %+v", r)
	}
	var patch []struct {
		Op, Path string
		Value    json.RawMessage
	}
	if json.Unmarshal(r.Body, &patch) != nil || len(patch) != 2 || patch[0].Op != "test" || patch[0].Path != "/rev" || string(patch[0].Value) != "9" {
		t.Fatalf("revision test absent: %s", r.Body)
	}
	if !strings.Contains(r.Path, "wit/workitems/42") || !strings.Contains(r.Path, "api-version=7.1") {
		t.Fatalf("unscoped Azure endpoint: %s", r.Path)
	}
}

func TestWriterRejectsUnsafeOrUnboundInputs(t *testing.T) {
	s, id := writerSource("github", "node-id-1", "issue")
	s.Scope.ItemIDs = []string{"issue:other-item"}
	w := ProviderWriter{Source: s, GitHubIssueNumbers: map[string]string{"node-id-1": "999"}, LoadComment: func(string, string) ([]byte, error) { return []byte("verified evidence"), nil }}
	if _, err := w.requestFor(commentOperation(id)); err == nil {
		t.Fatal("unscoped GitHub locator accepted")
	}
	s.Scope.ItemIDs = []string{"issue:7"}
	w.Source = s
	w.GitHubIssueNumbers["node-id-1"] = "7"
	bad := commentOperation(id)
	bad.Change.Comment.Path = "../../secret"
	if _, err := w.requestFor(bad); err == nil {
		t.Fatal("unsafe comment path accepted")
	}
	bad = commentOperation(id)
	bad.Change.Comment.SHA256 = strings.Repeat("0", 64)
	if _, err := w.requestFor(bad); err == nil {
		t.Fatal("hash mismatch accepted")
	}
	// Typed builders cannot be turned into absolute or alternate-host URLs.
	r, err := w.requestFor(commentOperation(id))
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(r.Path)
	if err != nil || u.IsAbs() || u.Host != "" {
		t.Fatalf("non-relative endpoint: %q", r.Path)
	}
}

func TestFixtureApplyOutcomesAndScopeDenial(t *testing.T) {
	s, id := writerSource("github", "node-id-7", "issue")
	s.Scope.ItemIDs = []string{"issue:7"}
	for _, tc := range []struct {
		name      string
		status    int
		wantState string
	}{
		{"created", 201, "response-observed"}, {"authn-denied", 401, "rejected"}, {"authz-denied", 403, "rejected"}, {"precondition-conflict", 412, "conflict"}, {"rate-limited", 429, "unknown-awaiting-reconciliation"}, {"ambiguous-server", 503, "unknown-awaiting-reconciliation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			op, cap, _, grant, policy, rev, now := authorizedFixture(t, s, id)
			fake := &fixtureWriteTransport{dispatch: Response{Status: tc.status}}
			sink, _ := fixtureEvidenceSink()
			w := NewProviderWriter(WriterConfig{Source: s, Transport: fake, GitHubIssueNumbers: map[string]string{"node-id-7": "7"}, CommentLoader: func(string, string) ([]byte, error) { return []byte("verified evidence"), nil }, AuthReference: "auth-fixture", EvidenceSink: sink})
			got, err := dispatchFixture(t, w, op, cap, id, grant, policy, rev, now)
			if tc.status == 503 {
				if err != nil {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if got.State != tc.wantState {
				t.Fatalf("dispatch state for HTTP %d: got %s want %s", tc.status, got.State, tc.wantState)
			}
			if len(fake.dispatched) != 1 {
				t.Fatalf("expected one dispatch, got %d", len(fake.dispatched))
			}
		})
	}
	for _, field := range []string{"auth-reference", "origin", "principal"} {
		t.Run("wrong-"+field, func(t *testing.T) {
			op, cap, _, grant, policy, rev, now := authorizedFixture(t, s, id)
			source := s
			authRef := "auth-fixture"
			switch field {
			case "auth-reference":
				authRef = "different-auth"
			case "origin":
				source.Origin = "https://attacker.invalid"
			case "principal":
				source.PrincipalPseudonym = "other-principal"
			}
			fake := &fixtureWriteTransport{dispatch: Response{Status: 201}}
			w := NewProviderWriter(WriterConfig{Source: source, Transport: fake, GitHubIssueNumbers: map[string]string{"node-id-7": "7"}, CommentLoader: func(string, string) ([]byte, error) { return []byte("verified evidence"), nil }, AuthReference: authRef})
			got, err := dispatchFixture(t, w, op, cap, id, grant, policy, rev, now)
			if err != nil {
				t.Fatal(err)
			}
			if got.State != "rejected" || len(fake.dispatched) != 0 {
				t.Fatalf("scope mismatch dispatched: %+v", got)
			}
		})
	}
	for _, transportErr := range []error{context.DeadlineExceeded, context.Canceled, errors.New("response lost with secret-canary")} {
		t.Run("transport-ambiguous-"+transportErr.Error(), func(t *testing.T) {
			op, cap, _, grant, policy, rev, now := authorizedFixture(t, s, id)
			fake := &fixtureWriteTransport{err: transportErr}
			w := NewProviderWriter(WriterConfig{Source: s, Transport: fake, GitHubIssueNumbers: map[string]string{"node-id-7": "7"}, CommentLoader: func(string, string) ([]byte, error) { return []byte("verified evidence"), nil }, AuthReference: "auth-fixture"})
			got, err := dispatchFixture(t, w, op, cap, id, grant, policy, rev, now)
			if err == nil || got.State != "unknown-awaiting-reconciliation" || strings.Contains(err.Error(), "secret-canary") {
				t.Fatalf("lost/cancelled response was not safely unknown: %+v %v", got, err)
			}
		})
	}
}

func TestReadbackRequiresCompleteCorrelatedObservation(t *testing.T) {
	s, id := writerSource("linear", "550e8400-e29b-41d4-a716-446655440000", "issue")
	op, cap, validated, _, _, _, _ := authorizedFixture(t, s, id)
	_ = cap
	prior := receiptFor(op).Value
	prior.Disposition = "unknown"
	partial := &fixtureWriteTransport{read: Response{Status: 200, Body: []byte(`{"data":{"issue":{"id":"550e8400-e29b-41d4-a716-446655440000","comments":{"nodes":[{"body":"<!-- aok-op:op-fixture-1 -->"}],"pageInfo":{"hasNextPage":true}}}}}`)}}
	sink, assets := fixtureEvidenceSink()
	w := NewProviderWriter(WriterConfig{Source: s, Transport: partial, CommentLoader: func(string, string) ([]byte, error) { return []byte("verified evidence"), nil }, EvidenceSink: sink})
	got, err := w.Reconcile(context.Background(), validated, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != connector.Unknown || got.Value.Disposition != "unknown" || len(partial.dispatched) != 0 || len(partial.readRequests) != 1 {
		t.Fatalf("partial readback result: %+v", got)
	}
	complete := &fixtureWriteTransport{read: Response{Status: 200, Body: []byte(`{"data":{"issue":{"id":"550e8400-e29b-41d4-a716-446655440000","comments":{"nodes":[{"body":"verified evidence\n\n<!-- aok-op:op-fixture-1 -->"}],"pageInfo":{"hasNextPage":false}}}}}`)}}
	w.Transport = complete
	got, err = w.Reconcile(context.Background(), validated, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != connector.Supported || got.Value.Disposition != "applied" {
		t.Fatalf("complete marker readback not accepted: %+v", got)
	}
	assertReceiptSchema(t, got.Value)
	if len(got.Value.ReadbackEvidence) != 1 {
		t.Fatalf("correlated readback evidence missing: %+v", got.Value.ReadbackEvidence)
	}
	ref := got.Value.ReadbackEvidence[0]
	captured := assets[ref.Asset.Path]
	if ref.Asset.SHA256 != Hash(captured) || len(captured) == 0 || ref.ObservedAt != got.Value.ObservedAt {
		t.Fatalf("captured evidence does not match receipt: %+v", ref)
	}
	prior.OperationDigest = "changed"
	got, err = w.Reconcile(context.Background(), validated, prior)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != connector.Conflict || len(complete.readRequests) != 1 {
		t.Fatalf("mismatched receipt reached readback: %+v", got)
	}
}

func TestStructuredCommentReadbackCorrelation(t *testing.T) {
	cases := []struct {
		provider, remote, body string
		scope                  []string
		response               string
	}{
		{"github", "node-id-7", "verified evidence\n\n<!-- aok-op:op-fixture-1 -->", []string{"issue:7"}, `[{"id":91,"issue_url":"https://api.github.com/repos/owner/repo/issues/7","body":"verified evidence\n\n<!-- aok-op:op-fixture-1 -->"}]`},
		{"linear", "550e8400-e29b-41d4-a716-446655440000", "verified evidence\n\n<!-- aok-op:op-fixture-1 -->", nil, `{"data":{"issue":{"id":"550e8400-e29b-41d4-a716-446655440000","comments":{"nodes":[{"body":"verified evidence\n\n<!-- aok-op:op-fixture-1 -->"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}},"errors":[]}`},
		{"jira", "10007", "verified evidence\n\n<!-- aok-op:op-fixture-1 -->", nil, `{"startAt":0,"maxResults":50,"total":1,"comments":[{"id":"20001","self":"https://example.atlassian.net/rest/api/3/issue/10007/comment/20001","body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"verified evidence\n\n<!-- aok-op:op-fixture-1 -->"}]}]}}]}`},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			s, id := writerSource(tc.provider, tc.remote, "issue")
			if tc.scope != nil {
				s.Scope.ItemIDs = tc.scope
			}
			w := NewProviderWriter(WriterConfig{Source: s, GitHubIssueNumbers: map[string]string{tc.remote: "7"}, CommentLoader: func(string, string) ([]byte, error) { return []byte("verified evidence"), nil }})
			op := commentOperation(id)
			resp := Response{Status: 200, Body: []byte(tc.response)}
			if !completeReadback(tc.provider, resp) || !w.readbackMatches(op, resp) {
				t.Fatalf("valid structured response rejected: %s", resp.Body)
			}
			bad := strings.Replace(tc.response, "verified evidence", "different text", 1)
			if w.readbackMatches(op, Response{Status: 200, Body: []byte(bad)}) {
				t.Fatal("wrong comment body accepted")
			}
			if tc.provider == "github" {
				bad = strings.Replace(tc.response, "/issues/7", "/issues/8", 1)
			} else if tc.provider == "linear" {
				bad = strings.Replace(tc.response, tc.remote, "550e8400-e29b-41d4-a716-446655440001", 1)
			} else {
				bad = strings.Replace(tc.response, "/issue/10007/", "/issue/10008/", 1)
			}
			if w.readbackMatches(op, Response{Status: 200, Body: []byte(bad)}) {
				t.Fatal("wrong native item accepted")
			}
			dup := strings.Replace(tc.response, marker(op), marker(op)+marker(op), 1)
			if w.readbackMatches(op, Response{Status: 200, Body: []byte(dup)}) {
				t.Fatal("duplicate operation marker accepted")
			}
		})
	}
}

func TestLinearReadbackRejectsMissingPageInfoAndGraphQLErrors(t *testing.T) {
	for _, raw := range []string{
		`{"data":{"issue":{"id":"550e8400-e29b-41d4-a716-446655440000","comments":{"nodes":[]}}}}`,
		`{"data":{"issue":{"id":"550e8400-e29b-41d4-a716-446655440000","comments":{"nodes":[],"pageInfo":{"hasNextPage":false}}}},"errors":[{"message":"partial"}]}`,
	} {
		if completeReadback("linear", Response{Status: 200, Body: []byte(raw)}) {
			t.Fatalf("incomplete or GraphQL-error response accepted: %s", raw)
		}
	}
}

func TestJiraCommentSelfMustMatchExactConfiguredOriginAndIssue(t *testing.T) {
	s, _ := writerSource("jira", "10007", "issue")
	w := ProviderWriter{Source: s}
	if !w.jiraCommentSelfMatches("https://example.atlassian.net/rest/api/3/issue/10007/comment/20001", "10007") {
		t.Fatal("documented Jira Cloud comment self URL rejected")
	}
	for _, raw := range []string{
		"https://attacker.invalid/rest/api/3/issue/10007/comment/20001",
		"https://example.atlassian.net/rest/api/3/issue/10008/comment/20001",
		"https://example.atlassian.net/rest/api/3/issue/10007/comment/20001?redirect=https://attacker.invalid",
		"https://example.atlassian.net:444/rest/api/3/issue/10007/comment/20001",
	} {
		if w.jiraCommentSelfMatches(raw, "10007") {
			t.Fatalf("unbound Jira self URL accepted: %s", raw)
		}
	}
	s.Origin = "https://api.atlassian.com"
	w.Source = s
	if !w.jiraCommentSelfMatches("https://api.atlassian.com/ex/jira/tenant-1/rest/api/3/issue/10007/comment/20001", "10007") {
		t.Fatal("documented Jira API gateway URL rejected")
	}
	if w.jiraCommentSelfMatches("https://api.atlassian.com/ex/jira/other-tenant/rest/api/3/issue/10007/comment/20001", "10007") {
		t.Fatal("wrong Jira tenant accepted")
	}
}

func TestAzureReadbackRequiresTypedExactEffectAndNativeIdentity(t *testing.T) {
	s, id := writerSource("azure-devops", "42", "task")
	op := contracts.SyncOperation{OperationID: "op-azure-1", OperationKind: "update-fields", ExpectedRevision: "9", Authority: contracts.ExternalAuthorityRef{Scope: id}, Change: contracts.SyncChange{Fields: []contracts.FieldChange{{ProviderFieldID: "System.Title", Value: json.RawMessage(`"new title"`)}}}}
	w := ProviderWriter{Source: s}
	good := Response{Status: 200, Body: []byte(`{"id":42,"rev":10,"fields":{"System.Title":"new title"}}`)}
	if !w.readbackMatches(op, good) {
		t.Fatal("exact Azure effect not observed")
	}
	for _, raw := range []string{
		`{"id":43,"rev":10,"fields":{"System.Title":"new title"}}`,
		`{"id":42,"rev":9,"fields":{"System.Title":"new title"}}`,
		`{"id":42,"rev":10,"fields":{"System.Title":1}}`,
	} {
		if w.readbackMatches(op, Response{Status: 200, Body: []byte(raw)}) {
			t.Fatalf("invalid Azure observation accepted: %s", raw)
		}
	}
	bind := contracts.Reference{Kind: "WorkItemBinding", ID: "binding-1", Version: "1.0.0"}
	capRef := contracts.Reference{Kind: "ConnectorCapability", ID: "tracker-cap", Version: "1.0.0"}
	op.WorkItemBindingRef = bind
	op.ConnectorCapabilityRef = capRef
	op.DocumentHeader = contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "SyncOperation", Metadata: contracts.OperationalMetadata{ID: "operation-doc", Name: "azure-update", Version: "1.0.0", Description: "fixture"}}
	digest, err := contracts.OperationDigest(bind, id, capRef, "update-fields", op.ExpectedRevision, op.Change)
	if err != nil {
		t.Fatal(err)
	}
	op.OperationDigest = digest
	op.Authority = contracts.ExternalAuthorityRef{Reference: "grant-fixture", Scope: id, Operation: "update-fields", OperationDigest: digest}
	capability := contracts.ConnectorCapability{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "ConnectorCapability", Metadata: contracts.OperationalMetadata{ID: capRef.ID, Name: "fixture", Version: capRef.Version, Description: "fixture"}}, Provider: s.Provider, AdapterID: "tracker-azure-devops", AdapterVersion: "1.0.0", Scope: id, Operations: []contracts.OperationCapability{{Operation: "update-fields", Supported: true, ConditionalWrite: true, Concurrency: "conditional"}}}
	validated, diagnostics := connector.ValidateOperation(op, capability, id)
	if len(diagnostics) > 0 {
		t.Fatalf("fixture operation invalid: %+v", diagnostics)
	}
	sink, _ := fixtureEvidenceSink()
	transport := &fixtureWriteTransport{read: good}
	writer := NewProviderWriter(WriterConfig{Source: s, Transport: transport, EvidenceSink: sink})
	got, err := writer.Reconcile(context.Background(), validated, receiptFor(op).Value)
	if err != nil {
		t.Fatal(err)
	}
	if got.Disposition != connector.Supported || got.Value.Disposition != "applied" {
		t.Fatalf("Azure observed update not reconciled: %+v", got)
	}
	assertReceiptSchema(t, got.Value)
}

func TestWriteStatusDiagnosticsAndReceiptSchema(t *testing.T) {
	s, id := writerSource("github", "node-id-7", "issue")
	op, _, _, _, _, _, _ := authorizedFixture(t, s, id)
	for _, tc := range []struct {
		status      int
		header      map[string]string
		disposition connector.Disposition
		code        string
		hint        string
	}{
		{401, nil, connector.Unsupported, "tracker.write.authentication", ""},
		{403, nil, connector.Unsupported, "tracker.write.permission", ""},
		{429, map[string]string{"Retry-After": "12"}, connector.Unknown, "tracker.write.rate_limited", "12 seconds"},
		{429, map[string]string{"retry-after": "999999"}, connector.Unknown, "tracker.write.rate_limited", "do not automatically resend"},
	} {
		result := classifyWriteResponse(receiptFor(op), Response{Status: tc.status, Headers: tc.header})
		if result.Disposition != tc.disposition || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != tc.code || (tc.hint != "" && !strings.Contains(result.Diagnostics[0].Message, tc.hint)) {
			t.Fatalf("status %d classification: %+v", tc.status, result)
		}
		assertReceiptSchema(t, result.Value)
	}
	assertReceiptSchema(t, receiptFor(op).Value)
}

func TestUnsafeCommentPathNeverInvokesLoader(t *testing.T) {
	source, id := writerSource("github", "native-7", "issue")
	op := commentOperation(id)
	op.Change.Comment = &contracts.AssetRef{Path: "../../private-credential", SHA256: Hash([]byte("credential-canary"))}
	calls := 0
	w := NewProviderWriter(WriterConfig{Source: source, CommentLoader: func(string, string) ([]byte, error) { calls++; return []byte("credential-canary"), nil }})
	if _, err := w.requestFor(op); err == nil {
		t.Fatal("unsafe path accepted")
	}
	if _, err := w.expectedComment(op); err == nil {
		t.Fatal("unsafe readback path accepted")
	}
	if calls != 0 {
		t.Fatal("credential loader called for unsafe path")
	}
}
