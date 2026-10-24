// Package providers supplies bounded HTTP transports for the narrowly qualified
// tracker writer slices. It does not mint connector authorization.
package providers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
)

// CredentialResolver resolves an opaque, protected authorization reference.
// Implementations should read from a secret manager or an operator-owned
// environment mapping. The provider source never names an environment variable.
type CredentialResolver interface {
	Resolve(context.Context, string) (Credential, error)
}

type Credential struct{ Token, Username string }
type CredentialResolverFunc func(context.Context, string) (Credential, error)

func (f CredentialResolverFunc) Resolve(ctx context.Context, ref string) (Credential, error) {
	return f(ctx, ref)
}

// Route is the exact immutable destination and identity tuple approved outside
// task data. Source is retained for typed request construction by tracker.
type Route struct {
	Source        tracker.Source
	AuthReference string
	Resolver      CredentialResolver
	RoundTripper  http.RoundTripper
}

type httpTransport struct{ route Route }

// NewTransport fails closed unless the configured route is an exact HTTPS
// provider origin and has an externally supplied credential resolver.
func newTransport(route Route) (*httpTransport, error) {
	if err := tracker.ValidateSource(route.Source); err != nil {
		return nil, fmt.Errorf("invalid provider source")
	}
	if route.AuthReference == "" || route.Resolver == nil {
		return nil, fmt.Errorf("protected auth reference and resolver required")
	}
	route.Source = cloneProviderSource(route.Source)
	u, err := url.Parse(route.Source.Origin)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" || !officialOrigin(route.Source.Provider, strings.ToLower(u.Hostname())) {
		return nil, fmt.Errorf("unsupported or invalid official provider origin")
	}
	return &httpTransport{route: route}, nil
}

func (t *httpTransport) Dispatch(ctx context.Context, source tracker.Source, req tracker.WriteRequest) (tracker.Response, error) {
	return t.do(ctx, source, req)
}
func (t *httpTransport) Readback(ctx context.Context, source tracker.Source, req tracker.WriteRequest) (tracker.Response, error) {
	return t.do(ctx, source, req)
}

func (t *httpTransport) do(ctx context.Context, source tracker.Source, r tracker.WriteRequest) (tracker.Response, error) {
	if t == nil || t.route.Resolver == nil || !sameRoute(t.route.Source, source) {
		return tracker.Response{}, fmt.Errorf("provider route mismatch")
	}
	if !allowedProviderRequest(source, r) {
		return tracker.Response{}, fmt.Errorf("invalid typed provider request")
	}
	limits, err := tracker.EffectiveLimits(source)
	if err != nil {
		return tracker.Response{}, fmt.Errorf("invalid request limits")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(limits.RequestSeconds)*time.Second)
	defer cancel()
	secret, err := t.route.Resolver.Resolve(ctx, t.route.AuthReference)
	if err != nil || secret.Token == "" || strings.ContainsAny(secret.Token+secret.Username, "\r\n") {
		return tracker.Response{}, fmt.Errorf("provider credential unavailable")
	}
	target := strings.TrimSuffix(source.Origin, "/") + r.Path
	u, err := url.Parse(target)
	base, _ := url.Parse(source.Origin)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Host, base.Host) || u.User != nil || u.Fragment != "" {
		return tracker.Response{}, fmt.Errorf("provider target escaped pinned origin")
	}
	hreq, err := http.NewRequestWithContext(ctx, r.Method, target, bytes.NewReader(r.Body))
	if err != nil {
		return tracker.Response{}, fmt.Errorf("invalid provider request")
	}
	hreq.Header.Set("Accept", "application/json")
	if source.Provider == "github" {
		hreq.Header.Set("Accept", "application/vnd.github+json")
	}
	hreq.Header.Set("Content-Type", r.ContentType)
	if err := setAuth(hreq, source, secret); err != nil {
		return tracker.Response{}, err
	}
	client := http.Client{Transport: t.route.RoundTripper, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(hreq)
	if err != nil {
		return tracker.Response{}, fmt.Errorf("provider request failed")
	}
	defer resp.Body.Close()
	out := tracker.Response{Status: resp.StatusCode, Headers: safeHeaders(resp.Header)}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return out, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limits.ResponseBytes)+1))
	if err != nil {
		return tracker.Response{}, fmt.Errorf("provider response read failed")
	}
	if len(body) > limits.ResponseBytes {
		return tracker.Response{}, fmt.Errorf("provider response exceeded byte limit")
	}
	if bytes.Contains(body, []byte(secret.Token)) || secret.Username != "" && bytes.Contains(body, []byte(secret.Username)) || bytes.Contains(body, []byte(hreq.Header.Get("Authorization"))) {
		return tracker.Response{}, fmt.Errorf("provider response reflected protected credentials")
	}
	// Body is needed for marker/native-ID correlation by the tracker writer.
	// Its digest is retained as an allowlisted response header for audit sinks.
	sum := sha256.Sum256(body)
	out.Headers["X-AOK-Response-SHA256"] = hex.EncodeToString(sum[:])
	sanitized, err := redactJSON(body)
	if err != nil {
		return tracker.Response{}, fmt.Errorf("provider response was not valid bounded JSON")
	}
	out.Body = sanitized
	return out, nil
}

