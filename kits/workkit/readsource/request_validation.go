package readsource

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var mutationWord = regexp.MustCompile(`(?i)\b(mutation|subscription)\b`)

func ValidateRequest(s Source, r Request) error {
	if r.Kind == "azure-wiql" {
		var b struct {
			Query string `json:"query"`
		}
		expected, e := SharedAzureScopedWIQL(s)
		if e != nil || decodeRaw(r.Body, &b) != nil || b.Query != expected {
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
	if e := SharedValidateEndpoint(s, r, u); e != nil {
		return e
	}
	if e := SharedValidateSelectedItem(s, r, u); e != nil {
		return e
	}
	if r.Method == "POST" && (s.Provider == "github" || s.Provider == "linear") {
		return SharedValidateQueryTemplate(s, r)
	}
	return nil
}
func usageDecode(b []byte, target any) error { return decodeRaw(b, target) }
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
