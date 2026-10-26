package sdk

import (
	"context"
	"errors"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	z "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv2"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"os"
	"reflect"
	"sort"
	"sync"
	"time"
)

// IntegrationBackend is protected compiled plumbing. Current independently checks
// policy custody, revocation, external anti-rollback and global v1/v2 ownership.
// No ordinary binary installs it. Poll must use operational read grants; neither
// webhook possession nor a worker lease supplies provider-send authority.
type IntegrationBackend interface {
	Current(context.Context, i.Policy, i.Enrollment) error
	Claim(context.Context, i.Enrollment, string, int64) (int64, error)
	Check(context.Context, i.Enrollment, string, int64) error
	Poll(context.Context, i.Enrollment, string, func(context.Context) error) (PreparedSyncV2, error)
	Record(context.Context, i.Enrollment, PreparedSyncV2) error
	Ingest(context.Context, i.Policy, i.Enrollment, i.EventEnvelope) (i.Report, error)
	Inspect(context.Context, i.Enrollment, string) (i.Report, error)
	Events(context.Context, i.Enrollment, string, int64, bool) (i.Report, error)
	State(context.Context, i.Enrollment, PreparedSyncV2) (z.State, error)
	Commit(context.Context, i.Enrollment, PreparedSyncV2, z.State, []string, string) error
	OpenOperation(context.Context, PreparedOperation, []byte, []byte) (*OperationSession, error)
}

func (s *Service) WithIntegrationBackend(b IntegrationBackend) *Service {
	v := *s
	v.integration = b
	return &v
}
func integrationReport(kind string, store i.Store) i.Report {
	return i.Report{Version: i.Version, Kind: kind, Status: "ok", Store: store, Events: []i.Event{}, Operations: []o.Observation{}, Diagnostics: []api.Diagnostic{}}
}
func (s *Service) integrationAuthority(ctx context.Context, policy, enrollment []byte, subject, purpose string) (i.Policy, i.Enrollment, error) {
	if err := ctx.Err(); err != nil {
		return i.Policy{}, i.Enrollment{}, err
	}
	p, e, err := ValidateIntegrationAuthority(policy, enrollment, subject, purpose, time.Now().UTC())
	if err != nil {
		return p, e, operationFailure("integration.authority", "current scoped integration enrollment required", 3, err)
	}
	if s.integration == nil {
		return p, e, operationFailure("integration.unavailable", "protected integration bootstrap unavailable", 3, nil)
	}
	if err = s.integration.Current(ctx, p, e); err != nil {
		return p, e, operationFailure("integration.current", "protected current policy/ownership proof failed", 3, err)
	}
	return p, e, nil
}
func (s *Service) IngestIntegration(ctx context.Context, envelope, policy, enrollment, out string) (i.Report, error) {
	raw, err := ReadLocal(ctx, envelope)
	if err != nil {
		return i.Report{}, err
	}
	praw, err := ReadLocal(ctx, policy)
	if err != nil {
		return i.Report{}, err
	}
	eraw, err := ReadLocal(ctx, enrollment)
	if err != nil {
		return i.Report{}, err
	}
	var env i.EventEnvelope
	if DecodeRead(raw, &env) != nil {
		return i.Report{}, errors.New("closed event envelope required")
	}
	p, e, err := s.integrationAuthority(ctx, praw, eraw, SHA256(raw), "ingest-hint")
	if err != nil {
		return i.Report{}, err
	}
	if e.OutputPath != out || e.RouteID != env.RouteID {
		return i.Report{}, errors.New("exact ingestion output/route required")
	}
	w, err := engine.ReserveBundle(ctx, out)
	if err != nil {
		return i.Report{}, err
	}
	r, err := s.integration.Ingest(ctx, p, e, env)
	preserve, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	files := map[string][]byte{"assets/envelope.json": raw, "assets/policy.json": praw, "assets/enrollment.json": eraw}
	if writeErr := w.Write(preserve, operationalReportFiles(i.Version, r, files)); writeErr != nil {
		return r, writeErr
	}
	if syncErr := w.Sync(preserve); syncErr != nil {
		return r, syncErr
	}
	return r, err
}

