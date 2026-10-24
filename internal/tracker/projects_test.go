package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

func fixtureProjectPage(kind, variableID string, pageIndex int, response string, after string) (Page, []byte) {
	requestBody, _ := json.Marshal(map[string]any{"query": "query captured", "variables": map[string]string{"id": variableID, "after": after}})
	raw := []byte(response)
	assetPath := fmt.Sprintf("assets/project-%02d.json", pageIndex)
	return Page{Request: Request{Method: "POST", Path: "/graphql", Body: requestBody, Kind: kind}, Status: 200, Asset: structAsset(assetPath, raw), ObservedAt: "2026-10-24T12:00:00Z"}, raw
}

// structAsset keeps fixture pages independent of the capture engine.
func structAsset(path string, raw []byte) (asset contracts.AssetRef) {
	return contracts.AssetRef{Path: path, SHA256: Hash(raw)}
}

func TestJoinGitHubProjectsMapsCapturedFieldValuesAndProvenance(t *testing.T) {
	s := Source{Provider: "github"}
	projectPage, projectRaw := fixtureProjectPage("github-project-items", "PROJECT1", 0, `{"data":{"node":{"id":"PROJECT1","items":{"nodes":[{"id":"PVTI_1","content":{"id":"ISSUE1","number":1}},{"id":"PVTI_DRAFT","content":null}]}}}}`, "")
	fieldsPage, fieldsRaw := fixtureProjectPage("github-item-fields", "PVTI_1", 1, `{"data":{"node":{"id":"PVTI_1","fieldValues":{"nodes":[{"__typename":"ProjectV2ItemFieldTextValue","text":"In review","field":{"id":"FIELD_STATUS"}}]}}}}`, "")
	draftPage, draftRaw := fixtureProjectPage("github-item-fields", "PVTI_DRAFT", 2, `{"data":{"node":{"id":"PVTI_DRAFT","fieldValues":{"nodes":[{"__typename":"ProjectV2ItemFieldTextValue","text":"draft only","field":{"id":"FIELD_STATUS"}}]}}}}`, "")
	snapshot := Snapshot{Source: s, Pages: []Page{fieldsPage, draftPage, projectPage}, Items: []Item{{Identity: contracts.RemoteIdentity{Provider: "github", RemoteID: "ISSUE1", ItemKind: "issue"}, Fields: map[string]json.RawMessage{"title": json.RawMessage(`"Existing"`)}}}}
	assets := map[string][]byte{projectPage.Asset.Path: projectRaw, fieldsPage.Asset.Path: fieldsRaw, draftPage.Asset.Path: draftRaw}
	if err := joinGitHubProjects(&snapshot, assets); err != nil {
		t.Fatal(err)
	}
	key := Digest(struct{ ProjectID, FieldID string }{"PROJECT1", "FIELD_STATUS"})
	fieldID := "projectFields." + key
	raw, ok := mappedNativeField(snapshot.Items[0], fieldID)
	if !ok {
		t.Fatal("joined project field is not visible through mappedNativeField")
	}
	var wrapped projectFieldValue
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		t.Fatal(err)
	}
	if wrapped.ProjectID != "PROJECT1" || wrapped.FieldID != "FIELD_STATUS" || string(wrapped.Value) != `{"__typename":"ProjectV2ItemFieldTextValue","text":"In review","field":{"id":"FIELD_STATUS"}}` {
		t.Fatalf("native value wrapper lost traceability or raw node: %#v", wrapped)
	}
	pointers := snapshot.Items[0].Provenance[fieldID]
	if len(pointers) != 1 || pointers[0].AssetSHA256 != fieldsPage.Asset.SHA256 || pointers[0].JSONPointer != "/data/node/fieldValues/nodes/0" {
		t.Fatalf("native project field provenance missing: %#v", pointers)
	}
	if len(snapshot.Items) != 1 || len(snapshot.Items[0].ProjectFields) != 1 {
		t.Fatal("project join created a draft or foreign work item")
	}
	if value, ok := mappedNativeField(snapshot.Items[0], "title"); !ok || string(value) != `"Existing"` {
		t.Fatalf("ordinary native fields no longer resolve: %s %v", value, ok)
	}
}

