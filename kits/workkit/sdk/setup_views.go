package sdk

import (
	"context"
	"errors"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"net/url"
	"path"
	"strings"
)

func PlanSetupView(ctx context.Context, observation, view, parent, existing, operation, asOf, out string) (s.ViewDraft, error) {
	draft := s.ViewDraft{Report: check.NewReport("ViewEvaluation", asOf), ParentID: parent, ExistingID: existing, Differences: []string{}, ManualInstructions: []string{}}
	if e := setupOutput(out, observation); e != nil {
		return draft, e
	}
	o, assets, e := LoadSetupObservation(ctx, observation)
	if e != nil {
		return draft, e
	}
	raw, e := ReadLocal(ctx, view)
	if e != nil {
		return draft, e
	}
	var v api.ViewDefinition
	if e = setupDocument(raw, "ViewDefinition", &v); e != nil {
		return draft, e
	}
	draft.DefinitionSHA256 = SHA256(raw)
	draft.Report.Inputs = map[string]string{"observation": check.Hash(o), "view": SHA256(raw)}
	check.Fresh(&draft.Report, o, asOf, 300)
	if v.Spec.Location != "provider" || v.Spec.ConnectionRef == nil || *v.Spec.ConnectionRef != o.ConnectionRef || !setupContainer(o, parent) {
		return draft, errors.New("provider view requires exact existing connection and container")
	}
	if operation != "plan-create" && operation != "plan-update" {
		return draft, errors.New("unsupported view operation")
	}
	if operation == "plan-update" {
		if existing == "" {
			return draft, errors.New("view update requires exact existing native ID")
		}
		if entity, ok := check.Find(o, "view", existing); !ok || entity.ParentID != parent {
			check.Block(&draft.Report, "views.identity", "view", "native view not observed in selected parent")
		}
	}
	for _, id := range append(append(append([]string{}, v.Spec.Columns...), v.Spec.GroupBy...), v.Spec.SortBy...) {
		if field, ok := check.Find(o, "field", id); !ok || field.ParentID != parent {
			check.Block(&draft.Report, "views.field", id, "unknown native view field")
		}
	}
	if !check.Category(o, "views") {
		check.Block(&draft.Report, "views.capability", "views", "view discovery/support unavailable")
	}
	// Drafts retain the full requested definition; they never claim an upstream
	// administrative endpoint or executable query from display-only metadata.
	check.Block(&draft.Report, "views.dispatch", "views", "provider view execution remains unavailable pending P5 capability qualification")
	draft.Differences = append(draft.Differences, "requested "+operation+" for exact native parent", "requested-layout="+v.Spec.Layout, "requested-query-dialect="+v.Spec.Query.Dialect, "native comparison remains unknown unless a provider-specific definition translator is available; original definition retained")
	draft.ManualInstructions = append(draft.ManualInstructions, "Inspect the captured native view metadata and configure this definition manually in the selected provider container. Re-capture and verify afterwards; no automatic fallback or mutation.")
	inputs := map[string][]byte{"observation.json": engine.Encode(o), "view.json": raw}
	for n, b := range assets {
		inputs["native/"+n] = b
	}
	e = writeSetupReport(ctx, out, draft, inputs)
	return draft, e
}

// InspectSetupView validates the domain definition without interpreting provider
// query text as an executable URL, query or command.
func InspectSetupView(raw []byte) (api.ViewDefinition, error) {
	var v api.ViewDefinition
	e := setupDocument(raw, "ViewDefinition", &v)
	return v, e
}
func validSetupURL(raw string, o s.Observation) bool {
	u, e := url.Parse(raw)
	origin, e2 := url.Parse(o.Scope.Origin)
	if e != nil || e2 != nil || u.User != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || path.Clean(u.Path) != u.Path || strings.Contains(u.EscapedPath(), "%") {
		return false
	}
	hostAllowed := u.Host == origin.Host
	if o.Scope.Provider == "github" && o.Scope.Origin == "https://api.github.com" && u.Host == "github.com" {
		hostAllowed = true
	}
	return hostAllowed && (o.Scope.BasePath == "" || strings.HasPrefix(u.Path, o.Scope.BasePath+"/"))
}

// InspectSetupProviderView returns only URLs actually present on the captured
// exact native view. It never guesses a URL from labels or opens a browser.
func InspectSetupProviderView(ctx context.Context, observation, definition, id, asOf string) (s.ViewInspection, error) {
	result := s.ViewInspection{Report: check.NewReport("ViewInspection", asOf), NativeID: id, SourceURLs: []string{}}
	o, files, e := LoadSetupObservation(ctx, observation)
	if e != nil {
		return result, e
	}
	raw, e := ReadLocal(ctx, definition)
	if e != nil {
		return result, e
	}
	if e = setupDocument(raw, "ViewDefinition", &result.Definition); e != nil {
		return result, e
	}
	v := result.Definition
	if v.Spec.Location != "provider" || v.Spec.ConnectionRef == nil || *v.Spec.ConnectionRef != o.ConnectionRef || id == "" {
		return result, errors.New("exact provider view definition and native ID required")
	}
	result.Report.Inputs = map[string]string{"observation": check.Hash(o), "view": SHA256(raw)}
	check.Fresh(&result.Report, o, asOf, 300)
	entity, ok := check.Find(o, "view", id)
	if !ok || !setupContainer(o, entity.ParentID) {
		check.Block(&result.Report, "views.identity", id, "selected native view not captured")
		return result, nil
	}
	node, e := check.Pointer(files[entity.Provenance.Asset.Path], entity.Provenance.Pointer)
	if e != nil {
		return result, e
	}
	m, _ := node.(map[string]any)
	candidate, _ := m["url"].(string)
	if candidate != "" && validSetupURL(candidate, o) {
		result.SourceURLs = append(result.SourceURLs, candidate)
	} else {
		check.Block(&result.Report, "views.url", id, "no captured URL satisfies exact provider origin/base-path policy")
	}
	return result, nil
}

func setupContainer(o s.Observation, id string) bool {
	v, ok := check.Find(o, "container", id)
	return ok && (id == o.Scope.ContainerID || v.ParentID == o.Scope.ContainerID)
}
