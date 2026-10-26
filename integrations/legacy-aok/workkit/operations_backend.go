package workkit

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	r "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/readv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"time"
)

// OperationRoute is supplied only by protected compiled bootstrap. Current must
// validate policy custody, actual P4 binding proof, exact live capability,
// disjoint namespace/global target-field ownership and current revocation.
// Observe revalidates complete scoped source/target preconditions; neither
// callback is selected by workspace data. No default route is installed.
type OperationRoute struct {
	Current        func(context.Context, sdk.PreparedOperation, o.Enrollment, []byte) error
	Observe        func(context.Context, sdk.PreparedOperation) error
	Activation     *operations.Activation
	Operation      contracts.SyncOperation
	Capability     contracts.ConnectorCapability
	Identity       contracts.RemoteIdentity
	Grant          writeback.Grant
	Policy         writeback.TrustPolicy
	Revocation     func(context.Context) (writeback.RevocationSnapshot, error)
	Context        writeback.GrantContext
	VerifyEvidence writeback.EvidenceVerifier
	WriterFactory  func(func(context.Context) error) (connector.Writer, error)
	CreateIntent   writeback.CreateIntent
	CreateGrant    writeback.CreateGrant
	CreatorFactory func(func(context.Context) error) (writeback.Creator, error)
	Readback       func(context.Context, sdk.PreparedOperation) (writeback.ReconciliationReport, error)
	CreateReadback func(context.Context, sdk.PreparedOperation) (writeback.CreateReadbackReport, error)
}
type OperationBackend struct {
	Bootstrap func(context.Context, sdk.PreparedOperation, o.Enrollment) (OperationRoute, error)
}

func MutationNamespaceSHA256(store o.Store) string { b, _ := json.Marshal(store); return sdk.SHA256(b) }
func storeIdentity(s o.Store) writeback.WorkkitStoreIdentity {
	return writeback.WorkkitStoreIdentity{Version: s.Version, Kind: s.Kind, Path: s.Path, ID: s.ID, Namespace: s.Namespace, Owner: s.Owner}
}
func PublicOperationStore(s writeback.WorkkitStoreIdentity) o.Store {
	return o.Store{Version: s.Version, Kind: s.Kind, Path: s.Path, ID: s.ID, Namespace: s.Namespace, Owner: s.Owner}
}
func (b OperationBackend) OpenOperation(ctx context.Context, p sdk.PreparedOperation, policy, enrollment []byte) (*sdk.OperationSession, error) {
	if b.Bootstrap == nil {
		return nil, &sdk.Error{Code: "operation.unavailable", Message: "protected mutation bootstrap unavailable", ExitCode: 3}
	}
	var e o.Enrollment
	if sdk.DecodeRead(enrollment, &e) != nil || sdk.ValidateOperationAuthority(p, policy, enrollment) != nil {
		return nil, errors.New("invalid operation enrollment")
	}
	route, err := b.Bootstrap(ctx, p, e)
	if err != nil {
		return nil, err
	}
	driver := &operationDriver{route: route, policy: append([]byte(nil), policy...), evidence: map[string][]byte{}, enrollment: append([]byte(nil), enrollment...)}
	return sdk.NewOperationSession(ctx, p, policy, enrollment, driver)
}
func (b OperationBackend) InspectOperations(ctx context.Context, store o.Store, id string) ([]o.Observation, error) {
	j, e := writeback.OpenWorkkit(ctx, store.Path, store.Namespace, store.Owner, false)
	if e != nil {
		return nil, e
	}
	defer j.Close()
	if j.Identity != storeIdentity(store) {
		return nil, errors.New("store identity mismatch")
	}
	ids := []string{id}
	if id == "" {
		ids, e = j.ListPlans(ctx)
		if e != nil {
			return nil, e
		}
	}
	out := []o.Observation{}
	for _, id := range ids {
		raw, e := j.Plan(ctx, id)
		if e != nil {
			return nil, e
		}
		var d o.Draft
		if sdk.DecodeRead(raw, &d) != nil {
			return nil, errors.New("invalid persisted draft")
		}
		entry, e := j.Box.Get(id)
		if e != nil {
			create, ce := j.Box.GetCreate(id)
			if ce != nil {
				if errors.Is(e, sql.ErrNoRows) && errors.Is(ce, sql.ErrNoRows) {
					out = append(out, observation(d, sdk.SHA256(raw), store, "planned-not-admitted", 0, 0))
					continue
				}
				return nil, e
			}
			out = append(out, observation(d, sdk.SHA256(raw), store, create.State, create.Fence, create.Attempts))
			continue
		}
		out = append(out, observation(d, sdk.SHA256(raw), store, entry.State, entry.Fence, entry.Attempts))
	}
	return out, nil
}
func observation(d o.Draft, digest string, store o.Store, state string, fence int64, attempts int) o.Observation {
	return o.Observation{Version: o.Version, Kind: "OperationObservation", OperationID: d.ID, Digest: digest, Store: store, State: state, Fence: fence, Attempts: attempts, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Evidence: []api.Asset{}, Diagnostics: []api.Diagnostic{}}
}

