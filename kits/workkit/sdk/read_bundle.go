package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type readEvidence struct {
	Policy, Enrollment []byte
	Lineage            map[string][]byte
}

type ReadBundle struct {
	manifest                                         r.Manifest
	config                                           r.Configuration
	configBytes                                      []byte
	capture                                          *readsource.Capture
	records                                          []r.Record
	catalog                                          []r.CatalogEntry
	digest                                           string
	root                                             string
	bytes                                            int
	manifestRaw, sourceRaw, policyRaw, enrollmentRaw []byte
	lineage                                          map[string][]byte
}

func (b *ReadBundle) Manifest() r.Manifest      { return cloneRead[r.Manifest](b.manifest) }
func (b *ReadBundle) Records() []r.Record       { return cloneRead[[]r.Record](b.records) }
func (b *ReadBundle) Catalog() []r.CatalogEntry { return cloneRead[[]r.CatalogEntry](b.catalog) }
func (b *ReadBundle) Digest() string            { return b.digest }
func cloneRead[T any](v T) T {
	var out T
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	if e = json.Unmarshal(b, &out); e != nil {
		panic(e)
	}
	return out
}
func encodedRead(v any) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return append(b, '\n')
}
func buildReadBundle(configRaw []byte, c *readsource.Capture, transport r.TransportIdentity, policy, previous string, proof ...readEvidence) (r.Manifest, map[string][]byte, error) {
	var cfg r.Configuration
	if e := DecodeRead(configRaw, &cfg); e != nil {
		return r.Manifest{}, nil, e
	}
	if e := validateReadConfiguration(cfg); e != nil {
		return r.Manifest{}, nil, e
	}
	if !readsource.ValidCapture(c) || readsource.Digest(cfg.Source) != readsource.Digest(c.Snapshot.Source) {
		return r.Manifest{}, nil, errors.New("capture/configuration mismatch")
	}
	if transport.Principal != cfg.Source.PrincipalPseudonym || transport.Kind == "captured-source" && (transport.Version != "unverified" || transport.ExecutableSHA256 != "" || transport.AuthMode != cfg.Source.Auth.Mode) {
		return r.Manifest{}, nil, errors.New("transport context mismatch")
	}
	m := r.Manifest{Version: r.Version, ImplementationSHA256: engine.ImplementationSHA256, ConnectionID: cfg.ConnectionID, ConfigurationSHA256: SHA256(configRaw), SourceSHA256: SHA256(c.Files["manifest.json"]), ReadPolicySHA256: policy, AdapterVersion: r.AdapterVersion, Transport: transport, PreviousBundleSHA256: previous, Mode: c.Snapshot.Mode, Complete: c.Snapshot.Complete, Assets: []r.Asset{}, Diagnostics: append([]readsource.Diagnostic{}, c.Snapshot.Report.Diagnostics...), LiveQualification: "not-established"}
	if c.Snapshot.ObservedAt != "" {
		at := c.Snapshot.ObservedAt
		m.OriginalObservedAt = &at
		last := at
		for _, p := range c.Snapshot.Pages {
			a, _ := time.Parse(time.RFC3339Nano, p.ObservedAt)
			b, _ := time.Parse(time.RFC3339Nano, last)
			if a.After(b) {
				last = p.ObservedAt
			}
		}
		m.LastObservedAt = &last
	}
	files := map[string][]byte{"assets/configuration.json": append([]byte(nil), configRaw...)}
	roles := map[string]string{"assets/configuration.json": "configuration"}
	if len(proof) > 0 {
		if SHA256(proof[0].Policy) != policy {
			return m, nil, errors.New("policy input hash mismatch")
		}
		files["assets/read-policy.json"] = append([]byte(nil), proof[0].Policy...)
		roles["assets/read-policy.json"] = "read-policy"
		files["assets/enrollment.json"] = append([]byte(nil), proof[0].Enrollment...)
		roles["assets/enrollment.json"] = "enrollment"
		for name, raw := range proof[0].Lineage {
			if !strings.HasPrefix(name, "assets/lineage/") {
				return m, nil, errors.New("unsafe lineage asset")
			}
			files[name] = append([]byte(nil), raw...)
			roles[name] = "lineage"
		}
	} else if transport.Kind != "captured-source" || policy != cfg.Source.ReadPolicySHA256 {
		return m, nil, errors.New("live capture requires original policy and enrollment evidence")
	}

	for name, raw := range c.Files {
		if name == "snapshot.json" {
			continue
		}
		path := "assets/source/" + name
		files[path] = append([]byte(nil), raw...)
		roles[path] = "source"
	}
	records := []r.Record{}
	for _, i := range c.Snapshot.Items {
		records = append(records, r.Record{ConnectionID: cfg.ConnectionID, Principal: cfg.Source.PrincipalPseudonym, Identity: NativeIdentity(cfg, i), Data: i})
	}
	catalog := []r.CatalogEntry{}
	for key, raw := range c.Snapshot.Discovery {
		idx := strings.LastIndex(key, ":")
		if idx < 0 {
			return m, nil, errors.New("catalog provenance missing")
		}
		pointer, known := "", false
		for _, page := range c.Snapshot.Pages {
			if page.Asset.SHA256 == key[idx+1:] {
				pointer, known = catalogPointer(c.Files[page.Asset.Path], raw)
				break
			}
		}
		catalog = append(catalog, r.CatalogEntry{Name: key[:idx], Data: raw, AssetSHA256: key[idx+1:], JSONPointer: pointer, ProvenanceKnown: known})
	}
	sort.Slice(catalog, func(i, j int) bool {
		return catalog[i].Name+catalog[i].AssetSHA256 < catalog[j].Name+catalog[j].AssetSHA256
	})
	// Batches are assets, not workspace/configuration resources. Bound each file.
	addBatches := func(role string, rows []json.RawMessage) error {
		batch := []json.RawMessage{}
		size := 3
		index := 0
		flush := func() {
			p := fmt.Sprintf("assets/%s/%04d.json", role, index)
			files[p] = encodedRead(batch)
			roles[p] = role
			index++
			batch = []json.RawMessage{}
			size = 3
		}
		for _, row := range rows {
			if len(row)+3 > engine.MaxDocumentBytes {
				return errors.New("individual normalized value exceeds document bound")
			}
			if size+len(row)+1 > engine.MaxDocumentBytes {
				flush()
			}
			batch = append(batch, row)
			size += len(row) + 1
		}
		flush()
		return nil
	}
	itemRows := []json.RawMessage{}
	for _, v := range records {
		b, _ := json.Marshal(v)
		itemRows = append(itemRows, b)
	}
	if e := addBatches("items", itemRows); e != nil {
		return m, nil, e
	}
	catRows := []json.RawMessage{}
	for _, v := range catalog {
		b, _ := json.Marshal(v)
		catRows = append(catRows, b)
	}
	if e := addBatches("catalog", catRows); e != nil {
		return m, nil, e
	}
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		m.Assets = append(m.Assets, r.Asset{Path: name, SHA256: SHA256(files[name]), Role: roles[name]})
	}
	files["bundle.json"] = encodedRead(m)
	total := 0
	for _, raw := range files {
		if len(raw) > engine.MaxDocumentBytes {
			return m, nil, errors.New("document budget exceeded")
		}
		total += len(raw)
	}
	if total > engine.MaxTotalBytes || len(files) > engine.MaxResources {
		return m, nil, errors.New("capture aggregate output budget exceeded; narrow scope")
	}
	return m, files, nil
}
func ImportRead(ctx context.Context, configuration, manifest, out string) (r.Manifest, error) {
	c, raw, e := LoadReadConfiguration(ctx, configuration)
	if e != nil {
		return r.Manifest{}, readFailure(e)
	}
	capture, e := importSource(ctx, manifest)
	if e != nil {
		return r.Manifest{}, readFailure(e)
	}
	transport := r.TransportIdentity{Kind: "captured-source", Version: "unverified", Principal: c.Source.PrincipalPseudonym, AuthMode: c.Source.Auth.Mode}
	m, files, e := buildReadBundle(raw, capture, transport, c.Source.ReadPolicySHA256, "")
	if e == nil {
		e = engine.WriteBundle(ctx, out, files, filepath.Dir(manifest))
	}
	if e != nil {
		return m, readFailure(e)
	}
	return m, nil
}
func LoadReadBundle(ctx context.Context, root string) (*ReadBundle, error) {
	raw, e := ReadLocal(ctx, filepath.Join(root, "bundle.json"))
	if e != nil {
		return nil, readFailure(e)
	}
	var m r.Manifest
	if e = DecodeRead(raw, &m); e != nil {
		return nil, readFailure(e)
	}
	if !readDigestOrEmpty(m.ImplementationSHA256) || m.ImplementationSHA256 == "" || !readDigestOrEmpty(m.PreviousBundleSHA256) || m.Version != r.Version || m.AdapterVersion != r.AdapterVersion || m.AuthorizationGranted || m.ExecutionReady || m.MigrationActivated || m.ProductionActivated || m.LiveQualification != "not-established" || len(m.Assets) < 3 || len(m.Assets) > 1023 {
		return nil, readFailure(errors.New("unsupported or activating read bundle"))
	}
	files := map[string][]byte{}
	total := len(raw)
	roles := map[string]string{}
	for _, a := range m.Assets {
		if roles[a.Path] != "" || !strings.HasPrefix(a.Path, "assets/") {
			return nil, readFailure(errors.New("duplicate/unsafe read asset"))
		}
		if a.Role != "configuration" && a.Role != "source" && a.Role != "items" && a.Role != "catalog" && a.Role != "read-policy" && a.Role != "enrollment" && a.Role != "lineage" {
			return nil, readFailure(errors.New("unknown asset role"))
		}
		p, e := ConfinedLocal(root, a.Path)
		if e != nil {
			return nil, readFailure(e)
		}
		b, e := ReadLocal(ctx, p)
		if e != nil || SHA256(b) != a.SHA256 {
			return nil, readFailure(errors.New("asset hash mismatch"))
		}
		total += len(b)
		if total > engine.MaxTotalBytes {
			return nil, readFailure(errors.New("read budget exceeded"))
		}
		files[a.Path] = b
		roles[a.Path] = a.Role
	}
	configRaw := files["assets/configuration.json"]
	var cfg r.Configuration
	if SHA256(configRaw) != m.ConfigurationSHA256 || DecodeRead(configRaw, &cfg) != nil || validateReadConfiguration(cfg) != nil {
		return nil, readFailure(errors.New("configuration binding invalid"))
	}
	var source readsource.Manifest
	sourceRaw := files["assets/source/manifest.json"]
	if SHA256(sourceRaw) != m.SourceSHA256 || readsource.Decode(sourceRaw, "manifest", &source) != nil {
		return nil, readFailure(errors.New("source manifest binding invalid"))
	}
	sourceFiles := map[string][]byte{}
	for _, page := range source.Pages {
		b, ok := files["assets/source/"+page.Asset.Path]
		if !ok {
			return nil, readFailure(errors.New("missing source page"))
		}
		sourceFiles[page.Asset.Path] = b
	}
	capture, e := readsource.ReconstructBytes(sourceRaw, sourceFiles)
	if e != nil {
		return nil, readFailure(e)
	}
	proof := []readEvidence{}
	if m.Transport.Kind != "captured-source" {
		p, en := files["assets/read-policy.json"], files["assets/enrollment.json"]
		if len(p) == 0 || len(en) == 0 {
			return nil, readFailure(errors.New("missing live read inputs"))
		}
		var enrollment r.ReadEnrollment
		if DecodeRead(en, &enrollment) != nil {
			return nil, readFailure(errors.New("invalid original enrollment"))
		}
		observed, e := time.Parse(time.RFC3339Nano, enrollment.IssuedAt)
		if e != nil {
			return nil, readFailure(e)
		}
		if _, _, _, e = validateReadEnrollment(configRaw, p, en, observed, false); e != nil {
			return nil, readFailure(e)
		}
		if readsource.Digest(enrollment.Transport) != readsource.Digest(m.Transport) {
			return nil, readFailure(errors.New("transport evidence mismatch"))
		}
		lineage := map[string][]byte{}
		for name, role := range roles {
			if role == "lineage" {
				lineage[name] = files[name]
			}
		}
		if e = validateReadLineage(configRaw, m, source, p, en, lineage); e != nil {
			return nil, readFailure(e)
		}
		proof = append(proof, readEvidence{Policy: p, Enrollment: en, Lineage: lineage})
	}
	expected, rebuilt, e := buildReadBundle(configRaw, capture, m.Transport, m.ReadPolicySHA256, m.PreviousBundleSHA256, proof...)
	if e != nil {
		return nil, readFailure(e)
	}
	expected.ImplementationSHA256 = m.ImplementationSHA256
	if !bytes.Equal(encodedRead(expected), encodedRead(m)) || len(rebuilt) != len(files)+1 {
		return nil, readFailure(errors.New("manifest claims differ from original sources"))
	}
	for name, b := range files {
		if !bytes.Equal(rebuilt[name], b) {
			return nil, readFailure(errors.New("derived asset differs from original sources"))
		}
	}
	seen := 0
	e = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		seen++
		if seen > 4096 || d.Type()&os.ModeSymlink != 0 {
			return errors.New("unsafe read directory")
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if !d.IsDir() {
			rel, e := filepath.Rel(root, path)
			if e != nil {
				return e
			}
			name := filepath.ToSlash(rel)
			if name != "bundle.json" && roles[name] == "" {
				return errors.New("unindexed read file")
			}
		}
		return nil
	})
	if e != nil {
		return nil, readFailure(e)
	}
	b := &ReadBundle{manifest: m, config: cfg, configBytes: append([]byte(nil), configRaw...), capture: capture, digest: SHA256(raw), root: root, bytes: total, manifestRaw: raw, sourceRaw: sourceRaw, policyRaw: files["assets/read-policy.json"], enrollmentRaw: files["assets/enrollment.json"], lineage: map[string][]byte{}}
	for _, a := range m.Assets {
		if a.Role == "lineage" {
			b.lineage[a.Path] = files[a.Path]
		}
		switch a.Role {
		case "items":
			var rows []r.Record
			if e = DecodeRead(files[a.Path], &rows); e != nil {
				return nil, readFailure(e)
			}
			b.records = append(b.records, rows...)
		case "catalog":
			var rows []r.CatalogEntry
			if e = DecodeRead(files[a.Path], &rows); e != nil {
				return nil, readFailure(e)
			}
			b.catalog = append(b.catalog, rows...)
		}
	}
	for _, a := range m.Assets {
		p, e := ConfinedLocal(root, a.Path)
		if e != nil {
			return nil, readFailure(e)
		}
		now, e := ReadLocal(ctx, p)
		if e != nil || !bytes.Equal(now, files[a.Path]) {
			return nil, readFailure(errors.New("asset changed during read"))
		}
	}
	again, e := ReadLocal(ctx, filepath.Join(root, "bundle.json"))
	if e != nil || !bytes.Equal(raw, again) {
		return nil, readFailure(errors.New("bundle changed during read"))
	}
	return b, nil
}

func catalogPointer(raw, target []byte) (string, bool) {
	value, e := engine.JSONValue(raw)
	if e != nil {
		return "", false
	}
	wanted, e := engine.JSONValue(target)
	if e != nil {
		return "", false
	}
	hash := sourceSemantic(wanted)
	matches := []string{}
	visited := 0
	var walk func(any, string)
	walk = func(v any, p string) {
		visited++
		if visited > 4096 {
			return
		}
		if sourceSemantic(v) == hash {
			matches = append(matches, p)
		}
		switch x := v.(type) {
		case map[string]any:
			keys := []string{}
			for key := range x {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				walk(x[key], p+"/"+strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1"))
			}
		case []any:
			for i, child := range x {
				walk(child, fmt.Sprintf("%s/%d", p, i))
			}
		}
	}
	walk(value, "")
	if visited > 4096 || len(matches) != 1 {
		return "", false
	}
	return matches[0], true
}
