package nativeadapter

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/nativeparse"
)

const (
	maxBundleFiles = 128
	maxBundleBytes = 4 << 20
)

type Engine struct{ registry *Registry }

func NewEngine(registry *Registry) *Engine {
	if registry == nil {
		registry = NewRegistry()
	}
	return &Engine{registry: registry}
}

func (r *Registry) Register(adapter Adapter) error {
	if r == nil {
		return errors.New("native adapter registry is nil")
	}
	if adapter == nil {
		return errors.New("native adapter is nil")
	}
	d := adapter.Descriptor()
	if strings.TrimSpace(d.Target) == "" || strings.TrimSpace(d.AdapterVersion) == "" {
		return errors.New("adapter target and version are required")
	}
	if err := validateDescriptor(d); err != nil {
		return err
	}
	if r.adapters == nil {
		r.adapters = map[string]Adapter{}
	}
	if _, exists := r.adapters[d.Target]; exists {
		return fmt.Errorf("adapter target %q is already registered", d.Target)
	}
	r.adapters[d.Target] = adapter
	return nil
}

func (r *Registry) Lookup(target string) (Adapter, bool) {
	if r == nil {
		return nil, false
	}
	a, ok := r.adapters[target]
	return a, ok
}

func (e *Engine) Inspect(target, scope string, files []File, required []Control) Report {
	r := newReport("inspect", target, scope)
	a, d, ok := e.adapter(target, scope, &r)
	if !ok {
		return finish(r)
	}
	r.HostLoading = d.HostLoading
	if err := validateFiles(files); err != nil {
		add(&r, "native.input.files", target, "files", "error", err.Error())
		return finish(r)
	}
	doc, ds, err := a.Decode(scope, cloneFiles(files))
	r.Diagnostics = append(r.Diagnostics, sanitizeAdapterDiagnostics(ds, target)...)
	if err != nil {
		add(&r, "native.decode", target, "", "error", "adapter could not decode supplied native input")
		r.Status = "invalid"
		return finish(r)
	}
	if err := validateDocument(doc, d); err != nil {
		add(&r, "native.document", target, "", "error", err.Error())
		r.Status = "invalid"
		return finish(r)
	}
	if sensitive := sensitiveFields(doc); len(sensitive) != 0 {
		for _, key := range sensitive {
			add(&r, "native.credential-field", target, key, "error", "credential-bearing field is not permitted in a native bundle")
		}
	}
	checkControls(&r, d, required, doc)
	r.Projection = mapKeys(doc.Fields)
	r.Unknown = mapKeys(doc.Unknown)
	r.SourceHashes = hashesFor(files)
	r.Status, r.MappingStatus = "valid", "inspected"
	if hasError(r.Diagnostics) {
		r.Status = "blocked"
		r.MappingStatus = "blocked"
	} else if hasWarning(r.Diagnostics) {
		r.MappingStatus = "lossy"
	}
	return finish(r)
}

