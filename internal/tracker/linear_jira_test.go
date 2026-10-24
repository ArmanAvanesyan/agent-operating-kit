package tracker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile(filepath.Join("testdata", name))
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestLinearNestedCursorAndGraphQLErrors(t *testing.T) {
	s := Source{Version: Version, Provider: "linear", Origin: "https://api.linear.app", APIVersion: "graphql", NativeTenantID: "workspace-uuid", ContainerID: "team-uuid", Scope: Scope{Comments: true}}
	a := NewLinear()
	reqs, e := a.Initial(s, "capture", nil)
	if e != nil || len(reqs) != 1 {
		t.Fatalf("Initial=%v %v", reqs, e)
	}
	p, e := a.Decode(s, reqs[0], Response{Status: 200, Body: fixture(t, "linear-nested-cursor.json")})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Items) != 1 || p.Items[0].DisplayKey != "ENG-7" || len(p.Next) != 2 {
		t.Fatalf("unexpected result: %#v", p)
	}
	if !strings.Contains(string(p.Next[0].Body), `"after":"cursor-2"`) {
		t.Fatalf("missing nested end cursor: %s", p.Next[0].Body)
	}
	bad, e := a.Decode(s, reqs[0], Response{Status: 200, Body: fixture(t, "linear-partial-errors.json")})
	if e != nil {
		t.Fatal(e)
	}
	if bad.Complete || len(bad.Items) != 0 || len(bad.Diagnostics) == 0 {
		t.Fatalf("GraphQL partial success accepted: %#v", bad)
	}
}

