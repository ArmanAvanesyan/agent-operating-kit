package tracker

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// offlineTransport is deliberately closed over fixture responses. Any unplanned
// request fails the test, so these tests cannot fall through to a live account.
type offlineTransport struct {
	responses map[string][]byte
	requests  []Request
	failKind  string
}

func (m *offlineTransport) Do(_ context.Context, _ Source, r Request) (Response, error) {
	if r.Method != "GET" && r.Method != "POST" {
		return Response{}, errors.New("non-read request")
	}
	if strings.HasPrefix(r.Path, "http://") || strings.HasPrefix(r.Path, "https://") || !strings.HasPrefix(r.Path, "/") {
		return Response{}, errors.New("request path is not origin-relative")
	}
	if strings.Contains(strings.ToLower(string(r.Body)), "mutation ") {
		return Response{}, errors.New("GraphQL mutation prohibited")
	}
	m.requests = append(m.requests, r)
	if r.Kind == m.failKind {
		return Response{}, errors.New("offline fixture interruption")
	}
	b, ok := m.responses[r.Kind]
	if !ok {
		return Response{}, errors.New("unplanned offline request: " + r.Kind)
	}
	return Response{Status: 200, Headers: map[string]string{"etag": "fixture-etag"}, Body: append([]byte(nil), b...)}, nil
}

func integrationSources() map[string]Source {
	common := func(provider, origin, api, tenant, container, key string, auth Auth) Source {
		return Source{Version: Version, Provider: provider, Origin: origin, APIVersion: api, NativeTenantID: tenant, ContainerID: container, ContainerKey: key, PrincipalPseudonym: "test-principal", Auth: auth, Scope: Scope{ItemIDs: []string{"1"}}, MappingVersion: "map-v1"}
	}
	return map[string]Source{
		"github":       common("github", "https://api.github.com", "2022-11-28", "owner-node", "repo-node", "acme/widgets", Auth{Mode: "gh-session"}),
		"azure-devops": common("azure-devops", "https://dev.azure.com", "7.1", "tenant-guid", "project-guid", "acme/project", Auth{Mode: "bearer", TokenEnv: "AOK_TEST_TOKEN"}),
		"linear":       common("linear", "https://api.linear.app", "graphql", "workspace-guid", "team-guid", "ENG", Auth{Mode: "bearer", TokenEnv: "AOK_TEST_TOKEN"}),
		"jira":         common("jira", "https://acme.atlassian.net", "3", "cloud-guid", "10001", "ENG", Auth{Mode: "bearer", TokenEnv: "AOK_TEST_TOKEN"}),
	}
}

