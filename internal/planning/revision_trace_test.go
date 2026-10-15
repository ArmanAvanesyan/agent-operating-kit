package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentvalidate"
)

func TestRevisionChainPinsExactPlansPredecessorOrdinalGoalAndBudget(t *testing.T) {
	t.Run("valid chain", func(t *testing.T) {
		root := rtCorpus(t, 2)
		rtAddPlan(t, root, "plan", "0.1.0", "goal", "0.1.0")
		rtAddPlan(t, root, "plan", "0.2.0", "goal", "0.1.0")
		rtAddPlan(t, root, "plan", "0.3.0", "goal", "0.1.0")
		rtAddRevision(t, root, "rev1", "0.1.0", "plan", "0.1.0", "plan", "0.2.0", nil, 1)
		rtAddRevision(t, root, "rev2", "0.1.0", "plan", "0.2.0", "plan", "0.3.0", map[string]any{"kind": "PlanRevision", "id": "rev1", "version": "0.1.0"}, 2)
		rtRequireValid(t, root)
	})
	t.Run("wrong predecessor exact pin", func(t *testing.T) {
		root := rtValidTwoRevisionChain(t, 2)
		rtAddRevision(t, root, "rev2", "0.1.0", "plan", "0.2.0", "plan", "0.3.0", map[string]any{"kind": "PlanRevision", "id": "rev1", "version": "9.9.9"}, 2)
		rtRequireDiagnostic(t, root, "planning.revision.predecessor")
	})
	t.Run("ordinal must follow predecessor", func(t *testing.T) {
		root := rtValidTwoRevisionChain(t, 2)
		rtAddRevision(t, root, "rev2", "0.1.0", "plan", "0.2.0", "plan", "0.3.0", map[string]any{"kind": "PlanRevision", "id": "rev1", "version": "0.1.0"}, 3)
		rtRequireDiagnostic(t, root, "planning.revision.ordinal")
	})
	t.Run("before plan must equal predecessor after plan", func(t *testing.T) {
		root := rtValidTwoRevisionChain(t, 2)
		rtAddRevision(t, root, "rev2", "0.1.0", "plan", "0.1.0", "plan", "0.3.0", map[string]any{"kind": "PlanRevision", "id": "rev1", "version": "0.1.0"}, 2)
		rtRequireDiagnostic(t, root, "planning.revision.chain")
	})
	t.Run("after plan must change exact version", func(t *testing.T) {
		root := rtCorpus(t, 2)
		rtAddPlan(t, root, "plan", "0.1.0", "goal", "0.1.0")
		rtAddRevision(t, root, "rev1", "0.1.0", "plan", "0.1.0", "plan", "0.1.0", nil, 1)
		rtRequireDiagnostic(t, root, "planning.revision.plan")
	})
	t.Run("after plan must retain goal pin", func(t *testing.T) {
		root := rtCorpus(t, 2)
		rtAddGoal(t, root, "goal2", 2)
		rtAddPlan(t, root, "plan", "0.1.0", "goal", "0.1.0")
		rtAddPlan(t, root, "plan", "0.2.0", "goal2", "0.1.0")
		rtAddRevision(t, root, "rev1", "0.1.0", "plan", "0.1.0", "plan", "0.2.0", nil, 1)
		rtRequireDiagnostic(t, root, "planning.revision.goal")
	})
	t.Run("revision count respects goal replan limit", func(t *testing.T) {
		root := rtValidTwoRevisionChain(t, 1)
		rtRequireDiagnostic(t, root, "planning.revision.budget")
	})
}

