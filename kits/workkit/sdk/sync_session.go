package sdk

import (
	"context"
	"errors"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	y "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/syncv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	check "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/setup"
	"sort"
	"time"
)

type PreparedSync struct {
	plan  y.Plan
	raw   []byte
	files map[string][]byte
}

func (p PreparedSync) Plan() y.Plan              { return cloneRead(p.plan) }
func (p PreparedSync) Digest() string            { return SHA256(p.raw) }
func (p PreparedSync) Assets() map[string][]byte { return cloneBytes(p.files) }
func PrepareSync(ctx context.Context, root string) (PreparedSync, error) {
	p, files, err := loadCapturedSyncPlan(ctx, root)
	if err != nil {
		return PreparedSync{}, err
	}
	return PreparedSync{p, files["report.json"], files}, nil
}

// SyncBackend is protected compiled plumbing: Current checks external authority,
// route ownership, exact stored revision and complete current endpoint bindings.
// Commit consumes anti-rollback authority then atomically records confirmed state
// using the same integration journal as ordinary operations. No sender lives here.
type SyncBackend interface {
	CurrentSync(context.Context, PreparedSync, o.Enrollment, []byte) (y.State, error)
	BeginSync(context.Context, PreparedSync, o.Enrollment, []byte) (y.State, error)
	CommitSync(context.Context, PreparedSync, o.Enrollment, []byte, y.State, []string) error
	InspectSync(context.Context, o.Store, string) (y.State, error)
}

func (svc *Service) WithSyncBackend(b SyncBackend) *Service { v := *svc; v.sync = b; return &v }
func SyncEnrollmentSigningBytes(e o.Enrollment) []byte      { return mutationSigningBytes(e) }
func ValidateSyncAuthority(p PreparedSync, policy, enrollment []byte) (o.Enrollment, error) {
	var a, b s.Observation
	if DecodeRead(p.files["assets/source-observation/bundle.json"], &a) != nil || DecodeRead(p.files["assets/destination-observation/bundle.json"], &b) != nil {
		return o.Enrollment{}, errors.New("exact sync scopes required")
	}
	var limits o.Policy
	if DecodeRead(policy, &limits) != nil || len(p.plan.Proposals) > limits.MaxOperations {
		return o.Enrollment{}, errors.New("sync operation count exceeds policy")
	}
	e, err := validateMutationAuthority(policy, enrollment, p.Digest(), "sync-action", []s.Scope{a.Scope, b.Scope}, time.Now().UTC())
	if err != nil {
		return e, err
	}
	switch e.Action {
	case "sync-run", "sync-initialize":
		at, err := time.Parse(time.RFC3339Nano, p.plan.AsOf)
		now := time.Now().UTC()
		if err != nil || now.Before(at) || now.Sub(at) >= 5*time.Minute {
			return e, errors.New("sync plan stale")
		}
		for _, observation := range []s.Observation{a, b} {
			observed, err := time.Parse(time.RFC3339Nano, observation.ObservedAt)
			if err != nil || now.Before(observed) || now.Sub(observed) >= 5*time.Minute {
				return e, errors.New("sync metadata expired since planning")
			}
		}
		for _, name := range []string{"source", "destination"} {
			var manifest r.Manifest
			if DecodeRead(p.files["assets/"+name+"/bundle.json"], &manifest) != nil || manifest.OriginalObservedAt == nil {
				return e, errors.New("original sync observation required")
			}
			observed, err := time.Parse(time.RFC3339Nano, *manifest.OriginalObservedAt)
			if err != nil || now.Before(observed) || now.Sub(observed) >= 5*time.Minute {
				return e, errors.New("sync item observation expired since planning")
			}
		}

	case "sync-pause", "sync-resume", "sync-reconcile":
	default:
		return e, errors.New("unknown sync action")
	}
	return e, nil
}

type ExecuteSyncRequest struct {
	Plan, Invocation, Policy, Enrollment, Action, Out string
	Once                                              bool
}

