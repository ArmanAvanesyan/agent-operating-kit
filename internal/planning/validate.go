package planning

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentvalidate"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

// Validate runs closed-schema/reference validation followed by planning graph
// and cross-contract checks. Decision and experiment-specific semantics are
// intentionally owned by their respective validators.
func Validate(directory string) Report {
	validated := agentvalidate.Validate(directory)
	report := Report{Diagnostics: append([]contracts.Diagnostic{}, validated.Diagnostics...)}
	if validated.Status == "invalid" {
		report.finish("invalid")
		return report
	}
	corpus, loaded := agentvalidate.Load(directory)
	report.Diagnostics = append(report.Diagnostics, loaded.Diagnostics...)
	if loaded.Status == "invalid" || corpus == nil {
		if corpus != nil {
			setContextHashes(&report, corpus)
		}
		report.finish("invalid")
		return report
	}
	setContextHashes(&report, corpus)
	report.Diagnostics = append(report.Diagnostics, semanticChecks(corpus)...)
	if hasError(report.Diagnostics) {
		report.finish("invalid")
	} else {
		report.finish("valid")
	}
	return report
}

func setContextHashes(report *Report, corpus *agentvalidate.Corpus) {
	if digest, err := contextSHA256(corpus); err == nil {
		report.ContextSHA256 = digest
	}
	report.PlanHashes = map[string]string{}
	for _, doc := range corpus.Resources {
		if doc.Resource.Kind == "PlanDefinition" {
			sum := sha256.Sum256(doc.Resource.Document)
			report.PlanHashes[filepath.ToSlash(doc.Path)] = hex.EncodeToString(sum[:])
		}
	}
}

func semanticChecks(c *agentvalidate.Corpus) []contracts.Diagnostic {
	r := Report{Diagnostics: []contracts.Diagnostic{}}
	byKey := map[docKey]agentvalidate.LoadedResource{}
	for _, doc := range c.Resources {
		k := docKey{doc.Resource.Kind, doc.Resource.Envelope.Metadata.ID, doc.Resource.Envelope.Metadata.Version}
		if _, exists := byKey[k]; exists {
			r.add("planning.resource.duplicate", doc.Path, "metadata", "error", fmt.Sprintf("duplicate %s %s@%s", k.kind, k.id, k.version))
		} else {
			byKey[k] = doc
		}
	}
	capabilities := map[docKey]string{}
	for _, doc := range c.Resources {
		if doc.Resource.Kind != "CapabilityRegistry" {
			continue
		}
		for i, value := range array(doc.Object["entries"]) {
			entry := object(value)
			key := docKey{"Capability", stringValue(entry["id"]), stringValue(entry["version"])}
			if _, exists := capabilities[key]; exists {
				r.add("planning.capability.duplicate", doc.Path, fmt.Sprintf("entries[%d]", i), "error", "duplicate capability registry entry")
			}
			capabilities[key] = stringValue(entry["type"])
		}
	}
	goals := map[docKey]agentvalidate.LoadedResource{}
	plans := map[docKey]agentvalidate.LoadedResource{}
	for key, doc := range byKey {
		switch key.kind {
		case "GoalContract":
			goals[key] = doc
		case "PlanDefinition":
			plans[key] = doc
		}
	}
	for _, goal := range goals {
		if value, ok := goal.Object["planRef"]; ok && value != nil {
			planRef := refValue(value)
			plan, found := resolveMapRef(plans, planRef, "PlanDefinition")
			if !found {
				r.add("planning.goal.plan", goal.Path, "planRef", "error", "exact declared plan version does not exist")
			} else if !sameRef(refValue(plan.Object["goalRef"]), contracts.Reference{Kind: "GoalContract", ID: goal.Resource.Envelope.Metadata.ID, Version: goal.Resource.Envelope.Metadata.Version}) {
				r.add("planning.goal.plan", goal.Path, "planRef", "error", "declared plan points to a different goal")
			}
		}
	}
	for _, doc := range c.Resources {
		switch doc.Resource.Kind {
		case "GoalContract":
			validateGoal(&r, doc, byKey, capabilities)
		case "PlanDefinition":
			validatePlan(&r, doc, byKey, capabilities)
		case "PlanRevision":
			validateRevision(&r, doc, plans, goals, byKey)
			for _, field := range []string{"evidence", "preservedEvidence", "invalidatedEvidence"} {
				for i, value := range array(doc.Object[field]) {
					checkLocalReference(&r, c, doc, fmt.Sprintf("%s[%d]", field, i), stringValue(value))
				}
			}
			if ref := stringValue(doc.Object["authorizationRef"]); ref != "" {
				checkLocalReference(&r, c, doc, "authorizationRef", ref)
			}
		case "SDLCTraceability":
			validateTraceability(&r, c, doc, byKey, plans)
		case "PlanningEvidence":
			validatePlanningEvidence(&r, c, doc, byKey)
		case "DecisionContract":
			validateDecisionContract(&r, c, doc)
		case "DecisionResult":
			validateDecisionResult(&r, c, doc, byKey)
		case "OptimizationExperiment":
			validateExperiment(&r, c, doc, byKey)
		}
	}
	validateRevisionChains(&r, c.Resources, plans, goals)
	return sorted(r.Diagnostics)
}

