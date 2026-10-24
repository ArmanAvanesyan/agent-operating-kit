// Fixture-backed write adapters. These deliberately have no production
// transport: the existing P5 Transport continues to reject mutations.
package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
)

// WriteRequest is produced only by a typed provider builder. Body, path and
// headers are never accepted from the operation or a task-provided URL.
type WriteRequest struct {
	Method      string
	Path        string
	ContentType string
	Body        []byte
	ItemID      string
}

// WriteTransport is intentionally separate from Transport (the P5 read path).
// Implementations must pin the configured origin and reject redirects.
type WriteTransport interface {
	Dispatch(context.Context, Source, WriteRequest) (Response, error)
	Readback(context.Context, Source, WriteRequest) (Response, error)
}

type CommentLoader func(path, sha256 string) ([]byte, error)
type EvidenceSink func(stage string, body []byte, observedAt string) (contracts.EvidenceRef, error)

type ProviderWriter struct {
	Source      Source
	Transport   WriteTransport
	LoadComment CommentLoader
	// GitHubIssueNumbers binds immutable issue database IDs to REST issue
	// numbers. GitHub RemoteIdentity.RemoteID is never interpreted as a number.
	GitHubIssueNumbers map[string]string
	AuthReference      string
	EvidenceSink       EvidenceSink
}

// WriterConfig groups the explicit connector boundary dependencies.
type WriterConfig struct {
	Source             Source
	Transport          WriteTransport
	CommentLoader      CommentLoader
	GitHubIssueNumbers map[string]string
	AuthReference      string
	EvidenceSink       EvidenceSink
}

func NewProviderWriter(c WriterConfig) ProviderWriter {
	return ProviderWriter{Source: c.Source, Transport: c.Transport, LoadComment: c.CommentLoader, GitHubIssueNumbers: c.GitHubIssueNumbers, AuthReference: c.AuthReference, EvidenceSink: c.EvidenceSink}
}

var markerID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func marker(op contracts.SyncOperation) string { return "<!-- aok-op:" + op.OperationID + " -->" }

