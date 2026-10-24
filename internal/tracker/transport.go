package tracker

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// HTTPTransport owns redirect rejection. Inject only a RoundTripper, so callers
// cannot accidentally replace that policy with a redirect-following client.
type HTTPTransport struct {
	// DCRootsPEM is protected operator input, never captured source material.
	DCRootsPEM   []byte
	Policy       *ReadPolicy
	RoundTripper http.RoundTripper
	LookupEnv    func(string) (string, bool)
}

func (t HTTPTransport) Do(ctx context.Context, s Source, r Request) (Response, error) {
	if e := ValidateRequest(s, r); e != nil {
		return Response{}, e
	}
	if s.Provider == "github" {
		return Response{}, fmt.Errorf("GitHub uses gh-session transport")
	}
	if !policyAllows(t.Policy, s) {
		return Response{}, &TransportError{Disposition: "authentication", Message: "operator read policy does not approve this credential destination and scope"}
	}
	roundTripper := t.RoundTripper
	if s.Provider == "jira-datacenter" {
		if s.Deployment == nil || len(t.DCRootsPEM) == 0 || Hash(t.DCRootsPEM) != s.Deployment.TLSTrustSHA256 || t.RoundTripper != nil {
			return Response{}, fmt.Errorf("Data Center requires exact externally supplied pinned TLS roots and the protected TLS transport")
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(t.DCRootsPEM) {
			return Response{}, fmt.Errorf("invalid pinned Data Center roots")
		}
		roundTripper = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
	}
	limits, _ := EffectiveLimits(s)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(limits.RequestSeconds)*time.Second)
	defer cancel()
	var body io.Reader
	if len(r.Body) > 0 && !bytes.Equal(bytes.TrimSpace(r.Body), []byte("null")) {
		body = bytes.NewReader(r.Body)
	}
	req, e := http.NewRequestWithContext(ctx, r.Method, strings.TrimSuffix(s.Origin, "/")+r.Path, body)
	if e != nil {
		return Response{}, fmt.Errorf("invalid request")
	}
	lookup := t.LookupEnv
	if lookup == nil {
		lookup = os.LookupEnv
	}
	token, ok := lookup(s.Auth.TokenEnv)
	if !ok || token == "" || strings.ContainsAny(token, "\r\n") {
		return Response{}, &TransportError{Disposition: "authentication", Message: "credential reference unavailable"}
	}
	switch s.Auth.Mode {
	case "bearer":
		req.Header.Set("Authorization", "Bearer "+token)
	case "api-key":
		req.Header.Set("Authorization", token)
	case "pat":
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+token)))
	case "basic":
		user, ok := lookup(s.Auth.UsernameEnv)
		if !ok || user == "" || strings.ContainsAny(user, "\r\n") {
			return Response{}, &TransportError{Disposition: "authentication", Message: "username reference unavailable"}
		}
		req.SetBasicAuth(user, token)
	default:
		return Response{}, fmt.Errorf("unsupported HTTP authentication")
	}
	req.Header.Set("Accept", "application/json")
	if len(r.Body) > 0 && !bytes.Equal(r.Body, []byte("null")) {
		req.Header.Set("Content-Type", "application/json")
	}
	client := http.Client{Transport: roundTripper, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, &TransportError{Disposition: "transport-error", Message: "provider request failed"}
	}
	defer resp.Body.Close()
	out := Response{Status: resp.StatusCode, Headers: allowedHeaders(resp.Header)}
	for _, v := range out.Headers {
		if strings.Contains(v, token) || strings.Contains(v, req.Header.Get("Authorization")) {
			return Response{}, &TransportError{Disposition: "privacy-blocked", Message: "provider response reflected authentication material"}
		}
	}
	if out.Status < 200 || out.Status >= 300 {
		return out, nil
	} // provider error payloads can contain private diagnostics
	out.Body, e = io.ReadAll(io.LimitReader(resp.Body, int64(limits.ResponseBytes)+1))
	if e != nil {
		return Response{}, fmt.Errorf("response read failed")
	}
	if len(out.Body) > limits.ResponseBytes {
		return Response{}, &TransportError{Disposition: "incomplete", Message: "response byte limit reached"}
	}
	if bytes.Contains(out.Body, []byte(token)) || bytes.Contains(out.Body, []byte(req.Header.Get("Authorization"))) {
		return Response{}, &TransportError{Disposition: "privacy-blocked", Message: "provider response reflected authentication material"}
	}
	return out, nil
}

type TransportError struct{ Disposition, Message string }

