package agentresolve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
)

func catalogFixture(t *testing.T) (string, string) {
	t.Helper()
	manifest := "../../docs/research/model-catalog-manifest.json"
	policy := "../../docs/research/model-catalog-policy.json"
	c, r := modelcatalog.Import(manifest, policy)
	if c == nil || r.Status != "valid" {
		t.Fatalf("catalog import: %+v", r)
	}
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "catalog")
	if err := evidence.WriteFiles(root, c.Files); err != nil {
		t.Fatal(err)
	}
	return root, policy
}

func TestCatalogResolutionBindsExactInputsAndPreservesLegacy(t *testing.T) {
	bundle, policy := catalogFixture(t)
	assignment := filepath.Join(fixtureRoot(), "assignment.json")
	legacy := Resolve(assignment, fixtureRoot())
	if legacy.Snapshot == nil || legacy.Snapshot.CatalogEvidence != nil {
		t.Fatal("legacy resolution changed catalog behavior")
	}
	at := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	first := ResolveWithCatalog(assignment, fixtureRoot(), bundle, policy, at)
	second := ResolveWithCatalog(assignment, fixtureRoot(), bundle, policy, at)
	if first.Status != "blocked" || first.Snapshot == nil || first.Snapshot.CatalogEvidence == nil {
		t.Fatalf("missing blocked evidence: %+v", first)
	}
	if first.Snapshot.Digest == legacy.Snapshot.Digest || first.Snapshot.Digest != second.Snapshot.Digest {
		t.Fatal("catalog inputs not deterministically digest bound")
	}
	e := first.Snapshot.CatalogEvidence
	if e.CatalogSHA256 == "" || e.PolicySHA256 == "" || e.ProfileSHA256 == "" || e.Check.AuthorizationGranted || e.Check.ExecutionReady {
		t.Fatalf("incomplete or unsafe evidence: %+v", e)
	}
	if Resolve(assignment, fixtureRoot()).Snapshot.Digest != legacy.Snapshot.Digest {
		t.Fatal("optional catalog use mutated legacy behavior")
	}
	// A policy byte change remains visible even when semantics are identical.
	pb, err := os.ReadFile(policy)
	if err != nil {
		t.Fatal(err)
	}
	modified := filepath.Join(t.TempDir(), "policy.json")
	if err = os.WriteFile(modified, append(pb, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	changed := ResolveWithCatalog(assignment, fixtureRoot(), bundle, modified, at)
	if changed.Snapshot == nil || changed.Snapshot.Digest == first.Snapshot.Digest {
		t.Fatal("effective policy bytes were not bound into resolution")
	}
	if changed.Snapshot.CatalogEvidence.PolicySHA256 == e.PolicySHA256 {
		t.Fatal("effective policy digest unchanged")
	}
	shifted := ResolveWithCatalog(assignment, fixtureRoot(), bundle, policy, at.Add(time.Hour))
	if shifted.Snapshot == nil || shifted.Snapshot.Digest == first.Snapshot.Digest {
		t.Fatal("as-of time not bound into snapshot")
	}
}

func TestCatalogResolutionRejectsInvalidBundleWithoutWeakeningReadiness(t *testing.T) {
	bundle, policy := catalogFixture(t)
	p := filepath.Join(bundle, "catalog.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err = json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	c["version"] = "aok-model-catalog/v999"
	b, err = json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(p, b, 0600); err != nil {
		t.Fatal(err)
	}
	r := ResolveWithCatalog(filepath.Join(fixtureRoot(), "assignment.json"), fixtureRoot(), bundle, policy, time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC))
	if r.Status != "invalid" || r.Snapshot != nil {
		t.Fatalf("invalid catalog accepted: %+v", r)
	}
	r = ResolveWithCatalog(filepath.Join(fixtureRoot(), "assignment.json"), fixtureRoot(), bundle, policy, time.Time{})
	if r.Status != "invalid" || !hasDiagnostic(r, "resolve.catalog.as-of") {
		t.Fatalf("missing evaluation time accepted: %+v", r)
	}
}
