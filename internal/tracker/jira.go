package tracker

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

type Jira struct{}

const jiraFieldsPageSize = 50
const jiraFieldsMaxStart = 10000

func NewJira() Adapter        { return Jira{} }
func (Jira) Provider() string { return "jira" }

func (Jira) Initial(s Source, mode string, cp *Checkpoint) ([]Request, error) {
	if s.Provider != "jira" || s.APIVersion != "3" || s.NativeTenantID == "" || s.ContainerID == "" || s.ContainerKey == "" {
		return nil, fmt.Errorf("invalid Jira Cloud source: cloud id, project id/key and API version 3 are required")
	}
	u, e := url.Parse(s.Origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("Jira origin must be an HTTPS site origin")
	}
	host := strings.ToLower(u.Hostname())
	if host != "api.atlassian.com" && !strings.HasSuffix(host, ".atlassian.net") {
		return nil, fmt.Errorf("Jira Data Center origins are unsupported; use a Jira Cloud site origin")
	}
	if mode != "capture" && mode != "refresh" && mode != "discover" {
		return nil, fmt.Errorf("unsupported Jira mode %q", mode)
	}
	if cp != nil {
		return append([]Request(nil), cp.Pending...), nil
	}
	if s.Scope.Query != "" {
		return nil, fmt.Errorf("Jira scope.query is unsupported; use explicit itemIds or the configured project scope")
	}
	if mode == "discover" {
		return []Request{{Method: "GET", Path: jiraPath(s, "/rest/api/3/project/"+url.PathEscape(s.ContainerKey)), Kind: "jira-project"}, {Method: "GET", Path: jiraPath(s, "/rest/api/3/issuetype/project?projectId="+url.QueryEscape(s.ContainerID)), Kind: "jira-types"}, jiraFieldsRequest(s, 0)}, nil
	}
	var metadata []Request
	if s.Scope.Projects {
		metadata = []Request{{Method: "GET", Path: jiraPath(s, "/rest/api/3/project/"+url.PathEscape(s.ContainerKey)), Kind: "jira-project"}, {Method: "GET", Path: jiraPath(s, "/rest/api/3/issuetype/project?projectId="+url.QueryEscape(s.ContainerID)), Kind: "jira-types"}, jiraFieldsRequest(s, 0)}
	}
	items := append([]string(nil), s.Scope.ItemIDs...)
	if len(items) > 0 {
		out := make([]Request, 0, len(items))
		for _, id := range items {
			if id == "" {
				return nil, fmt.Errorf("empty Jira item id")
			}
			out = append(out, Request{Method: "GET", Path: jiraPath(s, "/rest/api/3/issue/"+url.PathEscape(id)+"?expand=names"), Kind: "jira-item", ItemID: id})
		}
		return append(out, metadata...), nil
	}
	q := "project = \"" + strings.ReplaceAll(s.ContainerKey, "\"", "\\\"") + "\" ORDER BY updated DESC"
	path := "/rest/api/3/search/jql?jql=" + url.QueryEscape(q) + "&maxResults=50&fields=*all"
	return append([]Request{{Method: "GET", Path: jiraPath(s, path), Kind: "jira-search"}}, metadata...), nil
}