type operationDriver struct {
	route      OperationRoute
	policy     []byte
	evidence   map[string][]byte
	enrollment []byte
}

func (d *operationDriver) Current(ctx context.Context, p sdk.PreparedOperation, e o.Enrollment, policy []byte) error {
	if err := sdk.CheckPinnedSyncOperation(ctx, p, policy, d.enrollment); err != nil {
		return err
	}
	if err := sdk.CheckSyncAdmission(ctx); err != nil {
		return err
	}
	if err := sdk.ValidateOperationAuthority(p, policy, mustJSON(e)); err != nil {
		return err
	}
	if d.route.Current == nil || d.route.Observe == nil || d.route.Activation == nil || d.route.Activation.ScopeSHA256() != MutationNamespaceSHA256(e.Store) || sdk.SHA256(policy) != sdk.SHA256(d.policy) {
		return errors.New("exact protected namespace, observer and authority required")
	}
	if err := d.route.Current(ctx, p, e, policy); err != nil {
		return err
	}
	if err := d.route.Activation.Check(ctx); err != nil {
		return err
	}
	j, err := writeback.OpenWorkkit(ctx, e.Store.Path, e.Store.Namespace, e.Store.Owner, false)
	if err != nil {
		return err
	}
	defer j.Close()
	if j.Identity != storeIdentity(e.Store) {
		return errors.New("current store identity mismatch")
	}
	return matchCompiledOperation(p, d.route)
}
func matchCompiledOperation(p sdk.PreparedOperation, c OperationRoute) error {
	d := p.Draft()
	files := p.Assets()
	var cfg r.Configuration
	if sdk.DecodeRead(files["assets/capture/assets/configuration.json"], &cfg) != nil {
		return errors.New("captured configuration required")
	}
	raw, _ := json.Marshal(cfg.Source)
	var source tracker.Source
	if tracker.Decode(raw, "source", &source) != nil {
		return errors.New("provider source unavailable")
	}
	t := d.Target
	provider := t.Provider
	if provider == "jira-cloud" {
		provider = "jira"
	}
	identity := contracts.RemoteIdentity{Provider: provider, TenantID: tracker.TenantScopeID(source), ContainerID: t.ContainerID, RemoteID: t.NativeID, ItemKind: t.ItemKind}
	if d.Operation == "create-item" || d.Operation == "provision-field" || d.Operation == "provision-option" {
		i := c.CreateIntent
		if i.ID != d.ID || i.ClientCorrelationID != d.ID || i.Parent != identity || i.TypeID != d.Inputs["typeId"] || i.SchemaDigest != d.Inputs["creationSchema"] || i.Budget != 1 {
			return errors.New("creator does not bind exact prepared parent/type/schema")
		}
		var fields map[string]json.RawMessage
		if writeback.Decode(i.Fields, &fields) != nil || !rawEqual(fields, d.Changes) {
			return errors.New("create fields differ")
		}
		return nil
	}
	op := c.Operation
	if op.OperationID != d.ID || op.OperationKind != d.Operation || op.ExpectedRevision != d.Revision.Value || c.Identity != identity || op.Authority.Scope != identity {
		return errors.New("compiled operation identity differs from neutral draft")
	}
	switch d.Operation {
	case "comment":
		asset := op.Change.Comment
		if asset == nil || asset.Path != "assets/comment.txt" || asset.SHA256 != sdk.SHA256(files[asset.Path]) {
			return errors.New("exact captured comment asset required")
		}
		var body string
		if json.Unmarshal(d.Changes["body"], &body) != nil || string(files[asset.Path]) != body {
			return errors.New("comment body differs")
		}
	case "update-fields":
		fields := map[string]json.RawMessage{}
		for _, f := range op.Change.Fields {
			if _, ok := fields[f.ProviderFieldID]; ok {
				return errors.New("duplicate compiled field")
			}
			fields[f.ProviderFieldID] = f.Value
		}
		if !rawEqual(fields, d.Changes) {
			return errors.New("compiled field values differ")
		}
	default:
		return errors.New("concrete operation unavailable")
	}
	return nil
}
func rawEqual(a, b map[string]json.RawMessage) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
func (d *operationDriver) Apply(ctx context.Context, p sdk.PreparedOperation, e o.Enrollment) (o.Observation, error) {
	draft := p.Draft()
	result := observation(draft, p.Digest(), e.Store, "blocked", 0, 0)
	if err := d.Current(ctx, p, e, d.policy); err != nil {
		return result, err
	}
	if err := d.route.Observe(ctx, p); err != nil {
		return result, err
	}
	if err := d.Current(ctx, p, e, d.policy); err != nil {
		return result, err
	}
	j, err := writeback.OpenWorkkitWritable(ctx, storeIdentity(e.Store))
	if err != nil {
		return result, err
	}
	defer j.Close()
	if err = d.capture(ctx, j, p.Draft().ID, map[string]any{"operation": d.route.Operation, "capability": d.route.Capability, "grant": d.route.Grant, "policy": d.route.Policy, "createIntent": d.route.CreateIntent, "createGrant": d.route.CreateGrant}); err != nil {
		return result, err
	}
	if route, revision, plan, action := sdk.SyncAdmissionScope(ctx); route != "" && action == "sync-run" {
		if err = j.Box.BindWorkkitAdmission(route, revision, plan, draft.ID); err != nil {
			return result, err
		}
	}

	if err = j.PutPlan(ctx, draft.ID, p.Digest(), draft.Operation, p.Bytes()); err != nil {
		return result, err
	}
	for _, dep := range draft.DependsOn {
		row, err := j.Box.Get(dep)
		if err != nil || row.State != "readback-verified" {
			return result, errors.New("confirmed dependency required")
		}
	}
	if draft.Operation == "create-item" || draft.Operation == "provision-field" || draft.Operation == "provision-option" {
		return d.applyCreate(ctx, p, e, j)
	}
	if existing, err := j.Box.Get(draft.ID); err == nil {
		return observation(draft, p.Digest(), e.Store, existing.State, existing.Fence, existing.Attempts), nil
	}
	if d.route.WriterFactory == nil || d.route.Revocation == nil {
		return result, errors.New("qualified writer/current revocation unavailable")
	}
	rev, err := d.route.Revocation(ctx)
	if err != nil {
		return result, err
	}
	now := time.Now().UTC()
	approved, err := writeback.VerifyGrantBound(d.route.Grant, d.route.Policy, rev, d.route.Operation, d.route.Capability, d.route.Identity, d.route.Context, d.route.VerifyEvidence, now)
	if err != nil {
		return result, err
	}
	if err = j.Box.ImportRevocation(d.route.Policy, rev, now); err != nil {
		return result, err
	}
	if _, err = j.Box.Enqueue(d.route.Operation, approved); err != nil {
		return result, err
	}
	guard := func(c context.Context) error {
		if err := d.Current(c, p, e, d.policy); err != nil {
			return err
		}
		rev, err := d.route.Revocation(c)
		if err != nil {
			return err
		}
		_, err = writeback.VerifyGrantBound(d.route.Grant, d.route.Policy, rev, d.route.Operation, d.route.Capability, d.route.Identity, d.route.Context, d.route.VerifyEvidence, time.Now().UTC())
		return err
	}
	inner, err := d.route.WriterFactory(guard)
	if err != nil {
		return result, err
	}
	if inner == nil {
		return result, errors.New("writer unavailable")
	}
	writer := &currentWriter{inner: inner, check: guard}
	row, sendErr := connector.DispatchOnceActivated(ctx, j.Box, writer, d.route.Operation, d.route.Capability, d.route.Identity, d.route.Grant, d.route.Policy, rev, d.route.Context, d.route.VerifyEvidence, e.Store.Owner, now, d.route.Activation, MutationNamespaceSHA256(e.Store))
	result = observation(draft, p.Digest(), e.Store, row.State, row.Fence, row.Attempts)
	if sendErr != nil {
		return result, sendErr
	}
	return d.reconcileItem(ctx, p, e, j, result)
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }

