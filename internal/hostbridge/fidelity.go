package hostbridge

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
)

func configurationFidelity(profile contracts.ExecutionProfile, p Policy) error {
	c := p.Configuration
	if p.Version == VersionV2 && (c.Effort != nil || c.ReasoningBudgetTokens != nil || c.Generation != nil || c.ServiceTier != nil || c.ModelRevision != nil || c.Deployment != nil || c.ToolProfile != nil) {
		return fmt.Errorf("v2 host protocol cannot enforce requested configuration dimensions")
	}
	if c.ModelRevision != nil || c.ReasoningBudgetTokens != nil || c.Generation != nil || c.ServiceTier != nil || c.Deployment != nil {
		return fmt.Errorf("unobserved configuration dimensions cannot be represented as enforced")
	}
	if c.ReasoningMode == nil || *c.ReasoningMode != profile.Model.Reasoning.Mode {
		return fmt.Errorf("reasoning identity mismatch")
	}
	if profile.Model.Provider != nil && (c.Provider == nil || *c.Provider != *profile.Model.Provider) {
		return fmt.Errorf("provider identity mismatch")
	}
	if c.ToolProfile != nil && *c.ToolProfile != Digest(p.Tools) {
		return fmt.Errorf("tool-profile identity must bind exact approved tool set")
	}
	if profile.Model.Reasoning.Mode == "effort" {
		if profile.Model.Reasoning.Effort == nil || c.Effort == nil || *c.Effort != *profile.Model.Reasoning.Effort {
			return fmt.Errorf("effort mismatch")
		}
		if p.Host == "codex" {
			var effort string
			if json.Unmarshal(p.CodexConfig["model_reasoning_effort"], &effort) != nil || effort != *c.Effort {
				return fmt.Errorf("Codex effort override must match exact profile")
			}
		}
	} else if c.Effort != nil {
		return fmt.Errorf("provider-default effort must remain unknown")
	}
	return nil
}
