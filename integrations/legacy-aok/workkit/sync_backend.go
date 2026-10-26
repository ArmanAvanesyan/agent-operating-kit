package workkit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	y "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"sort"
	"time"
)

// SyncRouteAuthority is compiled protected enrollment. Current must verify exact
// global route/field ownership, fresh endpoint observations and externally trusted
// bindings, policy and revocation. Fixtures cannot install it in the standard CLI.
type SyncRouteAuthority struct {
	Current    func(context.Context, sdk.PreparedSync, o.Enrollment, []byte) error
	Activation *operations.Activation
}
type SyncBackend struct {
	Bootstrap func(context.Context, sdk.PreparedSync, o.Enrollment) (SyncRouteAuthority, error)
}

func (b SyncBackend) authority(ctx context.Context, p sdk.PreparedSync, e o.Enrollment, policy []byte) (SyncRouteAuthority, error) {
	if b.Bootstrap == nil {
		return SyncRouteAuthority{}, &sdk.Error{Code: "sync.unavailable", Message: "protected sync bootstrap unavailable", ExitCode: 3}
	}
	if _, err := sdk.ValidateSyncAuthority(p, policy, mustJSON(e)); err != nil {
		return SyncRouteAuthority{}, err
	}
	a, err := b.Bootstrap(ctx, p, e)
	if err != nil {
		return a, err
	}
	if a.Current == nil || a.Activation == nil || a.Activation.ScopeSHA256() != MutationNamespaceSHA256(e.Store) {
		return a, errors.New("protected exact namespace/current route authority required")
	}
	if err = a.Current(ctx, p, e, policy); err != nil {
		return a, err
	}
	return a, a.Activation.Check(ctx)
}
func (b SyncBackend) CurrentSync(ctx context.Context, p sdk.PreparedSync, e o.Enrollment, policy []byte) (y.State, error) {
	if _, err := b.authority(ctx, p, e, policy); err != nil {
		return y.State{}, err
	}
	j, err := writeback.OpenWorkkit(ctx, e.Store.Path, e.Store.Namespace, e.Store.Owner, false)
	if err != nil {
		return y.State{}, err
	}
	defer j.Close()
	if j.Identity != storeIdentity(e.Store) {
		return y.State{}, errors.New("store mismatch")
	}
	plan := p.Plan()
	revision, raw, err := j.Route(ctx, plan.RouteSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		var initial y.State
		if sdk.DecodeRead(p.Assets()["assets/state.json"], &initial) != nil || initial.Initialized || initial.Revision != 0 || len(initial.Baselines) > 0 || len(initial.Unresolved) > 0 || initial.CompletedWatermark != "" || initial.PendingPlanSHA256 != "" {
			return initial, errors.New("fresh route requires explicit empty initial state")
		}
		return initial, nil
	}
	if err != nil {
		return y.State{}, err
	}
	var state y.State
	if sdk.DecodeRead(raw, &state) != nil || state.Revision != revision || state.RouteSHA256 != plan.RouteSHA256 {
		return state, errors.New("stored route corrupt")
	}
	var prior y.State
	if sdk.DecodeRead(p.Assets()["assets/state.json"], &prior) != nil {
		return state, errors.New("pinned prior missing")
	}
	if string(mustJSON(state)) != string(mustJSON(prior)) && !((e.Action == "sync-reconcile" || e.Action == "sync-run") && state.PendingPlanSHA256 == p.Digest()) {
		return state, errors.New("route revision drift")
	}
	return state, nil
}
func (b SyncBackend) CommitSync(ctx context.Context, p sdk.PreparedSync, e o.Enrollment, policy []byte, next y.State, confirmed []string) error {
	prior, err := b.CurrentSync(ctx, p, e, policy)
	if err != nil {
		return err
	}
	expected, err := sdk.ProjectSyncState(p, prior, e.Action, confirmed)
	if err != nil || string(mustJSON(expected)) != string(mustJSON(next)) {
		return errors.New("route projection differs from confirmed operations")
	}
	a, err := b.authority(ctx, p, e, policy)
	if err != nil {
		return err
	}
	j, err := writeback.OpenWorkkitWritable(ctx, storeIdentity(e.Store))
	if err != nil {
		return err
	}
	defer j.Close()
	if err = a.Activation.Consume(ctx, e.ID+":settle", "provider-send", p.Digest()); err != nil {
		return err
	}
	targets := []string{}
	var config y.Configuration
	if sdk.DecodeRead(p.Assets()["assets/configuration.json"], &config) != nil {
		return errors.New("configuration missing")
	}
	for _, baseline := range next.Baselines {
		for _, co := range config.Correlations {
			if co.ID == baseline.CorrelationID {
				targets = append(targets, sdk.SHA256(mustJSON(struct {
					Target any
					Field  string
				}{co.Destination, baseline.Field})))
			}
		}
	}
	sort.Strings(targets)
	raw := mustJSON(next)
	return j.CommitRoute(ctx, p.Plan().RouteSHA256, prior.Revision, raw, e.ID, sdk.SHA256(raw), targets, confirmed, time.Now().UTC())
}
func (b SyncBackend) InspectSync(ctx context.Context, store o.Store, route string) (y.State, error) {
	j, err := writeback.OpenWorkkit(ctx, store.Path, store.Namespace, store.Owner, false)
	if err != nil {
		return y.State{}, err
	}
	defer j.Close()
	if j.Identity != storeIdentity(store) {
		return y.State{}, errors.New("store identity mismatch")
	}
	revision, raw, err := j.Route(ctx, route)
	if err != nil {
		return y.State{}, err
	}
	var state y.State
	if err = json.Unmarshal(raw, &state); err != nil || state.Revision != revision {
		return state, errors.New("route corrupt")
	}
	return state, nil
}

func (b SyncBackend) BeginSync(ctx context.Context, p sdk.PreparedSync, e o.Enrollment, policy []byte) (y.State, error) {
	prior, err := b.CurrentSync(ctx, p, e, policy)
	if err != nil {
		return prior, err
	}
	if e.Action != "sync-run" {
		return prior, errors.New("run action required")
	}
	next, err := sdk.PendingSyncState(p, prior)
	if err != nil {
		return prior, err
	}
	a, err := b.authority(ctx, p, e, policy)
	if err != nil {
		return prior, err
	}
	j, err := writeback.OpenWorkkitWritable(ctx, storeIdentity(e.Store))
	if err != nil {
		return prior, err
	}
	defer j.Close()
	if err = a.Activation.Consume(ctx, e.ID+":admit", "provider-send", p.Digest()); err != nil {
		return prior, err
	}
	raw := mustJSON(next)
	err = j.CommitRoute(ctx, p.Plan().RouteSHA256, prior.Revision, raw, e.ID+":admit", sdk.SHA256(raw), nil, nil, time.Now().UTC())
	return next, err
}