type docKey struct{ kind, id, version string }

func validateGoal(r *Report, doc agentvalidate.LoadedResource, byKey map[docKey]agentvalidate.LoadedResource, capabilities map[docKey]string) {
	goal := doc.Object
	if planValue, ok := goal["planRef"]; ok && planValue != nil {
		resolveRef(r, byKey, doc.Path, "planRef", refValue(planValue), "PlanDefinition")
	}
	known := map[string]bool{}
	requiredSuccess := 0
	for _, field := range []string{"successPredicates", "invariants"} {
		for i, raw := range array(goal[field]) {
			p := object(raw)
			id := stringValue(p["id"])
			if id == "" || known[id] {
				r.add("planning.predicate.duplicate", doc.Path, fmt.Sprintf("%s[%d].id", field, i), "error", "predicate IDs must be non-empty and unique within the goal")
			}
			known[id] = true
			if field == "successPredicates" && p["required"] == true {
				requiredSuccess++
			}
			validateVerifier(r, doc.Path, fmt.Sprintf("%s[%d].verifierRef", field, i), refValue(p["verifierRef"]), capabilities)
		}
	}
	if requiredSuccess == 0 {
		r.add("planning.goal.success-required", doc.Path, "successPredicates", "error", "goal must declare at least one required success predicate")
	}
}

