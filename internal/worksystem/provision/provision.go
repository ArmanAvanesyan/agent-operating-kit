// Package provision prepares deterministic additive work-system plans. Its
// effect boundary requires protected authority, one-send consumption and readback.
package provision

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

const Version = "aok-work-system-plan/v1"

type Scope struct {
	Provider    string `json:"provider"`
	Origin      string `json:"origin"`
	TenantID    string `json:"tenantId"`
	ProjectID   string `json:"projectId"`
	ProcessID   string `json:"processId"`
	ProcessType string `json:"processType"`
}
type Resource struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	Name     string          `json:"name"`
	ParentID string          `json:"parentId"`
	Value    json.RawMessage `json:"value"`
}
type Observation struct {
	Version    string     `json:"version"`
	Scope      Scope      `json:"scope"`
	ObservedAt string     `json:"observedAt"`
	Revision   string     `json:"revision"`
	Complete   bool       `json:"complete"`
	Resources  []Resource `json:"resources"`
}
type Desired struct {
	Version   string     `json:"version"`
	Scope     Scope      `json:"scope"`
	Resources []Resource `json:"resources"`
}
type Change struct {
	ID        string   `json:"id"`
	Operation string   `json:"operation"`
	Resource  Resource `json:"resource"`
}
type Plan struct {
	Version              string   `json:"version"`
	Scope                Scope    `json:"scope"`
	AsOf                 string   `json:"asOf"`
	ObservationSHA256    string   `json:"observationSha256"`
	CurrentStateSHA256   string   `json:"currentStateSha256"`
	DesiredSHA256        string   `json:"desiredSha256"`
	ExpectedRevision     string   `json:"expectedRevision"`
	Changes              []Change `json:"changes"`
	Status               string   `json:"status"`
	Reasons              []string `json:"reasons"`
	Digest               string   `json:"digest"`
	AuthorizationGranted bool     `json:"authorizationGranted"`
	ExecutionReady       bool     `json:"executionReady"`
}

