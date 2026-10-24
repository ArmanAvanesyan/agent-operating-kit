package agentvalidate

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/workflowcheck"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

type resourceKey struct{ Kind, ID, Version string }
type capability struct {
	ID, Version, Type, Source, BindingPath string
	ParameterSchema                        any
	Deps                                   []contracts.Reference
}

func semanticChecks(c *Corpus) []contracts.Diagnostic {
	r := Report{}
	byKey := map[resourceKey]LoadedResource{}
	caps := map[resourceKey]capability{}
	for _, doc := range c.Resources {
		m := doc.Resource.Metadata
		if m.ID == "" {
			continue
		}
		k := resourceKey{doc.Resource.Kind, m.ID, m.Version}
		if old, ok := byKey[k]; ok {
			r.add("resource.duplicate", doc.Path, "metadata", "error", fmt.Sprintf("duplicate %s %s@%s also declared at %s", k.Kind, k.ID, k.Version, old.Path))
		} else {
			byKey[k] = doc
		}
		if doc.Resource.Kind == "CapabilityRegistry" {
			for i, raw := range array(doc.Object["entries"]) {
				e := obj(raw)
				id := str(e["id"])
				ver := str(e["version"])
				if id == "" {
					continue
				}
				ck := resourceKey{"Capability", id, ver}
				if _, ok := caps[ck]; ok {
					r.add("capability.duplicate", doc.Path, fmt.Sprintf("entries[%d].id", i), "error", fmt.Sprintf("duplicate capability %s@%s", id, ver))
				} else {
					binding := obj(e["binding"])
					caps[ck] = capability{ID: id, Version: ver, Type: str(e["type"]), Source: doc.Path, BindingPath: str(binding["sourcePath"]), ParameterSchema: binding["parameterSchema"], Deps: references(e["dependencies"])}
				}
			}
		}
	}
	checkCapabilityCycles(&r, caps)
	// Capability requirements reference registry entries, with the ref kind
	// constraining the entry's declared capability type.
	for _, doc := range c.Resources {
		switch doc.Resource.Kind {
		case "CapabilityRegistry":
			validateCapabilityRegistry(&r, c, doc, caps)
		case "Agent":
			validateAgent(&r, c, doc, byKey, caps)
		case "ExecutionProfile":
			validateProfile(&r, doc, byKey)
		case "AgentAssignment":
			validateAssignment(&r, c, doc, byKey, caps)
		case "InvocationContract":
			validateInvocation(&r, c, doc)
		case "OutputContract":
			validateOutput(&r, c, doc)
		case "ContextPolicy":
			validateContext(&r, c, doc)
		case "EvaluationContract":
			validateEvaluation(&r, c, doc, byKey)
		}
	}
	delegationCycles(&r, c.Resources, byKey)
	validateAssetSchemas(&r, c)
	workflowDocs := make([]workflowcheck.Document, 0, len(c.Resources))
	for _, doc := range c.Resources {
		workflowDocs = append(workflowDocs, workflowcheck.Document{Resource: doc.Resource, Path: doc.Path})
	}
	r.Diagnostics = append(r.Diagnostics, workflowcheck.Check(workflowDocs, func(owner, ref string) ([]byte, error) {
		path, err := c.ResolveFile(owner, ref)
		if err != nil {
			return nil, err
		}
		return readBounded(path)
	})...)
	sortDiagnostics(r.Diagnostics)
	return r.Diagnostics
}