func TestLinearMetadataAndIssueLabelConnectionsPaginate(t *testing.T) {
	s := Source{Version: Version, Provider: "linear", Origin: "https://api.linear.app", APIVersion: "graphql", NativeTenantID: "workspace", ContainerID: "team", Scope: Scope{Projects: true}}
	reqs, e := NewLinear().Initial(s, "discover", nil)
	if e != nil || len(reqs) != 1 || reqs[0].Kind != "linear-metadata" {
		t.Fatalf("metadata plan: %#v %v", reqs, e)
	}
	metadata := []byte(`{"data":{"organization":{"id":"workspace"},"team":{"id":"team","states":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"state-2"}},"labels":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"label-2"}}}}}`)
	p, e := NewLinear().Decode(s, reqs[0], Response{Status: 200, Body: metadata})
	if e != nil || !p.Complete || len(p.Next) != 2 {
		t.Fatalf("metadata page did not schedule both continuations: %#v %v", p, e)
	}
	for _, r := range p.Next {
		var body struct {
			Query     string            `json:"query"`
			Variables map[string]string `json:"variables"`
		}
		if json.Unmarshal(r.Body, &body) != nil || body.Variables["teamId"] != "team" || body.Variables["after"] == "" {
			t.Fatalf("bad metadata continuation request: %#v", r)
		}
		conn := `{"nodes":[{"id":"next","name":"Next"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}`
		resp := []byte(`{"data":{"organization":{"id":"workspace"},"team":{"id":"team","` + strings.TrimPrefix(r.Kind, "linear-") + `":` + conn + `}}}`)
		out, e := NewLinear().Decode(s, r, Response{Status: 200, Body: resp})
		if e != nil || !out.Complete || len(out.Discovery) != 1 {
			t.Fatalf("metadata continuation failed: %#v %v", out, e)
		}
	}

	listReq := Request{Method: "POST", Path: "/graphql", Kind: "linear-items"}
	listBody := []byte(`{"data":{"organization":{"id":"workspace"},"issues":{"nodes":[{"id":"issue-1","identifier":"ENG-1","team":{"id":"team"},"labels":{"nodes":[{"id":"L1","name":"one"}],"pageInfo":{"hasNextPage":true,"endCursor":"issue-label-2"}}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`)
	base, e := NewLinear().Decode(s, listReq, Response{Status: 200, Body: listBody})
	if e != nil || len(base.Items) != 1 || len(base.Next) != 1 || base.Items[0].Fields["labelsConnection"] == nil {
		t.Fatalf("base labels page not represented: %#v %v", base, e)
	}
	if !has(base.Items[0].LossyFields, "labelsConnection") {
		t.Fatalf("partial native connection not marked lossy: %#v", base.Items[0].LossyFields)
	}
	page := func(after, name string, more bool, next string) Response {
		return Response{Status: 200, Body: []byte(`{"data":{"organization":{"id":"workspace"},"issue":{"id":"issue-1","team":{"id":"team"},"labels":{"nodes":[{"id":"` + name + `","name":"` + name + `"}],"pageInfo":{"hasNextPage":` + map[bool]string{true: "true", false: "false"}[more] + `,"endCursor":"` + next + `"}}}}}`)}
	}
	first, e := NewLinear().Decode(s, base.Next[0], page("issue-label-2", "L2", true, "issue-label-3"))
	if e != nil || len(first.Items) != 1 || len(first.Next) != 1 || string(first.Items[0].Fields["labels"]) != `[{"id":"L2","name":"L2"}]` {
		t.Fatalf("first issue-label page failed: %#v %v", first, e)
	}
	last, e := NewLinear().Decode(s, first.Next[0], page("issue-label-3", "L3", false, ""))
	if e != nil || !last.Complete || len(last.Items) != 1 || last.Items[0].ProjectFields["labels-page:issue-label-3"] == nil {
		t.Fatalf("last issue-label page failed: %#v %v", last, e)
	}
	_, e = NewLinear().Decode(s, base.Next[0], page("issue-label-2", "L2", true, "issue-label-2"))
	if e == nil {
		t.Fatal("repeated issue-label cursor accepted")
	}
}
func TestJiraADFAndProjectScope(t *testing.T) {
	s := Source{Provider: "jira", Origin: "https://example.atlassian.net", APIVersion: "3", NativeTenantID: "cloud-uuid", ContainerID: "10001", ContainerKey: "ENG"}
	a := NewJira()
	reqs, e := a.Initial(s, "capture", nil)
	if e != nil || len(reqs) != 1 {
		t.Fatalf("Initial=%v %v", reqs, e)
	}
	p, e := a.Decode(s, Request{Kind: "jira-search", Path: reqs[0].Path}, Response{Status: 200, Body: fixture(t, "jira-adf-unsupported.json")})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Items) != 1 || len(p.Items[0].LossyFields) == 0 || !strings.Contains(string(p.Items[0].Fields["descriptionText"]), "hello world") {
		t.Fatalf("ADF preservation/flattening failed: %#v", p)
	}
	bad, e := a.Decode(s, Request{Kind: "jira-item"}, Response{Status: 200, Body: fixture(t, "jira-cross-project.json")})
	if e != nil {
		t.Fatal(e)
	}
	if bad.Complete || len(bad.Items) != 0 || len(bad.Diagnostics) == 0 {
		t.Fatalf("cross project item accepted: %#v", bad)
	}
}

func TestJiraRelationsRequireIssueAndProjectIdentity(t *testing.T) {
	s := Source{Provider: "jira", Origin: "https://example.atlassian.net", APIVersion: "3", NativeTenantID: "cloud", ContainerID: "10001", ContainerKey: "ENG"}
	adapter := NewJira()
	good := []byte(`{"id":"issue-1","key":"ENG-1","fields":{"project":{"id":"10001","key":"ENG"},"issuelinks":[]}}`)
	valid, err := adapter.Decode(s, Request{Kind: "jira-relations", ItemID: "issue-1"}, Response{Status: 200, Body: good})
	if err != nil || !valid.Complete || len(valid.Items) != 1 {
		t.Fatalf("in-scope relations rejected: %#v %v", valid, err)
	}
	for _, body := range [][]byte{
		[]byte(`{"id":"other-issue","key":"ENG-1","fields":{"project":{"id":"10001","key":"ENG"},"issuelinks":[]}}`),
		[]byte(`{"id":"issue-1","key":"ENG-1","fields":{"project":{"id":"99999","key":"ENG"},"issuelinks":[]}}`),
		[]byte(`{"id":"issue-1","key":"ENG-1","fields":{"project":{"id":"10001","key":"OTHER"},"issuelinks":[]}}`),
	} {
		result, err := adapter.Decode(s, Request{Kind: "jira-relations", ItemID: "issue-1"}, Response{Status: 200, Body: body})
		if err != nil || result.Complete || len(result.Items) != 0 || len(result.Diagnostics) == 0 {
			t.Fatalf("out-of-scope Jira relations accepted: %#v %v", result, err)
		}
	}
}

