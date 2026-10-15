package planning

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRevisionChangesMatchBeforeAndAfterStepSets(t *testing.T) {
	t.Run("valid add modify and remove", func(t *testing.T) {
		root := rcCorpus(t, 3)
		rcWritePlans(t, root, []string{"keep", "remove"}, []string{"keep", "new"}, 3)
		rcWriteRevision(t, root, []any{
			map[string]any{"stepId": "keep", "operation": "modify", "reason": "update existing step"},
			map[string]any{"stepId": "new", "operation": "add", "reason": "introduce step"},
			map[string]any{"stepId": "remove", "operation": "remove", "reason": "retire step"},
		}, []string{}, []string{})
		if got := Validate(root); got.Status != "valid" {
			t.Fatalf("report=%+v", got)
		}
	})
	for _, tc := range []struct {
		name          string
		before, after []string
		change        map[string]any
	}{
		{name: "add already present", before: []string{"same"}, after: []string{"same"}, change: map[string]any{"stepId": "same", "operation": "add", "reason": "bad add"}},
		{name: "remove still present", before: []string{"same"}, after: []string{"same"}, change: map[string]any{"stepId": "same", "operation": "remove", "reason": "bad remove"}},
		{name: "modify missing", before: []string{"old"}, after: []string{"new"}, change: map[string]any{"stepId": "missing", "operation": "modify", "reason": "bad modify"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := rcCorpus(t, 3)
			rcWritePlans(t, root, tc.before, tc.after, 3)
			rcWriteRevision(t, root, []any{tc.change}, []string{}, []string{})
			got := Validate(root)
			if got.Status != "invalid" || !hasCode(got.Diagnostics, "planning.revision.change") {
				t.Fatalf("report=%+v", got)
			}
		})
	}
}

func TestRevisionEvidenceListsDoNotOverlapAndPlanRevisionLimitApplies(t *testing.T) {
	t.Run("overlapping evidence", func(t *testing.T) {
		root := rcCorpus(t, 3)
		rcWritePlans(t, root, []string{"step"}, []string{"step"}, 3)
		rcWriteRevision(t, root, []any{map[string]any{"stepId": "step", "operation": "modify", "reason": "edit"}}, []string{"artifact-a"}, []string{"artifact-a"})
		got := Validate(root)
		if got.Status != "invalid" || !hasCode(got.Diagnostics, "planning.revision.evidence-overlap") {
			t.Fatalf("report=%+v", got)
		}
	})
	t.Run("per plan max revisions", func(t *testing.T) {
		root := rcCorpus(t, 3)
		rcWritePlans(t, root, []string{"step"}, []string{"step"}, 0)
		rcWriteRevision(t, root, []any{map[string]any{"stepId": "step", "operation": "modify", "reason": "edit"}}, []string{}, []string{})
		got := Validate(root)
		if got.Status != "invalid" || !hasCode(got.Diagnostics, "planning.revision.plan-budget") {
			t.Fatalf("report=%+v", got)
		}
	})
	t.Run("plan replan ceiling cannot exceed goal", func(t *testing.T) {
		root := rcCorpus(t, 1)
		rcWritePlans(t, root, []string{"step"}, []string{"step"}, 2)
		got := Validate(root)
		if got.Status != "invalid" || !hasCode(got.Diagnostics, "planning.replanning.limit") {
			t.Fatalf("report=%+v", got)
		}
	})
}