func validatePlan(r *Report, doc agentvalidate.LoadedResource, byKey map[docKey]agentvalidate.LoadedResource, capabilities map[docKey]string) {
	plan := doc.Object
	goalDoc, goalFound := resolveRef(r, byKey, doc.Path, "goalRef", refValue(plan["goalRef"]), "GoalContract")
	goalPredicates := map[string]map[string]any{}
	if goalFound {
		planMaxRevisions := integer(object(plan["replanning"])["maxRevisions"])
		goalMaxReplans := integer(object(goalDoc.Object["limits"])["maxReplans"])
		if planMaxRevisions > goalMaxReplans {
			r.add("planning.replanning.limit", doc.Path, "replanning.maxRevisions", "error", "plan maxRevisions cannot exceed its goal maxReplans limit")
		}
		for _, field := range []string{"successPredicates", "invariants"} {
			for _, raw := range array(goalDoc.Object[field]) {
				p := object(raw)
				goalPredicates[stringValue(p["id"])] = p
			}
		}
	}
	stepIDs := map[string]int{}
	steps := array(plan["steps"])
	inventoryIDs := map[string]bool{}
	for i, raw := range steps {
		step := object(raw)
		id := stringValue(step["id"])
		if id == "" || stepIDs[id] > 0 {
			r.add("planning.step.duplicate", doc.Path, fmt.Sprintf("steps[%d].id", i), "error", "step IDs must be non-empty and unique")
		}
		stepIDs[id] = i + 1
		resolveRef(r, byKey, doc.Path, fmt.Sprintf("steps[%d].assignmentRef", i), refValue(step["assignmentRef"]), "AgentAssignment")
		validateVerifier(r, doc.Path, fmt.Sprintf("steps[%d].verifierRef", i), refValue(step["verifierRef"]), capabilities)
		for _, field := range []string{"preconditions", "completionChecks"} {
			for j, rawPredicate := range array(step[field]) {
				predicate := object(rawPredicate)
				validateVerifier(r, doc.Path, fmt.Sprintf("steps[%d].%s[%d].verifierRef", i, field, j), refValue(predicate["verifierRef"]), capabilities)
			}
		}
		requiredChecks := 0
		for _, p := range array(step["completionChecks"]) {
			if object(p)["required"] == true {
				requiredChecks++
			}
		}
		if requiredChecks == 0 {
			r.add("planning.step.completion-required", doc.Path, fmt.Sprintf("steps[%d].completionChecks", i), "error", "each step must have at least one required completion check")
		}
	}
	predicateIDs := map[string]bool{}
	for i, raw := range steps {
		step := object(raw)
		for _, field := range []string{"preconditions", "completionChecks"} {
			for j, rawPredicate := range array(step[field]) {
				id := stringValue(object(rawPredicate)["id"])
				if id == "" || predicateIDs[id] {
					r.add("planning.predicate.duplicate", doc.Path, fmt.Sprintf("steps[%d].%s[%d].id", i, field, j), "error", "predicate IDs must be non-empty and unique within the plan")
				}
				predicateIDs[id] = true
			}
		}
	}
	for id := range predicateIDs {
		if _, exists := goalPredicates[id]; exists {
			r.add("planning.predicate.ambiguous", doc.Path, "steps", "error", "plan predicate ID collides with referenced goal predicate: "+id)
		}
	}
	for i, raw := range array(plan["inventory"]) {
		item := object(raw)
		itemID := stringValue(item["id"])
		if itemID == "" || inventoryIDs[itemID] {
			r.add("planning.inventory.duplicate", doc.Path, fmt.Sprintf("inventory[%d].id", i), "error", "inventory IDs must be non-empty and unique")
		}
		inventoryIDs[itemID] = true
		for j, stepRef := range array(item["stepRefs"]) {
			if _, ok := stepIDs[stringValue(stepRef)]; !ok {
				r.add("planning.inventory.step", doc.Path, fmt.Sprintf("inventory[%d].stepRefs[%d]", i, j), "error", "inventory item references an unknown plan step")
			}
		}
	}
	graph := map[string][]string{}
	for i, raw := range steps {
		step := object(raw)
		id := stringValue(step["id"])
		for j, dependency := range array(step["dependsOn"]) {
			dep := stringValue(dependency)
			if _, ok := stepIDs[dep]; !ok {
				r.add("planning.dependency.missing", doc.Path, fmt.Sprintf("steps[%d].dependsOn[%d]", i, j), "error", "dependency does not name a declared step")
			}
			if dep == id {
				r.add("planning.dependency.cycle", doc.Path, fmt.Sprintf("steps[%d].dependsOn[%d]", i, j), "error", "step cannot depend on itself")
			}
			graph[id] = append(graph[id], dep)
		}
	}
	checkCycles(r, doc.Path, graph)
	for i, raw := range array(plan["aggregateVerification"]) {
		p := object(raw)
		validateVerifier(r, doc.Path, fmt.Sprintf("aggregateVerification[%d].verifierRef", i), refValue(p["verifierRef"]), capabilities)
		id := stringValue(p["id"])
		if prior, exists := goalPredicates[id]; exists {
			if !sameRef(refValue(prior["verifierRef"]), refValue(p["verifierRef"])) || prior["required"] != p["required"] {
				r.add("planning.predicate.ambiguous", doc.Path, fmt.Sprintf("aggregateVerification[%d].id", i), "error", "aggregate predicate alias conflicts with goal definition")
			}
		} else if id == "" || predicateIDs[id] {
			r.add("planning.predicate.duplicate", doc.Path, fmt.Sprintf("aggregateVerification[%d].id", i), "error", "predicate IDs must be non-empty and unique within the plan")
		}
		if id != "" {
			predicateIDs[id] = true
		}
	}
	for id := range stepIDs {
		if predicateIDs[id] {
			r.add("planning.identifier.ambiguous", doc.Path, "steps", "error", "step and predicate identifiers must not collide: "+id)
		}
	}
}

