package sdk

import (
	"context"
	"errors"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"os"
	"time"
)

// Normalization is an internal driver argument, never a v1 signed envelope.
// Its version/store stay v2. Verification always uses original integration bytes.
func ValidateIntegrationOperation(p PreparedOperation, policy, enrollment []byte, now time.Time) (o.Enrollment, error) {
	var original i.Enrollment
	if DecodeRead(enrollment, &original) != nil {
		return o.Enrollment{}, errors.New("integration operation envelope required")
	}
	start, startErr := CurrentProcessStart()
	if startErr != nil || original.ProcessID != os.Getpid() || original.ProcessStart != start {
		return o.Enrollment{}, errors.New("operation grant belongs to another process; use owner IPC")
	}
	action := ""
	switch original.Purpose {
	case "provider-send":
		action = "apply"
	case "operation-reconcile":
		action = "reconcile"
	case "operation-cancel":
		action = "cancel"
	default:
		return o.Enrollment{}, errors.New("operation-specific purpose required")
	}
	_, e, err := ValidateIntegrationAuthority(policy, enrollment, p.Digest(), original.Purpose, now)
	if err != nil {
		return o.Enrollment{}, err
	}
	if e.RouteID == "" || e.WorkerID == "" || e.Fence < 1 {
		return o.Enrollment{}, errors.New("exact worker route/fence required")
	}
	if action == "apply" {
		if p.draft.Status != "draft" || p.draft.Operation != "update-fields" {
			return o.Enrollment{}, errors.New("P7 dispatch admits approved ordinary field patches only")
		}
		for _, name := range []string{"assets/capability.json", "assets/metadata/bundle.json"} {
			var at string
			if name == "assets/capability.json" {
				var cap o.Capability
				if DecodeRead(p.files[name], &cap) != nil {
					return o.Enrollment{}, errors.New("full capability required")
				}
				at = cap.ObservedAt
			} else {
				var meta s.Observation
				if DecodeRead(p.files[name], &meta) != nil {
					return o.Enrollment{}, errors.New("metadata required")
				}
				at = meta.ObservedAt
			}
			t, err := nativeTime(at)
			if err != nil || now.Before(t) || now.Sub(t) >= 5*time.Minute {
				return o.Enrollment{}, errors.New("operation evidence stale")
			}
		}
		at, err := nativeTime(p.draft.ObservedAt)
		if err != nil || now.Before(at) || now.Sub(at) >= 5*time.Minute {
			return o.Enrollment{}, errors.New("operation stale")
		}
	}
	return o.Enrollment{Version: i.Version, Kind: "IntegrationEnrollment", ID: e.ID, Purpose: e.Purpose, OperationSHA256: e.SubjectSHA256, Store: o.Store{Version: e.Store.Version, Kind: e.Store.Kind, Path: e.Store.Path, ID: e.Store.ID, Namespace: e.Store.Namespace, Owner: e.Store.Owner}, Action: action, PolicySHA256: e.PolicySHA256, ImplementationSHA256: e.ImplementationSHA256, OutputPath: e.OutputPath, IssuedAt: e.IssuedAt, ExpiresAt: e.ExpiresAt, Issuer: e.Issuer, Signature: e.Signature}, nil
}
func NewIntegrationOperationSession(ctx context.Context, p PreparedOperation, policy, enrollment []byte, driver OperationDriver) (*OperationSession, error) {
	if driver == nil {
		return nil, errors.New("protected shared coordinator required")
	}
	e, err := ValidateIntegrationOperation(p, policy, enrollment, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err = driver.Current(ctx, p, e, policy); err != nil {
		return nil, err
	}
	return &OperationSession{prepared: p, policy: append([]byte(nil), policy...), rawEnrollment: append([]byte(nil), enrollment...), enrollment: e, driver: driver, validate: ValidateIntegrationOperation}, nil
}
func IntegrationOperationReport(ctx context.Context, session *OperationSession, out string) (o.Observation, error) {
	if session == nil || out != session.enrollment.OutputPath || out == "" {
		return o.Observation{}, errors.New("exact fresh operation output required")
	}
	w, e := engine.ReserveBundle(ctx, out)
	if e != nil {
		return o.Observation{}, e
	}
	r, err := session.Run(ctx)
	preserve, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	if e = w.Write(preserve, operationalReportFiles(i.Version, r, session.Evidence())); e != nil {
		return r, e
	}
	if e = w.Sync(preserve); e != nil {
		return r, e
	}
	return r, err
}