type currentWriter struct {
	inner connector.Writer
	check func(context.Context) error
}

func (w *currentWriter) Apply(ctx context.Context, p connector.ValidatedOperation, a connector.AuthorizationContext) (connector.Result[contracts.SyncReceipt], error) {
	if err := w.check(ctx); err != nil {
		return connector.Result[contracts.SyncReceipt]{Disposition: connector.Unknown}, err
	}
	return w.inner.Apply(ctx, p, a)
}
func (w *currentWriter) Reconcile(ctx context.Context, p connector.ValidatedOperation, r contracts.SyncReceipt) (connector.Result[contracts.SyncReceipt], error) {
	if err := w.check(ctx); err != nil {
		return connector.Result[contracts.SyncReceipt]{Disposition: connector.Unknown}, err
	}
	return w.inner.Reconcile(ctx, p, r)
}
func (d *operationDriver) reconcileItem(ctx context.Context, p sdk.PreparedOperation, e o.Enrollment, j *writeback.WorkkitJournal, result o.Observation) (o.Observation, error) {
	if d.route.Readback == nil {
		return result, errors.New("independently signed readback unavailable; reconcile without resend")
	}
	if err := d.Current(ctx, p, e, d.policy); err != nil {
		return result, err
	}
	report, err := d.route.Readback(ctx, p)
	if err != nil {
		return result, err
	}
	if err = d.capture(ctx, j, p.Draft().ID, report); err != nil {
		return result, err
	}
	verified, err := writeback.VerifyReconciliation(report, d.route.Policy, d.route.Operation, d.route.Identity, time.Now().UTC())
	if err != nil {
		return result, err
	}
	row, err := j.Box.ReconcileVerified(verified)
	if err != nil {
		return result, err
	}
	result = observation(p.Draft(), p.Digest(), e.Store, row.State, row.Fence, row.Attempts)
	result.Evidence = []api.Asset{}
	for _, a := range report.Evidence {
		result.Evidence = append(result.Evidence, api.Asset{Path: a.Path, Sha256: api.Sha(a.SHA256)})
	}
	return result, nil
}
func (d *operationDriver) Reconcile(ctx context.Context, p sdk.PreparedOperation, e o.Enrollment) (o.Observation, error) {
	result := observation(p.Draft(), p.Digest(), e.Store, "blocked", 0, 0)
	if err := d.Current(ctx, p, e, d.policy); err != nil {
		return result, err
	}
	j, err := writeback.OpenWorkkitWritable(ctx, storeIdentity(e.Store))
	if err != nil {
		return result, err
	}
	defer j.Close()
	if p.Draft().Operation == "create-item" || p.Draft().Operation == "provision-field" || p.Draft().Operation == "provision-option" {
		return d.reconcileCreate(ctx, p, e, j)
	}
	row, err := j.Box.Get(p.Draft().ID)
	if err != nil {
		return result, err
	}
	result = observation(p.Draft(), p.Digest(), e.Store, row.State, row.Fence, row.Attempts)
	if row.State == "readback-verified" || row.State == "cancelled" || row.State == "rejected" {
		return result, nil
	}
	return d.reconcileItem(ctx, p, e, j, result)
}
func (d *operationDriver) Cancel(ctx context.Context, p sdk.PreparedOperation, e o.Enrollment) (o.Observation, error) {
	result := observation(p.Draft(), p.Digest(), e.Store, "blocked", 0, 0)
	if err := d.Current(ctx, p, e, d.policy); err != nil {
		return result, err
	}
	j, err := writeback.OpenWorkkitWritable(ctx, storeIdentity(e.Store))
	if err != nil {
		return result, err
	}
	defer j.Close()
	if p.Draft().Operation == "create-item" {
		return result, &sdk.Error{Code: "operation.cancel-unsupported", Message: "creation cancellation unavailable; reconcile without resend", ExitCode: 3}
	}
	if err = j.Cancel(ctx, p.Draft().ID); err != nil {
		return result, err
	}
	row, err := j.Box.Get(p.Draft().ID)
	if err != nil {
		return result, err
	}
	return observation(p.Draft(), p.Digest(), e.Store, row.State, row.Fence, row.Attempts), nil
}
func (d *operationDriver) applyCreate(ctx context.Context, p sdk.PreparedOperation, e o.Enrollment, j *writeback.WorkkitJournal) (o.Observation, error) {
	result := observation(p.Draft(), p.Digest(), e.Store, "blocked", 0, 0)
	if old, err := j.Box.GetCreate(p.Draft().ID); err == nil {
		return observation(p.Draft(), p.Digest(), e.Store, old.State, old.Fence, old.Attempts), nil
	}
	if d.route.CreatorFactory == nil || d.route.Revocation == nil {
		return result, errors.New("injected qualified creator/current revocation required")
	}
	rev, err := d.route.Revocation(ctx)
	if err != nil {
		return result, err
	}
	v, err := writeback.VerifyCreateGrant(d.route.CreateGrant, d.route.Policy, rev, d.route.CreateIntent, time.Now().UTC())
	if err != nil {
		return result, err
	}
	if err = j.Box.ImportRevocation(d.route.Policy, rev, time.Now().UTC()); err != nil {
		return result, err
	}
	if _, err = j.Box.EnqueueCreate(v); err != nil {
		return result, err
	}
	row, err := j.Box.ClaimCreate(p.Draft().ID, e.Store.Owner, time.Now().UTC(), 30*time.Second)
	if err != nil {
		return result, err
	}
	if err = d.route.Activation.Consume(ctx, p.Draft().ID, "provider-send", v.IntentDigest()); err != nil {
		return result, err
	}
	guard := func(c context.Context) error {
		if err := d.Current(c, p, e, d.policy); err != nil {
			return err
		}
		rev, err := d.route.Revocation(c)
		if err != nil {
			return err
		}
		_, err = writeback.VerifyCreateGrant(d.route.CreateGrant, d.route.Policy, rev, d.route.CreateIntent, time.Now().UTC())
		return err
	}
	inner, err := d.route.CreatorFactory(guard)
	if err != nil {
		return result, err
	}
	if inner == nil {
		return result, errors.New("creator unavailable")
	}
	creator := currentCreator{inner: inner, check: guard}
	row, err = j.Box.DispatchCreateOnce(ctx, creator, p.Draft().ID, e.Store.Owner, row.Fence, time.Now().UTC(), v)
	result = observation(p.Draft(), p.Digest(), e.Store, row.State, row.Fence, row.Attempts)
	if err != nil {
		return result, err
	}
	return d.reconcileCreate(ctx, p, e, j)
}