func TestTraceabilityRequiresPredicateStepAssignmentOutputAndEvidence(t *testing.T) {
	t.Run("valid verified mapping", func(t *testing.T) {
		root := rtTraceCorpus(t, "verified", []string{"goal-pass", "invariant-pass"}, []string{"run-1"}, "pass", true)
		rtRequireValid(t, root)
	})
	t.Run("verified coverage must link required predicates", func(t *testing.T) {
		root := rtTraceCorpus(t, "verified", []string{"goal-pass"}, []string{"run-1"}, "pass", true)
		rtRequireDiagnostic(t, root, "planning.traceability.coverage")
	})
	t.Run("linked step must exist", func(t *testing.T) {
		root := rtTraceCorpus(t, "planned", []string{"goal-pass", "invariant-pass"}, nil, "pass", true)
		rtRewriteTrace(t, root, func(trace map[string]any) { trace["links"].([]any)[0].(map[string]any)["stepRef"] = "missing-step" })
		rtRequireDiagnostic(t, root, "planning.traceability.step")
	})
	t.Run("assignment must match step", func(t *testing.T) {
		root := rtTraceCorpus(t, "planned", []string{"goal-pass", "invariant-pass"}, nil, "pass", true)
		rtRewriteTrace(t, root, func(trace map[string]any) {
			trace["links"].([]any)[0].(map[string]any)["assignmentRef"] = map[string]any{"kind": "AgentAssignment", "id": "unknown-assignment", "version": "0.1.0"}
		})
		rtRequireDiagnostic(t, root, "planning.traceability.assignment")
	})
	t.Run("output must match linked assignment", func(t *testing.T) {
		root := rtTraceCorpus(t, "planned", []string{"goal-pass", "invariant-pass"}, nil, "pass", true)
		rtRewriteTrace(t, root, func(trace map[string]any) {
			trace["links"].([]any)[0].(map[string]any)["outputContractRef"] = map[string]any{"kind": "OutputContract", "id": "other-output", "version": "0.1.0"}
		})
		rtRequireDiagnostic(t, root, "planning.traceability.output")
	})
	t.Run("verified coverage needs run refs", func(t *testing.T) {
		root := rtTraceCorpus(t, "verified", []string{"goal-pass", "invariant-pass"}, nil, "pass", true)
		rtRequireDiagnostic(t, root, "planning.traceability.run")
	})
	t.Run("failed evidence cannot be verified", func(t *testing.T) {
		root := rtTraceCorpus(t, "verified", []string{"goal-pass", "invariant-pass"}, []string{"run-1"}, "fail", true)
		rtRequireDiagnostic(t, root, "planning.traceability.outcome")
	})
	t.Run("evidence without artifact refs cannot be verified", func(t *testing.T) {
		root := rtTraceCorpus(t, "verified", []string{"goal-pass", "invariant-pass"}, []string{"run-1"}, "pass", true)
		rtRewriteEvidence(t, root, func(evidence map[string]any) {
			for _, raw := range evidence["outcomes"].([]any) {
				outcome := raw.(map[string]any)
				if outcome["predicateId"] == "goal-pass" {
					outcome["artifactRefs"] = []any{}
				}
			}
		})
		rtRequireDiagnostic(t, root, "planning.traceability.outcome")
	})
	t.Run("verified coverage requires evidence artifact", func(t *testing.T) {
		root := rtTraceCorpus(t, "verified", []string{"goal-pass", "invariant-pass"}, []string{"run-1"}, "pass", false)
		rtRequireDiagnostic(t, root, "planning.traceability.evidence")
	})
}

func rtCorpus(t *testing.T, maxReplans int) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "corpus")
	copyTree(t, filepath.Join("..", "agentresolve", "testdata", "corpus"), root)
	rtAddGoal(t, root, "goal", maxReplans)
	return root
}

func rtAddGoal(t *testing.T, root, id string, maxReplans int) {
	t.Helper()
	verifier := map[string]any{"kind": "Capability", "id": "output-schema-valid", "version": "0.1.0"}
	goal := map[string]any{"apiVersion": "aok/v1alpha1", "kind": "GoalContract", "metadata": metadata(id, id), "outcome": "deliver result", "scope": map[string]any{"repositories": []string{"."}, "writeRoots": []string{}}, "successPredicates": []any{predicate("goal-pass", verifier)}, "invariants": []any{predicate("invariant-pass", verifier)}, "limits": map[string]any{"maxAttempts": 2, "maxReplans": maxReplans, "timeoutSeconds": 100}, "stopConditions": []string{"verified-success", "cancelled", "budget-exhausted", "blocked", "no-defensible-next-step"}, "verificationPolicy": "all-required-pass-no-required-unknown"}
	writeJSON(t, filepath.Join(root, id+".json"), goal)
}