// Import creates an explicitly incomplete projection Bundle. Supplying a prior
// bundle binds the operation to its original files and makes drift a conflict.
func (e *Engine) Import(target, scope string, files []File, prior *Bundle, required []Control) (Bundle, Report) {
	r := newReport("import", target, scope)
	a, d, ok := e.adapter(target, scope, &r)
	if !ok {
		return Bundle{}, finish(r)
	}
	r.HostLoading = d.HostLoading
	if err := validateFiles(files); err != nil {
		add(&r, "native.input.files", target, "files", "error", err.Error())
		return Bundle{}, finish(r)
	}
	if prior != nil {
		if err := validateBundle(*prior, target, scope, d); err != nil {
			add(&r, "native.bundle.integrity", target, "bundle", "error", err.Error())
			r.Status = "invalid"
			return Bundle{}, finish(r)
		}
		if !sameFileSet(files, currentBoundFiles(*prior)) {
			add(&r, "native.bundle.conflict", target, "files", "error", "current native files differ from sidecar-bound files")
			r.Status = "invalid"
			return Bundle{}, finish(r)
		}
	}
	doc, ds, err := a.Decode(scope, cloneFiles(files))
	r.Diagnostics = append(r.Diagnostics, sanitizeAdapterDiagnostics(ds, target)...)
	if err != nil {
		add(&r, "native.decode", target, "", "error", "adapter could not decode supplied native input")
		r.Status = "invalid"
		return Bundle{}, finish(r)
	}
	if err := validateDocument(doc, d); err != nil {
		add(&r, "native.document", target, "", "error", err.Error())
		r.Status = "invalid"
		return Bundle{}, finish(r)
	}
	sensitive := sensitiveFields(doc)
	for _, key := range sensitive {
		add(&r, "native.credential-field", target, key, "error", "credential-bearing field is not permitted in a native bundle")
	}
	checkControls(&r, d, required, doc)
	r.Projection, r.Unknown, r.SourceHashes = mapKeys(doc.Fields), mapKeys(doc.Unknown), hashesFor(files)
	if len(sensitive) != 0 {
		r.Status = "blocked"
		return Bundle{}, finish(r)
	}
	if hasError(r.Diagnostics) {
		r.Status = "blocked"
	}
	originalFiles := bindFiles(files)
	var renderedFiles []BoundFile
	if prior != nil {
		originalFiles = cloneBoundFiles(prior.OriginalFiles)
		renderedFiles = cloneBoundFiles(prior.RenderedFiles)
	}
	b := Bundle{Version: BundleVersion, Target: target, Scope: scope, AdapterVersion: d.AdapterVersion, Completeness: "draft-incomplete", OriginalFiles: originalFiles, RenderedFiles: renderedFiles, Document: cloneDocument(doc)}
	if prior != nil {
		b.Canonical = cloneCanonical(prior.Canonical)
	}
	if prior == nil {
		add(&r, "native.provenance.incomplete", target, "sidecar", "warning", "no prior sidecar was supplied; imported projection remains an incomplete canonical draft")
	}
	b.Digest, err = bundleDigest(b)
	if err != nil {
		add(&r, "native.bundle.digest", target, "bundle", "error", "could not create bundle integrity digest")
		r.Status = "invalid"
		return Bundle{}, finish(r)
	}
	if err := checkBundleSize(b); err != nil {
		add(&r, "native.bundle.size", target, "bundle", "error", err.Error())
		r.Status = "blocked"
		return Bundle{}, finish(r)
	}
	r.MappingStatus = "draft"
	if hasError(r.Diagnostics) {
		r.MappingStatus = "blocked"
	}
	r.Status = "blocked" // a projection has not passed canonical corpus validation
	return b, finish(r)
}

