package replay

import (
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/selection"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"path/filepath"
	"sort"
	"time"
)

// FrozenPolicy binds each comparison name to exact policy bytes before audit.
type FrozenPolicy struct {
	Name   string `json:"name"`
	Asset  string `json:"asset"`
	SHA256 string `json:"sha256"`
}
type SelectionCase struct {
	ID                 string `json:"id"`
	Partition          string `json:"partition"`
	Repository         string `json:"repository"`
	Family             string `json:"family"`
	NearDuplicateGroup string `json:"nearDuplicateGroup"`
	Task               string `json:"task"`
	Candidates         string `json:"candidates"`
	Catalog            string `json:"catalog"`
	CatalogPolicy      string `json:"catalogPolicy"`
	Usage              string `json:"usage"`
	UsagePolicy        string `json:"usagePolicy"`
	EvidencePolicy     string `json:"evidencePolicy"`
	AsOf               string `json:"asOf"`
}
type SelectionReplayManifest struct {
	Version  string          `json:"version"`
	FrozenAt string          `json:"frozenAt"`
	AuditAt  string          `json:"auditAt"`
	Policies []FrozenPolicy  `json:"policies"`
	Cases    []SelectionCase `json:"cases"`
}
type ReplayedSelection struct {
	CaseID   string                      `json:"caseId"`
	Policy   string                      `json:"policy"`
	Decision selection.SelectionDecision `json:"decision"`
}
type SelectionReplayReport struct {
	PartitionMetadataVerified bool                      `json:"partitionMetadataVerified"`
	PolicySemanticsVerified   bool                      `json:"policySemanticsVerified"`
	Version                   string                    `json:"version"`
	Mode                      string                    `json:"mode"`
	ManifestSHA256            string                    `json:"manifestSha256"`
	FrozenAt                  string                    `json:"frozenAt"`
	AuditAt                   string                    `json:"auditAt"`
	PolicyDigests             map[string]string         `json:"policyDigests"`
	Decisions                 []ReplayedSelection       `json:"decisions"`
	Dispositions              map[string]map[string]int `json:"dispositions"`
	QualityEvidenceQualified  bool                      `json:"qualityEvidenceQualified"`
	AuthorizationGranted      bool                      `json:"authorizationGranted"`
	ExecutionReady            bool                      `json:"executionReady"`
	Claim                     string                    `json:"claim"`
}