// requestFor builds one narrowly typed operation. GitHub, Linear and Jira
// support comment operations only; Azure supports one revision-tested field
// patch. Other operation kinds remain unsupported pending provider gates.
func (w ProviderWriter) requestFor(op contracts.SyncOperation) (WriteRequest, error) {
	s := w.Source
	if err := ValidateSource(s); err != nil {
		return WriteRequest{}, err
	}
	if op.OperationID == "" || !markerID.MatchString(op.OperationID) {
		return WriteRequest{}, fmt.Errorf("invalid stable operation id")
	}
	id := op.Authority.Scope
	if id.Provider != s.Provider || id.TenantID != TenantScopeID(s) || id.ContainerID != s.ContainerID || id.RemoteID == "" {
		return WriteRequest{}, fmt.Errorf("operation is outside configured provider scope")
	}
	if op.OperationKind == "comment" {
		if op.Change.Comment == nil || w.LoadComment == nil {
			return WriteRequest{}, fmt.Errorf("comment asset loader required")
		}
		asset := op.Change.Comment
		if err := evidence.CheckPath(asset.Path); err != nil {
			return WriteRequest{}, fmt.Errorf("unsafe comment asset path")
		}
		content, err := w.LoadComment(asset.Path, asset.SHA256)
		if err != nil {
			return WriteRequest{}, fmt.Errorf("comment asset unavailable")
		}
		if len(content) > 1<<20 || Hash(content) != asset.SHA256 || bytes.ContainsAny(content, "\x00") {
			return WriteRequest{}, fmt.Errorf("comment asset hash, size or encoding invalid")
		}
		if strings.ContainsAny(asset.Path, "\\\r\n\x00") || strings.HasPrefix(asset.Path, "/") || strings.Contains(asset.Path, "..") || strings.ContainsAny(string(content), "\x01\x02\x03\x04\x05\x06\x07\x08\x0b\x0c\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f") {
			return WriteRequest{}, fmt.Errorf("comment asset path or text contains prohibited characters")
		}
		if strings.Contains(string(content), "<!-- aok-op:") {
			return WriteRequest{}, fmt.Errorf("comment asset contains a reserved operation marker")
		}
		text := string(content) + "\n\n" + marker(op)
		switch s.Provider {
		case "github":
			// Official API contract: https://docs.github.com/en/rest/issues/comments#create-an-issue-comment
			owner, repo, err := githubRepo(s)
			if err != nil {
				return WriteRequest{}, err
			}
			if id.ItemKind != "issue" && id.ItemKind != "pull-request" {
				return WriteRequest{}, fmt.Errorf("GitHub comment target must be an issue or pull request")
			}
			number := w.GitHubIssueNumbers[id.RemoteID]
			if !positiveNumericID(number) || !hasGitHubIssueScope(s.Scope.ItemIDs, number) {
				return WriteRequest{}, fmt.Errorf("GitHub immutable item ID lacks an explicitly scoped issue-number binding")
			}
			body, _ := json.Marshal(map[string]string{"body": text})
			return WriteRequest{Method: "POST", Path: "/repos/" + owner + "/" + repo + "/issues/" + number + "/comments", ContentType: "application/json", Body: body, ItemID: id.RemoteID}, nil
		case "linear":
			// Official GraphQL endpoint/mutation semantics: https://linear.app/developers/graphql
			if id.ItemKind != "issue" || !safeUUID.MatchString(id.RemoteID) {
				return WriteRequest{}, fmt.Errorf("Linear target must be a native issue UUID")
			}
			if !has(s.Scope.ItemIDs, id.RemoteID) {
				return WriteRequest{}, fmt.Errorf("Linear issue UUID is not explicitly scoped")
			}
			const query = "mutation AOKComment($input: CommentCreateInput!) { commentCreate(input: $input) { success comment { id body issue { id } } } }"
			body, _ := json.Marshal(map[string]any{"query": query, "variables": map[string]any{"input": map[string]string{"issueId": id.RemoteID, "body": text}}})
			return WriteRequest{Method: "POST", Path: "/graphql", ContentType: "application/json", Body: body, ItemID: id.RemoteID}, nil
		case "jira":
			// Official Jira Cloud API v3 comment resource: https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-comments/
			if id.ItemKind != "issue" || !numericJiraID(id.RemoteID) {
				return WriteRequest{}, fmt.Errorf("Jira target must be an immutable numeric issue ID")
			}
			if !has(s.Scope.ItemIDs, id.RemoteID) {
				return WriteRequest{}, fmt.Errorf("Jira issue key is not explicitly scoped")
			}
			path := "/rest/api/3/issue/" + url.PathEscape(id.RemoteID) + "/comment"
			if u, _ := url.Parse(s.Origin); u.Hostname() == "api.atlassian.com" {
				path = "/ex/jira/" + url.PathEscape(s.NativeTenantID) + path
			}
			body, _ := json.Marshal(map[string]any{"body": plainTextADF(text)})
			return WriteRequest{Method: "POST", Path: path, ContentType: "application/json", Body: body, ItemID: id.RemoteID}, nil
		default:
			return WriteRequest{}, fmt.Errorf("comment unsupported for provider")
		}
	}
	if op.OperationKind == "update-fields" && s.Provider == "azure-devops" {
		// Azure's official update example tests /rev in the same JSON Patch request:
		// https://learn.microsoft.com/en-us/rest/api/azure/devops/wit/work-items/update?view=azure-devops-rest-7.1
		if id.ItemKind == "" || !validAzureID(id.RemoteID) {
			return WriteRequest{}, fmt.Errorf("invalid Azure work item identity")
		}
		if !has(s.Scope.ItemIDs, id.RemoteID) {
			return WriteRequest{}, fmt.Errorf("Azure work item ID is not explicitly scoped")
		}
		rev, err := strconv.Atoi(op.ExpectedRevision)
		if err != nil || rev < 1 {
			return WriteRequest{}, fmt.Errorf("Azure revision must be a positive integer")
		}
		if len(op.Change.Fields) != 1 {
			return WriteRequest{}, fmt.Errorf("Azure writer accepts exactly one mapped field")
		}
		field := op.Change.Fields[0]
		if !safeAzureField.MatchString(field.ProviderFieldID) {
			return WriteRequest{}, fmt.Errorf("unsafe Azure field reference")
		}
		var value any
		dec := json.NewDecoder(bytes.NewReader(field.Value))
		dec.UseNumber()
		if err := dec.Decode(&value); err != nil {
			return WriteRequest{}, fmt.Errorf("invalid Azure field value")
		}
		body, _ := json.Marshal([]map[string]any{{"op": "test", "path": "/rev", "value": rev}, {"op": "add", "path": "/fields/" + field.ProviderFieldID, "value": value}})
		org, project, err := azureScope(s)
		if err != nil {
			return WriteRequest{}, err
		}
		return WriteRequest{Method: "PATCH", Path: adoPath(org, project, "wit/workitems/"+id.RemoteID+"?api-version=7.1"), ContentType: "application/json-patch+json", Body: body, ItemID: id.RemoteID}, nil
	}
	return WriteRequest{}, fmt.Errorf("operation is not in the fixture-qualified provider slice")
}

