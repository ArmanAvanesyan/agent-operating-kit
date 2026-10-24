package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
)

// cliTrackerFixtureTransport is deliberately offline and returns only the two
// planned GitHub fixture responses. It has no fallback to credentials or live IO.
type cliTrackerFixtureTransport struct{ title string }

func (f cliTrackerFixtureTransport) Do(_ context.Context, _ tracker.Source, r tracker.Request) (tracker.Response, error) {
	var body string
	switch r.Kind {
	case "github-repository":
		body = `{"id":1,"node_id":"repo-node","name":"widgets","owner":{"login":"acme","node_id":"owner-node"}}`
	case "github-item":
		b, _ := json.Marshal(map[string]any{"id": 7, "node_id": "issue-node-7", "number": 1, "title": f.title, "body": "captured specification", "state": "open", "html_url": "https://github.com/acme/widgets/issues/1", "repository_url": "https://api.github.com/repos/acme/widgets"})
		return tracker.Response{Status: 200, Headers: map[string]string{"etag": "offline-fixture"}, Body: b}, nil
	default:
		return tracker.Response{}, errors.New("unplanned offline fixture request")
	}
	return tracker.Response{Status: 200, Headers: map[string]string{"etag": "offline-fixture"}, Body: []byte(body)}, nil
}

func cliTrackerSource() tracker.Source {
	return tracker.Source{Version: tracker.Version, Provider: "github", Origin: "https://api.github.com", APIVersion: "2022-11-28", NativeTenantID: "owner-node", ContainerID: "repo-node", ContainerKey: "acme/widgets", PrincipalPseudonym: "offline-test", Auth: tracker.Auth{Mode: "gh-session"}, Scope: tracker.Scope{ItemIDs: []string{"1"}}, MappingVersion: "fixture-map-v1"}
}

