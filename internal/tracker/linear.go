package tracker

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

// Linear is a read-only GraphQL adapter. Every operation is a fixed query.
type Linear struct{}

func NewLinear() Adapter        { return Linear{} }
func (Linear) Provider() string { return "linear" }

const linearIssuesQuery = `query AOKIssues($teamId: ID!, $after: String) { organization { id } issues(filter: {team: {id: {eq: $teamId}}}, first: 50, after: $after) { nodes { id identifier url title description priority state { id name } assignee { id name } creator { id name } team { id key name } project { id name } labels { nodes { id name } pageInfo { hasNextPage endCursor } } createdAt updatedAt } pageInfo { hasNextPage endCursor } } }`
const linearExplicitIssueQuery = `query AOKIssue($issueId: ID!) { organization { id } issue(id: $issueId) { id identifier url title description priority state { id name } assignee { id name } creator { id name } team { id key name } project { id name } labels { nodes { id name } pageInfo { hasNextPage endCursor } } createdAt updatedAt } }`
const linearStatesQuery = `query AOKStates($teamId: ID!, $after: String) { organization { id } team(id: $teamId) { id states(first: 50, after: $after) { nodes { id name type } pageInfo { hasNextPage endCursor } } } }`
const linearLabelsQuery = `query AOKLabels($teamId: ID!, $after: String) { organization { id } team(id: $teamId) { id labels(first: 50, after: $after) { nodes { id name } pageInfo { hasNextPage endCursor } } } }`
const linearIssueLabelsQuery = `query AOKIssueLabels($issueId: ID!, $after: String) { organization { id } issue(id: $issueId) { id team { id } labels(first: 50, after: $after) { nodes { id name } pageInfo { hasNextPage endCursor } } } }`
const linearCommentsQuery = `query AOKComments($issueId: ID!, $after: String) { organization { id } issue(id: $issueId) { id team { id } comments(first: 50, after: $after) { nodes { id body updatedAt } pageInfo { hasNextPage endCursor } } } }`
const linearRelationsQuery = `query AOKRelations($issueId: ID!, $after: String) { organization { id } issue(id: $issueId) { id team { id } relations(first: 50, after: $after) { nodes { id type relatedIssue { id identifier url } } pageInfo { hasNextPage endCursor } } } }`
const linearMetadataQuery = `query AOKMetadata($teamId: ID!) { organization { id } team(id: $teamId) { id key name states(first: 50) { nodes { id name type } pageInfo { hasNextPage endCursor } } labels(first: 50) { nodes { id name } pageInfo { hasNextPage endCursor } } } }`

func (Linear) Initial(s Source, mode string, cp *Checkpoint) ([]Request, error) {
	if s.Provider != "linear" || s.Origin != "https://api.linear.app" || s.APIVersion != "graphql" || s.ContainerID == "" || s.NativeTenantID == "" {
		return nil, fmt.Errorf("invalid Linear source: expected api.linear.app, graphql, workspace and team ids")
	}
	if mode != "capture" && mode != "refresh" && mode != "discover" {
		return nil, fmt.Errorf("unsupported Linear mode %q", mode)
	}
	if cp != nil {
		return append([]Request(nil), cp.Pending...), nil
	}
	if s.Scope.Query != "" {
		return nil, fmt.Errorf("Linear scope.query is unsupported; use explicit itemIds or the configured team scope")
	}
	if mode == "discover" {
		b, _ := json.Marshal(map[string]any{"query": linearMetadataQuery, "variables": map[string]any{"teamId": s.ContainerID}})
		return []Request{{Method: "POST", Path: "/graphql", Body: b, Kind: "linear-metadata"}}, nil
	}
	if len(s.Scope.ItemIDs) > 0 {
		out := make([]Request, 0, len(s.Scope.ItemIDs))
		for _, id := range s.Scope.ItemIDs {
			b, _ := json.Marshal(map[string]any{"query": linearExplicitIssueQuery, "variables": map[string]any{"issueId": id}})
			out = append(out, Request{Method: "POST", Path: "/graphql", Body: b, Kind: "linear-item", ItemID: id})
		}
		return out, nil
	}
	body, _ := json.Marshal(map[string]any{"query": linearIssuesQuery, "variables": map[string]any{"teamId": s.ContainerID}})
	reqs := []Request{{Method: "POST", Path: "/graphql", Body: body, Kind: "linear-items"}}
	if s.Scope.Projects {
		b, _ := json.Marshal(map[string]any{"query": linearMetadataQuery, "variables": map[string]any{"teamId": s.ContainerID}})
		reqs = append(reqs, Request{Method: "POST", Path: "/graphql", Body: b, Kind: "linear-metadata"})
	}
	return reqs, nil
}

