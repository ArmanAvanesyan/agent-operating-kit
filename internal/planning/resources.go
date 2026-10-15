package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentvalidate"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/decision"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/experiment"
)

func validatePlanningEvidence(r *Report, c *agentvalidate.Corpus, doc agentvalidate.LoadedResource, byKey map[docKey]agentvalidate.LoadedResource) {
	ref := refValue(doc.Object["planRef"])
	plan, ok := resolveRef(r, byKey, doc.Path, "planRef", ref, "PlanDefinition")
	if !ok {
		return
	}
	digest := sha256.Sum256(plan.Resource.Document)
	if stringValue(doc.Object["planSHA256"]) != hex.EncodeToString(digest[:]) {
		r.add("planning.evidence.plan-hash", doc.Path, "planSHA256", "error", "evidence plan hash does not match the exact referenced plan bytes")
	}
	contextHash, err := contextSHA256(c)
	if err != nil {
		r.add("planning.evidence.context-hash", doc.Path, "contextSHA256", "error", err.Error())
	} else if stringValue(doc.Object["contextSHA256"]) != contextHash {
		r.add("planning.evidence.context-hash", doc.Path, "contextSHA256", "error", "evidence context hash does not match the canonical resource snapshot")
	}
	eval, evalErr := time.Parse(time.RFC3339, stringValue(doc.Object["evaluatedAt"]))
	expires, expErr := time.Parse(time.RFC3339, stringValue(doc.Object["expiresAt"]))
	if evalErr != nil || expErr != nil || !expires.After(eval) {
		r.add("planning.evidence.time", doc.Path, "evaluatedAt", "error", "evidence timestamps must be RFC3339 with expiresAt after evaluatedAt")
	}
	known := map[string]bool{}
	for _, raw := range array(plan.Object["steps"]) {
		s := object(raw)
		for _, field := range []string{"preconditions", "completionChecks"} {
			for _, p := range array(s[field]) {
				known[stringValue(object(p)["id"])] = true
			}
		}
	}
	for _, p := range array(plan.Object["aggregateVerification"]) {
		known[stringValue(object(p)["id"])] = true
	}
	goal, goalFound := byKey[docKey{"GoalContract", refValue(plan.Object["goalRef"]).ID, refValue(plan.Object["goalRef"]).Version}]
	if goalFound {
		for _, field := range []string{"successPredicates", "invariants"} {
			for _, p := range array(goal.Object[field]) {
				known[stringValue(object(p)["id"])] = true
			}
		}
	}
	seen := map[string]bool{}
	for i, raw := range array(doc.Object["outcomes"]) {
		id := stringValue(object(raw)["predicateId"])
		if seen[id] {
			r.add("planning.evidence.duplicate-outcome", doc.Path, fmt.Sprintf("outcomes[%d].predicateId", i), "error", "predicate outcomes must be unique")
		}
		seen[id] = true
		if !known[id] {
			r.add("planning.evidence.unknown-outcome", doc.Path, fmt.Sprintf("outcomes[%d].predicateId", i), "error", "outcome does not name a declared goal predicate or plan check")
		}
	}
	stepIDs := map[string]bool{}
	for _, s := range array(plan.Object["steps"]) {
		stepIDs[stringValue(object(s)["id"])] = true
	}
	for i, v := range array(doc.Object["completedSteps"]) {
		if !stepIDs[stringValue(v)] {
			r.add("planning.evidence.unknown-step", doc.Path, fmt.Sprintf("completedSteps[%d]", i), "error", "completed step is not declared by the plan")
		}
	}
	items := map[string]bool{}
	for _, item := range array(plan.Object["inventory"]) {
		items[stringValue(object(item)["id"])] = true
	}
	for i, v := range array(doc.Object["inventoryCoverage"]) {
		if !items[stringValue(v)] {
			r.add("planning.evidence.unknown-inventory", doc.Path, fmt.Sprintf("inventoryCoverage[%d]", i), "error", "inventory coverage item is not declared by the plan")
		}
	}
	checkArtifactRefs(r, c, doc, array(doc.Object["outcomes"]))
}