func validateRevision(r *Report, doc agentvalidate.LoadedResource, plans map[docKey]agentvalidate.LoadedResource, goals map[docKey]agentvalidate.LoadedResource, byKey map[docKey]agentvalidate.LoadedResource) {
	revision := doc.Object
	before := refValue(revision["beforePlan"])
	after := refValue(revision["afterPlan"])
	beforeDoc, beforeOK := resolveRef(r, byKey, doc.Path, "beforePlan", before, "PlanDefinition")
	afterDoc, afterOK := resolveRef(r, byKey, doc.Path, "afterPlan", after, "PlanDefinition")
	if beforeOK && afterOK {
		if before.ID != after.ID || before.Version == after.Version {
			r.add("planning.revision.plan", doc.Path, "afterPlan", "error", "revision must retain plan ID and change its exact version")
		}
		beforeGoal, afterGoal := refValue(beforeDoc.Object["goalRef"]), refValue(afterDoc.Object["goalRef"])
		if !sameRef(beforeGoal, afterGoal) {
			r.add("planning.revision.goal", doc.Path, "afterPlan", "error", "revision must preserve the plan goal reference")
		}
		beforeValues, afterValues := map[string]any{}, map[string]any{}
		for _, step := range array(beforeDoc.Object["steps"]) {
			beforeValues[stringValue(object(step)["id"])] = step
		}
		for _, step := range array(afterDoc.Object["steps"]) {
			afterValues[stringValue(object(step)["id"])] = step
		}
		beforeSteps := stepIDSet(beforeDoc.Object)
		afterSteps := stepIDSet(afterDoc.Object)
		seenChanges := map[string]bool{}
		for i, raw := range array(revision["changes"]) {
			change := object(raw)
			id, operation := stringValue(change["stepId"]), stringValue(change["operation"])
			if seenChanges[id] {
				r.add("planning.revision.change-duplicate", doc.Path, fmt.Sprintf("changes[%d].stepId", i), "error", "each step may have only one change entry per revision")
			}
			seenChanges[id] = true
			beforeHas, afterHas := beforeSteps[id], afterSteps[id]
			consistent := (operation == "add" && !beforeHas && afterHas) || (operation == "remove" && beforeHas && !afterHas) || (operation == "modify" && beforeHas && afterHas && !reflect.DeepEqual(beforeValues[id], afterValues[id]))
			if !consistent {
				r.add("planning.revision.change", doc.Path, fmt.Sprintf("changes[%d].stepId", i), "error", fmt.Sprintf("%s change requires appropriate step presence in beforePlan and afterPlan", operation))
			}
		}
		all := map[string]bool{}
		for id := range beforeValues {
			all[id] = true
		}
		for id := range afterValues {
			all[id] = true
		}
		for id := range all {
			if !reflect.DeepEqual(beforeValues[id], afterValues[id]) && !seenChanges[id] {
				r.add("planning.revision.change-missing", doc.Path, "changes", "error", "changed step has no declared operation: "+id)
			}
		}
	}
	if revision["state"] == "accepted" && (stringValue(revision["authorizationRef"]) == "" || len(array(revision["evidence"])) == 0) {
		r.add("planning.revision.acceptance", doc.Path, "state", "error", "accepted revisions require evidence and an authorization reference; references do not themselves grant authority")
	}
	preserved := map[string]bool{}
	for _, value := range array(revision["preservedEvidence"]) {
		preserved[stringValue(value)] = true
	}
	for i, value := range array(revision["invalidatedEvidence"]) {
		if preserved[stringValue(value)] {
			r.add("planning.revision.evidence-overlap", doc.Path, fmt.Sprintf("invalidatedEvidence[%d]", i), "error", "evidence cannot be both preserved and invalidated")
		}
	}
	if predecessor := revision["predecessor"]; predecessor != nil {
		resolveRef(r, byKey, doc.Path, "predecessor", refValue(predecessor), "PlanRevision")
	}
	ordinal := integer(revision["ordinal"])
	if ordinal < 1 {
		r.add("planning.revision.ordinal", doc.Path, "ordinal", "error", "revision ordinal must be positive")
	}
	_ = plans
	_ = goals
}

