package tracker

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

// JiraDataCenter is deliberately separate from the Cloud adapter. Its API 2
// pagination and wiki text are never treated as Cloud continuation or ADF.
type JiraDataCenter struct{}

func (JiraDataCenter) Provider() string { return "jira-datacenter" }
func workflowVersion(s Source) string {
	if s.Version == Version2 {
		return "aok/v1alpha2"
	}
	return "aok/v1alpha1"
}
func dcPath(s Source, suffix string) string { return s.Deployment.BasePath + "/rest/api/2/" + suffix }
func dcSearch(s Source, start int) Request {
	q := url.Values{"jql": {`project = "` + s.ContainerKey + `" ORDER BY updated DESC`}, "startAt": {strconv.Itoa(start)}, "maxResults": {"50"}, "fields": {"*all"}}
	return Request{Method: "GET", Path: dcPath(s, "search") + "?" + q.Encode(), Kind: "jira-dc-search"}
}
func (JiraDataCenter) Initial(s Source, mode string, cp *Checkpoint) ([]Request, error) {
	if e := ValidateSource(s); e != nil {
		return nil, e
	}
	if s.Provider != "jira-datacenter" || s.Version != Version2 || !safeID.MatchString(s.ContainerKey) || s.Scope.Query != "" {
		return nil, fmt.Errorf("explicit Data Center instance/project scope required; arbitrary queries unsupported")
	}
	if cp != nil {
		return nil, fmt.Errorf("Data Center continuation requires captured root replay")
	}
	metadata := []Request{{Method: "GET", Path: dcPath(s, "project/"+s.ContainerKey), Kind: "jira-dc-project"}}
	if mode == "discover" {
		return metadata, nil
	}
	if mode != "capture" {
		return nil, fmt.Errorf("unsupported Data Center mode")
	}
	var out []Request
	if len(s.Scope.ItemIDs) == 0 {
		out = append(out, dcSearch(s, 0))
	} else {
		for _, id := range s.Scope.ItemIDs {
			out = append(out, Request{Method: "GET", Path: dcPath(s, "issue/"+id) + "?expand=names", Kind: "jira-dc-item", ItemID: id})
		}
	}
	if s.Scope.Projects {
		out = append(out, metadata...)
	}
	return out, nil
}
func validateDCEndpoint(s Source, r Request, u *url.URL) error {
	if s.Deployment == nil || r.Method != "GET" {
		return fmt.Errorf("Data Center read-only API 2 required")
	}
	for _, vs := range u.Query() {
		if len(vs) != 1 {
			return fmt.Errorf("duplicate Data Center parameter")
		}
	}
	allowed := false
	switch r.Kind {
	case "jira-dc-project":
		allowed = u.Path == dcPath(s, "project/"+s.ContainerKey) && len(u.Query()) == 0
	case "jira-dc-search":
		n, e := strconv.Atoi(u.Query().Get("startAt"))
		expected, _ := url.Parse(dcSearch(s, n).Path)
		allowed = e == nil && n >= 0 && n <= 10000 && u.Path == expected.Path && u.Query().Encode() == expected.Query().Encode() && len(s.Scope.ItemIDs) == 0
	case "jira-dc-item":
		allowed = safeID.MatchString(r.ItemID) && u.Path == dcPath(s, "issue/"+r.ItemID) && u.Query().Encode() == "expand=names"
		if len(s.Scope.ItemIDs) > 0 && !has(s.Scope.ItemIDs, r.ItemID) {
			allowed = false
		}
	case "jira-dc-comments":
		n, e := strconv.Atoi(u.Query().Get("startAt"))
		expected, _ := url.Parse(dcComments(s, r.ItemID, n).Path)
		allowed = e == nil && n >= 0 && n <= 10000 && s.Scope.Comments && r.baseScopeValidated && safeID.MatchString(r.ItemID) && u.Path == expected.Path && u.Query().Encode() == expected.Query().Encode()
	case "jira-dc-transitions":
		allowed = safeID.MatchString(r.ItemID) && u.Path == dcPath(s, "issue/"+r.ItemID+"/transitions") && u.Query().Encode() == "expand=transitions.fields" && r.baseScopeValidated
	}
	if !allowed {
		return fmt.Errorf("outside exact Data Center read plan")
	}
	return nil
}
func (JiraDataCenter) Decode(s Source, req Request, res Response) (PageResult, error) {
	p := PageResult{Complete: true}
	if res.Status != 200 {
		return p, fmt.Errorf("Data Center HTTP %d", res.Status)
	}
	var root map[string]json.RawMessage
	if e := usage.Decode(res.Body, &root); e != nil || root == nil {
		return p, fmt.Errorf("Data Center object required")
	}
	if req.Kind == "jira-dc-project" {
		var id, key string
		_ = json.Unmarshal(root["id"], &id)
		_ = json.Unmarshal(root["key"], &key)
		if id != s.ContainerID || key != s.ContainerKey {
			return p, fmt.Errorf("Data Center project scope mismatch")
		}
		p.Discovery = map[string]json.RawMessage{"project": append([]byte(nil), res.Body...)}
		return p, nil
	}

	if req.Kind == "jira-dc-transitions" {
		var rows []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Fields map[string]struct {
				Required bool `json:"required"`
			} `json:"fields"`
		}
		if root["transitions"] == nil || json.Unmarshal(root["transitions"], &rows) != nil || rows == nil {
			return p, fmt.Errorf("Data Center transitions metadata missing")
		}
		item := NewItem(s, req.ItemID, "issue")
		item.Complete = true
		item.Fields["transitionsRaw"] = append([]byte(nil), res.Body...)
		item.Provenance["transitionsRaw"] = []Pointer{{Hash(res.Body), ""}}
		for _, r := range rows {
			if r.ID == "" {
				return p, fmt.Errorf("transition ID missing")
			}
			required := []string{}
			for id, f := range r.Fields {
				if f.Required {
					required = append(required, id)
				}
			}
			sort.Strings(required)
			item.Transitions = append(item.Transitions, Transition{ID: r.ID, Name: r.Name, RequiredFields: required, RequirementsKnown: r.Fields != nil})
		}
		p.Items = []Item{item}
		return p, nil
	}
	if req.Kind == "jira-dc-comments" {
		var page struct {
			StartAt  int `json:"startAt"`
			Total    int `json:"total"`
			Comments []struct {
				ID        string          `json:"id"`
				Body      json.RawMessage `json:"body"`
				UpdatedAt string          `json:"updated"`
			} `json:"comments"`
		}
		if json.Unmarshal(res.Body, &page) != nil || page.Comments == nil || root["startAt"] == nil || root["total"] == nil {
			return p, fmt.Errorf("Data Center comments pagination missing")
		}
		u, _ := url.Parse(req.Path)
		expected, _ := strconv.Atoi(u.Query().Get("startAt"))
		if page.StartAt != expected || page.Total < 0 || len(page.Comments) > 50 {
			return p, fmt.Errorf("Data Center comments offset mismatch")
		}
		item := NewItem(s, req.ItemID, "issue")
		item.Complete = true
		field := "commentsRaw/" + strconv.Itoa(page.StartAt)
		item.Fields[field] = append([]byte(nil), res.Body...)
		item.Provenance[field] = []Pointer{{Hash(res.Body), ""}}
		for _, c := range page.Comments {
			if c.ID == "" {
				return p, fmt.Errorf("comment ID missing")
			}
			var wiki string
			if json.Unmarshal(c.Body, &wiki) != nil {
				return p, fmt.Errorf("unsupported Data Center comment body format")
			}
			item.Comments = append(item.Comments, Comment{ID: c.ID, Body: wiki, UpdatedAt: c.UpdatedAt})
		}
		item.LossyFields = append(item.LossyFields, "comments.body")
		next := page.StartAt + len(page.Comments)
		if next < page.Total {
			if len(page.Comments) == 0 {
				item.Complete = false
				p.Complete = false
			} else {
				p.Next = append(p.Next, dcComments(s, req.ItemID, next))
			}
		}
		p.Items = []Item{item}
		return p, nil
	}
	var rows []map[string]any
	if req.Kind == "jira-dc-search" {
		var page struct {
			StartAt    int               `json:"startAt"`
			MaxResults int               `json:"maxResults"`
			Total      int               `json:"total"`
			Issues     []json.RawMessage `json:"issues"`
		}
		// Provider response fields are open; exact bytes remain captured.
		if e := json.Unmarshal(res.Body, &page); e != nil || root["issues"] == nil || root["total"] == nil || root["startAt"] == nil || page.Issues == nil {
			return p, fmt.Errorf("Data Center search required fields drifted")
		}
		u, _ := url.Parse(req.Path)
		expected, _ := strconv.Atoi(u.Query().Get("startAt"))
		if page.StartAt != expected || page.Total < 0 || len(page.Issues) > 50 {
			return p, fmt.Errorf("invalid Data Center pagination")
		}
		for _, rawIssue := range page.Issues {
			var row map[string]any
			if e := usage.Decode(rawIssue, &row); e != nil {
				return p, e
			}
			rows = append(rows, row)
		}
		next := page.StartAt + len(rows)
		if next < page.Total {
			if len(rows) == 0 {
				p.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{"incomplete", "search", "empty page before total"})
			} else {
				p.Next = append(p.Next, dcSearch(s, next))
			}
		}
	} else if req.Kind == "jira-dc-item" {
		var row map[string]any
		if e := usage.Decode(res.Body, &row); e != nil {
			return p, e
		}
		rows = []map[string]any{row}
	} else {
		return p, fmt.Errorf("unsupported Data Center response kind")
	}
	for _, row := range rows {
		item, ok := jiraItem(s, row)
		if !ok {
			return p, fmt.Errorf("Data Center item scope mismatch")
		}
		if req.Kind == "jira-dc-item" && item.Identity.RemoteID != req.ItemID && item.DisplayKey != req.ItemID {
			return p, fmt.Errorf("Data Center selected item mismatch")
		}
		// Retain wiki source exactly. Rendering it as plain text would be lossy.
		delete(item.Fields, "descriptionText")
		item.LossyFields = nil
		if d, exists := item.Fields["description"]; exists && string(d) != "null" {
			item.LossyFields = append(item.LossyFields, "description")
		}

		if s.Scope.Comments {
			p.Next = append(p.Next, dcComments(s, item.Identity.RemoteID, 0))
		}
		if s.Scope.Relations {
			fields, _ := row["fields"].(map[string]any)
			links, _ := fields["issuelinks"].([]any)
			for _, raw := range links {
				link, _ := raw.(map[string]any)
				typ, _ := link["type"].(map[string]any)
				target, _ := link["outwardIssue"].(map[string]any)
				direction := "outward"
				if target == nil {
					target, _ = link["inwardIssue"].(map[string]any)
					direction = "inward"
				}
				tid, _ := target["id"].(string)
				nativeType, _ := typ["id"].(string)
				item.Relations = append(item.Relations, Relation{Type: nativeType + ":" + direction, TargetID: tid, Supported: false})
			}
		}
		p.Items = append(p.Items, item)
		p.Next = append(p.Next, Request{Method: "GET", Path: dcPath(s, "issue/"+item.Identity.RemoteID+"/transitions") + "?expand=transitions.fields", Kind: "jira-dc-transitions", ItemID: item.Identity.RemoteID, baseScopeValidated: true})
	}
	return p, nil
}

// TLS trust is an externally pinned transport prerequisite. Capturing a source
// pin does not install trust roots or certify a live route.
func dataCenterScope(s Source) string {
	return strings.TrimSuffix(s.Origin, "/") + s.Deployment.BasePath
}

func dcComments(s Source, id string, start int) Request {
	return Request{Method: "GET", Path: dcPath(s, "issue/"+id+"/comment") + "?maxResults=50&startAt=" + strconv.Itoa(start), Kind: "jira-dc-comments", ItemID: id, baseScopeValidated: true}
}
