// Package campaign builds advisory static DAGs and compares frozen soft policies.
// It cannot launch a task or change a runtime policy.
package campaign

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation/replay"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/planning"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/selection"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

const Version = "aok-campaign-proposal/v1"

type Node struct {
	ID           string             `json:"id"`
	Requirements []string           `json:"requirements"`
	DependsOn    []string           `json:"dependsOn"`
	Plan         contracts.AssetRef `json:"plan"`
	Corpus       string             `json:"corpus"`
}
type Spec struct {
	Version              string              `json:"version"`
	ID                   string              `json:"id"`
	FrozenAt             string              `json:"frozenAt"`
	Nodes                []Node              `json:"nodes"`
	Unresolved           []string            `json:"unresolved"`
	BasePolicy           contracts.AssetRef  `json:"basePolicy"`
	AllowedPriorities    []string            `json:"allowedPriorities"`
	EvidencePolicySHA256 string              `json:"evidencePolicySha256"`
	Cohorts              []evaluation.Cohort `json:"cohorts"`
}
type Proposal struct {
	BasePolicyBytes      json.RawMessage            `json:"basePolicyBytes"`
	ValidatedPlans       map[string]json.RawMessage `json:"validatedPlans"`
	ValidationReports    map[string]planning.Report `json:"validationReports"`
	Version              string                     `json:"version"`
	ID                   string                     `json:"id"`
	AsOf                 string                     `json:"asOf"`
	SpecSHA256           string                     `json:"specSha256"`
	Spec                 Spec                       `json:"spec"`
	Order                []string                   `json:"order"`
	Status               string                     `json:"status"`
	Activation           string                     `json:"activation"`
	Digest               string                     `json:"digest"`
	AuthorizationGranted bool                       `json:"authorizationGranted"`
	ExecutionReady       bool                       `json:"executionReady"`
}