// Export renders a candidate from an explicit Bundle. It refuses integrity or
// source drift conflicts, but required unsupported controls only block status;
// a semantic candidate may still be returned for review.
func (e *Engine) Export(target, scope string, current []File, b Bundle, required []Control) ([]File, Bundle, Report) {
	r := newReport("export", target, scope)
	a, d, ok := e.adapter(target, scope, &r)
	if !ok {
		return nil, Bundle{}, finish(r)
	}
	r.HostLoading = d.HostLoading
	if sensitive := sensitiveFields(b.Document); len(sensitive) != 0 {
		for _, key := range sensitive {
			add(&r, "native.credential-field", target, key, "error", "credential-bearing field is not permitted in a native bundle")
		}
		r.Status, r.MappingStatus = "blocked", "blocked"
		return nil, Bundle{}, finish(r)
	}
	if err := validateBundle(b, target, scope, d); err != nil {
		add(&r, "native.bundle.integrity", target, "bundle", "error", err.Error())
		r.Status = "invalid"
		return nil, Bundle{}, finish(r)
	}
	if err := validateFiles(current); err != nil {
		add(&r, "native.input.files", target, "files", "error", err.Error())
		r.Status = "invalid"
		return nil, Bundle{}, finish(r)
	}
	if !sameFileSet(current, currentBoundFiles(b)) {
		add(&r, "native.bundle.conflict", target, "files", "error", "current native files differ from sidecar-bound files")
		r.Status = "invalid"
		return nil, Bundle{}, finish(r)
	}
	if err := validateDocument(b.Document, d); err != nil {
		add(&r, "native.document", target, "", "error", err.Error())
		r.Status = "invalid"
		return nil, Bundle{}, finish(r)
	}
	if sensitive := sensitiveFields(b.Document); len(sensitive) != 0 {
		for _, key := range sensitive {
			add(&r, "native.credential-field", target, key, "error", "credential-bearing field is not permitted in a native bundle")
		}
		r.Status = "blocked"
		return nil, Bundle{}, finish(r)
	}
	checkControls(&r, d, required, b.Document)
	files, ds, err := a.Encode(scope, cloneDocument(b.Document))
	r.Diagnostics = append(r.Diagnostics, sanitizeAdapterDiagnostics(ds, target)...)
	if err != nil {
		add(&r, "native.encode", target, "", "error", "adapter could not encode the requested projection")
		r.Status = "invalid"
		return nil, Bundle{}, finish(r)
	}
	if err = validateFiles(files); err != nil {
		add(&r, "native.output.files", target, "files", "error", "adapter produced an invalid file set")
		r.Status = "invalid"
		return nil, Bundle{}, finish(r)
	}
	decoded, decodeDS, decodeErr := a.Decode(scope, cloneFiles(files))
	r.Diagnostics = append(r.Diagnostics, sanitizeAdapterDiagnostics(decodeDS, target)...)
	if decodeErr != nil || !documentsEqual(b.Document, decoded) {
		add(&r, "native.export.roundtrip", target, "projection", "error", "encoded candidate does not preserve the requested projection")
		r.Status = "blocked"
		return nil, Bundle{}, finish(r)
	}
	if sensitive := sensitiveFields(decoded); len(sensitive) != 0 {
		for _, key := range sensitive {
			add(&r, "native.credential-field", target, key, "error", "credential-bearing field is not permitted in exported data")
		}
		r.Status = "blocked"
		return nil, Bundle{}, finish(r)
	}
	next := cloneBundle(b)
	next.RenderedFiles = bindFiles(files)
	next.Digest, err = bundleDigest(next)
	if err != nil {
		add(&r, "native.bundle.digest", target, "bundle", "error", "could not update bundle integrity digest")
		r.Status = "invalid"
		return nil, Bundle{}, finish(r)
	}
	if err := checkBundleSize(next); err != nil {
		add(&r, "native.bundle.size", target, "bundle", "error", "rendered bundle exceeds 4 MiB")
		r.Status = "blocked"
		return nil, Bundle{}, finish(r)
	}
	r.Projection, r.Unknown, r.SourceHashes = mapKeys(b.Document.Fields), mapKeys(b.Document.Unknown), hashesFor(files)
	r.Status = "blocked" // exported output is still an incomplete canonical draft
	if hasError(r.Diagnostics) {
		r.MappingStatus = "blocked"
	} else if hasWarning(r.Diagnostics) {
		r.MappingStatus = "lossy"
	} else {
		r.MappingStatus = "draft"
	}
	return cloneFiles(files), next, finish(r)
}