// Control subjects bind action, consumer and sequence. A caller-provided digest
// cannot authorize a different acknowledgement, route or control operation.
func IntegrationCommandSubject(c i.Command) string {
	c.SubjectSHA256 = ""
	c.Policy = nil
	c.Enrollment = nil
	return SHA256(engine.Encode(c))
}
func (s *Service) IntegrationCommand(ctx context.Context, c i.Command) (i.Report, error) {
	if c.Version != i.Version || c.Kind != "IntegrationCommand" || c.SubjectSHA256 != IntegrationCommandSubject(c) {
		return i.Report{}, errors.New("exact action-specific command subject required")
	}
	purpose := ""
	switch c.Action {
	case "status":
		purpose = "local-inspect"
	case "events":
		purpose = "events-read"
	case "ack":
		purpose = "events-ack"
	case "ingest":
		purpose = "ingest-hint"
	case "submit":
		purpose = "submit-batch"
	default:
		return i.Report{}, operationFailure("integration.command", "unsupported control; use immutable sync plan and specific grant", 3, nil)
	}
	_, e, err := s.integrationAuthority(ctx, c.Policy, c.Enrollment, c.SubjectSHA256, purpose)
	if err != nil {
		return i.Report{}, err
	}
	if c.RouteID != e.RouteID {
		return i.Report{}, errors.New("control route substitution")
	}
	switch c.Action {
	case "submit":
		if c.Batch == nil {
			return i.Report{}, errors.New("exact approved batch required")
		}
		batch := c.Batch
		p, err := PrepareSyncV2(ctx, batch.Plan)
		if err != nil || p.Digest() != batch.PlanSHA256 {
			return i.Report{}, errors.New("submitted plan changed")
		}
		raw, err := ReadLocal(ctx, batch.Enrollment)
		if err != nil {
			return i.Report{}, err
		}
		var batchEn i.Enrollment
		if DecodeRead(raw, &batchEn) != nil || batchEn.Store != e.Store || batchEn.RouteID != e.RouteID || batchEn.WorkerID != e.WorkerID || batchEn.Fence != e.Fence {
			return i.Report{}, errors.New("submitted batch worker/scope changed")
		}
		return s.ExecuteSyncV2(ctx, ExecuteSyncV2Request{Plan: batch.Plan, Invocation: batch.Invocation, Policy: batch.Policy, Enrollment: batch.Enrollment, Out: batch.Output, Action: batch.Action, Once: true})
	case "status":
		return s.integration.Inspect(ctx, e, c.RouteID)
	case "events", "ack":
		return s.integration.Events(ctx, e, c.Consumer, c.AfterSequence, c.Action == "ack")
	case "ingest":
		if c.Envelope == nil {
			return i.Report{}, errors.New("event envelope required")
		}
		var p i.Policy
		DecodeRead(c.Policy, &p)
		return s.integration.Ingest(ctx, p, e, *c.Envelope)
	}
	return i.Report{}, errors.New("unsupported integration control")
}

type integrationWorker struct {
	leaseMu                       sync.Mutex
	mu                            sync.Mutex
	svc                           *Service
	configuration                 i.Configuration
	configRaw, policy, enrollment []byte
	en                            i.Enrollment
	started                       time.Time
	fences                        map[string]int64
	cycles                        int
}