func validateCapabilityRegistry(r *Report, c *Corpus, doc LoadedResource, caps map[resourceKey]capability) {
	entries := array(doc.Object["entries"])
	for i, raw := range entries {
		e := obj(raw)
		typeName := str(e["type"])
		switch typeName {
		case "tool", "skill", "mcp", "plugin", "verification", "template":
		default:
			r.add("capability.type", doc.Path, fmt.Sprintf("entries[%d].type", i), "error", fmt.Sprintf("unsupported capability type %q", typeName))
		}
		for j, ref := range references(e["dependencies"]) {
			if ref.Kind != "Capability" {
				r.add("reference.kind", doc.Path, fmt.Sprintf("entries[%d].dependencies[%d]", i, j), "error", "capability dependency references must use kind Capability")
			}
			resolveCapability(r, caps, doc.Path, fmt.Sprintf("entries[%d].dependencies[%d]", i, j), ref, "Capability", true)
		}
		binding := obj(e["binding"])
		bindingKind := str(binding["type"])
		allowed := bindingKind == typeName || (typeName == "tool" && (bindingKind == "native-tool" || bindingKind == "command"))
		if !allowed {
			r.add("capability.binding.type", doc.Path, fmt.Sprintf("entries[%d].binding.type", i), "error", "binding type does not match capability type")
		}
		requiredFields := map[string][]string{"native-tool": {"adapter", "name"}, "command": {"executableRef"}, "skill": {"sourcePath"}, "mcp": {"connectionRef"}, "plugin": {"adapter", "name"}, "verification": {"name"}, "template": {"sourcePath"}}
		for _, field := range requiredFields[bindingKind] {
			if str(binding[field]) == "" {
				r.add("capability.binding.required", doc.Path, fmt.Sprintf("entries[%d].binding.%s", i, field), "error", "binding field is required for this capability")
			}
		}
		if p := str(binding["sourcePath"]); p != "" {
			requireFile(r, c, doc.Path, fmt.Sprintf("entries[%d].binding.sourcePath", i), p)
		}
		if ps, ok := binding["parameterSchema"]; ok {
			if _, err := compileInlineSchema(ps, fmt.Sprintf("%s#entries/%d/parameterSchema", doc.Path, i)); err != nil {
				r.add("schema.compile", doc.Path, fmt.Sprintf("entries[%d].binding.parameterSchema", i), "error", err.Error())
			}
		}
	}
}

func validateAgent(r *Report, c *Corpus, doc LoadedResource, byKey map[resourceKey]LoadedResource, caps map[resourceKey]capability) {
	o := doc.Object
	execution := obj(o["execution"])
	defaultProfile := ref(execution["defaultProfile"])
	resolve(r, byKey, doc.Path, "execution.defaultProfile", defaultProfile, "ExecutionProfile", false)
	allowed := map[resourceKey]bool{}
	for i, v := range array(execution["allowedProfiles"]) {
		x := ref(v)
		allowed[resourceKey{x.Kind, x.ID, x.Version}] = true
		resolve(r, byKey, doc.Path, fmt.Sprintf("execution.allowedProfiles[%d]", i), x, "ExecutionProfile", false)
	}
	if !allowed[resourceKey{defaultProfile.Kind, defaultProfile.ID, defaultProfile.Version}] {
		r.add("agent.profile.default", doc.Path, "execution.defaultProfile", "error", "default profile must be included in allowedProfiles")
	}
	for i, v := range array(execution["fallbackProfiles"]) {
		x := ref(v)
		if !allowed[resourceKey{x.Kind, x.ID, x.Version}] {
			r.add("agent.profile.fallback", doc.Path, fmt.Sprintf("execution.fallbackProfiles[%d]", i), "error", "fallback profile must be included in allowedProfiles")
		}
	}
	for field, kind := range map[string]string{"invocationContract": "InvocationContract", "contextPolicy": "ContextPolicy", "lifecyclePolicy": "LifecyclePolicy", "evaluationContract": "EvaluationContract", "migrationContract": "MigrationContract"} {
		resolve(r, byKey, doc.Path, field, ref(o[field]), kind, false)
	}
	resolve(r, byKey, doc.Path, "environment.profile", ref(obj(o["environment"])["profile"]), "EnvironmentProfile", false)
	out := obj(o["outputs"])
	resolve(r, byKey, doc.Path, "outputs.contract", ref(out["contract"]), "OutputContract", false)
	for i, v := range array(out["allowedContracts"]) {
		resolve(r, byKey, doc.Path, fmt.Sprintf("outputs.allowedContracts[%d]", i), ref(v), "OutputContract", false)
	}
	for field, kind := range map[string]string{"tools": "Tool", "skills": "Skill", "mcps": "MCP", "plugins": "Plugin"} {
		for i, v := range array(obj(o["capabilities"])[field]) {
			x := ref(v)
			resolveCapability(r, caps, doc.Path, fmt.Sprintf("capabilities.%s[%d]", field, i), x, kind, boolValue(obj(v)["required"]))
		}
	}
	validateParameters(r, doc)
	for _, v := range array(obj(o["instructions"])["sources"]) {
		requireFile(r, c, doc.Path, "instructions.sources.path", str(obj(v)["path"]))
	}
	for _, v := range array(obj(o["verification"])["required"]) {
		id := str(v)
		matches := capabilityMatches(caps, id, "verification")
		if len(matches) == 0 {
			r.add("capability.missing", doc.Path, "verification.required", "error", "required verification capability "+id+" is missing")
		} else if len(matches) > 1 {
			r.add("capability.ambiguous", doc.Path, "verification.required", "error", "verification capability "+id+" has multiple versions; pinning is required")
		}
	}
	delegation := obj(o["delegation"])
	enabled, maxDepth, agents := boolValue(delegation["enabled"]), numberInt(delegation["maxDepth"]), array(delegation["allowedAgents"])
	if enabled && (maxDepth < 1 || len(agents) == 0) {
		r.add("agent.delegation.policy", doc.Path, "delegation", "error", "enabled delegation requires a positive maxDepth and at least one allowed agent")
	}
	if !enabled && (maxDepth != 0 || len(agents) > 0) {
		r.add("agent.delegation.policy", doc.Path, "delegation", "error", "disabled delegation must have maxDepth zero and no allowed agents")
	}
	for i, v := range agents {
		resolve(r, byKey, doc.Path, fmt.Sprintf("delegation.allowedAgents[%d]", i), ref(v), "Agent", false)
	}
}