func Hash(v any) string              { return usage.Digest(v) }
func stateHash(o Observation) string { o.ObservedAt = ""; return Hash(o) }
func validScope(s Scope) bool {
	u, e := url.Parse(s.Origin)
	return e == nil && u.Scheme == "https" && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == "" && u.Path == "" && (s.Provider == "github" || s.Provider == "azure-devops") && s.TenantID != "" && s.ProjectID != ""
}
func Build(o Observation, d Desired, at time.Time) (Plan, error) {
	p := Plan{Version: Version, Scope: o.Scope, AsOf: at.UTC().Format(time.RFC3339Nano), ObservationSHA256: Hash(o), CurrentStateSHA256: stateHash(o), DesiredSHA256: Hash(d), ExpectedRevision: o.Revision, Changes: []Change{}, Status: "draft", Reasons: []string{}}
	if at.IsZero() || o.Version != Version || d.Version != Version || !validScope(o.Scope) || o.Scope != d.Scope {
		return p, fmt.Errorf("version, as-of and exact existing resource scope required")
	}
	block := func(reason string) { p.Status = "blocked"; p.Reasons = append(p.Reasons, reason) }
	t, e := time.Parse(time.RFC3339Nano, o.ObservedAt)
	if e != nil || at.Before(t) || at.Sub(t) >= 5*time.Minute {
		block("refresh-needed")
	}
	if !o.Complete || o.Revision == "" {
		block("complete versioned observation required")
	}
	if o.Scope.Provider == "azure-devops" && (o.Scope.ProcessID == "" || o.Scope.ProcessType != "inherited") {
		block("Azure customization requires an existing inherited process")
	}
	existing := map[string]Resource{}
	names := map[string]Resource{}
	for _, r := range o.Resources {
		if r.ID == "" || r.Name == "" || existing[r.ID].ID != "" {
			return p, fmt.Errorf("invalid observed resource identity")
		}
		existing[r.ID] = r
		key := Hash(struct{ Kind, Name, Parent string }{r.Kind, r.Name, r.ParentID})
		if _, exists := names[key]; exists {
			return p, fmt.Errorf("ambiguous observed resource name")
		}
		names[key] = r
	}
	seen := map[string]bool{}
	for _, r := range d.Resources {
		key := Hash(struct{ Kind, Name, Parent string }{r.Kind, r.Name, r.ParentID})
		if r.Name == "" || seen[key] || len(r.Value) == 0 {
			return p, fmt.Errorf("invalid or duplicate desired resource")
		}
		seen[key] = true
		if e := usage.Decode(r.Value, new(any)); e != nil {
			return p, e
		}
		kindAllowed := o.Scope.Provider == "github" && (r.Kind == "project-field" || r.Kind == "field-option") || o.Scope.Provider == "azure-devops" && (r.Kind == "process-field" || r.Kind == "process-state")
		if !kindAllowed {
			block("unsupported additive resource kind: " + r.Kind)
			continue
		}
		if r.ParentID != "" {
			if _, ok := existing[r.ParentID]; !ok {
				block("parent must be an observed existing resource")
				continue
			}
		}
		if r.ID != "" {
			old, ok := existing[r.ID]
			if !ok || Hash(old) != Hash(r) {
				block("replacement and modification of existing resources excluded")
			}
			continue
		}
		if old, ok := names[key]; ok {
			if Hash(old.Value) != Hash(r.Value) {
				block("existing same-name resource differs; destructive replacement excluded")
			}
			continue
		}
		c := Change{Operation: "create-additive", Resource: r}
		c.ID = Hash(struct {
			Scope    Scope
			Resource Resource
		}{o.Scope, r})
		p.Changes = append(p.Changes, c)
	}
	sort.Slice(p.Changes, func(i, j int) bool { return p.Changes[i].ID < p.Changes[j].ID })
	sort.Strings(p.Reasons)
	p.Digest = Hash(p)
	return p, nil
}
func Validate(p Plan) error {
	saved := p.Digest
	p.Digest = ""
	if p.Version != Version || p.AuthorizationGranted || p.ExecutionReady || saved != Hash(p) || !validScope(p.Scope) || p.Scope.Provider == "azure-devops" && (p.Scope.ProcessType != "inherited" || p.Scope.ProcessID == "") || p.Status != "draft" && p.Status != "blocked" {
		return fmt.Errorf("invalid provision plan")
	}
	seen := map[string]bool{}
	for _, c := range p.Changes {
		if c.Operation != "create-additive" || c.ID != Hash(struct {
			Scope    Scope
			Resource Resource
		}{p.Scope, c.Resource}) || !allowedKind(p.Scope, c.Resource.Kind) || c.Resource.ID != "" || seen[c.ID] {
			return fmt.Errorf("invalid changes")
		}
		seen[c.ID] = true
	}
	return nil
}

// Authority is externally supplied and must check protected grants, revocations,
// operation qualification and current scope. No task boolean can implement it.
type Authority interface {
	Verify(context.Context, Plan, Change, time.Time) error
}
type Boundary interface {
	Consume(context.Context, string, string) error
	Record(context.Context, string, string) error
}
type Writer interface {
	Observe(context.Context, Scope) (Observation, error)
	Send(context.Context, Scope, Change, string) error
	Readback(context.Context, Scope, Change) (Resource, error)
}
type Receipt struct {
	Version              string `json:"version"`
	PlanSHA256           string `json:"planSha256"`
	ChangeID             string `json:"changeId"`
	Disposition          string `json:"disposition"`
	ReadbackID           string `json:"readbackId"`
	AuthorizationGranted bool   `json:"authorizationGranted"`
	ExecutionReady       bool   `json:"executionReady"`
}