func (Jira) Decode(s Source, req Request, res Response) (PageResult, error) {
	if res.Status < 200 || res.Status >= 300 {
		return PageResult{}, fmt.Errorf("Jira returned HTTP %d", res.Status)
	}
	p := PageResult{Complete: true}
	if req.Kind == "jira-fields" {
		return jiraDecodeFields(s, req, res, p)
	}
	if req.Kind == "jira-types" {
		var types []map[string]any
		if err := usage.Decode(res.Body, &types); err != nil {
			return PageResult{}, err
		}
		p.Discovery = map[string]json.RawMessage{"types": append([]byte(nil), res.Body...)}
		if types == nil {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "types", Message: "Jira project issue types response must be a non-null array"})
			return p, nil
		}
		for i, issueType := range types {
			if issueType == nil {
				p.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: fmt.Sprintf("types.%d", i), Message: "Jira project issue type response contained a non-object entry"})
				continue
			}
			id := fmt.Sprint(issueType["id"])
			if id == "<nil>" || id == "" {
				p.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: fmt.Sprintf("types.%d.id", i), Message: "Jira project issue type omitted its ID"})
			}
		}
		return p, nil
	}
	var root map[string]any
	if err := usage.Decode(res.Body, &root); err != nil {
		return PageResult{}, err
	}
	if root == nil {
		p.Complete = false
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "response", Message: "Jira response must be an object"})
		return p, nil
	}
	if req.Kind == "jira-project" {
		id := fmt.Sprint(root["id"])
		key, _ := root["key"].(string)
		if id != s.ContainerID || key != s.ContainerKey {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "scope-identity-mismatch", Field: "project.id", Message: "Jira project metadata does not match the configured project ID and key"})
			return p, nil
		}
		p.Discovery = map[string]json.RawMessage{"project": append([]byte(nil), res.Body...)}
		return p, nil
	}
	if req.Kind == "jira-search" {
		if issues, ok := root["issues"].([]any); ok {
			for _, x := range issues {
				n, _ := x.(map[string]any)
				if it, ok := jiraItem(s, n); ok {
					p.Items = append(p.Items, it)
					p.Next = append(p.Next, jiraExtras(s, it.Identity.RemoteID)...)
				} else {
					p.Complete = false
					p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "scope-identity-mismatch", Field: "issues", Message: "Jira issue project identity did not match configured project"})
				}
			}
		} else {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "issues", Message: "Jira search response omitted an issues collection"})
		}
		if token, _ := root["nextPageToken"].(string); token != "" {
			u, _ := url.ParseRequestURI(req.Path)
			q := u.Query()
			q.Set("nextPageToken", token)
			u.RawQuery = q.Encode()
			p.Next = append(p.Next, Request{Method: "GET", Path: u.String(), Kind: "jira-search"})
		}
		return p, nil
	}
	if req.Kind == "jira-item" {
		if it, ok := jiraItem(s, root); ok && (req.ItemID == "" || it.Identity.RemoteID == req.ItemID || it.DisplayKey == req.ItemID) {
			p.Items = []Item{it}
			p.Next = append(p.Next, jiraExtras(s, it.Identity.RemoteID)...)
		} else {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "scope-identity-mismatch", Field: "project.id", Message: "Jira issue was missing or belongs to another project"})
		}
		return p, nil
	}
	if req.Kind == "jira-comments" || req.Kind == "jira-relations" || req.Kind == "jira-transitions" {
		item := NewItem(s, req.ItemID, "issue")
		item.Complete = true
		if req.Kind == "jira-comments" {
			if comments, ok := root["comments"].([]any); ok {
				for _, v := range comments {
					c, _ := v.(map[string]any)
					id, _ := c["id"].(string)
					body, _ := c["body"].(string)
					updated, _ := c["updated"].(string)
					text, loss := adfText(c["body"])
					if _, isADF := c["body"].(map[string]any); isADF {
						body = text
					}
					if loss {
						body = text
						item.LossyFields = append(item.LossyFields, "comments.body")
					}
					item.Comments = append(item.Comments, Comment{ID: id, Body: body, UpdatedAt: updated})
				}
				b, _ := json.Marshal(comments)
				item.Fields["commentsRaw"] = b
				start := jiraInt(root["startAt"])
				total := jiraInt(root["total"])
				count := jiraInt(root["maxResults"])
				if count > 0 && start+count < total {
					path := fmt.Sprintf("/rest/api/3/issue/%s/comment?startAt=%d&maxResults=%d", url.PathEscape(req.ItemID), start+count, count)
					p.Next = append(p.Next, Request{Method: "GET", Path: jiraPath(s, path), Kind: "jira-comments", ItemID: req.ItemID})
				} else if root["total"] == nil || root["maxResults"] == nil || root["startAt"] == nil {
					p.Complete = false
					p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "pagination-unknown", Field: "comments", Message: "Jira comment response omitted usable pagination metadata"})
				}
			} else {
				p.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "comments", Message: "Jira comment response omitted the comments collection"})
			}
		}
		if req.Kind == "jira-relations" {
			rootID, _ := root["id"].(string)
			fields, _ := root["fields"].(map[string]any)
			project, _ := fields["project"].(map[string]any)
			projectID := fmt.Sprint(project["id"])
			projectKey, _ := project["key"].(string)
			if rootID == "" || rootID != req.ItemID || projectID != s.ContainerID || projectKey != s.ContainerKey {
				p.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "scope-identity-mismatch", Field: "relations.identity", Message: "Jira relation response issue or project identity did not match configured scope"})
				return p, nil
			}
			item.Fields["relationsRaw"] = append([]byte(nil), res.Body...)
			if links, ok := fields["issuelinks"].([]any); ok {
				for _, v := range links {
					l, _ := v.(map[string]any)
					typ, _ := l["type"].(map[string]any)
					name, _ := typ["name"].(string)
					target, _ := l["outwardIssue"].(map[string]any)
					if target == nil {
						target, _ = l["inwardIssue"].(map[string]any)
					}
					targetID, _ := target["id"].(string)
					item.Relations = append(item.Relations, Relation{Type: name, TargetID: targetID, Supported: false})
				}
			}
		}
		if req.Kind == "jira-transitions" {
			item.Fields["transitionsRaw"] = append([]byte(nil), res.Body...)
			if ts, ok := root["transitions"].([]any); ok {
				for _, v := range ts {
					t, _ := v.(map[string]any)
					to, _ := t["to"].(map[string]any)
					id, _ := t["id"].(string)
					name, _ := to["name"].(string)
					required := []string{}
					if fs, ok := t["fields"].(map[string]any); ok {
						for field, x := range fs {
							f, _ := x.(map[string]any)
							if f["required"] == true {
								required = append(required, field)
							}
						}
					}
					_, known := t["fields"].(map[string]any)
					item.Transitions = append(item.Transitions, Transition{ID: id, Name: name, RequiredFields: required, RequirementsKnown: known})
				}
			} else {
				p.Complete = false
				item.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "transitions", Message: "Jira transition response omitted transitions"})
			}
		}
		p.Items = []Item{item}
		return p, nil
	}
	return p, nil
}