func asset(root string, a contracts.AssetRef) ([]byte, error) {
	b, e := modelcatalog.ReadAsset(root, a.Path)
	if e != nil {
		return nil, e
	}
	if usage.Hash(b) != a.SHA256 {
		return nil, fmt.Errorf("changed campaign asset %s", a.Path)
	}
	return b, nil
}
func Order(nodes []Node) ([]string, error) {
	if len(nodes) == 0 || len(nodes) > 1000 {
		return nil, fmt.Errorf("1..1000 explicit nodes required")
	}
	pending := map[string]Node{}
	done := map[string]bool{}
	order := []string{}
	for _, n := range nodes {
		if n.ID == "" || pending[n.ID].ID != "" || len(n.Requirements) == 0 {
			return nil, fmt.Errorf("unique node and explicit requirements required")
		}
		pending[n.ID] = n
	}
	for _, n := range nodes {
		seen := map[string]bool{}
		for _, dep := range n.DependsOn {
			if dep == n.ID || pending[dep].ID == "" || seen[dep] {
				return nil, fmt.Errorf("invalid dependency")
			}
			seen[dep] = true
		}
	}
	for len(pending) > 0 {
		ready := []string{}
		for id, n := range pending {
			ok := true
			for _, dep := range n.DependsOn {
				if !done[dep] {
					ok = false
				}
			}
			if ok {
				ready = append(ready, id)
			}
		}
		if len(ready) == 0 {
			return nil, fmt.Errorf("cyclic campaign")
		}
		sort.Strings(ready)
		for _, id := range ready {
			done[id] = true
			order = append(order, id)
			delete(pending, id)
		}
	}
	return order, nil
}
func Build(path string, asOf time.Time) (Proposal, error) {
	var p Proposal
	b, e := modelcatalog.ReadFile(path)
	if e != nil {
		return p, e
	}
	var s Spec
	if e = usage.Decode(b, &s); e != nil {
		return p, e
	}
	if s.Version != Version || s.ID == "" || asOf.IsZero() {
		return p, fmt.Errorf("version, id and as-of required")
	}
	f, e := time.Parse(time.RFC3339Nano, s.FrozenAt)
	if e != nil || f.After(asOf) {
		return p, fmt.Errorf("invalid campaign freeze time")
	}
	root := filepath.Dir(path)
	order, e := Order(s.Nodes)
	if e != nil {
		return p, e
	}
	corpusBytes := int64(0)
	corpusFiles := 0
	totalCaptured := len(b)
	corpora := map[string]planning.Report{}
	plans := map[string]json.RawMessage{}
	reports := map[string]planning.Report{}
	for _, n := range s.Nodes {
		raw, e := asset(root, n.Plan)
		if e != nil {
			return p, e
		}
		r, _, e := contracts.Parse(raw)
		if e != nil {
			return p, e
		}
		if r.(contracts.Resource).Kind != "PlanDefinition" {
			return p, fmt.Errorf("campaign node must reference a PlanDefinition")
		}
		if e := safeCorpus(root, n.Corpus); e != nil {
			return p, e
		}
		report, already := corpora[n.Corpus]
		if !already {
			if len(corpora) >= 32 {
				return p, fmt.Errorf("campaign exceeds 32 validated corpora")
			}
			if e := boundedCorpus(filepath.Join(root, n.Corpus), &corpusBytes, &corpusFiles); e != nil {
				return p, e
			}
			report = planning.Validate(filepath.Join(root, n.Corpus))
			corpora[n.Corpus] = report
		}
		rb, _ := json.Marshal(report)
		totalCaptured += len(raw) + len(rb)
		if totalCaptured > 3<<20 {
			return p, fmt.Errorf("campaign captured evidence exceeds 3 MiB")
		}

		plans[n.ID] = append([]byte(nil), raw...)
		reports[n.ID] = report
		if report.Status != "valid" {
			return p, fmt.Errorf("campaign plan corpus is invalid: %s", n.ID)
		}
		// The exact plan must belong to this validated corpus.
		rel, e := filepath.Rel(filepath.Join(root, n.Corpus), filepath.Join(root, n.Plan.Path))
		if e != nil {
			return p, e
		}
		if e := safeCorpus(filepath.Join(root, n.Corpus), rel); e != nil {
			return p, fmt.Errorf("plan outside validated corpus")
		}
	}
	base, e := asset(root, s.BasePolicy)
	if e != nil {
		return p, e
	}
	if totalCaptured+len(base) > 3<<20 {
		return p, fmt.Errorf("campaign exceeds capture budget")
	}
	var policy selection.SelectionPolicy
	if e = selection.ValidateJSON(base, "policy"); e != nil {
		return p, e
	}
	if e = usage.Decode(base, &policy); e != nil {
		return p, e
	}
	seen := map[string]bool{}
	for _, v := range s.AllowedPriorities {
		if v != "quality" && v != "cost" && v != "capacity" && v != "deadline" || seen[v] {
			return p, fmt.Errorf("only explicit unique priority variants are permitted")
		}
		seen[v] = true
	}
	if len(seen) == 0 {
		return p, fmt.Errorf("bounded soft-policy search space required")
	}
	if len(s.EvidencePolicySHA256) != 64 {
		return p, fmt.Errorf("frozen external evidence policy digest required")
	}
	p = Proposal{BasePolicyBytes: append([]byte(nil), base...), ValidatedPlans: plans, ValidationReports: reports, Version: Version, ID: s.ID, AsOf: asOf.UTC().Format(time.RFC3339Nano), SpecSHA256: usage.Hash(b), Spec: s, Order: order, Status: "advisory", Activation: "pending-qualified-fixed-dag-route"}
	if len(s.Unresolved) > 0 {
		p.Status = "abstain-unresolved-requirements"
	}
	p.Digest = usage.Digest(p)
	return p, nil
}
func safeCorpus(root, rel string) error {
	if rel == "" {
		return fmt.Errorf("explicit corpus path required")
	}
	if rel == "." {
		return nil
	}
	return evidence.CheckPath(rel)
}
func Validate(p Proposal) error {
	saved := p.Digest
	p.Digest = ""
	if p.Version != Version || p.Spec.Version != Version || usage.Hash(p.BasePolicyBytes) != p.Spec.BasePolicy.SHA256 || saved != usage.Digest(p) || p.AuthorizationGranted || p.ExecutionReady || p.Activation != "pending-qualified-fixed-dag-route" {
		return fmt.Errorf("invalid advisory proposal")
	}
	for _, n := range p.Spec.Nodes {
		raw, ok := p.ValidatedPlans[n.ID]
		if !ok || usage.Hash(raw) != n.Plan.SHA256 {
			return fmt.Errorf("missing or changed validated plan")
		}
		r, _, e := contracts.Parse(raw)
		if e != nil {
			return e
		}
		if r.(contracts.Resource).Kind != "PlanDefinition" {
			return fmt.Errorf("plan kind mismatch")
		}
		report, ok := p.ValidationReports[n.ID]
		if !ok || report.Status != "valid" || report.ContextSHA256 == "" {
			return fmt.Errorf("plan validation is not valid")
		}
		rel, e := filepath.Rel(n.Corpus, n.Plan.Path)
		if e != nil || report.PlanHashes[filepath.ToSlash(rel)] != n.Plan.SHA256 {
			return fmt.Errorf("plan does not match validated corpus report")
		}
	}
	if p.Status != "advisory" && p.Status != "abstain-unresolved-requirements" {
		return fmt.Errorf("invalid campaign disposition")
	}
	if (len(p.Spec.Unresolved) > 0) != (p.Status == "abstain-unresolved-requirements") {
		return fmt.Errorf("unresolved requirements cannot be cleared")
	}
	order, e := Order(p.Spec.Nodes)
	if e != nil {
		return e
	}
	if usage.Digest(order) != usage.Digest(p.Order) {
		return fmt.Errorf("changed campaign order")
	}
	return nil
}