func (s *Service) newIntegrationWorker(ctx context.Context, workspace, configuration, policy, enrollment string) (*integrationWorker, error) {
	raw, err := ReadLocal(ctx, configuration)
	if err != nil {
		return nil, err
	}
	var c i.Configuration
	if DecodeRead(raw, &c) != nil || ValidateIntegrationConfiguration(c) != nil {
		return nil, errors.New("invalid bounded integration configuration")
	}
	ws, err := ReadLocal(ctx, workspace)
	if err != nil || SHA256(ws) != c.WorkspaceSHA256 {
		return nil, errors.New("workspace configuration digest mismatch")
	}
	doc, err := Parse(ws, "json")
	if err != nil || doc.Kind() != "Workspace" {
		return nil, errors.New("valid domain workspace required")
	}
	p, err := ReadLocal(ctx, policy)
	if err != nil {
		return nil, err
	}
	e, err := ReadLocal(ctx, enrollment)
	if err != nil {
		return nil, err
	}
	pol, en, err := s.integrationAuthority(ctx, p, e, SHA256(raw), "worker-start")
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(c.Routes, pol.Routes) || !integrationLimitsFit(c.Limits, pol.Limits) {
		return nil, errors.New("worker configuration exceeds current route/limits policy")
	}
	start, startErr := CurrentProcessStart()
	if startErr != nil || en.ProcessID != os.Getpid() || en.ProcessStart != start {
		return nil, errors.New("exact process-bound worker enrollment required")
	}
	if en.Store != c.Store || en.WorkerID == "" {
		return nil, errors.New("worker identity/store mismatch")
	}
	for route, fence := range en.AttachedFences {
		if fence < 1 || c.Routes[route] == "" {
			return nil, errors.New("invalid protected recovery fence attachment")
		}
	}
	w := &integrationWorker{svc: s, configuration: c, configRaw: raw, policy: p, enrollment: e, en: en, started: time.Now().UTC(), fences: cloneRead(en.AttachedFences)}
	if w.fences == nil {
		w.fences = map[string]int64{}
	}
	return w, nil
}
func (w *integrationWorker) current(ctx context.Context) error {
	if time.Since(w.started) >= time.Duration(w.configuration.Limits.ProcessSeconds)*time.Second {
		return errors.New("bounded worker lifetime ended")
	}
	_, _, e := w.svc.integrationAuthority(ctx, w.policy, w.enrollment, SHA256(w.configRaw), "worker-start")
	return e
}
func (w *integrationWorker) cycle(ctx context.Context) (i.Report, error) {
	if e := integrationLock(ctx, &w.mu); e != nil {
		return i.Report{}, e
	}
	defer w.mu.Unlock()
	r := integrationReport("WorkerCycle", w.en.Store)
	if err := w.current(ctx); err != nil {
		return r, err
	}
	ctx = context.WithValue(ctx, integrationBudgetKey{}, &integrationBudget{limits: w.configuration.Limits})
	ctx, cancel := context.WithTimeout(ctx, time.Duration(w.configuration.Limits.CycleSeconds)*time.Second)
	defer cancel()
	ids := make([]string, 0, len(w.configuration.Routes))
	for id := range w.configuration.Routes {
		ids = append(ids, id)
	}
	sortStrings(ids)
	for _, route := range ids {
		if e := w.current(ctx); e != nil {
			return r, e
		}
		fence, e := w.svc.integration.Claim(ctx, w.en, route, w.fence(route))
		if e != nil {
			return r, e
		}
		w.leaseMu.Lock()
		w.fences[route] = fence
		w.leaseMu.Unlock()
		guard := func(c context.Context) error {
			if err := w.current(c); err != nil {
				return err
			}
			return w.svc.integration.Check(c, w.en, route, fence)
		}
		p, err := w.svc.integration.Poll(ctx, w.en, route, guard)
		if err != nil {
			r.Status = "blocked"
			r.Diagnostics = append(r.Diagnostics, api.Diagnostic{Code: "integration.poll-blocked", Field: route, Message: "operational read enrollment/coverage unavailable; no provider sends", Severity: "warning"})
			continue
		}
		if p.plan.RouteID != route || p.plan.ConfigurationSHA256 != w.configuration.Routes[route] {
			return r, errors.New("poll returned different route/configuration")
		}
		if err = guard(ctx); err != nil {
			return r, err
		}
		if err = w.svc.integration.Record(ctx, w.en, p); err != nil {
			return r, err
		}
	}
	w.cycles++
	r.Cycles = w.cycles
	return r, nil
}
func sortStrings(v []string) { sort.Strings(v) }

