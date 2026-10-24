package runtime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
)

type testPilotWitness struct{ e operations.Epoch }

func (w *testPilotWitness) Current(context.Context, string) (operations.Epoch, error) {
	return w.e, nil
}
func (w *testPilotWitness) Consume(context.Context, operations.Consumption) error { return nil }

type testPilotVerifier struct{}

func (testPilotVerifier) VerifyWitness(context.Context, operations.AuthorityWitness, string, string, []byte, time.Time) error {
	return nil
}
func TestPilotActivationRejectsScopeAndSupersededAuthority(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	scope := strings.Repeat("a", 64)
	w := &testPilotWitness{operations.Epoch{Authority: "protected", Holder: "one", ScopeSHA256: scope, Generation: 1, ObservedAt: now, ValidUntil: now.Add(time.Minute)}}
	a, e := operations.Bind(ctx, w, testPilotVerifier{}, "protected", "one", scope, []byte("fixture"), func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	c := &Controller{Policy: records.Policy{AllowDispatch: true}}
	trials := []PilotTrialBinding{{RunID: "run", StepID: "step", TrialID: "stable-trial", ManifestSHA256: scope, HostPolicySHA256: scope, ConfigurationSHA256: scope, ProfileSHA256: scope, NativeRunner: true}}
	if c.ConfigurePilotActivation(ctx, a, strings.Repeat("b", 64), trials) == nil {
		t.Fatal("wrong route activated")
	}
	if c.ConfigurePilotActivation(ctx, a, scope, trials) != nil {
		t.Fatal("fixture bind failed")
	}
	w.e.Generation = 2
	r, e := c.Dispatch(ctx, "run")
	if e != nil {
		t.Fatal(e)
	}
	if r.Status != "blocked" {
		t.Fatalf("superseded authority reached runtime state: %+v", r)
	}
}
