package workkit

import (
	"context"
	"errors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	o "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/operationsv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/sdk"
	"time"
)

func (d *operationDriver) validateAuthority(p sdk.PreparedOperation, policy []byte) error {
	if d.integration {
		_, e := sdk.ValidateIntegrationOperation(p, policy, d.enrollment, time.Now().UTC())
		return e
	}
	return sdk.ValidateOperationAuthority(p, policy, d.enrollment)
}
func (d *operationDriver) openStore(ctx context.Context, s o.Store, writable bool) (*writeback.WorkkitJournal, error) {
	if d.integration {
		if s.Version != i.JournalVersion {
			return nil, errors.New("integration requires actual journal/v2")
		}
		if writable {
			return writeback.OpenWorkkitV2Writable(ctx, storeIdentity(s))
		}
		return writeback.OpenWorkkitV2(ctx, s.Path, s.Namespace, s.Owner, false)
	}
	if writable {
		return writeback.OpenWorkkitWritable(ctx, storeIdentity(s))
	}
	return writeback.OpenWorkkit(ctx, s.Path, s.Namespace, s.Owner, false)
}
func (d *operationDriver) bindAdmission(ctx context.Context, j *writeback.WorkkitJournal, e o.Enrollment, route string, revision int64, plan, id string) error {
	if !d.integration {
		return j.Box.BindWorkkitAdmission(route, revision, plan, id)
	}
	var en i.Enrollment
	if sdk.DecodeRead(d.enrollment, &en) != nil {
		return errors.New("original v2 authority required")
	}
	return j.Box.BindWorkkitAdmissionV2(route, revision, plan, id, en.WorkerID, en.Fence)
}

// IntegrationOperationBackend supplies only a versioned front door into the same
// P5 driver, grant verifier, DispatchOnceActivated, readback and outbox.
type IntegrationOperationBackend struct {
	Bootstrap func(context.Context, sdk.PreparedOperation, i.Enrollment) (OperationRoute, error)
}

func (b IntegrationOperationBackend) Open(ctx context.Context, p sdk.PreparedOperation, policy, raw []byte) (*sdk.OperationSession, error) {
	if b.Bootstrap == nil {
		return nil, &sdk.Error{Code: "integration.unqualified", Message: "protected v2 operation bootstrap missing", ExitCode: 3}
	}
	if _, e := sdk.ValidateIntegrationOperation(p, policy, raw, time.Now().UTC()); e != nil {
		return nil, e
	}
	var en i.Enrollment
	if sdk.DecodeRead(raw, &en) != nil {
		return nil, errors.New("v2 enrollment required")
	}
	route, e := b.Bootstrap(ctx, p, en)
	if e != nil {
		return nil, e
	}
	originalCurrent := route.Current
	route.Current = func(c context.Context, p sdk.PreparedOperation, e o.Enrollment, policy []byte) error {
		if originalCurrent == nil {
			return errors.New("external global ownership/route proof required")
		}
		j, err := writeback.OpenWorkkitV2(c, en.Store.Path, en.Store.Namespace, en.Store.Owner, false)
		if err != nil {
			return err
		}
		defer j.Close()
		if err = j.CheckWorker(c, en.RouteID, en.WorkerID, en.Fence, time.Now().UTC()); err != nil {
			return err
		}
		return originalCurrent(c, p, e, policy)
	}
	driver := &operationDriver{route: route, policy: append([]byte(nil), policy...), enrollment: append([]byte(nil), raw...), evidence: map[string][]byte{}, integration: true}
	return sdk.NewIntegrationOperationSession(ctx, p, policy, raw, driver)
}
