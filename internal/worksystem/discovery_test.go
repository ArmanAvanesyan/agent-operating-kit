package worksystem

import (
	"context"
	"testing"
)

type fixtureReader struct {
	report map[string]any
	calls  int
}

func (r *fixtureReader) ReadOnlyDiscovery(context.Context, Request) (Observation, error) {
	r.calls++
	return Observation{Report: r.report}, nil
}
func TestSetupRequiresInjectedReaderAndRejectsAzureSystemProcess(t *testing.T) {
	if _, err := Setup(context.Background(), nil, Request{Provider: "github", Owner: "org", ProjectNumber: 2}); err == nil {
		t.Fatal("setup without reader accepted")
	}
	r := &fixtureReader{report: map[string]any{"conformant": true, "binding": map[string]any{"process": map[string]any{"customizationType": "system"}}, "checks": []any{}}}
	out, err := Setup(context.Background(), r, Request{Provider: "azure-devops", Organization: "https://dev.azure.com/o", Project: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if out["conformant"] != false || out["binding"] != nil || out["remoteMutation"] != false || r.calls != 1 {
		t.Fatalf("unsafe report: %#v", out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Setup(ctx, r, Request{Provider: "github", Owner: "org", ProjectNumber: 2}); err == nil {
		t.Fatal("cancelled discovery accepted")
	}
	if r.calls != 1 {
		t.Fatal("reader called after cancellation")
	}
}

func TestSetupDoesNotTrustConformantBooleanAlone(t *testing.T) {
	r := &fixtureReader{report: map[string]any{"conformant": true, "binding": map[string]any{"provider": "github", "supportLevel": "scaffolded", "owner": "org", "projectNumber": 2, "projectId": "id", "url": "url", "fields": map[string]any{"Status": map[string]any{"id": "f"}}}}}
	out, err := Setup(context.Background(), r, Request{Provider: "github", Owner: "org", ProjectNumber: 2})
	if err != nil {
		t.Fatal(err)
	}
	if out["conformant"] != false || out["binding"] != nil {
		t.Fatalf("incomplete report was trusted: %#v", out)
	}
}