func setAuth(req *http.Request, s tracker.Source, c Credential) error {
	switch s.Provider {
	case "github":
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("X-GitHub-Api-Version", s.APIVersion)
	case "azure-devops":
		if s.Auth.Mode == "pat" {
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+c.Token)))
		} else {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
	case "linear":
		if s.Auth.Mode == "api-key" {
			req.Header.Set("Authorization", c.Token)
		} else {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
	case "jira":
		if s.Auth.Mode == "basic" {
			if c.Username == "" {
				return fmt.Errorf("provider credential unavailable")
			}
			req.SetBasicAuth(c.Username, c.Token)
		} else {
			req.Header.Set("Authorization", "Bearer "+c.Token)
		}
	default:
		return fmt.Errorf("unsupported provider")
	}
	return nil
}

func sameRoute(a, b tracker.Source) bool {
	return reflect.DeepEqual(a, b)
}
func safeHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"Content-Type", "ETag", "Link", "Retry-After", "X-RateLimit-Remaining", "X-RateLimit-Reset", "X-Request-Id", "X-GitHub-Request-Id"} {
		if v := h.Get(k); v != "" {
			out[k] = v
		}
	}
	return out
}

// redactJSON removes credential-like properties from captured response payloads
// while retaining fields needed for native identity and marker reconciliation.
func redactJSON(b []byte) ([]byte, error) {
	if contracts.ValidateJSON(b) != nil {
		return nil, fmt.Errorf("invalid or ambiguous JSON")
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("trailing provider response JSON")
	}
	var clean func(any)
	clean = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, child := range x {
				key := normalizeCredentialKey(k)
				if strings.Contains(key, "token") || strings.Contains(key, "secret") || strings.Contains(key, "password") || strings.Contains(key, "auth") || strings.Contains(key, "cookie") || strings.Contains(key, "apikey") || strings.Contains(key, "privatekey") || strings.Contains(key, "credential") {
					delete(x, k)
					continue
				}
				clean(child)
			}
		case []any:
			for _, child := range x {
				clean(child)
			}
		}
	}
	clean(value)
	return json.Marshal(value)
}

// WriterOptions are the tracker-layer inputs needed to construct the typed
// operation writer. Authorization still comes only from connector's opaque
// permit at Apply time.
type WriterOptions struct {
	CommentLoader      tracker.CommentLoader
	GitHubIssueNumbers map[string]string
	EvidenceSink       tracker.EvidenceSink
}

// NewWriter builds a tracker ProviderWriter behind this package's pinned HTTP
// transport. The caller must still pass operations through connector dispatch
// and supply a valid one-use AuthorizationContext.
func NewWriter(route Route, options WriterOptions) (connector.Writer, error) {
	source := cloneProviderSource(route.Source)
	route.Source = source
	transport, err := newTransport(route)
	if err != nil {
		return nil, err
	}
	return &providerWriter{inner: tracker.NewProviderWriter(tracker.WriterConfig{
		Source:             source,
		Transport:          transport,
		CommentLoader:      options.CommentLoader,
		GitHubIssueNumbers: cloneStringMap(options.GitHubIssueNumbers),
		AuthReference:      route.AuthReference,
		EvidenceSink:       options.EvidenceSink,
	})}, nil
}

