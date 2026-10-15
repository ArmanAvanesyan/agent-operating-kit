// Package contracts contains the versioned, offline operational document model.
package contracts

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

//go:embed schemas/*.json
var schemaFS embed.FS

const MaxDocumentBytes = 4 << 20
const MaxDepth = 64

type Envelope struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
}
type Metadata struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}
type Reference struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version string `json:"version"`
}
type Diagnostic struct {
	Code     string `json:"code"`
	Source   string `json:"source"`
	Field    string `json:"field"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Resource is a schema-validated document; Document preserves its exact JSON bytes.
type Resource struct {
	Envelope `json:"-"`
	Document json.RawMessage `json:"-"`
}

// Parse validates canonical syntax, the supported envelope and the embedded operational schema.
func Parse(data []byte) (any, []Diagnostic, error) {
	if len(data) > MaxDocumentBytes {
		return fail("document.size", "", "document exceeds 4 MiB")
	}
	if !utf8Valid(data) {
		return fail("document.utf8", "", "document is not valid UTF-8")
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return fail("document.bom", "", "UTF-8 BOM is not permitted")
	}
	if err := scanJSON(data); err != nil {
		return fail("document.syntax", "", err.Error())
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	var head struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
	}
	if err := dec.Decode(&head); err != nil {
		return fail("document.syntax", "", err.Error())
	}
	if head.APIVersion != "aok/v1alpha1" {
		return fail("document.api-version", "apiVersion", fmt.Sprintf("unsupported apiVersion %q", head.APIVersion))
	}
	switch head.Kind {
	case "Agent", "ExecutionProfile", "AgentAssignment", "CapabilityRegistry", "InvocationContract", "ContextPolicy", "OutputContract", "EnvironmentProfile", "LifecyclePolicy", "EvaluationContract", "MigrationContract", "GoalContract", "PlanDefinition", "PlanRevision", "DecisionContract", "DecisionResult", "OptimizationExperiment", "SDLCTraceability", "PlanningEvidence":
	default:
		return fail("document.kind", "kind", fmt.Sprintf("unsupported kind %q", head.Kind))
	}
	if err := validateSchema(head.Kind, data); err != nil {
		return fail("document.schema", "", err.Error())
	}
	var envelope Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fail("document.envelope", "", err.Error())
	}
	return Resource{Envelope: envelope, Document: append(json.RawMessage(nil), data...)}, nil, nil
}
func fail(code, field, message string) (any, []Diagnostic, error) {
	e := errors.New(message)
	return nil, []Diagnostic{{Code: code, Field: field, Severity: "error", Message: message}}, e
}
func utf8Valid(b []byte) bool { return utf8.Valid(b) }
func scanJSON(b []byte) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var parse func(int) error
	parse = func(depth int) error {
		if depth > MaxDepth {
			return errors.New("JSON nesting exceeds 64 levels")
		}
		t, e := dec.Token()
		if e != nil {
			return e
		}
		switch x := t.(type) {
		case json.Number:
			// Bound exact-arithmetic work before the schema engine sees numbers.
			if len(x) > 1024 {
				return errors.New("JSON numeric lexeme exceeds 1024 bytes")
			}
			if i := strings.IndexAny(string(x), "eE"); i >= 0 {
				exponent, err := strconv.ParseInt(string(x)[i+1:], 10, 32)
				if err != nil || exponent < -10000 || exponent > 10000 {
					return errors.New("JSON numeric exponent exceeds supported range")
				}
			}
		case json.Delim:
			switch x {
			case '{':
				seen := map[string]bool{}
				for dec.More() {
					k, e := dec.Token()
					if e != nil {
						return e
					}
					key, ok := k.(string)
					if !ok {
						return errors.New("object key is not a string")
					}
					if seen[key] {
						return fmt.Errorf("duplicate JSON key %q", key)
					}
					seen[key] = true
					if e = parse(depth + 1); e != nil {
						return e
					}
				}
				_, e = dec.Token()
				return e
			case '[':
				for dec.More() {
					if e := parse(depth + 1); e != nil {
						return e
					}
				}
				_, e = dec.Token()
				return e
			}
		}
		return nil
	}
	if err := parse(0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing JSON value or data")
	}
	return nil
}