func validateProfile(r *Report, doc LoadedResource, byKey map[resourceKey]LoadedResource) {
	resolve(r, byKey, doc.Path, "environment.profile", ref(obj(doc.Object["environment"])["profile"]), "EnvironmentProfile", false)
	model := obj(doc.Object["model"])
	reason := obj(model["reasoning"])
	mode := str(reason["mode"])
	effort := str(reason["effort"])
	_, hasBudget := reason["budgetTokens"]
	switch mode {
	case "effort":
		if effort == "" || hasBudget {
			r.add("profile.reasoning", doc.Path, "model.reasoning", "error", "effort mode requires effort and forbids budgetTokens")
		}
	case "budget":
		if !hasBudget || effort != "" {
			r.add("profile.reasoning", doc.Path, "model.reasoning", "error", "budget mode requires budgetTokens and forbids effort")
		}
	default:
		if effort != "" || hasBudget {
			r.add("profile.reasoning", doc.Path, "model.reasoning", "error", "reasoning effort and token budget are mutually exclusive settings and are unavailable in this mode")
		}
	}
}

func validateAssignment(r *Report, c *Corpus, doc LoadedResource, byKey map[resourceKey]LoadedResource, caps map[resourceKey]capability) {
	o := doc.Object
	agentRef := ref(o["agent"])
	agent, ok := resolve(r, byKey, doc.Path, "agent", agentRef, "Agent", false)
	profileRef := ref(o["executionProfile"])
	resolve(r, byKey, doc.Path, "executionProfile", profileRef, "ExecutionProfile", false)
	contractRef := ref(obj(o["outputs"])["contract"])
	resolve(r, byKey, doc.Path, "outputs.contract", contractRef, "OutputContract", false)
	if p := str(o["taskPacket"]); p != "" {
		validateTaskPacketFile(r, c, doc.Path, "taskPacket", p)
	}
	if ok {
		ex := obj(agent.Object["execution"])
		allowed := false
		for _, p := range array(ex["allowedProfiles"]) {
			if sameRef(ref(p), profileRef) {
				allowed = true
			}
		}
		if !allowed {
			r.add("assignment.profile", doc.Path, "executionProfile", "error", "selected execution profile is not allowed by the assigned agent")
		}
		aout := obj(agent.Object["outputs"])
		valid := sameRef(ref(aout["contract"]), contractRef)
		for _, p := range array(aout["allowedContracts"]) {
			if sameRef(ref(p), contractRef) {
				valid = true
			}
		}
		if !valid {
			r.add("assignment.output", doc.Path, "outputs.contract", "error", "selected output contract is not allowed by the assigned agent")
		}
		validateInputValues(r, doc, agent)
		invRef := ref(agent.Object["invocationContract"])
		if inv, yes := byKey[resourceKey{invRef.Kind, invRef.ID, invRef.Version}]; yes {
			schemaPath := str(inv.Object["inputSchema"])
			if schemaPath != "" {
				path, err := requireFile(r, c, inv.Path, "inputSchema", schemaPath)
				if err == nil {
					if err = validateInstanceAgainstFileSchema(c.Root, path, effectiveInputs(agent.Object, obj(o["inputs"]))); err != nil {
						r.add("assignment.inputs.schema", doc.Path, "inputs", "error", err.Error())
					}
				}
			}
		}
		if outputDoc, found := byKey[resourceKey{contractRef.Kind, contractRef.ID, contractRef.Version}]; found {
			validateOutputParameters(r, c, doc, agent, outputDoc, caps)
		}
	}
}

