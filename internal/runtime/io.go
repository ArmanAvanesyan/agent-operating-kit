// Package runtime implements bounded local workflow admission. It does not
// accept completed work or apply tracker updates.
package runtime

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const Version = records.Version

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,95}$`)

func Encode(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }
func Decode(b []byte, kind string, v any) error {
	if err := records.ValidateJSON(b, kind); err != nil {
		return err
	}
	return usage.Decode(b, v)
}
func ReadFile(path string) ([]byte, error) { return hostbridge.ReadFile(path) }
func readAsset(root string, a records.Asset) ([]byte, error) {
	var b []byte
	var err error
	if filepath.IsAbs(a.Path) {
		b, err = ReadFile(a.Path)
	} else {
		b, err = hostbridge.ReadAsset(root, a.Path)
	}
	if err != nil || usage.Hash(b) != a.SHA256 {
		return nil, fmt.Errorf("hash-bound asset unavailable or changed: %s", a.Path)
	}
	return b, nil
}
func ReadPolicy(path string) (records.Policy, string, error) {
	var p records.Policy
	b, err := ReadFile(path)
	if err != nil {
		return p, "", err
	}
	if err = Decode(b, "policy", &p); err != nil {
		return p, "", err
	}
	root := filepath.Dir(path)
	normalize := func(a *records.Asset) error {
		if _, err := readAsset(root, *a); err != nil {
			return err
		}
		if !filepath.IsAbs(a.Path) {
			a.Path = filepath.Join(root, filepath.FromSlash(a.Path))
		}
		return nil
	}
	if err = normalize(&p.UsagePolicy); err != nil {
		return p, "", err
	}
	for i := range p.Hosts {
		if err = normalize(&p.Hosts[i].Policy); err != nil {
			return p, "", err
		}
		if p.Hosts[i].Qualification != nil {
			if err = normalize(p.Hosts[i].Qualification); err != nil {
				return p, "", err
			}
		}
	}
	if err = ValidatePolicy(p); err != nil {
		return p, "", err
	}
	return p, usage.Hash(b), nil
}
func ValidatePolicy(p records.Policy) error {
	if (p.Version != Version && p.Version != records.VersionV2) || !identifier.MatchString(p.Authority) || p.MaxConcurrent < 1 || p.MaxConcurrent > 16 || p.LeaseSeconds < 5 || p.LeaseSeconds > 30 || p.HeartbeatSeconds < 1 || p.HeartbeatSeconds*2 >= p.LeaseSeconds || p.MaxAttempts < 1 || p.MaxAttempts > 10 {
		return fmt.Errorf("invalid runtime policy or bounds")
	}
	if !filepath.IsAbs(p.StagingRoot) || !filepath.IsAbs(p.AccountingPath) || len(p.RepositoryRoots) == 0 {
		return fmt.Errorf("absolute runtime roots required")
	}
	for _, root := range p.RepositoryRoots {
		if !filepath.IsAbs(root) {
			return fmt.Errorf("absolute repository root required")
		}
	}
	seen := map[string]bool{}
	for _, h := range p.Hosts {
		attempt := h.Attempt
		if attempt == 0 {
			attempt = 1
		}
		if attempt < 1 || attempt > p.MaxAttempts || h.StepID != "" && !identifier.MatchString(h.StepID) {
			return fmt.Errorf("invalid host attempt/step scope")
		}
		legacyHost := h.Host == "codex" || h.Host == "claude-code"
		v2Host := p.Version == records.VersionV2 && (h.Host == "opencode" || h.Host == "cursor" || h.Host == "gemini" || h.Host == "openclaw")
		if !(legacyHost || v2Host) || seen[fmt.Sprintf("%s/%s/%d", h.Host, h.StepID, attempt)] {
			return fmt.Errorf("unsupported or duplicate host route")
		}
		seen[fmt.Sprintf("%s/%s/%d", h.Host, h.StepID, attempt)] = true
	}
	return nil
}
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
func permittedRepository(p records.Policy, repo string) error {
	real, err := filepath.EvalSymlinks(repo)
	if err != nil || real != repo || !filepath.IsAbs(repo) {
		return fmt.Errorf("canonical repository required")
	}
	for _, root := range p.RepositoryRoots {
		canonical, err := filepath.EvalSymlinks(root)
		if err == nil && canonical == root && within(root, repo) {
			if within(repo, p.StagingRoot) || within(p.StagingRoot, repo) {
				return fmt.Errorf("staging overlaps repository")
			}
			return nil
		}
	}
	return fmt.Errorf("repository outside operator scope")
}
func CaptureManifest(path string, p records.Policy) (records.Manifest, map[string][]byte, string, error) {
	var m records.Manifest
	raw, err := ReadFile(path)
	if err != nil {
		return m, nil, "", err
	}
	if err = Decode(raw, "manifest", &m); err != nil {
		return m, nil, "", err
	}
	if !identifier.MatchString(m.ID) || len(m.Steps) == 0 || m.InputRevision == "" || len(m.RepositoryAssets) == 0 {
		return m, nil, "", fmt.Errorf("run identity, exact repository inventory and steps required")
	}
	if err = permittedRepository(p, m.Repository); err != nil {
		return m, nil, "", err
	}
	files := map[string][]byte{"manifest.json": raw}
	seen := map[string]bool{}
	total := 0
	for _, a := range m.Assets {
		if err = evidence.CheckPath(a.Path); err != nil {
			return m, nil, "", err
		}
		key := strings.ToLower(a.Path)
		if seen[key] {
			return m, nil, "", fmt.Errorf("duplicate captured path")
		}
		seen[key] = true
		b, err := readAsset(filepath.Dir(path), a)
		if err != nil {
			return m, nil, "", err
		}
		total += len(b)
		if total > hostbridge.MaxCaptureBytes {
			return m, nil, "", fmt.Errorf("capture exceeds bound")
		}
		files["inputs/"+a.Path] = b
	}
	for _, path := range []string{m.Corpus, m.Plan, m.Evidence} {
		if path == "" || evidence.CheckPath(path) != nil {
			return m, nil, "", fmt.Errorf("invalid plan/corpus path")
		}
	}
	ids := map[string]bool{}
	for _, s := range m.Steps {
		if !identifier.MatchString(s.ID) || ids[s.ID] {
			return m, nil, "", fmt.Errorf("invalid or duplicate step")
		}
		ids[s.ID] = true
		for _, path := range []string{s.Assignment, s.Selection.Task, s.Selection.Candidates, s.Selection.CatalogPolicy, s.Selection.Policy, s.Selection.EvidencePolicy} {
			if evidence.CheckPath(path) != nil || !seen[strings.ToLower(path)] {
				return m, nil, "", fmt.Errorf("uncaptured required input: %s", path)
			}
		}
		if evidence.CheckPath(s.Selection.Catalog) != nil {
			return m, nil, "", fmt.Errorf("unsafe catalog path")
		}
	}
	for _, a := range m.RepositoryAssets {
		if evidence.CheckPath(a.Path) != nil || len(a.SHA256) != 64 {
			return m, nil, "", fmt.Errorf("invalid repository inventory")
		}
	}
	return m, files, usage.Hash(raw), nil
}
func VerifyCapture(root string, m records.Manifest, sha string) error {
	raw, err := hostbridge.ReadAsset(root, "manifest.json")
	if err != nil || usage.Hash(raw) != sha {
		return fmt.Errorf("manifest capture changed")
	}
	for _, a := range m.Assets {
		if _, err = readAsset(filepath.Join(root, "inputs"), a); err != nil {
			return err
		}
	}
	return nil
}
func NewReport() records.Report {
	return records.Report{Version: Version, Status: "valid", Reasons: []string{}, CoreGate: "offline-core", CodexLiveGate: "blocked-p6-control-proofs-and-live-conformance", ClaudeLiveGate: "blocked-p6-control-proofs-and-live-conformance", RuntimeGate: "connected-qualification-pending", Accepted: false}
}
func writeNew(root string, files map[string][]byte, protected ...string) error {
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	return evidence.WriteFiles(root, files, protected...)
}