type contextSource struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// contextSHA256 hashes AOK-PLANNING-CONTEXT\x00v1\x00 followed by compact JSON
// for the path-sorted [{"path":...,"sha256":...}] canonical-resource manifest;
// PlanningEvidence resources are omitted to avoid circular hashes.
func contextSHA256(c *agentvalidate.Corpus) (string, error) {
	sources := []contextSource{}
	for _, doc := range c.Resources {
		if doc.Resource.Kind == "PlanningEvidence" {
			continue
		}
		sum := sha256.Sum256(doc.Resource.Document)
		sources = append(sources, contextSource{Path: filepath.ToSlash(doc.Path), SHA256: hex.EncodeToString(sum[:])})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	data, err := json.Marshal(sources)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, _ = h.Write([]byte("AOK-PLANNING-CONTEXT\x00v1\x00"))
	_, _ = h.Write(data)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func checkArtifactRefs(r *Report, c *agentvalidate.Corpus, owner agentvalidate.LoadedResource, outcomes []any) {
	for i, raw := range outcomes {
		for j, value := range array(object(raw)["artifactRefs"]) {
			artifact := object(value)
			field := fmt.Sprintf("outcomes[%d].artifactRefs[%d]", i, j)
			path, err := c.ResolveFile(owner.Path, stringValue(artifact["path"]))
			if err != nil {
				r.add("planning.artifact.path", owner.Path, field+".path", "error", err.Error())
				continue
			}
			contents, err := readLocal(path)
			if err != nil {
				r.add("planning.artifact.read", owner.Path, field+".path", "error", err.Error())
				continue
			}
			digest := sha256.Sum256(contents)
			if stringValue(artifact["sha256"]) != hex.EncodeToString(digest[:]) {
				r.add("planning.artifact.hash", owner.Path, field+".sha256", "error", "artifact hash does not match file contents")
			}
		}
	}
}

func validateDecisionContract(r *Report, c *agentvalidate.Corpus, doc agentvalidate.LoadedResource) {
	path, err := c.ResolveFile(doc.Path, stringValue(doc.Object["inputSchema"]))
	if err != nil {
		r.add("planning.decision.input-schema", doc.Path, "inputSchema", "error", err.Error())
		return
	}
	contents, err := readLocal(path)
	if err != nil {
		r.add("planning.decision.input-schema", doc.Path, "inputSchema", "error", err.Error())
		return
	}
	if _, err = contracts.DecodeJSON(contents); err != nil {
		r.add("planning.decision.input-schema", doc.Path, "inputSchema", "error", err.Error())
	}
	if err = agentvalidate.CheckSchemaFile(c.Root, doc.Path, stringValue(doc.Object["inputSchema"])); err != nil {
		r.add("planning.decision.input-schema", doc.Path, "inputSchema", "error", err.Error())
	}
	appendOwned(r, doc.Path, decision.CheckContract(doc.Object))
	uncertainty := object(doc.Object["uncertainty"])
	if calibration := uncertainty["calibrationRef"]; calibration != nil && stringValue(calibration) != "" {
		checkLocalReference(r, c, doc, "uncertainty.calibrationRef", stringValue(calibration))
	}
}

func validateDecisionResult(r *Report, c *agentvalidate.Corpus, doc agentvalidate.LoadedResource, byKey map[docKey]agentvalidate.LoadedResource) {
	contractDoc, ok := resolveRef(r, byKey, doc.Path, "decisionRef", refValue(doc.Object["decisionRef"]), "DecisionContract")
	if ok {
		appendOwned(r, doc.Path, decision.Check(contractDoc.Object, doc.Object))
	}
	for i, value := range array(doc.Object["evidence"]) {
		checkLocalReference(r, c, doc, fmt.Sprintf("evidence[%d]", i), stringValue(value))
	}
}

func validateExperiment(r *Report, c *agentvalidate.Corpus, doc agentvalidate.LoadedResource, byKey map[docKey]agentvalidate.LoadedResource) {
	appendOwned(r, doc.Path, experiment.Check(doc.Object))
	resolveRef(r, byKey, doc.Path, "agentRef", refValue(doc.Object["agentRef"]), "Agent")
	for i, value := range array(doc.Object["profileRefs"]) {
		resolveRef(r, byKey, doc.Path, fmt.Sprintf("profileRefs[%d]", i), refValue(value), "ExecutionProfile")
	}
	resolveRef(r, byKey, doc.Path, "evaluator.ref", refValue(object(doc.Object["evaluator"])["ref"]), "EvaluationContract")
	for i, result := range array(doc.Object["results"]) {
		for j, evidence := range array(object(result)["evidence"]) {
			checkLocalReference(r, c, doc, fmt.Sprintf("results[%d].evidence[%d]", i, j), stringValue(evidence))
		}
	}
}

func checkLocalReference(r *Report, c *agentvalidate.Corpus, doc agentvalidate.LoadedResource, field, ref string) {
	if _, err := c.ResolveFile(doc.Path, ref); err != nil {
		r.add("planning.file.reference", doc.Path, field, "error", err.Error())
	}
}

func readLocal(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, fmt.Errorf("referenced file must be a regular non-symlink file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("referenced file changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, contracts.MaxDocumentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > contracts.MaxDocumentBytes {
		return nil, fmt.Errorf("file exceeds 4 MiB")
	}
	return data, nil
}

func appendOwned(r *Report, source string, diagnostics []contracts.Diagnostic) {
	for _, d := range diagnostics {
		d.Source = source
		r.Diagnostics = append(r.Diagnostics, d)
	}
}