func (e *Engine) Compare(target, scope string, current []File, b Bundle, required []Control) Report {
	r := newReport("compare", target, scope)
	a, d, ok := e.adapter(target, scope, &r)
	if !ok {
		return finish(r)
	}
	r.HostLoading = d.HostLoading
	if err := validateBundle(b, target, scope, d); err != nil {
		add(&r, "native.bundle.integrity", target, "bundle", "error", err.Error())
		r.Status = "invalid"
		return finish(r)
	}
	if err := validateFiles(current); err != nil {
		add(&r, "native.input.files", target, "files", "error", err.Error())
		r.Status = "invalid"
		return finish(r)
	}
	conflict := !sameFileSet(current, currentBoundFiles(b))
	if conflict {
		add(&r, "native.bundle.conflict", target, "files", "error", "current native files differ from sidecar-bound originals")
	}
	doc, ds, err := a.Decode(scope, cloneFiles(current))
	r.Diagnostics = append(r.Diagnostics, sanitizeAdapterDiagnostics(ds, target)...)
	if err != nil {
		add(&r, "native.decode", target, "", "error", "adapter could not decode supplied native input")
		r.Status = "invalid"
		return finish(r)
	}
	if err := validateDocument(doc, d); err != nil {
		add(&r, "native.document", target, "", "error", err.Error())
		r.Status = "invalid"
		return finish(r)
	}
	for _, key := range sensitiveFields(doc) {
		add(&r, "native.credential-field", target, key, "error", "credential-bearing field is not permitted in native data")
	}
	checkControls(&r, d, required, doc)
	r.Differences = compareDocuments(b.Document, doc)
	r.Projection, r.Unknown, r.SourceHashes = mapKeys(doc.Fields), mapKeys(doc.Unknown), hashesFor(current)
	if conflict {
		r.Status, r.MappingStatus = "invalid", "lossy"
	} else if hasError(r.Diagnostics) {
		r.Status, r.MappingStatus = "blocked", "blocked"
	} else {
		r.Status = "valid"
		if len(r.Differences) > 0 || hasWarning(r.Diagnostics) {
			r.MappingStatus = "lossy"
		} else {
			r.MappingStatus = "compatible"
		}
	}
	return finish(r)
}

func (e *Engine) adapter(target, scope string, r *Report) (Adapter, Descriptor, bool) {
	if e == nil || e.registry == nil {
		add(r, "native.target.unregistered", target, "target", "error", "no adapter is explicitly registered for target")
		r.Status = "blocked"
		return nil, Descriptor{}, false
	}
	a, ok := e.registry.Lookup(target)
	if !ok {
		add(r, "native.target.unregistered", target, "target", "error", "no adapter is explicitly registered for target")
		r.Status = "blocked"
		return nil, Descriptor{}, false
	}
	d := a.Descriptor()
	if err := validateDescriptor(d); err != nil || d.Target != target {
		add(r, "native.adapter.descriptor", target, "descriptor", "error", "registered adapter descriptor is invalid")
		r.Status = "invalid"
		return nil, Descriptor{}, false
	}
	if !contains(d.Scopes, scope) {
		add(r, "native.scope.unsupported", target, "scope", "error", "scope is not supported by the registered adapter")
		r.Status = "blocked"
		return nil, Descriptor{}, false
	}
	if d.HostLoading.Status == "verified" && !contains(d.HostLoading.Scopes, scope) {
		d.HostLoading.Status = "not-run"
	}
	return a, d, true
}

func validateDescriptor(d Descriptor) error {
	if d.Target == "" || d.AdapterVersion == "" {
		return errors.New("adapter target and version are required")
	}
	if len(d.Scopes) == 0 {
		return errors.New("adapter must declare at least one scope")
	}
	scopes := map[string]bool{}
	for _, s := range d.Scopes {
		if s == "" || scopes[s] {
			return errors.New("adapter scopes must be non-empty and unique")
		}
		scopes[s] = true
	}
	neutral, native := map[string]bool{}, map[string]bool{}
	for _, f := range d.Fields {
		if f.NeutralPath == "" || f.NativePath == "" || neutral[f.NeutralPath] || native[f.NativePath] {
			return errors.New("adapter field paths must be non-empty and unique")
		}
		if !contains([]string{"native", "executor-backed", "guidance-only", "unsupported"}, f.Support) {
			return errors.New("adapter field has invalid support value")
		}
		if !contains([]string{"native", "executor", "advisory", "none"}, f.EnforcementOwner) {
			return errors.New("adapter field has invalid enforcement owner")
		}
		neutral[f.NeutralPath], native[f.NativePath] = true, true
	}
	if !contains([]string{"not-run", "verified", "unavailable"}, d.HostLoading.Status) {
		return errors.New("host loading evidence status is invalid")
	}
	if d.HostLoading.Status == "verified" && (strings.TrimSpace(d.HostLoading.HostVersion) == "" || len(d.HostLoading.Evidence) == 0 || len(d.HostLoading.Scopes) == 0) {
		return errors.New("verified host loading requires host version, evidence, and scope binding")
	}
	return nil
}

