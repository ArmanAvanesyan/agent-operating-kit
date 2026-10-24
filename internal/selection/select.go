package selection

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"math/big"
	"path/filepath"
	"sort"
	"time"
)

func Select(req Request) (SelectionDecision, error) {
	d := SelectionDecision{Version: Version, AsOf: req.AsOf.UTC().Format(time.RFC3339Nano), Inputs: map[string]string{}, Candidates: []CandidateResult{}, Explanation: []string{}, NextEvidence: []string{}, Disposition: "abstain"}
	if req.AsOf.IsZero() {
		return d, fmt.Errorf("as-of required")
	}
	var task TaskProfile
	var manifest CandidateManifest
	var policy SelectionPolicy
	var ep evaluation.EvidencePolicy
	for _, i := range []struct {
		name, path string
		target     any
	}{{"task", req.TaskPath, &task}, {"candidates", req.CandidatesPath, &manifest}, {"policy", req.PolicyPath, &policy}, {"evidencePolicy", req.EvidencePolicyPath, &ep}} {
		b, e := read(i.path, i.target)
		if e != nil {
			return d, e
		}
		kind := i.name
		if kind == "task" || kind == "candidates" || kind == "policy" {
			if e = ValidateJSON(b, kind); e != nil {
				return d, e
			}
		}
		d.Inputs[i.name] = usage.Hash(b)
	}
	if e := validateInputs(task, manifest, policy); e != nil {
		return d, e
	}
	taskRoot := filepath.Dir(req.TaskPath)
	for name, a := range map[string]Asset{"assignment": task.Assignment, "packet": task.Packet, "inputRevision": task.InputRevision} {
		_, e := asset(taskRoot, a)
		if e != nil {
			return d, e
		}
		d.Inputs[name] = a.SHA256
	}
	assignmentBytes, _ := asset(taskRoot, task.Assignment)
	res, _, err := contracts.Parse(assignmentBytes)
	if err != nil {
		return d, fmt.Errorf("assignment: %w", err)
	}
	resource, ok := res.(contracts.Resource)
	if !ok {
		return d, fmt.Errorf("invalid assignment resource")
	}
	typed, err := contracts.DecodeTyped(resource)
	if err != nil {
		return d, err
	}
	assignment, ok := typed.(contracts.AgentAssignment)
	if !ok {
		return d, fmt.Errorf("assignment kind required")
	}
	packetPath := filepath.Join(taskRoot, filepath.FromSlash(task.Packet.Path))
	assignmentPacket := filepath.Join(filepath.Dir(filepath.Join(taskRoot, task.Assignment.Path)), filepath.FromSlash(assignment.TaskPacket))
	if filepath.Clean(packetPath) != filepath.Clean(assignmentPacket) {
		return d, fmt.Errorf("assignment packet reference mismatch")
	}
	catalog, report := modelcatalog.Load(req.CatalogPath, req.CatalogPolicyPath)
	if report.Status != "valid" {
		return d, fmt.Errorf("invalid catalog: %v", report.Diagnostics)
	}
	exp, up, err := usage.Load(req.UsagePath, req.UsagePolicyPath)
	if err != nil {
		return d, err
	}
	d.Inputs["catalog"] = catalog.CatalogSHA256
	d.Inputs["catalogPolicy"] = catalog.PolicySHA256
	d.Inputs["usage"] = usage.Digest(exp)
	d.Inputs["usagePolicy"] = usage.PolicyHash(up)
	root := filepath.Dir(req.CandidatesPath)
	shards, err := resolveShards(root, manifest.OutcomeShards)
	if err != nil {
		return d, err
	}
	verified, err := evaluation.Load(shards, req.EvidencePolicyPath)
	if err != nil {
		return d, err
	}
	// The external policy and every signed shard digest are included transitively in estimates.
	d.Inputs["acceptedEvidencePolicy"] = evaluation.PolicySHA256(verified)
	d.Inputs["acceptedEvidence"] = evaluation.EvidenceSHA256(verified)
	if d.Inputs["evidencePolicy"] != evaluation.PolicySHA256(verified) {
		return d, fmt.Errorf("evidence policy changed during capture")
	}
	var previous *SelectionDecision
	if manifest.PreviousDecision != nil {
		b, e := asset(root, *manifest.PreviousDecision)
		if e != nil {
			return d, e
		}
		var p SelectionDecision
		if e = usage.Decode(b, &p); e != nil {
			return d, e
		}
		if p.Version != Version || p.AuthorizationGranted || p.ExecutionReady {
			return d, fmt.Errorf("invalid previous decision")
		}
		saved := p.Digest
		p.Digest = ""
		if saved != usage.Digest(p) {
			return d, fmt.Errorf("previous decision digest mismatch")
		}
		p.Digest = saved
		if p.Inputs["task"] == d.Inputs["task"] && p.Inputs["policy"] == d.Inputs["policy"] {
			previous = &p
		}
		d.Inputs["previousDecision"] = manifest.PreviousDecision.SHA256
	}
	deadline, _ := time.Parse(time.RFC3339Nano, task.Deadline)
	until := decisionExpiry(catalog, exp, up, req.AsOf)
	for _, candidate := range manifest.Candidates {
		cr := CandidateResult{ConfigurationID: candidate.ConfigurationID, AccountScope: candidate.AccountScope, ProfileSHA256: candidate.Profile.SHA256, Gates: []Gate{}, Requirements: []modelcatalog.Finding{}}
		add := func(n, s, r string) { cr.Gates = append(cr.Gates, Gate{n, s, r}) }
		pb, e := asset(root, candidate.Profile)
		if e != nil {
			return d, e
		}
		d.Inputs["profile:"+candidate.ConfigurationID] = usage.Hash(pb)
		pr, ds, e := contracts.Parse(pb)
		if e != nil || hasErrors(ds) {
			return d, fmt.Errorf("invalid candidate profile")
		}
		rr, ok := pr.(contracts.Resource)
		if !ok {
			return d, fmt.Errorf("profile resource required")
		}
		pt, e := contracts.DecodeTyped(rr)
		if e != nil {
			return d, e
		}
		profile, ok := pt.(contracts.ExecutionProfile)
		if !ok {
			return d, fmt.Errorf("execution profile required")
		}
		boundAssignment, routeSHA, e := routeAssignment(root, candidate, task, assignment)
		if e != nil {
			return d, e
		}
		d.Inputs["assignment:"+candidate.ConfigurationID] = routeSHA
		contextTask := task
		contextTask.Assignment.SHA256 = routeSHA
		if boundAssignment.ExecutionProfile.Kind != "ExecutionProfile" || boundAssignment.ExecutionProfile.ID != profile.Metadata.ID || boundAssignment.ExecutionProfile.Version != profile.Metadata.Version {
			add("assignment-profile", "fail", "assignment execution-profile reference does not match original candidate profile")
		} else {
			add("assignment-profile", "pass", "assignment binds this exact profile identity")
		}
		exact := false
		if cfg, known := modelcatalog.LookupConfiguration(catalog, candidate.ConfigurationID); known {
			exact = exactProfile(cfg, profile)
		}
		if !exact || candidate.CatalogSHA256 != catalog.CatalogSHA256 {
			add("identity", "fail", "configuration/profile/catalog identity mismatch or unknown dimensions")
		} else {
			add("identity", "pass", "exact catalog configuration and original profile")
		}
		route := false
		for _, r := range policy.Routes {
			if r.ConfigurationID == candidate.ConfigurationID && r.AccountScope == candidate.AccountScope {
				route = true
			}
		}
		if !route {
			add("route", "fail", "route or billing account is not allowed")
		} else {
			add("route", "pass", "explicit allowed route")
		}
		binding := bindingsMatch(exp, candidate)
		if binding {
			add("account-binding", "pass", "exact declared pool bindings")
		} else {
			add("account-binding", "unknown", "account binding coverage does not match current usage export")
		}
		for _, need := range []struct {
			dim    string
			values []string
		}{{"account", []string{candidate.AccountScope}}, {"region", task.Regions}, {"permissions", task.Permissions}, {"namedTools", task.NamedTools}} {
			if len(need.values) == 0 {
				continue
			}
			supported := false
			for _, o := range candidate.Context {
				if o.Dimension == need.dim && contextTrusted(o, candidate, contextTask, ep, usage.ConfigurationBindingsDigest(exp, candidate.ConfigurationID), req.AsOf) && containsAll(o.Values, need.values) {
					supported = true
					t, _ := time.Parse(time.RFC3339Nano, o.ExpiresAt)
					obs, _ := time.Parse(time.RFC3339Nano, o.ObservedAt)
					t = minTime(t, obs.Add(5*time.Minute))
					until = minTime(until, t)
				}
			}
			if supported {
				add(need.dim, "pass", "signed fresh exact-scope static context; no execution authorization")
			} else {
				add(need.dim, "unknown", "fresh signed exact-scope context required")
			}
		}
		cr.Compatibility = modelcatalog.CheckWithProfileDigest(catalog, profile, candidate.Profile.SHA256, req.AsOf)
		add("profile-compatibility", gateStatus(cr.Compatibility.Compatibility), "catalog static compatibility")
		requirements := append([]modelcatalog.Requirement(nil), task.Requirements...)
		requirements = append(requirements, modelcatalog.Requirement{Dimension: "contextTokens", Value: json.RawMessage(task.ContextTokens)}, modelcatalog.Requirement{Dimension: "maxOutputTokens", Value: json.RawMessage(task.OutputTokens)})
		cr.Requirements = modelcatalog.CheckRequirements(catalog, profile, candidate.Profile.SHA256, requirements, req.AsOf)
		for _, f := range cr.Requirements {
			add("capability:"+f.Field, gateStatus(f.Status), f.Message)
		}
		cohort := evaluation.Cohort{ConfigurationID: candidate.ConfigurationID, ProfileSHA256: candidate.Profile.SHA256, TaskClass: task.TaskClass, Language: task.Language, Risk: task.Risk, AccountScope: candidate.AccountScope, ProtocolSHA256: task.ProtocolSHA256}
		est, e := evaluation.Estimate(verified, cohort, req.AsOf)
		if e != nil {
			add("quality", "unknown", e.Error())
			add("cost-time", "unknown", "bounded accepted-cohort estimates required")
		} else {
			if e = applyUpperBounds(&est, policy, filepath.Dir(req.PolicyPath), req.AsOf); e != nil {
				return d, e
			}
			cr.Estimate = &est
			floor := policy.StandardFloor
			if task.Risk == "high" {
				floor = policy.HighRiskFloor
			}
			qtime, _ := time.Parse(time.RFC3339Nano, est.QualityValidUntil)
			dtime, _ := time.Parse(time.RFC3339Nano, est.DemandValidUntil)
			if !req.AsOf.Before(qtime) {
				add("quality", "unknown", "quality evidence expired")
			} else if est.Families < 30 || rat(est.QualityLower).Cmp(rat(floor)) < 0 {
				add("quality", "fail", "family-level Wilson lower bound below floor")
			} else {
				add("quality", "pass", "independent accepted task-family evidence clears floor")
				until = minTime(until, qtime)
			}
			if est.CostUpper == nil || est.SecondsUpper == nil || !req.AsOf.Before(dtime) || est.Currency != task.Currency {
				add("cost-time", "unknown", "fresh bounded complete cost and time in requested currency required")
			} else if rat(*est.CostUpper).Cmp(rat(task.SpendLimit)) > 0 || rat(*est.SecondsUpper).Cmp(big.NewRat(int64(deadline.Sub(req.AsOf)/time.Nanosecond), 1_000_000_000)) > 0 {
				add("cost-time", "fail", "estimated cost/time exceeds task limit")
			} else {
				add("cost-time", "pass", "bounded protocol cost and time satisfy task limits")
				until = minTime(until, dtime)
			}
			demand := MaterializeDemand(exp, up, candidate.ConfigurationID, candidate.Profile.SHA256, est, req.AsOf)
			cap := usage.Capacity(exp, up, candidate.ConfigurationID, &demand, req.AsOf)
			cr.Capacity = &cap
			if cap.Disposition == "observed-fit" {
				add("capacity", "pass", "fresh observed fit across all current constraints")
			} else {
				add("capacity", "unknown", cap.Disposition)
			}
			if est.CostUpper != nil && est.SecondsUpper != nil && rat(est.QualityLower).Sign() > 0 {
				cr.Rank = &Rank{Quality: est.QualityLower, CostIndex: divide(*est.CostUpper, est.QualityLower), TimeIndex: divide(*est.SecondsUpper, est.QualityLower), Scarcity: scarcity(cap, req.AsOf)}
			}
		}
		if task.MidRun && !task.SafeCheckpoint {
			add("checkpoint", "unknown", "explicit safe checkpoint required for mid-run change")
		}
		cr.Eligible = allGates(cr.Gates)
		d.Candidates = append(d.Candidates, cr)
	}
	sort.Slice(d.Candidates, func(i, j int) bool { return d.Candidates[i].ConfigurationID < d.Candidates[j].ConfigurationID })
	decide(&d, task, policy, previous, req.AsOf)
	d.ValidUntil = until.UTC().Format(time.RFC3339Nano)
	d.Digest = usage.Digest(d)
	return d, nil
}
func gateStatus(s string) string {
	if s == "supported" {
		return "pass"
	}
	if s == "unsupported" {
		return "fail"
	}
	return "unknown"
}
func allGates(g []Gate) bool {
	for _, v := range g {
		if v.Status != "pass" {
			return false
		}
	}
	return true
}
func containsAll(a, b []string) bool {
	for _, v := range b {
		found := false
		for _, w := range a {
			if w == v {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
func rat(n json.Number) *big.Rat {
	x, e := usage.NumberRat(n)
	if e != nil {
		return new(big.Rat)
	}
	return x
}
func divide(a, b json.Number) json.Number { return ceilDecimal(new(big.Rat).Quo(rat(a), rat(b))) }
func exactProfile(c modelcatalog.Configuration, p contracts.ExecutionProfile) bool {
	expected := modelcatalog.Configuration{Host: &p.Client.Adapter, HostVersion: p.Client.VersionConstraint, Provider: p.Model.Provider, ModelID: &p.Model.Selector, ReasoningMode: &p.Model.Reasoning.Mode, Effort: p.Model.Reasoning.Effort, ReasoningBudgetTokens: p.Model.Reasoning.BudgetTokens, Generation: p.Model.Generation}
	return c.HostVersion != nil && c.Provider != nil && usage.Digest(c) == usage.Digest(expected)
}
func bindingsMatch(e usage.Export, c Candidate) bool {
	expected := []string{}
	for _, b := range e.Bindings {
		for _, id := range b.ConfigurationIDs {
			if id == c.ConfigurationID {
				expected = append(expected, b.ID)
			}
		}
	}
	return len(expected) > 0 && len(expected) == len(c.BindingIDs) && containsAll(expected, c.BindingIDs) && containsAll(c.BindingIDs, expected)
}
func scarcity(c usage.CapacitySnapshot, at time.Time) json.Number {
	worst := new(big.Rat)
	for _, b := range c.Balances {
		if b.Demand == nil || b.Headroom == nil {
			continue
		}
		h := rat(*b.Headroom)
		d := rat(*b.Demand)
		if h.Sign() == 0 {
			if d.Sign() > 0 {
				return json.Number("1e100")
			}
			continue
		}
		r := new(big.Rat).Quo(d, h)
		weight := big.NewRat(1, 1)
		if b.ResetAt != nil {
			t, e := time.Parse(time.RFC3339Nano, *b.ResetAt)
			if e == nil {
				seconds := int64(t.Sub(at) / time.Second)
				if seconds < 0 {
					seconds = 0
				}
				if seconds < 86400 {
					weight = big.NewRat(seconds, 86400)
				}
			}
		}
		r.Mul(r, weight)
		if r.Cmp(worst) > 0 {
			worst.Set(r)
		}
	}
	return ceilDecimal(worst)
}

func hasErrors(ds []contracts.Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

func ceilDecimal(r *big.Rat) json.Number {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	n := new(big.Int).Mul(r.Num(), scale)
	q, rem := new(big.Int), new(big.Int)
	q.QuoRem(n, r.Denom(), rem)
	if rem.Sign() > 0 {
		q.Add(q, big.NewInt(1))
	}
	return usage.Decimal(new(big.Rat).SetFrac(q, scale))
}

// MaterializeDemand binds generation-neutral estimates to the same current
// constraints used by selection and accounting. It never predicts new windows.
func MaterializeDemand(exp usage.Export, up usage.Policy, configurationID, profileSHA256 string, est evaluation.EstimateArtifact, asOf time.Time) usage.Demand {
	current := usage.CurrentConstraints(exp, up, configurationID, asOf)
	demand := usage.Demand{Version: usage.Version, ConfigurationID: configurationID, ProfileSHA256: profileSHA256, Source: "aok-evaluation/v1:" + usage.Digest(est), ValidUntil: est.DemandValidUntil, Amounts: []usage.Amount{}}
	for _, balance := range current.Balances {
		for _, bound := range est.Bounds {
			if bound.Upper != nil && bound.PoolID == balance.Constraint.PoolID && bound.WindowFamily == usage.WindowFamily(balance.Constraint) && bound.Metric == balance.Constraint.Metric && bound.Unit == balance.Constraint.Unit {
				demand.Amounts = append(demand.Amounts, usage.Amount{ConstraintID: balance.Constraint.ID, Generation: balance.Generation, Unit: balance.Constraint.Unit, Amount: *bound.Upper})
			}
		}
	}
	return demand
}