// hasGitHubIssueScope accepts the provider's canonical issue-number scope
// (issue:42) and the legacy bare-number form. Operation identity remains the
// immutable remote ID and is mapped separately to this REST number.
func hasGitHubIssueScope(ids []string, number string) bool {
	return has(ids, "issue:"+number) || has(ids, number)
}

var safeUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
var safeAzureField = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,127}$`)

func positiveNumericID(v string) bool { n, e := strconv.Atoi(v); return e == nil && n > 0 }
func plainTextADF(text string) map[string]any {
	return map[string]any{"type": "doc", "version": 1, "content": []any{map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}}}}
}

func (w ProviderWriter) Apply(ctx context.Context, validated connector.ValidatedOperation, auth connector.AuthorizationContext) (connector.Result[contracts.SyncReceipt], error) {
	op := validated.Operation()
	result := receiptFor(op)
	// Defense in depth for callers that bypass connector.Apply.
	approvedOrigin, approvedPrincipal, approvedAuthRef := connector.AuthorizationScope(auth)
	if approvedOrigin != w.Source.Origin || approvedPrincipal != w.Source.PrincipalPseudonym || w.AuthReference == "" || approvedAuthRef != w.AuthReference || !connector.AuthorizationValid(auth, validated, time.Now()) {
		result.Disposition = connector.Unsupported
		result.Value.Disposition = "rejected"
		result.Diagnostics = []contracts.Diagnostic{{Code: "tracker.write.authorization", Severity: "error", Message: "authorization context is absent, expired or mismatched"}}
		return result, nil
	}
	if w.Transport == nil {
		result.Disposition = connector.Unsupported
		return result, nil
	}
	req, err := w.requestFor(op)
	if err != nil {
		result.Disposition = connector.Unsupported
		result.Diagnostics = []contracts.Diagnostic{{Code: "tracker.write.request", Severity: "error", Message: err.Error()}}
		return result, nil
	}
	limits, err := EffectiveLimits(w.Source)
	if err != nil {
		result.Disposition = connector.Unsupported
		return result, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(limits.RequestSeconds)*time.Second)
	defer cancel()
	resp, err := w.Transport.Dispatch(requestCtx, w.Source, req)
	if err != nil {
		result.Disposition = connector.Unknown
		result.Value.Disposition = "unknown"
		result.Diagnostics = []contracts.Diagnostic{{Code: "tracker.write.transport_ambiguous", Severity: "error", Message: "provider write ended without a confirmed response; reconcile before any new dispatch"}}
		return result, errors.New("provider write transport failed; outcome is unknown")
	}
	if len(resp.Body) > limits.ResponseBytes {
		result.Disposition = connector.Unknown
		result.Value.Disposition = "unknown"
		result.Diagnostics = []contracts.Diagnostic{{Code: "tracker.write.response_limit", Severity: "error", Message: "provider write response exceeded the configured byte limit; outcome is unknown"}}
		return result, nil
	}
	dispatchObservedAt := time.Now().UTC().Format(time.RFC3339Nano)
	result.Value.ObservedAt = dispatchObservedAt
	if ref, evidenceErr := w.captureEvidence("dispatch", resp.Body, dispatchObservedAt); evidenceErr == nil {
		result.Value.DispatchEvidence = append(result.Value.DispatchEvidence, ref)
	} else {
		result.Diagnostics = append(result.Diagnostics, contracts.Diagnostic{Code: "tracker.write.dispatch_evidence_unavailable", Severity: "warning", Message: "provider response evidence was not durably captured"})
	}
	return classifyWriteResponse(result, resp), nil
}

func classifyWriteResponse(result connector.Result[contracts.SyncReceipt], resp Response) connector.Result[contracts.SyncReceipt] {
	switch {
	case resp.Status == 401:
		result.Disposition = connector.Unsupported
		result.Value.Disposition = "rejected"
		result.Diagnostics = append(result.Diagnostics, contracts.Diagnostic{Code: "tracker.write.authentication", Severity: "error", Message: "provider rejected the configured authentication"})
	case resp.Status == 403:
		result.Disposition = connector.Unsupported
		result.Value.Disposition = "rejected"
		result.Diagnostics = append(result.Diagnostics, contracts.Diagnostic{Code: "tracker.write.permission", Severity: "error", Message: "provider denied permission for the approved operation"})
	case resp.Status == 429:
		result.Disposition = connector.Unknown
		result.Value.Disposition = "unknown"
		message := "provider rate limited the write; do not automatically resend"
		if seconds := retryAfterSeconds(resp.Headers); seconds > 0 {
			message = fmt.Sprintf("provider rate limited the write; retry-after hint is %d seconds; do not automatically resend", seconds)
		}
		result.Diagnostics = append(result.Diagnostics, contracts.Diagnostic{Code: "tracker.write.rate_limited", Severity: "warning", Message: message})
	case resp.Status == 409 || resp.Status == 412:
		result.Disposition = connector.Conflict
		result.Value.Disposition = "conflict"
	case resp.Status >= 200 && resp.Status < 300:
		result.Disposition = connector.Supported
		result.Value.Disposition = "unknown" // a send receipt alone is not verified application
	default:
		result.Disposition = connector.Unknown
		result.Value.Disposition = "unknown"
	}
	return result
}

func retryAfterSeconds(headers map[string]string) int {
	for k, v := range headers {
		if strings.EqualFold(k, "retry-after") {
			n, e := strconv.Atoi(strings.TrimSpace(v))
			if e == nil && n >= 1 && n <= 300 {
				return n
			}
			return 0
		}
	}
	return 0
}

func (w ProviderWriter) captureEvidence(stage string, body []byte, observedAt string) (contracts.EvidenceRef, error) {
	if w.EvidenceSink == nil || (stage != "dispatch" && stage != "readback") {
		return contracts.EvidenceRef{}, errors.New("evidence sink unavailable")
	}
	ref, err := w.EvidenceSink(stage, append([]byte(nil), body...), observedAt)
	if err != nil || ref.Asset.SHA256 != Hash(body) || ref.ObservedAt != observedAt {
		return contracts.EvidenceRef{}, errors.New("evidence sink returned a mismatched asset")
	}
	path := ref.Asset.Path
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") || strings.ContainsAny(path, "\x00\r\n") {
		return contracts.EvidenceRef{}, errors.New("evidence asset path is unsafe")
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return contracts.EvidenceRef{}, errors.New("evidence asset path is unsafe")
		}
	}
	wantPurpose := "provider-dispatch-response"
	if stage == "readback" {
		wantPurpose = "provider-readback-observation"
	}
	if ref.Purpose != wantPurpose {
		return contracts.EvidenceRef{}, errors.New("evidence purpose does not match stage")
	}
	return ref, nil
}

func (w ProviderWriter) Reconcile(ctx context.Context, validated connector.ValidatedOperation, prior contracts.SyncReceipt) (connector.Result[contracts.SyncReceipt], error) {
	op := validated.Operation()
	result := receiptFor(op)
	if prior.OperationID != op.OperationID || prior.OperationDigest != op.OperationDigest || prior.ReceiptID == "" {
		result.Disposition = connector.Conflict
		result.Value.Disposition = "conflict"
		return result, nil
	}
	if w.Transport == nil {
		result.Disposition = connector.Unsupported
		return result, nil
	}
	req, err := w.readbackRequest(op)
	if err != nil {
		result.Disposition = connector.Unsupported
		return result, nil
	}
	limits, err := EffectiveLimits(w.Source)
	if err != nil {
		result.Disposition = connector.Unsupported
		return result, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, time.Duration(limits.RequestSeconds)*time.Second)
	defer cancel()
	resp, err := w.Transport.Readback(requestCtx, w.Source, req)
	if err != nil {
		result.Disposition = connector.Unknown
		result.Value.Disposition = "unknown"
		result.Diagnostics = []contracts.Diagnostic{{Code: "tracker.write.readback_unavailable", Severity: "warning", Message: "bounded readback failed or was cancelled; delivery remains unknown"}}
		return result, errors.New("provider readback failed")
	}
	if resp.Status < 200 || resp.Status >= 300 {
		result.Disposition = connector.Unknown
		result.Value.Disposition = "unknown"
		return result, nil
	}
	if len(resp.Body) > limits.ResponseBytes || !completeReadback(w.Source.Provider, resp) || !w.readbackMatches(op, resp) {
		result.Disposition = connector.Unknown
		result.Value.Disposition = "unknown"
		return result, nil
	}
	if op.OperationKind == "update-fields" || w.readbackMatches(op, resp) {
		observedAt := time.Now().UTC().Format(time.RFC3339Nano)
		ref, evidenceErr := w.captureEvidence("readback", resp.Body, observedAt)
		if evidenceErr != nil {
			result.Disposition = connector.Unknown
			result.Value.Disposition = "unknown"
			result.Diagnostics = []contracts.Diagnostic{{Code: "tracker.write.readback_evidence_unavailable", Severity: "warning", Message: "correlated provider state was observed but its response evidence could not be durably captured"}}
			return result, nil
		}
		result.Disposition = connector.Supported
		result.Value.Disposition = "applied"
		result.Value.ObservedAt = observedAt
		result.Value.ReadbackEvidence = []contracts.EvidenceRef{ref}
	} else {
		// A bounded readback without the marker does not establish non-delivery.
		result.Disposition = connector.Unknown
		result.Value.Disposition = "unknown"
	}
	_ = prior
	return result, nil
}

func (w ProviderWriter) readbackMatches(op contracts.SyncOperation, resp Response) bool {
	if contracts.ValidateJSON(resp.Body) != nil {
		return false
	}
	if op.OperationKind == "update-fields" {
		if len(op.Change.Fields) != 1 {
			return false
		}
		var doc struct {
			ID     json.Number                `json:"id"`
			Rev    json.Number                `json:"rev"`
			Fields map[string]json.RawMessage `json:"fields"`
		}
		dec := json.NewDecoder(bytes.NewReader(resp.Body))
		dec.UseNumber()
		if dec.Decode(&doc) != nil || dec.Decode(new(any)) != io.EOF {
			return false
		}
		if string(doc.ID) != op.Authority.Scope.RemoteID {
			return false
		}
		old, oldErr := strconv.Atoi(op.ExpectedRevision)
		next, nextErr := strconv.Atoi(string(doc.Rev))
		if oldErr != nil || nextErr != nil || next <= old {
			return false
		}
		got, ok := doc.Fields[op.Change.Fields[0].ProviderFieldID]
		return ok && jsonEquivalent(got, op.Change.Fields[0].Value)
	}
	if op.OperationKind != "comment" {
		return false
	}
	want, err := w.expectedComment(op)
	if err != nil {
		return false
	}
	var comments []map[string]json.RawMessage
	switch w.Source.Provider {
	case "github":
		if json.Unmarshal(resp.Body, &comments) != nil {
			return false
		}
		number := w.GitHubIssueNumbers[op.Authority.Scope.RemoteID]
		endpoint := "/repos/" + w.Source.ContainerKey + "/issues/" + number
		return exactlyOneCorrelatedComment(comments, "body", want, func(c map[string]json.RawMessage) bool {
			var issueURL string
			if json.Unmarshal(c["issue_url"], &issueURL) != nil {
				return false
			}
			u, err := url.Parse(issueURL)
			return err == nil && u.Host == "api.github.com" && strings.TrimSuffix(u.Path, "/") == endpoint
		})
	case "linear":
		var v struct {
			Data struct {
				Issue struct {
					ID       string `json:"id"`
					Comments struct {
						Nodes []struct {
							Body string `json:"body"`
						} `json:"nodes"`
						PageInfo struct {
							HasNextPage bool `json:"hasNextPage"`
						} `json:"pageInfo"`
					} `json:"comments"`
				} `json:"issue"`
			} `json:"data"`
			Errors []json.RawMessage `json:"errors"`
		}
		if json.Unmarshal(resp.Body, &v) != nil || len(v.Errors) > 0 || v.Data.Issue.ID != op.Authority.Scope.RemoteID || v.Data.Issue.Comments.Nodes == nil || v.Data.Issue.Comments.PageInfo.HasNextPage {
			return false
		}
		matches := 0
		markerCount := 0
		for _, c := range v.Data.Issue.Comments.Nodes {
			markerCount += strings.Count(c.Body, marker(op))
			if c.Body == want {
				matches++
			}
		}
		return matches == 1 && markerCount == 1
	case "jira":
		var envelope struct {
			Comments []map[string]json.RawMessage `json:"comments"`
		}
		if !numericJiraID(op.Authority.Scope.RemoteID) || json.Unmarshal(resp.Body, &envelope) != nil || envelope.Comments == nil {
			return false
		}
		matches := 0
		markerCount := 0
		for _, c := range envelope.Comments {
			var self string
			if json.Unmarshal(c["self"], &self) != nil || !w.jiraCommentSelfMatches(self, op.Authority.Scope.RemoteID) {
				continue
			}
			var body any
			if json.Unmarshal(c["body"], &body) != nil {
				continue
			}
			text := collectADFText(body)
			markerCount += strings.Count(text, marker(op))
			if text == want {
				matches++
			}
		}
		return matches == 1 && markerCount == 1
	default:
		return false
	}
}

func (w ProviderWriter) jiraCommentSelfMatches(raw, issueID string) bool {
	if !numericJiraID(issueID) {
		return false
	}
	origin, err := url.Parse(w.Source.Origin)
	if err != nil {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || u.Scheme != "https" || u.Host != origin.Host || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	base := "/rest/api/3/issue/" + url.PathEscape(issueID) + "/comment/"
	if origin.Hostname() == "api.atlassian.com" {
		base = "/ex/jira/" + url.PathEscape(w.Source.NativeTenantID) + base
	}
	if !strings.HasPrefix(u.EscapedPath(), base) {
		return false
	}
	commentID := strings.TrimPrefix(u.EscapedPath(), base)
	return positiveNumericID(commentID) && !strings.Contains(commentID, "/")
}

func jsonEquivalent(a, b []byte) bool {
	var av, bv any
	ad := json.NewDecoder(bytes.NewReader(a))
	ad.UseNumber()
	bd := json.NewDecoder(bytes.NewReader(b))
	bd.UseNumber()
	return ad.Decode(&av) == nil && ad.Decode(new(any)) == io.EOF && bd.Decode(&bv) == nil && bd.Decode(new(any)) == io.EOF && reflect.DeepEqual(av, bv)
}

func (w ProviderWriter) expectedComment(op contracts.SyncOperation) (string, error) {
	asset := op.Change.Comment
	if asset == nil || w.LoadComment == nil {
		return "", fmt.Errorf("comment asset unavailable")
	}
	if err := evidence.CheckPath(asset.Path); err != nil {
		return "", fmt.Errorf("unsafe comment asset path")
	}
	b, err := w.LoadComment(asset.Path, asset.SHA256)
	if err != nil || len(b) > 1<<20 || Hash(b) != asset.SHA256 {
		return "", fmt.Errorf("comment asset unavailable")
	}
	return string(b) + "\n\n" + marker(op), nil
}

func exactlyOneCorrelatedComment(comments []map[string]json.RawMessage, bodyKey, want string, correlated func(map[string]json.RawMessage) bool) bool {
	matches, markers := 0, 0
	for _, c := range comments {
		var body string
		if json.Unmarshal(c[bodyKey], &body) != nil {
			continue
		}
		markers += strings.Count(body, "<!-- aok-op:")
		if body == want && correlated(c) {
			matches++
		}
	}
	return matches == 1 && markers == 1
}

func collectADFText(v any) string {
	switch x := v.(type) {
	case map[string]any:
		if text, ok := x["text"].(string); ok {
			return text
		}
		var b strings.Builder
		if children, ok := x["content"].([]any); ok {
			for _, child := range children {
				b.WriteString(collectADFText(child))
			}
		}
		return b.String()
	case []any:
		var b strings.Builder
		for _, child := range x {
			b.WriteString(collectADFText(child))
		}
		return b.String()
	default:
		return ""
	}
}

func numericJiraID(s string) bool { return positiveNumericID(s) }

func completeReadback(provider string, r Response) bool {
	if contracts.ValidateJSON(r.Body) != nil {
		return false
	}
	switch provider {
	case "github":
		for k, v := range r.Headers {
			if strings.EqualFold(k, "link") && v != "" {
				return false
			}
		}
		var comments []json.RawMessage
		return json.Unmarshal(r.Body, &comments) == nil && comments != nil
	case "linear":
		var v map[string]any
		if json.Unmarshal(r.Body, &v) != nil || v == nil {
			return false
		}
		if errs, exists := v["errors"]; exists {
			a, ok := errs.([]any)
			if !ok || len(a) > 0 {
				return false
			}
		}
		data, ok := v["data"].(map[string]any)
		if !ok {
			return false
		}
		issue, ok := data["issue"].(map[string]any)
		if !ok || issue["id"] == nil {
			return false
		}
		comments, ok := issue["comments"].(map[string]any)
		if !ok || comments["nodes"] == nil {
			return false
		}
		pi, ok := comments["pageInfo"].(map[string]any)
		if !ok {
			return false
		}
		more, ok := pi["hasNextPage"].(bool)
		return ok && !more
	case "jira":
		var v map[string]json.RawMessage
		if json.Unmarshal(r.Body, &v) != nil || v == nil {
			return false
		}
		for _, k := range []string{"startAt", "maxResults", "total", "comments"} {
			if _, ok := v[k]; !ok {
				return false
			}
		}
		var start, max, total int
		var comments []json.RawMessage
		return json.Unmarshal(v["startAt"], &start) == nil && json.Unmarshal(v["maxResults"], &max) == nil && json.Unmarshal(v["total"], &total) == nil && json.Unmarshal(v["comments"], &comments) == nil && comments != nil && start == 0 && max > 0 && total <= max && len(comments) == total
	case "azure-devops":
		return true
	default:
		return false
	}
}

func (w ProviderWriter) readbackRequest(op contracts.SyncOperation) (WriteRequest, error) {
	id := op.Authority.Scope
	switch w.Source.Provider {
	case "github":
		number := w.GitHubIssueNumbers[id.RemoteID]
		if !positiveNumericID(number) || !hasGitHubIssueScope(w.Source.Scope.ItemIDs, number) {
			return WriteRequest{}, fmt.Errorf("GitHub immutable item ID lacks issue-number binding")
		}
		return WriteRequest{Method: "GET", Path: "/repos/" + w.Source.ContainerKey + "/issues/" + number + "/comments?per_page=100", ItemID: id.RemoteID}, nil
	case "linear":
		const q = "query AOKCommentRead($id: String!) { issue(id: $id) { comments { nodes { id body } pageInfo { hasNextPage endCursor } } } }"
		b, _ := json.Marshal(map[string]any{"query": q, "variables": map[string]string{"id": id.RemoteID}})
		return WriteRequest{Method: "POST", Path: "/graphql", ContentType: "application/json", Body: b, ItemID: id.RemoteID}, nil
	case "jira":
		if !numericJiraID(id.RemoteID) {
			return WriteRequest{}, fmt.Errorf("Jira readback requires immutable numeric issue ID")
		}
		path := "/rest/api/3/issue/" + url.PathEscape(id.RemoteID) + "/comment"
		if u, _ := url.Parse(w.Source.Origin); u.Hostname() == "api.atlassian.com" {
			path = "/ex/jira/" + url.PathEscape(w.Source.NativeTenantID) + path
		}
		return WriteRequest{Method: "GET", Path: path, ItemID: id.RemoteID}, nil
	case "azure-devops":
		org, project, e := azureScope(w.Source)
		if e != nil {
			return WriteRequest{}, e
		}
		return WriteRequest{Method: "GET", Path: adoPath(org, project, "wit/workitems/"+id.RemoteID+"?api-version=7.1&$expand=Fields"), ItemID: id.RemoteID}, nil
	default:
		return WriteRequest{}, fmt.Errorf("unsupported provider")
	}
}

func receiptFor(op contracts.SyncOperation) connector.Result[contracts.SyncReceipt] {
	key := Hash([]byte(op.OperationID))[:20]
	observed := time.Now().UTC().Format(time.RFC3339Nano)
	return connector.Result[contracts.SyncReceipt]{Disposition: connector.Unknown, Value: contracts.SyncReceipt{DocumentHeader: contracts.DocumentHeader{APIVersion: "aok/v1alpha1", Kind: "SyncReceipt", Metadata: contracts.OperationalMetadata{ID: "receipt-" + key, Name: "Tracker write receipt", Version: "1.0.0", Description: "Typed tracker write dispatch and reconciliation observation"}}, SyncOperationRef: contracts.Reference{Kind: "SyncOperation", ID: op.Metadata.ID, Version: op.Metadata.Version}, OperationID: op.OperationID, OperationDigest: op.OperationDigest, ReceiptID: "tracker-" + key, ObservedAt: observed, Disposition: "unknown", DispatchEvidence: []contracts.EvidenceRef{}, ReadbackEvidence: []contracts.EvidenceRef{}}}
}
