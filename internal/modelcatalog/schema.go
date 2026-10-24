package modelcatalog

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// Validate validates all closed model catalog records against the embedded
// schemas after the common strict JSON syntax checks.
func Validate(data []byte, schemaName string) error {
	if err := contracts.ValidateJSON(data); err != nil {
		return err
	}
	schemaBytes, err := schemaFS.ReadFile("schemas/" + schemaName)
	if err != nil {
		return err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(denyLoader{})
	var doc any
	if err = json.Unmarshal(schemaBytes, &doc); err != nil {
		return err
	}
	if err = c.AddResource("https://aok.invalid/modelcatalog/"+schemaName, doc); err != nil {
		return err
	}
	s, err := c.Compile("https://aok.invalid/modelcatalog/" + schemaName)
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
		return fmt.Errorf("catalog schema: %w", err)
	}
	return nil
}

type denyLoader struct{}

func (denyLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference is forbidden: %s", url)
}

// schemaFS contains only local JSON schemas; external references are rejected.
//
//go:embed schemas/*.json
var schemaFS embed.FS