func checkControls(r *Report, d Descriptor, required []Control, doc Document) {
	capabilities := map[string]FieldCapability{}
	for _, f := range d.Fields {
		capabilities[f.NeutralPath] = f
	}
	for _, control := range required {
		f, found := capabilities[control.Path]
		if !found || f.Support == "unsupported" || f.Support == "guidance-only" || f.EnforcementOwner == "none" || f.EnforcementOwner == "advisory" || f.Support == "executor-backed" || f.EnforcementOwner == "executor" {
			severity, code := "warning", "native.control.optional-unsupported"
			if control.Required {
				severity, code = "error", "native.control.required-unsupported"
			}
			message := "control has no supported native mapping"
			if f.EnforcementOwner == "executor" || f.Support == "executor-backed" {
				message = "executor-backed enforcement is unavailable in the native adapter engine"
			}
			add(r, code, d.Target, control.Path, severity, message)
			continue
		}
		if _, present := doc.Fields[control.Path]; !present {
			severity, code := "warning", "native.control.optional-missing"
			if control.Required {
				severity, code = "error", "native.control.required-missing"
			}
			add(r, code, d.Target, control.Path, severity, "control is not present in the decoded projection")
		}
	}
}

func validateDocument(doc Document, d Descriptor) error {
	if err := validateAssets(doc.Assets); err != nil {
		return err
	}
	known := map[string]bool{}
	for _, f := range d.Fields {
		known[f.NeutralPath] = true
	}
	for field, raw := range doc.Fields {
		if !validDottedPath(field) {
			return errors.New("document contains a non-canonical neutral field path")
		}
		if !known[field] {
			return errors.New("document contains an undeclared mapped field")
		}
		if _, err := contracts.DecodeJSON(raw); err != nil {
			return errors.New("document contains invalid JSON field data")
		}
	}
	for field, raw := range doc.Unknown {
		if !validNativePath(field) {
			return errors.New("document contains a non-canonical native field path")
		}
		if _, err := contracts.DecodeJSON(raw); err != nil {
			return errors.New("document contains invalid JSON field data")
		}
		for _, cap := range d.Fields {
			if cap.Support != "unsupported" && pathsOverlap(field, cap.NativePath) {
				return errors.New("mapped field collides with inert unknown field")
			}
		}
	}
	return nil
}

func sensitiveFields(doc Document) []string {
	out := []string{}
	for p, raw := range doc.Fields {
		if sensitivePath(p) {
			out = append(out, p)
		}
		out = append(out, findSensitive(p, raw)...)
	}
	for p, raw := range doc.Unknown {
		if sensitivePath(p) {
			out = append(out, p)
		}
		out = append(out, findSensitive(p, raw)...)
	}
	sort.Strings(out)
	return out
}

func sensitivePath(p string) bool {
	parts := strings.FieldsFunc(p, func(r rune) bool { return r == '.' || r == '/' || r == '[' || r == ']' })
	return len(parts) > 0 && sensitiveKey(parts[len(parts)-1])
}

func findSensitive(pathName string, raw json.RawMessage) []string {
	v, err := contracts.DecodeJSON(raw)
	if err != nil {
		return nil
	}
	out := []string{}
	var walk func(any, string)
	walk = func(value any, prefix string) {
		switch x := value.(type) {
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				field := k
				if prefix != "" {
					field = prefix + "." + k
				}
				if sensitiveKey(k) {
					out = append(out, field)
				}
				walk(x[k], field)
			}
		case []any:
			for i, item := range x {
				walk(item, prefix+"["+strconv.Itoa(i)+"]")
			}
		}
	}
	walk(v, pathName)
	return out
}

func sensitiveKey(k string) bool {
	n := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(k))
	switch n {
	case "apikey", "password", "accesstoken", "privatekey", "token", "clientsecret", "refreshtoken", "authtoken", "bearertoken", "apisecret", "secret", "authorization", "secretkey", "credential", "credentials", "clientcredential", "clientcredentials":
		return true
	}
	// Provider prefixes do not make common credential environment keys safe.
	// Reference/limit suffixes are not matched (for example apiKeyRef/maxTokens).
	for _, suffix := range []string{"apikey", "password", "privatekey", "accesstoken", "clientsecret", "refreshtoken", "authtoken", "bearertoken", "apisecret", "secretkey", "secretaccesskey"} {
		if strings.HasSuffix(n, suffix) {
			return true
		}
	}
	if strings.HasSuffix(strings.ToUpper(k), "_TOKEN") {
		return true
	}
	return false
}

