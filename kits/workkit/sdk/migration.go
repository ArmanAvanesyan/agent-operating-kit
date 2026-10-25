package sdk

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
)

type SourceRequest struct{ Source, Format, AsOf string }
type LegacyCapture struct {
	Files                  map[string][]byte
	Projections            [][]byte
	OriginalObservedAt     *string
	Losses                 []string
	UnresolvedOperationIDs []string
	Provenance             json.RawMessage
}

// LegacyReader is injected by integration bootstrap; the kit has no legacy
// package imports or provider authority. Inspect must only read bounded files.
type LegacyReader interface {
	Inspect(context.Context, SourceRequest) (LegacyCapture, error)
}
type MigrationReport struct {
	SourceFormat           string   `json:"sourceFormat"`
	Resources              int      `json:"resources"`
	Losses                 []string `json:"losses"`
	UnresolvedOperationIDs []string `json:"unresolvedOperationIds"`
	OriginalObservedAt     *string  `json:"originalObservedAt"`
	Activate               bool     `json:"activate"`
	AuthorizationGranted   bool     `json:"authorizationGranted"`
	ExecutionReady         bool     `json:"executionReady"`
}

func (s *Service) migration(ctx context.Context, r SourceRequest) (MigrationReport, map[string][]byte, error) {
	if _, e := time.Parse(time.RFC3339Nano, r.AsOf); e != nil {
		return MigrationReport{}, nil, failure("migration.as-of", "as-of must be RFC3339", 2, e)
	}
	if s.legacy == nil {
		return MigrationReport{}, nil, failure("migration.unsupported", "legacy adapter unavailable", 3, nil)
	}
	captured, e := s.legacy.Inspect(ctx, r)
	if e != nil {
		var known *Error
		if errors.As(e, &known) {
			return MigrationReport{}, nil, e
		}
		return MigrationReport{}, nil, failure("migration.source", "legacy source failed bounded validation", 2, e)
	}
	report := MigrationReport{SourceFormat: r.Format, Losses: append([]string{}, captured.Losses...), UnresolvedOperationIDs: append([]string{}, captured.UnresolvedOperationIDs...), OriginalObservedAt: captured.OriginalObservedAt}
	files := map[string][]byte{}
	sources := []any{}
	names := make([]string, 0, len(captured.Files))
	for name := range captured.Files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b := captured.Files[name]
		target := "assets/original/" + name
		files[target] = append([]byte(nil), b...)
		sources = append(sources, map[string]any{"path": target, "sha256": SHA256(b)})
	}
	if len(sources) == 0 {
		return report, nil, failure("migration.source", "legacy source has no preservable assets", 2, nil)
	}
	for _, raw := range captured.Projections {
		d, e := Parse(raw, "json")
		if e != nil {
			report.Losses = append(report.Losses, "projection failed target contract; original bytes retained")
			continue
		}
		spec := engine.Map(d.document.Value["spec"])
		if d.Kind() == "WorkBinding" && spec["status"] != "draft" || d.Kind() == "SyncRoute" && spec["enabled"] != false {
			report.Losses = append(report.Losses, "active binding/route projection refused; original bytes retained")
			continue
		}
		name := "resources/" + d.document.Kind + "-" + d.document.ID + ".json"
		if _, ok := files[name]; ok {
			return report, nil, failure("migration.conflict", "duplicate projected identity", 4, nil)
		}
		files[name] = d.Bytes()
		report.Resources++
	}
	sort.Strings(report.Losses)
	sort.Strings(report.UnresolvedOperationIDs)
	index := []any{}
	names = nil
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		index = append(index, map[string]any{"path": name, "sha256": SHA256(files[name])})
	}
	// The implementation pin binds this kit's immutable, versioned conversion
	// protocol. Source implementation provenance is supplied independently below.
	implementation := engine.ImplementationSHA256
	manifest := map[string]any{"apiVersion": "workkit/v1alpha1", "kind": "MigrationManifest", "metadata": map[string]any{"id": "aok-import", "revision": 1}, "spec": map[string]any{"sourceFormat": r.Format, "sourceAssets": sources, "implementationSha256": implementation, "originalObservedAt": captured.OriginalObservedAt, "destinationFormat": "workkit/v1alpha1", "losses": report.Losses, "unresolvedOperationIds": report.UnresolvedOperationIDs, "activate": false}, "extensions": map[string]any{"workkit/import-assets": index, "workkit/inspection-as-of": r.AsOf, "aok/original-provenance": captured.Provenance}}
	raw := engine.Encode(manifest)
	if _, e := Parse(raw, "json"); e != nil {
		return report, nil, e
	}
	files["bundle.json"] = raw
	return report, files, nil
}
func (s *Service) InspectMigration(ctx context.Context, r SourceRequest) (MigrationReport, error) {
	report, _, e := s.migration(ctx, r)
	return report, e
}
func (s *Service) Import(ctx context.Context, r SourceRequest, out string) (MigrationReport, error) {
	report, files, e := s.migration(ctx, r)
	if e != nil {
		return report, e
	}
	source := r.Source
	if info, err := os.Stat(source); err == nil && !info.IsDir() {
		source = filepath.Dir(source)
	}
	if e = engine.WriteBundle(ctx, out, files, source); e != nil {
		return report, failure("migration.output", "immutable output could not be created", 1, e)
	}
	return report, nil
}
func (s *Service) VerifyMigration(ctx context.Context, root string) (MigrationReport, error) {
	b, e := engine.ReadFile(ctx, filepath.Join(root, "bundle.json"))
	if e != nil {
		return MigrationReport{}, failure("migration.invalid", "import bundle is incomplete or unsafe", 2, e)
	}
	d, e := engine.Parse(b, "json")
	if e != nil || d.Kind != "MigrationManifest" {
		return MigrationReport{}, failure("migration.invalid", "invalid migration manifest", 2, e)
	}
	spec := engine.Map(d.Value["spec"])
	extensions := engine.Map(d.Value["extensions"])
	allowed := map[string]bool{"workkit/import-assets": true, "workkit/inspection-as-of": true, "aok/original-provenance": true}
	for k := range extensions {
		if !allowed[k] {
			return MigrationReport{}, failure("migration.invalid", "unsupported import extension", 2, nil)
		}
	}
	if _, e = time.Parse(time.RFC3339Nano, engine.String(extensions["workkit/inspection-as-of"])); e != nil {
		return MigrationReport{}, failure("migration.invalid", "missing inspection time", 2, e)
	}
	expected := map[string]bool{"bundle.json": true}
	total := 0
	count := 0
	hashes := map[string]string{}
	index, ok := extensions["workkit/import-assets"].([]any)
	if !ok {
		return MigrationReport{}, failure("migration.invalid", "missing import index", 2, nil)
	}
	for _, raw := range index {
		asset := engine.Map(raw)
		if len(asset) != 2 {
			return MigrationReport{}, failure("migration.invalid", "invalid asset entry", 2, nil)
		}
		name := engine.String(asset["path"])
		if expected[name] || !(strings.HasPrefix(name, "assets/original/") || strings.HasPrefix(name, "resources/")) {
			return MigrationReport{}, failure("migration.invalid", "duplicate or unsafe import asset", 2, nil)
		}
		expected[name] = true
		p, e := engine.Confined(root, name)
		if e != nil {
			return MigrationReport{}, failure("migration.invalid", "unsafe import asset", 2, e)
		}
		data, e := engine.ReadFile(ctx, p)
		if e != nil {
			return MigrationReport{}, failure("migration.invalid", "missing import asset", 2, e)
		}
		total += len(data)
		if total > engine.MaxTotalBytes || len(expected) > engine.MaxResources {
			return MigrationReport{}, failure("migration.invalid", "import bounds exceeded", 2, nil)
		}
		h := SHA256(data)
		if h != engine.String(asset["sha256"]) {
			return MigrationReport{}, failure("migration.invalid", "changed import asset", 2, nil)
		}
		hashes[name] = h
		if strings.HasPrefix(name, "resources/") {
			if _, e = engine.Parse(data, "json"); e != nil {
				return MigrationReport{}, failure("migration.invalid", "invalid projected resource", 2, e)
			}
			if projected, e := engine.Parse(data, "json"); e == nil {
				p := engine.Map(projected.Value["spec"])
				if projected.Kind == "WorkBinding" && p["status"] != "draft" || projected.Kind == "SyncRoute" && p["enabled"] != false {
					return MigrationReport{}, failure("migration.invalid", "active imported projection rejected", 2, nil)
				}
			}
			count++
		}
	}
	for _, a := range engine.List(spec["sourceAssets"]) {
		asset := engine.Map(a)
		if h, ok := hashes[engine.String(asset["path"])]; !ok || h != engine.String(asset["sha256"]) {
			return MigrationReport{}, failure("migration.invalid", "original source pin mismatch", 2, nil)
		}
	}
	seen := 0
	e = filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		seen++
		if seen > 4096 {
			return errors.New("import entry limit")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errors.New("import symlink")
		}
		if !d.IsDir() {
			rel, e := filepath.Rel(root, p)
			if e != nil || !expected[filepath.ToSlash(rel)] {
				return errors.New("unmanifested import file")
			}
		}
		return nil
	})
	if e != nil {
		return MigrationReport{}, failure("migration.invalid", "unsafe import contents", 2, e)
	}
	report := MigrationReport{SourceFormat: engine.String(spec["sourceFormat"]), Resources: count, Losses: []string{}, UnresolvedOperationIDs: []string{}}
	for _, v := range engine.List(spec["losses"]) {
		report.Losses = append(report.Losses, engine.String(v))
	}
	for _, v := range engine.List(spec["unresolvedOperationIds"]) {
		report.UnresolvedOperationIDs = append(report.UnresolvedOperationIDs, engine.String(v))
	}
	if at, ok := spec["originalObservedAt"].(string); ok {
		report.OriginalObservedAt = &at
	}
	return report, nil
}
