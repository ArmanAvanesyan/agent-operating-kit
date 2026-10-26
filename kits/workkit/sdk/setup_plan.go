package sdk

import (
	"context"
	"errors"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"sort"
	"strings"
	"time"
)

func setupInputs(o s.Observation, raw []byte, kind string, asOf string) s.Report {
	r := check.NewReport(kind, asOf)
	r.Inputs["observation"] = check.Hash(o)
	r.Inputs["desired"] = SHA256(raw)
	r.Inputs["connection"] = string(o.ConnectionRef.Sha256)
	r.Inputs["configuration"] = o.ConfigurationSHA256
	return r
}
func EvaluateSetup(o s.Observation, desiredRaw []byte, asOf string) (s.Report, error) {
	r := setupInputs(o, desiredRaw, "SetupEvaluation", asOf)
	if e := check.Validate(o); e != nil {
		return r, e
	}
	if _, e := time.Parse(time.RFC3339Nano, asOf); e != nil {
		return r, e
	}
	var d api.DesiredSetup
	if e := setupDocument(desiredRaw, "DesiredSetup", &d); e != nil {
		return r, e
	}
	if d.Spec.ConnectionRef != o.ConnectionRef || d.Spec.AllowDestructive || d.Spec.ContainerId == "" {
		return r, errors.New("desired exact connection and existing container required; destructive setup excluded")
	}
	check.Fresh(&r, o, asOf, 300)
	for _, cat := range []string{"containers", "fields"} {
		if !check.Category(o, cat) {
			check.Block(&r, "metadata.incomplete", cat, "complete category required")
		} else {
			check.OK(&r, "category:"+cat)
		}
	}
	if len(o.Diagnostics) > 0 {
		check.Block(&r, "metadata.diagnostics", "observation", "metadata diagnostics require review")
	}
	if container, ok := check.Find(o, "container", d.Spec.ContainerId); !ok || d.Spec.ContainerId != o.Scope.ContainerID && container.ParentID != o.Scope.ContainerID {
		check.Block(&r, "container.missing", d.Spec.ContainerId, "selected native container was not observed")
	} else {
		check.OK(&r, "container")
	}
	allowed := o.Scope.Provider == "github" || o.Scope.Provider == "azure-devops"
	if o.Scope.Provider == "azure-devops" {
		inherited := false
		for _, v := range o.Entities {
			if v.Kind == "process" && v.ParentID == d.Spec.ContainerId && v.NativeType == "inherited" {
				inherited = true
			}
		}
		if !check.Category(o, "processes") || !inherited {
			check.Block(&r, "azure.process", "process", "existing associated inherited process required")
		}
	}
	seen := map[string]bool{}
	names := map[string]bool{}
	change := func(op, parent, logical string, value any) {
		b := engine.Encode(value)
		id := check.Hash(struct{ Operation, Parent, Logical, Value string }{op, parent, logical, check.Canonical(b)})
		r.Changes = append(r.Changes, s.Change{ID: id, Operation: op, ParentID: parent, LogicalID: logical, Value: b})
	}
	for _, f := range d.Spec.Fields {
		id := string(f.LogicalId)
		if seen[id] || names[f.Name] || f.Name == "" {
			return r, errors.New("duplicate desired field identity or name")
		}
		seen[id] = true
		names[f.Name] = true
		wanted := map[string]string{"text": "string", "number": "number", "single-select": "enum"}[f.Type]
		if wanted == "" {
			check.Block(&r, "field.type", id, "date/iteration setup not implemented; unsupported native constraints remain unknown")
			continue
		}
		opts := map[string]bool{}
		for _, v := range f.Options {
			if v == "" || opts[v] {
				return r, errors.New("duplicate or empty option")
			}
			opts[v] = true
		}
		if wanted != "enum" && len(opts) > 0 {
			return r, errors.New("options require enum field")
		}
		if f.NativeId == nil {
			collision := false
			for _, v := range o.Entities {
				if v.Kind == "field" && v.ParentID == d.Spec.ContainerId && v.Name == f.Name {
					collision = true
				}
			}
			if collision {
				check.Block(&r, "field.ambiguous", id, "existing fields must be selected by immutable native ID")
				continue
			}
			if !allowed {
				check.Block(&r, "setup.unsupported", id, "administrative create requires manual provider setup")
				continue
			}
			sorted := append([]string{}, f.Options...)
			sort.Strings(sorted)
			change("field.create", d.Spec.ContainerId, id, map[string]any{"name": f.Name, "type": f.Type, "options": sorted})
			continue
		}
		field, ok := check.Find(o, "field", *f.NativeId)
		if !ok || field.ParentID != d.Spec.ContainerId || field.Name != f.Name || field.Type != wanted {
			check.Block(&r, "field.mismatched", id, "exact field identity, parent, name and type required")
			continue
		}
		existing := map[string]bool{}
		for _, v := range field.Options {
			if v.ID == "" || v.Name == "" || existing[v.Name] {
				check.Block(&r, "option.ambiguous", id, "native options require unique IDs and names")
			}
			existing[v.Name] = true
		}
		for _, v := range f.Options {
			if !existing[v] {
				if !allowed {
					check.Block(&r, "setup.unsupported", id, "manual option setup required")
				} else {
					change("option.create", field.ID, id, v)
				}
			}
		}
		check.OK(&r, "field:"+id)
	}
	if len(d.Spec.Views) > 0 {
		check.Block(&r, "views.required", "views", "required views must be verified through separately bound view evidence")
	}
	sort.Slice(r.Changes, func(i, j int) bool { return r.Changes[i].ID < r.Changes[j].ID })
	if len(r.Changes) > 0 && r.Status == "ok" {
		r.Status = "partial"
	}
	return r, nil
}
func PlanSetup(ctx context.Context, observation, desired, asOf, out string) (s.Report, error) {
	if e := setupOutput(out, observation); e != nil {
		return s.Report{}, e
	}
	o, assets, e := LoadSetupObservation(ctx, observation)
	if e != nil {
		return s.Report{}, e
	}
	raw, e := ReadLocal(ctx, desired)
	if e != nil {
		return s.Report{}, e
	}
	r, e := EvaluateSetup(o, raw, asOf)
	if e != nil {
		return r, e
	}
	inputs := map[string][]byte{"observation.json": engine.Encode(o), "desired.json": raw}
	plan := api.SetupPlan{APIVersion: api.Version, Kind: "SetupPlan", Metadata: api.Metadata{Id: "setup-" + check.Hash(r.Inputs)[:24], Revision: 1}, Spec: api.SetupPlanSpec{ConnectionRef: o.ConnectionRef, Baseline: api.Asset{Path: "assets/observation.json", Sha256: api.Sha(SHA256(engine.Encode(o)))}, DesiredSha256: api.Sha(SHA256(raw)), Operations: []api.Ref{}, CreatedAt: api.Time(asOf)}}
	inputs["setup-plan.json"] = engine.Encode(plan)
	for n, b := range assets {
		inputs["native/"+n] = b
	}
	e = writeSetupReport(ctx, out, r, inputs)
	return r, e
}

