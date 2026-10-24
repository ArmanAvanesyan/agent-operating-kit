package usage

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schemas/*.json
var schemaFS embed.FS

// ValidateJSON applies common strict JSON checks and a closed embedded schema.
func ValidateJSON(data []byte, schemaName string) error {
	if err := contracts.ValidateJSON(data); err != nil {
		return err
	}
	raw, err := schemaFS.ReadFile("schemas/" + schemaName)
	if err != nil {
		return err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(usageDenyLoader{})
	var doc any
	if err = json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	uri := "https://aok.invalid/usage/" + schemaName
	if err = c.AddResource(uri, doc); err != nil {
		return err
	}
	s, err := c.Compile(uri)
	if err != nil {
		return err
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err = d.Decode(&value); err != nil {
		return err
	}
	if err = s.Validate(value); err != nil {
		return fmt.Errorf("usage schema: %w", err)
	}
	// Typed decoding closes nested records too; the embedded root schema provides
	// independently inspectable versioned JSON contracts.
	var target any
	switch schemaName {
	case "export.json":
		target = &Export{}
	case "policy.json":
		target = &Policy{}
	case "binding.json":
		target = &Binding{}
	case "demand.json":
		target = &Demand{}
	default:
		return fmt.Errorf("unknown usage schema %q", schemaName)
	}
	if err = Decode(data, target); err != nil {
		return err
	}
	return nil
}

type usageDenyLoader struct{}

func (usageDenyLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference is forbidden: %s", url)
}