func (e *TransportError) Error() string { return e.Message }

type Runner interface {
	Run(context.Context, []string, []byte, int) ([]byte, error)
}
type CommandRunner struct{}
type boundedBuffer struct {
	bytes.Buffer
	max int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.max-b.Len() {
		return 0, errors.New("command output limit")
	}
	return b.Buffer.Write(p)
}
func (CommandRunner) Run(ctx context.Context, args []string, input []byte, max int) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Stdin = bytes.NewReader(input)
	out := &boundedBuffer{max: max}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 100 * time.Millisecond
	if e := cmd.Run(); e != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return out.Bytes(), fmt.Errorf("gh read failed")
	}
	return out.Bytes(), nil
}

type GitHubTransport struct {
	Runner Runner
	Policy *ReadPolicy
}

func (t GitHubTransport) Do(ctx context.Context, s Source, r Request) (Response, error) {
	if e := ValidateRequest(s, r); e != nil {
		return Response{}, e
	}
	if s.Provider != "github" {
		return Response{}, fmt.Errorf("wrong transport")
	}
	if !policyAllows(t.Policy, s) {
		return Response{}, &TransportError{Disposition: "authentication", Message: "operator read policy does not approve this gh host and scope"}
	}
	limits, _ := EffectiveLimits(s)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(limits.RequestSeconds)*time.Second)
	defer cancel()
	u, _ := url.Parse(s.Origin)
	host := u.Host
	if host == "api.github.com" {
		host = "github.com"
	}
	args := []string{"api", "--hostname", host, "--include", "--method", r.Method, r.Path, "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: " + s.APIVersion}
	if len(r.Body) > 0 && !bytes.Equal(r.Body, []byte("null")) {
		args = append(args, "--input", "-")
	}
	runner := t.Runner
	if runner == nil {
		runner = CommandRunner{}
	}
	b, e := runner.Run(ctx, args, r.Body, limits.ResponseBytes+16384)
	if e != nil && len(b) == 0 {
		if ctx.Err() != nil {
			return Response{}, ctx.Err()
		}
		return Response{}, &TransportError{Disposition: "transport-error", Message: "gh read failed; authenticate with the configured gh host"}
	}
	// gh --include emits an HTTP header block followed by the unchanged JSON body.
	cut := bytes.Index(b, []byte("\r\n\r\n"))
	sep := 4
	if cut < 0 {
		cut = bytes.Index(b, []byte("\n\n"))
		sep = 2
	}
	if cut < 0 {
		return Response{}, fmt.Errorf("gh response headers missing")
	}
	lines := strings.Split(strings.ReplaceAll(string(b[:cut]), "\r\n", "\n"), "\n")
	var status int
	if _, e = fmt.Sscanf(lines[0], "HTTP/%*s %d", &status); e != nil {
		parts := strings.Fields(lines[0])
		if len(parts) < 2 {
			return Response{}, fmt.Errorf("invalid gh status")
		}
		if _, e = fmt.Sscanf(parts[1], "%d", &status); e != nil {
			return Response{}, fmt.Errorf("invalid gh status")
		}
	}
	headers := http.Header{}
	for _, line := range lines[1:] {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			headers.Add(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}
	body := b[cut+sep:]
	if len(body) > limits.ResponseBytes {
		return Response{}, &TransportError{Disposition: "incomplete", Message: "response byte limit reached"}
	}
	if status < 200 || status >= 300 {
		body = nil
	}
	return Response{Status: status, Headers: allowedHeaders(headers), Body: body}, nil
}
func allowedHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"ETag", "Last-Modified", "Date", "Retry-After", "Link", "X-RateLimit-Remaining", "X-RateLimit-Reset", "X-MS-ContinuationToken"} {
		if v := h.Get(k); v != "" {
			out[strings.ToLower(k)] = v
		}
	}
	return out
}

var mutationWord = regexp.MustCompile(`(?i)\b(mutation|subscription)\b`)