func validateRevisionChains(r *Report, resources []agentvalidate.LoadedResource, plans map[docKey]agentvalidate.LoadedResource, goals map[docKey]agentvalidate.LoadedResource) {
	revisions := map[docKey]agentvalidate.LoadedResource{}
	ordered := []agentvalidate.LoadedResource{}
	for _, doc := range resources {
		if doc.Resource.Kind != "PlanRevision" {
			continue
		}
		key := docKey{"PlanRevision", doc.Resource.Envelope.Metadata.ID, doc.Resource.Envelope.Metadata.Version}
		revisions[key] = doc
		ordered = append(ordered, doc)
	}
	for _, doc := range ordered {
		rv := doc.Object
		ordinal := integer(rv["ordinal"])
		predValue := rv["predecessor"]
		if predValue == nil {
			if ordinal != 1 {
				r.add("planning.revision.chain", doc.Path, "predecessor", "error", "first revision must have ordinal 1")
			}
			continue
		}
		pred := refValue(predValue)
		previous, ok := revisions[docKey{pred.Kind, pred.ID, pred.Version}]
		if !ok {
			r.add("planning.revision.predecessor", doc.Path, "predecessor", "error", "exact predecessor revision does not exist")
			continue
		}
		if integer(previous.Object["ordinal"])+1 != ordinal {
			r.add("planning.revision.ordinal", doc.Path, "ordinal", "error", "revision ordinal must follow its predecessor")
		}
		if !sameRef(refValue(previous.Object["afterPlan"]), refValue(rv["beforePlan"])) {
			r.add("planning.revision.chain", doc.Path, "beforePlan", "error", "revision beforePlan must equal predecessor afterPlan")
		}
	}
	// Count every declared revision record conservatively, regardless of its
	// proposal state, because no state here proves that the record is inert.
	counts := map[string]int64{}
	planCounts := map[string]int64{}
	for _, doc := range ordered {
		before := refValue(doc.Object["beforePlan"])
		if plan, ok := plans[docKey{before.Kind, before.ID, before.Version}]; ok {
			planCounts[before.ID]++
			planLimit := integer(object(plan.Object["replanning"])["maxRevisions"])
			if planCounts[before.ID] > planLimit {
				r.add("planning.revision.plan-budget", doc.Path, "ordinal", "error", "revision chain exceeds the plan maxRevisions limit")
			}
			goal := refValue(plan.Object["goalRef"])
			counts[refKey(goal)]++
			if gd, found := goals[docKey{goal.Kind, goal.ID, goal.Version}]; found {
				max := integer(object(gd.Object["limits"])["maxReplans"])
				if counts[refKey(goal)] > max {
					r.add("planning.revision.budget", doc.Path, "ordinal", "error", "revision chain exceeds goal maxReplans")
				}
			}
		}
	}
}

func stepIDSet(plan map[string]any) map[string]bool {
	ids := map[string]bool{}
	for _, raw := range array(plan["steps"]) {
		ids[stringValue(object(raw)["id"])] = true
	}
	return ids
}

