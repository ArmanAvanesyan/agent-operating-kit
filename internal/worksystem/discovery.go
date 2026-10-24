// Package worksystem defines the cancellable, read-only boundary used by setup
// and binding validation. It deliberately has no provider transport of its own.
package worksystem

import (
	"context"
	"errors"
	"fmt"
)

const SupportLevel = "scaffolded"

type Request struct {
	Provider      string `json:"provider"`
	Owner         string `json:"owner,omitempty"`
	ProjectNumber int    `json:"projectNumber,omitempty"`
	Organization  string `json:"organization,omitempty"`
	Project       string `json:"project,omitempty"`
	Team          string `json:"team,omitempty"`
	Board         string `json:"board,omitempty"`
	WorkItemType  string `json:"workItemType,omitempty"`
}

// Observation is provider data returned by an injected read-only connector.
// Setup never accepts commands, credentials, or a mutation callback.
type Observation struct{ Report map[string]any }
type Reader interface {
	ReadOnlyDiscovery(context.Context, Request) (Observation, error)
}

func Setup(ctx context.Context, reader Reader, req Request) (map[string]any, error) {
	if ctx == nil {
		return nil, errors.New("cancellable context is required")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if reader == nil {
		return nil, errors.New("read-only work-system discovery reader is required")
	}
	if req.Provider != "github" && req.Provider != "azure-devops" {
		return nil, fmt.Errorf("unsupported work-system provider: %s", req.Provider)
	}
	if req.Provider == "github" && (req.Owner == "" || (req.ProjectNumber <= 0 && req.Project == "")) {
		return nil, errors.New("GitHub discovery requires owner and project number or title")
	}
	if req.Provider == "azure-devops" && (req.Organization == "" || req.Project == "") {
		return nil, errors.New("Azure DevOps discovery requires organization and project")
	}
	obs, e := reader.ReadOnlyDiscovery(ctx, req)
	if e != nil {
		return nil, e
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	if obs.Report == nil {
		return nil, errors.New("work-system reader returned no report")
	}
	// Reassert the safety metadata at the trust boundary. A provider adapter may
	// report observations but cannot upgrade support or claim that it wrote state.
	obs.Report["provider"] = req.Provider
	obs.Report["supportLevel"] = SupportLevel
	obs.Report["remoteMutation"] = false
	if !reportConformant(obs.Report, req.Provider) {
		obs.Report["conformant"] = false
		checks, _ := obs.Report["checks"].([]any)
		checks = append(checks, map[string]any{"name": "reader-report", "status": "mismatched", "detail": "read-only discovery report is incomplete or contains an unsatisfied check"})
		obs.Report["checks"] = checks
		obs.Report["binding"] = nil
	}
	if req.Provider == "azure-devops" {
		process := asMap(asMap(obs.Report["binding"])["process"])
		if process["customizationType"] == "system" {
			obs.Report["conformant"] = false
			checks, _ := obs.Report["checks"].([]any)
			checks = append(checks, map[string]any{"name": "process", "status": "mismatched", "detail": "Azure DevOps system processes cannot be customized"})
			obs.Report["checks"] = checks
			obs.Report["binding"] = nil
		}
	}
	return obs.Report, nil
}

func reportConformant(r map[string]any, provider string) bool {
	if r["conformant"] != true {
		return false
	}
	checks, ok := r["checks"].([]any)
	if !ok || len(checks) == 0 {
		return false
	}
	for _, v := range checks {
		c := asMap(v)
		if c == nil || c["status"] != "satisfied" {
			return false
		}
	}
	b := asMap(r["binding"])
	if b == nil || b["provider"] != provider || b["supportLevel"] != SupportLevel {
		return false
	}
	if provider == "github" {
		if b["owner"] == nil || b["projectNumber"] == nil || b["projectId"] == nil || b["url"] == nil || len(asMap(b["fields"])) == 0 {
			return false
		}
	}
	if provider == "azure-devops" {
		for _, k := range []string{"organization", "project", "team", "board", "process", "workItemType", "states", "fields"} {
			if b[k] == nil {
				return false
			}
		}
	}
	return true
}

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