func writeTrackerBundle(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func runTrackerJSON(t *testing.T, args ...string) (int, []byte, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(append([]string{"tracker"}, args...), &stdout, &stderr)
	return code, stdout.Bytes(), stderr.String()
}
func rawJSONPointer(value string) *json.RawMessage { raw := json.RawMessage(value); return &raw }

func TestTrackerOfflineCLIImportValidateInspectPacketDiffAndPlanUpdate(t *testing.T) {
	// This whole command chain runs with no executable PATH. The only source
	// reads were generated from the in-memory fixture transport above.
	t.Setenv("PATH", "")
	t.Setenv("AOK_TRACKER_READ_POLICY", "")
	s := cliTrackerSource()
	captureA, err := tracker.CaptureSource(context.Background(), s, "capture", nil, cliTrackerFixtureTransport{title: "Fixture issue"})
	if err != nil {
		t.Fatal(err)
	}
	captureB, err := tracker.CaptureSource(context.Background(), s, "capture", nil, cliTrackerFixtureTransport{title: "Updated fixture issue"})
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	inputA := filepath.Join(base, "input-a")
	inputB := filepath.Join(base, "input-b")
	writeTrackerBundle(t, inputA, captureA.Files)
	writeTrackerBundle(t, inputB, captureB.Files)
	outA := filepath.Join(base, "imported-a")
	code, out, stderr := runTrackerJSON(t, "import", "--manifest", filepath.Join(inputA, "manifest.json"), "--out", outA, "--json")
	if code != 0 {
		t.Fatalf("tracker import failed (%d): %s %s", code, out, stderr)
	}
	var snapshot tracker.Snapshot
	if err = json.Unmarshal(out, &snapshot); err != nil {
		t.Fatal(err)
	}
	if !snapshot.Complete || len(snapshot.Items) != 1 {
		t.Fatalf("import did not return complete fixture snapshot: %#v", snapshot.Report)
	}
	code, out, stderr = runTrackerJSON(t, "validate", outA, "--json")
	if code != 0 {
		t.Fatalf("tracker validate failed (%d): %s %s", code, out, stderr)
	}
	var report tracker.Report
	if err = json.Unmarshal(out, &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "valid" || report.AuthorizationGranted || report.ExecutionReady {
		t.Fatalf("unexpected validation report: %#v", report)
	}
	asOf := time.Now().UTC().Format(time.RFC3339Nano)
	code, out, stderr = runTrackerJSON(t, "inspect", outA, "--as-of", asOf, "--json")
	if code != 0 {
		t.Fatalf("tracker inspect failed (%d): %s %s", code, out, stderr)
	}
	var inspection map[string]any
	if err = json.Unmarshal(out, &inspection); err != nil {
		t.Fatal(err)
	}
	if inspection["authorizationGranted"] != false || inspection["executionReady"] != false {
		t.Fatalf("inspect granted authority: %#v", inspection)
	}

	mapping := tracker.Mapping{Version: tracker.Version, ID: "offline-map", Provider: "github", MappingVersion: s.MappingVersion, Rules: []tracker.FieldRule{
		{PacketField: "title", ProviderFieldID: "title", Owner: "human"}, {PacketField: "spec", ProviderFieldID: "body", Owner: "human"},
		{PacketField: "status", Literal: rawJSONPointer(`"inbox"`), Owner: "aok"},
		{PacketField: "phase", Literal: rawJSONPointer(`"intake"`), Owner: "aok"},
		{PacketField: "acceptanceCriteria", Literal: rawJSONPointer(`["explicit acceptance"]`), Owner: "aok"},
		{PacketField: "verification.commands", Literal: rawJSONPointer(`["go test ./..."]`), Owner: "aok"},
	}}
	mapPath := filepath.Join(base, "mapping.json")
	mapBytes, _ := json.Marshal(mapping)
	if err = os.WriteFile(mapPath, mapBytes, 0600); err != nil {
		t.Fatal(err)
	}
	packetOut := filepath.Join(base, "packets")
	code, out, stderr = runTrackerJSON(t, "packet", "--snapshot", outA, "--mapping", mapPath, "--out", packetOut, "--json")
	if code != 0 {
		t.Fatalf("tracker packet failed (%d): %s %s", code, out, stderr)
	}
	var projection tracker.Projection
	if err = json.Unmarshal(out, &projection); err != nil {
		t.Fatal(err)
	}
	if projection.Report.Status != "valid" || len(projection.Bindings) != 1 || len(projection.Packets) != 1 {
		t.Fatalf("packet projection incomplete: %#v", projection.Report)
	}
	if _, err = os.Stat(filepath.Join(packetOut, "bindings")); err != nil {
		t.Fatalf("binding output missing: %v", err)
	}

	outB := filepath.Join(base, "imported-b")
	code, out, stderr = runTrackerJSON(t, "import", "--manifest", filepath.Join(inputB, "manifest.json"), "--out", outB, "--json")
	if code != 0 {
		t.Fatalf("second tracker import failed (%d): %s %s", code, out, stderr)
	}
	code, out, stderr = runTrackerJSON(t, "diff", outA, outB, "--json")
	if code != 0 {
		t.Fatalf("tracker diff failed (%d): %s %s", code, out, stderr)
	}
	var diff tracker.DiffReport
	if err = json.Unmarshal(out, &diff); err != nil {
		t.Fatal(err)
	}
	if diff.Report.Status != "valid" || len(diff.Changes) != 1 || !strings.Contains(strings.Join(diff.Changes[0].Fields, ","), "title") {
		t.Fatalf("fixture diff did not report title change: %#v", diff)
	}

	item := snapshot.Items[0]
	desired := tracker.Desired{Version: tracker.Version, Identity: item.Identity, ExpectedRevision: item.Revision, Fields: map[string]json.RawMessage{"title": json.RawMessage(`"Proposed title"`)}, HumanOwnedFields: []string{"title"}}
	desiredPath := filepath.Join(base, "desired.json")
	desiredBytes, _ := json.Marshal(desired)
	if err = os.WriteFile(desiredPath, desiredBytes, 0600); err != nil {
		t.Fatal(err)
	}
	planOut := filepath.Join(base, "plan")
	code, out, stderr = runTrackerJSON(t, "plan-update", "--snapshot", outA, "--mapping", mapPath, "--desired", desiredPath, "--as-of", time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano), "--out", planOut, "--json")
	if code != 0 {
		t.Fatalf("tracker plan-update failed (%d): %s %s", code, out, stderr)
	}
	var plan tracker.TrackerUpdatePlan
	if err = json.Unmarshal(out, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Report.Status != "valid" || plan.ConditionalWrite != "unverified-read-only" || plan.Report.AuthorizationGranted || plan.Report.ExecutionReady {
		t.Fatalf("unexpected update plan: %#v", plan)
	}
	if _, err = os.Stat(filepath.Join(planOut, "update-plan.json")); err != nil {
		t.Fatalf("update plan evidence missing: %v", err)
	}

	// Reusing an output directory must fail without replacing its existing data.
	before, err := os.ReadFile(filepath.Join(outA, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ = runTrackerJSON(t, "import", "--manifest", filepath.Join(inputB, "manifest.json"), "--out", outA, "--json")
	if code != 1 {
		t.Fatalf("existing output directory was accepted: code=%d", code)
	}
	after, err := os.ReadFile(filepath.Join(outA, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed import modified existing immutable evidence")
	}
}

func TestTrackerLiveCLIRequiresOperatorReadPolicyBeforeUsingCredentials(t *testing.T) {
	t.Setenv("PATH", "")
	t.Setenv("AOK_TRACKER_READ_POLICY", "")
	s := cliTrackerSource()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	sourcePath := filepath.Join(t.TempDir(), "source.json")
	if err = os.WriteFile(sourcePath, b, 0600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "live-output")
	code, _, stderr := runTrackerJSON(t, "capture", "--source", sourcePath, "--out", out, "--json")
	if code != 1 || !strings.Contains(stderr, "operator-supplied --read-policy") {
		t.Fatalf("live capture did not fail closed without policy: code=%d stderr=%s", code, stderr)
	}
	if _, err = os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("live output created without read approval: %v", err)
	}
}
