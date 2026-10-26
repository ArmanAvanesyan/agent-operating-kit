package workkit

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/worksystem/provision"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"time"
)

// ProvisioningRoute delegates to an existing authoritative provisioning namespace.
// No operation is copied to a second active WorkKit outbox.
type ProvisioningRoute struct {
	Store         o.Store
	Plan          provision.Plan
	Index         int
	Authority     provision.Authority
	Boundary      provision.ActivatedBoundary
	WriterFactory func(func(context.Context) error) (provision.Writer, error)
	Current       func(context.Context, sdk.PreparedSetupOperation, o.Enrollment, []byte) error
}
type SetupOperationBackend struct {
	Bootstrap func(context.Context, sdk.PreparedSetupOperation, o.Enrollment) (ProvisioningRoute, error)
}

func (b SetupOperationBackend) ExecuteSetup(ctx context.Context, p sdk.PreparedSetupOperation, policy, enraw []byte) (o.Observation, error) {
	en, err := sdk.ValidateSetupOperationAuthority(p, policy, enraw)
	if err != nil {
		return o.Observation{}, err
	}
	h := p.Handoff()
	result := o.Observation{Version: o.Version, Kind: "OperationObservation", OperationID: h.Change.ID, Digest: p.Digest(), Store: en.Store, State: "blocked", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Evidence: []api.Asset{}, Diagnostics: []api.Diagnostic{}}
	if b.Bootstrap == nil {
		return result, &sdk.Error{Code: "setup.unavailable", Message: "protected provisioning bootstrap unavailable", ExitCode: 3}
	}
	route, err := b.Bootstrap(ctx, p, en)
	if err != nil {
		return result, err
	}
	if route.Store != en.Store || route.Boundary.RouteSHA256 != MutationNamespaceSHA256(en.Store) || route.Current == nil || route.Authority == nil || route.Boundary.Activation == nil || route.Boundary.Journal == nil || route.WriterFactory == nil || route.Index < 0 || route.Index >= len(route.Plan.Changes) {
		return result, errors.New("existing exact authoritative provisioning boundary required")
	}
	c := route.Plan.Changes[route.Index]
	scope := route.Plan.Scope
	expectedKind := "project-field"
	if scope.Provider == "azure-devops" {
		expectedKind = "process-field"
	}
	if h.Change.Operation == "option.create" {
		if scope.Provider != "github" {
			return result, errors.New("Azure option adapter unsupported")
		}
		expectedKind = "field-option"
	}
	name := ""
	if h.Change.Operation == "field.create" {
		var value struct {
			Name    string   `json:"name"`
			Type    string   `json:"type"`
			Options []string `json:"options"`
		}
		if sdk.DecodeRead(h.Change.Value, &value) != nil || value.Name == "" {
			return result, errors.New("exact field specification required")
		}
		name = value.Name
	} else {
		if json.Unmarshal(h.Change.Value, &name) != nil || name == "" {
			return result, errors.New("exact option name required")
		}
	}
	if scope.Provider == "azure-devops" {
		matched := false
		for _, entity := range h.Observation.Entities {
			if entity.Kind == "process" && entity.ID == scope.ProcessID && entity.ParentID == scope.ProjectID && entity.NativeType == "inherited" {
				matched = true
			}
		}
		if !matched || scope.ProcessType != "inherited" {
			return result, errors.New("exact associated inherited process required")
		}
	}
	// Names are never existing-resource identities. Native parents and values must match.
	if scope.Provider != h.Observation.Scope.Provider || scope.Origin != h.Observation.Scope.Origin || scope.TenantID != h.Observation.Scope.TenantID || scope.ProjectID != h.Observation.Scope.ContainerID || route.Plan.ExpectedRevision != h.Observation.Revision || c.Resource.Name != name || c.Resource.ParentID != h.Change.ParentID || c.Operation != "create-additive" || c.Resource.Kind != expectedKind || string(c.Resource.Value) != string(h.Change.Value) {
		return result, errors.New("native provisioning plan differs from WorkKit handoff")
	}
	guard := func(c context.Context) error {
		if _, e := sdk.ValidateSetupOperationAuthority(p, policy, enraw); e != nil {
			return e
		}
		if e := route.Boundary.Activation.Check(c); e != nil {
			return e
		}
		if err := route.Current(c, p, en, policy); err != nil {
			return err
		}
		return route.Authority.Verify(c, route.Plan, route.Plan.Changes[route.Index], time.Now().UTC())
	}
	if err = guard(ctx); err != nil {
		return result, err
	}
	inner, err := route.WriterFactory(guard)
	if err != nil {
		return result, err
	}
	if inner == nil {
		return result, errors.New("qualified provisioning writer unavailable")
	}
	writer := guardedProvisionWriter{inner: inner, guard: guard}
	auth := guardedProvisionAuthority{inner: route.Authority, guard: guard}
	var receipt provision.Receipt
	if en.Action == "apply" {
		receipt, err = provision.Apply(ctx, route.Plan, route.Index, auth, workkitProvisionBoundary{route.Boundary}, writer)
	} else {
		receipt, err = provision.Reconcile(ctx, route.Plan, route.Index, auth, workkitProvisionBoundary{route.Boundary}, writer)
	}
	result.State = receipt.Disposition
	if receipt.Disposition == "readback-observed" {
		result.State = "readback-observed"
	}
	return result, err
}

type guardedProvisionAuthority struct {
	inner provision.Authority
	guard func(context.Context) error
}

func (a guardedProvisionAuthority) Verify(ctx context.Context, p provision.Plan, c provision.Change, now time.Time) error {
	if err := a.guard(ctx); err != nil {
		return err
	}
	return a.inner.Verify(ctx, p, c, now)
}

type guardedProvisionWriter struct {
	inner provision.Writer
	guard func(context.Context) error
}

func (w guardedProvisionWriter) Observe(ctx context.Context, s provision.Scope) (provision.Observation, error) {
	if e := w.guard(ctx); e != nil {
		return provision.Observation{}, e
	}
	return w.inner.Observe(ctx, s)
}
func (w guardedProvisionWriter) Send(ctx context.Context, s provision.Scope, c provision.Change, r string) error {
	if e := w.guard(ctx); e != nil {
		return e
	}
	return w.inner.Send(ctx, s, c, r)
}
func (w guardedProvisionWriter) Readback(ctx context.Context, s provision.Scope, c provision.Change) (provision.Resource, error) {
	if e := w.guard(ctx); e != nil {
		return provision.Resource{}, e
	}
	return w.inner.Readback(ctx, s, c)
}

// Retain the existing v1 activation vocabulary. Domain-specific identities and
// digests distinguish provisioning; no new activation boundary is inferred.
type workkitProvisionBoundary struct{ provision.ActivatedBoundary }

func (b workkitProvisionBoundary) Consume(ctx context.Context, id, digest string) error {
	if b.Activation == nil || b.Journal == nil || b.Activation.ScopeSHA256() != b.RouteSHA256 {
		return errors.New("exact protected provisioning boundary required")
	}
	if err := b.Activation.Consume(ctx, "workkit-provision:"+id, "provider-send", digest); err != nil {
		return err
	}
	return b.Journal.Consume(ctx, id, digest)
}