func validateFiles(files []File) error {
	if len(files) == 0 {
		return errors.New("at least one explicit input file is required")
	}
	if len(files) > maxBundleFiles {
		return errors.New("input file set exceeds 128 files")
	}
	seen, total := map[string]bool{}, 0
	for _, f := range files {
		if evidence.CheckPath(f.Path) != nil {
			return errors.New("input path must be a canonical relative POSIX path")
		}
		if seen[strings.ToLower(f.Path)] {
			return errors.New("input file paths must be unique")
		}
		seen[strings.ToLower(f.Path)] = true
		if strings.TrimSpace(f.Format) == "" {
			return errors.New("input file format is required")
		}
		if len(f.Data) > nativeparse.MaxDocumentBytes {
			return errors.New("input file exceeds 4 MiB")
		}
		total += len(f.Data)
		if total > maxBundleBytes {
			return errors.New("input file set exceeds 4 MiB")
		}
	}
	for name := range seen {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if seen[parent] {
				return errors.New("input file paths contain a file/directory collision")
			}
		}
	}
	return nil
}

func validNativePath(p string) bool {
	if p == "" || strings.Contains(p, "\\") || strings.HasPrefix(p, "/") || path.Clean(p) != p {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func validDottedPath(p string) bool {
	if p == "" {
		return false
	}
	for _, part := range strings.Split(p, ".") {
		if part == "" {
			return false
		}
		for _, r := range part {
			if !(r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
				return false
			}
		}
	}
	return true
}
func pathsOverlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".") || strings.HasPrefix(a, b+"[") || strings.HasPrefix(b, a+"[")
}
func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}
func mapKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func hashesFor(files []File) []FileHash {
	out := make([]FileHash, 0, len(files))
	for _, f := range files {
		sum := sha256.Sum256(f.Data)
		out = append(out, FileHash{Path: f.Path, Format: f.Format, SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func bindFiles(files []File) []BoundFile {
	out := make([]BoundFile, 0, len(files))
	for _, f := range files {
		sum := sha256.Sum256(f.Data)
		f.Data = append([]byte(nil), f.Data...)
		out = append(out, BoundFile{File: f, SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func boundFiles(b Bundle) []File {
	out := make([]File, 0, len(b.OriginalFiles))
	for _, f := range b.OriginalFiles {
		out = append(out, f.File)
	}
	return out
}
func sameFileSet(files []File, bound []File) bool {
	a, b := hashesFor(files), hashesFor(bound)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func cloneFiles(files []File) []File {
	out := make([]File, len(files))
	for i, f := range files {
		out[i] = f
		out[i].Data = append([]byte(nil), f.Data...)
	}
	return out
}
func cloneDocument(d Document) Document {
	assets := map[string][]byte{}
	for k, v := range d.Assets {
		assets[k] = append([]byte(nil), v...)
	}
	if d.Assets == nil {
		assets = nil
	}
	return Document{Fields: cloneRawMap(d.Fields), Unknown: cloneRawMap(d.Unknown), Assets: assets}
}
func cloneRawMap(in map[string]json.RawMessage) map[string]json.RawMessage {
	if in == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(in))
	for k, v := range in {
		out[k] = append(json.RawMessage(nil), v...)
	}
	return out
}

func compareDocuments(a, b Document) []Difference {
	out := []Difference{}
	compareMap := func(prefix string, x, y map[string]json.RawMessage) {
		keys := map[string]bool{}
		for k := range x {
			keys[k] = true
		}
		for k := range y {
			keys[k] = true
		}
		ordered := make([]string, 0, len(keys))
		for k := range keys {
			ordered = append(ordered, k)
		}
		sort.Strings(ordered)
		for _, k := range ordered {
			xv, xok := x[k]
			yv, yok := y[k]
			status := ""
			if !xok {
				status = "added"
			} else if !yok {
				status = "missing"
			} else if !jsonEquivalent(xv, yv) {
				status = "changed"
			}
			if status != "" {
				p := k
				if prefix != "" {
					p = prefix + k
				}
				out = append(out, Difference{Path: p, Status: status})
			}
		}
	}
	compareMap("", a.Fields, b.Fields)
	compareMap("unknown.", a.Unknown, b.Unknown)
	assetMap := func(values map[string][]byte) map[string]json.RawMessage {
		m := map[string]json.RawMessage{}
		for k, v := range values {
			m[k], _ = json.Marshal(v)
		}
		return m
	}
	compareMap("assets.", assetMap(a.Assets), assetMap(b.Assets))
	return out
}

func documentsEqual(a, b Document) bool { return len(compareDocuments(a, b)) == 0 }

func jsonEquivalent(a, b []byte) bool {
	av, ea := contracts.DecodeJSON(a)
	bv, eb := contracts.DecodeJSON(b)
	if ea != nil || eb != nil {
		return false
	}
	return equalJSON(av, bv)
}
func equalJSON(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		xr, xok := numberRat(string(x))
		yr, yok := numberRat(string(y))
		return xok && yok && xr.Cmp(yr) == 0
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for k, v := range x {
			other, ok := y[k]
			if !ok || !equalJSON(v, other) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i := range x {
			if !equalJSON(x[i], y[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
func numberRat(n string) (*big.Rat, bool) {
	if r, ok := new(big.Rat).SetString(n); ok {
		return r, true
	}
	parts := strings.SplitN(strings.ToLower(n), "e", 2)
	mant := parts[0]
	exp := 0
	if len(parts) == 2 {
		var err error
		exp, err = strconv.Atoi(parts[1])
		if err != nil || exp > 10000 || exp < -10000 {
			return nil, false
		}
	}
	neg := strings.HasPrefix(mant, "-")
	mant = strings.TrimPrefix(strings.TrimPrefix(mant, "-"), "+")
	frac := 0
	if dot := strings.IndexByte(mant, '.'); dot >= 0 {
		frac = len(mant) - dot - 1
		mant = mant[:dot] + mant[dot+1:]
	}
	if mant == "" {
		return nil, false
	}
	coeff, ok := new(big.Int).SetString(mant, 10)
	if !ok {
		return nil, false
	}
	if neg {
		coeff.Neg(coeff)
	}
	scale := exp - frac
	ten := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(abs(scale))), nil)
	if scale >= 0 {
		coeff.Mul(coeff, ten)
		return new(big.Rat).SetInt(coeff), true
	}
	return new(big.Rat).SetFrac(coeff, ten), true
}
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func newReport(op, target, scope string) Report {
	return Report{Operation: op, Target: target, Scope: scope, Status: "invalid", RuntimeEnforcement: "unverified", Diagnostics: []contracts.Diagnostic{}}
}
func add(r *Report, code, source, field, severity, message string) {
	r.Diagnostics = append(r.Diagnostics, contracts.Diagnostic{Code: code, Source: source, Field: field, Severity: severity, Message: message})
}
func hasError(ds []contracts.Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}
func hasWarning(ds []contracts.Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == "warning" {
			return true
		}
	}
	return false
}
func finish(r Report) Report {
	sort.Slice(r.Diagnostics, func(i, j int) bool {
		a, b := r.Diagnostics[i], r.Diagnostics[j]
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		return a.Message < b.Message
	})
	sort.Strings(r.Projection)
	sort.Strings(r.Unknown)
	sort.Slice(r.Differences, func(i, j int) bool {
		if r.Differences[i].Path != r.Differences[j].Path {
			return r.Differences[i].Path < r.Differences[j].Path
		}
		return r.Differences[i].Status < r.Differences[j].Status
	})
	sort.Slice(r.SourceHashes, func(i, j int) bool { return r.SourceHashes[i].Path < r.SourceHashes[j].Path })
	return r
}
