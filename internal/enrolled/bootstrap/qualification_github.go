package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	githubcollector "github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/github"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

const qualificationGitHubCommentPrefix = "AOK synthetic qualification "
const qualificationGitHubCommentClaim = "\nNo task acceptance, completion, or production claim."

func validateQualificationGitHub(c Config, q *QualificationRecords, at time.Time) error {
	if c.Service != "qualification" || c.Authority != nil || c.Worker != nil || c.Anchor != nil || c.Verifier != nil || c.Listen != "" || c.TLS.Certificate != "" || c.TLS.PrivateKey != "" || q == nil || q.Version != QualificationVersion || q.Purpose != "synthetic-conformance-only" || q.ParentID == "" || q.OperationID == "" || q.Action != "github-comment" || q.GitHubWrite == nil || q.GitHubRead != nil || q.CodexQuota != nil || q.Host != nil {
		return ErrBlocked
	}
	issued, issuedErr := time.Parse(time.RFC3339Nano, q.IssuedAt)
	end, endErr := qualificationEnd(*q)
	if issuedErr != nil || endErr != nil || issued.After(at) || !at.Before(end) || !end.After(issued) || end.Sub(issued) > 5*time.Minute {
		return ErrBlocked
	}
	g := q.GitHubWrite
	if g.Operation.OperationID != q.OperationID || g.Operation.OperationKind != "comment" || g.Operation.Change.Comment == nil || len(g.Grant.EvidenceDependencies) != 0 || g.Owner == "" || g.OutboxPath == "" || g.Credential.AuthReference == "" || c.SecretEnv[g.Credential.EnvReference] == "" || !sameStrings(g.Credential.Scopes, []string{"issues:write", "metadata:read"}) {
		return ErrBlocked
	}
	if tracker.ValidateSource(g.Collector.Source) != nil || g.Collector.Source.Provider != "github" || g.Collector.Source.Origin != "https://api.github.com" || g.Collector.Source.Auth.Mode != "gh-session" || g.Collector.Policy == nil || tracker.ReadPolicyHash(g.Collector.Policy) == "" || g.Collector.Source.ReadPolicySHA256 != tracker.ReadPolicyHash(g.Collector.Policy) {
		return ErrBlocked
	}
	if g.Collector.Policy.Version != g.Collector.Source.Version || len(g.Collector.Policy.Rules) != 1 || len(g.Collector.Source.Scope.ItemIDs) != 1 {
		return ErrBlocked
	}
	rule := g.Collector.Policy.Rules[0]
	if rule.Provider != "github" || rule.Origin != g.Collector.Source.Origin || rule.NativeTenantID != g.Collector.Source.NativeTenantID || rule.ContainerID != g.Collector.Source.ContainerID || rule.ContainerKey != g.Collector.Source.ContainerKey || rule.Auth != g.Collector.Source.Auth || rule.PrincipalPseudonym != g.Collector.Source.PrincipalPseudonym || rule.Deployment != nil {
		return ErrBlocked
	}
	if _, err := githubIssueNumberMapping(g.Collector.Source, g.ImmutableIssueID, g.IssueNumber, g.Operation.Authority.Scope.RemoteID); err != nil || g.Identity.RemoteID != g.ImmutableIssueID || g.Identity.ItemKind != "issue" || usage.Digest(g.Identity) != usage.Digest(g.Operation.Authority.Scope) || usage.Digest(g.Identity) != usage.Digest(g.Capability.Scope) {
		return ErrBlocked
	}
	asset := g.Operation.Change.Comment
	want := qualificationGitHubCommentPrefix + q.ParentID + "/" + q.OperationID + qualificationGitHubCommentClaim
	if asset.SHA256 != tracker.Hash([]byte(want)) {
		return errors.New("synthetic GitHub comment body digest is not the exact fixed qualification body")
	}
	body, err := loadGitHubComment(g.CommentAssetsRoot, asset.Path, asset.SHA256)
	if err != nil || string(body) != want {
		return errors.New("synthetic GitHub comment asset does not match its fixed body")
	}
	if requireRealDirectory(g.EvidenceRoot) != nil {
		return ErrBlocked
	}
	until, err := time.Parse(time.RFC3339Nano, g.Credential.ValidUntil)
	if err != nil || endErr != nil || !at.Before(until) || g.Credential.PrincipalPseudonym != g.Collector.Source.PrincipalPseudonym || g.Credential.GrantRef == "" || g.Credential.AuthReference != g.Collector.AuthReference {
		return ErrBlocked
	}
	if g.Grant.OperationID != q.OperationID || g.Grant.Operation != "comment" || g.Grant.ProviderOrigin != g.Collector.Source.Origin || g.Grant.Principal != g.Credential.PrincipalPseudonym || g.Grant.AuthRef != g.Credential.AuthReference || g.Grant.Scope.RemoteID != g.ImmutableIssueID || g.Grant.OperationDigest != g.Operation.OperationDigest || len(g.Policy.CompletionRules) != 0 {
		return ErrBlocked
	}
	if _, err := writeback.VerifyGrantBound(g.Grant, g.Policy, g.Revocation, g.Operation, g.Capability, g.Identity, g.Current, nil, at); err != nil {
		return fmt.Errorf("synthetic GitHub operation is not covered by its existing P8 grant: %w", err)
	}
	return nil
}

