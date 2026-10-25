package workkit

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
)

func temp(t *testing.T) string {
	t.Helper()
	p, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func copyFixture(t *testing.T, provider string) string {
	t.Helper()
	root := temp(t)
	source := filepath.Join("../../../internal/tracker/testdata/offline", provider)
	e := filepath.WalkDir(source, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(source, p)
		dest := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(dest, 0700)
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(dest, b, 0600)
	})
	if e != nil {
		t.Fatal(e)
	}
	return root
}
func TestTrackerImportsRemainInactiveAndPreserveAllBytes(t *testing.T) {
	for _, provider := range []string{"github", "azure-devops", "linear", "jira"} {
		t.Run(provider, func(t *testing.T) {
			root := copyFixture(t, provider)
			service := sdk.New(&Reader{})
			out := filepath.Join(temp(t), "import")
			request := sdk.SourceRequest{Source: root, Format: "aok-tracker/v1", AsOf: "2026-10-25T12:00:00Z"}
			r, e := service.Import(context.Background(), request, out)
			if e != nil {
				t.Fatal(e)
			}
			if r.Activate || r.AuthorizationGranted || r.ExecutionReady || r.Resources < 2 {
				t.Fatalf("migration %+v", r)
			}
			verified, e := service.VerifyMigration(context.Background(), out)
			if e != nil {
				t.Fatal(e)
			}
			if verified.Resources != r.Resources || verified.OriginalObservedAt == nil {
				t.Fatal("lost observations")
			}
			b, e := os.ReadFile(filepath.Join(root, "manifest.json"))
			if e != nil {
				t.Fatal(e)
			}
			original, _ := os.ReadFile(filepath.Join(out, "assets/original/manifest.json"))
			if string(b) != string(original) {
				t.Fatal("original bytes changed")
			}
			if _, e = service.Import(context.Background(), request, out); e == nil {
				t.Fatal("overwrite accepted")
			}
			original[0] = '!'
			os.WriteFile(filepath.Join(out, "assets/original/manifest.json"), original, 0600)
			if _, e = service.VerifyMigration(context.Background(), out); e == nil {
				t.Fatal("tampered evidence accepted")
			}
		})
	}
}
func workflowFixture(t *testing.T, kind string, version string) string {
	t.Helper()
	names := map[string]string{"WorkItemBinding": "work-item-binding", "HostTaskBinding": "host-task-binding", "SyncOperation": "sync-operation", "SyncReceipt": "sync-receipt"}
	raw, e := os.ReadFile("../../../internal/contracts/testdata/" + names[kind] + ".json")
	if e != nil {
		t.Fatal(e)
	}
	var record map[string]any
	json.Unmarshal(raw, &record)
	record["apiVersion"] = version
	asset := []byte("synthetic evidence\n")
	hash := sdk.SHA256(asset)
	if kind == "WorkItemBinding" {
		record["packet"].(map[string]any)["sha256"] = hash
	}
	if kind == "SyncReceipt" {
		record["dispatchEvidence"].([]any)[0].(map[string]any)["asset"].(map[string]any)["sha256"] = hash
	}
	root := temp(t)
	os.Mkdir(filepath.Join(root, "assets"), 0700)
	os.WriteFile(filepath.Join(root, "assets/item.json"), asset, 0600)
	p := filepath.Join(root, "record.json")
	os.WriteFile(p, encode(record), 0600)
	return p
}
func TestAllWorkflowKindsAndVersionsPreserveUnknownSemantics(t *testing.T) {
	for _, version := range []string{"aok/v1alpha1", "aok/v1alpha2"} {
		for _, kind := range []string{"WorkItemBinding", "HostTaskBinding", "SyncOperation", "SyncReceipt"} {
			t.Run(version+"/"+kind, func(t *testing.T) {
				p := workflowFixture(t, kind, version)
				out := filepath.Join(temp(t), "out")
				r, e := sdk.New(&Reader{}).Import(context.Background(), sdk.SourceRequest{Source: p, Format: version, AsOf: "2026-10-25T12:00:00Z"}, out)
				if e != nil {
					t.Fatal(e)
				}
				if r.Resources != 0 || len(r.Losses) == 0 || r.Activate {
					t.Fatalf("fabricated projection %+v", r)
				}
				if kind == "SyncReceipt" && r.OriginalObservedAt == nil {
					t.Fatal("lost original time")
				}
				if kind != "SyncReceipt" && r.OriginalObservedAt != nil {
					t.Fatal("invented original time")
				}
				if _, e = sdk.New(nil).VerifyMigration(context.Background(), out); e != nil {
					t.Fatal(e)
				}
			})
		}
	}
}
func TestVersionUnsafeHashCancellationAndPrivacy(t *testing.T) {
	root := copyFixture(t, "github")
	reader := &Reader{}
	request := sdk.SourceRequest{Source: root, Format: "aok-tracker/v2", AsOf: "2026-10-25T12:00:00Z"}
	if _, e := reader.Inspect(context.Background(), request); e == nil {
		t.Fatal("version mismatch")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request.Format = "aok-tracker/v1"
	if _, e := reader.Inspect(ctx, request); e == nil {
		t.Fatal("ignored cancellation")
	}
	p := workflowFixture(t, "WorkItemBinding", "aok/v1alpha1")
	b, _ := os.ReadFile(p)
	var record map[string]any
	json.Unmarshal(b, &record)
	record["packet"].(map[string]any)["path"] = "../private"
	os.WriteFile(p, encode(record), 0600)
	if _, e := reader.Inspect(context.Background(), sdk.SourceRequest{Source: p, Format: "aok/v1alpha1"}); e == nil {
		t.Fatal("unsafe asset")
	}
	_, _, e := contracts.Parse([]byte(`{"credential":"P1_SECRET_CANARY"}`))
	if e == nil {
		t.Fatal("invalid source unexpectedly valid")
	}
	_, e = sdk.New(reader).InspectMigration(context.Background(), sdk.SourceRequest{Source: p, Format: "unknown", AsOf: "2026-10-25T12:00:00Z"})
	if e == nil || strings.Contains(e.Error(), "P1_SECRET_CANARY") {
		t.Fatal("unsupported/privacy boundary")
	}
}

func TestDataCenterV2PreservesDeploymentAndDoesNotInventTLSAsset(t *testing.T) {
	root := temp(t)
	source := tracker.Source{Version: tracker.Version2, Provider: "jira-datacenter", Origin: "https://jira.example.test", APIVersion: "2", NativeTenantID: "installation-1", ContainerID: "10", ContainerKey: "ENG", PrincipalPseudonym: "operator", MappingVersion: "1", Auth: tracker.Auth{Mode: "bearer", TokenEnv: "AOK_JIRA_TOKEN"}, Deployment: &tracker.Deployment{BasePath: "/jira", ServerVersion: "10.3", TLSTrustSHA256: strings.Repeat("a", 64)}, Scope: tracker.Scope{ItemIDs: []string{"100"}}}
	adapter := tracker.JiraDataCenter{}
	requests, e := adapter.Initial(source, "capture", nil)
	if e != nil {
		t.Fatal(e)
	}
	item := []byte(`{"id":"100","key":"ENG-1","fields":{"project":{"id":"10","key":"ENG"},"summary":"hello","description":"h1. wiki","updated":"2026-10-24T00:00:00Z"}}`)
	response := tracker.Response{Status: 200, Body: item}
	decoded, e := adapter.Decode(source, requests[0], response)
	if e != nil {
		t.Fatal(e)
	}
	files := map[string][]byte{"assets/item.json": item}
	pages := []tracker.Page{{Request: requests[0], Status: 200, Asset: contracts.AssetRef{Path: "assets/item.json", SHA256: sdk.SHA256(item)}, ObservedAt: "2026-10-24T00:00:00Z"}}
	for i, request := range decoded.Next {
		body := []byte(`{"transitions":[]}`)
		name := fmt.Sprintf("assets/next-%d.json", i)
		files[name] = body
		pages = append(pages, tracker.Page{Request: request, Status: 200, Asset: contracts.AssetRef{Path: name, SHA256: sdk.SHA256(body)}, ObservedAt: "2026-10-24T00:00:00Z"})
	}
	manifest := tracker.Manifest{Version: tracker.Version2, Source: source, Mode: "capture", Pages: pages}
	files["manifest.json"] = encode(manifest)
	for name, b := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0700)
		os.WriteFile(p, b, 0600)
	}
	out := filepath.Join(temp(t), "out")
	service := sdk.New(&Reader{})
	r, e := service.Import(context.Background(), sdk.SourceRequest{Source: root, Format: tracker.Version2, AsOf: "2026-10-25T12:00:00Z"}, out)
	if e != nil {
		t.Fatal(e)
	}
	if len(r.Losses) == 0 || r.Activate {
		t.Fatal("invented conformance")
	}
	if _, e = service.VerifyMigration(context.Background(), out); e != nil {
		t.Fatal(e)
	}
	matches, _ := filepath.Glob(filepath.Join(out, "resources/ConnectionProfile*"))
	if len(matches) != 0 {
		t.Fatal("invented DC trust asset")
	}
	matches, _ = filepath.Glob(filepath.Join(out, "resources/WorkItem*"))
	if len(matches) != 1 {
		t.Fatalf("missing DC item %+v", r)
	}
	b, _ := os.ReadFile(matches[0])
	if !strings.Contains(string(b), `"provider":"jira-datacenter"`) {
		t.Fatal("Cloud/DC identity confusion")
	}
}

func TestInterruptedEmptyCaptureKeepsObservationUnknown(t *testing.T) {
	root := copyFixture(t, "github")
	p := filepath.Join(root, "manifest.json")
	raw, _ := os.ReadFile(p)
	var manifest tracker.Manifest
	if e := json.Unmarshal(raw, &manifest); e != nil {
		t.Fatal(e)
	}
	manifest.Pages = []tracker.Page{}
	os.WriteFile(p, encode(manifest), 0600)
	service := sdk.New(&Reader{})
	out := filepath.Join(temp(t), "out")
	report, e := service.Import(context.Background(), sdk.SourceRequest{Source: root, Format: tracker.Version, AsOf: "2026-10-25T12:00:00Z"}, out)
	if e != nil {
		t.Fatal(e)
	}
	if report.OriginalObservedAt != nil || len(report.Losses) == 0 {
		t.Fatal("unknown capture timestamp renewed")
	}
	if _, e = service.VerifyMigration(context.Background(), out); e != nil {
		t.Fatal(e)
	}
}