func jiraFieldsRequest(s Source, startAt int) Request {
	path := fmt.Sprintf("/rest/api/3/field/search?projectIds=%s&startAt=%d&maxResults=%d", url.QueryEscape(s.ContainerID), startAt, jiraFieldsPageSize)
	return Request{Method: "GET", Path: jiraPath(s, path), Kind: "jira-fields"}
}

func jiraDecodeFields(s Source, req Request, res Response, p PageResult) (PageResult, error) {
	u, err := url.ParseRequestURI(req.Path)
	if err != nil {
		return PageResult{}, fmt.Errorf("invalid Jira field catalog request")
	}
	query := u.Query()
	if u.Path != jiraPath(s, "/rest/api/3/field/search") || len(query) != 3 || len(query["projectIds"]) != 1 || len(query["startAt"]) != 1 || len(query["maxResults"]) != 1 {
		return PageResult{}, fmt.Errorf("Jira field catalog request does not match the fixed project-scoped endpoint")
	}
	startAt, err := strconv.Atoi(query.Get("startAt"))
	if err != nil || startAt < 0 || startAt > jiraFieldsMaxStart || query.Get("projectIds") != s.ContainerID || query.Get("maxResults") != strconv.Itoa(jiraFieldsPageSize) {
		return PageResult{}, fmt.Errorf("Jira field catalog request is outside the configured project or pagination bounds")
	}
	var root map[string]any
	if err := usage.Decode(res.Body, &root); err != nil {
		return PageResult{}, err
	}
	values, ok := root["values"].([]any)
	if !ok {
		p.Complete = false
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "fields.values", Message: "Jira project field search response omitted a non-null values array"})
		return p, nil
	}
	returnedStart, startOK := root["startAt"].(json.Number)
	returnedMax, maxOK := root["maxResults"].(json.Number)
	total, totalOK := root["total"].(json.Number)
	isLast, lastOK := root["isLast"].(bool)
	startNumber, startErr := returnedStart.Int64()
	maxNumber, maxErr := returnedMax.Int64()
	totalNumber, totalErr := total.Int64()
	if !startOK || !maxOK || !totalOK || !lastOK || startErr != nil || maxErr != nil || totalErr != nil || startNumber != int64(startAt) || maxNumber < 1 || maxNumber > jiraFieldsPageSize || totalNumber < 0 || int64(len(values)) > maxNumber {
		p.Complete = false
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "pagination-unknown", Field: "fields.page", Message: "Jira field search response omitted valid page metadata"})
		return p, nil
	}
	p.Discovery = map[string]json.RawMessage{fmt.Sprintf("fields:%d", startAt): append([]byte(nil), res.Body...)}
	if int64(startAt)+maxNumber > int64(jiraFieldsMaxStart) {
		return PageResult{}, fmt.Errorf("Jira field catalog pagination exceeds configured offset bounds")
	}
	nextStart := startAt + int(maxNumber)
	if !isLast {
		if len(values) == 0 || nextStart <= startAt || nextStart > jiraFieldsMaxStart || int64(nextStart) >= totalNumber {
			return PageResult{}, fmt.Errorf("Jira field catalog pagination did not advance within configured bounds")
		}
		p.Next = append(p.Next, jiraFieldsRequest(s, nextStart))
	}
	return p, nil
}