func integrationResponses(provider string) map[string][]byte {
	switch provider {
	case "github":
		return map[string][]byte{
			"github-repository": []byte(`{"id":1,"node_id":"repo-node","name":"widgets","owner":{"login":"acme","node_id":"owner-node"}}`),
			"github-item":       []byte(`{"id":7,"node_id":"issue-node-7","number":1,"title":"Fixture issue","body":"captured","state":"open","html_url":"https://github.com/acme/widgets/issues/1"}`),
			"github-comments":   []byte(`[]`), "github-timeline": []byte(`[]`),
		}
	case "azure-devops":
		return map[string][]byte{
			"azure-project":  []byte(`{"id":"project-guid","name":"project"}`),
			"azure-items":    []byte(`{"value":[{"id":1,"rev":2,"url":"https://dev.azure.com/acme/project/_apis/wit/workItems/1","fields":{"System.TeamProject":"project","System.WorkItemType":"Bug","System.Title":"Fixture issue","System.Description":"captured"}}]}`),
			"azure-comments": []byte(`{"comments":[{"id":2,"text":"reviewed","modifiedDate":"2026-10-22T10:00:00Z"}]}`),
		}
	case "linear":
		return map[string][]byte{
			"linear-item":      []byte(`{"data":{"organization":{"id":"workspace-guid"},"issue":{"id":"1","identifier":"ENG-1","url":"https://linear.app/acme/issue/ENG-1","title":"Fixture issue","description":"captured","team":{"id":"team-guid","key":"ENG"},"labels":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}},"updatedAt":"2026-10-22T10:00:00Z"}}}`),
			"linear-comments":  []byte(`{"data":{"organization":{"id":"workspace-guid"},"issue":{"id":"1","team":{"id":"team-guid"},"comments":{"nodes":[{"id":"comment-1","body":"reviewed","updatedAt":"2026-10-22T10:00:00Z"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
			"linear-relations": []byte(`{"data":{"organization":{"id":"workspace-guid"},"issue":{"id":"1","team":{"id":"team-guid"},"relations":{"nodes":[],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`),
		}
	case "jira":
		return map[string][]byte{
			"jira-item":        []byte(`{"id":"1","key":"ENG-1","self":"https://acme.atlassian.net/rest/api/3/issue/1","fields":{"project":{"id":"10001","key":"ENG"},"summary":"Fixture issue","description":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"captured"}]}]},"updated":"2026-10-22T10:00:00.000+0000"}}`),
			"jira-comments":    []byte(`{"startAt":0,"maxResults":50,"total":1,"comments":[{"id":"comment-1","body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"reviewed"}]}]},"updated":"2026-10-22T10:00:00.000+0000"}]}`),
			"jira-relations":   []byte(`{"id":"1","key":"ENG-1","fields":{"project":{"id":"10001","key":"ENG"},"issuelinks":[]}}`),
			"jira-transitions": []byte(`{"transitions":[{"id":"t1","name":"Open","to":{"id":"1","name":"Open"},"fields":{"resolution":{"required":true}}}]}`),
		}
	}
	return nil
}

func writeCapture(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for name, b := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
}

func TestOfflineProviderCaptureImportRoundTrip(t *testing.T) {
	for provider, source := range integrationSources() {
		t.Run(provider, func(t *testing.T) {
			mock := &offlineTransport{responses: integrationResponses(provider)}
			captured, err := CaptureSource(context.Background(), source, "capture", nil, mock)
			if err != nil {
				t.Fatal(err)
			}
			if !ValidCapture(captured) {
				t.Fatal("capture seal invalid")
			}
			if !captured.Snapshot.Complete || captured.Snapshot.Report.Status != "valid" || len(captured.Snapshot.Items) != 1 {
				t.Fatalf("capture not complete: status=%s items=%d diagnostics=%#v", captured.Snapshot.Report.Status, len(captured.Snapshot.Items), captured.Snapshot.Report.Diagnostics)
			}
			for _, r := range mock.requests {
				if r.Method != "GET" && r.Method != "POST" {
					t.Fatalf("non-read request: %#v", r)
				}
				if strings.HasPrefix(r.Path, "http") {
					t.Fatalf("absolute target: %s", r.Path)
				}
			}
			item := captured.Snapshot.Items[0]
			if item.Identity.Provider != provider || item.Identity.TenantID != TenantScopeID(source) || item.Identity.ContainerID != source.ContainerID || len(item.Provenance) == 0 {
				t.Fatalf("identity/provenance missing: %#v", item)
			}
			root := t.TempDir()
			writeCapture(t, root, captured.Files)
			imported, err := Import(filepath.Join(root, "manifest.json"))
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if imported.Snapshot.Digest != captured.Snapshot.Digest || loaded.Snapshot.Digest != captured.Snapshot.Digest {
				t.Fatal("import/load changed the captured snapshot")
			}
			var manifest Manifest
			if err := json.Unmarshal(captured.Files["manifest.json"], &manifest); err != nil {
				t.Fatal(err)
			}
			if len(manifest.Pages) != len(mock.requests) {
				t.Fatalf("planned request count mismatch: manifest=%d transport=%d", len(manifest.Pages), len(mock.requests))
			}
		})
	}
}

func TestOfflineJiraProjectDiscoveryIsScopedAndTyped(t *testing.T) {
	s := integrationSources()["jira"]
	valid := map[string][]byte{
		"jira-project": []byte(`{"id":"10001","key":"ENG","name":"Engineering"}`),
		"jira-types":   []byte(`[{"id":"10002","name":"Bug","subtask":false},{"id":"10003","name":"Task","subtask":false}]`),
		"jira-fields":  []byte(`{"values":[{"id":"summary","name":"Summary","schema":{"type":"string"}}],"startAt":0,"maxResults":50,"total":1,"isLast":true}`),
	}
	captured, err := CaptureSource(context.Background(), s, "discover", nil, &offlineTransport{responses: valid})
	if err != nil {
		t.Fatal(err)
	}
	if captured.Snapshot.Report.Status != "valid" || !captured.Snapshot.Complete || len(captured.Snapshot.Discovery) < 3 {
		t.Fatalf("valid Jira project discovery failed: %#v", captured.Snapshot)
	}
	for _, test := range []struct {
		name      string
		responses map[string][]byte
	}{
		{"wrong-project", map[string][]byte{"jira-project": []byte(`{"id":"99999","key":"ENG"}`), "jira-types": valid["jira-types"], "jira-fields": valid["jira-fields"]}},
		{"null-types", map[string][]byte{"jira-project": valid["jira-project"], "jira-types": []byte(`null`), "jira-fields": valid["jira-fields"]}},
	} {
		t.Run(test.name, func(t *testing.T) {
			blocked, err := CaptureSource(context.Background(), s, "discover", nil, &offlineTransport{responses: test.responses})
			if err != nil {
				if test.name == "wrong-project" {
					return // A mismatched immutable identity aborts bundle construction.
				}
				t.Fatal(err)
			}
			if blocked.Snapshot.Report.Status != "blocked" || blocked.Snapshot.Complete {
				t.Fatalf("invalid Jira metadata discovery was accepted: %#v", blocked.Snapshot)
			}
		})
	}
}

func TestOfflineDuplicatePagesReorderAndCheckpointDrift(t *testing.T) {
	s := integrationSources()["github"]
	mock := &offlineTransport{responses: integrationResponses("github")}
	c, err := CaptureSource(context.Background(), s, "capture", nil, mock)
	if err != nil {
		t.Fatal(err)
	}
	var manifest Manifest
	if err = json.Unmarshal(c.Files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	// A repeated identical request/asset is safely deduplicated during import.
	manifest.Pages = append(manifest.Pages, manifest.Pages[0])
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	files := map[string][]byte{}
	for k, v := range c.Files {
		files[k] = v
	}
	files["manifest.json"] = append(mb, '\n')
	root := t.TempDir()
	writeCapture(t, root, files)
	dup, err := Import(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dup.Snapshot.Items) != 1 {
		t.Fatalf("duplicate page duplicated item: %d", len(dup.Snapshot.Items))
	}
	// Reordered source pages remain importable and normalize to the same item set.
	manifest.Pages[0], manifest.Pages[1] = manifest.Pages[1], manifest.Pages[0]
	mb, _ = json.MarshalIndent(manifest, "", "  ")
	files["manifest.json"] = append(mb, '\n')
	root2 := t.TempDir()
	writeCapture(t, root2, files)
	reordered, err := Import(filepath.Join(root2, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(reordered.Snapshot.Items) != 1 || reordered.Snapshot.Items[0].Identity.RemoteID != c.Snapshot.Items[0].Identity.RemoteID {
		t.Fatal("reordered pages changed normalized item identity")
	}
	// An interrupted read leaves its exact request pending; changing source scope invalidates the checkpoint.
	linear := integrationSources()["linear"]
	failed := &offlineTransport{responses: integrationResponses("linear"), failKind: "linear-item"}
	partial, err := CaptureSource(context.Background(), linear, "capture", nil, failed)
	if err != nil {
		t.Fatal(err)
	}
	if partial.Snapshot.Complete || partial.Snapshot.Report.Status != "blocked" || len(partial.Snapshot.Checkpoint.Pending) != 1 {
		t.Fatalf("interruption did not preserve pending request: %#v", partial.Snapshot.Checkpoint)
	}
	partialProjection, err := Project(partial, fixtureMapping(linear))
	if err != nil || partialProjection.Report.Status != "blocked" || len(partialProjection.Packets) != 0 || len(partialProjection.Bindings) != 0 {
		t.Fatalf("partial capture produced approved packet/binding: %#v %v", partialProjection, err)
	}
	if err = ValidateCheckpoint(partial.Snapshot.Checkpoint, linear); err != nil {
		t.Fatalf("original checkpoint rejected: %v", err)
	}
	linear.ContainerID = "other-team"
	if err = ValidateCheckpoint(partial.Snapshot.Checkpoint, linear); err == nil {
		t.Fatal("scope drift accepted checkpoint")
	}
}

type jiraPaginationTransport struct{ requests []Request }

func (m *jiraPaginationTransport) Do(_ context.Context, _ Source, r Request) (Response, error) {
	if err := ValidateRequest(integrationSources()["jira"], r); err != nil {
		return Response{}, err
	}
	m.requests = append(m.requests, r)
	switch r.Kind {
	case "jira-search":
		if strings.Contains(r.Path, "nextPageToken=") {
			return Response{Status: 200, Body: []byte(`{"issues":[]}`)}, nil
		}
		return Response{Status: 200, Body: []byte(`{"issues":[{"id":"1","key":"ENG-1","fields":{"project":{"id":"10001","key":"ENG"},"summary":"page one"}}],"nextPageToken":"cursor-two"}`)}, nil
	case "jira-transitions":
		return Response{Status: 200, Body: []byte(`{"transitions":[]}`)}, nil
	default:
		return Response{}, errors.New("unexpected paged Jira request")
	}
}

func TestCaptureSourceFollowsOfflineJiraCursor(t *testing.T) {
	s := integrationSources()["jira"]
	s.Scope.ItemIDs = nil
	m := &jiraPaginationTransport{}
	c, err := CaptureSource(context.Background(), s, "capture", nil, m)
	if err != nil {
		t.Fatal(err)
	}
	searches := 0
	foundCursor := false
	for _, r := range m.requests {
		if r.Kind == "jira-search" {
			searches++
			if strings.Contains(r.Path, "nextPageToken=cursor-two") {
				foundCursor = true
			}
		}
	}
	if searches != 2 || !foundCursor || len(c.Snapshot.Pages) != 3 {
		t.Fatalf("offline pagination plan mismatch: requests=%#v pages=%d", m.requests, len(c.Snapshot.Pages))
	}
}

func fixtureMapping(s Source) Mapping {
	titleField, specField := "title", "description"
	switch s.Provider {
	case "github":
		titleField, specField = "title", "body"
	case "azure-devops":
		titleField, specField = "System.Title", "System.Description"
	case "jira":
		titleField, specField = "summary", "descriptionText"
	}
	lit := func(value string) *json.RawMessage { r := json.RawMessage(value); return &r }
	return Mapping{Version: Version, ID: "fixture", Provider: s.Provider, MappingVersion: s.MappingVersion, Rules: []FieldRule{
		{PacketField: "title", ProviderFieldID: titleField, Owner: "human"},
		{PacketField: "spec", ProviderFieldID: specField, Owner: "human"},
		{PacketField: "status", Literal: lit(`"inbox"`), Owner: "aok"},
		{PacketField: "phase", Literal: lit(`"intake"`), Owner: "aok"},
		{PacketField: "acceptanceCriteria", Literal: lit(`["explicit acceptance"]`), Owner: "aok"},
		{PacketField: "verification.commands", Literal: lit(`["go test ./..."]`), Owner: "aok"},
	}}
}

func captureFixture(t *testing.T, s Source, responses map[string][]byte) *Capture {
	t.Helper()
	c, e := CaptureSource(context.Background(), s, "capture", nil, &offlineTransport{responses: responses})
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestOfflineProjectionAndBindingsForAllProviders(t *testing.T) {
	for provider, s := range integrationSources() {
		t.Run(provider, func(t *testing.T) {
			c := captureFixture(t, s, integrationResponses(provider))
			m := fixtureMapping(s)
			projection, e := Project(c, m)
			if e != nil {
				t.Fatal(e)
			}
			if projection.Report.Status != "valid" || len(projection.Packets) != 1 || len(projection.Bindings) != 1 {
				t.Fatalf("valid projection missing: report=%#v packets=%d bindings=%d", projection.Report, len(projection.Packets), len(projection.Bindings))
			}
			packet := projection.Packets[c.Snapshot.Items[0].Identity.RemoteID]
			if e = validatePacket(packet); e != nil {
				t.Fatalf("projected packet violates embedded schema: %v", e)
			}
			var p map[string]any
			if e = json.Unmarshal(packet, &p); e != nil {
				t.Fatal(e)
			}
			if p["status"] != "inbox" || p["phase"] != "intake" || p["title"] == "" || p["spec"] == "" {
				t.Fatalf("packet field mapping wrong: %#v", p)
			}
			b := projection.Bindings[0]
			if len(b.FieldMappings) != 2 || b.FieldMappings[0].Owner != "tracker" || b.FieldMappings[1].Owner != "tracker" {
				t.Fatalf("human ownership not translated into binding: %#v", b.FieldMappings)
			}
			if b.Packet.SHA256 != Hash(projection.Files[b.Packet.Path]) {
				t.Fatal("binding packet digest mismatch")
			}
			item := c.Snapshot.Items[0]
			for _, field := range []string{fixtureMapping(s).Rules[0].ProviderFieldID, fixtureMapping(s).Rules[1].ProviderFieldID} {
				if len(item.Provenance[field]) == 0 {
					t.Fatalf("field %s lacks source provenance", field)
				}
			}
			if c.Snapshot.Report.AuthorizationGranted || c.Snapshot.Report.ExecutionReady || projection.Report.AuthorizationGranted || projection.Report.ExecutionReady {
				t.Fatal("offline projection granted execution authority")
			}
		})
	}
}

func TestOfflinePlanUpdateRequiresFreshExplicitSemantics(t *testing.T) {
	for provider, s := range integrationSources() {
		t.Run(provider, func(t *testing.T) {
			c := captureFixture(t, s, integrationResponses(provider))
			m := fixtureMapping(s)
			item := c.Snapshot.Items[0]
			titleField := m.Rules[0].ProviderFieldID
			observed, e := time.Parse(time.RFC3339Nano, c.Snapshot.ObservedAt)
			if e != nil {
				t.Fatal(e)
			}
			d := Desired{Version: Version, Identity: item.Identity, ExpectedRevision: item.Revision, Fields: map[string]json.RawMessage{titleField: json.RawMessage(`"changed title"`)}, HumanOwnedFields: []string{titleField}}
			plan, e := PlanUpdate(c, m, d, observed.Add(time.Second))
			if e != nil {
				t.Fatal(e)
			}
			if plan.Report.Status != "valid" {
				t.Fatalf("current explicit update blocked: %#v", plan.Report)
			}
			stale, e := PlanUpdate(c, m, d, observed.Add(6*time.Minute))
			if e != nil || stale.Report.Status != "blocked" || !hasCode(stale.Report.Diagnostics, "refresh-needed") {
				t.Fatalf("stale plan not blocked for refresh: %#v %v", stale, e)
			}
			wrong := d
			wrong.ExpectedRevision = "stale-revision"
			conflict, e := PlanUpdate(c, m, wrong, observed.Add(time.Second))
			if e != nil || !hasCode(conflict.Report.Diagnostics, "conflict") {
				t.Fatalf("revision mismatch accepted: %#v %v", conflict, e)
			}
			unknown := d
			unknown.Fields = map[string]json.RawMessage{"not-mapped": json.RawMessage(`1`)}
			unmapped, e := PlanUpdate(c, m, unknown, observed.Add(time.Second))
			if e != nil || !hasCode(unmapped.Report.Diagnostics, "unmapped-field") {
				t.Fatalf("unknown native field accepted: %#v %v", unmapped, e)
			}
			unowned := d
			unowned.HumanOwnedFields = nil
			blocked, e := PlanUpdate(c, m, unowned, observed.Add(time.Second))
			if e != nil || !hasCode(blocked.Report.Diagnostics, "human-owned") {
				t.Fatalf("human-owned edit without intent accepted: %#v %v", blocked, e)
			}
			if provider == "jira" {
				transition := d
				transition.TransitionID = "t1"
				transitionPlan, e := PlanUpdate(c, m, transition, observed.Add(time.Second))
				if e != nil || !hasCode(transitionPlan.Report.Diagnostics, "transition-required-field") {
					t.Fatalf("Jira transition requirement not enforced: %#v %v", transitionPlan, e)
				}
			}
		})
	}
}

func hasCode(ds []Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestProjectedUnsupportedJiraADFBlocksSpec(t *testing.T) {
	s := integrationSources()["jira"]
	responses := integrationResponses("jira")
	responses["jira-item"] = []byte(`{"id":"1","key":"ENG-1","fields":{"project":{"id":"10001","key":"ENG"},"summary":"Fixture issue","description":{"type":"doc","version":1,"content":[{"type":"widget","attrs":{"id":"raw-widget"}}]},"updated":"2026-10-22T10:00:00.000+0000"}}`)
	c := captureFixture(t, s, responses)
	p, e := Project(c, fixtureMapping(s))
	if e != nil {
		t.Fatal(e)
	}
	if p.Report.Status != "blocked" || len(p.Packets) != 0 || !hasCode(p.Report.Diagnostics, "packet-incomplete") {
		t.Fatalf("lossy ADF was approved as spec: %#v", p)
	}
}

type linearNestedTransport struct{ requests []Request }

func (m *linearNestedTransport) Do(_ context.Context, s Source, r Request) (Response, error) {
	if err := ValidateRequest(s, r); err != nil {
		return Response{}, err
	}
	m.requests = append(m.requests, r)
	var body string
	switch r.Kind {
	case "linear-items":
		body = `{"data":{"organization":{"id":"workspace-guid"},"issues":{"nodes":[{"id":"issue-1","identifier":"ENG-1","team":{"id":"team-guid"},"title":"Nested labels","description":"captured","labels":{"nodes":[{"id":"L1","name":"first"}],"pageInfo":{"hasNextPage":true,"endCursor":"issue-label-cursor-1"}}}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}`
	case "linear-metadata":
		body = `{"data":{"organization":{"id":"workspace-guid"},"team":{"id":"team-guid","states":{"nodes":[{"id":"s1","name":"Todo","type":"unstarted"}],"pageInfo":{"hasNextPage":true,"endCursor":"state-cursor-1"}},"labels":{"nodes":[{"id":"m1","name":"Team label"}],"pageInfo":{"hasNextPage":true,"endCursor":"metadata-label-cursor-1"}}}}}`
	case "linear-issue-labels":
		var req struct {
			Variables struct {
				After string `json:"after"`
			} `json:"variables"`
		}
		_ = json.Unmarshal(r.Body, &req)
		if req.Variables.After == "issue-label-cursor-1" {
			body = `{"data":{"organization":{"id":"workspace-guid"},"issue":{"id":"issue-1","team":{"id":"team-guid"},"labels":{"nodes":[{"id":"L2","name":"second"}],"pageInfo":{"hasNextPage":true,"endCursor":"issue-label-cursor-2"}}}}}`
		} else {
			body = `{"data":{"organization":{"id":"workspace-guid"},"issue":{"id":"issue-1","team":{"id":"team-guid"},"labels":{"nodes":[{"id":"L3","name":"third"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`
		}
	case "linear-states":
		body = `{"data":{"organization":{"id":"workspace-guid"},"team":{"id":"team-guid","states":{"nodes":[{"id":"s2","name":"Done","type":"completed"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`
	case "linear-labels":
		body = `{"data":{"organization":{"id":"workspace-guid"},"team":{"id":"team-guid","labels":{"nodes":[{"id":"m2","name":"Second team label"}],"pageInfo":{"hasNextPage":false,"endCursor":null}}}}}`
	default:
		return Response{}, errors.New("unexpected Linear nested-pagination request: " + r.Kind)
	}
	return Response{Status: 200, Body: []byte(body)}, nil
}

func TestCaptureSourceCompletesNestedLinearMetadataAndIssueLabels(t *testing.T) {
	s := integrationSources()["linear"]
	s.Scope.ItemIDs = nil
	s.Scope.Projects = true
	m := &linearNestedTransport{}
	c, e := CaptureSource(context.Background(), s, "capture", nil, m)
	if e != nil {
		t.Fatal(e)
	}
	if !c.Snapshot.Complete || c.Snapshot.Report.Status != "valid" || len(c.Snapshot.Items) != 1 {
		t.Fatalf("nested Linear capture incomplete: %#v", c.Snapshot.Report)
	}
	item := c.Snapshot.Items[0]
	var labels []map[string]any
	if e = json.Unmarshal(item.Fields["labels"], &labels); e != nil || len(labels) != 3 {
		t.Fatalf("all issue label pages not merged: %s %v", item.Fields["labels"], e)
	}
	if !has(item.LossyFields, "labelsConnection") || len(item.ProjectFields) != 2 {
		t.Fatalf("native and continuation label provenance not retained: %#v %#v", item.LossyFields, item.ProjectFields)
	}
	statePages, labelPages := 0, 0
	for k := range c.Snapshot.Discovery {
		if strings.Contains(k, "linear-states:") {
			statePages++
		}
		if strings.Contains(k, "linear-labels:") {
			labelPages++
		}
	}
	if statePages != 1 || labelPages != 1 {
		t.Fatalf("metadata continuation evidence missing: states=%d labels=%d discovery=%#v", statePages, labelPages, c.Snapshot.Discovery)
	}
}
