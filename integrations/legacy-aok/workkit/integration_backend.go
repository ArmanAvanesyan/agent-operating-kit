package workkit

import (
	"context"
	"database/sql"
	"errors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	z "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv2"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"sync"
	"time"
)

// All callbacks are compiled protected custody. Current must verify policy roots,
// revocation, external store retention and ownership across ALL v1/v2 namespaces.
// Local bookkeeping authority never consumes a provider-send activation.
type IntegrationBackend struct {
	Stopped    func(context.Context, i.Enrollment, i.WorkerRecovery) error
	Trust      func(context.Context, i.Policy, i.Enrollment) error
	Witness    func(context.Context, i.Enrollment, string, string) error
	Poller     func(context.Context, i.Enrollment, string, func(context.Context) error) (sdk.PreparedSyncV2, error)
	Ingress    func(context.Context, i.Enrollment, i.IngressReceipt) ([]byte, error)
	Operations IntegrationOperationBackend
	mu         sync.Mutex
	policies   map[string]i.Policy
}

func (b *IntegrationBackend) Current(ctx context.Context, p i.Policy, e i.Enrollment) error {
	if b == nil || b.Trust == nil || b.Witness == nil {
		return &sdk.Error{Code: "integration.unavailable", Message: "protected integration trust/witness unavailable", ExitCode: 3}
	}
	if err := b.Trust(ctx, p, e); err != nil {
		return err
	}
	b.mu.Lock()
	if b.policies == nil {
		b.policies = map[string]i.Policy{}
	}
	b.policies[sdk.SHA256(mustJSON(e))] = p
	b.mu.Unlock()
	return nil
}
func (b *IntegrationBackend) open(ctx context.Context, e i.Enrollment, writable bool) (*writeback.WorkkitJournal, error) {
	if b == nil || b.Trust == nil || b.Witness == nil || e.Store.Version != i.JournalVersion {
		return nil, errors.New("protected v2 journal authority required")
	}
	b.mu.Lock()
	policy, verified := b.policies[sdk.SHA256(mustJSON(e))]
	b.mu.Unlock()
	if !verified {
		return nil, errors.New("current protected session required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := sdk.CheckIntegrationWindow(policy, e, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := b.Trust(ctx, policy, e); err != nil {
		return nil, err
	}
	s := writeback.WorkkitStoreIdentity{Version: e.Store.Version, Kind: e.Store.Kind, Path: e.Store.Path, ID: e.Store.ID, Namespace: e.Store.Namespace, Owner: e.Store.Owner}
	if writable {
		return writeback.OpenWorkkitV2Writable(ctx, s)
	}
	j, err := writeback.OpenWorkkitV2(ctx, s.Path, s.Namespace, s.Owner, false)
	if err == nil && j.Identity != s {
		j.Close()
		return nil, errors.New("store identity changed")
	}
	return j, err
}
func (b *IntegrationBackend) Claim(ctx context.Context, e i.Enrollment, route string, expected int64) (int64, error) {
	j, err := b.open(ctx, e, true)
	if err != nil {
		return 0, err
	}
	defer j.Close()
	if e.WorkerID == "" || e.Purpose != "worker-start" {
		return 0, errors.New("process-bound worker-start required")
	}
	if err = b.Witness(ctx, e, "worker-claim:"+route, sdk.SHA256(mustJSON(struct {
		Route, Worker string
		Fence         int64
		At            string
	}{route, e.WorkerID, expected, time.Now().UTC().Format(time.RFC3339Nano)}))); err != nil {
		return 0, err
	}
	return j.ClaimWorker(ctx, route, e.WorkerID, time.Now().UTC(), expected)
}
func (b *IntegrationBackend) Check(ctx context.Context, e i.Enrollment, route string, fence int64) error {
	j, err := b.open(ctx, e, false)
	if err != nil {
		return err
	}
	defer j.Close()
	return j.CheckWorker(ctx, route, e.WorkerID, fence, time.Now().UTC())
}
func (b *IntegrationBackend) Poll(ctx context.Context, e i.Enrollment, route string, guard func(context.Context) error) (sdk.PreparedSyncV2, error) {
	if b.Poller == nil {
		return sdk.PreparedSyncV2{}, errors.New("operational read grant/poller unavailable")
	}
	if err := guard(ctx); err != nil {
		return sdk.PreparedSyncV2{}, err
	}
	return b.Poller(ctx, e, route, guard)
}
func (b *IntegrationBackend) Record(ctx context.Context, e i.Enrollment, p sdk.PreparedSyncV2) error {
	j, err := b.open(ctx, e, true)
	if err != nil {
		return err
	}
	defer j.Close()
	if err = b.Witness(ctx, e, "candidate:"+p.Plan().RouteID, p.Digest()); err != nil {
		return err
	}
	if p.Plan().Complete && p.Plan().Status != "blocked" && sdk.ValidateSyncV2Fresh(p, time.Now().UTC()) == nil {
		through := time.Now().UTC()
		checkpoints := map[string][]byte{}
		for _, side := range []string{"source", "destination"} {
			var manifest r.Manifest
			var metadata s.Observation
			if sdk.DecodeRead(p.Assets()["assets/"+side+"/bundle.json"], &manifest) != nil || manifest.OriginalObservedAt == nil || sdk.DecodeRead(p.Assets()["assets/"+side+"-observation/bundle.json"], &metadata) != nil {
				return errors.New("complete coverage source required")
			}
			at, err := time.Parse(time.RFC3339Nano, *manifest.OriginalObservedAt)
			if err != nil {
				return err
			}
			if at.Before(through) {
				through = at
			}
			checkpoint := i.Checkpoint{Version: i.Version, Kind: "IntegrationCheckpoint", RouteID: p.Plan().RouteID, ConfigurationSHA256: p.Plan().ConfigurationSHA256, ConnectionID: manifest.ConnectionID, AccountSHA256: sdk.SHA256(mustJSON(struct{ Principal, Configuration string }{metadata.Scope.Principal, metadata.ConfigurationSHA256})), QuerySHA256: p.Plan().Inputs[side+"-query"], APIVersion: metadata.Scope.APIVersion, ObservedAt: *manifest.OriginalObservedAt, Complete: true, CompletedWatermark: ""}
			checkpoints[manifest.ConnectionID] = mustJSON(checkpoint)
		}
		return j.RecordIntegrationRead(ctx, p.Plan().RouteID, p.Digest(), mustJSON(p.Plan()), through, checkpoints, time.Now().UTC())
	}
	return j.RecordIntegrationPlan(ctx, p.Plan().RouteID, p.Digest(), mustJSON(p.Plan()), time.Now().UTC())
}
func (b *IntegrationBackend) Ingest(ctx context.Context, p i.Policy, e i.Enrollment, env i.EventEnvelope) (i.Report, error) {
	r := i.Report{Version: i.Version, Kind: "IngestionReport", Store: e.Store, RouteID: e.RouteID, Status: "blocked"}
	if b.Ingress == nil || e.RouteID != env.RouteID {
		return r, errors.New("independent ingress scope/secret resolver required")
	}
	secret, err := b.Ingress(ctx, e, env.Receipt)
	if err != nil {
		return r, err
	}
	hint, err := sdk.VerifyGitHubHint(env, p, secret, time.Now().UTC())
	for index := range secret {
		secret[index] = 0
	}
	if err != nil {
		return r, err
	}
	j, err := b.open(ctx, e, true)
	if err != nil {
		return r, err
	}
	defer j.Close()
	if err = b.Witness(ctx, e, "ingest-hint", e.SubjectSHA256); err != nil {
		return r, err
	}
	status, err := j.IngestHint(ctx, hint.RouteID, hint.ConnectionID, hint.DeliveryID, hint.BodySHA256, mustJSON(hint), p.Limits.MaxHints, p.Limits.InboxBytes, time.Now().UTC())
	r.Status = status
	if status == "queued" || status == "duplicate" || status == "coalesced" {
		r.Status = "ok"
	}
	if status == "quarantined" {
		r.Status = "conflict"
	}
	return r, err
}
func (b *IntegrationBackend) Inspect(ctx context.Context, e i.Enrollment, route string) (i.Report, error) {
	r := i.Report{Version: i.Version, Kind: "IntegrationStatus", Store: e.Store, RouteID: route, Status: "ok", Events: []i.Event{}}
	j, err := b.open(ctx, e, false)
	if err != nil {
		return r, err
	}
	defer j.Close()
	r.Fence = e.Fence
	r.Hints, err = j.IntegrationHintCount(ctx, route)
	if err != nil {
		return r, err
	}
	r.Notifications, r.Delivered, r.Acknowledged, err = j.IntegrationNotificationStatus(ctx, route)
	if err != nil {
		return r, err
	}
	digest, raw, candidateErr := j.IntegrationCandidate(ctx, route)
	if candidateErr == nil {
		var p z.Plan
		if sdk.DecodeRead(raw, &p) != nil || sdk.SHA256(raw) != digest {
			return r, errors.New("invalid stored candidate")
		}
		r.CandidateSHA256 = digest
		r.Conflicts = p.Conflicts
		if len(p.Conflicts) > 0 {
			r.Status = "conflict"
		}
	} else if !errors.Is(candidateErr, sql.ErrNoRows) {
		return r, candidateErr
	}
	if rev, raw, stateErr := j.Route(ctx, route); stateErr == nil {
		var state z.State
		if sdk.DecodeRead(raw, &state) != nil || state.Revision != rev {
			return r, errors.New("invalid route state")
		}
		r.State = &state
	} else if !errors.Is(stateErr, sql.ErrNoRows) {
		return r, stateErr
	}
	return r, nil
}
func (b *IntegrationBackend) Events(ctx context.Context, e i.Enrollment, consumer string, after int64, ack bool) (i.Report, error) {
	r := i.Report{Version: i.Version, Kind: "NotificationReport", Store: e.Store, RouteID: e.RouteID, Status: "ok", Events: []i.Event{}}
	j, err := b.open(ctx, e, true)
	if err != nil {
		return r, err
	}
	defer j.Close()
	digest := sdk.SHA256(mustJSON(struct {
		Consumer string
		After    int64
		Ack      bool
	}{consumer, after, ack}))
	if err = b.Witness(ctx, e, "events", digest); err != nil {
		return r, err
	}
	if ack {
		err = j.AckEvents(ctx, sdk.SHA256(mustJSON(struct{ Consumer, Route string }{consumer, e.RouteID})), after, time.Now().UTC())
		r.Acknowledged = after
		return r, err
	}
	events, err := j.EventsForRoute(ctx, consumer, e.RouteID, after, time.Now().UTC())
	for _, ev := range events {
		r.Events = append(r.Events, i.Event{Sequence: ev.Sequence, RouteID: ev.RouteID, Kind: ev.Kind, SubjectSHA256: ev.SubjectSHA256, ObservedAt: ev.ObservedAt})
	}
	return r, err
}
func (b *IntegrationBackend) State(ctx context.Context, e i.Enrollment, p sdk.PreparedSyncV2) (z.State, error) {
	j, err := b.open(ctx, e, false)
	if err != nil {
		return z.State{}, err
	}
	defer j.Close()
	rev, raw, err := j.Route(ctx, p.Plan().RouteID)
	if errors.Is(err, sql.ErrNoRows) {
		var prior z.State
		if sdk.DecodeRead(p.Assets()["assets/state.json"], &prior) != nil || prior.Initialized || prior.Revision != 0 || len(prior.Baselines) > 0 || len(prior.Unresolved) > 0 || prior.PendingPlanSHA256 != "" || prior.CompletedWatermark != "" {
			return prior, errors.New("initial state cannot adopt obligations")
		}
		return prior, nil
	}
	var state z.State
	if err != nil || sdk.DecodeRead(raw, &state) != nil || state.Revision != rev || state.Version != z.Version {
		return state, errors.New("invalid directional state")
	}
	return state, nil
}
func (b *IntegrationBackend) Commit(ctx context.Context, e i.Enrollment, p sdk.PreparedSyncV2, next z.State, confirmed []string, action string) error {
	prior, err := b.State(ctx, e, p)
	if err != nil {
		return err
	}
	expected, err := sdk.ProjectSyncV2State(p, prior, action, confirmed)
	if err != nil || string(mustJSON(expected)) != string(mustJSON(next)) {
		return errors.New("baseline projection differs from confirmed lineage")
	}
	if err = b.Check(ctx, e, p.Plan().RouteID, e.Fence); err != nil {
		return err
	}
	j, err := b.open(ctx, e, true)
	if err != nil {
		return err
	}
	defer j.Close()
	if err = b.Witness(ctx, e, "route:"+action, sdk.SHA256(mustJSON(next))); err != nil {
		return err
	}
	var c z.Configuration
	if sdk.DecodeRead(p.Assets()["assets/configuration.json"], &c) != nil {
		return errors.New("route configuration missing")
	}
	targets := []string{}
	for _, co := range c.Correlations {
		for _, rule := range c.Rules {
			if rule.Owner == "source" || rule.Owner == "shared" {
				targets = append(targets, sdk.SHA256(mustJSON(struct {
					Target any
					Field  string
				}{co.Destination, rule.DestinationField})))
			}
			if rule.Owner == "destination" || rule.Owner == "shared" {
				targets = append(targets, sdk.SHA256(mustJSON(struct {
					Target any
					Field  string
				}{co.Source, rule.SourceField})))
			}
		}
	}
	if action == "begin" || action == "initialize" {
		base := ""
		if raw, ok := p.Assets()["assets/resolution-base.json"]; ok {
			base = sdk.SHA256(raw)
		}
		if err := j.BindIntegrationCandidate(p.Plan().RouteID, p.Digest(), base, mustJSON(p.Plan())); err != nil {
			return err
		}
	}
	return j.CommitRoute(ctx, p.Plan().RouteID, prior.Revision, mustJSON(next), e.ID+":"+action, sdk.SHA256(mustJSON(next)), targets, confirmed, time.Now().UTC())
}
func (b *IntegrationBackend) OpenOperation(ctx context.Context, p sdk.PreparedOperation, policy, enrollment []byte) (*sdk.OperationSession, error) {
	return b.Operations.Open(ctx, p, policy, enrollment)
}
func (b *IntegrationBackend) Recover(ctx context.Context, e i.Enrollment, request i.WorkerRecovery) (i.Report, error) {
	r := i.Report{Version: i.Version, Kind: "RecoveryReport", Status: "blocked", Store: e.Store, RouteID: e.RouteID}
	if b.Stopped == nil {
		return r, errors.New("independent stop/quiescence verifier missing")
	}
	if err := b.Stopped(ctx, e, request); err != nil {
		return r, err
	}
	j, err := b.open(ctx, e, true)
	if err != nil {
		return r, err
	}
	defer j.Close()
	if err = b.Witness(ctx, e, "worker-recover", e.SubjectSHA256); err != nil {
		return r, err
	}
	r.Fence, err = j.RecoverWorker(ctx, request.RouteID, request.NewWorkerID, request.QuiescenceSHA256, request.PriorFence, time.Now().UTC())
	if err == nil {
		r.Status = "ok"
	}
	return r, err
}
