package hostbridge

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/pelletier/go-toml/v2"
	"sort"
	"strings"
)

type ConfigurationCensus struct {
	Version      string   `json:"version"`
	Path         string   `json:"path"`
	SHA256       string   `json:"sha256"`
	DisabledKeys []string `json:"disabledKeys"`
}

// CodexCensus reveals integration names only to its caller, never secret values.
// The source configuration remains outside the captured evidence bundle.
func CodexCensus(path string) (ConfigurationCensus, error) {
	c := ConfigurationCensus{Version: Version, Path: path, DisabledKeys: []string{}}
	raw, e := ReadFile(path)
	if e != nil {
		return c, e
	}
	c.SHA256 = Hash(raw)
	var config map[string]any
	if e = toml.Unmarshal(raw, &config); e != nil {
		return c, fmt.Errorf("native configuration is not valid TOML")
	}
	if provider, ok := config["model_provider"].(string); ok && provider != "openai" {
		return c, fmt.Errorf("native qualification requires the default subscription provider")
	}
	for _, group := range []string{"mcp_servers", "plugins"} {
		if entries, ok := config[group].(map[string]any); ok {
			for key := range entries {
				quoted, _ := json.Marshal(key)
				c.DisabledKeys = append(c.DisabledKeys, group+"."+string(quoted)+".enabled")
			}
		}
	}
	sort.Strings(c.DisabledKeys)
	return c, nil
}
func auditCodexAmbient(p Policy) error {
	found := false
	for _, a := range p.ConfigurationAssets {
		raw, e := assetBytes(p.root, a)
		if e != nil {
			return e
		}
		var census ConfigurationCensus
		if usage.Decode(raw, &census) != nil || census.Version != Version {
			continue
		}
		actual, e := CodexCensus(census.Path)
		if e != nil {
			return e
		}
		if Digest(actual) != Digest(census) {
			return fmt.Errorf("native configuration changed since census")
		}
		for _, key := range actual.DisabledKeys {
			v, ok := p.CodexConfig[key]
			if !ok || strings.TrimSpace(string(v)) != "false" {
				return fmt.Errorf("ambient integration has not been explicitly disabled")
			}
		}
		found = true
	}
	if !found {
		return fmt.Errorf("native configuration census absent")
	}
	return nil
}