func validateInvocation(r *Report, c *Corpus, doc LoadedResource) {
	if p := str(doc.Object["inputSchema"]); p != "" {
		if path, err := requireFile(r, c, doc.Path, "inputSchema", p); err == nil {
			if _, e := compileFileSchema(c.Root, path); e != nil {
				r.add("schema.compile", doc.Path, "inputSchema", "error", e.Error())
			}
		}
	}
}
func validateOutput(r *Report, c *Corpus, doc LoadedResource) {
	uniqueIDs(r, doc, "deliverables")
	destinations := []string{}
	for i, v := range array(doc.Object["deliverables"]) {
		d := obj(v)
		if destination := str(d["destination"]); !safeDestination(destination) {
			r.add("output.destination", doc.Path, fmt.Sprintf("deliverables[%d].destination", i), "error", "destination must be a confined relative path")
		}
		destination := strings.ToLower(str(d["destination"]))
		for _, prior := range destinations {
			if destination == prior || strings.HasPrefix(destination, prior+"/") || strings.HasPrefix(prior, destination+"/") {
				r.add("output.destination.collision", doc.Path, fmt.Sprintf("deliverables[%d].destination", i), "error", "output destinations collide (case-insensitive file/directory comparison)")
				break
			}
		}
		destinations = append(destinations, destination)
		for _, field := range []string{"template", "schema"} {
			if p := str(d[field]); p != "" {
				if path, err := requireFile(r, c, doc.Path, fmt.Sprintf("deliverables[%d].%s", i, field), p); err == nil && field == "schema" {
					if _, e := compileFileSchema(c.Root, path); e != nil {
						r.add("schema.compile", doc.Path, fmt.Sprintf("deliverables[%d].schema", i), "error", e.Error())
					}
				}
			}
		}
	}
	if p := str(doc.Object["resultSchema"]); p != "" {
		if path, err := requireFile(r, c, doc.Path, "resultSchema", p); err == nil {
			if _, e := compileFileSchema(c.Root, path); e != nil {
				r.add("schema.compile", doc.Path, "resultSchema", "error", e.Error())
			}
		}
	}
}

func validateContext(r *Report, c *Corpus, doc LoadedResource) {
	for i, v := range array(doc.Object["requiredDocuments"]) {
		requireFile(r, c, doc.Path, fmt.Sprintf("requiredDocuments[%d]", i), str(v))
	}
}