func (svc *Service) ExecuteSync(ctx context.Context, q ExecuteSyncRequest) (y.Run, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	result := y.Run{Version: y.Version, Kind: "SyncRun", Operations: []o.Observation{}, Status: "blocked"}
	if q.Action == "sync-run" && !q.Once {
		return result, operationFailure("sync.once", "explicit --once required", 2, nil)
	}
	if err := setupOutput(q.Out, q.Plan); err != nil {
		return result, err
	}
	p, err := PrepareSync(ctx, q.Plan)
	if err != nil {
		return result, err
	}
	result.PlanSHA256 = p.Digest()
	result.State = p.plan.Candidate
	policy, err := ReadLocal(ctx, q.Policy)
	if err != nil {
		return result, err
	}
	enraw, err := ReadLocal(ctx, q.Enrollment)
	if err != nil {
		return result, err
	}
	en, err := ValidateSyncAuthority(p, policy, enraw)
	if err != nil || en.Action != q.Action || en.OutputPath != q.Out {
		return result, operationFailure("sync.authority", "invalid exact sync enrollment", 3, err)
	}
	if svc.sync == nil {
		return result, operationFailure("sync.unavailable", "protected sync enrollment unavailable", 3, nil)
	}
	prior, err := svc.sync.CurrentSync(ctx, p, en, policy)
	if err != nil {
		return result, err
	}

	// The raw state digest is authoritative; candidate baselines may differ during initialization.
	var declared y.State
	if DecodeRead(p.files["assets/state.json"], &declared) != nil || (check.Hash(prior) != check.Hash(declared) && !(q.Action == "sync-reconcile" && prior.PendingPlanSHA256 == p.Digest())) {
		return result, operationFailure("sync.conflict", "stale route revision", 4, nil)
	}
	next := cloneRead(prior)
	confirmed := []string{}
	switch q.Action {
	case "sync-initialize":
		if prior.Initialized || p.plan.Status != "baseline-candidate" || !p.plan.Complete || len(p.plan.Proposals) > 0 {
			return result, operationFailure("sync.initialization", "complete equal paired baseline candidate required", 3, nil)
		}
		next = p.plan.Candidate
		next.Initialized = true
	case "sync-pause":
		next.Paused = true
	case "sync-resume":
		if len(prior.Unresolved) > 0 {
			return result, operationFailure("sync.unresolved", "reconcile obligations before resume", 5, nil)
		}
		next.Paused = false
	case "sync-run", "sync-reconcile":
		if !prior.Initialized || q.Action == "sync-run" && (prior.Paused || len(prior.Unresolved) > 0 || p.plan.Status != "ready") {
			return result, operationFailure("sync.blocked", "initialized unpaused conflict-free route required", 3, nil)
		}
		var inv y.Invocation
		raw, err := ReadLocal(ctx, q.Invocation)
		if err != nil || DecodeRead(raw, &inv) != nil || inv.Version != y.Version || inv.Kind != "SyncInvocation" || inv.PlanSHA256 != p.Digest() || len(inv.Operations) != len(p.plan.Proposals) || len(inv.Operations) > 100 {
			return result, errors.New("exact bounded operation invocation required")
		}
		prepared := make([]PreparedOperation, len(inv.Operations))
		policyHashes, enrollmentHashes := make([]string, len(inv.Operations)), make([]string, len(inv.Operations))
		seen := map[string]bool{}
		for i, call := range inv.Operations {
			op, e := PrepareOperation(ctx, call.Plan)
			if e != nil {
				return result, e
			}
			d := op.Draft()
			var desired o.Desired
			if DecodeRead(op.files["assets/desired.json"], &desired) != nil {
				return result, errors.New("pinned desired operation required")
			}
			match := false
			for _, want := range p.plan.Proposals {
				if check.Hash(want) == check.Hash(desired) {
					match = true
				}
			}
			if !match || seen[d.ID] || d.Inputs["snapshot"] != p.plan.Inputs["destination"] || d.Inputs["binding"] != p.plan.Inputs["destination-binding"] || d.Inputs["mapping"] != p.plan.Inputs["mapping"] {
				return result, errors.New("operation not in exact approved sync set")
			}
			seen[d.ID] = true
			prepared[i] = op
			pol, e := ReadLocal(ctx, call.Policy)
			if e != nil {
				return result, e
			}
			auth, e := ReadLocal(ctx, call.Enrollment)
			if e != nil {
				return result, e
			}
			policyHashes[i] = SHA256(pol)
			enrollmentHashes[i] = SHA256(auth)
			oe, e := validateOperationEnrollment(op, pol, auth, time.Now().UTC())
			action := "apply"
			if q.Action == "sync-reconcile" {
				action = "reconcile"
			}
			if e != nil || oe.Action != action || oe.Store != en.Store || oe.OutputPath != call.Output {
				return result, errors.New("per-operation authority mismatch")
			}
		}
		w, err := engine.ReserveBundle(ctx, q.Out)
		if err != nil {
			return result, err
		}
		if q.Action == "sync-run" {
			prior, err = svc.sync.BeginSync(ctx, p, en, policy)
			if err != nil {
				return result, operationFailure("sync.admission", "run admission uncertain; inspect/reconcile without retry", 5, err)
			}
			next = cloneRead(prior)
		}
		for i, call := range inv.Operations {
			// Recheck route pause/ownership/authority before each admission, including partial runs.
			if _, err = ValidateSyncAuthority(p, policy, enraw); err != nil {
				break
			}
			if _, err = svc.sync.CurrentSync(ctx, p, en, policy); err != nil {
				break
			}
			action := "apply"
			if q.Action == "sync-reconcile" {
				action = "reconcile"
			}
			operationCtx := context.WithValue(ctx, syncAdmissionKey{}, syncAdmission{Route: p.plan.RouteSHA256, Revision: prior.Revision, Plan: p.Digest(), Action: q.Action, OperationDigest: prepared[i].Digest(), PolicyDigest: policyHashes[i], EnrollmentDigest: enrollmentHashes[i], Check: func(c context.Context) error {
				if _, e := ValidateSyncAuthority(p, policy, enraw); e != nil {
					return e
				}
				state, e := svc.sync.CurrentSync(c, p, en, policy)
				if e != nil {
					return e
				}
				if q.Action == "sync-run" && state.Paused || state.Revision != prior.Revision {
					return errors.New("sync admission paused or changed")
				}
				return nil
			}})
			obs, e := svc.ExecuteOperation(operationCtx, ExecuteOperationRequest{Plan: call.Plan, Policy: call.Policy, Enrollment: call.Enrollment, Action: action, Out: call.Output})
			result.Operations = append(result.Operations, obs)
			if e != nil || obs.State != "readback-verified" {
				continue
			}
			confirmed = append(confirmed, obs.OperationID)
		}
		latest, latestErr := svc.sync.CurrentSync(ctx, p, en, policy)
		if latestErr != nil {
			return result, operationFailure("sync.state", "effects require reconciliation after route drift", 5, latestErr)
		}
		prior = latest
		projected, projectionErr := ProjectSyncState(p, prior, q.Action, confirmed)
		if projectionErr != nil {
			return result, projectionErr
		}
		next = projected
		result.State = next
		result.Status = "partial"
		if len(next.Unresolved) == 0 && p.plan.Complete {
			result.Status = "confirmed"
		}

		if commitErr := svc.sync.CommitSync(ctx, p, en, policy, next, confirmed); commitErr != nil {
			result.Status = "uncertain"
			err = operationFailure("sync.commit", "provider effects may exist; reconcile without resend", 5, commitErr)
		}
		if e := w.Write(ctx, operationalReportFiles(y.Version, result, map[string][]byte{"assets/plan.json": p.raw, "assets/policy.json": policy, "assets/enrollment.json": enraw, "assets/invocation.json": raw})); e != nil {
			return result, operationFailure("sync.receipt", "sync receipt uncertain", 5, e)
		}
		if e := w.Sync(ctx); e != nil {
			return result, operationFailure("sync.receipt", "sync durability uncertain", 5, e)
		}
		if err != nil {
			return result, err
		}
		if len(next.Unresolved) > 0 {
			return result, operationFailure("sync.partial", "unresolved operations retained; no completed watermark", 6, nil)
		}
		return result, nil
	}
	next.Revision = prior.Revision + 1
	result.State = next
	if _, err = ValidateSyncAuthority(p, policy, enraw); err != nil {
		return result, err
	}
	w, err := engine.ReserveBundle(ctx, q.Out)
	if err != nil {
		return result, err
	}
	if err = svc.sync.CommitSync(ctx, p, en, policy, next, confirmed); err != nil {
		return result, operationFailure("sync.commit", "local route control requires reconciliation", 5, err)
	}
	result.Status = "confirmed"
	if err = w.Write(ctx, operationalReportFiles(y.Version, result, map[string][]byte{"assets/plan.json": p.raw, "assets/policy.json": policy, "assets/enrollment.json": enraw})); err != nil {
		return result, operationFailure("sync.receipt", "local control receipt uncertain", 5, err)
	}
	return result, w.Sync(ctx)
}
func updateConfirmedBaseline(p PreparedSync, next *y.State, d o.Draft) {
	for _, update := range p.plan.Updates {
		if update.ConfirmedOperationID != d.ID {
			continue
		}
		for i, prior := range next.Baselines {
			if prior.CorrelationID == update.CorrelationID && prior.Field == update.Field {
				next.Baselines[i] = cloneRead(update)
			}
		}
	}
}

