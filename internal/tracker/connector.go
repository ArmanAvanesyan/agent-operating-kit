package tracker

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/connector"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"time"
)

// Reader implements the P1 boundary with the exact operator-configured source.
// It has no Writer or constructor for an authorization context.
type Reader struct {
	Source    Source
	Transport Transport
}

func (r Reader) Describe() contracts.ConnectorCapability { return Capability(r.Source) }
func (r Reader) inScope(id contracts.RemoteIdentity) bool {
	return id.Provider == r.Source.Provider && id.TenantID == TenantScopeID(r.Source) && id.ContainerID == r.Source.ContainerID
}
func (r Reader) Discover(ctx context.Context, id contracts.RemoteIdentity) (connector.Result[[]contracts.RemoteIdentity], error) {
	out := connector.Result[[]contracts.RemoteIdentity]{Disposition: connector.Unknown}
	if !r.inScope(id) {
		return out, fmt.Errorf("reader discovery scope mismatch")
	}
	c, e := CaptureSource(ctx, r.Source, "capture", nil, r.Transport)
	if e != nil {
		return out, e
	}
	for _, item := range c.Snapshot.Items {
		out.Value = append(out.Value, item.Identity)
	}
	if c.Snapshot.Complete {
		out.Disposition = connector.Supported
	}
	return out, nil
}
func (r Reader) Read(ctx context.Context, id contracts.RemoteIdentity) (connector.Result[connector.ReadResult], error) {
	out := connector.Result[connector.ReadResult]{Disposition: connector.Unknown}
	if !r.inScope(id) {
		return out, fmt.Errorf("reader item scope mismatch")
	}
	c, e := CaptureSource(ctx, r.Source, "capture", nil, r.Transport)
	if e != nil {
		return out, e
	}
	for _, item := range c.Snapshot.Items {
		if Digest(item.Identity) == Digest(id) {
			b, _ := json.Marshal(item)
			out.Value = connector.ReadResult{Identity: item.Identity, Revision: item.Revision, Captured: b}
			if c.Snapshot.Complete && item.Complete {
				out.Disposition = connector.Supported
			}
			return out, nil
		}
	}
	return out, nil
}

type Mapper struct{}

func (Mapper) Normalize(r connector.ReadResult) (connector.Result[connector.PortableItem], error) {
	out := connector.Result[connector.PortableItem]{Disposition: connector.Unknown}
	var item Item
	if e := usage.Decode(r.Captured, &item); e != nil {
		return out, e
	}
	if Digest(item.Identity) != Digest(r.Identity) || r.Revision != item.Revision {
		return out, fmt.Errorf("captured item identity/revision mismatch")
	}
	fields := map[string]any{}
	for k, v := range item.Fields {
		var raw any
		if e := usage.Decode(v, &raw); e != nil {
			return out, e
		}
		fields[k] = raw
	}
	out.Value = connector.PortableItem{Identity: item.Identity, Revision: item.Revision, Fields: fields}
	if item.Complete {
		out.Disposition = connector.Supported
	}
	return out, nil
}
func (Mapper) PlanUpdate(connector.PortableItem, map[string]any, contracts.ConnectorCapability) (connector.Result[contracts.SyncOperation], error) {
	return connector.Result[contracts.SyncOperation]{Disposition: connector.Unsupported, Diagnostics: []contracts.Diagnostic{{Code: "tracker.draft-only", Severity: "error", Message: "use DraftPlanner; P5 cannot construct an authorized SyncOperation"}}}, nil
}

type LocalPlanner struct{}

func (LocalPlanner) Plan(c *Capture, m Mapping, d Desired, at time.Time) (TrackerUpdatePlan, error) {
	return PlanUpdate(c, m, d, at)
}

var _ connector.Reader = Reader{}
var _ connector.Mapper = Mapper{}
var _ DraftPlanner = LocalPlanner{}