func TestJoinGitHubProjectsRejectsSemanticConflicts(t *testing.T) {
	first, firstRaw := fixtureProjectPage("github-project-items", "PROJECT1", 0, `{"data":{"node":{"id":"PROJECT1","items":{"nodes":[{"id":"PVTI_1","content":{"id":"ISSUE1"}}]}}}}`, "")
	fieldsA, rawA := fixtureProjectPage("github-item-fields", "PVTI_1", 1, `{"data":{"node":{"id":"PVTI_1","fieldValues":{"nodes":[{"text":"Open","field":{"id":"FIELD_STATUS"}}]}}}}`, "")
	fieldsB, rawB := fixtureProjectPage("github-item-fields", "PVTI_1", 2, `{"data":{"node":{"id":"PVTI_1","fieldValues":{"nodes":[{"text":"Closed","field":{"id":"FIELD_STATUS"}}]}}}}`, "cursor-2")
	s := Snapshot{Source: Source{Provider: "github"}, Pages: []Page{first, fieldsA, fieldsB}, Items: []Item{{Identity: contracts.RemoteIdentity{Provider: "github", RemoteID: "ISSUE1"}}}}
	assets := map[string][]byte{first.Asset.Path: firstRaw, fieldsA.Asset.Path: rawA, fieldsB.Asset.Path: rawB}
	if err := joinGitHubProjects(&s, assets); err == nil || !strings.Contains(err.Error(), "conflicting GitHub project field values") {
		t.Fatalf("conflicting native field values were not rejected: %v", err)
	}
}

func TestJoinGitHubProjectsIgnoresNonGitHubSnapshotsAndRejectsChangedAssets(t *testing.T) {
	if err := joinGitHubProjects(&Snapshot{Source: Source{Provider: "jira"}}, nil); err != nil {
		t.Fatalf("non-GitHub snapshot should not be changed: %v", err)
	}
	page, raw := fixtureProjectPage("github-project-items", "PROJECT1", 0, `{"data":{"node":{"id":"PROJECT1","items":{"nodes":[]}}}}`, "")
	s := Snapshot{Source: Source{Provider: "github"}, Pages: []Page{page}}
	if err := joinGitHubProjects(&s, map[string][]byte{page.Asset.Path: append(raw, byte(' '))}); err == nil {
		t.Fatal("modified project discovery asset accepted")
	}
}

type projectsCaptureTransport struct {
	responses map[string][]byte
	requests  []Request
}

func (m *projectsCaptureTransport) Do(_ context.Context, _ Source, r Request) (Response, error) {
	if r.Method != "GET" && r.Method != "POST" {
		return Response{}, fmt.Errorf("unexpected non-read request %s", r.Method)
	}
	if err := ValidateRequest(projectsCaptureSource(), r); err != nil {
		return Response{}, err
	}
	body, ok := m.responses[r.Kind]
	if !ok {
		return Response{}, fmt.Errorf("unexpected request kind %s", r.Kind)
	}
	m.requests = append(m.requests, r)
	return Response{Status: 200, Headers: map[string]string{"etag": "fixture-etag"}, Body: append([]byte(nil), body...)}, nil
}

func projectsCaptureSource() Source {
	s := Source{Version: Version, Provider: "github", Origin: "https://api.github.com", APIVersion: "2022-11-28", NativeTenantID: "owner-node", ContainerID: "repo-node", ContainerKey: "acme/widgets", PrincipalPseudonym: "fixture-principal", Auth: Auth{Mode: "gh-session"}, Scope: Scope{ItemIDs: []string{"1"}, Projects: true}, MappingVersion: "mapping-v1"}
	return s
}

