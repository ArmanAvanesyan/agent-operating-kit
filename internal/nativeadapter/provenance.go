package nativeadapter

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
)

var ErrBundleTooLarge = errors.New("native bundle exceeds 4 MiB")

// EncodeBundle serializes an explicit sidecar bundle within the canonical
// operational document bound. Callers decide where the returned bytes go.
func EncodeBundle(bundle Bundle) ([]byte, error) {
	if err := validateBundleShape(bundle); err != nil {
		return nil, err
	}
	expected, err := bundleDigest(bundle)
	if err != nil || expected != bundle.Digest {
		return nil, errors.New("native bundle digest mismatch")
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return nil, errors.New("native bundle cannot be encoded")
	}
	if len(data) > contracts.MaxDocumentBytes {
		return nil, ErrBundleTooLarge
	}
	return data, nil
}

// DecodeBundle strictly reads a versioned sidecar. Unknown top-level or nested
// struct fields, duplicate JSON keys, trailing values and oversized input fail.
func DecodeBundle(data []byte) (Bundle, error) {
	if len(data) > contracts.MaxDocumentBytes {
		return Bundle{}, ErrBundleTooLarge
	}
	if _, err := contracts.DecodeJSON(data); err != nil {
		return Bundle{}, errors.New("native bundle is not strict bounded JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var bundle Bundle
	if err := dec.Decode(&bundle); err != nil {
		return Bundle{}, errors.New("native bundle has an unsupported shape")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return Bundle{}, errors.New("native bundle has trailing data")
	}
	if err := validateBundleShape(bundle); err != nil {
		return Bundle{}, err
	}
	expected, err := bundleDigest(bundle)
	if err != nil || expected != bundle.Digest {
		return Bundle{}, errors.New("native bundle digest mismatch")
	}
	return cloneBundle(bundle), nil
}

func validateBundle(bundle Bundle, target, scope string, descriptor Descriptor) error {
	if err := validateBundleShape(bundle); err != nil {
		return err
	}
	if err := validateDocument(bundle.Document, descriptor); err != nil {
		return errors.New("native bundle projection conflicts with adapter descriptor")
	}
	if bundle.Target != target || bundle.Scope != scope || bundle.AdapterVersion != descriptor.AdapterVersion {
		return errors.New("native bundle target, scope, or adapter version mismatch")
	}
	expected, err := bundleDigest(bundle)
	if err != nil || expected != bundle.Digest {
		return errors.New("native bundle digest mismatch")
	}
	return nil
}

func validateBundleShape(bundle Bundle) error {
	if err := validateCanonical(bundle.Canonical); err != nil {
		return err
	}
	if err := validateAssets(bundle.Document.Assets); err != nil {
		return err
	}
	if bundle.Version != BundleVersion || bundle.Target == "" || bundle.Scope == "" || bundle.AdapterVersion == "" || bundle.Completeness != "draft-incomplete" {
		return errors.New("native bundle header is invalid or unsupported")
	}
	if len(bundle.OriginalFiles) == 0 {
		return errors.New("native bundle has no original source files")
	}
	if err := validateBoundFileSet(bundle.OriginalFiles); err != nil {
		return err
	}
	if len(bundle.RenderedFiles) != 0 {
		if err := validateBoundFileSet(bundle.RenderedFiles); err != nil {
			return err
		}
	}
	if err := validateRawMap(bundle.Document.Fields); err != nil {
		return err
	}
	if err := validateRawMap(bundle.Document.Unknown); err != nil {
		return err
	}
	if sensitive := sensitiveFields(bundle.Document); len(sensitive) != 0 {
		return errors.New("native bundle contains a credential-bearing field")
	}
	if bundle.Digest == "" {
		return errors.New("native bundle digest is required")
	}
	if err := checkBundleSize(bundle); err != nil {
		return err
	}
	return nil
}

func validateBoundFileSet(files []BoundFile) error {
	plain := make([]File, len(files))
	for i, file := range files {
		plain[i] = file.File
		sum := sha256.Sum256(file.Data)
		if file.SHA256 != hex.EncodeToString(sum[:]) {
			return errors.New("native bundle file hash mismatch")
		}
	}
	return validateFiles(plain)
}

func validateRawMap(values map[string]json.RawMessage) error {
	for key, value := range values {
		if key == "" {
			return errors.New("native bundle contains an empty field path")
		}
		if _, err := contracts.DecodeJSON(value); err != nil {
			return errors.New("native bundle contains invalid JSON field data")
		}
	}
	return nil
}

func bundleDigest(bundle Bundle) (string, error) {
	bundle.Digest = ""
	data, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func checkBundleSize(bundle Bundle) error {
	data, err := json.Marshal(bundle)
	if err != nil {
		return errors.New("native bundle cannot be encoded")
	}
	if len(data) > contracts.MaxDocumentBytes {
		return ErrBundleTooLarge
	}
	return nil
}

func currentBoundFiles(bundle Bundle) []File {
	if len(bundle.RenderedFiles) != 0 {
		return plainBoundFiles(bundle.RenderedFiles)
	}
	return plainBoundFiles(bundle.OriginalFiles)
}

func plainBoundFiles(files []BoundFile) []File {
	out := make([]File, len(files))
	for i, file := range files {
		out[i] = file.File
		out[i].Data = append([]byte(nil), file.Data...)
	}
	return out
}

func cloneBoundFiles(files []BoundFile) []BoundFile {
	out := make([]BoundFile, len(files))
	for i, file := range files {
		out[i] = file
		out[i].Data = append([]byte(nil), file.Data...)
	}
	return out
}

func cloneBundle(bundle Bundle) Bundle {
	out := bundle
	out.Canonical = cloneCanonical(bundle.Canonical)
	out.OriginalFiles = cloneBoundFiles(bundle.OriginalFiles)
	out.RenderedFiles = cloneBoundFiles(bundle.RenderedFiles)
	out.Document = cloneDocument(bundle.Document)
	return out
}

func sanitizeAdapterDiagnostics(in []contracts.Diagnostic, target string) []contracts.Diagnostic {
	out := make([]contracts.Diagnostic, 0, len(in))
	for _, diagnostic := range in {
		severity := diagnostic.Severity
		if severity != "error" && severity != "warning" && severity != "info" {
			severity = "error"
		}
		// Adapter parsers may include source excerpts or native scalar contents in
		// their messages. Keep only the severity and replace all free text.
		out = append(out, contracts.Diagnostic{Code: "native.adapter.diagnostic", Source: target, Severity: severity, Message: "adapter reported a native input condition"})
	}
	return out
}

func validateAssets(assets map[string][]byte) error {
	if len(assets) == 0 {
		return nil
	}
	files := []File{}
	for path, data := range assets {
		files = append(files, File{Path: path, Format: "text", Data: data})
	}
	return validateFiles(files)
}

func cloneCanonical(c *CanonicalProvenance) *CanonicalProvenance {
	if c == nil {
		return nil
	}
	out := *c
	out.Sources = append([]CanonicalSource(nil), c.Sources...)
	return &out
}

var digestPattern = regexp.MustCompile("^[a-f0-9]{64}$")
var versionPattern = regexp.MustCompile("^[0-9]+\\.[0-9]+\\.[0-9]+$")
var idPattern = regexp.MustCompile("^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$")

func validateCanonical(c *CanonicalProvenance) error {
	if c == nil {
		return nil
	}
	if !digestPattern.MatchString(c.SnapshotDigest) || len(c.Sources) == 0 || len(c.Sources) > 512 {
		return errors.New("invalid canonical provenance")
	}
	for kind, ref := range map[string]contracts.Reference{"Agent": c.Agent, "ExecutionProfile": c.Profile, "AgentAssignment": c.Assignment} {
		if ref.Kind != kind || !idPattern.MatchString(ref.ID) || !versionPattern.MatchString(ref.Version) {
			return errors.New("invalid canonical selection pin")
		}
	}
	seen := map[string]bool{}
	for _, source := range c.Sources {
		if evidence.CheckPath(source.Path) != nil || seen[source.Path] || !digestPattern.MatchString(source.SHA256) {
			return errors.New("invalid canonical source hash")
		}
		seen[source.Path] = true
	}
	return nil
}