type Evaluation struct {
	Disposition          string                        `json:"disposition"`
	Version              string                        `json:"version"`
	ProposalSHA256       string                        `json:"proposalSha256"`
	EvidenceSHA256       string                        `json:"evidenceSha256"`
	EvidencePolicySHA256 string                        `json:"evidencePolicySha256"`
	AsOf                 string                        `json:"asOf"`
	Estimates            []evaluation.EstimateArtifact `json:"estimates"`
	Claim                string                        `json:"claim"`
	AuthorizationGranted bool                          `json:"authorizationGranted"`
	ExecutionReady       bool                          `json:"executionReady"`
}

func Evaluate(p Proposal, shards []string, policy string, at time.Time) (Evaluation, error) {
	out := Evaluation{Disposition: p.Status, Version: Version, ProposalSHA256: p.Digest, AsOf: at.UTC().Format(time.RFC3339Nano), Estimates: []evaluation.EstimateArtifact{}, Claim: "cohort-specific signed evidence; no dispatch or production activation"}
	if e := Validate(p); e != nil {
		return out, e
	}
	if at.IsZero() {
		return out, fmt.Errorf("as-of required")
	}
	v, e := evaluation.Load(shards, policy)
	if e != nil {
		return out, e
	}
	if evaluation.PolicySHA256(v) != p.Spec.EvidencePolicySHA256 {
		return out, fmt.Errorf("evidence policy differs from frozen proposal")
	}
	out.EvidenceSHA256 = evaluation.EvidenceSHA256(v)
	out.EvidencePolicySHA256 = evaluation.PolicySHA256(v)
	for _, c := range p.Spec.Cohorts {
		est, e := evaluation.Estimate(v, c, at)
		if e != nil {
			return out, e
		}
		out.Estimates = append(out.Estimates, est)
	}
	return out, nil
}

