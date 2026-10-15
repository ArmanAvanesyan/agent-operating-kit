package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentvalidate"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

func TestValidateAndReadyRequirePinnedArtifactBackedEvidence(t *testing.T) {
	root := filepath.Join(t.TempDir(), "corpus")
	copyTree(t, filepath.Join("..", "agentresolve", "testdata", "corpus"), root)
	verifier := map[string]any{"kind": "Capability", "id": "output-schema-valid", "version": "0.1.0"}
	goal := map[string]any{
		"apiVersion": "aok/v1alpha1", "kind": "GoalContract", "metadata": metadata("goal", "Goal"),
		"outcome": "deliver result", "scope": map[string]any{"repositories": []string{"."}, "writeRoots": []string{}},
		"successPredicates": []any{predicate("goal-pass", verifier)}, "invariants": []any{predicate("unchanged", verifier)},
		"limits":             map[string]any{"maxAttempts": 2, "maxReplans": 0, "timeoutSeconds": 100},
		"stopConditions":     []string{"verified-success", "cancelled", "budget-exhausted", "blocked", "no-defensible-next-step"},
		"verificationPolicy": "all-required-pass-no-required-unknown",
	}
	plan := map[string]any{
		"apiVersion": "aok/v1alpha1", "kind": "PlanDefinition", "metadata": metadata("plan", "Plan"),
		"goalRef": map[string]any{"kind": "GoalContract", "id": "goal", "version": "0.1.0"}, "strategy": "fixed-dag",
		"steps":      []any{map[string]any{"id": "step1", "description": "work", "dependsOn": []string{}, "preconditions": []any{}, "outputs": []string{"result"}, "completionChecks": []any{predicate("check1", verifier)}, "assignmentRef": map[string]any{"kind": "AgentAssignment", "id": "explore-aok-001", "version": "0.1.0"}, "workflowRef": "workflow", "ownership": []string{"."}, "recovery": "stop", "verifierRef": verifier}, map[string]any{"id": "step2", "description": "follow-up", "dependsOn": []string{"step1"}, "preconditions": []any{}, "outputs": []string{"result2"}, "completionChecks": []any{predicate("check2", verifier)}, "assignmentRef": map[string]any{"kind": "AgentAssignment", "id": "explore-aok-001", "version": "0.1.0"}, "workflowRef": "workflow", "ownership": []string{"."}, "recovery": "stop", "verifierRef": verifier}},
		"methodRefs": []string{}, "inventory": []any{}, "aggregateVerification": []any{}, "concurrencyLimit": 1, "replanning": map[string]any{"maxRevisions": 0, "preserveCompletedEvidence": true},
	}
	writeJSON(t, filepath.Join(root, "goal.json"), goal)
	planBytes := writeJSON(t, filepath.Join(root, "plan.json"), plan)
	digest := sha256.Sum256(planBytes)
	corpus, report := agentvalidate.Load(root)
	if report.Status != "valid" {
		t.Fatalf("fixture before evidence: %+v", report.Diagnostics)
	}
	contextHash, err := contextSHA256(corpus)
	if err != nil {
		t.Fatal(err)
	}
	asset, err := os.ReadFile(filepath.Join(root, "assets", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	assetHash := sha256.Sum256(asset)
	artifact := map[string]any{"path": "assets/AGENTS.md", "sha256": hex.EncodeToString(assetHash[:])}
	passed := func(id string) any {
		return map[string]any{"predicateId": id, "status": "pass", "artifactRefs": []any{artifact}}
	}
	evidence := map[string]any{
		"apiVersion": "aok/v1alpha1", "kind": "PlanningEvidence", "metadata": metadata("evidence", "Evidence"),
		"planRef": map[string]any{"kind": "PlanDefinition", "id": "plan", "version": "0.1.0"}, "planSHA256": hex.EncodeToString(digest[:]),
		"contextSHA256": contextHash,
		"evaluatedAt":   "2026-01-01T00:00:00Z", "expiresAt": "2027-01-01T00:00:00Z",
		"outcomes": []any{passed("check1"), passed("check2"), passed("goal-pass"), passed("unchanged")}, "completedSteps": []string{"step1", "step2"}, "inventoryCoverage": []string{},
		"revisionsUsed": 0, "attemptsUsed": 1, "elapsedSeconds": 5, "cancelled": false,
	}
	writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
	if got := Validate(root); got.Status != "valid" {
		t.Fatalf("Validate status=%s diagnostics=%+v", got.Status, got.Diagnostics)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	planArg, err := filepath.Rel(cwd, filepath.Join(root, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	evidenceArg, err := filepath.Rel(cwd, filepath.Join(root, "evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := Ready(planArg, evidenceArg, root, "2026-06-01T00:00:00Z")
	if got.Status != "valid" || got.GoalStatus != "verified-success" || len(got.EligibleSteps) != 0 {
		t.Fatalf("Ready report=%+v", got)
	}
	validated := Validate(root)
	if validated.ContextSHA256 != contextHash || validated.PlanHashes["plan.json"] != hex.EncodeToString(digest[:]) {
		t.Fatalf("hash fields missing: %+v", validated)
	}
	deleteOutcome(evidence, "goal-pass")
	evidence["completedSteps"] = []string{}
	writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
	got = Ready(planArg, evidenceArg, root, "2026-06-01T00:00:00Z")
	if got.Status != "valid" || got.GoalStatus != "in-progress" || len(got.EligibleSteps) != 1 || got.EligibleSteps[0] != "step1" {
		t.Fatalf("root eligibility=%+v", got)
	}
	evidence["completedSteps"] = []string{"step1"}
	writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
	got = Ready(planArg, evidenceArg, root, "2026-06-01T00:00:00Z")
	if got.Status != "valid" || len(got.EligibleSteps) != 1 || got.EligibleSteps[0] != "step2" {
		t.Fatalf("dependency eligibility=%+v", got)
	}
	setOutcomeStatus(evidence, "unchanged", "fail")
	writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
	got = Ready(planArg, evidenceArg, root, "2026-06-01T00:00:00Z")
	if got.Status != "blocked" || len(got.EligibleSteps) != 0 {
		t.Fatalf("failed invariant=%+v", got)
	}
	setOutcomeStatus(evidence, "unchanged", "pass")
	evidence["completedSteps"] = []string{"step1", "step2"}
	goal["outcome"] = "mutated context without changing plan bytes"
	writeJSON(t, filepath.Join(root, "goal.json"), goal)
	got = Ready(planArg, evidenceArg, root, "2026-06-01T00:00:00Z")
	if got.Status != "invalid" {
		t.Fatalf("changed context status=%s diagnostics=%+v", got.Status, got.Diagnostics)
	}
	goal["outcome"] = "deliver result"
	writeJSON(t, filepath.Join(root, "goal.json"), goal)
	evidence["planSHA256"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
	got = Ready(filepath.Join(root, "plan.json"), filepath.Join(root, "evidence.json"), root, "2026-06-01T00:00:00Z")
	if got.Status != "invalid" {
		t.Fatalf("stale evidence status=%s diagnostics=%+v", got.Status, got.Diagnostics)
	}
	evidence["planSHA256"] = hex.EncodeToString(digest[:])
	evidence["expiresAt"] = "2026-05-01T00:00:00Z"
	writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
	got = Ready(filepath.Join(root, "plan.json"), filepath.Join(root, "evidence.json"), root, "2026-06-01T00:00:00Z")
	if got.Status != "blocked" || got.GoalStatus == "verified-success" {
		t.Fatalf("expired evidence report=%+v", got)
	}
	evidence["expiresAt"] = "2027-01-01T00:00:00Z"
	evidence["cancelled"] = true
	writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
	got = Ready(filepath.Join(root, "plan.json"), filepath.Join(root, "evidence.json"), root, "2026-06-01T00:00:00Z")
	if got.Status != "blocked" || got.GoalStatus == "verified-success" {
		t.Fatalf("cancelled evidence report=%+v", got)
	}
}

func TestReadyBlocksUnknownOrFailedPrerequisitesAndBudgets(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mutate       func(map[string]any, map[string]any)
		wantStatus   string
		wantEligible int
	}{
		{name: "unknown outcome", mutate: func(_, e map[string]any) {
			e["outcomes"] = append(e["outcomes"].([]any), map[string]any{"predicateId": "missing", "status": "pass", "artifactRefs": []any{}})
		}, wantStatus: "invalid"},
		{name: "duplicate outcome", mutate: func(_, e map[string]any) { e["outcomes"] = append(e["outcomes"].([]any), e["outcomes"].([]any)[0]) }, wantStatus: "invalid"},
		{name: "failed invariant", mutate: func(_, e map[string]any) { setOutcomeStatus(e, "invariant", "fail") }, wantStatus: "blocked"},
		{name: "failed precondition", mutate: func(p, e map[string]any) {
			addPrecondition(p)
			deleteOutcome(e, "success")
			e["outcomes"] = append(e["outcomes"].([]any), map[string]any{"predicateId": "precondition", "status": "fail", "artifactRefs": e["outcomes"].([]any)[0].(map[string]any)["artifactRefs"]})
		}, wantStatus: "blocked"},
		{name: "unknown precondition", mutate: func(p, e map[string]any) {
			addPrecondition(p)
			deleteOutcome(e, "success")
			e["outcomes"] = append(e["outcomes"].([]any), map[string]any{"predicateId": "precondition", "status": "unknown", "artifactRefs": e["outcomes"].([]any)[0].(map[string]any)["artifactRefs"]})
		}, wantStatus: "blocked"},
		{name: "exceeded replans", mutate: func(_, e map[string]any) { e["revisionsUsed"] = 1 }, wantStatus: "invalid"},
		{name: "unverified aggregate", mutate: func(p, e map[string]any) {
			p["aggregateVerification"] = []any{predicate("aggregate", map[string]any{"kind": "Capability", "id": "output-schema-valid", "version": "0.1.0"})}
			e["completedSteps"] = []string{"root", "second"}
		}, wantStatus: "blocked"},
		{name: "exhausted attempts", mutate: func(_, e map[string]any) { e["attemptsUsed"] = json.Number("2"); deleteOutcome(e, "success") }, wantStatus: "blocked"},
		{name: "exact exponent integer", mutate: func(_, e map[string]any) { e["attemptsUsed"] = json.Number("1e0"); deleteOutcome(e, "success") }, wantStatus: "valid", wantEligible: 1},
		{name: "inventory omitted", mutate: func(p, _ map[string]any) {
			p["inventory"] = []any{map[string]any{"id": "component", "stepRefs": []string{"root"}, "disposition": "included", "rationale": "required"}}
		}, wantStatus: "blocked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, plan, evidence := newReadyFixture(t)
			tc.mutate(plan, evidence)
			writeJSON(t, filepath.Join(root, "plan.json"), plan)
			refreshTestEvidence(t, root, evidence)
			writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
			got := Ready(filepath.Join(root, "plan.json"), filepath.Join(root, "evidence.json"), root, "2026-06-01T00:00:00Z")
			if got.Status != tc.wantStatus || len(got.EligibleSteps) != tc.wantEligible {
				t.Fatalf("report=%+v", got)
			}
		})
	}
}

func TestValidateRejectsMissingAndCyclicDependencies(t *testing.T) {
	for _, tc := range []struct {
		name         string
		deps1, deps2 []string
		code         string
	}{
		{name: "missing", deps1: []string{"unknown"}, deps2: []string{}, code: "planning.dependency.missing"},
		{name: "cycle", deps1: []string{"second"}, deps2: []string{"root"}, code: "planning.dependency.cycle"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, plan, evidence := newReadyFixture(t)
			plan["steps"].([]any)[0].(map[string]any)["dependsOn"] = tc.deps1
			plan["steps"].([]any)[1].(map[string]any)["dependsOn"] = tc.deps2
			writeJSON(t, filepath.Join(root, "plan.json"), plan)
			refreshTestEvidence(t, root, evidence)
			writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
			got := Validate(root)
			if got.Status != "invalid" || !hasCode(got.Diagnostics, tc.code) {
				t.Fatalf("report=%+v", got)
			}
		})
	}
}

func newReadyFixture(t *testing.T) (string, map[string]any, map[string]any) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "corpus")
	copyTree(t, filepath.Join("..", "agentresolve", "testdata", "corpus"), root)
	verifier := map[string]any{"kind": "Capability", "id": "output-schema-valid", "version": "0.1.0"}
	goal := map[string]any{"apiVersion": "aok/v1alpha1", "kind": "GoalContract", "metadata": metadata("ready-goal", "Goal"), "outcome": "outcome", "scope": map[string]any{"repositories": []string{"."}, "writeRoots": []string{}}, "successPredicates": []any{predicate("success", verifier)}, "invariants": []any{predicate("invariant", verifier)}, "limits": map[string]any{"maxAttempts": 2, "maxReplans": 0, "timeoutSeconds": 100}, "stopConditions": []string{"verified-success", "cancelled", "budget-exhausted", "blocked", "no-defensible-next-step"}, "verificationPolicy": "all-required-pass-no-required-unknown"}
	step := func(id string, deps []string, check string) map[string]any {
		return map[string]any{"id": id, "description": "work", "dependsOn": deps, "preconditions": []any{}, "outputs": []string{"output"}, "completionChecks": []any{predicate(check, verifier)}, "assignmentRef": map[string]any{"kind": "AgentAssignment", "id": "explore-aok-001", "version": "0.1.0"}, "workflowRef": "workflow", "ownership": []string{"."}, "recovery": "stop", "verifierRef": verifier}
	}
	plan := map[string]any{"apiVersion": "aok/v1alpha1", "kind": "PlanDefinition", "metadata": metadata("ready-plan", "Plan"), "goalRef": map[string]any{"kind": "GoalContract", "id": "ready-goal", "version": "0.1.0"}, "strategy": "fixed-dag", "steps": []any{step("root", []string{}, "root-check"), step("second", []string{"root"}, "second-check")}, "methodRefs": []string{}, "inventory": []any{}, "aggregateVerification": []any{}, "concurrencyLimit": 1, "replanning": map[string]any{"maxRevisions": 0, "preserveCompletedEvidence": true}}
	writeJSON(t, filepath.Join(root, "goal.json"), goal)
	writeJSON(t, filepath.Join(root, "plan.json"), plan)
	asset, err := os.ReadFile(filepath.Join(root, "assets", "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(asset)
	artifact := map[string]any{"path": "assets/AGENTS.md", "sha256": hex.EncodeToString(sum[:])}
	pass := func(id string) any {
		return map[string]any{"predicateId": id, "status": "pass", "artifactRefs": []any{artifact}}
	}
	evidence := map[string]any{"apiVersion": "aok/v1alpha1", "kind": "PlanningEvidence", "metadata": metadata("ready-evidence", "Evidence"), "planRef": map[string]any{"kind": "PlanDefinition", "id": "ready-plan", "version": "0.1.0"}, "evaluatedAt": "2026-01-01T00:00:00Z", "expiresAt": "2027-01-01T00:00:00Z", "outcomes": []any{pass("success"), pass("invariant"), pass("root-check"), pass("second-check")}, "completedSteps": []string{}, "inventoryCoverage": []string{}, "revisionsUsed": 0, "attemptsUsed": json.Number("1"), "elapsedSeconds": 1, "cancelled": false}
	refreshTestEvidence(t, root, evidence)
	writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
	return root, plan, evidence
}
func refreshTestEvidence(t *testing.T, root string, evidence map[string]any) {
	t.Helper()
	planBytes, err := os.ReadFile(filepath.Join(root, "plan.json"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(planBytes)
	evidence["planSHA256"] = hex.EncodeToString(sum[:])
	corpus, report := agentvalidate.Load(root)
	if report.Status != "valid" {
		t.Fatalf("fixture invalid before evidence update: %+v", report.Diagnostics)
	}
	contextHash, err := contextSHA256(corpus)
	if err != nil {
		t.Fatal(err)
	}
	evidence["contextSHA256"] = contextHash
}
func hasCode(ds []contracts.Diagnostic, code string) bool {
	for _, d := range ds {
		if d.Code == code {
			return true
		}
	}
	return false
}

func predicate(id string, verifier map[string]any) map[string]any {
	return map[string]any{"id": id, "description": "verified condition", "verifierRef": verifier, "required": true}
}
func deleteOutcome(evidence map[string]any, id string) {
	out := []any{}
	for _, v := range evidence["outcomes"].([]any) {
		if v.(map[string]any)["predicateId"] != id {
			out = append(out, v)
		}
	}
	evidence["outcomes"] = out
}
func addPrecondition(plan map[string]any) {
	step := plan["steps"].([]any)[0].(map[string]any)
	verifier := map[string]any{"kind": "Capability", "id": "output-schema-valid", "version": "0.1.0"}
	step["preconditions"] = []any{predicate("precondition", verifier)}
}
func setOutcomeStatus(evidence map[string]any, id, status string) {
	for _, v := range evidence["outcomes"].([]any) {
		o := v.(map[string]any)
		if o["predicateId"] == id {
			o["status"] = status
		}
	}
}
func metadata(id, name string) map[string]any {
	return map[string]any{"id": id, "version": "0.1.0", "name": name, "description": "planning test fixture"}
}
func writeJSON(t *testing.T, path string, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return data
}
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, e := filepath.Rel(src, path)
		if e != nil {
			return e
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
}