func (Linear) Decode(s Source, req Request, res Response) (PageResult, error) {
	if res.Status < 200 || res.Status >= 300 {
		return PageResult{}, fmt.Errorf("Linear returned HTTP %d", res.Status)
	}
	var root map[string]any
	if err := usage.Decode(res.Body, &root); err != nil {
		return PageResult{}, err
	}
	data, _ := root["data"].(map[string]any)
	if errs, ok := root["errors"].([]any); ok && len(errs) > 0 {
		return PageResult{Diagnostics: []Diagnostic{{Code: "graphql-errors", Field: "response.errors", Message: fmt.Sprintf("Linear returned %d GraphQL error(s); partial data was rejected", len(errs))}}, Complete: false}, nil
	}
	p := PageResult{Complete: true, Discovery: map[string]json.RawMessage{}}
	org, _ := data["organization"].(map[string]any)
	workspaceID, _ := org["id"].(string)
	if workspaceID != s.NativeTenantID {
		p.Complete = false
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "tenant-identity-mismatch", Field: "organization.id", Message: "Linear response workspace does not match the configured native tenant"})
		return p, nil
	}
	kind := strings.TrimPrefix(req.Kind, "linear-")
	if kind == "metadata" {
		b, _ := json.Marshal(data)
		p.Discovery = map[string]json.RawMessage{"linear-metadata": b}
		team, _ := data["team"].(map[string]any)
		teamID, _ := team["id"].(string)
		if teamID != s.ContainerID {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "scope-identity-mismatch", Field: "team.id", Message: "Linear metadata does not belong to the configured team"})
			return p, nil
		}
		for _, name := range []string{"states", "labels"} {
			conn, _ := team[name].(map[string]any)
			if conn == nil {
				p.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: name, Message: "Linear metadata omitted the requested connection"})
				continue
			}
			if _, ok := conn["nodes"].([]any); !ok {
				p.Complete = false

				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: name + ".nodes", Message: "Linear metadata omitted connection nodes"})
				continue
			}
			if _, ok := conn["pageInfo"].(map[string]any); !ok {
				p.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: name + ".pageInfo", Message: "Linear metadata omitted pagination state"})
				continue
			}
			pi, _ := conn["pageInfo"].(map[string]any)
			if more, valid := linearHasNext(&p, pi, name+".hasNextPage"); valid && more {
				cursor, _ := pi["endCursor"].(string)
				if err := addLinearCursor(&p, req, cursor, name, s.ContainerID); err != nil {
					return PageResult{}, err
				}
			}
		}
		return p, nil
	}
	if kind == "states" || kind == "labels" {
		team, _ := data["team"].(map[string]any)
		teamID, _ := team["id"].(string)
		if teamID != s.ContainerID {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "scope-identity-mismatch", Field: "team.id", Message: "Linear metadata response does not belong to the configured team"})
			return p, nil
		}
		conn, _ := team[kind].(map[string]any)
		if conn == nil {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: kind, Message: "Linear metadata response omitted the requested connection"})
			return p, nil
		}
		if _, ok := conn["nodes"].([]any); !ok {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: kind + ".nodes", Message: "Linear metadata omitted connection nodes"})
			return p, nil
		}
		if _, ok := conn["pageInfo"].(map[string]any); !ok {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: kind + ".pageInfo", Message: "Linear metadata omitted pagination state"})
			return p, nil
		}
		cursor := linearRequestCursor(req)
		raw, _ := json.Marshal(conn)
		p.Discovery["linear-"+kind+":"+cursor] = raw
		pi, _ := conn["pageInfo"].(map[string]any)
		if more, valid := linearHasNext(&p, pi, kind+".hasNextPage"); valid && more {
			next, _ := pi["endCursor"].(string)
			if err := addLinearCursor(&p, req, next, kind, s.ContainerID); err != nil {
				return PageResult{}, err
			}
		}
		return p, nil
	}
	if kind == "issue-labels" {
		n, _ := data["issue"].(map[string]any)
		id, _ := n["id"].(string)
		team, _ := n["team"].(map[string]any)
		teamID, _ := team["id"].(string)
		if n == nil || id != req.ItemID || teamID != s.ContainerID {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "scope-identity-mismatch", Field: "issue", Message: "Linear label page does not match the requested scoped issue"})
			return p, nil
		}
		conn, _ := n["labels"].(map[string]any)
		if conn == nil {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "labels", Message: "Linear issue label page omitted the labels connection"})
			return p, nil
		}
		nodes, ok := conn["nodes"].([]any)
		if !ok {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "labels.nodes", Message: "Linear label page omitted label nodes"})
			return p, nil
		}
		if _, ok := conn["pageInfo"].(map[string]any); !ok {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "labels.pageInfo", Message: "Linear issue label page omitted pagination state"})
			return p, nil
		}
		fragment := NewItem(s, id, "issue")
		fragment.Complete = true
		fragment.Fields["labels"], _ = json.Marshal(nodes)
		cursor := linearRequestCursor(req)
		fragment.ProjectFields["labels-page:"+cursor], _ = json.Marshal(conn)
		p.Items = append(p.Items, fragment)
		pi, _ := conn["pageInfo"].(map[string]any)
		if more, valid := linearHasNext(&p, pi, "labels.hasNextPage"); valid && more {
			next, _ := pi["endCursor"].(string)
			if err := addLinearCursor(&p, req, next, "issue-labels", id); err != nil {
				return PageResult{}, err
			}
		}
		return p, nil
	}
	if kind == "item" {
		n, _ := data["issue"].(map[string]any)
		if n == nil {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "issue", Message: "Linear issue was not returned"})
			p.Complete = false
			return p, nil
		}
		id, _ := n["id"].(string)
		team, _ := n["team"].(map[string]any)
		teamID, _ := team["id"].(string)
		identifier, _ := n["identifier"].(string)
		if id != req.ItemID && identifier != req.ItemID || teamID != s.ContainerID {
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "scope-identity-mismatch", Field: "issue", Message: "Linear issue identity does not match the requested scoped item"})
			p.Complete = false
			return p, nil
		}
		labels, ok := n["labels"].(map[string]any)
		if !ok {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "labels", Message: "Linear issue response omitted labels connection"})
			return p, nil
		}
		if _, ok := labels["nodes"].([]any); !ok {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "labels.nodes", Message: "Linear issue response omitted label nodes"})
			return p, nil
		}
		if _, ok := labels["pageInfo"].(map[string]any); !ok {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "labels.pageInfo", Message: "Linear issue response omitted label pagination state"})
			return p, nil
		}
		item := linearItem(s, n)
		lpi, _ := labels["pageInfo"].(map[string]any)
		if more, valid := linearHasNext(&p, lpi, "labels.hasNextPage"); !valid {
			item.Complete = false
		} else if more {
			cursor, _ := lpi["endCursor"].(string)
			if err := addLinearCursor(&p, req, cursor, "issue-labels", id); err != nil {
				return PageResult{}, err
			}
		}
		p.Items = []Item{item}
		return p, nil
	}
	connPath := "issues"
	if kind == "comments" {
		connPath = "comments"
	}
	if kind == "relations" {
		connPath = "relations"
	}
	var conn map[string]any
	var issue map[string]any
	if kind == "items" {
		conn, _ = data[connPath].(map[string]any)
	} else {
		issue, _ = data["issue"].(map[string]any)
		issueID, _ := issue["id"].(string)
		if issueID == "" || issueID != req.ItemID {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "scope-identity-mismatch", Field: "issue.id", Message: "Linear auxiliary response issue does not match the requested item"})
			return p, nil
		}
		team, _ := issue["team"].(map[string]any)
		teamID, _ := team["id"].(string)
		if teamID != s.ContainerID {
			p.Complete = false
			p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "scope-identity-mismatch", Field: "issue.team.id", Message: "Linear auxiliary response does not belong to the configured team"})
			return p, nil
		}
		conn, _ = issue[connPath].(map[string]any)
	}
	if conn == nil {
		p.Complete = false
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: connPath, Message: "Linear response omitted the requested collection"})
		return p, nil
	}
	if _, ok := conn["pageInfo"].(map[string]any); !ok {
		p.Complete = false
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: connPath + ".pageInfo", Message: "Linear response omitted pagination state"})
		return p, nil
	}
	nodes, _ := conn["nodes"].([]any)
	if nodes == nil {
		p.Complete = false
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: connPath + ".nodes", Message: "Linear response omitted the requested connection nodes"})
		return p, nil
	}
	for _, v := range nodes {
		n, _ := v.(map[string]any)
		if n == nil {
			continue
		}
		if kind == "items" {
			team, _ := n["team"].(map[string]any)
			teamID, _ := team["id"].(string)
			if teamID != s.ContainerID {
				p.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "scope-identity-mismatch", Field: "team.id", Message: "Linear issue team does not match configured team"})
				continue
			}
			labels, ok := n["labels"].(map[string]any)
			if !ok {
				p.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "labels", Message: "Linear issue omitted labels connection"})
				continue
			}
			if _, ok := labels["nodes"].([]any); !ok {
				p.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "labels.nodes", Message: "Linear issue omitted label nodes"})
				continue
			}
			if _, ok := labels["pageInfo"].(map[string]any); !ok {
				p.Complete = false
				p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: "labels.pageInfo", Message: "Linear issue omitted label pagination state"})
				continue
			}
			item := linearItem(s, n)
			lpi, _ := labels["pageInfo"].(map[string]any)
			if more, valid := linearHasNext(&p, lpi, "labels.hasNextPage"); !valid {
				item.Complete = false
			} else if more {
				id, _ := n["id"].(string)
				cursor, _ := lpi["endCursor"].(string)
				if err := addLinearCursor(&p, req, cursor, "issue-labels", id); err != nil {
					return PageResult{}, err
				}
			}
			p.Items = append(p.Items, item)
		} else {
			fragment := NewItem(s, req.ItemID, "issue")
			fragment.Complete = true
			if kind == "comments" {
				id, _ := n["id"].(string)
				body, _ := n["body"].(string)
				updated, _ := n["updatedAt"].(string)
				fragment.Comments = append(fragment.Comments, Comment{ID: id, Body: body, UpdatedAt: updated})
			}
			if kind == "relations" {
				rel, _ := n["relatedIssue"].(map[string]any)
				target, _ := rel["id"].(string)
				typ, _ := n["type"].(string)
				u, _ := rel["url"].(string)
				if target != "" {
					fragment.Relations = append(fragment.Relations, Relation{Type: typ, TargetID: target, URL: u, Supported: false})
				}
			}
			p.Items = append(p.Items, fragment)
		}
	}
	pi, _ := conn["pageInfo"].(map[string]any)
	if kind == "items" {
	}
	if next, valid := linearHasNext(&p, pi, connPath+".hasNextPage"); valid && next {
		cur, _ := pi["endCursor"].(string)
		if strings.TrimSpace(cur) == "" {
			return PageResult{}, fmt.Errorf("Linear pagination has next page without endCursor")
		}
		var vars map[string]any
		_ = json.Unmarshal(req.Body, &vars)
		q, _ := vars["query"].(string)
		var body []byte
		if kind == "items" {
			body, _ = json.Marshal(map[string]any{"query": linearIssuesQuery, "variables": map[string]any{"teamId": s.ContainerID, "after": cur}})
		} else {
			query := linearCommentsQuery
			if kind == "relations" {
				query = linearRelationsQuery
			}
			body, _ = json.Marshal(map[string]any{"query": query, "variables": map[string]any{"issueId": req.ItemID, "after": cur}})
		}
		_ = q
		p.Next = append(p.Next, Request{Method: "POST", Path: "/graphql", Body: body, Kind: req.Kind, ItemID: req.ItemID})
	}
	if kind == "items" {
		for _, it := range p.Items {
			if s.Scope.Comments {
				b, _ := json.Marshal(map[string]any{"query": linearCommentsQuery, "variables": map[string]any{"issueId": it.Identity.RemoteID}})
				p.Next = append(p.Next, Request{Method: "POST", Path: "/graphql", Body: b, Kind: "linear-comments", ItemID: it.Identity.RemoteID})
			}
			if s.Scope.Relations {
				b, _ := json.Marshal(map[string]any{"query": linearRelationsQuery, "variables": map[string]any{"issueId": it.Identity.RemoteID}})
				p.Next = append(p.Next, Request{Method: "POST", Path: "/graphql", Body: b, Kind: "linear-relations", ItemID: it.Identity.RemoteID})
			}
		}
	}
	return p, nil
}
func linearItem(s Source, n map[string]any) Item {
	id, _ := n["id"].(string)
	identifier, _ := n["identifier"].(string)
	it := NewItem(s, id, "issue")
	it.Complete = true
	it.DisplayKey = identifier
	if u, _ := n["url"].(string); u != "" {
		it.URL = u
	}
	if u, _ := n["updatedAt"].(string); u != "" {
		it.UpdatedAt = u
	}
	for k, v := range n {
		if k == "labels" {
			conn, _ := v.(map[string]any)
			nodes, _ := conn["nodes"].([]any)
			it.Fields["labels"], _ = json.Marshal(nodes)
			it.Fields["labelsConnection"], _ = json.Marshal(conn)
			if pi, ok := conn["pageInfo"].(map[string]any); ok {
				if more, _ := pi["hasNextPage"].(bool); more {
					it.LossyFields = append(it.LossyFields, "labelsConnection")
				}
			}
			continue
		}
		b, _ := json.Marshal(v)
		it.Fields[k] = b
	}
	it.Revision = Digest(n)
	it.RevisionKind = "observation-fingerprint"
	return it
}