func validateTraceability(r *Report, c *agentvalidate.Corpus, doc agentvalidate.LoadedResource, byKey map[docKey]agentvalidate.LoadedResource, plans map[docKey]agentvalidate.LoadedResource) {
	trace := doc.Object
	goalRef := refValue(trace["goalRef"])
	planRef := refValue(trace["planRef"])
	resolveRef(r, byKey, doc.Path, "goalRef", goalRef, "GoalContract")
	plan, planOK := resolveRef(r, byKey, doc.Path, "planRef", planRef, "PlanDefinition")
	if planOK && !sameRef(goalRef, refValue(plan.Object["goalRef"])) {
		r.add("planning.traceability.goal", doc.Path, "planRef", "error", "traceability goal must match the referenced plan goal")
	}
	linkedPredicates := map[string]bool{}
	for i, raw := range array(trace["links"]) {
		link := object(raw)
		for j, pred := range array(link["predicateRefs"]) {
			predicateID := stringValue(pred)
			linkedPredicates[predicateID] = true
			if !knownPredicate(byKey, goalRef, predicateID) {
				r.add("planning.traceability.predicate", doc.Path, fmt.Sprintf("links[%d].predicateRefs[%d]", i, j), "error", "predicate is not declared by the referenced goal")
			}
		}
		if stringValue(trace["coverage"]) == "verified" && len(array(link["runRefs"])) == 0 {
			r.add("planning.traceability.run", doc.Path, fmt.Sprintf("links[%d].runRefs", i), "error", "verified coverage requires run references for every link")
		}
		stepID := stringValue(link["stepRef"])
		if planOK && !planHasStep(plan, stepID) {
			r.add("planning.traceability.step", doc.Path, fmt.Sprintf("links[%d].stepRef", i), "error", "step is not declared by the referenced plan")
		}
		resolveRef(r, byKey, doc.Path, fmt.Sprintf("links[%d].assignmentRef", i), refValue(link["assignmentRef"]), "AgentAssignment")
		resolveRef(r, byKey, doc.Path, fmt.Sprintf("links[%d].outputContractRef", i), refValue(link["outputContractRef"]), "OutputContract")
		resolveRef(r, byKey, doc.Path, fmt.Sprintf("links[%d].verificationRef", i), refValue(link["verificationRef"]), "EvaluationContract")
		if planOK {
			for _, step := range array(plan.Object["steps"]) {
				s := object(step)
				if stringValue(s["id"]) == stepID && !sameRef(refValue(s["assignmentRef"]), refValue(link["assignmentRef"])) {
					r.add("planning.traceability.assignment", doc.Path, fmt.Sprintf("links[%d].assignmentRef", i), "error", "assignment does not match the linked plan step")
				}
			}
		}
		if planOK {
			for _, rawStep := range array(plan.Object["steps"]) {
				step := object(rawStep)
				if stringValue(step["id"]) != stepID {
					continue
				}
				assignmentRef := refValue(link["assignmentRef"])
				assignmentDoc, found := byKey[docKey{assignmentRef.Kind, assignmentRef.ID, assignmentRef.Version}]
				if found && !sameRef(refValue(object(assignmentDoc.Object["outputs"])["contract"]), refValue(link["outputContractRef"])) {
					r.add("planning.traceability.output", doc.Path, fmt.Sprintf("links[%d].outputContractRef", i), "error", "output contract does not match the linked assignment")
				}
			}
		}
	}
	if stringValue(trace["coverage"]) == "verified" {
		foundEvidence := false
		var evidenceDoc *agentvalidate.LoadedResource
		for _, path := range array(trace["evidence"]) {
			resolved, err := c.ResolveFile(doc.Path, stringValue(path))
			if err != nil {
				r.add("planning.traceability.evidence", doc.Path, "evidence", "error", err.Error())
				continue
			}
			rel, _ := filepath.Rel(c.Root, resolved)
			normalized := filepath.ToSlash(rel)
			for _, evidence := range byKey {
				if evidence.Resource.Kind == "PlanningEvidence" && evidence.Path == normalized && sameRef(refValue(evidence.Object["planRef"]), planRef) {
					foundEvidence = true
					e := evidence
					evidenceDoc = &e
				}
			}
		}
		if !foundEvidence {
			r.add("planning.traceability.evidence", doc.Path, "evidence", "error", "verified coverage requires a matching PlanningEvidence document with validated artifact references")
		} else if evidenceDoc != nil {
			outcomes := map[string]map[string]any{}
			for _, raw := range array(evidenceDoc.Object["outcomes"]) {
				o := object(raw)
				outcomes[stringValue(o["predicateId"])] = o
			}
			goalDoc, ok := byKey[docKey{goalRef.Kind, goalRef.ID, goalRef.Version}]
			if ok {
				for _, field := range []string{"successPredicates", "invariants"} {
					for _, raw := range array(goalDoc.Object[field]) {
						p := object(raw)
						id := stringValue(p["id"])
						if p["required"] != true {
							continue
						}
						if !linkedPredicates[id] {
							r.add("planning.traceability.coverage", doc.Path, "links", "error", "verified coverage omits required goal predicate "+id)
						}
						o, exists := outcomes[id]
						if !exists || stringValue(o["status"]) != "pass" || len(array(o["artifactRefs"])) == 0 {
							r.add("planning.traceability.outcome", evidenceDoc.Path, "outcomes", "error", "verified coverage requires a passing artifact-backed outcome for "+id)
						}
					}
				}
			}
			if eval, e1 := time.Parse(time.RFC3339, stringValue(evidenceDoc.Object["evaluatedAt"])); e1 != nil {
				r.add("planning.traceability.time", evidenceDoc.Path, "evaluatedAt", "error", "evidence timestamp must be RFC3339")
			} else if exp, e2 := time.Parse(time.RFC3339, stringValue(evidenceDoc.Object["expiresAt"])); e2 != nil || !exp.After(eval) {
				r.add("planning.traceability.time", evidenceDoc.Path, "expiresAt", "error", "evidence expiry must follow evaluatedAt")
			}
		}
	}
}

