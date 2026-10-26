package sdk

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

type OperationBackend interface {
	OpenOperation(context.Context, PreparedOperation, []byte, []byte) (*OperationSession, error)
	InspectOperations(context.Context, o.Store, string) ([]o.Observation, error)
}

// OperationDriver is protected compiled integration plumbing. Implementations
// must use the authoritative durable outbox, not manufacture status from JSON.
type OperationDriver interface {
	Current(context.Context, PreparedOperation, o.Enrollment, []byte) error
	Apply(context.Context, PreparedOperation, o.Enrollment) (o.Observation, error)
	Reconcile(context.Context, PreparedOperation, o.Enrollment) (o.Observation, error)
	Cancel(context.Context, PreparedOperation, o.Enrollment) (o.Observation, error)
}
type OperationSession struct {
	prepared   PreparedOperation
	policy     []byte
	enrollment o.Enrollment
	driver     OperationDriver
	used       atomic.Bool
}

func (s *Service) WithOperationBackend(b OperationBackend) *Service {
	v := *s
	v.operations = b
	return &v
}
func OperationsImplementationSHA256() string                { return engine.ImplementationSHA256 }
func OperationEnrollmentSigningBytes(e o.Enrollment) []byte { return mutationSigningBytes(e) }
func mutationSigningBytes(e o.Enrollment) []byte {
	e.Signature = ""
	return append([]byte("workkit-operations/v1:"+e.Purpose+"\n"), engine.Encode(e)...)
}
func validateOperationEnrollment(p PreparedOperation, policyRaw, enrollmentRaw []byte, now time.Time) (o.Enrollment, error) {
	var cap o.Capability
	if DecodeRead(p.files["assets/capability.json"], &cap) != nil {
		return o.Enrollment{}, errors.New("pinned full capability scope required")
	}
	e, err := validateMutationAuthority(policyRaw, enrollmentRaw, p.Digest(), "operation-action", []s.Scope{cap.Scope}, now)
	if err != nil {
		return e, err
	}
	if e.Action != "apply" && e.Action != "reconcile" && e.Action != "cancel" {
		return e, errors.New("unsupported operation action")
	}
	if e.Action == "apply" {
		observed, err := time.Parse(time.RFC3339Nano, p.draft.ObservedAt)
		if err != nil || now.Before(observed) || now.Sub(observed) >= 5*time.Minute || p.draft.Status != "draft" {
			return e, errors.New("fresh eligible operation required")
		}
		var metadata s.Observation
		if DecodeRead(p.files["assets/metadata/bundle.json"], &metadata) != nil {
			return e, errors.New("captured metadata required")
		}
		for _, stamp := range []string{cap.ObservedAt, metadata.ObservedAt} {
			observed, err := time.Parse(time.RFC3339Nano, stamp)
			if err != nil || now.Before(observed) || now.Sub(observed) >= 5*time.Minute {
				return e, errors.New("operation evidence expired since planning")
			}
		}

	}
	return e, nil
}
func validateMutationAuthority(policyRaw, enrollmentRaw []byte, digest, purpose string, scopes []s.Scope, now time.Time) (o.Enrollment, error) {
	var e o.Enrollment
	var policy o.Policy
	if DecodeRead(policyRaw, &policy) != nil || DecodeRead(enrollmentRaw, &e) != nil || policy.Version != o.Version || policy.Kind != "OperationPolicy" || e.Version != o.Version || e.Kind != "OperationEnrollment" || e.Purpose != purpose || e.ID == "" || e.OperationSHA256 != digest || e.PolicySHA256 != SHA256(policyRaw) || e.ImplementationSHA256 != engine.ImplementationSHA256 || e.Store.Version != o.Version || e.Store.Kind != "IntegrationJournal" || e.Store.ID == "" || e.Store.Namespace == "" || e.Store.Owner == "" || !filepath.IsAbs(e.Store.Path) || filepath.Clean(e.Store.Path) != e.Store.Path || !filepath.IsAbs(e.OutputPath) || filepath.Clean(e.OutputPath) != e.OutputPath {
		return e, errors.New("invalid or mismatched operation enrollment")
	}
	allowed := false
	for _, a := range policy.AllowedActions {
		if a == e.Action {
			allowed = true
		}
	}
	if !allowed || policy.MaxOperations < 1 || policy.MaxOperations > 100 {
		return e, errors.New("operation action not approved")
	}
	for _, id := range policy.RevokedIDs {
		if id == e.ID {
			return e, errors.New("revoked enrollment")
		}
	}
	issue, ie := time.Parse(time.RFC3339Nano, e.IssuedAt)
	end, ee := time.Parse(time.RFC3339Nano, e.ExpiresAt)
	start, se := time.Parse(time.RFC3339Nano, policy.NotBefore)
	stop, ste := time.Parse(time.RFC3339Nano, policy.ExpiresAt)
	if ie != nil || ee != nil || se != nil || ste != nil || now.Before(issue) || !now.Before(end) || !end.After(issue) || end.Sub(issue) > 5*time.Minute || now.Before(start) || !now.Before(stop) || end.After(stop) {
		return e, errors.New("expired operation authority")
	}
	var key ed25519.PublicKey
	seen := map[string]bool{}
	for _, i := range policy.Issuers {
		b, err := base64.StdEncoding.DecodeString(i.PublicKey)
		if err != nil || len(b) != ed25519.PublicKeySize || i.ID == "" || seen[i.ID] {
			return e, errors.New("invalid issuer set")
		}
		seen[i.ID] = true
		if i.ID == e.Issuer {
			key = b
		}
	}
	sig, err := base64.StdEncoding.DecodeString(e.Signature)
	if err != nil || len(key) != ed25519.PublicKeySize || !ed25519.Verify(key, mutationSigningBytes(e), sig) {
		return e, errors.New("operation signature invalid")
	}
	for _, required := range scopes {
		scopeAllowed := false
		for _, scope := range policy.AllowedScopes {
			if scope == required {
				scopeAllowed = true
			}
		}
		if !scopeAllowed {
			return e, errors.New("operation outside scope policy")
		}
	}

	return e, nil
}
func NewOperationSession(ctx context.Context, p PreparedOperation, policy, enrollment []byte, driver OperationDriver) (*OperationSession, error) {
	if p.Digest() == SHA256(nil) || driver == nil {
		return nil, errors.New("protected operation driver unavailable")
	}
	e, err := validateOperationEnrollment(p, policy, enrollment, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err = driver.Current(ctx, p, e, policy); err != nil {
		return nil, err
	}
	return &OperationSession{prepared: p, policy: append([]byte(nil), policy...), enrollment: e, driver: driver}, nil
}
func (s *OperationSession) Run(ctx context.Context) (o.Observation, error) {
	if s == nil || !s.used.CompareAndSwap(false, true) {
		return o.Observation{}, errors.New("operation session consumed")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	before := o.Observation{Version: o.Version, Kind: "OperationObservation", OperationID: s.prepared.draft.ID, Digest: s.prepared.Digest(), Store: s.enrollment.Store, State: "blocked", ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Evidence: []api.Asset{}, Diagnostics: []api.Diagnostic{}}
	if _, e := validateOperationEnrollment(s.prepared, s.policy, engine.Encode(s.enrollment), time.Now().UTC()); e != nil {
		return before, operationFailure("operation.authority", "authority failed before send admission", 3, e)
	}
	if e := s.driver.Current(ctx, s.prepared, s.enrollment, s.policy); e != nil {
		return before, operationFailure("operation.preflight", "current controls failed before send admission", 3, e)
	}

	var result o.Observation
	var err error
	switch s.enrollment.Action {
	case "apply":
		result, err = s.driver.Apply(ctx, s.prepared, s.enrollment)
	case "reconcile":
		result, err = s.driver.Reconcile(ctx, s.prepared, s.enrollment)
	case "cancel":
		result, err = s.driver.Cancel(ctx, s.prepared, s.enrollment)
	default:
		return result, errors.New("unknown operation action")
	}
	return result, operationOutcomeError(result, err, s.enrollment.Action)

}

type ExecuteOperationRequest struct{ Plan, Policy, Enrollment, Action, Out string }

func (s *Service) ExecuteOperation(ctx context.Context, q ExecuteOperationRequest) (o.Observation, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var result o.Observation
	if e := setupOutput(q.Out, q.Plan); e != nil {
		return result, e
	}
	p, e := PrepareOperation(ctx, q.Plan)
	if e != nil {
		return result, e
	}
	policy, e := ReadLocal(ctx, q.Policy)
	if e != nil {
		return result, e
	}
	enrollment, e := ReadLocal(ctx, q.Enrollment)
	if e != nil {
		return result, e
	}
	if e = CheckPinnedSyncOperation(ctx, p, policy, enrollment); e != nil {
		return result, e
	}
	var declaration o.Enrollment
	if DecodeRead(enrollment, &declaration) != nil || declaration.Action != q.Action || declaration.OutputPath != q.Out {
		return result, operationFailure("operation.enrollment", "action/output mismatch", 2, nil)
	}
	if _, e = validateOperationEnrollment(p, policy, enrollment, time.Now().UTC()); e != nil {
		return result, operationFailure("operation.authority", "invalid or expired closed enrollment", 3, e)
	}
	w, e := engine.ReserveBundle(ctx, q.Out)
	if e != nil {
		return result, e
	}
	result = o.Observation{Version: o.Version, Kind: "OperationObservation", OperationID: p.draft.ID, Digest: p.Digest(), State: "blocked", Store: declaration.Store, ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), Evidence: []api.Asset{}, Diagnostics: []api.Diagnostic{}}
	nativeFiles := map[string][]byte{}
	if s.operations == nil {
		e = operationFailure("operation.unavailable", "protected writer enrollment unavailable", 3, nil)
	} else {
		var session *OperationSession
		session, e = s.operations.OpenOperation(ctx, p, policy, enrollment)
		if e == nil {
			result, e = session.Run(ctx)
			nativeFiles = session.Evidence()
		}
	}
	if result.OperationID != p.draft.ID || result.Digest != p.Digest() || result.Store != declaration.Store || result.AuthorizationGranted || result.ExecutionReady || result.MigrationActivated || result.ProductionActivated {
		return result, operationFailure("operation.result", "unbound driver result", 5, nil)
	}
	e = operationOutcomeError(result, e, declaration.Action)
	files := operationalReportFiles(o.Version, result, map[string][]byte{"assets/draft.json": p.raw, "assets/policy.json": policy, "assets/enrollment.json": enrollment})
	result.Evidence = []api.Asset{}
	for n, b := range nativeFiles {
		if !strings.HasPrefix(n, "assets/native/") {
			return result, operationFailure("operation.evidence", "unsafe evidence path", 5, nil)
		}
		files[n] = b
		result.Evidence = append(result.Evidence, api.Asset{Path: n, Sha256: api.Sha(SHA256(b))})
	}
	sort.Slice(result.Evidence, func(i, j int) bool { return result.Evidence[i].Path < result.Evidence[j].Path })
	planBytes := engine.Encode(p.draft.Plan)
	receipt, projectionErr := ProjectOperationReceipt(result, SHA256(planBytes))
	if projectionErr != nil {
		return result, operationFailure("operation.domain-receipt", "domain receipt projection failed; retain journal evidence", 5, projectionErr)
	}
	files["assets/domain-operation-plan.json"] = planBytes
	files["assets/domain-operation-receipt.json"] = engine.Encode(receipt)
	files = operationalReportFiles(o.Version, result, filesWithoutReports(files))
	if err := w.Write(ctx, files); err != nil {
		return result, operationFailure("operation.receipt", "receipt write uncertain", 5, err)
	}
	if err := w.Sync(ctx); err != nil {
		return result, operationFailure("operation.receipt", "receipt durability uncertain", 5, err)
	}
	return result, e
}
func (s *Service) InspectOperations(ctx context.Context, store o.Store, id string) ([]o.Observation, error) {
	if s.operations == nil {
		return nil, operationFailure("operation.unavailable", "journal inspection backend unavailable", 3, nil)
	}
	return s.operations.InspectOperations(ctx, store, id)
}

// ValidateOperationAuthority is a read-only guard for protected final admission.
func ValidateOperationAuthority(p PreparedOperation, policy, enrollment []byte) error {
	_, e := validateOperationEnrollment(p, policy, enrollment, time.Now().UTC())
	return e
}

func (s *OperationSession) Evidence() map[string][]byte {
	if driver, ok := s.driver.(interface{ OperationEvidence() map[string][]byte }); ok {
		return cloneBytes(driver.OperationEvidence())
	}
	return map[string][]byte{}
}
func filesWithoutReports(files map[string][]byte) map[string][]byte {
	out := map[string][]byte{}
	for n, b := range files {
		if n != "report.json" && n != "bundle.json" {
			out[n] = b
		}
	}
	return out
}

func operationFailure(code, message string, exit int, cause error) error {
	if exit == 5 {
		return &Error{Code: code, Message: message, ExitCode: 5, cause: cause}
	}
	return failure(code, message, exit, cause)
}

// An idempotent observation of an unresolved send is never successful execution.
// Only authoritative confirmed state or a confirmed pre-send cancel is success.
func operationOutcomeError(result o.Observation, cause error, action string) error {
	switch result.State {
	case "unknown-awaiting-reconciliation", "dispatch-intent", "response-observed":
		return operationFailure("operation.uncertain", "read-only reconciliation required; do not resend", 5, cause)
	case "readback-verified":
		if cause == nil {
			return nil
		}
		return operationFailure("operation.failed", "confirmed effect retained; inspect evidence failure", 1, cause)
	case "cancelled":
		if cause == nil && action == "cancel" {
			return nil
		}
		return operationFailure("operation.cancelled", "operation was cancelled; no resend authorized", 3, cause)
	case "rejected":
		return operationFailure("operation.rejected", "provider rejection retained", 1, cause)
	case "blocked", "prepared", "authorized", "planned-not-admitted":
		if cause != nil {
			var typed *Error
			if errors.As(cause, &typed) {
				return cause
			}
		}
		return operationFailure("operation.blocked", "operation not confirmed; inspect prerequisites without resend", 3, cause)
	default:
		return operationFailure("operation.uncertain", "unrecognized operation disposition; inspect durable state", 5, cause)
	}
}