func TestJiraADFFlatteningMarksDiscardedFormattingAndAttributesLossy(t *testing.T) {
	doc := map[string]any{"type": "doc", "content": []any{
		map[string]any{"type": "paragraph", "content": []any{
			map[string]any{"type": "text", "text": "linked", "marks": []any{map[string]any{"type": "link", "attrs": map[string]any{"href": "https://example.invalid"}}}},
			map[string]any{"type": "mention", "attrs": map[string]any{"id": "account-1"}},
			map[string]any{"type": "inlineCard", "attrs": map[string]any{"url": "https://example.invalid/card"}},
		}},
	}}
	text, lossy := adfText(doc)
	if !lossy || !strings.Contains(text, "linked") {
		t.Fatalf("ADF dropped formatting/identity without loss marker: text=%q lossy=%v", text, lossy)
	}
}

func TestJiraProjectFieldCatalogPaginatesWithinProject(t *testing.T) {
	s := Source{Provider: "jira", Origin: "https://example.atlassian.net", APIVersion: "3", NativeTenantID: "cloud", ContainerID: "10001", ContainerKey: "ENG"}
	requests, err := NewJira().Initial(s, "discover", nil)
	if err != nil || len(requests) != 3 || requests[2].Kind != "jira-fields" || !strings.Contains(requests[2].Path, "projectIds=10001") {
		t.Fatalf("project field discovery request missing or unscoped: %#v %v", requests, err)
	}
	first, err := NewJira().Decode(s, requests[2], Response{Status: 200, Body: []byte(`{"values":[{"id":"summary","name":"Summary"},{"id":"description","name":"Description"}],"startAt":0,"maxResults":2,"total":3,"isLast":false}`)})
	if err != nil || !first.Complete || len(first.Next) != 1 || first.Discovery["fields:0"] == nil || !strings.Contains(first.Next[0].Path, "startAt=2") {
		t.Fatalf("first Jira field page did not advance: %#v %v", first, err)
	}
	last, err := NewJira().Decode(s, first.Next[0], Response{Status: 200, Body: []byte(`{"values":[{"id":"status","name":"Status"}],"startAt":2,"maxResults":2,"total":3,"isLast":true}`)})
	if err != nil || !last.Complete || len(last.Next) != 0 || last.Discovery["fields:2"] == nil {
		t.Fatalf("last Jira field page failed: %#v %v", last, err)
	}
	if _, err := NewJira().Decode(s, requests[2], Response{Status: 200, Body: []byte(`{"values":[],"startAt":0,"maxResults":50,"total":3,"isLast":false}`)}); err == nil {
		t.Fatal("non-advancing Jira field page accepted")
	}
	bad, err := NewJira().Decode(s, requests[2], Response{Status: 200, Body: []byte(`{"values":null,"startAt":0,"maxResults":50,"total":0,"isLast":true}`)})
	if err != nil || bad.Complete || len(bad.Diagnostics) == 0 {
		t.Fatalf("null Jira field values treated as complete: %#v %v", bad, err)
	}
}

