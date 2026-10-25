package tracker

import (
	"encoding/json"
	"testing"
)

func TestGitHubGraphConnectionAndNodeScope(t *testing.T) {
	s := integrationSources()["github"]
	s.Scope.Projects = true
	req := ghGraph("github-project-fields", ghProjectFields, map[string]any{"id": "project-1", "after": ""})
	for _, raw := range []string{
		`{"data":{"node":{"id":"project-1","fields":null}}}`,
		`{"data":{"node":{"id":"project-1","fields":{}}}}`,
		`{"data":{"node":{"id":"project-1","fields":{"nodes":[],"pageInfo":{}}}}}`,
		`{"data":{"node":{"id":"project-2","fields":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`,
	} {
		if _, e := NewGitHub().Decode(s, req, Response{Status: 200, Body: []byte(raw)}); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	result, e := NewGitHub().Decode(s, req, Response{Status: 200, Body: []byte(`{"data":{"node":{"id":"project-1","fields":{"nodes":[],"pageInfo":{"hasNextPage":false}}}}}`)})
	if e != nil || !result.Complete {
		t.Fatalf("valid empty connection rejected: %v", e)
	}
}
func TestGitHubProjectChildOrderAndIssuePRSeparation(t *testing.T) {
	s := integrationSources()["github"]
	s.Scope.Projects = true
	r := githubProjectsRequest("acme", "widgets", "")
	raw := []byte(`{"data":{"repository":{"id":"repo-node","projectsV2":{"nodes":[{"id":"p1","title":"Roadmap"}],"pageInfo":{"hasNextPage":false}}}}}`)
	for i := 0; i < 10; i++ {
		p, e := NewGitHub().Decode(s, r, Response{Status: 200, Body: raw})
		if e != nil {
			t.Fatal(e)
		}
		if len(p.Next) != 2 || p.Next[0].Kind != "github-project-fields" || p.Next[1].Kind != "github-project-items" {
			t.Fatal("unstable child order")
		}
	}
	issue := Request{Method: "GET", Path: "/repos/acme/widgets/issues", Kind: "github-issues"}
	p, e := NewGitHub().Decode(s, issue, Response{Status: 200, Body: []byte(`[{"node_id":"issue-side-id","number":1,"pull_request":{"url":"x"}}]`)})
	if e != nil || len(p.Items) != 0 || len(p.Next) != 1 {
		t.Fatalf("PR issue was normalized as issue: %#v %v", p, e)
	}
	pr, e := NewGitHub().Decode(s, p.Next[0], Response{Status: 200, Body: []byte(`{"node_id":"pull-side-id","number":1,"merged":true,"title":"PR"}`)})
	if e != nil || len(pr.Items) != 1 || pr.Items[0].Identity.RemoteID != "pull-side-id" || pr.Items[0].Identity.ItemKind != "pull-request" {
		t.Fatal("PR native identity lost")
	}
	if _, ok := pr.Items[0].Fields["accepted"]; ok {
		t.Fatal("merged became accepted")
	}
	_ = json.Valid(raw)
}
func TestGitHubNullCollectionIsIncomplete(t *testing.T) {
	s := integrationSources()["github"]
	for _, kind := range []string{"github-issues", "github-comments", "github-timeline"} {
		if _, e := NewGitHub().Decode(s, Request{Kind: kind}, Response{Status: 200, Body: []byte("null")}); e == nil {
			t.Fatalf("null %s accepted", kind)
		}
	}
}
func TestGitHubTypedSelectionDoesNotWiden(t *testing.T) {
	s := integrationSources()["github"]
	s.Scope.ItemIDs = []string{"issue:1"}
	p := Request{Method: "GET", Path: "/repos/acme/widgets/pulls/1", Kind: "github-item", ItemID: "pr:1"}
	if ValidateRequest(s, p) == nil {
		t.Fatal("issue-only selection allowed PR endpoint")
	}
	p.Path = "/repos/acme/widgets/issues/1"
	p.ItemID = "issue:1"
	if _, e := NewGitHub().Decode(s, p, Response{Status: 200, Body: []byte(`{"node_id":"pr-issue-id","number":1,"pull_request":{}}`)}); e == nil {
		t.Fatal("issue-only response redirected to PR")
	}
	s.Scope.ItemIDs = []string{"pr:1"}
	if ValidateRequest(s, p) == nil {
		t.Fatal("PR-only selection allowed base issue endpoint")
	}
	p.Kind = "github-comments"
	p.Path += "/comments"
	p.ItemID = "pr-native"
	if ValidateRequest(s, p) == nil {
		t.Fatal("unbound auxiliary read accepted")
	}
	s.Scope.Comments = true
	parent, err := NewGitHub().Decode(s, Request{Method: "GET", Path: "/repos/acme/widgets/pulls/1", Kind: "github-item", ItemID: "pr:1"}, Response{Status: 200, Body: []byte(`{"node_id":"pr-native","number":1,"title":"PR"}`)})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, next := range parent.Next {
		if next.Kind == "github-comments" {
			p = next
			found = true
		}
	}
	if !found {
		t.Fatal("parent response did not produce bound comment read")
	}
	if e := ValidateRequest(s, p); e != nil {
		t.Fatal(e)
	}
}
