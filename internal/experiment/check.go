// Package experiment checks inert optimization boundaries; it runs no optimizer.
package experiment

import (
	"regexp"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

var fieldPath = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*(\.[A-Za-z][A-Za-z0-9_-]*)*$`)
var frozenDataset = regexp.MustCompile(`^([^@\s]+)@([a-f0-9]{64})$`)

func values(v any) []any       { a, _ := v.([]any); return a }
func str(v any) string         { s, _ := v.(string); return s }
func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

// Check requires frozen dataset references (ID@content-sha256), disjoint IDs
// and content hashes across all splits, and protected-field separation.
func Check(doc map[string]any) []contracts.Diagnostic {
	ds := []contracts.Diagnostic{}
	add := func(code, field, message string) {
		ds = append(ds, contracts.Diagnostic{Code: code, Field: field, Severity: "error", Message: message})
	}
	if doc["baselineVersion"] != nil && doc["baselineVersion"] != obj(doc["agentRef"])["version"] {
		add("experiment.baseline", "baselineVersion", "baseline version must match the pinned agent version")
	}
	protected := []string{"permissions", "verification", "outputs", "identity.boundaries", "evaluator", "datasets.test"}
	for _, raw := range values(doc["protectedFields"]) {
		p := str(raw)
		if !fieldPath.MatchString(p) {
			add("experiment.field", "protectedFields", "protected fields must be canonical dotted paths")
		}
		protected = append(protected, p)
	}
	for _, raw := range values(doc["mutableFields"]) {
		path := str(raw)
		if !fieldPath.MatchString(path) {
			add("experiment.field", "mutableFields", "mutable fields must be canonical dotted paths")
			continue
		}
		for _, p := range protected {
			if path == p || strings.HasPrefix(path, p+".") || strings.HasPrefix(p, path+".") {
				add("experiment.protected", "mutableFields", "mutable field overlaps a protected boundary: "+path)
				break
			}
		}
	}
	datasets := obj(doc["datasets"])
	ids := map[string]string{}
	hashes := map[string]string{}
	for _, split := range []string{"train", "validation", "test"} {
		if len(values(datasets[split])) == 0 {
			add("experiment.dataset.empty", "datasets."+split, "each dataset split requires at least one frozen reference")
		}
		for _, raw := range values(datasets[split]) {
			matches := frozenDataset.FindStringSubmatch(str(raw))
			if matches == nil {
				add("experiment.dataset.revision", "datasets."+split, "dataset reference must be ID@content-sha256")
				continue
			}
			if prior, ok := ids[matches[1]]; ok {
				add("experiment.dataset.overlap", "datasets."+split, "dataset identity also occurs in "+prior)
			}
			if prior, ok := hashes[matches[2]]; ok {
				add("experiment.dataset.overlap", "datasets."+split, "dataset content also occurs in "+prior)
			}
			ids[matches[1]] = split
			hashes[matches[2]] = split
		}
	}
	if obj(doc["evaluator"])["immutable"] != true {
		add("experiment.evaluator", "evaluator.immutable", "evaluator must be immutable")
	}
	if obj(doc["promotion"])["automatic"] != false {
		add("experiment.promotion", "promotion.automatic", "automatic promotion is not supported")
	}
	return ds
}