// buildQualificationGitHubWriterWithTransport reuses the installed typed
// provider construction for fixture testing. Production has no way to set the
// private round tripper from signed configuration.
func buildQualificationGitHubWriterWithTransport(c Config, q *QualificationRecords, roundTripper http.RoundTripper) (connector.Writer, error) {
	if err := validateQualificationGitHub(c, q, time.Now().UTC()); err != nil {
		return nil, err
	}
	g := q.GitHubWrite
	resolver := githubcollector.TokenResolverFunc(func(ctx context.Context, ref string) (githubcollector.Token, error) {
		if ctx == nil || ref != g.Credential.AuthReference {
			return githubcollector.Token{}, ErrBlocked
		}
		value, err := resolveSecret(c, g.Credential.EnvReference)
		until, parseErr := time.Parse(time.RFC3339Nano, g.Credential.ValidUntil)
		if err != nil || parseErr != nil || !time.Now().Before(until) {
			return githubcollector.Token{}, ErrBlocked
		}
		return githubcollector.Token{Value: value, PrincipalPseudonym: g.Credential.PrincipalPseudonym, GrantRef: g.Credential.GrantRef, Scopes: append([]string(nil), g.Credential.Scopes...), ValidUntil: until}, nil
	})
	read := githubcollector.AuthorityFunc(func(ctx context.Context, a githubcollector.Authorization) error {
		if ctx == nil || ctx.Err() != nil || a.Method != "GET" || a.Path != "/repos/"+g.Collector.Source.ContainerKey || a.Origin != g.Collector.Source.Origin || a.Principal != g.Credential.PrincipalPseudonym || a.GrantRef != g.Credential.GrantRef || !sameStrings(a.Scopes, []string{"metadata:read"}) || usage.Digest(a.ItemIDs) != usage.Digest(g.Collector.Source.Scope.ItemIDs) || a.SourceSHA256 != tracker.ScopeDigest(g.Collector.Source) || a.PolicySHA256 != tracker.ReadPolicyHash(g.Collector.Policy) {
			return ErrBlocked
		}
		return nil
	})
	write := githubcollector.WriteAuthorityFunc(func(ctx context.Context, a githubcollector.Authorization) error {
		if validateQualificationGitHub(c, q, time.Now().UTC()) != nil {
			return ErrBlocked
		}
		wantPath := "/repos/" + g.Collector.Source.ContainerKey + "/issues/" + g.IssueNumber + "/comments"
		if ctx == nil || ctx.Err() != nil || a.Method != "POST" || a.Path != wantPath || a.Origin != g.Collector.Source.Origin || a.Principal != g.Credential.PrincipalPseudonym || a.GrantRef != g.Credential.GrantRef || !sameStrings(a.Scopes, []string{"issues:write", "metadata:read"}) || usage.Digest(a.ItemIDs) != usage.Digest(g.Collector.Source.Scope.ItemIDs) || a.SourceSHA256 != tracker.ScopeDigest(g.Collector.Source) || a.PolicySHA256 != tracker.ReadPolicyHash(g.Collector.Policy) {
			return ErrBlocked
		}
		return nil
	})
	return buildGitHubWriterWithTransport(g, resolver, read, write, roundTripper)
}

