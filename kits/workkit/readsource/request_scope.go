// Read-side extraction from AOK; legacy formats intentionally retain their identities.
package readsource

import (
	"encoding/json"
	"fmt"

	"net/url"
	"strings"
)

func SharedValidateQueryTemplate(s Source, r Request) error {
	var b struct {
		Query     string                     `json:"query"`
		Variables map[string]json.RawMessage `json:"variables"`
	}
	if e := decodeRaw(r.Body, &b); e != nil {
		return e
	}
	template := map[string]string{"github-projects": ghProjects, "github-project-fields": GhProjectFields, "github-project-items": ghProjectItems, "github-item-fields": ghItemFields, "linear-items": linearIssuesQuery, "linear-item": linearExplicitIssueQuery, "linear-comments": linearCommentsQuery, "linear-relations": linearRelationsQuery, "linear-metadata": linearMetadataQuery, "linear-states": linearStatesQuery, "linear-labels": linearLabelsQuery, "linear-issue-labels": linearIssueLabelsQuery}[r.Kind]
	if template == "" || b.Query != template {
		return fmt.Errorf("GraphQL request is not an approved fixed query")
	}
	get := func(k string) string { var v string; _ = decodeRaw(b.Variables[k], &v); return v }
	allowed := map[string]bool{"after": true}
	switch r.Kind {
	case "github-projects":
		allowed["owner"] = true
		allowed["repo"] = true
		parts := strings.Split(s.ContainerKey, "/")
		if !s.Scope.Projects || len(parts) != 2 || get("owner") != parts[0] || get("repo") != parts[1] {
			return fmt.Errorf("GitHub Projects query scope mismatch")
		}
	case "github-project-fields", "github-project-items", "github-item-fields":
		allowed["id"] = true
		if !s.Scope.Projects || get("id") == "" {
			return fmt.Errorf("GitHub project identity missing")
		}
	case "linear-items", "linear-metadata", "linear-states", "linear-labels":
		allowed["teamId"] = true
		if get("teamId") != s.ContainerID {
			return fmt.Errorf("Linear team query scope mismatch")
		}
	case "linear-item", "linear-comments", "linear-relations", "linear-issue-labels":
		allowed["issueId"] = true
		if get("issueId") == "" {
			return fmt.Errorf("Linear issue identity missing")
		}
		if r.ItemID != "" && get("issueId") != r.ItemID {
			return fmt.Errorf("Linear request item mismatch")
		}
		if len(s.Scope.ItemIDs) > 0 && r.Kind == "linear-item" && !has(s.Scope.ItemIDs, get("issueId")) {
			return fmt.Errorf("Linear request outside selected issue IDs")
		}
	}
	for k := range b.Variables {
		if !allowed[k] {
			return fmt.Errorf("GraphQL variable outside approved query")
		}
	}
	return nil
}
func SharedValidateSelectedItem(s Source, r Request, u *url.URL) error {
	if (r.Kind == "github-comments" || r.Kind == "github-timeline") && !r.baseScopeValidated {
		return fmt.Errorf("GitHub auxiliary read requires a validated scoped base item")
	}
	if len(s.Scope.ItemIDs) == 0 {
		return nil
	}
	switch s.Provider {
	case "github":
		parts := strings.Split(strings.TrimPrefix(u.Path, "/repos/"+s.ContainerKey+"/"), "/")
		if len(parts) >= 2 && (parts[0] == "issues" || parts[0] == "pulls") {
			num := parts[1]
			allowed := false
			for _, id := range s.Scope.ItemIDs {
				value := strings.TrimPrefix(strings.TrimPrefix(id, "issue:"), "pr:")
				if value == num {
					if r.Kind == "github-item" && (strings.HasPrefix(id, "pr:") && parts[0] != "pulls" || strings.HasPrefix(id, "issue:") && parts[0] != "issues") {
						continue
					}
					allowed = true
				}
			}
			if !allowed {
				return fmt.Errorf("GitHub request outside selected item numbers")
			}
		}
	case "azure-devops":
		if strings.HasSuffix(u.Path, "/workitemsbatch") {
			var b struct {
				IDs []json.Number `json:"ids"`
			}
			var raw map[string]json.RawMessage
			if decodeRaw(r.Body, &raw) != nil || decodeRaw(raw["ids"], &b.IDs) != nil {
				return fmt.Errorf("invalid Azure read IDs")
			}
			for _, id := range b.IDs {
				if !has(s.Scope.ItemIDs, string(id)) {
					return fmt.Errorf("Azure batch outside selected IDs")
				}
			}
		}
	case "jira":
		prefix := strings.Split(u.Path, "/rest/api/3/issue/")
		if len(prefix) == 2 {
			value := strings.Split(prefix[1], "/")[0]
			if r.Kind == "jira-item" && !has(s.Scope.ItemIDs, value) {
				return fmt.Errorf("Jira request outside selected issue IDs")
			}
		}
	}
	return nil
}
