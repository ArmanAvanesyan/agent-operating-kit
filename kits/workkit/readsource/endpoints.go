// Read-side extraction from AOK; legacy formats intentionally retain their identities.
package readsource

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

func SharedValidateEndpoint(s Source, r Request, u *url.URL) error {
	match := func(pattern string) bool { return regexp.MustCompile(pattern).MatchString(u.Path) }
	ok := false
	if r.Method == "POST" && (s.Provider == "github" || s.Provider == "linear") {
		return nil
	} // exact query validator follows
	if r.Method != "GET" && s.Provider != "azure-devops" {
		return fmt.Errorf("endpoint requires GET")
	}
	switch s.Provider {
	case "github":
		root := regexp.QuoteMeta("/repos/" + s.ContainerKey)
		switch r.Kind {
		case "github-repository":
			ok = u.Path == "/repos/"+s.ContainerKey
		case "github-issues":
			ok = u.Path == "/repos/"+s.ContainerKey+"/issues"
		case "github-item":
			ok = match("^" + root + "/(issues|pulls)/[0-9]+$")
		case "github-comments":
			ok = match("^" + root + "/issues/[0-9]+/comments$")
		case "github-timeline":
			ok = match("^" + root + "/issues/[0-9]+/timeline$")
		}
	case "linear":
		ok = false
	case "azure-devops":
		parts := strings.Split(s.ContainerKey, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid Azure container")
		}
		root := "/" + parts[0] + "/" + parts[1] + "/_apis/"
		base := regexp.QuoteMeta(root)
		switch r.Kind {
		case "azure-project":
			ok = u.Path == "/"+parts[0]+"/_apis/projects/"+parts[1] && r.Method == "GET"
		case "azure-processes":
			ok = u.Path == "/"+parts[0]+"/_apis/process/processes" && r.Method == "GET"
		case "azure-types":
			ok = u.Path == root+"wit/workitemtypes" && r.Method == "GET"
		case "azure-fields":
			ok = u.Path == root+"wit/fields" && r.Method == "GET"
		case "azure-states":
			ok = match("^"+base+"wit/workitemtypes/[^/]+/states$") && r.Method == "GET"
		case "azure-items":
			ok = u.Path == root+"wit/workitemsbatch" && r.Method == "POST"
		case "azure-wiql":
			ok = u.Path == root+"wit/wiql" && r.Method == "POST"
		case "azure-item":
			ok = match("^"+base+"wit/workitems/[0-9]+$") && r.Method == "GET"
		case "azure-comments":
			ok = match("^"+base+"wit/workItems/[0-9]+/comments$") && r.Method == "GET"
		}
	case "jira-datacenter":
		return SharedValidateDCEndpoint(s, r, u)
	case "jira":
		prefix := "/rest/api/3/"
		if s.Origin == "https://api.atlassian.com" {
			prefix = "/ex/jira/" + s.NativeTenantID + prefix
		}
		path := strings.TrimPrefix(u.Path, prefix)
		switch r.Kind {
		case "jira-project":
			ok = path == "project/"+s.ContainerKey
		case "jira-fields":
			offset, err := strconv.Atoi(u.Query().Get("startAt"))
			ok = path == "field/search" && u.Query().Get("projectIds") == s.ContainerID && u.Query().Get("maxResults") == "50" && err == nil && offset >= 0 && offset <= 10000
			for k, values := range u.Query() {
				if len(values) != 1 || k != "projectIds" && k != "startAt" && k != "maxResults" {
					ok = false
				}
			}
		case "jira-types":
			ok = path == "issuetype/project" && u.Query().Get("projectId") == s.ContainerID
		case "jira-search":
			expected := "project = \"" + strings.ReplaceAll(s.ContainerKey, "\"", "\\\"") + "\" ORDER BY updated DESC"
			ok = path == "search/jql" && u.Query().Get("jql") == expected
		case "jira-item":
			ok = regexp.MustCompile(`^issue/[A-Za-z0-9._:-]+$`).MatchString(path)
		case "jira-comments", "jira-transitions", "jira-relations":
			ending := map[string]string{"jira-comments": "/comment", "jira-transitions": "/transitions", "jira-relations": ""}[r.Kind]
			ok = r.ItemID != "" && path == "issue/"+r.ItemID+ending
		}
	}
	for _, values := range u.Query() {
		if len(values) != 1 {
			return fmt.Errorf("duplicate query parameter prohibited")
		}
	}
	if !ok {
		return fmt.Errorf("request is outside approved tracker read endpoints")
	}
	return nil
}