// ProjectSyncV2State is the sole baseline projection. Confirmed operations must
// additionally exist as readback-verified in the inherited provider outbox.
func ProjectSyncV2State(p PreparedSyncV2, prior z.State, action string, confirmed []string) (z.State, error) {
	next := cloneRead(prior)
	if prior.RouteID != p.plan.RouteID || prior.ConfigurationSHA256 != p.plan.ConfigurationSHA256 {
		return next, errors.New("route identity changed")
	}
	if action == "initialize" {
		if prior.Initialized || p.plan.Status != "baseline-candidate" || !p.plan.Complete || len(p.plan.Changes) > 0 || len(p.plan.Conflicts) > 0 {
			return next, errors.New("complete equal initial baseline required")
		}
		next.Initialized = true
		next.Baselines = cloneRead(p.plan.Advances)
	} else if action == "pause" {
		next.Paused = true
	} else if action == "resume" {
		if len(prior.Unresolved) > 0 || prior.PendingPlanSHA256 != "" {
			return next, errors.New("unresolved effects block resume")
		}
		next.Paused = false
	} else if action == "begin" {
		if !prior.Initialized || prior.Paused || len(prior.Unresolved) > 0 || prior.PendingPlanSHA256 != "" || p.plan.Status != "ready" {
			return next, errors.New("clean initialized route required")
		}
		next.Unresolved = []string{}
		for _, change := range p.plan.Changes {
			next.Unresolved = append(next.Unresolved, operationID(change.Desired.LogicalKey, change.Target, change.Desired.Operation))
		}
		next.PendingPlanSHA256 = p.Digest()
	} else if action == "finish" {
		if prior.PendingPlanSHA256 != p.Digest() {
			return next, errors.New("pending lineage mismatch")
		}
		done := map[string]bool{}
		for _, id := range confirmed {
			if done[id] {
				return next, errors.New("duplicate confirmation")
			}
			done[id] = true
		}
		base := map[string]z.Baseline{}
		for _, b := range prior.Baselines {
			base[baselineKey(b.CorrelationID, b.RuleID)] = b
		}
		for _, b := range p.plan.Advances {
			base[baselineKey(b.CorrelationID, b.RuleID)] = b
		}
		next.Unresolved = []string{}
		for _, change := range p.plan.Changes {
			id := operationID(change.Desired.LogicalKey, change.Target, change.Desired.Operation)
			if done[id] {
				b := change.Next
				b.ConfirmedOperations = []string{id}
				base[baselineKey(b.CorrelationID, b.RuleID)] = b
				delete(done, id)
			} else {
				next.Unresolved = append(next.Unresolved, id)
			}
		}
		if len(done) > 0 {
			return next, errors.New("foreign operation confirmation")
		}
		next.Baselines = []z.Baseline{}
		for _, b := range base {
			next.Baselines = append(next.Baselines, b)
		}
		sort.Slice(next.Baselines, func(a, b int) bool {
			return baselineKey(next.Baselines[a].CorrelationID, next.Baselines[a].RuleID) < baselineKey(next.Baselines[b].CorrelationID, next.Baselines[b].RuleID)
		})
		if len(next.Unresolved) == 0 && p.plan.Complete {
			next.CompletedWatermark = p.Digest()
			next.PendingPlanSHA256 = ""
		}
	} else {
		return next, errors.New("unknown sync v2 projection")
	}
	next.Revision = prior.Revision + 1
	return next, nil
}

type ExecuteSyncV2Request struct {
	Plan, Invocation, Policy, Enrollment, Action, Out string
	Once                                              bool
}