func (svc *Service) InspectSync(ctx context.Context, store o.Store, route string) (y.State, error) {
	if svc.sync == nil {
		return y.State{}, operationFailure("sync.unavailable", "protected journal unavailable", 3, nil)
	}
	return svc.sync.InspectSync(ctx, store, route)
}

// ProjectSyncState is the only baseline transition used by protected persistence.
// It recomputes state from a validated plan and authoritative confirmed IDs.
func ProjectSyncState(p PreparedSync, prior y.State, action string, confirmed []string) (y.State, error) {
	next := cloneRead(prior)
	switch action {
	case "sync-initialize":
		if prior.Initialized || p.plan.Status != "baseline-candidate" || !p.plan.Complete || len(p.plan.Proposals) > 0 {
			return next, errors.New("initialization blocked")
		}
		next = p.plan.Candidate
		next.Initialized = true
	case "sync-pause":
		next.Paused = true
	case "sync-resume":
		if len(prior.Unresolved) > 0 {
			return next, errors.New("unresolved resume")
		}
		next.Paused = false
	case "sync-run", "sync-reconcile":
		if !prior.Initialized || action == "sync-run" && (p.plan.Status != "ready" || prior.PendingPlanSHA256 != p.Digest()) {
			return next, errors.New("sync admission blocked")
		}
		if action == "sync-reconcile" && prior.PendingPlanSHA256 != p.Digest() {
			return next, errors.New("wrong reconciliation lineage")
		}
		var config y.Configuration
		if DecodeRead(p.files["assets/configuration.json"], &config) != nil {
			return next, errors.New("configuration missing")
		}
		expected := map[string]bool{}
		for _, want := range p.plan.Proposals {
			for _, co := range config.Correlations {
				if co.Destination.NativeID == want.ItemID {
					expected[operationID(want.LogicalKey, co.Destination, want.Operation)] = true
				}
			}
		}
		seen := map[string]bool{}
		for _, id := range confirmed {
			if !expected[id] || seen[id] {
				return next, errors.New("unapproved confirmed operation")
			}
			seen[id] = true
			updateConfirmedBaseline(p, &next, o.Draft{ID: id})
		}
		next.Unresolved = []string{}
		for id := range expected {
			if !seen[id] {
				next.Unresolved = append(next.Unresolved, id)
			}
		}
		sort.Strings(next.Unresolved)
		next.PendingPlanSHA256 = p.Digest()
		if len(next.Unresolved) == 0 && p.plan.Complete {
			next.CompletedWatermark = p.Digest()
			next.PendingPlanSHA256 = ""
		}
	default:
		return next, errors.New("unsupported route control")
	}
	next.Revision = prior.Revision + 1
	return next, nil
}