func validateVerifier(r *Report, source, field string, ref contracts.Reference, capabilities map[docKey]string) {
	if ref.Kind != "Capability" {
		r.add("planning.verifier.kind", source, field, "error", "verifierRef must use kind Capability")
		return
	}
	if typ, ok := capabilities[docKey{"Capability", ref.ID, ref.Version}]; !ok {
		r.add("planning.verifier.missing", source, field, "error", "exact verifier capability is missing")
	} else if typ != "verification" {
		r.add("planning.verifier.type", source, field, "error", "verifier capability entry must have type verification")
	}
}

func resolveRef(r *Report, byKey map[docKey]agentvalidate.LoadedResource, source, field string, ref contracts.Reference, expected string) (agentvalidate.LoadedResource, bool) {
	if ref.Kind != expected || ref.ID == "" || ref.Version == "" {
		r.add("planning.reference.pin", source, field, "error", "reference must be an exact typed kind/id/version pin for "+expected)
		return agentvalidate.LoadedResource{}, false
	}
	doc, ok := byKey[docKey{ref.Kind, ref.ID, ref.Version}]
	if !ok {
		r.add("planning.reference.missing", source, field, "error", fmt.Sprintf("%s %s@%s does not exist", ref.Kind, ref.ID, ref.Version))
	}
	return doc, ok
}

func resolveMapRef(index map[docKey]agentvalidate.LoadedResource, ref contracts.Reference, expected string) (agentvalidate.LoadedResource, bool) {
	if ref.Kind != expected {
		return agentvalidate.LoadedResource{}, false
	}
	doc, ok := index[docKey{ref.Kind, ref.ID, ref.Version}]
	return doc, ok
}

func knownPredicate(index map[docKey]agentvalidate.LoadedResource, goal contracts.Reference, id string) bool {
	doc, ok := index[docKey{goal.Kind, goal.ID, goal.Version}]
	if !ok {
		return false
	}
	for _, field := range []string{"successPredicates", "invariants"} {
		for _, raw := range array(doc.Object[field]) {
			if stringValue(object(raw)["id"]) == id {
				return true
			}
		}
	}
	return false
}
func planHasStep(plan agentvalidate.LoadedResource, id string) bool {
	for _, raw := range array(plan.Object["steps"]) {
		if stringValue(object(raw)["id"]) == id {
			return true
		}
	}
	return false
}
func checkCycles(r *Report, source string, graph map[string][]string) {
	state := map[string]uint8{}
	var visit func(string)
	visit = func(node string) {
		if state[node] == 1 {
			r.add("planning.dependency.cycle", source, "steps.dependsOn", "error", "step dependency graph contains a cycle")
			return
		}
		if state[node] == 2 {
			return
		}
		state[node] = 1
		for _, next := range graph[node] {
			visit(next)
		}
		state[node] = 2
	}
	for node := range graph {
		visit(node)
	}
}

func refValue(v any) contracts.Reference {
	m := object(v)
	return contracts.Reference{Kind: stringValue(m["kind"]), ID: stringValue(m["id"]), Version: stringValue(m["version"])}
}
func sameRef(a, b contracts.Reference) bool {
	return a.Kind == b.Kind && a.ID == b.ID && a.Version == b.Version
}
func integer(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		rat, ok := new(big.Rat).SetString(n.String())
		if !ok || !rat.IsInt() || !rat.Num().IsInt64() {
			return 0
		}
		return rat.Num().Int64()
	case int64:
		return n
	}
	return 0
}
func refKey(r contracts.Reference) string { return r.Kind + "\x00" + r.ID + "\x00" + r.Version }
func object(v any) map[string]any         { m, _ := v.(map[string]any); return m }
func array(v any) []any                   { a, _ := v.([]any); return a }
func stringValue(v any) string            { s, _ := v.(string); return s }
func hasError(ds []contracts.Diagnostic) bool {
	for _, d := range ds {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}
func sorted(ds []contracts.Diagnostic) []contracts.Diagnostic {
	sort.Slice(ds, func(i, j int) bool {
		a, b := ds[i], ds[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		return a.Message < b.Message
	})
	return ds
}
func (r *Report) finish(status string) {
	r.Status = status
	r.Diagnostics = sorted(r.Diagnostics)
	if r.Diagnostics == nil {
		r.Diagnostics = []contracts.Diagnostic{}
	}
}