func ValidateRequest(s Source, r Request) error {
	if r.Kind == "azure-wiql" {
		var b struct {
			Query string `json:"query"`
		}
		expected, e := azureScopedWIQL(s)
		if e != nil || usage.Decode(r.Body, &b) != nil || b.Query != expected {
			return fmt.Errorf("Azure query does not match project-scoped source")
		}
	}
	if r.Method == "GET" && len(r.Body) > 0 && !bytes.Equal(bytes.TrimSpace(r.Body), []byte("null")) {
		return fmt.Errorf("GET request body is not supported")
	}
	if e := ValidateSource(s); e != nil {
		return e
	}
	u, e := url.Parse(r.Path)
	if e != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(r.Path, "/") || strings.HasPrefix(r.Path, "//") || u.Fragment != "" || strings.ContainsAny(r.Path, "\\\r\n") {
		return fmt.Errorf("request escapes fixed provider origin")
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == ".." || part == "." {
			return fmt.Errorf("unsafe request path")
		}
	}
	if r.Method != "GET" && r.Method != "POST" {
		return fmt.Errorf("remote mutation methods prohibited")
	}
	if r.Kind == "" || !strings.HasPrefix(r.Kind, map[string]string{"github": "github-", "azure-devops": "azure-", "linear": "linear-", "jira": "jira-", "jira-datacenter": "jira-dc-"}[s.Provider]) {
		return fmt.Errorf("request kind does not match provider")
	}
	if r.Method == "POST" {
		if s.Provider == "github" || s.Provider == "linear" {
			if u.Path != "/graphql" {
				return fmt.Errorf("only query GraphQL POST allowed")
			}
			var body struct {
				Query     string                     `json:"query"`
				Variables map[string]json.RawMessage `json:"variables"`
			}
			if e := usageDecode(r.Body, &body); e != nil {
				return e
			}
			if !strings.HasPrefix(strings.TrimSpace(body.Query), "query") || mutationWord.MatchString(body.Query) {
				return fmt.Errorf("only fixed read queries permitted")
			}
		}
		if s.Provider == "azure-devops" {
			if !strings.HasSuffix(u.Path, "/wit/workitemsbatch") && !strings.HasSuffix(u.Path, "/wit/wiql") {
				return fmt.Errorf("only Azure read batch/query POST allowed")
			}
			if e := validateAzureReadBody(u.Path, r.Body); e != nil {
				return e
			}
		}
		if s.Provider == "jira" {
			return fmt.Errorf("Jira v1 transport uses GET only")
		}
	}
	switch s.Provider {
	case "github":
		if u.Path != "/graphql" {
			parts := strings.Split(s.ContainerKey, "/")
			if len(parts) != 2 || (!strings.HasPrefix(u.Path, "/repos/"+parts[0]+"/"+parts[1]+"/") && u.Path != "/repos/"+s.ContainerKey) {
				return fmt.Errorf("GitHub request outside repository")
			}
		}
	case "azure-devops":
		parts := strings.Split(s.ContainerKey, "/")
		if len(parts) != 2 || (!strings.HasPrefix(u.Path, "/"+url.PathEscape(parts[0])+"/"+url.PathEscape(parts[1])+"/_apis/") && u.Path != "/"+url.PathEscape(parts[0])+"/_apis/projects/"+url.PathEscape(parts[1]) && u.Path != "/"+url.PathEscape(parts[0])+"/_apis/process/processes") {
			return fmt.Errorf("Azure request outside configured organization")
		}
	case "linear":
		if u.Path != "/graphql" {
			return fmt.Errorf("unexpected Linear endpoint")
		}
	case "jira":
		prefix := "/rest/api/3/"
		origin, _ := url.Parse(s.Origin)
		if origin.Hostname() == "api.atlassian.com" {
			prefix = "/ex/jira/" + url.PathEscape(s.NativeTenantID) + prefix
		}
		if !strings.HasPrefix(u.Path, prefix) {
			return fmt.Errorf("unexpected Jira Cloud endpoint")
		}
	}
	if e := validateEndpoint(s, r, u); e != nil {
		return e
	}
	if e := validateSelectedItem(s, r, u); e != nil {
		return e
	}
	if r.Method == "POST" && (s.Provider == "github" || s.Provider == "linear") {
		return validateQueryTemplate(s, r)
	}
	return nil
}
func usageDecode(b []byte, target any) error { return usage.Decode(b, target) }
func validateAzureReadBody(path string, b []byte) error {
	if strings.HasSuffix(path, "/wiql") {
		var q struct {
			Query string `json:"query"`
		}
		if e := usageDecode(b, &q); e != nil {
			return e
		}
		if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(q.Query)), "SELECT ") || strings.ContainsAny(q.Query, ";\x00") {
			return fmt.Errorf("WIQL must be read-only SELECT")
		}
	} else {
		var q map[string]json.RawMessage
		if e := usageDecode(b, &q); e != nil {
			return e
		}
		for k := range q {
			if k != "ids" && k != "$expand" && k != "fields" && k != "asOf" && k != "errorPolicy" {
				return fmt.Errorf("unsupported read-batch parameter")
			}
		}
	}
	return nil
}