func jiraExtras(s Source, id string) []Request {
	var out []Request
	prefix := "/rest/api/3/issue/" + url.PathEscape(id)
	if s.Scope.Comments {
		out = append(out, Request{Method: "GET", Path: jiraPath(s, prefix+"/comment"), Kind: "jira-comments", ItemID: id})
	}
	if s.Scope.Relations {
		out = append(out, Request{Method: "GET", Path: jiraPath(s, prefix+"?fields=issuelinks,project"), Kind: "jira-relations", ItemID: id})
	}
	out = append(out, Request{Method: "GET", Path: jiraPath(s, prefix+"/transitions?expand=transitions.fields"), Kind: "jira-transitions", ItemID: id})
	return out
}

func jiraPath(s Source, p string) string {
	u, _ := url.Parse(s.Origin)
	if strings.EqualFold(u.Hostname(), "api.atlassian.com") {
		return "/ex/jira/" + url.PathEscape(s.NativeTenantID) + p
	}
	return p
}
func jiraInt(v any) int64 {
	n, ok := v.(json.Number)
	if !ok {
		return 0
	}
	i, _ := n.Int64()
	return i
}

func jiraItem(s Source, n map[string]any) (Item, bool) {
	if n == nil {
		return Item{}, false
	}
	id, _ := n["id"].(string)
	key, _ := n["key"].(string)
	fields, _ := n["fields"].(map[string]any)
	project, _ := fields["project"].(map[string]any)
	pid := fmt.Sprint(project["id"])
	pkey, _ := project["key"].(string)
	if id == "" || pid != s.ContainerID || pkey != s.ContainerKey {
		return Item{}, false
	}
	it := NewItem(s, id, "issue")
	it.Complete = true
	it.DisplayKey = key
	if u, _ := n["self"].(string); u != "" {
		it.URL = u
	}
	if u, _ := fields["updated"].(string); u != "" {
		it.UpdatedAt = u
	}
	for k, v := range fields {
		b, _ := json.Marshal(v)
		it.Fields[k] = b
	}
	it.Revision = Digest(n)
	it.RevisionKind = "observation-fingerprint"
	if desc, ok := fields["description"]; ok {
		txt, loss := adfText(desc)
		b, _ := json.Marshal(txt)
		it.Fields["descriptionText"] = b
		if loss {
			it.LossyFields = append(it.LossyFields, "description", "descriptionText")
		}
	}
	return it, true
}

func adfText(v any) (string, bool) {
	var out strings.Builder
	loss := false
	var walk func(any)
	walk = func(x any) {
		switch n := x.(type) {
		case map[string]any:
			t, _ := n["type"].(string)
			switch t {
			case "doc", "paragraph", "text", "bulletList", "orderedList", "listItem", "heading", "codeBlock", "hardBreak":
			default:
				if t != "" {
					loss = true
				}
			}
			if t == "text" {
				if z, ok := n["text"].(string); ok {
					out.WriteString(z)
				}
				if marks, ok := n["marks"].([]any); ok && len(marks) > 0 {
					loss = true // Flattening discards formatting and link attributes.
				}
			}
			if t == "hardBreak" {
				out.WriteByte('\n')
			}
			if c, ok := n["content"].([]any); ok {
				for _, child := range c {
					walk(child)
				}
				if t == "paragraph" || t == "heading" || t == "listItem" || t == "codeBlock" {
					out.WriteByte('\n')
				}
			}
			if t == "mention" || t == "inlineCard" {
				loss = true // Identity, target, or other card attributes remain only in raw ADF.
				if attrs, ok := n["attrs"].(map[string]any); ok {
					if z, ok := attrs["text"].(string); ok {
						out.WriteString(z)
					}
				}
			}
		default:
		}
	}
	walk(v)
	return strings.TrimSpace(out.String()), loss
}