// Apply sends at most one additive operation. A failed/ambiguous call spends its
// stable logical ID and must be reconciled; no automatic resend or rollback.
func Apply(ctx context.Context, p Plan, index int, auth Authority, b Boundary, w Writer) (Receipt, error) {
	r := Receipt{Version: Version, PlanSHA256: p.Digest, Disposition: "blocked"}
	if e := Validate(p); e != nil {
		return r, e
	}
	if p.Status != "draft" || index < 0 || index >= len(p.Changes) || auth == nil || b == nil || w == nil {
		return r, fmt.Errorf("qualified writer, authority and durable boundary required")
	}
	c := p.Changes[index]
	r.ChangeID = c.ID
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if e := auth.Verify(ctx, p, c, time.Now().UTC()); e != nil {
		return r, e
	}
	o, e := w.Observe(ctx, p.Scope)
	if e != nil {
		return r, e
	}
	observedAt, parseErr := time.Parse(time.RFC3339Nano, o.ObservedAt)
	now := time.Now().UTC()
	if parseErr != nil || now.Before(observedAt) || now.Sub(observedAt) >= 5*time.Minute {
		return r, fmt.Errorf("fresh provider observation required")
	}
	if o.Scope != p.Scope || !o.Complete || o.Revision != p.ExpectedRevision || stateHash(o) != p.CurrentStateSHA256 {
		return r, fmt.Errorf("provisioning drift; rebuild plan")
	}
	if e = auth.Verify(ctx, p, c, time.Now().UTC()); e != nil {
		return r, e
	}
	if e = b.Consume(ctx, c.ID, Hash(struct {
		Plan   string
		Change Change
	}{p.Digest, c})); e != nil {
		return r, e
	}
	r.Disposition = "unknown-awaiting-reconciliation"
	if e = ctx.Err(); e != nil {
		return r, e
	}
	if e = w.Send(ctx, p.Scope, c, p.ExpectedRevision); e != nil {
		_ = b.Record(ctx, c.ID, r.Disposition)
		return r, e
	}
	id, e := w.Readback(ctx, p.Scope, c)
	if e != nil || !matches(c.Resource, id) {
		_ = b.Record(ctx, c.ID, r.Disposition)
		return r, fmt.Errorf("readback required; reconcile without resend")
	}
	r.ReadbackID = id.ID
	r.Disposition = "readback-observed"
	if e = b.Record(ctx, c.ID, r.Disposition); e != nil {
		return r, e
	}
	return r, nil
}
func Reconcile(ctx context.Context, p Plan, index int, auth Authority, b Boundary, w Writer) (Receipt, error) {
	r := Receipt{Version: Version, PlanSHA256: p.Digest, Disposition: "unknown-awaiting-reconciliation"}
	if e := Validate(p); e != nil {
		return r, e
	}
	if index < 0 || index >= len(p.Changes) || auth == nil || b == nil || w == nil {
		return r, fmt.Errorf("qualified reconciliation required")
	}
	c := p.Changes[index]
	r.ChangeID = c.ID
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if e := auth.Verify(ctx, p, c, time.Now().UTC()); e != nil {
		return r, e
	}
	id, e := w.Readback(ctx, p.Scope, c)
	if e != nil || !matches(c.Resource, id) {
		return r, fmt.Errorf("effect remains ambiguous")
	}
	r.ReadbackID = id.ID
	r.Disposition = "readback-observed"
	return r, b.Record(ctx, c.ID, r.Disposition)
}

func allowedKind(s Scope, k string) bool {
	return s.Provider == "github" && (k == "project-field" || k == "field-option") || s.Provider == "azure-devops" && (k == "process-field" || k == "process-state")
}

func matches(desired, observed Resource) bool {
	if observed.ID == "" {
		return false
	}
	observed.ID = ""
	var a, b any
	if usage.Decode(desired.Value, &a) != nil || usage.Decode(observed.Value, &b) != nil {
		return false
	}
	return desired.Kind == observed.Kind && desired.Name == observed.Name && desired.ParentID == observed.ParentID && Hash(a) == Hash(b)
}