func TestCaptureProjectsProjectsFieldProjectionAndHumanDraft(t *testing.T) {
	s := projectsCaptureSource()
	transport := &projectsCaptureTransport{responses: map[string][]byte{
		"github-repository":     []byte(`{"id":1,"node_id":"repo-node","name":"widgets","owner":{"login":"acme","node_id":"owner-node"}}`),
		"github-item":           []byte(`{"id":7,"node_id":"issue-node-7","number":1,"title":"Fixture issue","body":"captured specification","state":"open","html_url":"https://github.com/acme/widgets/issues/1","repository_url":"https://api.github.com/repos/acme/widgets"}`),
		"github-projects":       []byte(`{"data":{"repository":{"id":"repo-node","projectsV2":{"nodes":[{"id":"PROJECT1","title":"Roadmap"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
		"github-project-fields": []byte(`{"data":{"node":{"id":"PROJECT1","fields":{"nodes":[{"__typename":"ProjectV2SingleSelectField","id":"FIELD_STATUS","name":"Status","options":[{"id":"OPT1","name":"In Progress"}]}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
		"github-project-items":  []byte(`{"data":{"node":{"id":"PROJECT1","items":{"nodes":[{"id":"PVTI_1","content":{"id":"issue-node-7","number":1}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
		"github-item-fields":    []byte(`{"data":{"node":{"id":"PVTI_1","fieldValues":{"nodes":[{"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"In Progress","optionId":"OPT1","field":{"id":"FIELD_STATUS"}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
	}}
	capture, err := CaptureSource(context.Background(), s, "capture", nil, transport)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := map[string]bool{"github-repository": false, "github-item": false, "github-projects": false, "github-project-fields": false, "github-project-items": false, "github-item-fields": false}
	for _, request := range transport.requests {
		if _, ok := wantKinds[request.Kind]; !ok {
			t.Fatalf("unexpected captured query kind %q", request.Kind)
		}
		wantKinds[request.Kind] = true
	}
	for kind, seen := range wantKinds {
		if !seen {
			t.Errorf("expected offline capture of %s", kind)
		}
	}
	if !capture.Snapshot.Complete || len(capture.Snapshot.Items) != 1 {
		t.Fatalf("capture should be complete with only the scoped issue: %#v", capture.Snapshot)
	}
	item := capture.Snapshot.Items[0]
	if got := string(item.Fields["state"]); got != `"open"` {
		t.Fatalf("project capture changed native issue state: %s", got)
	}
	fieldKey := Digest(struct{ ProjectID, FieldID string }{"PROJECT1", "FIELD_STATUS"})
	fieldID := "projectFields." + fieldKey
	joined, ok := mappedNativeField(item, fieldID)
	if !ok {
		t.Fatal("ProjectV2 field value was not joined to its captured issue")
	}
	var wrapped projectFieldValue
	if err := json.Unmarshal(joined, &wrapped); err != nil || wrapped.ProjectID != "PROJECT1" || wrapped.FieldID != "FIELD_STATUS" || string(wrapped.Value) != `{"__typename":"ProjectV2ItemFieldSingleSelectValue","name":"In Progress","optionId":"OPT1","field":{"id":"FIELD_STATUS"}}` {
		t.Fatalf("joined native value lost project/field identity or raw value: %#v err=%v", wrapped, err)
	}
	provenance := item.Provenance[fieldID]
	if len(provenance) != 1 || provenance[0].JSONPointer != "/data/node/fieldValues/nodes/0" {
		t.Fatalf("joined value lacks raw response provenance: %#v", provenance)
	}

	literal := func(v string) *json.RawMessage { raw := json.RawMessage(v); return &raw }
	mapping := Mapping{Version: Version, ID: "projects-fixture", Provider: "github", MappingVersion: s.MappingVersion, Rules: []FieldRule{
		{PacketField: "title", ProviderFieldID: "title", Owner: "human"},
		{PacketField: "spec", ProviderFieldID: "body", Owner: "human"},
		{PacketField: "status", ProviderFieldID: fieldID, Owner: "human", ValueMap: map[string]json.RawMessage{semantic(joined): json.RawMessage(`"inbox"`)}},
		{PacketField: "phase", Literal: literal(`"intake"`), Owner: "aok"},
		{PacketField: "acceptanceCriteria", Literal: literal(`["explicit acceptance"]`), Owner: "aok"},
		{PacketField: "verification.commands", Literal: literal(`["go test ./..."]`), Owner: "aok"},
	}}
	projection, err := Project(capture, mapping)
	if err != nil {
		t.Fatal(err)
	}
	if projection.Report.Status != "valid" {
		t.Fatalf("explicit native value mapping should yield a valid packet: %#v", projection.Report)
	}
	var packet map[string]any
	if err := json.Unmarshal(projection.Packets[item.Identity.RemoteID], &packet); err != nil {
		t.Fatal(err)
	}
	if packet["status"] != "inbox" {
		t.Fatalf("project field was not translated through explicit mapping: %#v", packet)
	}

	observed, err := time.Parse(time.RFC3339Nano, capture.Snapshot.ObservedAt)
	if err != nil {
		t.Fatal(err)
	}
	desired := Desired{Version: Version, Identity: item.Identity, ExpectedRevision: item.Revision, Fields: map[string]json.RawMessage{fieldID: json.RawMessage(`"Done"`)}}
	withoutIntent, err := PlanUpdate(capture, mapping, desired, observed.Add(time.Second))
	if err != nil || withoutIntent.Report.Status != "blocked" {
		t.Fatalf("human-owned project field draft without explicit intent should block: %#v %v", withoutIntent.Report, err)
	}
	desired.HumanOwnedFields = []string{fieldID}
	plan, err := PlanUpdate(capture, mapping, desired, observed.Add(time.Second))
	if err != nil || plan.Report.Status != "valid" {
		t.Fatalf("explicit human intent should produce a valid draft update plan: %#v %v", plan.Report, err)
	}
}