// ValidateSetupPlan recomputes a plan from its pinned input bytes, never trusts
// supplied checks, status or operation IDs. Input asset hashes are rechecked.
func ValidateSetupPlan(ctx context.Context, root, asOf string) (s.Report, error) {
	files, e := loadSetupReportBundle(ctx, root)
	if e != nil {
		return s.Report{}, e
	}
	return validateCapturedSetupPlan(files, asOf)
}
func validateCapturedSetupPlan(files map[string][]byte, asOf string) (s.Report, error) {
	var o s.Observation
	var old s.Report
	if DecodeRead(files["assets/observation.json"], &o) != nil || DecodeRead(files["report.json"], &old) != nil {
		return old, errors.New("plan input missing")
	}
	native := map[string][]byte{}
	for n, b := range files {
		if strings.HasPrefix(n, "assets/native/") {
			native[strings.TrimPrefix(n, "assets/native/")] = b
		}
	}
	if e := validateSetupProvenance(o, native); e != nil {
		return old, e
	}
	expected, e := EvaluateSetup(o, files["assets/desired.json"], old.AsOf)
	if e != nil || check.Hash(expected) != check.Hash(old) {
		return old, errors.New("plan does not match recomputed inputs")
	}
	var domainPlan api.SetupPlan
	if e = setupDocument(files["assets/setup-plan.json"], "SetupPlan", &domainPlan); e != nil {
		return old, e
	}
	if domainPlan.Spec.ConnectionRef != o.ConnectionRef || domainPlan.Spec.DesiredSha256 != api.Sha(SHA256(files["assets/desired.json"])) || domainPlan.Spec.CreatedAt != api.Time(old.AsOf) || len(domainPlan.Spec.Operations) != 0 || domainPlan.Spec.Baseline.Path != "assets/observation.json" || domainPlan.Spec.Baseline.Sha256 != api.Sha(SHA256(files["assets/observation.json"])) {
		return old, errors.New("domain setup plan binding mismatch")
	}
	return EvaluateSetup(o, files["assets/desired.json"], asOf)
}
func reportExit(r s.Report) int {
	if r.Status == "blocked" {
		for _, d := range r.Diagnostics {
			if d.Code == "refresh-needed" {
				return 4
			}
		}
		return 3
	}
	if r.Status == "partial" {
		return 6
	}
	return 0
}