func runQualificationGitHub(ctx context.Context, c Config, q *QualificationRecords) (map[string][]byte, error) {
	if ctx == nil {
		return nil, ErrBlocked
	}
	if err := validateQualificationGitHub(c, q, time.Now().UTC()); err != nil {
		return nil, err
	}
	writer, err := buildQualificationGitHubWriterWithTransport(c, q, nil)
	if err != nil {
		return nil, err
	}
	return runQualificationGitHubWithWriter(ctx, c, q, writer)
}

func runQualificationGitHubWithWriter(ctx context.Context, c Config, q *QualificationRecords, writer connector.Writer) (map[string][]byte, error) {
	if ctx == nil || writer == nil {
		return nil, ErrBlocked
	}
	if err := validateQualificationGitHub(c, q, time.Now().UTC()); err != nil {
		return nil, err
	}
	g := q.GitHubWrite
	grantEnd, err := time.Parse(time.RFC3339Nano, g.Grant.ExpiresAt)
	if err != nil {
		return nil, ErrBlocked
	}
	qualificationExpiry, _ := qualificationEnd(*q)
	if qualificationExpiry.Before(grantEnd) {
		grantEnd = qualificationExpiry
	}
	ctx, cancel := context.WithDeadline(ctx, grantEnd)
	defer cancel()
	box, err := writeback.Open(g.OutboxPath)
	if err != nil {
		return nil, errors.New("synthetic GitHub durable outbox unavailable")
	}
	defer box.Close()
	now := time.Now().UTC()
	verified, err := writeback.VerifyGrantBound(g.Grant, g.Policy, g.Revocation, g.Operation, g.Capability, g.Identity, g.Current, nil, now)
	if err != nil {
		return nil, errors.New("synthetic GitHub P8 grant is no longer current")
	}
	if _, err = box.Enqueue(g.Operation, verified); err != nil {
		return nil, errors.New("synthetic GitHub operation already exists or could not be admitted; no resend")
	}
	entry, dispatchErr := connector.DispatchOnce(ctx, box, writer, g.Operation, g.Capability, g.Identity, g.Grant, g.Policy, g.Revocation, g.Current, nil, g.Owner, time.Now().UTC())
	var receipt contracts.SyncReceipt
	if entry.Detail == "" || json.Unmarshal([]byte(entry.Detail), &receipt) != nil {
		return nil, errors.New("synthetic GitHub dispatch receipt unavailable; operation remains consumed")
	}
	readback, readErr := connector.Reconcile(ctx, writer, g.Operation, g.Capability, g.Identity, receipt)
	observation := map[string]any{
		"action": "github-comment", "purpose": q.Purpose, "parentId": q.ParentID, "operationId": q.OperationID,
		"operationDigest": g.Operation.OperationDigest, "dispatchState": entry.State, "dispatchDisposition": receipt.Disposition,
		"dispatchError": dispatchErr != nil, "dispatchEvidence": receipt.DispatchEvidence,
		"readbackDisposition": readback.Value.Disposition, "readbackEvidence": readback.Value.ReadbackEvidence,
		"observationOnly": true, "authorizationGranted": false, "executionReady": false,
	}
	b, _ := json.MarshalIndent(observation, "", "  ")
	files := map[string][]byte{"github-write-observation.json": append(b, '\n')}
	if readErr != nil || readback.Value.Disposition != "applied" {
		if dispatchErr != nil {
			return files, fmt.Errorf("GitHub send outcome requires independent readback; no resend: %w", dispatchErr)
		}
		return files, errors.New("GitHub comment readback did not establish the exact single comment")
	}
	return files, nil
}
