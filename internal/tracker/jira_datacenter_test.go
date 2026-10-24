package tracker

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"net/url"
	"strings"
	"testing"
	"time"
)

func dcFixtureSource() Source {
	return Source{Version: Version2, Provider: "jira-datacenter", Origin: "https://jira.example.test", APIVersion: "2", NativeTenantID: "installation-1", ContainerID: "10", ContainerKey: "ENG", PrincipalPseudonym: "operator", MappingVersion: "1", Auth: Auth{Mode: "bearer", TokenEnv: "AOK_JIRA_TOKEN"}, Deployment: &Deployment{BasePath: "/jira", ServerVersion: "10.3", TLSTrustSHA256: strings.Repeat("a", 64)}, Scope: Scope{ItemIDs: []string{"100"}}}
}
func TestDataCenterIsolationAndClosedSchema(t *testing.T) {
	s := dcFixtureSource()
	if e := ValidateSource(s); e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(s)
	if e := ValidateJSON(b, "source"); e != nil {
		t.Fatal(e)
	}
	s.Version = Version
	b, _ = json.Marshal(s)
	if ValidateJSON(b, "source") == nil || ValidateSource(s) == nil {
		t.Fatal("v1 accepted DC deployment")
	}
	s = dcFixtureSource()
	s.Provider = "jira"
	if ValidateSource(s) == nil {
		t.Fatal("Cloud accepted DC")
	}
	s = dcFixtureSource()
	s.Deployment.BasePath = "/../other"
	if ValidateSource(s) == nil {
		t.Fatal("unsafe base")
	}
}
func TestDataCenterAPI2ScopedPaginationAndWiki(t *testing.T) {
	s := dcFixtureSource()
	a := JiraDataCenter{}
	reqs, e := a.Initial(s, "capture", nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidateRequest(s, reqs[0]); e != nil {
		t.Fatal(e)
	}
	res := Response{Status: 200, Body: []byte(`{"id":"100","key":"ENG-1","fields":{"project":{"id":"10","key":"ENG"},"summary":"hello","description":"h1. wiki *bold*","updated":"2026-10-24T00:00:00Z"}}`)}
	p, e := a.Decode(s, reqs[0], res)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Items) != 1 || string(p.Items[0].Fields["description"]) != `"h1. wiki *bold*"` || len(p.Items[0].LossyFields) == 0 {
		t.Fatal("wiki not preserved with loss warning")
	}
	if p.Items[0].RevisionKind != "observation-fingerprint" {
		t.Fatal("invented atomic revision")
	}
	if e = ValidateRequest(s, p.Next[0]); e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(p.Next[0].Path)
	u.Path = strings.Replace(u.Path, "/api/2/", "/api/3/", 1)
	r := p.Next[0]
	r.Path = u.String()
	if ValidateRequest(s, r) == nil {
		t.Fatal("Cloud endpoint accepted")
	}
	s.Scope.ItemIDs = nil
	r = dcSearch(s, 0)
	r.Path = strings.Replace(r.Path, "startAt=0", "startAt=-1", 1)
	if ValidateRequest(s, r) == nil {
		t.Fatal("bad offset")
	}
}

func TestDataCenterBundleProvenanceProjectionAndDraft(t *testing.T) {
	s := dcFixtureSource()
	a := JiraDataCenter{}
	initial, _ := a.Initial(s, "capture", nil)
	raw := []byte(`{"id":"100","key":"ENG-1","fields":{"project":{"id":"10","key":"ENG"},"summary":"hello","updated":"2026-10-24T00:00:00Z"}}`)
	decoded, e := a.Decode(s, initial[0], Response{Status: 200, Body: raw})
	if e != nil {
		t.Fatal(e)
	}
	tr := []byte(`{"transitions":[{"id":"31","name":"Done","fields":{"resolution":{"required":true}}}]}`)
	assets := map[string][]byte{"assets/item.json": raw, "assets/transitions.json": tr}
	m := Manifest{Version: Version2, Source: s, Mode: "capture", Pages: []Page{
		{Request: initial[0], Status: 200, Asset: contracts.AssetRef{Path: "assets/item.json", SHA256: Hash(raw)}, ObservedAt: "2026-10-24T00:00:00Z"},
		{Request: decoded.Next[0], Status: 200, Asset: contracts.AssetRef{Path: "assets/transitions.json", SHA256: Hash(tr)}, ObservedAt: "2026-10-24T00:00:00Z"},
	}}
	c, e := build(m, assets)
	if e != nil {
		t.Fatal(e)
	}
	if !c.Snapshot.Complete || len(c.Snapshot.Items) != 1 || len(c.Snapshot.Items[0].Transitions) != 1 {
		t.Fatal(c.Snapshot)
	}
	if len(c.Snapshot.Items[0].Provenance["summary"]) != 1 {
		t.Fatal("missing field provenance")
	}
	for name, body := range c.Files {
		if name == "manifest.json" {
			if e := ValidateJSON(body, "manifest"); e != nil {
				t.Fatal(e)
			}
		}
	}

	mapping := fixtureMapping(s)
	mapping.Version = Version2
	mapping.Rules[0].ProviderFieldID = "summary"
	explicit := json.RawMessage(`"operator-supplied plain task specification"`)
	mapping.Rules[1].ProviderFieldID = ""
	mapping.Rules[1].Literal = &explicit
	projection, e := Project(c, mapping)
	if e != nil || projection.Report.Status != "valid" || len(projection.Bindings) != 1 || projection.Bindings[0].APIVersion != "aok/v1alpha2" {
		t.Fatal(projection.Report, e)
	}
	at, _ := time.Parse(time.RFC3339, "2026-10-24T00:01:00Z")
	draft, e := PlanUpdate(c, mapping, Desired{Version: Version2, Identity: c.Snapshot.Items[0].Identity, ExpectedRevision: c.Snapshot.Items[0].Revision, TransitionID: "31"}, at)
	if e != nil || draft.Report.Status != "blocked" || draft.Report.AuthorizationGranted || draft.Report.ExecutionReady {
		t.Fatal(draft, e)
	}
	b, _ := json.Marshal(Capability(s))
	if _, _, e := contracts.Parse(b); e != nil {
		t.Fatal(e)
	}
	s.Version = Version
	s.Provider = "jira"
	b, _ = json.Marshal(Capability(s))
	if strings.Contains(string(b), "jira-datacenter") {
		t.Fatal("Cloud identity polluted")
	}
}
func TestDataCenterSearchPreservesExactCustomNumbers(t *testing.T) {
	s := dcFixtureSource()
	s.Scope.ItemIDs = nil
	r := dcSearch(s, 0)
	p, e := (JiraDataCenter{}).Decode(s, r, Response{Status: 200, Body: []byte(`{"startAt":0,"maxResults":50,"total":1,"issues":[{"id":"100","key":"ENG-1","fields":{"project":{"id":"10","key":"ENG"},"customfield_amount":0.12345678901234567890123456789}}]}`)})
	if e != nil {
		t.Fatal(e)
	}
	if string(p.Items[0].Fields["customfield_amount"]) != "0.12345678901234567890123456789" {
		t.Fatal("rounded custom number")
	}
}