func safeDestination(p string) bool {
	if p == "" || strings.ContainsAny(p, "\\:") || strings.HasPrefix(p, "/") || filepath.IsAbs(p) {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validateTaskPacketFile(r *Report, c *Corpus, owner, field, ref string) {
	path, err := requireFile(r, c, owner, field, ref)
	if err != nil {
		return
	}
	b, err := readBounded(path)
	if err == nil {
		err = contracts.ValidateTaskPacket(b)
	}
	if err != nil {
		r.add("task-packet.invalid", owner, field, "error", err.Error())
	}
}
func validateEvaluation(r *Report, c *Corpus, doc LoadedResource, byKey map[resourceKey]LoadedResource) {
	uniqueIDs(r, doc, "fixtures")
	uniqueIDs(r, doc, "metrics")
	resolve(r, byKey, doc.Path, "agent", ref(doc.Object["agent"]), "Agent", false)
	for i, v := range array(doc.Object["profiles"]) {
		resolve(r, byKey, doc.Path, fmt.Sprintf("profiles[%d]", i), ref(v), "ExecutionProfile", false)
	}
	for i, v := range array(doc.Object["results"]) {
		resolve(r, byKey, doc.Path, fmt.Sprintf("results[%d].profile", i), ref(obj(v)["profile"]), "ExecutionProfile", false)
	}
	for i, v := range array(doc.Object["fixtures"]) {
		validateTaskPacketFile(r, c, doc.Path, fmt.Sprintf("fixtures[%d].taskPacket", i), str(obj(v)["taskPacket"]))
	}
}

func resolve(r *Report, index map[resourceKey]LoadedResource, source, field string, x contracts.Reference, expected string, optional bool) (LoadedResource, bool) {
	if x.Kind != expected {
		r.add("reference.kind", source, field, "error", fmt.Sprintf("reference kind %q does not match expected kind %q", x.Kind, expected))
		return LoadedResource{}, false
	}
	if x.ID == "" || x.Version == "" {
		r.add("reference.pin", source, field, "error", "reference must include exact id and version")
		return LoadedResource{}, false
	}
	d, ok := index[resourceKey{x.Kind, x.ID, x.Version}]
	if !ok {
		sev := "error"
		if optional {
			sev = "warning"
		}
		r.add("reference.missing", source, field, sev, fmt.Sprintf("%s %s@%s is not present in this corpus", x.Kind, x.ID, x.Version))
		return LoadedResource{}, false
	}
	return d, true
}

func resolveCapability(r *Report, index map[resourceKey]capability, source, field string, x contracts.Reference, expected string, required bool) {
	if x.Kind != expected {
		r.add("reference.kind", source, field, "error", fmt.Sprintf("capability kind %q does not match expected %q", x.Kind, expected))
		return
	}
	cap, ok := index[resourceKey{"Capability", x.ID, x.Version}]
	if !ok {
		sev := "error"
		if !required {
			sev = "warning"
		}
		r.add("reference.missing", source, field, sev, fmt.Sprintf("capability %s@%s is not present", x.ID, x.Version))
		return
	}
	want := map[string]string{"Tool": "tool", "Skill": "skill", "MCP": "mcp", "Plugin": "plugin"}[expected]
	if want != "" && cap.Type != want {
		r.add("capability.type-mismatch", source, field, "error", fmt.Sprintf("capability %s@%s has type %q; expected %q", x.ID, x.Version, cap.Type, want))
	}
}

func validateParameters(r *Report, doc LoadedResource) {
	params := obj(obj(doc.Object["instructions"])["parameters"])
	for name, v := range params {
		p := obj(v)
		if values, exists := p["enum"]; exists {
			if len(array(values)) == 0 {
				r.add("parameter.enum.empty", doc.Path, "instructions.parameters."+name+".enum", "error", "enum must contain at least one value")
			}
			for _, value := range array(values) {
				if !valueTypeMatches(value, str(p["type"])) {
					r.add("parameter.enum.type", doc.Path, "instructions.parameters."+name+".enum", "error", "enum value does not match declared type")
				}
			}
		}
		if d, ok := p["default"]; ok {
			if !valueTypeMatches(d, str(p["type"])) {
				r.add("parameter.default.type", doc.Path, "instructions.parameters."+name+".default", "error", "default value does not match declared parameter type")
			}
			if !enumContains(p["enum"], d) {
				r.add("parameter.default.enum", doc.Path, "instructions.parameters."+name+".default", "error", "default value is outside declared enum")
			}
		}
	}
}
func validateInputValues(r *Report, assignment, agent LoadedResource) {
	declared := obj(obj(agent.Object["instructions"])["parameters"])
	inputs := effectiveInputs(agent.Object, obj(assignment.Object["inputs"]))
	for name, value := range inputs {
		p, ok := declared[name].(map[string]any)
		if !ok {
			// InvocationContract.inputSchema is authoritative for additional
			// structured inputs; it is checked separately against the local schema.
			continue
		}
		if !valueTypeMatches(value, str(p["type"])) {
			r.add("assignment.input.type", assignment.Path, "inputs."+name, "error", "input value does not match declared parameter type")
		}
		if !enumContains(p["enum"], value) {
			r.add("assignment.input.enum", assignment.Path, "inputs."+name, "error", "input value is outside declared enum")
		}
	}
}
func validateOutputParameters(r *Report, c *Corpus, assignment, agent, output LoadedResource, caps map[resourceKey]capability) {
	declared := obj(obj(agent.Object["outputs"])["parameters"])
	values := mergeValues(declared, obj(obj(assignment.Object["outputs"])["parameters"]))
	for name, value := range values {
		d, ok := declared[name]
		if !ok {
			r.add("assignment.output.parameter", assignment.Path, "outputs.parameters."+name, "error", "output parameter is not declared by the assigned agent")
			continue
		}
		if !valueTypeMatches(value, anyType(d)) {
			r.add("assignment.output.parameter.type", assignment.Path, "outputs.parameters."+name, "error", "output parameter type does not match the agent declaration")
		}
	}
	for i, raw := range array(output.Object["deliverables"]) {
		template := str(obj(raw)["template"])
		if template == "" {
			continue
		}
		path, err := c.ResolveFile(output.Path, template)
		if err != nil {
			continue
		}
		matches := 0
		for _, capability := range caps {
			if capability.Type != "template" || capability.BindingPath == "" || capability.ParameterSchema == nil {
				continue
			}
			registryPath, e := c.ResolveFile(capability.Source, capability.BindingPath)
			if e != nil || registryPath != path {
				continue
			}
			matches++
			if matches > 1 {
				r.add("assignment.template.ambiguous", assignment.Path, "outputs.contract", "error", "multiple registry entries describe the same template path")
			}
			schema, e := compileInlineSchema(capability.ParameterSchema, capability.Source+"#template-parameters")
			if e != nil {
				r.add("schema.compile", capability.Source, "entries.binding.parameterSchema", "error", e.Error())
				continue
			}
			if e = schema.Validate(values); e != nil {
				r.add("assignment.template.parameters", assignment.Path, fmt.Sprintf("outputs.parameters (deliverables[%d])", i), "error", e.Error())
			}
		}
	}
}

func requireFile(r *Report, c *Corpus, owner, field, ref string) (string, error) {
	p, e := c.ResolveFile(owner, ref)
	if e != nil {
		r.add("file.reference", owner, field, "error", fmt.Sprintf("%q: %v", ref, e))
		return "", e
	}
	return p, nil
}
func validateAssetSchemas(r *Report, c *Corpus) {
	paths := []string{}
	for p := range c.Files {
		if strings.HasSuffix(strings.ToLower(p), ".schema.json") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	for _, p := range paths {
		full, e := c.ResolveFile(".root", p)
		if e != nil {
			r.add("schema.path", p, "", "error", e.Error())
			continue
		}
		if _, e = compileFileSchema(c.Root, full); e != nil {
			r.add("schema.compile", p, "", "error", e.Error())
		}
	}
}

func compileFileSchema(root, path string) (*jsonschema.Schema, error) {
	info, e := os.Stat(path)
	if e != nil {
		return nil, e
	}
	if info.Size() > contracts.MaxDocumentBytes {
		return nil, fmt.Errorf("schema exceeds 4 MiB")
	}
	b, e := readBounded(path)
	if e != nil {
		return nil, e
	}
	doc, e := contracts.DecodeJSON(b)
	if e != nil {
		return nil, e
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(blockLoader{})
	resources := []string{}
	e = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == root {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(p), ".schema.json") {
			resources = append(resources, p)
		}
		return nil
	})
	if e != nil {
		return nil, e
	}
	sort.Strings(resources)
	for _, resourcePath := range resources {
		if resourcePath == path {
			continue
		}
		resourceBytes, readErr := readBounded(resourcePath)
		if readErr != nil {
			return nil, readErr
		}
		resourceDoc, readErr := contracts.DecodeJSON(resourceBytes)
		if readErr != nil {
			return nil, readErr
		}
		if readErr = c.AddResource(fileURL(resourcePath), resourceDoc); readErr != nil {
			return nil, readErr
		}
	}
	u := fileURL(path)
	if e = c.AddResource(u, doc); e != nil {
		return nil, e
	}
	return c.Compile(u)
}
func fileURL(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
func compileInlineSchema(value any, base string) (*jsonschema.Schema, error) {
	b, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	doc, e := contracts.DecodeJSON(b)
	if e != nil {
		return nil, e
	}
	u := "https://aok.invalid/inline/" + url.PathEscape(base) + ".json"
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(blockLoader{})
	if e = c.AddResource(u, doc); e != nil {
		return nil, e
	}
	return c.Compile(u)
}
func validateInstanceAgainstFileSchema(root, path string, value any) error {
	s, e := compileFileSchema(root, path)
	if e != nil {
		return e
	}
	return s.Validate(value)
}

type blockLoader struct{}

func (blockLoader) Load(uri string) (any, error) {
	return nil, fmt.Errorf("external schema reference is forbidden: %s", uri)
}

func obj(v any) map[string]any {
	x, _ := v.(map[string]any)
	if x == nil {
		return map[string]any{}
	}
	return x
}
func array(v any) []any    { x, _ := v.([]any); return x }
func str(v any) string     { x, _ := v.(string); return x }
func boolValue(v any) bool { x, _ := v.(bool); return x }
func ref(v any) contracts.Reference {
	m := obj(v)
	return contracts.Reference{Kind: str(m["kind"]), ID: str(m["id"]), Version: str(m["version"])}
}
func references(v any) []contracts.Reference {
	a := array(v)
	out := make([]contracts.Reference, 0, len(a))
	for _, x := range a {
		out = append(out, ref(x))
	}
	return out
}
func sameRef(a, b contracts.Reference) bool {
	return a.Kind == b.Kind && a.ID == b.ID && a.Version == b.Version
}
func valueTypeMatches(v any, t string) bool {
	switch t {
	case "null":
		return v == nil
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		switch v.(type) {
		case json.Number, float64, float32, int, int64:
			return true
		}
		return false
	default:
		return false
	}
}
func anyType(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number, float64, float32, int, int64:
		return "number"
	}
	return ""
}
func enumContains(enum, v any) bool {
	a, ok := enum.([]any)
	if !ok || len(a) == 0 {
		return true
	}
	for _, x := range a {
		if equalValue(x, v) {
			return true
		}
	}
	return false
}
func capabilityMatches(caps map[resourceKey]capability, id, typeName string) []capability {
	out := []capability{}
	for _, v := range caps {
		if v.ID == id && v.Type == typeName {
			out = append(out, v)
		}
	}
	return out
}
func checkCapabilityCycles(r *Report, caps map[resourceKey]capability) {
	color := map[resourceKey]uint8{}
	var visit func(resourceKey)
	visit = func(k resourceKey) {
		color[k] = 1
		for _, d := range caps[k].Deps {
			n := resourceKey{"Capability", d.ID, d.Version}
			if _, ok := caps[n]; !ok {
				continue
			}
			if color[n] == 1 {
				r.add("capability.cycle", caps[k].Source, "entries.dependencies", "error", fmt.Sprintf("dependency cycle reaches %s@%s", n.ID, n.Version))
				continue
			}
			if color[n] == 0 {
				visit(n)
			}
		}
		color[k] = 2
	}
	keys := make([]resourceKey, 0, len(caps))
	for k := range caps {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].ID != keys[j].ID {
			return keys[i].ID < keys[j].ID
		}
		return keys[i].Version < keys[j].Version
	})
	for _, k := range keys {
		if color[k] == 0 {
			visit(k)
		}
	}
}