func rtPlan(t *testing.T, root, id, version, goalID, goalVersion string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, goalID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var goal map[string]any
	if err := json.Unmarshal(b, &goal); err != nil {
		t.Fatal(err)
	}
	max := goal["limits"].(map[string]any)["maxReplans"]

	verifier := map[string]any{"kind": "Capability", "id": "output-schema-valid", "version": "0.1.0"}
	return map[string]any{"apiVersion": "aok/v1alpha1", "kind": "PlanDefinition", "metadata": map[string]any{"id": id, "name": "plan", "version": version, "description": "revision trace test plan"}, "goalRef": map[string]any{"kind": "GoalContract", "id": goalID, "version": goalVersion}, "strategy": "fixed-dag", "steps": []any{map[string]any{"id": "step1", "description": "work " + version, "dependsOn": []string{}, "preconditions": []any{}, "outputs": []string{"result"}, "completionChecks": []any{predicate("check1", verifier)}, "assignmentRef": map[string]any{"kind": "AgentAssignment", "id": "explore-aok-001", "version": "0.1.0"}, "verifierRef": verifier, "workflowRef": "workflow", "ownership": []string{"."}, "recovery": "stop"}}, "methodRefs": []string{}, "inventory": []any{}, "aggregateVerification": []any{}, "concurrencyLimit": 1, "replanning": map[string]any{"maxRevisions": max, "preserveCompletedEvidence": true}}
}

func rtAddPlan(t *testing.T, root, id, version, goalID, goalVersion string) {
	t.Helper()
	writeJSON(t, filepath.Join(root, "plan-"+version+".json"), rtPlan(t, root, id, version, goalID, goalVersion))
}

func rtAddRevision(t *testing.T, root, id, version, beforeID, beforeVersion, afterID, afterVersion string, predecessor any, ordinal int) {
	t.Helper()
	revision := map[string]any{"apiVersion": "aok/v1alpha1", "kind": "PlanRevision", "metadata": map[string]any{"id": id, "name": id, "version": version, "description": "revision trace test"}, "beforePlan": map[string]any{"kind": "PlanDefinition", "id": beforeID, "version": beforeVersion}, "afterPlan": map[string]any{"kind": "PlanDefinition", "id": afterID, "version": afterVersion}, "predecessor": predecessor, "ordinal": ordinal, "trigger": "new evidence", "evidence": []string{}, "changes": []any{map[string]any{"stepId": "step1", "operation": "modify", "reason": "updated scope"}}, "preservedEvidence": []string{}, "invalidatedEvidence": []string{}, "authorizationRef": nil, "state": "proposed"}
	writeJSON(t, filepath.Join(root, "revision-"+id+"-"+version+".json"), revision)
}

func rtValidTwoRevisionChain(t *testing.T, maxReplans int) string {
	t.Helper()
	root := rtCorpus(t, maxReplans)
	rtAddPlan(t, root, "plan", "0.1.0", "goal", "0.1.0")
	rtAddPlan(t, root, "plan", "0.2.0", "goal", "0.1.0")
	rtAddPlan(t, root, "plan", "0.3.0", "goal", "0.1.0")
	rtAddRevision(t, root, "rev1", "0.1.0", "plan", "0.1.0", "plan", "0.2.0", nil, 1)
	rtAddRevision(t, root, "rev2", "0.1.0", "plan", "0.2.0", "plan", "0.3.0", map[string]any{"kind": "PlanRevision", "id": "rev1", "version": "0.1.0"}, 2)
	return root
}

