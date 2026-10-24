package tracker

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"net/url"
	"strconv"
	"strings"
)

type GitHub struct{}

func NewGitHub() Adapter        { return GitHub{} }
func (GitHub) Provider() string { return "github" }
func githubRepo(s Source) (string, string, error) {
	parts := strings.Split(s.ContainerKey, "/")
	if len(parts) != 2 || !safeID.MatchString(parts[0]) || !safeID.MatchString(parts[1]) {
		return "", "", fmt.Errorf("GitHub containerKey must be OWNER/REPO")
	}
	if s.APIVersion != "2022-11-28" && s.APIVersion != "2026-03-10" {
		return "", "", fmt.Errorf("unsupported GitHub API version")
	}
	return parts[0], parts[1], nil
}
func (GitHub) Initial(s Source, mode string, cp *Checkpoint) ([]Request, error) {
	owner, repo, e := githubRepo(s)
	if e != nil {
		return nil, e
	}
	root := "/repos/" + owner + "/" + repo
	if cp != nil && len(cp.Pending) > 0 {
		return append([]Request(nil), cp.Pending...), nil
	}
	if mode != "capture" && mode != "discover" {
		return nil, fmt.Errorf("invalid GitHub mode")
	}
	requests := []Request{{Method: "GET", Path: root, Kind: "github-repository"}}
	if s.Scope.Projects {
		requests = append(requests, githubProjectsRequest(owner, repo, ""))
	}
	if mode == "discover" {
		return requests, nil
	}
	if s.Scope.Query != "" {
		return nil, fmt.Errorf("GitHub v1 capture uses repository issues or explicit IDs; arbitrary search unsupported")
	}
	if len(s.Scope.ItemIDs) == 0 {
		requests = append(requests, Request{Method: "GET", Path: root + "/issues?state=all&sort=updated&direction=asc&per_page=100", Kind: "github-issues"})
		return requests, nil
	}
	seen := map[string]bool{}
	for _, raw := range s.Scope.ItemIDs {
		if seen[raw] {
			continue
		}
		seen[raw] = true
		kind, num := "issue", raw
		parts := strings.Split(raw, ":")
		if len(parts) == 2 {
			kind, num = parts[0], parts[1]
		}
		n, e := strconv.Atoi(num)
		if e != nil || n < 1 || kind != "issue" && kind != "pr" {
			return nil, fmt.Errorf("GitHub item IDs must be issue:NUMBER, pr:NUMBER or NUMBER")
		}
		path := root + "/issues/" + num
		if kind == "pr" {
			path = root + "/pulls/" + num
		}
		requests = append(requests, Request{Method: "GET", Path: path, Kind: "github-item", ItemID: raw})
	}
	return requests, nil
}

const ghProjects = `query AOKProjects($owner:String!,$repo:String!,$after:String){repository(owner:$owner,name:$repo){id projectsV2(first:20,after:$after){nodes{id title}pageInfo{hasNextPage endCursor}}}}`
const ghProjectFields = `query AOKProjectFields($id:ID!,$after:String){node(id:$id){...on ProjectV2{id fields(first:100,after:$after){nodes{__typename ...on ProjectV2Field{id name dataType} ...on ProjectV2SingleSelectField{id name options{id name}} ...on ProjectV2IterationField{id name}}pageInfo{hasNextPage endCursor}}}}}`
const ghProjectItems = `query AOKProjectItems($id:ID!,$after:String){node(id:$id){...on ProjectV2{id items(first:100,after:$after){nodes{id content{...on Issue{id number} ...on PullRequest{id number}}}pageInfo{hasNextPage endCursor}}}}}`
const ghItemFields = `query AOKItemFields($id:ID!,$after:String){node(id:$id){...on ProjectV2Item{id fieldValues(first:100,after:$after){nodes{__typename ...on ProjectV2ItemFieldSingleSelectValue{name optionId field{...on ProjectV2SingleSelectField{id name}}} ...on ProjectV2ItemFieldTextValue{text field{...on ProjectV2Field{id name}}} ...on ProjectV2ItemFieldNumberValue{number field{...on ProjectV2Field{id name}}}}pageInfo{hasNextPage endCursor}}}}}`