func delegationCycles(r *Report, docs []LoadedResource, index map[resourceKey]LoadedResource) {
	graph := map[resourceKey][]resourceKey{}
	for _, doc := range docs {
		if doc.Resource.Kind != "Agent" {
			continue
		}
		key := resourceKey{"Agent", doc.Resource.Metadata.ID, doc.Resource.Metadata.Version}
		for i, v := range array(obj(doc.Object["delegation"])["allowedAgents"]) {
			x := ref(v)
			target := resourceKey{x.Kind, x.ID, x.Version}
			if _, ok := index[target]; ok {
				graph[key] = append(graph[key], target)
			}
			_ = i
		}
	}
	color := map[resourceKey]uint8{}
	reported := map[string]bool{}
	var visit func(resourceKey)
	visit = func(k resourceKey) {
		color[k] = 1
		for _, n := range graph[k] {
			if color[n] == 1 {
				edge := k.ID + "@" + k.Version + "->" + n.ID + "@" + n.Version
				if !reported[edge] {
					r.add("agent.delegation.cycle", sourceFor(docs, k), "delegation.allowedAgents", "error", "delegation cycle includes "+edge)
					reported[edge] = true
				}
				continue
			}
			if color[n] == 0 {
				visit(n)
			}
		}
		color[k] = 2
	}
	keys := make([]resourceKey, 0, len(graph))
	for k := range graph {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].ID != keys[j].ID {
			return keys[i].ID < keys[j].ID
		}
		return keys[i].Version < keys[j].Version
	})
	for _, k := range keys {
		if color[k] == 0 {
			visit(k)
		}
	}
}
func sourceFor(docs []LoadedResource, k resourceKey) string {
	for _, d := range docs {
		if d.Resource.Kind == k.Kind && d.Resource.Metadata.ID == k.ID && d.Resource.Metadata.Version == k.Version {
			return d.Path
		}
	}
	return ""
}
func numberInt(v any) int {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func mergeValues(defaults, supplied map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range defaults {
		out[k] = v
	}
	for k, v := range supplied {
		out[k] = v
	}
	return out
}
func effectiveInputs(agent, supplied map[string]any) map[string]any {
	defaults := map[string]any{}
	for k, v := range obj(obj(agent["instructions"])["parameters"]) {
		if value, ok := obj(v)["default"]; ok {
			defaults[k] = value
		}
	}
	return mergeValues(defaults, supplied)
}
func equalValue(a, b any) bool {
	if anyType(a) != anyType(b) {
		return false
	}
	if anyType(a) == "number" {
		x, ok := new(big.Rat).SetString(fmt.Sprint(a))
		y, yes := new(big.Rat).SetString(fmt.Sprint(b))
		return ok && yes && x.Cmp(y) == 0
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func uniqueIDs(r *Report, doc LoadedResource, field string) {
	seen := map[string]bool{}
	for i, value := range array(doc.Object[field]) {
		id := str(obj(value)["id"])
		if seen[id] {
			r.add("identifier.duplicate", doc.Path, fmt.Sprintf("%s[%d].id", field, i), "error", "nested identifier is duplicated")
		}
		seen[id] = true
	}
}
