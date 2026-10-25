// Read-side extraction from AOK; legacy formats intentionally retain their identities.
package readsource

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Azure is a read-only Azure DevOps Services work-item adapter.
type Azure struct{}

func NewAzure() Adapter        { return Azure{} }
func (Azure) Provider() string { return "azure-devops" }

const azureAPI = "7.1"

var azureID = regexp.MustCompile(`^[0-9]+$`)

func SharedAzureScope(s Source) (string, string, error) {
	if s.Provider != "azure-devops" {
		return "", "", fmt.Errorf("source provider must be azure-devops")
	}
	if s.APIVersion != "" && s.APIVersion != azureAPI {
		return "", "", fmt.Errorf("Azure DevOps API version must be %s", azureAPI)
	}
	if !strings.HasPrefix(strings.ToLower(s.Origin), "https://dev.azure.com") {
		return "", "", fmt.Errorf("Azure origin must use https://dev.azure.com")
	}
	u, err := url.Parse(s.Origin)
	if err != nil || u.Host != "dev.azure.com" || u.RawQuery != "" || u.Fragment != "" {
		return "", "", fmt.Errorf("invalid Azure DevOps origin")
	}
	parts := strings.Split(s.ContainerKey, "/")
	if len(parts) != 2 || !SharedSafeAzureName(parts[0]) || !SharedSafeAzureName(parts[1]) {
		return "", "", fmt.Errorf("containerKey must be ORG/PROJECT")
	}
	return parts[0], parts[1], nil
}
func SharedSafeAzureName(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `/\?#`)
}
func SharedAdoPath(org, project, tail string) string {
	return "/" + url.PathEscape(org) + "/" + url.PathEscape(project) + "/_apis/" + tail
}