type currentCreator struct {
	inner writeback.Creator
	check func(context.Context) error
}

func (c currentCreator) Create(ctx context.Context, p writeback.CreationPermit) (writeback.CreateResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.check(ctx); err != nil {
		return writeback.CreateResponse{}, err
	}
	return c.inner.Create(ctx, p)
}
func (d *operationDriver) reconcileCreate(ctx context.Context, p sdk.PreparedOperation, e o.Enrollment, j *writeback.WorkkitJournal) (o.Observation, error) {
	row, err := j.Box.GetCreate(p.Draft().ID)
	result := observation(p.Draft(), p.Digest(), e.Store, row.State, row.Fence, row.Attempts)
	if err != nil {
		return result, err
	}
	if row.State == "readback-verified" {
		return result, nil
	}
	if d.route.CreateReadback == nil {
		return result, errors.New("independent creation readback required")
	}
	if err := d.Current(ctx, p, e, d.policy); err != nil {
		return result, err
	}
	report, err := d.route.CreateReadback(ctx, p)
	if err != nil {
		return result, err
	}
	if err = d.capture(ctx, j, p.Draft().ID, report); err != nil {
		return result, err
	}
	verified, err := writeback.VerifyHistoricalCreateReadback(report, d.route.Policy, d.route.CreateIntent, time.Now().UTC())
	if err != nil {
		return result, err
	}
	row, err = j.Box.ReconcileCreateVerified(verified)
	result = observation(p.Draft(), p.Digest(), e.Store, row.State, row.Fence, row.Attempts)
	for _, a := range report.Evidence {
		result.Evidence = append(result.Evidence, api.Asset{Path: a.Path, Sha256: api.Sha(a.SHA256)})
	}
	return result, err
}

func (d *operationDriver) capture(ctx context.Context, j *writeback.WorkkitJournal, id string, v any) error {
	if err := j.PutEvidence(ctx, id, mustJSON(v)); err != nil {
		return err
	}
	files, err := j.Evidence(ctx, id)
	if err != nil {
		return err
	}
	d.evidence = files
	return nil
}
func (d *operationDriver) OperationEvidence() map[string][]byte {
	out := map[string][]byte{}
	for n, b := range d.evidence {
		out[n] = append([]byte(nil), b...)
	}
	return out
}
