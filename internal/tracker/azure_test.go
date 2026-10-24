package tracker

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
)

func azureSource() Source {
	return Source{Version: Version, Provider: "azure-devops", Origin: "https://dev.azure.com", APIVersion: "7.1", NativeTenantID: "tenant-guid", ContainerID: "project-guid", ContainerKey: "org/project"}
}

func TestAzureInitialChunksExplicitIDsAndRejectsUnsafe(t *testing.T) {
	a := NewAzure()
	s := azureSource()
	s.Scope.ItemIDs = make([]string, 401)
	for i := range s.Scope.ItemIDs {
		s.Scope.ItemIDs[i] = fmt.Sprint(i + 1)
	}
	rs, err := a.Initial(s, "capture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 4 || rs[0].Kind != "azure-project" {
		t.Fatalf("project identity check must precede 3 batches, got %#v", rs)
	}
	for i, want := range []int{200, 200, 1} {
		r := rs[i+1]
		var b struct {
			IDs         []int  `json:"ids"`
			ErrorPolicy string `json:"errorPolicy"`
		}
		if r.Method != "POST" || r.Kind != "azure-items" || json.Unmarshal(r.Body, &b) != nil || len(b.IDs) != want {
			t.Fatalf("bad chunk %d: %#v", i, r)
		}
		if b.ErrorPolicy != "Omit" || strings.Contains(r.Path, "$expand") {
			t.Fatalf("batch omission policy or query parameters misplaced: request=%#v body=%s", r, r.Body)
		}
	}
	s.Scope.ItemIDs = []string{"1/../2"}
	if _, err = a.Initial(s, "capture", nil); err == nil {
		t.Fatal("unsafe item id accepted")
	}
}

func TestAzureBatchExpansionIsInRequestBody(t *testing.T) {
	s := azureSource()
	s.Scope.ItemIDs = []string{"42"}
	s.Scope.Relations = true
	requests, err := NewAzure().Initial(s, "capture", nil)
	if err != nil || len(requests) != 2 || requests[0].Kind != "azure-project" {
		t.Fatalf("explicit-ID batch request missing: %#v %v", requests, err)
	}
	var body struct {
		IDs         []int  `json:"ids"`
		Expand      string `json:"$expand"`
		ErrorPolicy string `json:"errorPolicy"`
	}
	if err := json.Unmarshal(requests[1].Body, &body); err != nil || body.Expand != "Relations" || body.ErrorPolicy != "Omit" || len(body.IDs) != 1 || body.IDs[0] != 42 {
		t.Fatalf("Azure batch options must use the documented JSON body: %#v err=%v", body, err)
	}
	if strings.Contains(requests[1].Path, "$expand") {
		t.Fatalf("$expand should be in the batch JSON body: %s", requests[1].Path)
	}

	s.Scope.ItemIDs = nil
	s.Scope.Query = "SELECT [System.Id] FROM WorkItems WHERE [System.State] = 'Active'"
	wiql, err := NewAzure().Initial(s, "capture", nil)
	if err != nil || len(wiql) != 2 || wiql[0].Kind != "azure-project" {
		t.Fatal(err)
	}
	result, err := NewAzure().Decode(s, wiql[1], Response{Status: 200, Body: []byte(`{"workItems":[{"id":42}]}`)})
	if err != nil || len(result.Next) != 1 {
		t.Fatalf("WIQL result batch missing: %#v %v", result, err)
	}
	var queryBody struct {
		Expand      string `json:"$expand"`
		ErrorPolicy string `json:"errorPolicy"`
	}
	if err := json.Unmarshal(result.Next[0].Body, &queryBody); err != nil || queryBody.Expand != "Relations" || queryBody.ErrorPolicy != "Omit" || strings.Contains(result.Next[0].Path, "$expand") {
		t.Fatalf("WIQL work item batch options misplaced: request=%#v body=%#v err=%v", result.Next[0], queryBody, err)
	}
}