func ghGraph(kind, q string, vars map[string]any) Request {
	body, _ := json.Marshal(map[string]any{"query": q, "variables": vars})
	return Request{Method: "POST", Path: "/graphql", Body: body, Kind: kind}
}
func githubProjectsRequest(owner, repo, after string) Request {
	return ghGraph("github-projects", ghProjects, map[string]any{"owner": owner, "repo": repo, "after": after})
}
func (GitHub) Decode(s Source, req Request, resp Response) (PageResult, error) {
	if e := contracts.ValidateJSON(resp.Body); e != nil {
		return PageResult{}, e
	}
	return decodeGithub(s, req, resp)
}
func decodeGithub(s Source, req Request, resp Response) (PageResult, error) {
	out := PageResult{Complete: true, Items: []Item{}, Discovery: map[string]json.RawMessage{}}
	if req.Method == "POST" {
		return githubGraphDecode(s, req, resp)
	}
	if req.Kind == "github-repository" {
		m, e := DecodeObject(resp.Body)
		if e != nil {
			return out, e
		}
		var owner map[string]json.RawMessage
		_ = usage.Decode(m["owner"], &owner)
		if ghString(m, "node_id") != s.ContainerID || ghString(owner, "node_id") != s.NativeTenantID {
			return out, fmt.Errorf("GitHub immutable repository/owner identity mismatch")
		}
		out.Discovery["repository"] = append([]byte(nil), resp.Body...)
		return out, nil
	}
	if req.Kind == "github-comments" || req.Kind == "github-timeline" {
		var rows []map[string]json.RawMessage
		if e := usage.Decode(resp.Body, &rows); e != nil || rows == nil {
			return out, fmt.Errorf("GitHub collection missing or invalid")
		}
		item := NewItem(s, req.ItemID, "")
		item.Complete = true
		for _, r := range rows {
			if req.Kind == "github-comments" {
				item.Comments = append(item.Comments, Comment{ID: ghID(r, "node_id", "id"), Body: ghString(r, "body"), UpdatedAt: ghString(r, "updated_at")})
			} else {
				var src map[string]json.RawMessage
				_ = usage.Decode(r["source"], &src)
				var issue map[string]json.RawMessage
				_ = usage.Decode(src["issue"], &issue)
				target := ghID(issue, "node_id", "id")
				if target != "" {
					item.Relations = append(item.Relations, Relation{Type: ghString(r, "event"), TargetID: target, URL: ghString(issue, "html_url"), Supported: false})
				}
			}
		}
		out.Items = append(out.Items, item)
	} else {
		rows := []map[string]json.RawMessage{}
		if req.Kind == "github-issues" {
			if e := usage.Decode(resp.Body, &rows); e != nil || rows == nil {
				return out, fmt.Errorf("GitHub issue collection missing or invalid")
			}
		} else {
			m, e := DecodeObject(resp.Body)
			if e != nil {
				return out, e
			}
			rows = append(rows, m)
		}
		for _, r := range rows {
			number := ghID(r, "number")
			if number == "" {
				return out, fmt.Errorf("GitHub item number missing")
			}
			kind := "issue"
			if _, pr := r["pull_request"]; pr {
				if strings.HasPrefix(req.ItemID, "issue:") {
					return out, fmt.Errorf("GitHub requested issue is a pull request; typed scope mismatch")
				}
				out.Next = append(out.Next, Request{Method: "GET", Path: "/repos/" + s.ContainerKey + "/pulls/" + number, Kind: "github-item", ItemID: "pr:" + number})
				continue
			}
			if strings.Contains(req.Path, "/pulls/") {
				kind = "pull-request"
			}
			id := ghID(r, "node_id")
			if id == "" {
				return out, fmt.Errorf("GitHub immutable node ID missing")
			}
			item := NewItem(s, id, kind)
			item.Complete = true
			item.DisplayKey = s.ContainerKey + "#" + number
			item.URL = ghString(r, "html_url")
			item.UpdatedAt = ghString(r, "updated_at")
			item.Fields = r
			item.Revision = Digest(r)
			item.RevisionKind = "observation-fingerprint"
			if req.ItemID != "" {
				n := strings.TrimPrefix(strings.TrimPrefix(req.ItemID, "issue:"), "pr:")
				if n != number {
					return out, fmt.Errorf("GitHub requested item mismatch")
				}
			}
			if raw, ok := r["repository_url"]; ok {
				var actual string
				_ = usage.Decode(raw, &actual)
				expected := strings.TrimSuffix(s.Origin, "/") + "/repos/" + s.ContainerKey
				if s.Origin == "https://github.com" {
					expected = "https://api.github.com/repos/" + s.ContainerKey
				}
				if actual != expected {
					return out, fmt.Errorf("GitHub item outside repository")
				}
			}
			out.Items = append(out.Items, item)
			base := "/repos/" + s.ContainerKey + "/issues/" + number
			if s.Scope.Comments {
				out.Next = append(out.Next, Request{Method: "GET", Path: base + "/comments?per_page=100", Kind: "github-comments", ItemID: id, baseScopeValidated: true})
			}
			if s.Scope.Relations {
				out.Next = append(out.Next, Request{Method: "GET", Path: base + "/timeline?per_page=100", Kind: "github-timeline", ItemID: id, baseScopeValidated: true})
			}
		}
	}
	if link := resp.Headers["link"]; link != "" {
		next, e := githubNext(s, link)
		if e != nil {
			return out, e
		}
		if next != "" {
			r := req
			r.Path = next
			out.Next = append(out.Next, r)
		}
	}
	return out, nil
}
func ghString(m map[string]json.RawMessage, key string) string {
	var v string
	_ = usage.Decode(m[key], &v)
	return v
}
func ghID(m map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		raw := m[k]
		var s string
		if usage.Decode(raw, &s) == nil && s != "" {
			return s
		}
		var n json.Number
		if usage.Decode(raw, &n) == nil && n != "" {
			return string(n)
		}
	}
	return ""
}
func githubNext(s Source, link string) (string, error) {
	for _, part := range strings.Split(link, ",") {
		if !strings.Contains(part, `rel="next"`) {
			continue
		}
		start, end := strings.Index(part, "<"), strings.Index(part, ">")
		if start < 0 || end <= start {
			return "", fmt.Errorf("malformed pagination link")
		}
		u, e := url.Parse(part[start+1 : end])
		if e != nil {
			return "", e
		}
		base, _ := url.Parse(s.Origin)
		host := base.Host
		if host == "github.com" {
			host = "api.github.com"
		}
		if u.Scheme != "https" || u.Host != host || u.User != nil || u.Fragment != "" {
			return "", fmt.Errorf("pagination link escapes origin")
		}
		return u.RequestURI(), nil
	}
	return "", nil
}
func githubGraphDecode(s Source, req Request, resp Response) (PageResult, error) {
	out := PageResult{Complete: true, Discovery: map[string]json.RawMessage{}}
	var doc map[string]json.RawMessage
	if e := usage.Decode(resp.Body, &doc); e != nil {
		return out, e
	}
	var errs []json.RawMessage
	_ = usage.Decode(doc["errors"], &errs)
	if len(errs) > 0 {
		return PageResult{Complete: false, Diagnostics: []Diagnostic{{"graphql-errors", "response.errors", "GitHub GraphQL partial data cannot establish complete discovery"}}}, nil
	}
	var data map[string]json.RawMessage
	if e := usage.Decode(doc["data"], &data); e != nil {
		return out, e
	}
	key := "node"
	if req.Kind == "github-projects" {
		key = "repository"
	}
	var root map[string]json.RawMessage
	if e := usage.Decode(data[key], &root); e != nil || root == nil {
		return out, fmt.Errorf("GitHub GraphQL resource unavailable")
	}
	if key == "repository" && ghString(root, "id") != s.ContainerID {
		return out, fmt.Errorf("GitHub Projects repository scope mismatch")
	}
	if key == "node" {
		var b struct {
			Query     string                     `json:"query"`
			Variables map[string]json.RawMessage `json:"variables"`
		}
		if usage.Decode(req.Body, &b) != nil || ghString(root, "id") != ghString(b.Variables, "id") {
			return out, fmt.Errorf("GitHub project node scope mismatch")
		}
	}
	connection := map[string]string{"github-projects": "projectsV2", "github-project-fields": "fields", "github-project-items": "items", "github-item-fields": "fieldValues"}[req.Kind]
	if connection == "" {
		return out, fmt.Errorf("unsupported GitHub query kind")
	}
	var conn struct {
		Nodes    []map[string]json.RawMessage `json:"nodes"`
		PageInfo struct {
			HasNext bool   `json:"hasNextPage"`
			Cursor  string `json:"endCursor"`
		} `json:"pageInfo"`
	}
	shape, shapeErr := DecodeObject(root[connection])
	if shapeErr != nil || usage.Decode(root[connection], &conn) != nil || conn.Nodes == nil {
		return out, fmt.Errorf("GitHub GraphQL connection missing or invalid")
	}
	info, infoErr := DecodeObject(shape["pageInfo"])
	var hasNext bool
	if infoErr != nil || usage.Decode(info["hasNextPage"], &hasNext) != nil || string(info["hasNextPage"]) == "null" {
		return out, fmt.Errorf("GitHub GraphQL pageInfo missing or invalid")
	}
	out.Discovery[req.Kind+":"+ghString(root, "id")+":"+Hash(req.Body)] = append([]byte(nil), root[connection]...)
	for _, n := range conn.Nodes {
		id := ghString(n, "id")
		if (req.Kind == "github-projects" || req.Kind == "github-project-items") && id == "" {
			return out, fmt.Errorf("GitHub project entity identity missing")
		}
		if req.Kind == "github-projects" {
			for _, child := range []struct{ kind, query string }{{"github-project-fields", ghProjectFields}, {"github-project-items", ghProjectItems}} {
				kind, q := child.kind, child.query
				out.Next = append(out.Next, ghGraph(kind, q, map[string]any{"id": id, "after": ""}))
			}
		}
		if req.Kind == "github-project-items" {
			out.Next = append(out.Next, ghGraph("github-item-fields", ghItemFields, map[string]any{"id": id, "after": ""}))
		}
	}
	if conn.PageInfo.HasNext {
		if conn.PageInfo.Cursor == "" {
			return out, fmt.Errorf("GitHub pagination cursor missing")
		}
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if e := usage.Decode(req.Body, &body); e != nil {
			return out, e
		}
		if old, _ := body.Variables["after"].(string); old == conn.PageInfo.Cursor {
			return out, fmt.Errorf("GitHub cursor did not advance")
		}
		body.Variables["after"] = conn.PageInfo.Cursor
		b, _ := json.Marshal(body)
		next := req
		next.Body = b
		out.Next = append(out.Next, next)
	}
	return out, nil
}
