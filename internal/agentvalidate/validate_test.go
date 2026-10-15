package agentvalidate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func validCorpus(t *testing.T, mutate func(string, map[string]map[string]any)) string {
	t.Helper()
	root := t.TempDir()
	docs := map[string]map[string]any{}
	for _, name := range []string{"agent", "assignment", "capability-registry", "context-policy", "environment-profile", "execution-profile", "invocation-contract", "output-contract"} {
		b, e := os.ReadFile(filepath.Join("..", "contracts", "testdata", name+".json"))
		if e != nil {
			t.Fatal(e)
		}
		var d map[string]any
		if e = json.Unmarshal(b, &d); e != nil {
			t.Fatal(e)
		}
		docs[name] = d
	}
	proposal := "../../docs/proposals/agent-spec/examples/"
	for _, name := range []string{"lifecycle-policy", "evaluation-contract", "migration-contract"} {
		b, e := os.ReadFile(filepath.Join(proposal, name+".json"))
		if e != nil {
			t.Fatal(e)
		}
		var d map[string]any
		if e = json.Unmarshal(b, &d); e != nil {
			t.Fatal(e)
		}
		docs[name] = d
	}
	// Put every referenced support file beneath an explicit assets/ subtree.
	assets := filepath.Join(root, "assets")
	for _, dir := range []string{"schemas", "skills"} {
		if e := os.MkdirAll(filepath.Join(assets, dir), 0755); e != nil {
			t.Fatal(e)
		}
	}
	write := func(p, s string) {
		if e := os.WriteFile(filepath.Join(root, p), []byte(s), 0644); e != nil {
			t.Fatal(e)
		}
	}
	write("assets/instructions.md", "Instructions")
	packet, err := os.ReadFile(filepath.Join(proposal, "task-packet.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(assets, "task.json"), packet, 0644); err != nil {
		t.Fatal(err)
	}
	write("assets/AGENTS.md", "Project guidance")
	obj(docs["context-policy"])["requiredDocuments"] = []any{"assets/AGENTS.md"}
	write("assets/skills/project.md", "skill")
	write("assets/skills/readiness.md", "skill")
	write("assets/exploration.md", "{{includeEvidence}}")
	inputSchema := `{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"base.schema.json"}`
	inputDefinition := `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"repository":{"type":"string"},"depth":{"type":"string","enum":["overview","standard","deep"]}},"required":["repository","depth"],"additionalProperties":false}`
	outputSchema := `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":true}`
	write("assets/schemas/base.schema.json", inputDefinition)
	for _, p := range []string{"input", "inventory", "result"} {
		write("assets/schemas/"+p+".schema.json", outputSchema)
	}
	write("assets/schemas/input.schema.json", inputSchema)
	// Convert example-relative paths into corpus-relative asset references.
	obj(docs["agent"])["instructions"] = merge(obj(docs["agent"])["instructions"], map[string]any{"sources": []any{map[string]any{"path": "assets/instructions.md"}}})
	ex := obj(docs["agent"])["execution"].(map[string]any)
	profiles := array(ex["allowedProfiles"])
	ex["allowedProfiles"] = profiles[:1]
	obj(docs["agent"])["delegation"].(map[string]any)["maxDepth"] = json.Number("0")
	obj(docs["assignment"])["taskPacket"] = "assets/task.json"
	obj(docs["invocation-contract"])["inputSchema"] = "assets/schemas/input.schema.json"
	out := obj(docs["output-contract"])
	deliverables := array(out["deliverables"])
	for _, raw := range deliverables {
		d := obj(raw)
		if d["format"] == "markdown" {
			d["template"] = "assets/exploration.md"
		}
		if d["format"] == "json" {
			d["schema"] = "assets/schemas/inventory.schema.json"
		}
	}
	out["resultSchema"] = "assets/schemas/result.schema.json"
	eval := docs["evaluation-contract"]
	eval["agent"] = pin("Agent", "repository-explorer")
	eval["profiles"] = []any{pin("ExecutionProfile", "opencode-exploration")}
	eval["fixtures"] = []any{map[string]any{"id": "fixture", "taskPacket": "assets/task.json", "datasetRevision": "abc123"}}
	eval["results"] = []any{map[string]any{"profile": pin("ExecutionProfile", "opencode-exploration"), "status": "not-run", "evidence": []any{}}}
	// This representative template declaration exercises schema validation of
	// assignment output parameters, independently from the rendered content.
	registry := docs["capability-registry"]
	entries := array(registry["entries"])
	for _, raw := range entries {
		e := obj(raw)
		b := obj(e["binding"])
		switch e["id"] {
		case "project-exploration":
			b["sourcePath"] = "assets/skills/project.md"
		case "environment-readiness":
			b["sourcePath"] = "assets/skills/readiness.md"
		case "exploration-report-template":
			b["sourcePath"] = "assets/exploration.md"
			b["parameterSchema"] = map[string]any{"type": "object", "properties": map[string]any{"includeEvidence": map[string]any{"type": "boolean"}}, "required": []any{"includeEvidence"}, "additionalProperties": false}
		case "source.structure":
			e["dependencies"] = []any{}
		}
	}
	if mutate != nil {
		for _, name := range []string{"agent", "assignment", "capability-registry", "context-policy", "environment-profile", "execution-profile", "invocation-contract", "output-contract", "lifecycle-policy", "evaluation-contract", "migration-contract"} {
			mutate(name, docs)
		}
	}
	keys := make([]string, 0, len(docs))
	for name := range docs {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	for _, name := range keys {
		doc := docs[name]
		b, e := json.MarshalIndent(doc, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(root, name+".json"), append(b, '\n'), 0644); e != nil {
			t.Fatal(e)
		}
	}
	return root
}

func pin(kind, id string) map[string]any {
	return map[string]any{"kind": kind, "id": id, "version": "0.1.0"}
}
func merge(a, b any) map[string]any {
	m := obj(a)
	for k, v := range obj(b) {
		m[k] = v
	}
	return m
}

func TestValidateCompleteCorpus(t *testing.T) {
	root := validCorpus(t, nil)
	report := Validate(root)
	if report.Status != "valid" {
		t.Fatalf("report: %+v", report)
	}
	if report.Diagnostics == nil {
		t.Fatal("diagnostics must serialize as an empty array")
	}
}

func TestValidateRejectsSemanticFailures(t *testing.T) {
	cases := []struct {
		name, code string
		mutate     func(string, map[string]map[string]any)
	}{
		{"missing required reference", "reference.missing", func(n string, d map[string]map[string]any) {
			if n == "agent" {
				obj(d[n]["execution"])["defaultProfile"] = pin("ExecutionProfile", "missing")
			}
		}},
		{"duplicate resource tuple", "resource.duplicate", func(n string, d map[string]map[string]any) {
			if n == "assignment" {
				dup := jsonClone(d["agent"])
				dup["metadata"].(map[string]any)["id"] = "repository-explorer"
				d["duplicate-agent"] = dup
			}
		}},
		{"capability type mismatch", "capability.type-mismatch", func(n string, d map[string]map[string]any) {
			if n == "agent" {
				a := array(obj(d[n]["capabilities"])["tools"])
				obj(a[0])["id"] = "project-exploration"
			}
		}},
		{"capability dependency cycle", "capability.cycle", func(n string, d map[string]map[string]any) {
			if n == "capability-registry" {
				es := array(d[n]["entries"])
				a := obj(es[0])
				b := obj(es[1])
				a["dependencies"] = []any{pin("Capability", str(b["id"]))}
				b["dependencies"] = []any{pin("Capability", str(a["id"]))}
			}
		}},
		{"invalid default", "parameter.default.type", func(n string, d map[string]map[string]any) {
			if n == "agent" {
				p := obj(obj(d[n]["instructions"])["parameters"])["depth"].(map[string]any)
				p["default"] = json.Number("7")
			}
		}},
		{"invalid input", "assignment.input.enum", func(n string, d map[string]map[string]any) {
			if n == "assignment" {
				obj(d[n]["inputs"])["depth"] = "impossible"
			}
		}},
		{"disallowed profile", "assignment.profile", func(n string, d map[string]map[string]any) {
			if n == "assignment" {
				d[n]["executionProfile"] = pin("ExecutionProfile", "agy-exploration")
			}
		}},
		{"disallowed output", "assignment.output", func(n string, d map[string]map[string]any) {
			if n == "assignment" {
				obj(d[n]["outputs"])["contract"] = pin("OutputContract", "other")
			}
		}},
		{"unsafe asset path", "file.reference", func(n string, d map[string]map[string]any) {
			if n == "agent" {
				obj(obj(d[n]["instructions"])["sources"].([]any)[0])["path"] = "../outside.md"
			}
		}},
		{"unsafe output destination", "output.destination", func(n string, d map[string]map[string]any) {
			if n == "output-contract" {
				obj(array(d[n]["deliverables"])[0])["destination"] = "../outside.md"
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := validCorpus(t, tc.mutate)
			report := Validate(root)
			if report.Status != "invalid" {
				t.Fatalf("expected invalid, got %+v", report)
			}
			if !hasCode(report, tc.code) {
				t.Fatalf("expected %s, got %+v", tc.code, report.Diagnostics)
			}
		})
	}
}

func TestOptionalMissingCapabilityWarnsWithoutInvalidating(t *testing.T) {
	root := validCorpus(t, func(n string, d map[string]map[string]any) {
		if n == "capability-registry" {
			es := array(d[n]["entries"])
			kept := []any{}
			for _, raw := range es {
				if str(obj(raw)["id"]) != "source.structure" {
					kept = append(kept, raw)
				}
			}
			d[n]["entries"] = kept
		}
	})
	report := Validate(root)
	if report.Status != "valid" {
		t.Fatalf("optional miss should not invalidate: %+v", report)
	}
	if !hasCode(report, "reference.missing") {
		t.Fatalf("expected warning, got %+v", report.Diagnostics)
	}
}

func TestInvalidTaskPacketAssetIsRejected(t *testing.T) {
	root := validCorpus(t, nil)
	if err := os.WriteFile(filepath.Join(root, "assets", "task.json"), []byte("{\"title\":\"placeholder\"}"), 0644); err != nil {
		t.Fatal(err)
	}
	report := Validate(root)
	if report.Status != "invalid" || !hasCode(report, "task-packet.invalid") {
		t.Fatalf("expected invalid task packet diagnostic: %+v", report)
	}
}

func TestUnsafeSymlinksAndEmptyCorpus(t *testing.T) {
	root := t.TempDir()
	if report := Validate(root); report.Status != "invalid" || !hasCode(report, "corpus.empty") {
		t.Fatalf("empty corpus: %+v", report)
	}
	root = validCorpus(t, nil)
	if err := os.Symlink(filepath.Join(root, "agent.json"), filepath.Join(root, "linked.json")); err != nil {
		t.Fatal(err)
	}
	report := Validate(root)
	if !hasCode(report, "corpus.symlink") {
		t.Fatalf("symlink should be rejected: %+v", report)
	}
}

func TestValidationDiagnosticsAreDeterministic(t *testing.T) {
	root := validCorpus(t, func(n string, d map[string]map[string]any) {
		if n == "assignment" {
			d[n]["executionProfile"] = pin("ExecutionProfile", "missing")
		}
	})
	a, b := Validate(root), Validate(root)
	if len(a.Diagnostics) != len(b.Diagnostics) {
		t.Fatal("diagnostic count changed")
	}
	for i := range a.Diagnostics {
		if a.Diagnostics[i] != b.Diagnostics[i] {
			t.Fatalf("diagnostics differ at %d: %+v vs %+v", i, a.Diagnostics[i], b.Diagnostics[i])
		}
	}
}

func TestRemoteSchemaReferenceIsBlocked(t *testing.T) {
	root := validCorpus(t, nil)
	remote := `{"$schema":"https://json-schema.org/draft/2020-12/schema","$ref":"https://example.invalid/remote.schema.json"}`
	if err := os.WriteFile(filepath.Join(root, "assets/schemas/input.schema.json"), []byte(remote), 0644); err != nil {
		t.Fatal(err)
	}
	report := Validate(root)
	if report.Status != "invalid" || !hasCode(report, "schema.compile") {
		t.Fatalf("remote ref was not blocked: %+v", report)
	}
}

func jsonClone(v map[string]any) map[string]any {
	b, _ := json.Marshal(v)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}
func hasCode(r Report, code string) bool {
	for _, d := range r.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestAssetSchemasRejectDuplicateAndTrailingJSON(t *testing.T) {
	for _, body := range []string{`{"type":"object","type":"string"}`, `{"type":"object"} {}`} {
		root := validCorpus(t, nil)
		if err := os.WriteFile(filepath.Join(root, "assets/schemas/input.schema.json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if report := Validate(root); report.Status != "invalid" || !hasCode(report, "schema.compile") {
			t.Fatalf("accepted malformed schema %s: %+v", body, report)
		}
	}
}

func TestDefaultsAreAppliedBeforeInputAndTemplateValidation(t *testing.T) {
	root := validCorpus(t, func(_ string, docs map[string]map[string]any) {
		delete(obj(docs["assignment"]["inputs"]), "depth")
		delete(obj(docs["assignment"]["outputs"]), "parameters")
	})
	if r := Validate(root); r.Status != "valid" {
		t.Fatalf("defaults rejected: %+v", r)
	}
}
func TestNumberEnumsUseNumericEquality(t *testing.T) {
	if !enumContains([]any{json.Number("1.0")}, json.Number("1")) {
		t.Fatal("equivalent JSON numbers compared as strings")
	}
	if enumContains([]any{json.Number("1")}, "1") {
		t.Fatal("string treated as number")
	}
}

func TestInvalidBindingAndDuplicateDeliverable(t *testing.T) {
	for _, tc := range []struct {
		code   string
		mutate func(string, map[string]map[string]any)
	}{
		{"capability.binding.required", func(n string, d map[string]map[string]any) {
			if n == "capability-registry" {
				delete(obj(obj(array(d[n]["entries"])[0])["binding"]), "name")
			}
		}},
		{"identifier.duplicate", func(n string, d map[string]map[string]any) {
			if n == "output-contract" {
				a := array(d[n]["deliverables"])
				obj(a[1])["id"] = obj(a[0])["id"]
			}
		}},
	} {
		r := Validate(validCorpus(t, tc.mutate))
		if r.Status != "invalid" || !hasCode(r, tc.code) {
			t.Fatalf("expected %s: %+v", tc.code, r)
		}
	}
}

func TestBoundedReadRejectsNonRegularFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := readBounded(root); err == nil {
		t.Fatal("directory accepted")
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skip(err)
	}
	if _, err := readBounded(link); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestOutputDestinationCollisions(t *testing.T) {
	for _, destination := range []string{"reports/result.txt", "REPORTS/Result.txt", "reports/result.txt/nested", "reports"} {
		t.Run(destination, func(t *testing.T) {
			root := validCorpus(t, func(n string, docs map[string]map[string]any) {
				if n != "output-contract" {
					return
				}
				ds := array(docs[n]["deliverables"])
				obj(ds[0])["destination"] = "reports/result.txt"
				obj(ds[1])["destination"] = destination
			})
			report := Validate(root)
			if !hasCode(report, "output.destination.collision") {
				t.Fatalf("collision accepted: %+v", report)
			}
		})
	}
}
