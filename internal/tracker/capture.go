package tracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func Provider(name string) (Adapter, error) {
	switch name {
	case "github":
		return NewGitHub(), nil
	case "azure-devops":
		return NewAzure(), nil
	case "linear":
		return NewLinear(), nil
	case "jira":
		return NewJira(), nil
	case "jira-datacenter":
		return JiraDataCenter{}, nil
	}
	return nil, fmt.Errorf("unsupported provider")
}
func ScopeDigest(s Source) string { return Digest(s) }
func ValidateCheckpoint(c Checkpoint, s Source) error {
	saved := c.Digest
	c.Digest = ""
	if c.Version != s.Version || c.ScopeSHA256 != ScopeDigest(s) || !hashPattern.MatchString(saved) || saved != Digest(c) {
		return fmt.Errorf("checkpoint scope or digest mismatch")
	}
	for _, r := range c.Pending {
		r.baseScopeValidated = true // structural checkpoint validation; execution separately reconstructs the captured plan
		if e := ValidateRequest(s, r); e != nil {
			return e
		}
	}
	if c.Complete && len(c.Pending) > 0 {
		return fmt.Errorf("complete checkpoint contains pending requests")
	}
	return nil
}
func finishCheckpoint(c Checkpoint) Checkpoint { c.Digest = ""; c.Digest = Digest(c); return c }
func Import(path string) (*Capture, error) {
	b, e := ReadFile(path)
	if e != nil {
		return nil, e
	}
	var m Manifest
	if e = Decode(b, "manifest", &m); e != nil {
		return nil, e
	}
	root := filepath.Dir(path)
	limits, limitErr := EffectiveLimits(m.Source)
	if limitErr != nil {
		return nil, limitErr
	}
	totalRead := 0
	files := map[string][]byte{}
	for _, p := range m.Pages {
		b, e := ReadAsset(root, p.Asset.Path)
		if e != nil {
			return nil, e
		}
		totalRead += len(b)
		if len(b) > limits.ResponseBytes || totalRead > limits.TotalBytes {
			return nil, fmt.Errorf("capture byte limit exceeded while importing")
		}
		if Hash(b) != p.Asset.SHA256 {
			return nil, fmt.Errorf("captured asset hash changed")
		}
		if prior, ok := files[p.Asset.Path]; ok && !bytes.Equal(prior, b) {
			return nil, fmt.Errorf("asset collision")
		}
		files[p.Asset.Path] = b
	}
	return build(m, files)
}
func Load(root string) (*Capture, error) {
	c, e := Import(filepath.Join(root, "manifest.json"))
	if e != nil {
		return nil, e
	}
	b, e := ReadAsset(root, "snapshot.json")
	if e != nil {
		return nil, e
	}
	if e = ValidateJSON(b, "snapshot"); e != nil {
		return nil, e
	}
	expected, _ := json.Marshal(c.Snapshot)
	var actual Snapshot
	if e = usage.Decode(b, &actual); e != nil {
		return nil, e
	}
	encoded, _ := json.Marshal(actual)
	if !bytes.Equal(expected, encoded) {
		return nil, fmt.Errorf("snapshot differs from captured sources")
	}
	return c, nil
}
func CaptureSource(ctx context.Context, s Source, mode string, cp *Checkpoint, t Transport) (*Capture, error) {
	if cp != nil {
		if e := ValidateCheckpoint(*cp, s); e != nil {
			return nil, e
		}
		return nil, fmt.Errorf("resume requires a validated checkpoint bundle")
	}
	return captureSource(ctx, s, mode, nil, nil, t)
}
func CaptureWithCheckpoint(ctx context.Context, s Source, mode, checkpointPath string, t Transport) (*Capture, error) {
	if filepath.Base(checkpointPath) != "checkpoint.json" {
		return nil, fmt.Errorf("checkpoint must belong to a captured bundle")
	}
	b, e := ReadFile(checkpointPath)
	if e != nil {
		return nil, e
	}
	var cp Checkpoint
	if e = Decode(b, "checkpoint", &cp); e != nil {
		return nil, e
	}
	if e = ValidateCheckpoint(cp, s); e != nil {
		return nil, e
	}
	previous, e := Load(filepath.Dir(checkpointPath))
	if e != nil {
		return nil, e
	}
	if previous.Snapshot.Checkpoint.Digest != cp.Digest || previous.Snapshot.Mode != mode {
		return nil, fmt.Errorf("checkpoint differs from captured request plan")
	}
	cp = previous.Snapshot.Checkpoint // retain only rederived private scope proof
	if cp.Complete {
		return captureSource(ctx, s, mode, nil, nil, t)
	}
	return captureSource(ctx, s, mode, &cp, previous, t)
}
func captureSource(ctx context.Context, s Source, mode string, cp *Checkpoint, previous *Capture, t Transport) (*Capture, error) {
	if e := ValidateSource(s); e != nil {
		return nil, e
	}
	if t == nil {
		return nil, fmt.Errorf("read transport required")
	}
	if cp != nil {
		if e := ValidateCheckpoint(*cp, s); e != nil {
			return nil, e
		}
	}
	adapter, e := Provider(s.Provider)
	if e != nil {
		return nil, e
	}
	limits, _ := EffectiveLimits(s)
	ctx, cancel := context.WithTimeout(ctx, time.Duration(limits.CommandSeconds)*time.Second)
	defer cancel()
	queue, e := adapter.Initial(s, mode, cp)
	if e != nil {
		return nil, e
	}
	m := Manifest{Version: s.Version, Source: s, Mode: mode, Pages: []Page{}, Checkpoint: cp}
	files := map[string][]byte{}
	if previous != nil {
		if !ValidCapture(previous) || ScopeDigest(previous.Snapshot.Source) != ScopeDigest(s) {
			return nil, fmt.Errorf("prior capture scope mismatch")
		}
		var original Manifest
		if e = Decode(previous.Files["manifest.json"], "manifest", &original); e != nil {
			return nil, e
		}
		m = original
		// Failed attempts remain inspectable in the old immutable bundle. They must
		// not be replayed as unresolved failures after their successful replacement.
		m.Pages = []Page{}
		for _, p := range original.Pages {
			retry := false
			for _, request := range previous.Snapshot.Checkpoint.Pending {
				if requestKey(request) == requestKey(p.Request) {
					retry = true
				}
			}
			if p.Status >= 200 && p.Status < 300 && !retry {
				m.Pages = append(m.Pages, p)
			}
		}
		for _, p := range m.Pages {
			files[p.Asset.Path] = append([]byte(nil), previous.Files[p.Asset.Path]...)
		}
	}
	seen := map[string]bool{}
	total, items := 0, 0
	for _, b := range files {
		total += len(b)
	}
	initialPages := len(m.Pages)
	for len(queue) > 0 && len(m.Pages)-initialPages < limits.Requests && items < limits.Items {
		r := queue[0]
		queue = queue[1:]
		if e := ValidateRequest(s, r); e != nil {
			return nil, e
		}
		key := requestKey(r)
		if seen[key] {
			break
		}
		seen[key] = true
		response, e := t.Do(ctx, s, r)
		if e != nil {
			// A synthetic non-success page preserves an explicit interruption, not a
			// made-up provider response or an unsafe exception string.
			status := 599
			var te *TransportError
			if errors.As(e, &te) && te.Disposition == "authentication" {
				status = 401
			}
			if errors.Is(e, context.Canceled) {
				status = 499
			}
			if errors.Is(e, context.DeadlineExceeded) {
				status = 504
			}
			response = Response{Status: status, Headers: map[string]string{}}
			queue = append([]Request{r}, queue...)
		}
		headers := sanitizeHeaders(response.Headers)
		raw := response.Body
		if response.Status < 200 || response.Status >= 300 {
			raw = []byte(`{}`)
		}
		if len(raw) > limits.ResponseBytes || total+len(raw) > limits.TotalBytes {
			queue = append([]Request{r}, queue...)
			break
		}
		total += len(raw)
		name := fmt.Sprintf("assets/%04d.json", len(m.Pages))
		for files[name] != nil {
			name = "assets/" + Hash([]byte(name)) + ".json"
		}
		files[name] = append([]byte(nil), raw...)
		page := Page{Request: r, Status: response.Status, Headers: headers, Asset: contracts.AssetRef{Path: name, SHA256: Hash(raw)}, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		m.Pages = append(m.Pages, page)
		if response.Status < 200 || response.Status >= 300 {
			break
		}
		if e := contracts.ValidateJSON(raw); e != nil {
			break
		}
		result, e := adapter.Decode(s, r, response)
		if e != nil {
			return nil, e
		}
		items += len(result.Items)
		queue = append(queue, result.Next...)
	}
	// Build recomputes completeness and pending work from original pages. A
	// hard budget stop cannot silently mark the capture complete.
	return build(m, files)
}
func build(m Manifest, assets map[string][]byte) (*Capture, error) {
	if m.Version != m.Source.Version || m.Mode != "capture" && m.Mode != "discover" {
		return nil, fmt.Errorf("invalid tracker manifest")
	}
	if e := ValidateSource(m.Source); e != nil {
		return nil, e
	}
	if m.Checkpoint != nil {
		return nil, fmt.Errorf("import requires original root observations, not executable checkpoint requests")
	}
	adapter, e := Provider(m.Source.Provider)
	if e != nil {
		return nil, e
	}
	queue, e := adapter.Initial(m.Source, m.Mode, m.Checkpoint)
	if e != nil {
		return nil, e
	}
	limits, _ := EffectiveLimits(m.Source)
	if len(m.Pages) > 10000 {
		return nil, fmt.Errorf("capture request limit exceeded")
	}
	s := Snapshot{Version: m.Version, Source: m.Source, Mode: m.Mode, Pages: m.Pages, Items: []Item{}, Discovery: map[string]json.RawMessage{}, Complete: true, Report: BaseReport()}
	s.Report.Version = m.Version
	items := map[string]Item{}
	seen := map[string]string{}
	total := 0
	pending := map[string]Request{}
	planned := map[string]Request{}
	for _, r := range queue {
		pending[requestKey(r)] = r
		planned[requestKey(r)] = r
	}
	diagnose := func(code, field, message string) {
		s.Report.Diagnostics = append(s.Report.Diagnostics, Diagnostic{code, field, message})
		s.Complete = false
	}
	oldest := time.Time{}
	for _, p := range m.Pages {
		p.Request.baseScopeValidated = planned[requestKey(p.Request)].baseScopeValidated
		if e := ValidateRequest(m.Source, p.Request); e != nil {
			return nil, e
		}
		at, e := time.Parse(time.RFC3339Nano, p.ObservedAt)
		if e != nil {
			return nil, fmt.Errorf("original observation date required")
		}
		if oldest.IsZero() || at.Before(oldest) {
			oldest = at
		}
		raw, ok := assets[p.Asset.Path]
		if !ok || !hashPattern.MatchString(p.Asset.SHA256) || Hash(raw) != p.Asset.SHA256 {
			return nil, fmt.Errorf("asset integrity failure")
		}
		total += len(raw)
		if len(raw) > limits.ResponseBytes || total > limits.TotalBytes {
			return nil, fmt.Errorf("capture byte limit exceeded")
		}
		key := requestKey(p.Request)
		if prior, duplicate := seen[key]; duplicate {
			if prior != p.Asset.SHA256 {
				diagnose("conflicting-page", "pages", "duplicate request has conflicting captured bytes")
			}
			continue
		}
		seen[key] = p.Asset.SHA256
		if _, ok := pending[key]; !ok {
			return nil, fmt.Errorf("captured page is not in the scoped request plan")
		}
		delete(pending, key)
		if !safeHeaders(p.Headers) {
			return nil, fmt.Errorf("unapproved response header in capture")
		}
		if p.Status < 200 || p.Status >= 300 {
			disposition := statusDisposition(p.Status)
			diagnose(disposition, "response.status", "provider read did not complete")
			pending[key] = p.Request
			continue
		}
		if e := contracts.ValidateJSON(raw); e != nil {
			return nil, fmt.Errorf("malformed provider JSON: %w", e)
		}
		result, e := adapter.Decode(m.Source, p.Request, Response{Status: p.Status, Headers: p.Headers, Body: raw})
		if e != nil {
			return nil, e
		}
		if !result.Complete {
			s.Complete = false
			pending[key] = p.Request
		}
		for _, d := range result.Diagnostics {
			if strings.Contains(d.Code, "scope") || strings.Contains(d.Code, "tenant") {
				return nil, fmt.Errorf("provider response escapes configured immutable identity")
			}
		}
		s.Report.Diagnostics = append(s.Report.Diagnostics, result.Diagnostics...)
		for _, r := range result.Next {
			if e := ValidateRequest(m.Source, r); e != nil {
				return nil, e
			}
			rk := requestKey(r)
			planned[rk] = r
			if _, already := seen[rk]; already {
				diagnose("pagination-loop", "pages", "provider continuation repeated an already captured request")
			} else {
				pending[rk] = r
			}
		}
		for k, v := range result.Discovery {
			s.Discovery[k+":"+p.Asset.SHA256] = v
		}
		for _, item := range result.Items {
			if item.Identity.Provider != m.Source.Provider || item.Identity.TenantID != TenantScopeID(m.Source) || item.Identity.ContainerID != m.Source.ContainerID || item.Identity.RemoteID == "" {
				return nil, fmt.Errorf("normalized identity escapes source scope")
			}
			if item.Provenance == nil {
				item.Provenance = map[string][]Pointer{}
			}
			for f := range item.Fields {
				pointer := fieldPointer(m.Source, p.Request, item, f, raw)
				if pointer != "" {
					item.Provenance[f] = append(item.Provenance[f], Pointer{p.Asset.SHA256, pointer})
				} else {
					s.Report.Diagnostics = append(s.Report.Diagnostics, Diagnostic{"provenance-unresolved", f, "native field could not be uniquely anchored to this immutable item; mapped use will block"})
				}
			}
			id := item.Identity.RemoteID
			old, exists := items[id]
			if !exists {
				if len(items) >= limits.Items {
					diagnose("item-limit", "items", "normalized item limit reached; original source bytes retained")
					continue
				}
				items[id] = item
			} else {
				merged, conflict := mergeItem(old, item)
				items[id] = merged
				if conflict {
					diagnose("item-conflict", id, "incomparable revisions or fields disagree; no arrival-order winner selected")
				}
			}
		}

	}
	if len(m.Pages) == 0 {
		diagnose("empty-capture", "pages", "no source observations captured")
	}
	for _, item := range items {
		if !item.Complete {
			s.Complete = false
		}
		s.Items = append(s.Items, item)
	}
	sort.Slice(s.Items, func(i, j int) bool { return s.Items[i].Identity.RemoteID < s.Items[j].Identity.RemoteID })
	if e := joinGitHubProjects(&s, assets); e != nil {
		return nil, e
	}
	remaining := []Request{}
	for _, r := range pending {
		remaining = append(remaining, r)
	}
	sort.Slice(remaining, func(i, j int) bool { return Digest(remaining[i]) < Digest(remaining[j]) })
	if len(remaining) > 0 {
		diagnose("incomplete", "checkpoint", "capture has pending reads; completed watermark not advanced")
	}
	watermark := ""
	if m.Checkpoint != nil {
		watermark = m.Checkpoint.CompletedWatermark
	}
	if !oldest.IsZero() {
		s.ObservedAt = oldest.UTC().Format(time.RFC3339Nano)
	}
	if s.Complete {
		watermark = s.ObservedAt
	}
	s.Checkpoint = finishCheckpoint(Checkpoint{Version: m.Version, ScopeSHA256: ScopeDigest(m.Source), Pending: remaining, CompletedWatermark: watermark, Complete: s.Complete})
	if !s.Complete {
		s.Report.Status = "blocked"
	}
	stableDiagnostics(&s.Report)
	s.Digest = Digest(s)
	files := map[string][]byte{}
	for path, b := range assets {
		if strings.HasPrefix(path, "assets/") {
			files[path] = append([]byte(nil), b...)
		} else {
			return nil, fmt.Errorf("captured assets must reside under assets/")
		}
	}
	encode := func(name string, v any) error {
		b, e := json.MarshalIndent(v, "", "  ")
		if e == nil && len(b)+1 > contracts.MaxDocumentBytes {
			return fmt.Errorf("normalized control document exceeds 4 MiB; narrow the capture scope")
		}
		if e == nil {
			files[name] = append(b, '\n')
		}
		return e
	}
	if e = encode("manifest.json", m); e != nil {
		return nil, e
	}
	if e = encode("snapshot.json", s); e != nil {
		return nil, e
	}
	if e = encode("checkpoint.json", s.Checkpoint); e != nil {
		return nil, e
	}
	c := &Capture{Snapshot: s, Files: files}
	c.seal = captureSeal(c)
	return c, nil
}
func captureSeal(c *Capture) string {
	return Digest(struct {
		Snapshot Snapshot
		Files    map[string][]byte
	}{c.Snapshot, c.Files})
}
func ValidCapture(c *Capture) bool { return c != nil && c.seal != "" && c.seal == captureSeal(c) }
func statusDisposition(status int) string {
	switch status {
	case 401:
		return "authentication"
	case 403:
		return "permission"
	case 404:
		return "inaccessible-not-found"
	case 409, 412:
		return "conflict"
	case 429:
		return "rate-limit"
	case 499:
		return "cancelled"
	case 504:
		return "timeout"
	default:
		return "incomplete"
	}
}
func sanitizeHeaders(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		k = strings.ToLower(k)
		switch k {
		case "etag", "last-modified", "date", "retry-after", "link", "x-ratelimit-remaining", "x-ratelimit-reset", "x-ms-continuationtoken":
			out[k] = v
		}
	}
	return out
}
func safeHeaders(in map[string]string) bool { return len(sanitizeHeaders(in)) == len(in) }
