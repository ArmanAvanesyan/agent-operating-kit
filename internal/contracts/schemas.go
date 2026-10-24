package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

type denyLoader struct{}

func (denyLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema resource is forbidden: %s", url)
}

func validateSchema(kind string, data []byte) error {
	return validateSchemaVersion(kind, "aok/v1alpha1", data)
}
func validateSchemaVersion(kind, version string, data []byte) error {
	prefix := "https://aok.invalid/operational/"
	if version == "aok/v1alpha2" {
		prefix += "v2/"
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(denyLoader{})
	// Install every resource before compilation so relative $refs are resolved
	// solely from the embedded corpus. The compiler has no network loader.
	entries, err := schemaFS.ReadDir("schemas")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		raw, e := schemaFS.ReadFile("schemas/" + name)
		if e != nil {
			return e
		}
		var doc any
		if e = json.Unmarshal(raw, &doc); e != nil {
			return fmt.Errorf("invalid embedded schema %s: %w", name, e)
		}
		if version == "aok/v1alpha2" {
			extendConnectorSchema(doc)
		}
		if e = c.AddResource(prefix+name, doc); e != nil {
			return e
		}
	}
	filenames := map[string]string{"Agent": "agent.json", "ExecutionProfile": "execution-profile.json", "AgentAssignment": "assignment.json", "CapabilityRegistry": "capability-registry.json", "InvocationContract": "invocation-contract.json", "ContextPolicy": "context-policy.json", "OutputContract": "output-contract.json", "EnvironmentProfile": "environment-profile.json", "LifecyclePolicy": "lifecycle-policy.json", "EvaluationContract": "evaluation-contract.json", "MigrationContract": "migration-contract.json", "TaskPacket": "task-packet.json", "GoalContract": "goalcontract.json", "PlanDefinition": "plandefinition.json", "PlanRevision": "planrevision.json", "DecisionContract": "decisioncontract.json", "DecisionResult": "decisionresult.json", "OptimizationExperiment": "optimizationexperiment.json", "SDLCTraceability": "sdlctraceability.json", "PlanningEvidence": "planningevidence.json", "WorkItemBinding": "workitembinding.json", "HostTaskBinding": "hosttaskbinding.json", "RunRecord": "runrecord.json", "ConnectorCapability": "connectorcapability.json", "SyncOperation": "syncoperation.json", "SyncReceipt": "syncreceipt.json"}
	schemaName, ok := filenames[kind]
	if !ok {
		return fmt.Errorf("no operational schema for %s", kind)
	}
	schemaName = prefix + schemaName
	schema, err := c.Compile(schemaName)
	if err != nil {
		return fmt.Errorf("compile operational schema for %s: %w", kind, err)
	}
	var instance any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err = d.Decode(&instance); err != nil {
		return err
	}
	if err = schema.Validate(instance); err != nil {
		return fmt.Errorf("schema validation: %w", err)
	}
	return nil
}

// Derive the opt-in schema in memory; embedded v1 bytes and interpretation stay fixed.
func extendConnectorSchema(v any) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if k == "const" && child == "aok/v1alpha1" {
				x[k] = "aok/v1alpha2"
				continue
			}
			if k == "$id" {
				if str, ok := child.(string); ok {
					x[k] = strings.Replace(str, "/operational/", "/operational/v2/", 1)
				}
				continue
			}
			if k == "enum" {
				if a, ok := child.([]any); ok {
					for _, value := range a {
						if value == "jira" {
							x[k] = append(a, "jira-datacenter")
							break
						}
					}
				}
			}
			extendConnectorSchema(child)
		}
	case []any:
		for _, child := range x {
			extendConnectorSchema(child)
		}
	}
}