func TestLinearConnectionsRequireExplicitHasNextPage(t *testing.T) {
	s := Source{Provider: "linear", NativeTenantID: "w", ContainerID: "team"}
	cases := []struct {
		kind string
		body string
	}{
		{"linear-metadata", `{"data":{"organization":{"id":"w"},"team":{"id":"team","states":{"nodes":[],"pageInfo":{}},"labels":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`},
		{"linear-states", `{"data":{"organization":{"id":"w"},"team":{"id":"team","states":{"nodes":[],"pageInfo":{}}}}}`},
		{"linear-labels", `{"data":{"organization":{"id":"w"},"team":{"id":"team","labels":{"nodes":[],"pageInfo":{"hasNextPage":"false"}}}}}`},
		{"linear-issue-labels", `{"data":{"organization":{"id":"w"},"issue":{"id":"i1","team":{"id":"team"},"labels":{"nodes":[],"pageInfo":{}}}}}`},
		{"linear-item", `{"data":{"organization":{"id":"w"},"issue":{"id":"i1","identifier":"ENG-1","team":{"id":"team"},"labels":{"nodes":[],"pageInfo":{}}}}}`},
		{"linear-items", `{"data":{"organization":{"id":"w"},"issues":{"nodes":[],"pageInfo":{}}}}`},
		{"linear-comments", `{"data":{"organization":{"id":"w"},"issue":{"id":"i1","team":{"id":"team"},"comments":{"nodes":[],"pageInfo":{}}}}}`},
		{"linear-relations", `{"data":{"organization":{"id":"w"},"issue":{"id":"i1","team":{"id":"team"},"relations":{"nodes":[],"pageInfo":{}}}}}`},
	}
	for _, tc := range cases {
		result, err := NewLinear().Decode(s, Request{Kind: tc.kind, ItemID: "i1"}, Response{Status: 200, Body: []byte(tc.body)})
		if err != nil || result.Complete || len(result.Diagnostics) == 0 {
			t.Errorf("%s accepted missing or invalid hasNextPage: %#v %v", tc.kind, result, err)
		}
	}
}