// officialOrigin intentionally recognizes only the documented SaaS API hosts
// currently covered by the typed request and readback correlation logic.
func officialOrigin(provider, host string) bool {
	switch provider {
	case "github":
		return host == "api.github.com"
	case "azure-devops":
		return host == "dev.azure.com"
	case "linear":
		return host == "api.linear.app"
	case "jira":
		return host == "api.atlassian.com" || strings.HasSuffix(host, ".atlassian.net")
	default:
		return false
	}
}

type providerWriter struct{ inner tracker.ProviderWriter }

func cloneProviderSource(s tracker.Source) tracker.Source {
	s.Scope.ItemIDs = append([]string(nil), s.Scope.ItemIDs...)
	if s.Limits != nil {
		limits := *s.Limits
		s.Limits = &limits
	}
	return s
}
func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (w *providerWriter) Apply(ctx context.Context, op connector.ValidatedOperation, auth connector.AuthorizationContext) (connector.Result[contracts.SyncReceipt], error) {
	return w.inner.Apply(ctx, op, auth)
}
func (w *providerWriter) Reconcile(ctx context.Context, op connector.ValidatedOperation, prior contracts.SyncReceipt) (connector.Result[contracts.SyncReceipt], error) {
	return w.inner.Reconcile(ctx, op, prior)
}

func normalizeCredentialKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// allowedProviderRequest mirrors the exact paths generated by tracker request
// builders. This remains defense in depth even though the concrete transport
// is private behind connector.Writer.
func allowedProviderRequest(s tracker.Source, r tracker.WriteRequest) bool {
	if !strings.HasPrefix(r.Path, "/") || strings.HasPrefix(r.Path, "//") || strings.Contains(r.Path, "\\") || strings.ContainsAny(r.Path, "\r\n\x00") {
		return false
	}
	u, err := url.ParseRequestURI(r.Path)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" {
		return false
	}
	switch s.Provider {
	case "github":
		parts := strings.Split(s.ContainerKey, "/")
		path := strings.Split(u.Path, "/")
		if len(parts) != 2 || len(path) != 7 || path[1] != "repos" || path[2] != parts[0] || path[3] != parts[1] || path[4] != "issues" || !positiveProviderID(path[5]) || path[6] != "comments" {
			return false
		}
		return r.Method == "POST" && u.RawQuery == "" || r.Method == "GET" && u.Query().Get("per_page") == "100" && len(u.Query()) == 1
	case "azure-devops":
		org, project, ok := strings.Cut(s.ContainerKey, "/")
		if !ok {
			return false
		}
		prefix := "/" + url.PathEscape(org) + "/" + url.PathEscape(project) + "/_apis/wit/workitems/"
		if !strings.HasPrefix(u.Path, prefix) || !positiveProviderID(strings.TrimPrefix(u.Path, prefix)) {
			return false
		}
		q := u.Query()
		if q.Get("api-version") != "7.1" {
			return false
		}
		return r.Method == "PATCH" && len(q) == 1 || r.Method == "GET" && q.Get("$expand") == "Fields" && len(q) == 2
	case "linear":
		if r.Method != "POST" || u.Path != "/graphql" || u.RawQuery != "" {
			return false
		}
		var body struct {
			Query string `json:"query"`
		}
		return json.Unmarshal(r.Body, &body) == nil && (body.Query == "mutation AOKComment($input: CommentCreateInput!) { commentCreate(input: $input) { success comment { id body issue { id } } } }" || body.Query == "query AOKCommentRead($id: String!) { issue(id: $id) { comments { nodes { id body } pageInfo { hasNextPage endCursor } } } }")
	case "jira":
		prefix := "/rest/api/3/issue/"
		if strings.EqualFold(strings.SplitN(s.Origin, "/", 4)[2], "api.atlassian.com") {
			prefix = "/ex/jira/" + url.PathEscape(s.NativeTenantID) + prefix
		}
		if !strings.HasPrefix(u.Path, prefix) || !strings.HasSuffix(u.Path, "/comment") || u.RawQuery != "" {
			return false
		}
		id := strings.TrimSuffix(strings.TrimPrefix(u.Path, prefix), "/comment")
		return positiveProviderID(id) && (r.Method == "POST" || r.Method == "GET")
	default:
		return false
	}
}

func positiveProviderID(s string) bool {
	n, err := strconv.ParseUint(s, 10, 64)
	return err == nil && n > 0
}