type Comparison struct {
	AsOf                 string                       `json:"asOf"`
	Version              string                       `json:"version"`
	ProposalSHA256       string                       `json:"proposalSha256"`
	Replay               replay.SelectionReplayReport `json:"replay"`
	Adoption             string                       `json:"adoption"`
	AuthorizationGranted bool                         `json:"authorizationGranted"`
	ExecutionReady       bool                         `json:"executionReady"`
}

func Compare(p Proposal, root, manifest, partition string, at time.Time) (Comparison, error) {
	out := Comparison{AsOf: at.UTC().Format(time.RFC3339Nano), Version: Version, ProposalSHA256: p.Digest, Adoption: "new reviewed policy version required"}
	if e := Validate(p); e != nil {
		return out, e
	}
	if at.IsZero() {
		return out, fmt.Errorf("as-of required")
	}
	if p.Status != "advisory" {
		return out, fmt.Errorf("unresolved campaign cannot propose policy adoption")
	}
	if partition != "development" && partition != "validation" {
		return out, fmt.Errorf("optimization cannot inspect locked audit outcomes")
	}
	base := []byte(p.BasePolicyBytes)
	var e error
	_ = root
	var baseline selection.SelectionPolicy
	if e = usage.Decode(base, &baseline); e != nil {
		return out, e
	}
	raw, e := modelcatalog.ReadFile(manifest)
	if e != nil {
		return out, e
	}
	var m replay.SelectionReplayManifest
	if e = usage.Decode(raw, &m); e != nil {
		return out, e
	}
	if m.FrozenAt != p.Spec.FrozenAt {
		return out, fmt.Errorf("comparison freeze differs")
	}
	manifestRoot := filepath.Dir(manifest)
	for _, variant := range m.Policies {
		raw, e := asset(manifestRoot, contracts.AssetRef{Path: variant.Asset, SHA256: variant.SHA256})
		if e != nil {
			return out, e
		}
		var candidate selection.SelectionPolicy
		if e = usage.Decode(raw, &candidate); e != nil {
			return out, e
		}
		if e = SoftChange(baseline, candidate, p.Spec.AllowedPriorities); e != nil {
			return out, e
		}
	}
	for _, c := range m.Cases {
		raw, e := modelcatalog.ReadAsset(manifestRoot, c.EvidencePolicy)
		if e != nil {
			return out, e
		}
		if usage.Hash(raw) != p.Spec.EvidencePolicySHA256 {
			return out, fmt.Errorf("replay evidence policy differs")
		}
	}
	out.Replay, e = replay.ReplaySelections(manifest, partition)
	return out, e
}
func SoftChange(base, candidate selection.SelectionPolicy, allowed []string) error {
	ok := false
	for _, p := range allowed {
		if p == candidate.Priority {
			ok = true
		}
	}
	if !ok {
		return fmt.Errorf("priority outside permitted search space")
	}
	candidate.Priority = base.Priority
	if usage.Digest(base) != usage.Digest(candidate) {
		return fmt.Errorf("hard constraints or unapproved soft fields changed")
	}
	return nil
}

// Bound the entire source walk before the shared validator reads any corpus.
func boundedCorpus(root string, total *int64, count *int) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink campaign corpus")
		}
		*count++
		if *count > 2000 {
			return fmt.Errorf("campaign corpus entry count exceeds bound")
		}
		rel, _ := filepath.Rel(root, path)
		if len(strings.Split(filepath.ToSlash(rel), "/")) > 64 {
			return fmt.Errorf("campaign corpus depth exceeds bound")
		}
		if d.IsDir() {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular campaign corpus asset")
		}
		*total += info.Size()
		if *count > 2000 || *total > 64<<20 || info.Size() > 4<<20 {
			return fmt.Errorf("campaign corpus exceeds bounded source budget")
		}
		return nil
	})
}
