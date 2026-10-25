package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestPublicSnapshotsAreDefensiveAndNotAuthorization(t *testing.T) {
	raw, e := os.ReadFile("../testdata/corpora/offline/workspace.json")
	if e != nil {
		t.Fatal(e)
	}
	d, e := Parse(raw, "json")
	if e != nil {
		t.Fatal(e)
	}
	original := d.SHA256()
	raw[0] = '!'
	b := d.Bytes()
	b[0] = '!'
	c := d.CanonicalBytes()
	c[0] = '!'
	if original != d.SHA256() {
		t.Fatal("mutable source identity")
	}
	var resource api.Workspace
	if e = d.Decode(&resource); e != nil {
		t.Fatal(e)
	}
	resource.Metadata.Id = "changed"
	var again api.Workspace
	d.Decode(&again)
	if again.Metadata.Id == "changed" {
		t.Fatal("mutable typed result")
	}
	service := New(nil)
	report, e := service.Resolve(context.Background(), WorkspaceRequest{Workspace: "../testdata/corpora/offline/workspace.json"})
	if e != nil {
		t.Fatal(e)
	}
	if report.AuthorizationGranted || report.ExecutionReady || report.LiveQualification != "not-established" {
		t.Fatal("static result grants authority")
	}
}
func TestSuiteRenderingPreservesCompleteDomainResult(t *testing.T) {
	result := Result("migrate import-aok", "partial", map[string]any{"activation": false}, nil, "possible")
	wrapped := SuiteResult(result, 6)
	b, _ := json.Marshal(wrapped)
	var v struct {
		Spec struct {
			Data    json.RawMessage `json:"data"`
			Effects string          `json:"effects"`
			Exit    int             `json:"domainExitCode"`
		} `json:"spec"`
	}
	json.Unmarshal(b, &v)
	domain, _ := json.Marshal(result)
	if string(v.Spec.Data) != string(domain) || v.Spec.Exit != 6 || v.Spec.Effects != "possible" {
		t.Fatal("lossy suite wrapper")
	}
}
func TestSDKHasNoLegacyImports(t *testing.T) {
	for _, prefix := range []string{".", "../api", "../internal", "../readsource"} {
		filepath.WalkDir(prefix, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() && filepath.Ext(path) == ".go" && !strings.HasSuffix(path, "_test.go") {
				b, _ := os.ReadFile(path)
				if bytes.Contains(b, []byte(`agent-operating-kit/internal/`)) {
					t.Errorf("foreign internal import in %s", path)
				}
			}
			return nil
		})
	}
}

func TestConcurrentReadonlyResolutionAndParsing(t *testing.T) {
	raw, e := os.ReadFile("../testdata/corpora/native-only/workspace.json")
	if e != nil {
		t.Fatal(e)
	}
	errors := make(chan error, 32)
	var group sync.WaitGroup
	for i := 0; i < 32; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			d, e := Parse(raw, "json")
			if e != nil {
				errors <- e
				return
			}
			copy := d.Bytes()
			copy[0] = '!'
			report, e := New(nil).Resolve(context.Background(), WorkspaceRequest{Workspace: "../testdata/corpora/native-only/workspace.json"})
			if e != nil {
				errors <- e
				return
			}
			if report.AuthorizationGranted || report.ExecutionReady {
				errors <- fmt.Errorf("readonly resolution promoted authority")
			}
		}()
	}
	group.Wait()
	close(errors)
	for e := range errors {
		t.Fatal(e)
	}
}