func rcCorpus(t *testing.T, maxReplans int) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "corpus")
	copyTree(t, filepath.Join("..", "agentresolve", "testdata", "corpus"), root)
	verifier := map[string]any{"kind": "Capability", "id": "output-schema-valid", "version": "0.1.0"}
	goal := map[string]any{"apiVersion": "aok/v1alpha1", "kind": "GoalContract", "metadata": metadata("revision-goal", "Revision goal"), "outcome": "deliver result", "scope": map[string]any{"repositories": []string{"."}, "writeRoots": []string{}}, "successPredicates": []any{predicate("goal-pass", verifier)}, "invariants": []any{predicate("safe", verifier)}, "limits": map[string]any{"maxAttempts": 2, "maxReplans": maxReplans, "timeoutSeconds": 100}, "stopConditions": []string{"verified-success", "cancelled", "budget-exhausted", "blocked", "no-defensible-next-step"}, "verificationPolicy": "all-required-pass-no-required-unknown"}
	writeJSON(t, filepath.Join(root, "revision-goal.json"), goal)
	return root
}
func rcStep(id string) map[string]any {
	verifier := map[string]any{"kind": "Capability", "id": "output-schema-valid", "version": "0.1.0"}
	return map[string]any{"id": id, "description": "revision test step", "dependsOn": []string{}, "preconditions": []any{}, "outputs": []string{"result"}, "completionChecks": []any{predicate("check-"+id, verifier)}, "assignmentRef": map[string]any{"kind": "AgentAssignment", "id": "explore-aok-001", "version": "0.1.0"}, "workflowRef": "workflow", "ownership": []string{"."}, "recovery": "stop", "verifierRef": verifier}
}
func rcWritePlans(t *testing.T, root string, before, after []string, max int) {
	t.Helper()
	rcWritePlan(t, root, "0.1.0", before, max)
	rcWritePlan(t, root, "0.2.0", after, max)
}
func rcWritePlan(t *testing.T, root, version string, ids []string, max int) {
	t.Helper()
	steps := []any{}
	for _, id := range ids {
		step := rcStep(id)
		step["description"] = "work " + version
		steps = append(steps, step)
	}
	plan := map[string]any{"apiVersion": "aok/v1alpha1", "kind": "PlanDefinition", "metadata": rcMetadata("revision-plan", "Revision plan", version), "goalRef": map[string]any{"kind": "GoalContract", "id": "revision-goal", "version": "0.1.0"}, "strategy": "fixed-dag", "steps": steps, "methodRefs": []string{}, "inventory": []any{}, "aggregateVerification": []any{}, "concurrencyLimit": 1, "replanning": map[string]any{"maxRevisions": max, "preserveCompletedEvidence": true}}
	writeJSON(t, filepath.Join(root, "revision-plan-"+version+".json"), plan)
}
func rcMetadata(id, name, version string) map[string]any {
	return map[string]any{"id": id, "name": name, "version": version, "description": "revision consistency fixture"}
}
func rcWriteRevision(t *testing.T, root string, changes []any, preserved, invalidated []string) {
	t.Helper()
	revision := map[string]any{"apiVersion": "aok/v1alpha1", "kind": "PlanRevision", "metadata": metadata("revision", "Revision"), "trigger": "new evidence", "evidence": []string{}, "changes": changes, "preservedEvidence": preserved, "invalidatedEvidence": invalidated, "authorizationRef": nil, "state": "proposed", "beforePlan": map[string]any{"kind": "PlanDefinition", "id": "revision-plan", "version": "0.1.0"}, "afterPlan": map[string]any{"kind": "PlanDefinition", "id": "revision-plan", "version": "0.2.0"}, "predecessor": nil, "ordinal": 1}
	writeJSON(t, filepath.Join(root, "revision.json"), revision)
}

func TestRevisionRejectsOmittedChangesAndUnbackedAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		mutate     func(map[string]any)
	}{
		{"omitted changes", "planning.revision.change-missing", func(r map[string]any) { r["changes"] = []any{} }},
		{"accepted without evidence", "planning.revision.acceptance", func(r map[string]any) { r["state"] = "accepted" }},
		{"missing evidence file", "planning.file.reference", func(r map[string]any) { r["evidence"] = []string{"missing.txt"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := rcCorpus(t, 3)
			rcWritePlans(t, root, []string{"step"}, []string{"step"}, 3)
			rcWriteRevision(t, root, []any{map[string]any{"stepId": "step", "operation": "modify", "reason": "edit"}}, []string{}, []string{})
			files, err := filepath.Glob(filepath.Join(root, "revision.json"))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, path := range files {
				b, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var r map[string]any
				if err := json.Unmarshal(b, &r); err != nil {
					t.Fatal(err)
				}
				if r["kind"] != "PlanRevision" {
					continue
				}
				tc.mutate(r)
				writeJSON(t, path, r)
				found = true
			}
			if !found {
				t.Fatal("revision fixture missing")
			}
			got := Validate(root)
			if got.Status != "invalid" || !hasCode(got.Diagnostics, tc.code) {
				t.Fatalf("report=%+v", got)
			}
		})
	}
}