// ReplaySelections runs the actual selector for five frozen policies under each
// exact supplied state. It neither fabricates task outcomes nor simulates fit.
func ReplaySelections(path, partition string) (SelectionReplayReport, error) {
	out := SelectionReplayReport{Version: Version, Mode: "recorded-state-selector-replay", PolicyDigests: map[string]string{}, Decisions: []ReplayedSelection{}, Dispositions: map[string]map[string]int{}, Claim: "conditional decisions under identical supplied quota states; no causal capacity-saving or task-quality claim"}
	root := filepath.Dir(path)
	b, err := modelcatalog.ReadAsset(root, filepath.Base(path))
	if err != nil {
		return out, err
	}
	var m SelectionReplayManifest
	if err = usage.Decode(b, &m); err != nil {
		return out, err
	}
	if m.Version != Version || len(m.Policies) != 5 || len(m.Cases) == 0 || len(m.Cases) > 1000 {
		return out, fmt.Errorf("invalid selector replay manifest")
	}
	frozen, e := time.Parse(time.RFC3339Nano, m.FrozenAt)
	audit, e2 := time.Parse(time.RFC3339Nano, m.AuditAt)
	if e != nil || e2 != nil || frozen.After(audit) {
		return out, fmt.Errorf("invalid frozen audit interval")
	}
	out.ManifestSHA256 = usage.Hash(b)
	out.FrozenAt = m.FrozenAt
	out.AuditAt = m.AuditAt
	names := map[string]bool{"pinned": true, "quality-only": true, "cost-aware": true, "quota-aware": true, "reservation-aware": true}
	policyPaths := map[string]string{}
	var commonPolicy string
	for _, p := range m.Policies {
		if !names[p.Name] || policyPaths[p.Name] != "" || !validHash(p.SHA256) {
			return out, fmt.Errorf("five unique frozen named policies required")
		}
		pb, e := modelcatalog.ReadAsset(root, p.Asset)
		if e != nil {
			return out, e
		}
		if usage.Hash(pb) != p.SHA256 {
			return out, fmt.Errorf("frozen policy hash mismatch")
		}
		var policy selection.SelectionPolicy
		if err := usage.Decode(pb, &policy); err != nil {
			return out, err
		}
		expected := map[string]string{"pinned": "quality", "quality-only": "quality", "cost-aware": "cost", "quota-aware": "capacity", "reservation-aware": "capacity"}[p.Name]
		if policy.Priority != expected || (p.Name == "pinned" && policy.Pin == "") || (p.Name != "pinned" && policy.Pin != "") {
			return out, fmt.Errorf("policy semantics do not match comparison name %s", p.Name)
		}
		// Only the pin and soft priority may differ; all mandatory approval constraints
		// and upper-bound policies must be held fixed across comparison policies.
		copyPolicy := policy
		copyPolicy.Priority = "quality"
		copyPolicy.Pin = ""
		hardHash := usage.Digest(copyPolicy)
		if commonPolicy == "" {
			commonPolicy = hardHash
		} else if commonPolicy != hardHash {
			return out, fmt.Errorf("comparison policies change common mandatory constraints")
		}
		policyPaths[p.Name] = filepath.Join(root, filepath.FromSlash(p.Asset))
		out.PolicyDigests[p.Name] = p.SHA256
	}
	out.PolicySemanticsVerified = true
	groups := map[string]string{}
	ids := map[string]bool{}
	for _, c := range m.Cases {
		if c.ID == "" || ids[c.ID] || (c.Partition != "development" && c.Partition != "validation" && c.Partition != "locked-test") || c.Repository == "" || c.Family == "" || c.NearDuplicateGroup == "" {
			return out, fmt.Errorf("invalid/duplicate replay case")
		}
		ids[c.ID] = true
		for _, g := range []string{"repository:" + c.Repository, "family:" + c.Family, "near:" + c.NearDuplicateGroup} {
			if prior, ok := groups[g]; ok && prior != c.Partition {
				return out, fmt.Errorf("selector replay partition leakage")
			}
			groups[g] = c.Partition
		}
		at, e := time.Parse(time.RFC3339Nano, c.AsOf)
		if e != nil || at.Before(frozen) || at.After(audit) {
			return out, fmt.Errorf("case observation lies outside frozen audit interval")
		}
		if partition != "" && c.Partition != partition {
			continue
		}
		// ReadAsset verifies portable local file inputs; catalog is a directory whose
		// contained hashes and symlinks are validated by the catalog loader.
		paths := []string{c.Task, c.Candidates, c.CatalogPolicy, c.Usage, c.UsagePolicy, c.EvidencePolicy}
		for _, p := range paths {
			if _, e := modelcatalog.ReadAsset(root, p); e != nil {
				return out, e
			}
		}
		if !filepath.IsLocal(c.Catalog) || filepath.IsAbs(c.Catalog) {
			return out, fmt.Errorf("unsafe catalog path")
		}
		var shared map[string]string
		for _, name := range []string{"pinned", "quality-only", "cost-aware", "quota-aware", "reservation-aware"} {
			d, e := selection.Select(selection.Request{TaskPath: filepath.Join(root, c.Task), CandidatesPath: filepath.Join(root, c.Candidates), CatalogPath: filepath.Join(root, c.Catalog), CatalogPolicyPath: filepath.Join(root, c.CatalogPolicy), UsagePath: filepath.Join(root, c.Usage), UsagePolicyPath: filepath.Join(root, c.UsagePolicy), PolicyPath: policyPaths[name], EvidencePolicyPath: filepath.Join(root, c.EvidencePolicy), AsOf: at})
			if e != nil {
				return out, fmt.Errorf("%s/%s: %w", c.ID, name, e)
			}
			if d.Inputs["policy"] != out.PolicyDigests[name] {
				return out, fmt.Errorf("frozen policy changed while replaying")
			}
			if shared == nil {
				shared = d.Inputs
			} else {
				for _, key := range []string{"task", "candidates", "catalog", "catalogPolicy", "usage", "usagePolicy", "evidencePolicy", "acceptedEvidence"} {
					if shared[key] != d.Inputs[key] {
						return out, fmt.Errorf("comparison state changed: %s", key)
					}
				}
			}
			out.Decisions = append(out.Decisions, ReplayedSelection{c.ID, name, d})
			if out.Dispositions[name] == nil {
				out.Dispositions[name] = map[string]int{}
			}
			out.Dispositions[name][d.Disposition]++
		}
	}
	sort.Slice(out.Decisions, func(i, j int) bool {
		a, b := out.Decisions[i], out.Decisions[j]
		if a.CaseID == b.CaseID {
			return a.Policy < b.Policy
		}
		return a.CaseID < b.CaseID
	})
	if len(out.Decisions) == 0 {
		return out, fmt.Errorf("partition has no replay cases")
	}
	return out, nil
}