func (Azure) Initial(s Source, mode string, cp *Checkpoint) ([]Request, error) {
	org, project, err := SharedAzureScope(s)
	if err != nil {
		return nil, err
	}
	if mode != "capture" && mode != "discover" && mode != "import" {
		return nil, fmt.Errorf("unsupported tracker mode %q", mode)
	}
	q := func(kind, path string) Request { return Request{Method: "GET", Path: path, Kind: kind} }
	metadata := func() []Request {
		return []Request{q("azure-project", "/"+url.PathEscape(org)+"/_apis/projects/"+url.PathEscape(project)+"?api-version="+azureAPI), q("azure-processes", "/"+url.PathEscape(org)+"/_apis/process/processes?api-version="+azureAPI), q("azure-types", SharedAdoPath(org, project, "wit/workitemtypes?api-version="+azureAPI)), q("azure-fields", SharedAdoPath(org, project, "wit/fields?api-version="+azureAPI))}
	}
	if mode == "discover" {
		return metadata(), nil
	}
	if cp != nil && len(cp.Pending) > 0 {
		return append([]Request(nil), cp.Pending...), nil
	}
	batchPath := "wit/workitemsbatch?api-version=" + azureAPI
	if len(s.Scope.ItemIDs) > 0 {
		ids := make([]string, 0, len(s.Scope.ItemIDs))
		seen := map[string]bool{}
		for _, id := range s.Scope.ItemIDs {
			if !SharedValidAzureID(id) {
				return nil, fmt.Errorf("unsafe Azure work item id %q", id)
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		// Establish the immutable project GUID before any scoped item data is
		// captured. The human-readable project name alone is not a boundary.
		requests := make([]Request, 0, (len(ids)+199)/200+1)
		requests = append(requests, metadata()[0])
		for i := 0; i < len(ids); i += 200 {
			end := i + 200
			if end > len(ids) {
				end = len(ids)
			}
			numericIDs := make([]int, 0, end-i)
			for _, id := range ids[i:end] {
				n, _ := strconv.Atoi(id)
				numericIDs = append(numericIDs, n)
			}
			body, _ := json.Marshal(SharedAzureBatchBody(numericIDs, s.Scope.Relations))
			requests = append(requests, Request{Method: "POST", Path: SharedAdoPath(org, project, batchPath), Body: body, Kind: "azure-items", ItemID: strings.Join(ids[i:end], ",")})
		}
		return requests, nil
	}
	if s.Scope.Query != "" {
		// Only accept an explicitly bounded WIQL SELECT. The body is sent to the
		// documented read-only query endpoint; no operation/update endpoint exists.
		wiql, queryErr := SharedAzureScopedWIQL(s)
		if queryErr != nil {
			return nil, queryErr
		}
		b, _ := json.Marshal(map[string]string{"query": wiql})
		return []Request{metadata()[0], {Method: "POST", Path: SharedAdoPath(org, project, "wit/wiql?api-version="+azureAPI), Body: b, Kind: "azure-wiql"}}, nil
	}
	return metadata(), nil
}
func SharedValidAzureID(id string) bool {
	if !azureID.MatchString(id) {
		return false
	}
	n, err := strconv.ParseInt(id, 10, 32)
	return err == nil && n > 0
}

func SharedRawObject(b []byte) (map[string]json.RawMessage, error) {
	return DecodeObject(b)
}
func SharedRawString(m map[string]json.RawMessage, k string) string {
	var s string
	_ = json.Unmarshal(m[k], &s)
	return s
}
func (Azure) Decode(s Source, req Request, resp Response) (PageResult, error) {
	org, project, err := SharedAzureScope(s)
	if err != nil {
		return PageResult{}, err
	}
	if resp.Status < 200 || resp.Status >= 300 {
		return PageResult{}, fmt.Errorf("Azure DevOps returned HTTP %d", resp.Status)
	}
	root, err := SharedRawObject(resp.Body)
	if err != nil {
		return PageResult{}, fmt.Errorf("decode Azure response: %w", err)
	}
	switch req.Kind {
	case "azure-project":
		id := SharedRawString(root, "id")
		name := SharedRawString(root, "name")
		if id != s.ContainerID || !strings.EqualFold(name, project) {
			return PageResult{Complete: false, Diagnostics: []Diagnostic{{Code: "scope-mismatch", Field: "project", Message: "Azure project response does not match configured project"}}}, nil
		}
		return PageResult{Discovery: map[string]json.RawMessage{"project": append([]byte(nil), resp.Body...), "projectId": root["id"]}, Complete: true}, nil
	case "azure-processes", "azure-fields", "azure-states":
		if err := SharedRequireAzureArray(root, "value"); err != nil {
			return PageResult{}, fmt.Errorf("Azure %s response missing value: %w", req.Kind, err)
		}
		key := strings.TrimPrefix(req.Kind, "azure-")
		return PageResult{Discovery: map[string]json.RawMessage{key: append([]byte(nil), resp.Body...)}, Complete: true}, nil
	case "azure-types":
		if err := SharedRequireAzureArray(root, "value"); err != nil {
			return PageResult{}, fmt.Errorf("work item types response missing value: %w", err)
		}
		var types []map[string]json.RawMessage
		if err := json.Unmarshal(root["value"], &types); err != nil {
			return PageResult{}, fmt.Errorf("work item types response missing value: %w", err)
		}
		next := make([]Request, 0, len(types))
		for _, t := range types {
			name := SharedRawString(t, "referenceName")
			if name == "" {
				name = SharedRawString(t, "name")
			}
			if name != "" {
				next = append(next, Request{Method: "GET", Path: SharedAdoPath(org, project, "wit/workitemtypes/"+url.PathEscape(name)+"/states?api-version="+azureAPI), Kind: "azure-states", ItemID: name})
			}
		}
		return PageResult{Discovery: map[string]json.RawMessage{"types": append([]byte(nil), resp.Body...)}, Next: next, Complete: true}, nil
	case "azure-wiql":
		var refs []struct {
			ID int `json:"id"`
		}
		if err := SharedRequireAzureArray(root, "workItems"); err != nil {
			return PageResult{}, fmt.Errorf("WIQL response missing workItems: %w", err)
		}
		if err := json.Unmarshal(root["workItems"], &refs); err != nil {
			return PageResult{}, fmt.Errorf("WIQL response missing workItems: %w", err)
		}
		if len(refs) == 0 {
			return PageResult{Complete: true}, nil
		}
		ids := make([]string, 0, len(refs))
		for _, x := range refs {
			if x.ID <= 0 {
				return PageResult{}, fmt.Errorf("invalid work item id in WIQL result")
			}
			ids = append(ids, strconv.Itoa(x.ID))
		}
		next := make([]Request, 0, (len(ids)+199)/200)
		for i := 0; i < len(ids); i += 200 {
			end := i + 200
			if end > len(ids) {
				end = len(ids)
			}
			chunk := ids[i:end]
			numericIDs := make([]int, 0, len(chunk))
			for _, id := range chunk {
				n, _ := strconv.Atoi(id)
				numericIDs = append(numericIDs, n)
			}
			body, _ := json.Marshal(SharedAzureBatchBody(numericIDs, s.Scope.Relations))
			batchPath := "wit/workitemsbatch?api-version=" + azureAPI
			next = append(next, Request{Method: "POST", Path: SharedAdoPath(org, project, batchPath), Body: body, Kind: "azure-items", ItemID: strings.Join(chunk, ",")})
		}
		return PageResult{Next: next, Complete: true}, nil
	case "azure-items":
		if err := SharedRequireAzureArray(root, "value"); err != nil {
			return PageResult{}, fmt.Errorf("workitemsbatch response missing value: %w", err)
		}
		var values []map[string]json.RawMessage
		if err := json.Unmarshal(root["value"], &values); err != nil {
			return PageResult{}, fmt.Errorf("workitemsbatch response missing value: %w", err)
		}
		wanted := strings.Split(req.ItemID, ",")
		requested := make(map[string]bool, len(wanted))
		for _, id := range wanted {
			if !SharedValidAzureID(id) {
				return PageResult{}, fmt.Errorf("invalid requested Azure work item ID")
			}
			requested[id] = true
		}
		found := map[string]bool{}
		items := make([]Item, 0, len(values))
		next := []Request{}
		for _, v := range values {
			id := SharedRawString(v, "id")
			if id == "" {
				var n json.Number
				if json.Unmarshal(v["id"], &n) == nil {
					id = n.String()
				}
			}
			if !SharedValidAzureID(id) {
				return PageResult{}, fmt.Errorf("Azure response contained an invalid work item ID")
			}
			if !requested[id] {
				return PageResult{}, fmt.Errorf("Azure response included an unrequested work item")
			}
			if found[id] {
				return PageResult{}, fmt.Errorf("Azure response duplicated a work item ID")
			}
			fields := map[string]json.RawMessage{}
			_ = json.Unmarshal(v["fields"], &fields)
			if fields == nil {
				fields = map[string]json.RawMessage{}
			}
			team := SharedRawString(fields, "System.TeamProject")
			if team == "" {
				return PageResult{}, fmt.Errorf("Azure work item is missing its project identity")
			}
			if !strings.EqualFold(team, project) {
				return PageResult{}, fmt.Errorf("Azure response included a work item outside configured project")
			}
			kind := SharedRawString(fields, "System.WorkItemType")
			rev := SharedRawString(v, "rev")
			if rev == "" {
				var n json.Number
				if json.Unmarshal(v["rev"], &n) == nil {
					rev = n.String()
				}
			}
			nativeTenant := s.NativeTenantID
			itemURL := SharedRawString(v, "url")
			display := id
			if kind != "" {
				display = kind + " " + id
			}
			relations := []Relation{}
			if s.Scope.Relations {
				var links []struct {
					Rel string `json:"rel"`
					URL string `json:"url"`
				}
				_ = json.Unmarshal(v["relations"], &links)
				for _, link := range links {
					target := ""
					if p := strings.LastIndex(strings.TrimRight(link.URL, "/"), "/"); p >= 0 {
						target = link.URL[p+1:]
					}
					// Preserve native link types verbatim. A numeric target is not proof
					// that AOK supports the relation for any future write operation.
					relations = append(relations, Relation{Type: link.Rel, TargetID: target, URL: link.URL, Supported: false})
				}
			}
			item := NewItem(s, id, kind)
			item.NativeTenantID = nativeTenant
			item.DisplayKey = display
			item.URL = itemURL
			item.Revision = rev
			item.RevisionKind = "azure-work-item-rev"
			item.UpdatedAt = SharedRawString(fields, "System.ChangedDate")
			item.Fields = fields
			item.Relations = relations
			// Auxiliary comment reads are tracked by PageResult.Next and the
			// capture checkpoint. They do not make the base work-item record
			// incomplete; SharedMergeItem preserves this completeness when fragments arrive.
			item.Complete = true
			items = append(items, item)
			found[id] = true
			if s.Scope.Comments {
				next = append(next, Request{Method: "GET", Path: SharedAdoPath(org, project, "wit/workItems/"+url.PathEscape(id)+"/comments?api-version=7.1-preview.4"), Kind: "azure-comments", ItemID: id})
			}
		}
		missing := []string{}
		for _, id := range wanted {
			if id != "" && !found[id] {
				missing = append(missing, id)
			}
		}
		res := PageResult{Items: items, Next: next, Complete: len(missing) == 0}
		if len(missing) > 0 {
			res.Diagnostics = []Diagnostic{{Code: "missing-requested-items", Field: "items", Message: "Azure response omitted requested work item IDs: " + strings.Join(missing, ",")}}
		}
		return res, nil
	case "azure-comments":
		if !SharedValidAzureID(req.ItemID) {
			return PageResult{}, fmt.Errorf("unsafe Azure work item id %q", req.ItemID)
		}
		if err := SharedRequireAzureArray(root, "comments"); err != nil {
			return PageResult{}, fmt.Errorf("comments response missing comments: %w", err)
		}
		var values []map[string]json.RawMessage
		if err := json.Unmarshal(root["comments"], &values); err != nil {
			return PageResult{}, fmt.Errorf("comments response missing comments: %w", err)
		}
		comments := make([]Comment, 0, len(values))
		for _, v := range values {
			id := SharedRawString(v, "id")
			if id == "" {
				var n json.Number
				if json.Unmarshal(v["id"], &n) == nil {
					id = n.String()
				}
			}
			body := SharedRawString(v, "text")
			if body == "" {
				body = SharedRawString(v, "comment")
			}
			updated := SharedRawString(v, "modifiedDate")
			if updated == "" {
				updated = SharedRawString(v, "createdDate")
			}
			comments = append(comments, Comment{ID: id, Body: body, UpdatedAt: updated})
		}
		item := NewItem(s, req.ItemID, "")
		item.Comments = comments
		item.Complete = true
		next := []Request{}
		continuation := SharedRawString(root, "continuationToken")
		if continuation != "" {
			next = append(next, Request{Method: "GET", Path: SharedAdoPath(org, project, "wit/workItems/"+url.PathEscape(req.ItemID)+"/comments?api-version=7.1-preview.4&continuationToken="+url.QueryEscape(continuation)), Kind: "azure-comments", ItemID: req.ItemID})
		}
		return PageResult{Items: []Item{item}, Next: next, Complete: len(next) == 0}, nil
	default:
		return PageResult{}, fmt.Errorf("unsupported Azure request kind %q", req.Kind)
	}
}

func SharedAzureBatchBody(ids []int, includeRelations bool) map[string]any {
	body := map[string]any{"ids": ids, "errorPolicy": "Omit"}
	if includeRelations {
		body["$expand"] = "Relations"
	}
	return body
}

func SharedRequireAzureArray(root map[string]json.RawMessage, key string) error {
	raw, ok := root[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return fmt.Errorf("%s must be a JSON array", key)
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(raw, &rows); err != nil || rows == nil {
		return fmt.Errorf("%s must be a JSON array", key)
	}
	return nil
}

// SharedAzureScopedWIQL adds the project boundary outside the operator filter. It
// accepts only ID projection; joins, ordering and other WIQL forms are explicit
// unsupported inputs rather than requests that can broaden the approved scope.
func SharedAzureScopedWIQL(s Source) (string, error) {
	m := regexp.MustCompile(`(?is)^SELECT\s+\[System\.Id\]\s+FROM\s+WorkItems(?:\s+WHERE\s+(.+))?$`).FindStringSubmatch(strings.TrimSpace(s.Scope.Query))
	if m == nil || strings.ContainsAny(s.Scope.Query, ";\x00") {
		return "", fmt.Errorf("WIQL supports SELECT [System.Id] FROM WorkItems with an optional filter")
	}
	filter := strings.TrimSpace(m[1])
	if regexp.MustCompile(`(?i)\b(SELECT|FROM|ORDER|UPDATE|DELETE|INSERT|ASOF)\b`).MatchString(filter) {
		return "", fmt.Errorf("unsupported WIQL filter clause")
	}
	q := "SELECT [System.Id] FROM WorkItems WHERE [System.TeamProject] = @project"
	if filter != "" {
		q += " AND (" + filter + ")"
	}
	return q, nil
}