func TestProviderHardening(t *testing.T) {
	ls := Source{Provider: "linear", Origin: "https://api.linear.app", APIVersion: "graphql", NativeTenantID: "w", ContainerID: "team", Scope: Scope{ItemIDs: []string{"issue-id"}}}
	lr, e := NewLinear().Initial(ls, "capture", nil)
	if e != nil || len(lr) != 1 || lr[0].Kind != "linear-item" || !strings.Contains(string(lr[0].Body), "issue-id") {
		t.Fatalf("Linear explicit scope ignored: %#v %v", lr, e)
	}
	if _, e = NewLinear().Initial(Source{Provider: "linear", Origin: "https://api.linear.app", APIVersion: "graphql", NativeTenantID: "w", ContainerID: "team", Scope: Scope{Query: "anything"}}, "capture", nil); e == nil {
		t.Fatal("unsupported Linear query silently ignored")
	}
	wrongTenant, e := NewLinear().Decode(Source{Provider: "linear", NativeTenantID: "w", ContainerID: "team"}, Request{Kind: "linear-item", ItemID: "issue-id"}, Response{Status: 200, Body: []byte(`{"data":{"organization":{"id":"other-workspace"},"issue":{"id":"issue-id","identifier":"ENG-1","team":{"id":"team"}}}}`)})
	if e != nil || wrongTenant.Complete || len(wrongTenant.Items) != 0 || len(wrongTenant.Diagnostics) == 0 {
		t.Fatalf("Linear workspace mismatch accepted: %#v %v", wrongTenant, e)
	}
	wrongTeam, e := NewLinear().Decode(Source{Provider: "linear", NativeTenantID: "w", ContainerID: "team"}, Request{Kind: "linear-items"}, Response{Status: 200, Body: []byte(`{"data":{"organization":{"id":"w"},"issues":{"nodes":[{"id":"outside","identifier":"ENG-2","team":{"id":"other"}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`)})
	if e != nil || wrongTeam.Complete || len(wrongTeam.Items) != 0 || len(wrongTeam.Diagnostics) == 0 {
		t.Fatalf("wrong-team Linear row did not make capture incomplete: %#v %v", wrongTeam, e)
	}
	auxWrongTeam, e := NewLinear().Decode(Source{Provider: "linear", NativeTenantID: "w", ContainerID: "team"}, Request{Kind: "linear-comments", ItemID: "issue-id"}, Response{Status: 200, Body: []byte(`{"data":{"organization":{"id":"w"},"issue":{"team":{"id":"other"},"comments":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)})
	if e != nil || auxWrongTeam.Complete || len(auxWrongTeam.Diagnostics) == 0 {
		t.Fatalf("wrong-team Linear auxiliary read accepted: %#v %v", auxWrongTeam, e)
	}
	for _, kind := range []string{"linear-comments", "linear-relations"} {
		collection := strings.TrimPrefix(kind, "linear-")
		body := []byte(`{"data":{"organization":{"id":"w"},"issue":{"id":"different-issue","team":{"id":"team"},"` + collection + `":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`)
		wrongIssue, err := NewLinear().Decode(Source{Provider: "linear", NativeTenantID: "w", ContainerID: "team"}, Request{Kind: kind, ItemID: "issue-id"}, Response{Status: 200, Body: body})
		if err != nil || wrongIssue.Complete || len(wrongIssue.Items) != 0 || len(wrongIssue.Diagnostics) == 0 || wrongIssue.Diagnostics[0].Field != "issue.id" {
			t.Fatalf("wrong issue accepted for Linear %s: %#v %v", kind, wrongIssue, err)
		}
	}
	partial := []byte(`{"data":{"organization":{"id":"w"},"issues":{"nodes":[{"id":"i1","identifier":"ENG-1","team":{"id":"team"},"labels":{"nodes":[],"pageInfo":{"hasNextPage":true,"endCursor":"label-cursor"}}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`)
	pr, e := NewLinear().Decode(Source{Provider: "linear", NativeTenantID: "w", ContainerID: "team"}, Request{Kind: "linear-items"}, Response{Status: 200, Body: partial})
	if e != nil || !pr.Complete || len(pr.Next) != 1 || len(pr.Items) != 1 || len(pr.Items[0].LossyFields) != 1 || pr.Items[0].LossyFields[0] != "labelsConnection" {
		t.Fatalf("nested labels pagination not represented: %#v %v", pr, e)
	}
	js := Source{Provider: "jira", Origin: "https://example.atlassian.net", APIVersion: "3", NativeTenantID: "cloud", ContainerID: "10001", ContainerKey: "ENG"}
	jr, e := NewJira().Decode(js, Request{Kind: "jira-search", Path: "/rest/api/3/search/jql?jql=x&nextPageToken=old"}, Response{Status: 200, Body: []byte(`{"issues":[],"nextPageToken":"new"}`)})
	if e != nil || len(jr.Next) != 1 || strings.Count(jr.Next[0].Path, "nextPageToken=") != 1 || !strings.Contains(jr.Next[0].Path, "new") {
		t.Fatalf("Jira cursor not replaced: %#v %v", jr, e)
	}
	if _, e = NewJira().Initial(Source{Provider: "jira", Origin: "https://jira.internal", APIVersion: "3", NativeTenantID: "c", ContainerID: "p", ContainerKey: "ENG"}, "capture", nil); e == nil {
		t.Fatal("Data Center origin accepted")
	}
	missing, e := NewJira().Decode(js, Request{Kind: "jira-item", ItemID: "missing"}, Response{Status: 200, Body: []byte(`{}`)})
	if e != nil || missing.Complete || len(missing.Items) != 0 || len(missing.Diagnostics) == 0 {
		t.Fatalf("missing Jira record not reported: %#v %v", missing, e)
	}
	for _, body := range [][]byte{[]byte(`{}`), []byte(`{"issues":null}`), []byte(`{"issues":[{"id":"1","key":"ENG-1","fields":{"project":{"id":"10001","key":"OTHER"}}}]}`)} {
		r, e := NewJira().Decode(js, Request{Kind: "jira-search", Path: "/rest/api/3/search/jql"}, Response{Status: 200, Body: body})
		if e != nil || r.Complete || len(r.Diagnostics) == 0 {
			t.Fatalf("missing/out-of-scope Jira search treated as complete: %#v %v", r, e)
		}
	}
	wrongKey := []byte(`{"id":"20001","key":"OTHER-1","fields":{"project":{"id":"10001","key":"OTHER"}}}`)
	wrong, e := NewJira().Decode(js, Request{Kind: "jira-item", ItemID: "20001"}, Response{Status: 200, Body: wrongKey})
	if e != nil || len(wrong.Items) != 0 || len(wrong.Diagnostics) == 0 {
		t.Fatalf("Jira project ID match masked key mismatch: %#v %v", wrong, e)
	}
}
