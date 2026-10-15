package nativeadapter

// Render creates a reviewable native candidate from a canonical projection.
// Callers must validate their canonical corpus first. Provenance is declarative
// hash evidence; this operation never upgrades runtime readiness or authority.
func (e *Engine) Render(target, scope string, doc Document, controls []Control, provenance *CanonicalProvenance) ([]File, Bundle, Report) {
	r := newReport("export", target, scope)
	a, d, ok := e.adapter(target, scope, &r)
	if !ok {
		return nil, Bundle{}, finish(r)
	}
	r.HostLoading = d.HostLoading
	if err := validateDocument(doc, d); err != nil {
		add(&r, "native.document", target, "", "error", "canonical projection conflicts with adapter fields")
		return nil, Bundle{}, finish(r)
	}
	if len(sensitiveFields(doc)) > 0 {
		add(&r, "native.credential-field", target, "", "error", "credential-bearing content cannot be rendered")
		return nil, Bundle{}, finish(r)
	}
	files, ds, err := a.Encode(scope, cloneDocument(doc))
	r.Diagnostics = append(r.Diagnostics, sanitizeAdapterDiagnostics(ds, target)...)
	if err != nil || hasError(r.Diagnostics) {
		add(&r, "native.encode", target, "", "error", "adapter could not render the canonical projection")
		return nil, Bundle{}, finish(r)
	}
	b, imported := e.Import(target, scope, files, nil, controls)
	imported.Operation = "export"
	if b.Version == "" {
		return nil, Bundle{}, imported
	}
	if !documentsEqual(doc, b.Document) {
		add(&imported, "native.export.roundtrip", target, "", "error", "native encoding changed the canonical projection")
		imported.Status = "invalid"
		return nil, Bundle{}, finish(imported)
	}
	b.Canonical = cloneCanonical(provenance)
	b.Digest, err = bundleDigest(b)
	if err != nil {
		return nil, Bundle{}, r
	}
	if _, err := EncodeBundle(b); err != nil {
		add(&imported, "native.bundle.integrity", target, "", "error", "canonical provenance is invalid or exceeds limits")
		imported.Status = "invalid"
		return nil, Bundle{}, finish(imported)
	}
	imported.Diagnostics = append(imported.Diagnostics, r.Diagnostics...)
	return cloneFiles(files), b, finish(imported)
}

// Describe returns the explicitly registered adapter contract, without discovery.
func (e *Engine) Describe(target string) (Descriptor, bool) {
	if e == nil || e.registry == nil {
		return Descriptor{}, false
	}
	a, ok := e.registry.Lookup(target)
	if !ok {
		return Descriptor{}, false
	}
	return a.Descriptor(), true
}