func linearHasNext(p *PageResult, pageInfo map[string]any, field string) (bool, bool) {
	more, ok := pageInfo["hasNextPage"].(bool)
	if !ok {
		p.Complete = false
		p.Diagnostics = append(p.Diagnostics, Diagnostic{Code: "missing-record", Field: field, Message: "Linear response omitted a valid pagination hasNextPage boolean"})
		return false, false
	}
	return more, true
}

func addLinearCursor(p *PageResult, current Request, cursor, kind, id string) error {
	if strings.TrimSpace(cursor) == "" {
		return fmt.Errorf("Linear %s pagination has next page without endCursor", kind)
	}
	if cursor == linearRequestCursor(current) {
		return fmt.Errorf("Linear %s pagination cursor did not advance", kind)
	}
	var query string
	vars := map[string]any{"after": cursor}
	if kind == "states" {
		query = linearStatesQuery
		vars["teamId"] = id
	} else if kind == "labels" {
		query = linearLabelsQuery
		vars["teamId"] = id
	} else {
		query = linearIssueLabelsQuery
		vars["issueId"] = id
	}
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	req := Request{Method: "POST", Path: "/graphql", Body: body, Kind: "linear-" + kind}
	if kind == "issue-labels" {
		req.ItemID = id
	}
	p.Next = append(p.Next, req)
	return nil
}
func linearRequestCursor(r Request) string {
	var body struct {
		Variables map[string]any `json:"variables"`
	}
	_ = json.Unmarshal(r.Body, &body)
	cursor, _ := body.Variables["after"].(string)
	return cursor
}
