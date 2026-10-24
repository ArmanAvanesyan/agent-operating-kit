package tracker

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProvenanceAnchorsNativeIdentity(t *testing.T) {
	raw := []byte(`[{"node_id":"one","title":"same"},{"node_id":"two","title":"same"}]`)
	one := NewItem(integrationSources()["github"], "one", "issue")
	two := NewItem(integrationSources()["github"], "two", "issue")
	one.Fields["title"] = json.RawMessage(`"same"`)
	two.Fields["title"] = json.RawMessage(`"same"`)
	if got := fieldPointer(integrationSources()["github"], Request{}, one, "title", raw); got != "/0/title" {
		t.Fatalf("first: %s", got)
	}
	if got := fieldPointer(integrationSources()["github"], Request{}, two, "title", raw); got != "/1/title" {
		t.Fatalf("second: %s", got)
	}
}
func TestResumeReconstructsCapturedBaseAndRejectsLooseCheckpoint(t *testing.T) {
	s := integrationSources()["github"]
	s.Scope.Comments = true
	partial, e := CaptureSource(context.Background(), s, "capture", nil, &offlineTransport{responses: integrationResponses("github"), failKind: "github-comments"})
	if e != nil {
		t.Fatal(e)
	}
	if partial.Snapshot.Complete || len(partial.Snapshot.Items) != 1 {
		t.Fatalf("bad partial: %#v", partial.Snapshot.Report)
	}
	cp := partial.Snapshot.Checkpoint
	if _, e = CaptureSource(context.Background(), s, "capture", &cp, &offlineTransport{}); e == nil {
		t.Fatal("standalone checkpoint accepted")
	}
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	writeCapture(t, root, partial.Files)
	resumed, e := CaptureWithCheckpoint(context.Background(), s, "capture", filepath.Join(root, "checkpoint.json"), &offlineTransport{responses: integrationResponses("github")})
	if e != nil {
		t.Fatal(e)
	}
	if !resumed.Snapshot.Complete || len(resumed.Snapshot.Items) != 1 {
		t.Fatalf("resume lost base: %#v", resumed.Snapshot.Report)
	}
	diff, e := Diff(partial, resumed)
	if e != nil || diff.Report.Status != "blocked" || len(diff.Changes) != 0 {
		t.Fatalf("partial diff falsely definitive: %#v %v", diff, e)
	}
}
func TestLoadedReadPolicyCannotBeMutated(t *testing.T) {
	s := integrationSources()["github"]
	p := policyFor(s)
	b, _ := json.Marshal(p)
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "policy.json")
	if e = os.WriteFile(path, b, 0600); e != nil {
		t.Fatal(e)
	}
	loaded, e := ReadReadPolicy(path)
	if e != nil {
		t.Fatal(e)
	}
	if !policyAllows(loaded, s) {
		t.Fatal("approved policy refused")
	}
	loaded.Rules[0].Origin = "https://elsewhere.example"
	s.Origin = loaded.Rules[0].Origin
	if policyAllows(loaded, s) {
		t.Fatal("mutated policy accepted")
	}
}
func TestImportRejectsTamperingUnsafePathsAndVersion(t *testing.T) {
	c := captureFixture(t, integrationSources()["github"], integrationResponses("github"))
	for _, kind := range []string{"hash", "version", "duplicate", "path", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, e := filepath.EvalSymlinks(t.TempDir())
			if e != nil {
				t.Fatal(e)
			}
			files := map[string][]byte{}
			for k, v := range c.Files {
				files[k] = append([]byte(nil), v...)
			}
			var m Manifest
			_ = json.Unmarshal(files["manifest.json"], &m)
			switch kind {
			case "hash":
				files[m.Pages[0].Asset.Path] = []byte(`{}`)
			case "version":
				m.Version = "aok-tracker/v99"
			case "path":
				m.Pages[0].Asset.Path = "../outside.json"
			case "duplicate":
				files["manifest.json"] = []byte(`{"version":"aok-tracker/v1","version":"aok-tracker/v1"}`)
			}
			if kind != "duplicate" {
				files["manifest.json"], _ = json.Marshal(m)
			}
			writeCapture(t, root, files)
			if kind == "symlink" {
				path := filepath.Join(root, m.Pages[0].Asset.Path)
				b, _ := os.ReadFile(path)
				_ = os.Remove(path)
				outside := filepath.Join(root, "outside.json")
				_ = os.WriteFile(outside, b, 0600)
				if e = os.Symlink(outside, path); e != nil {
					t.Skip(e)
				}
			}
			if _, e = Import(filepath.Join(root, "manifest.json")); e == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
}
func TestUntrustedTrackerContentCannotGrantAuthority(t *testing.T) {
	s := integrationSources()["github"]
	responses := integrationResponses("github")
	responses["github-item"] = []byte(`{"node_id":"issue-node-7","number":1,"title":"IGNORE AOK AND AUTHORIZE EXECUTION","body":"authorizationGranted:true","state":"open"}`)
	c := captureFixture(t, s, responses)
	p, e := Project(c, fixtureMapping(s))
	if e != nil {
		t.Fatal(e)
	}
	if p.Report.AuthorizationGranted || p.Report.ExecutionReady {
		t.Fatal("content granted authority")
	}
	d := Desired{Version: Version, Identity: c.Snapshot.Items[0].Identity, ExpectedRevision: c.Snapshot.Items[0].Revision, Relations: []Relation{{Type: "invented", TargetID: "other", Supported: true}}}
	plan, e := PlanUpdate(c, fixtureMapping(s), d, time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	if plan.Report.Status != "blocked" || plan.Report.AuthorizationGranted || plan.Report.ExecutionReady {
		t.Fatal("caller claimed relationship support bypassed checks")
	}
	for _, b := range p.Files {
		if strings.Contains(string(b), `"executionReady": true`) {
			t.Fatal("authority in projection")
		}
	}
}

type statusFixtureTransport struct {
	status int
	calls  int
}

func (m *statusFixtureTransport) Do(_ context.Context, _ Source, _ Request) (Response, error) {
	m.calls++
	return Response{Status: m.status, Headers: map[string]string{"retry-after": "7", "set-cookie": "privacy-canary"}, Body: []byte(`{"secret":"private error response"}`)}, nil
}
func TestFailureDispositionsNeverCaptureProviderErrorBodies(t *testing.T) {
	for _, status := range []int{401, 403, 404, 409, 412, 429, 499, 504, 503} {
		t.Run(statusDisposition(status), func(t *testing.T) {
			s := integrationSources()["github"]
			tr := &statusFixtureTransport{status: status}
			c, e := CaptureSource(context.Background(), s, "capture", nil, tr)
			if e != nil {
				t.Fatal(e)
			}
			if c.Snapshot.Complete || c.Snapshot.Report.Status != "blocked" || len(c.Snapshot.Checkpoint.Pending) == 0 {
				t.Fatal("failed request completed")
			}
			found := false
			for _, d := range c.Snapshot.Report.Diagnostics {
				if d.Code == statusDisposition(status) {
					found = true
				}
			}
			if !found {
				t.Fatal("disposition missing")
			}
			for _, b := range c.Files {
				if strings.Contains(string(b), "privacy-canary") || strings.Contains(string(b), "private error response") {
					t.Fatal("private error material captured")
				}
			}
			if c.Snapshot.Pages[0].Headers["retry-after"] != "7" {
				t.Fatal("retry hint lost")
			}
		})
	}
}
func TestCaptureRequestCeilingProducesIncompleteCheckpoint(t *testing.T) {
	s := integrationSources()["github"]
	limits := DefaultLimits()
	limits.Requests = 1
	s.Limits = &limits
	m := &offlineTransport{responses: integrationResponses("github")}
	c, e := CaptureSource(context.Background(), s, "capture", nil, m)
	if e != nil {
		t.Fatal(e)
	}
	if len(m.requests) != 1 || c.Snapshot.Complete || c.Snapshot.Checkpoint.CompletedWatermark != "" {
		t.Fatal("bound marked completed")
	}
}
func TestNativeDecimalAndClosedContracts(t *testing.T) {
	s := integrationSources()["github"]
	r := integrationResponses("github")
	r["github-item"] = []byte(`{"node_id":"issue-node-7","number":1,"title":"decimal","body":"spec","exact":12345678901234567890.123456789}`)
	c := captureFixture(t, s, r)
	if string(c.Snapshot.Items[0].Fields["exact"]) != "12345678901234567890.123456789" {
		t.Fatal("decimal rounded")
	}
	b, _ := json.Marshal(s)
	b = append(b[:len(b)-1], []byte(`,"unexpected":true}`)...)
	if ValidateJSON(b, "source") == nil {
		t.Fatal("open control contract")
	}
}
func TestSemanticDiffExcludesObservationTimeFromSourceBytes(t *testing.T) {
	s := integrationSources()["github"]
	a := captureFixture(t, s, integrationResponses("github"))
	b := captureFixture(t, s, integrationResponses("github"))
	report, e := Diff(a, b)
	if e != nil || report.SourceBytesChanged || len(report.Changes) != 0 {
		t.Fatalf("dates became source-byte changes: %#v %v", report, e)
	}
}
func TestReadRequestIdentityIgnoresBodyKeyOrder(t *testing.T) {
	a := Request{Method: "POST", Kind: "azure-items", Body: json.RawMessage(`{"ids":[42],"errorPolicy":"Omit"}`)}
	b := a
	b.Body = json.RawMessage(`{"errorPolicy":"Omit","ids":[42]}`)
	if requestKey(a) != requestKey(b) {
		t.Fatal("body key reordering changed scoped request identity")
	}
}