func TestAzureDecodePreservesRawFieldsRevisionAndReportsMissingIDs(t *testing.T) {
	a := NewAzure()
	s := azureSource()
	req := Request{Method: "POST", Kind: "azure-items", ItemID: "42,43", Path: "/org/project/_apis/wit/workitemsbatch?api-version=7.1"}
	body, err := os.ReadFile("testdata/azure-item.json")
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.Decode(s, req, Response{Status: 200, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if r.Complete || len(r.Items) != 1 || len(r.Diagnostics) != 1 {
		t.Fatalf("incomplete result expected: %#v", r)
	}
	i := r.Items[0]
	if i.Revision != "7" || i.RevisionKind != "azure-work-item-rev" || i.Identity.ContainerID != "project-guid" || i.Identity.TenantID != TenantScopeID(s) || i.NativeTenantID != s.NativeTenantID || string(i.Fields["Custom.Exact"]) != "900719925474099312345" {
		t.Fatalf("raw data lost: %#v", i)
	}
}

func TestAzureDecodeRejectsForeignTeamProject(t *testing.T) {
	s := azureSource()
	req := Request{Method: "POST", Kind: "azure-items", ItemID: "42"}
	body := []byte(`{"value":[{"id":42,"rev":1,"fields":{"System.TeamProject":"other-project","System.WorkItemType":"Bug","Custom.Secret":"must not be retained"}}]}`)
	if _, err := NewAzure().Decode(s, req, Response{Status: 200, Body: body}); err == nil || !strings.Contains(err.Error(), "outside configured project") {
		t.Fatalf("foreign project item was not rejected: %v", err)
	}
}

func TestAzureDecodeRejectsUnrequestedAndUnscopedBatchItems(t *testing.T) {
	s := azureSource()
	req := Request{Method: "POST", Kind: "azure-items", ItemID: "42"}
	extra := []byte(`{"value":[{"id":42,"rev":1,"fields":{"System.TeamProject":"project","System.Title":"requested"}},{"id":43,"rev":1,"fields":{"System.TeamProject":"project","System.Title":"unrequested"}}]}`)
	if _, err := NewAzure().Decode(s, req, Response{Status: 200, Body: extra}); err == nil || !strings.Contains(err.Error(), "unrequested") {
		t.Fatalf("extra same-project item was not rejected: %v", err)
	}
	missingProject := []byte(`{"value":[{"id":42,"rev":1,"fields":{"System.WorkItemType":"Bug","System.Title":"unknown scope"}}]}`)
	if _, err := NewAzure().Decode(s, req, Response{Status: 200, Body: missingProject}); err == nil || !strings.Contains(err.Error(), "missing its project identity") {
		t.Fatalf("item with unknown project scope was accepted: %v", err)
	}
}

func TestAzureDiscoverIgnoresExplicitItemIDsAndCaptureIncludesProjectMetadata(t *testing.T) {
	a := NewAzure()
	s := azureSource()
	s.Scope.ItemIDs = []string{"42"}
	discovery, err := a.Initial(s, "discover", nil)
	if err != nil || len(discovery) != 4 {
		t.Fatalf("discover should return metadata: %#v %v", discovery, err)
	}
	s.Scope.Projects = true
	capture, err := a.Initial(s, "capture", nil)
	if err != nil || len(capture) != 2 || capture[0].Kind != "azure-project" {
		t.Fatalf("capture should include project metadata: %#v %v", capture, err)
	}
}

func TestAzureRejectsDuplicateJSONKeysAndPaginatesComments(t *testing.T) {
	a := NewAzure()
	s := azureSource()
	if _, err := a.Decode(s, Request{Kind: "azure-project"}, Response{Status: 200, Body: []byte(`{"id":"a","id":"b","name":"project"}`)}); err == nil {
		t.Fatal("duplicate JSON key accepted")
	}
	req := Request{Kind: "azure-comments", ItemID: "42"}
	r, err := a.Decode(s, req, Response{Status: 200, Body: []byte(`{"comments":[],"continuationToken":"next page/1"}`)})
	if err != nil || len(r.Next) != 1 || !strings.Contains(r.Next[0].Path, "continuationToken=next+page%2F1") {
		t.Fatalf("comment continuation missing: %#v %v", r, err)
	}
}

func TestAzureWIQLProducesOnlyBoundedReadRequests(t *testing.T) {
	a := NewAzure()
	s := azureSource()
	s.Scope.Query = "SELECT [System.Id] FROM WorkItems WHERE [System.State] = 'Active'"
	rs, err := a.Initial(s, "capture", nil)
	if err != nil || len(rs) != 2 || rs[1].Method != "POST" || !strings.Contains(rs[1].Path, "wit/wiql") {
		t.Fatalf("unexpected WIQL request: %#v %v", rs, err)
	}
	if strings.Contains(strings.ToUpper(rs[1].Path), "UPDATE") {
		t.Fatal("non-read endpoint generated")
	}
	s.Scope.Query = "SELECT [System.Id] FROM WorkItems; DELETE FROM WorkItems"
	if _, err = a.Initial(s, "capture", nil); err == nil {
		t.Fatal("multi-statement WIQL accepted")
	}
	resp := Response{Status: 200, Body: []byte(`{"workItems":[{"id":42},{"id":43}]}`)}
	out, err := a.Decode(s, rs[1], resp)
	if err != nil || len(out.Next) != 1 || out.Next[0].ItemID != "42,43" {
		t.Fatalf("WIQL refs not batched: %#v %v", out, err)
	}
}

func TestAzureScopeValidationAndProjectMismatch(t *testing.T) {
	a := NewAzure()
	s := azureSource()
	s.ContainerKey = "org/project/other"
	if _, err := a.Initial(s, "discover", nil); err == nil {
		t.Fatal("malformed container key accepted")
	}
	s = azureSource()
	r, err := a.Decode(s, Request{Kind: "azure-project"}, Response{Status: 200, Body: []byte(`{"id":"x","name":"other"}`)})
	if err != nil || r.Complete || len(r.Diagnostics) == 0 {
		t.Fatalf("scope mismatch should be incomplete: %#v %v", r, err)
	}
	wrongGUID, err := a.Decode(s, Request{Kind: "azure-project"}, Response{Status: 200, Body: []byte(`{"id":"other-guid","name":"project"}`)})
	if err != nil || wrongGUID.Complete || len(wrongGUID.Discovery) != 0 || len(wrongGUID.Diagnostics) == 0 {
		t.Fatalf("same-name project with wrong immutable GUID must fail closed: %#v %v", wrongGUID, err)
	}
}

func TestAzureRequestedCommentsProduceScopedPartialItems(t *testing.T) {
	a := NewAzure()
	s := azureSource()
	s.Scope.ItemIDs = []string{"42"}
	s.Scope.Comments = true
	reqs, err := a.Initial(s, "capture", nil)
	if err != nil {
		t.Fatal(err)
	}
	base, err := a.Decode(s, reqs[1], Response{Status: 200, Body: []byte(`{"value":[{"id":42,"rev":9,"fields":{"System.WorkItemType":"Bug","System.TeamProject":"project"}}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	if len(base.Next) != 1 || base.Next[0].Kind != "azure-comments" || !base.Items[0].Complete {
		t.Fatalf("comments should be scheduled without lowering base-item completeness: %#v", base)
	}
	comment, err := a.Decode(s, base.Next[0], Response{Status: 200, Body: []byte(`{"comments":[{"id":3,"text":"reviewed","modifiedDate":"2026-10-22T10:00:00Z"}]}`)})
	if err != nil || len(comment.Items) != 1 || comment.Items[0].Revision != "" || comment.Items[0].Comments[0].Body != "reviewed" {
		t.Fatalf("bad partial comments result: %#v %v", comment, err)
	}
	merged, conflict := mergeItem(base.Items[0], comment.Items[0])
	if conflict || !merged.Complete || len(merged.Fields) == 0 || merged.Comments[0].Body != "reviewed" {
		t.Fatalf("comments fragment should retain complete base record: %#v conflict=%v", merged, conflict)
	}
}

func TestAzureRejectsNullRequestedCollectionsAndPreservesUnknownRelations(t *testing.T) {
	a := NewAzure()
	s := azureSource()
	for _, tc := range []struct {
		kind string
		body string
	}{
		{"azure-items", `{"value":null}`},
		{"azure-wiql", `{"workItems":null}`},
		{"azure-comments", `{"comments":null}`},
		{"azure-types", `{"value":null}`},
		{"azure-processes", `{"value":null}`},
		{"azure-fields", `{"value":null}`},
		{"azure-states", `{"value":null}`},
	} {
		request := Request{Kind: tc.kind, ItemID: "42"}
		if tc.kind == "azure-items" {
			request.ItemID = "42"
		}
		if _, err := a.Decode(s, request, Response{Status: 200, Body: []byte(tc.body)}); err == nil {
			t.Errorf("%s accepted null requested collection", tc.kind)
		}
	}

	s.Scope.Relations = true
	item := []byte(`{"value":[{"id":42,"rev":2,"fields":{"System.TeamProject":"project"},"relations":[{"rel":"Custom.UnknownLinkType","url":"https://dev.azure.com/org/project/_apis/wit/workItems/43"}]}]}`)
	result, err := a.Decode(s, Request{Kind: "azure-items", ItemID: "42"}, Response{Status: 200, Body: item})
	if err != nil || len(result.Items) != 1 || len(result.Items[0].Relations) != 1 {
		t.Fatalf("unknown Azure relation was not retained: %#v %v", result, err)
	}
	relation := result.Items[0].Relations[0]
	if relation.Type != "Custom.UnknownLinkType" || relation.TargetID != "43" || relation.Supported {
		t.Fatalf("unknown relation type must remain raw and unsupported: %#v", relation)
	}
	// Azure omits relations when there are no links. An absent expansion is a
	// valid empty relation set, not evidence of an invented supported link.
	withoutLinks, err := a.Decode(s, Request{Kind: "azure-items", ItemID: "42"}, Response{Status: 200, Body: []byte(`{"value":[{"id":42,"rev":2,"fields":{"System.TeamProject":"project"}}]}`)})
	if err != nil || len(withoutLinks.Items) != 1 || len(withoutLinks.Items[0].Relations) != 0 {
		t.Fatalf("omitted empty relation expansion should remain an empty set: %#v %v", withoutLinks, err)
	}
}