type syncAdmissionKey struct{}
type syncAdmission struct {
	Route                                           string
	Revision                                        int64
	Plan                                            string
	Action                                          string
	OperationDigest, PolicyDigest, EnrollmentDigest string
	Check                                           func(context.Context) error
}

// SyncAdmissionScope is readable only from the context installed by bounded sync.
func SyncAdmissionScope(ctx context.Context) (string, int64, string, string) {
	v, _ := ctx.Value(syncAdmissionKey{}).(syncAdmission)
	return v.Route, v.Revision, v.Plan, v.Action
}
func CheckSyncAdmission(ctx context.Context) error {
	v, ok := ctx.Value(syncAdmissionKey{}).(syncAdmission)
	if !ok {
		return nil
	}
	return v.Check(ctx)
}

func PendingSyncState(p PreparedSync, prior y.State) (y.State, error) {
	if !prior.Initialized || prior.Paused || len(prior.Unresolved) > 0 || prior.PendingPlanSHA256 != "" || p.plan.Status != "ready" {
		return prior, errors.New("fresh clean unpaused route required")
	}
	next, err := ProjectSyncState(p, func() y.State { v := cloneRead(prior); v.PendingPlanSHA256 = p.Digest(); return v }(), "sync-run", nil)
	if err != nil {
		return prior, err
	}
	next.PendingPlanSHA256 = p.Digest()
	next.CompletedWatermark = prior.CompletedWatermark
	return next, nil
}

func CheckPinnedSyncOperation(ctx context.Context, p PreparedOperation, policy, enrollment []byte) error {
	gate, ok := ctx.Value(syncAdmissionKey{}).(syncAdmission)
	if !ok {
		return nil
	}
	if gate.OperationDigest != p.Digest() || gate.PolicyDigest != SHA256(policy) || gate.EnrollmentDigest != SHA256(enrollment) {
		return operationFailure("sync.operation-drift", "prepared operation or authority changed after approved-set validation", 4, nil)
	}
	return nil
}
