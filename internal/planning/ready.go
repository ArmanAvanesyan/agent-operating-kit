package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentvalidate"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

// Ready evaluates one plan against supplied, hash-bound evidence. It performs
// no dispatch and uses asOf instead of consulting the machine clock.
func Ready(planPath, evidencePath, corpusRoot, asOf string) Report {
	return ready(planPath, evidencePath, corpusRoot, asOf, nil)
}

// ReadyWithAcceptedDeps accepts predecessor IDs only from a caller that has
// already verified protected P8 runtime projections. Task data and planning
// evidence remain untrusted completion inputs at the runtime boundary.
func ReadyWithAcceptedDeps(planPath, evidencePath, corpusRoot, asOf string, acceptedSteps []string) Report {
	return ready(planPath, evidencePath, corpusRoot, asOf, acceptedSteps)
}

func ready(planPath, evidencePath, corpusRoot, asOf string, acceptedSteps []string) Report {
	if corpusRoot == "" {
		absPlan, err := filepath.Abs(planPath)
		if err != nil {
			r := Report{Diagnostics: []contracts.Diagnostic{}}
			r.add("planning.ready.plan-path", "", "planPath", "error", err.Error())
			r.finish("invalid")
			return r
		}
		corpusRoot = filepath.Dir(absPlan)
	}
	c, loaded := agentvalidate.Load(corpusRoot)
	r := Report{Diagnostics: append([]contracts.Diagnostic{}, loaded.Diagnostics...)}
	if c != nil {
		setContextHashes(&r, c)
	}
	if c == nil || loaded.Status == "invalid" {
		r.finish("invalid")
		return r
	}
	lexicalRoot, rootErr := filepath.Abs(corpusRoot)
	if rootErr != nil {
		r.add("planning.ready.root", "", "corpusRoot", "error", rootErr.Error())
		r.finish("invalid")
		return r
	}
	planRel, err := confinedName(lexicalRoot, planPath)
	if err != nil {
		r.add("planning.ready.plan-path", "", "planPath", "error", err.Error())
		r.finish("invalid")
		return r
	}
	evidenceRel, err := confinedName(lexicalRoot, evidencePath)
	if err != nil {
		r.add("planning.ready.evidence-path", "", "evidencePath", "error", err.Error())
		r.finish("invalid")
		return r
	}
	base := Validate(c.Root)
	r.Diagnostics = append(r.Diagnostics, base.Diagnostics...)
	if base.Status == "invalid" {
		r.finish("invalid")
		return r
	}
	var plan, evidence *agentvalidate.LoadedResource
	for i := range c.Resources {
		doc := &c.Resources[i]
		if doc.Path == planRel && doc.Resource.Kind == "PlanDefinition" {
			plan = doc
		}
		if doc.Path == evidenceRel && doc.Resource.Kind == "PlanningEvidence" {
			evidence = doc
		}
	}
	if plan == nil {
		r.add("planning.ready.plan", planRel, "planPath", "error", "planPath must name a corpus PlanDefinition document")
	}
	if evidence == nil {
		r.add("planning.ready.evidence", evidenceRel, "evidencePath", "error", "evidencePath must name a corpus PlanningEvidence document")
	}
	when, parseErr := time.Parse(time.RFC3339, asOf)
	if parseErr != nil {
		r.add("planning.ready.as-of", "", "asOf", "error", "asOf must be RFC3339")
	}
	if hasError(r.Diagnostics) {
		r.finish("invalid")
		return r
	}
	goalRef := refValue(plan.Object["goalRef"])
	if !sameRef(refValue(evidence.Object["planRef"]), contracts.Reference{Kind: "PlanDefinition", ID: plan.Resource.Envelope.Metadata.ID, Version: plan.Resource.Envelope.Metadata.Version}) {
		r.add("planning.ready.evidence-plan", evidence.Path, "planRef", "error", "evidence must pin the selected exact plan")
	}
	digest := sha256.Sum256(plan.Resource.Document)
	stale := stringValue(evidence.Object["planSHA256"]) != hex.EncodeToString(digest[:])
	if stale {
		r.add("planning.ready.stale", evidence.Path, "planSHA256", "warning", "evidence hash does not match the selected plan")
	}
	evaluated, evalErr := time.Parse(time.RFC3339, stringValue(evidence.Object["evaluatedAt"]))
	expires, expErr := time.Parse(time.RFC3339, stringValue(evidence.Object["expiresAt"]))
	expired := false
	if evalErr != nil || expErr != nil || !expires.After(evaluated) {
		r.add("planning.ready.evidence-time", evidence.Path, "evaluatedAt", "error", "evidence timestamps must be RFC3339 with expiresAt after evaluatedAt")
	} else if evaluated.After(when) {
		r.add("planning.ready.future", evidence.Path, "evaluatedAt", "error", "evidence was evaluated after asOf")
	} else if !when.Before(expires) {
		expired = true
		r.add("planning.ready.expired", evidence.Path, "expiresAt", "warning", "planning evidence has expired at asOf")
	}
	goalDoc, found := findResource(c, goalRef)
	if !found {
		r.add("planning.ready.goal", plan.Path, "goalRef", "error", "plan goal is missing")
	}
	if hasError(r.Diagnostics) {
		r.finish("invalid")
		return r
	}
	cancelled := evidence.Object["cancelled"] == true
	if cancelled {
		r.add("planning.ready.cancelled", evidence.Path, "cancelled", "warning", "evidence records cancellation")
	}
	budgetExhausted := isAtOrOver(evidence.Object["attemptsUsed"], object(goalDoc.Object["limits"])["maxAttempts"]) || isAtOrOver(evidence.Object["elapsedSeconds"], object(goalDoc.Object["limits"])["timeoutSeconds"])
	if budgetExhausted {
		r.add("planning.ready.budget", evidence.Path, "attemptsUsed", "warning", "a goal execution budget is exhausted")
	}
	if exceeds(evidence.Object["revisionsUsed"], object(goalDoc.Object["limits"])["maxReplans"]) {
		r.add("planning.ready.replans", evidence.Path, "revisionsUsed", "error", "revision usage exceeds the goal maxReplans limit")
	}
	if exceeds(evidence.Object["revisionsUsed"], object(plan.Object["replanning"])["maxRevisions"]) {
		r.add("planning.ready.replans", evidence.Path, "revisionsUsed", "error", "revision usage exceeds the plan maxRevisions limit")
	}
	if hasError(r.Diagnostics) {
		r.finish("invalid")
		return r
	}
	coverage := map[string]bool{}
	for _, v := range array(evidence.Object["inventoryCoverage"]) {
		coverage[stringValue(v)] = true
	}
	for _, raw := range array(plan.Object["inventory"]) {
		id := stringValue(object(raw)["id"])
		if !coverage[id] {
			r.add("planning.ready.inventory", evidence.Path, "inventoryCoverage", "warning", "inventory item is not covered: "+id)
		}
	}
	outcomes := map[string]map[string]any{}
	for _, raw := range array(evidence.Object["outcomes"]) {
		o := object(raw)
		outcomes[stringValue(o["predicateId"])] = o
	}
	stepByID := map[string]map[string]any{}
	for _, raw := range array(plan.Object["steps"]) {
		s := object(raw)
		stepByID[stringValue(s["id"])] = s
	}
	completed := map[string]bool{}
	for _, v := range array(evidence.Object["completedSteps"]) {
		completed[stringValue(v)] = true
	}
	accepted := map[string]bool{}
	for _, id := range acceptedSteps {
		if id != "" {
			accepted[id] = true
			completed[id] = true
		}
	}
	checkPassing := func(id string, required bool) bool {
		o, ok := outcomes[id]
		if !ok {
			return false
		}
		if stringValue(o["status"]) != "pass" || len(array(o["artifactRefs"])) == 0 {
			return false
		}
		return true
	}
	doneMemo := map[string]bool{}
	doneKnown := map[string]bool{}
	var fullyDone func(string) bool
	fullyDone = func(id string) (done bool) {
		if doneKnown[id] {
			return doneMemo[id]
		}
		defer func() { doneKnown[id] = true; doneMemo[id] = done }()
		if !completed[id] {
			return false
		}
		if accepted[id] {
			return true
		}
		s := stepByID[id]
		for _, dep := range array(s["dependsOn"]) {
			if !fullyDone(stringValue(dep)) {
				return false
			}
		}
		for _, raw := range array(s["completionChecks"]) {
			p := object(raw)
			if p["required"] == true && !checkPassing(stringValue(p["id"]), true) {
				return false
			}
		}
		return true
	}
	for _, raw := range array(goalDoc.Object["invariants"]) {
		p := object(raw)
		if p["required"] == true && !checkPassing(stringValue(p["id"]), true) {
			r.add("planning.ready.invariant", evidence.Path, "outcomes", "warning", "required invariant is not verified: "+stringValue(p["id"]))
		}
	}
	eligible := []string{}
	for id, s := range stepByID {
		if completed[id] {
			continue
		}
		ready := true
		for _, dep := range array(s["dependsOn"]) {
			if !fullyDone(stringValue(dep)) {
				r.add("planning.ready.dependency", plan.Path, "steps."+id+".dependsOn", "info", "dependency is not verified complete: "+stringValue(dep))
				ready = false
			}
		}
		for _, raw := range array(s["preconditions"]) {
			p := object(raw)
			if p["required"] == true && !checkPassing(stringValue(p["id"]), true) {
				r.add("planning.ready.precondition", plan.Path, "steps."+id+".preconditions", "info", "required precondition is not verified: "+stringValue(p["id"]))
				ready = false
			}
		}
		for _, raw := range array(goalDoc.Object["invariants"]) {
			p := object(raw)
			if p["required"] == true && !checkPassing(stringValue(p["id"]), true) {
				ready = false
			}
		}
		if ready {
			eligible = append(eligible, id)
		}
	}
	goalOK := true
	for _, field := range []string{"successPredicates", "invariants"} {
		for _, raw := range array(goalDoc.Object[field]) {
			p := object(raw)
			if p["required"] == true && !checkPassing(stringValue(p["id"]), false) {
				goalOK = false
			}
		}
	}
	for _, raw := range array(plan.Object["aggregateVerification"]) {
		p := object(raw)
		if p["required"] == true && !checkPassing(stringValue(p["id"]), false) {
			goalOK = false
		}
	}
	globalBlocked := stale || cancelled || expired
	for _, d := range r.Diagnostics {
		if d.Code == "planning.ready.inventory" {
			globalBlocked = true
		}
	}
	if goalOK && !globalBlocked {
		r.GoalStatus = "verified-success"
		eligible = []string{}
	} else {
		r.GoalStatus = "in-progress"
	}
	sort.Strings(eligible)
	blocked := false
	if budgetExhausted || stale || cancelled || globalBlocked {
		eligible = []string{}
		if !goalOK || stale || cancelled || globalBlocked {
			blocked = true
		}
	}
	if len(eligible) == 0 && r.GoalStatus != "verified-success" {
		r.add("planning.ready.no-eligible-step", plan.Path, "steps", "warning", "no step is eligible and the goal is not verified")
		blocked = true
	}
	if blocked {
		r.finish("blocked")
	} else {
		r.finish("valid")
	}
	r.EligibleSteps = eligible
	return r
}
func exceeds(value, limit any) bool {
	parse := func(v any) (*big.Rat, bool) {
		n, ok := v.(json.Number)
		if !ok {
			return nil, false
		}
		i, ok := new(big.Rat).SetString(n.String())
		return i, ok && i.IsInt()
	}
	a, okA := parse(value)
	b, okB := parse(limit)
	return okA && okB && a.Cmp(b) > 0
}

func confinedName(root, path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes corpus root")
	}
	return filepath.ToSlash(rel), nil
}
func findResource(c *agentvalidate.Corpus, ref contracts.Reference) (agentvalidate.LoadedResource, bool) {
	for _, d := range c.Resources {
		if d.Resource.Kind == ref.Kind && d.Resource.Envelope.Metadata.ID == ref.ID && d.Resource.Envelope.Metadata.Version == ref.Version {
			return d, true
		}
	}
	return agentvalidate.LoadedResource{}, false
}
func isAtOrOver(value, limit any) bool {
	parse := func(v any) (*big.Rat, bool) {
		n, ok := v.(json.Number)
		if !ok {
			return nil, false
		}
		i, ok := new(big.Rat).SetString(n.String())
		return i, ok && i.IsInt()
	}
	a, okA := parse(value)
	b, okB := parse(limit)
	return okA && okB && a.Cmp(b) >= 0
}