func (s *Service) ExecuteSyncV2(ctx context.Context, q ExecuteSyncV2Request) (result i.Report, resultErr error) {
	admitted := false
	defer func() {
		if admitted && resultErr != nil {
			result.Status = "unknown"
			resultErr = operationFailure("sync.v2.uncertain", "durable admission may exist; read-only reconciliation required", 5, resultErr)
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	p, err := PrepareSyncV2(ctx, q.Plan)
	if err != nil {
		return i.Report{}, err
	}
	policy, err := ReadLocal(ctx, q.Policy)
	if err != nil {
		return i.Report{}, err
	}
	raw, err := ReadLocal(ctx, q.Enrollment)
	if err != nil {
		return i.Report{}, err
	}
	purpose := q.Action
	if purpose == "" {
		purpose = "sync-run"
	}
	routePolicy, en, err := s.integrationAuthority(ctx, policy, raw, p.Digest(), purpose)
	if err != nil {
		return i.Report{}, err
	}
	cycleSeconds := routePolicy.Limits.CycleSeconds
	requestSeconds := routePolicy.Limits.RequestSeconds
	if budget, ok := ctx.Value(integrationBudgetKey{}).(*integrationBudget); ok {
		if budget.limits.CycleSeconds < cycleSeconds {
			cycleSeconds = budget.limits.CycleSeconds
		}
		if budget.limits.RequestSeconds < requestSeconds {
			requestSeconds = budget.limits.RequestSeconds
		}
	}
	ctx, limitCancel := context.WithTimeout(ctx, time.Duration(cycleSeconds)*time.Second)
	defer limitCancel()
	start, startErr := CurrentProcessStart()
	if startErr != nil || en.ProcessID != os.Getpid() || en.ProcessStart != start {
		return i.Report{}, operationFailure("integration.owner-ipc", "submit through the enrolled worker IPC; direct CLI is not the owner", 3, nil)
	}
	r := integrationReport("DirectionalSyncRun", en.Store)
	r.RouteID = en.RouteID
	r.Fence = en.Fence
	if routePolicy.Routes[p.plan.RouteID] != p.plan.ConfigurationSHA256 || en.RouteID != p.plan.RouteID || en.OutputPath != q.Out || en.Fence < 1 || en.WorkerID == "" {
		return r, errors.New("exact v2 worker/route/output binding required")
	}
	if !q.Once && purpose == "sync-run" {
		return r, operationFailure("sync.once", "explicit bounded --once required", 2, nil)
	}
	w, err := engine.ReserveBundle(ctx, q.Out)
	if err != nil {
		return r, err
	}
	guard := func(c context.Context) error {
		if purpose == "sync-run" || purpose == "sync-initialize" {
			if err := ValidateSyncV2Fresh(p, time.Now().UTC()); err != nil {
				return err
			}
		}
		if _, _, e := s.integrationAuthority(c, policy, raw, p.Digest(), purpose); e != nil {
			return e
		}
		return s.integration.Check(c, en, en.RouteID, en.Fence)
	}
	if err = guard(ctx); err != nil {
		return r, err
	}
	prior, err := s.integration.State(ctx, en, p)
	if err != nil {
		return r, err
	}
	var state z.State
	if DecodeRead(p.files["assets/state.json"], &state) != nil {
		return r, errors.New("pinned prior state missing")
	}
	if !reflect.DeepEqual(prior, state) && !(purpose == "sync-reconcile" && prior.PendingPlanSHA256 == p.Digest()) {
		return r, operationFailure("sync.v2.stale", "route revision changed", 4, nil)
	}
	action := map[string]string{"sync-initialize": "initialize", "sync-pause": "pause", "sync-resume": "resume"}[purpose]
	if action != "" {
		next, e := ProjectSyncV2State(p, prior, action, nil)
		if e != nil {
			return r, e
		}
		err = s.integration.Commit(ctx, en, p, next, nil, action)
	} else {
		if purpose != "sync-run" && purpose != "sync-reconcile" {
			return r, errors.New("unsupported sync v2 authority purpose")
		}
		if purpose == "sync-run" {
			at, e := nativeTime(p.plan.AsOf)
			if e != nil || time.Since(at) < 0 || time.Since(at) >= 5*time.Minute {
				return r, operationFailure("sync.v2.stale", "fresh directional plan required", 4, nil)
			}
		}
		invRaw, e := ReadLocal(ctx, q.Invocation)
		if e != nil {
			return r, e
		}
		var inv z.Invocation
		if DecodeRead(invRaw, &inv) != nil || inv.Version != z.Version || inv.Kind != "DirectionalSyncInvocation" || inv.PlanSHA256 != p.Digest() || len(inv.Operations) != len(p.plan.Changes) || len(inv.Operations) > routePolicy.Limits.MaxOperations {
			return r, errors.New("exact approved directional invocation required")
		}
		if budget, ok := ctx.Value(integrationBudgetKey{}).(*integrationBudget); ok && len(inv.Operations) > budget.limits.MaxOperations {
			return r, errors.New("batch exceeds worker operation bound")
		}
		type preparedCall struct {
			p                  PreparedOperation
			policy, enrollment []byte
			call               z.Call
		}
		calls := make([]preparedCall, len(inv.Operations))
		for index, call := range inv.Operations {
			change := p.plan.Changes[index]
			op, e := PrepareOperation(ctx, call.Plan)
			if e != nil {
				return r, e
			}
			d := op.Draft()
			var desired o.Desired
			if DecodeRead(op.files["assets/desired.json"], &desired) != nil || check.Hash(desired) != check.Hash(change.Desired) || d.Target != change.Target || call.Direction != change.Direction || call.ChangeSHA256 != check.Hash(change) {
				return r, errors.New("direction, target or desired substitution")
			}
			target, prefix := "destination", "forward"
			if change.Direction == "reverse" {
				target, prefix = "source", "reverse"
			}
			if d.Inputs["snapshot"] != p.plan.Inputs[target] || d.Inputs["binding"] != p.plan.Inputs[target+"-binding"] || d.Inputs["mapping"] != p.plan.Inputs[prefix+"-mapping"] || d.Inputs["observation"] != p.plan.Inputs[target+"-observation"] {
				return r, errors.New("directional evidence substitution")
			}
			opPolicy, e := ReadLocal(ctx, call.Policy)
			if e != nil {
				return r, e
			}
			opEn, e := ReadLocal(ctx, call.Enrollment)
			if e != nil {
				return r, e
			}
			var grant i.Enrollment
			if DecodeRead(opEn, &grant) != nil || grant.Store != en.Store || grant.RouteID != en.RouteID || grant.WorkerID != en.WorkerID || grant.Fence != en.Fence || grant.OutputPath != call.Output {
				return r, errors.New("operation worker/store substitution")
			}
			if purpose == "sync-run" && grant.Purpose != "provider-send" || purpose == "sync-reconcile" && grant.Purpose != "operation-reconcile" {
				return r, errors.New("separate action-specific operation grants required")
			}
			calls[index] = preparedCall{op, opPolicy, opEn, call}
		}
		if purpose == "sync-run" {
			pending, e := ProjectSyncV2State(p, prior, "begin", nil)
			if e != nil {
				return r, e
			}
			admitted = true
			if e = s.integration.Commit(ctx, en, p, pending, nil, "begin"); e != nil {
				return r, e
			}
			prior = pending
		}
		if purpose == "sync-reconcile" {
			admitted = true
		}
		confirmed := []string{}
		for _, call := range calls {
			if e = guard(ctx); e != nil {
				err = e
				break
			}
			boundedOpCtx, stopOperation := context.WithTimeout(ctx, time.Duration(requestSeconds)*time.Second)
			opCtx := context.WithValue(boundedOpCtx, syncAdmissionKey{}, syncAdmission{Route: en.RouteID, Revision: prior.Revision, Plan: p.Digest(), Action: "sync-run", OperationDigest: call.p.Digest(), PolicyDigest: SHA256(call.policy), EnrollmentDigest: SHA256(call.enrollment), Check: guard})
			session, e := s.integration.OpenOperation(opCtx, call.p, call.policy, call.enrollment)
			if e != nil {
				stopOperation()
				err = e
				break
			}
			ob, e := IntegrationOperationReport(opCtx, session, call.call.Output)
			stopOperation()
			r.Operations = append(r.Operations, ob)
			if e == nil && ob.State == "readback-verified" {
				confirmed = append(confirmed, ob.OperationID)
			} else {
				err = e
				if err == nil {
					err = errors.New("operation not confirmed")
				}
				break
			}
		}
		next, e := ProjectSyncV2State(p, prior, "finish", confirmed)
		if e != nil {
			return r, e
		}
		if commitErr := s.integration.Commit(ctx, en, p, next, confirmed, "finish"); commitErr != nil {
			err = commitErr
		}
		if err != nil || len(next.Unresolved) > 0 {
			r.Status = "unknown"
			err = operationFailure("sync.v2.uncertain", "retain obligations; read-only reconciliation without resend", 5, err)
		}
	}
	if err != nil && r.Status == "ok" {
		r.Status = "blocked"
	}
	preserve, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer stop()
	files := map[string][]byte{"assets/plan.json": p.raw, "assets/policy.json": policy, "assets/enrollment.json": raw}
	if e := w.Write(preserve, operationalReportFiles(i.Version, r, files)); e != nil {
		return r, e
	}
	if e := w.Sync(preserve); e != nil {
		return r, e
	}
	return r, err
}

func (w *integrationWorker) fence(route string) int64 {
	w.leaseMu.Lock()
	defer w.leaseMu.Unlock()
	return w.fences[route]
}
func (w *integrationWorker) heartbeat(ctx context.Context) error {
	if e := w.current(ctx); e != nil {
		return e
	}
	w.leaseMu.Lock()
	fences := map[string]int64{}
	for key, value := range w.fences {
		fences[key] = value
	}
	w.leaseMu.Unlock()
	for route, fence := range fences {
		if e := w.svc.integration.Check(ctx, w.en, route, fence); e != nil {
			return e
		}
		next, e := w.svc.integration.Claim(ctx, w.en, route, fence)
		if e != nil || next != fence {
			return errors.New("worker renewal failed; stop new admission")
		}
	}
	return nil
}

func integrationLimitsFit(a, b i.Limits) bool {
	if ValidateIntegrationLimits(a) != nil || ValidateIntegrationLimits(b) != nil {
		return false
	}
	return a.ProcessSeconds <= b.ProcessSeconds && a.CycleSeconds <= b.CycleSeconds && a.RequestSeconds <= b.RequestSeconds && a.Requests <= b.Requests && a.Items <= b.Items && a.ResponseBytes <= b.ResponseBytes && a.CaptureBytes <= b.CaptureBytes && a.MaxOperations <= b.MaxOperations && a.MaxHints <= b.MaxHints && a.InboxBytes <= b.InboxBytes
}
func ValidateSyncV2Fresh(p PreparedSyncV2, now time.Time) error {
	stamps := []string{p.plan.AsOf}
	for _, name := range []string{"source", "destination"} {
		var manifest r.Manifest
		var metadata s.Observation
		if DecodeRead(p.files["assets/"+name+"/bundle.json"], &manifest) != nil || manifest.OriginalObservedAt == nil || !manifest.Complete || DecodeRead(p.files["assets/"+name+"-observation/bundle.json"], &metadata) != nil {
			return errors.New("both complete original directional observations required")
		}
		stamps = append(stamps, *manifest.OriginalObservedAt, metadata.ObservedAt)
	}
	for _, stamp := range stamps {
		observed, e := nativeTime(stamp)
		if e != nil || now.Before(observed) || now.Sub(observed) >= 5*time.Minute {
			return errors.New("directional source/target evidence expired before admission")
		}
	}
	return nil
}

func integrationLock(ctx context.Context, mu *sync.Mutex) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if mu.TryLock() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// Recovery is a separate protected local action. The independently verified stop
// proof permits fence replacement, never clearing pending provider obligations.
func (s *Service) RecoverIntegration(ctx context.Context, recovery, policy, enrollment, out string) (i.Report, error) {
	raw, err := ReadLocal(ctx, recovery)
	if err != nil {
		return i.Report{}, err
	}
	praw, err := ReadLocal(ctx, policy)
	if err != nil {
		return i.Report{}, err
	}
	eraw, err := ReadLocal(ctx, enrollment)
	if err != nil {
		return i.Report{}, err
	}
	p, e, err := s.integrationAuthority(ctx, praw, eraw, SHA256(raw), "worker-recover")
	if err != nil {
		return i.Report{}, err
	}
	var request i.WorkerRecovery
	if DecodeRead(raw, &request) != nil || request.Version != i.Version || request.Kind != "WorkerRecovery" || request.Store != e.Store || request.RouteID != e.RouteID || request.NewWorkerID != e.WorkerID || request.PriorFence < 1 || request.ConfigurationSHA256 != p.Routes[e.RouteID] || !nativeDigest(request.QuiescenceSHA256) || e.OutputPath != out {
		return i.Report{}, errors.New("exact independently proven recovery required")
	}
	backend, ok := s.integration.(interface {
		Recover(context.Context, i.Enrollment, i.WorkerRecovery) (i.Report, error)
	})
	if !ok {
		return i.Report{}, operationFailure("integration.recovery-unavailable", "protected recovery verifier missing", 3, nil)
	}
	w, err := engine.ReserveBundle(ctx, out)
	if err != nil {
		return i.Report{}, err
	}
	report, err := backend.Recover(ctx, e, request)
	preserve, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if writeErr := w.Write(preserve, operationalReportFiles(i.Version, report, map[string][]byte{"assets/recovery.json": raw, "assets/policy.json": praw, "assets/enrollment.json": eraw})); writeErr != nil {
		return report, writeErr
	}
	if syncErr := w.Sync(preserve); syncErr != nil {
		return report, syncErr
	}
	return report, err
}