func rtTraceCorpus(t *testing.T, coverage string, linked []string, runs []string, outcomeStatus string, includeEvidence bool) string {
	t.Helper()
	if runs == nil {
		runs = []string{}
	}
	root := rtCorpus(t, 0)
	planBytes := writeJSON(t, filepath.Join(root, "plan-0.1.0.json"), rtPlan(t, root, "plan", "0.1.0", "goal", "0.1.0"))
	planHash := sha256.Sum256(planBytes)
	trace := map[string]any{"apiVersion": "aok/v1alpha1", "kind": "SDLCTraceability", "metadata": metadata("trace", "Traceability"), "goalRef": map[string]any{"kind": "GoalContract", "id": "goal", "version": "0.1.0"}, "planRef": map[string]any{"kind": "PlanDefinition", "id": "plan", "version": "0.1.0"}, "links": []any{map[string]any{"requirement": "deliver verified result", "predicateRefs": linked, "workflowType": "repository-onboarding", "stepRef": "step1", "assignmentRef": map[string]any{"kind": "AgentAssignment", "id": "explore-aok-001", "version": "0.1.0"}, "outputContractRef": map[string]any{"kind": "OutputContract", "id": "repository-exploration-report", "version": "0.1.0"}, "verificationRef": map[string]any{"kind": "EvaluationContract", "id": "exploration-evaluation", "version": "0.1.0"}, "runRefs": runs}}, "coverage": coverage, "evidence": []string{}}
	if includeEvidence {
		trace["evidence"] = []string{"evidence.json"}
	}
	writeJSON(t, filepath.Join(root, "trace.json"), trace)
	if includeEvidence {
		artifactBytes, err := os.ReadFile(filepath.Join(root, "assets", "AGENTS.md"))
		if err != nil {
			t.Fatal(err)
		}
		artifactHash := sha256.Sum256(artifactBytes)
		artifact := map[string]any{"path": "assets/AGENTS.md", "sha256": hex.EncodeToString(artifactHash[:])}
		outcomes := []any{}
		for _, id := range []string{"goal-pass", "invariant-pass", "check1"} {
			outcomes = append(outcomes, map[string]any{"predicateId": id, "status": outcomeStatus, "artifactRefs": []any{artifact}})
		}
		evidence := map[string]any{"apiVersion": "aok/v1alpha1", "kind": "PlanningEvidence", "metadata": metadata("evidence", "Evidence"), "planRef": map[string]any{"kind": "PlanDefinition", "id": "plan", "version": "0.1.0"}, "planSHA256": hex.EncodeToString(planHash[:]), "contextSHA256": strings.Repeat("0", 64), "evaluatedAt": "2026-01-01T00:00:00Z", "expiresAt": "2027-01-01T00:00:00Z", "outcomes": outcomes, "completedSteps": []string{"step1"}, "inventoryCoverage": []string{}, "revisionsUsed": 0, "attemptsUsed": 1, "elapsedSeconds": 5, "cancelled": false}
		writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
		corpus, report := agentvalidate.Load(root)
		if report.Status != "valid" {
			t.Fatalf("fixture before context hash: %+v", report.Diagnostics)
		}
		hash, err := contextSHA256(corpus)
		if err != nil {
			t.Fatal(err)
		}
		evidence["contextSHA256"] = hash
		writeJSON(t, filepath.Join(root, "evidence.json"), evidence)
	}
	return root
}

func rtRewriteTrace(t *testing.T, root string, mutate func(map[string]any)) {
	t.Helper()
	path := filepath.Join(root, "trace.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var trace map[string]any
	if err := json.Unmarshal(b, &trace); err != nil {
		t.Fatal(err)
	}
	mutate(trace)
	writeJSON(t, path, trace)
	// Evidence is bound to the canonical context; refresh it so the expected
	// diagnostic comes from the traceability condition under test.
	evidencePath := filepath.Join(root, "evidence.json")
	if b, err = os.ReadFile(evidencePath); err == nil {
		var evidence map[string]any
		if json.Unmarshal(b, &evidence) == nil {
			corpus, report := agentvalidate.Load(root)
			if report.Status == "valid" {
				if hash, err := contextSHA256(corpus); err == nil {
					evidence["contextSHA256"] = hash
					writeJSON(t, evidencePath, evidence)
				}
			}
		}
	}
}

func rtRewriteEvidence(t *testing.T, root string, mutate func(map[string]any)) {
	t.Helper()
	path := filepath.Join(root, "evidence.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var evidence map[string]any
	if err := json.Unmarshal(b, &evidence); err != nil {
		t.Fatal(err)
	}
	mutate(evidence)
	writeJSON(t, path, evidence)
}

func rtRequireValid(t *testing.T, root string) {
	t.Helper()
	got := Validate(root)
	if got.Status != "valid" {
		t.Fatalf("expected valid corpus, status=%s diagnostics=%+v", got.Status, got.Diagnostics)
	}
}

func rtRequireDiagnostic(t *testing.T, root, code string) {
	t.Helper()
	got := Validate(root)
	if got.Status == "valid" {
		t.Fatalf("expected %s diagnostic", code)
	}
	for _, diagnostic := range got.Diagnostics {
		if diagnostic.Code == code {
			return
		}
	}
	var codes []string
	for _, diagnostic := range got.Diagnostics {
		codes = append(codes, diagnostic.Code)
	}
	t.Fatalf("missing %s diagnostic; got %s", code, strings.Join(codes, ", "))
}
