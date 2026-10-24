package provision

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type authority struct{ err error }

func (a authority) Verify(context.Context, Plan, Change, time.Time) error { return a.err }

type boundary struct {
	seen   map[string]bool
	states map[string]string
}

func (b *boundary) Consume(_ context.Context, id, d string) error {
	if b.seen[id] {
		return errors.New("consumed")
	}
	b.seen[id] = true
	return nil
}
func (b *boundary) Record(_ context.Context, id, state string) error {
	if !b.seen[id] {
		return errors.New("not consumed")
	}
	b.states[id] = state
	return nil
}

type writer struct {
	o     Observation
	sent  int
	fail  bool
	wrong bool
}

func (w *writer) Observe(context.Context, Scope) (Observation, error) { return w.o, nil }
func (w *writer) Send(context.Context, Scope, Change, string) error {
	w.sent++
	if w.fail {
		return errors.New("lost response")
	}
	return nil
}
func (w *writer) Readback(_ context.Context, _ Scope, c Change) (Resource, error) {
	r := c.Resource
	r.ID = "native-field"
	if w.wrong {
		r.Name = "different"
	}
	return r, nil
}
func fixture(t *testing.T) (Plan, *writer) {
	t.Helper()
	at := time.Now().UTC()
	scope := Scope{Provider: "github", Origin: "https://github.com", TenantID: "org-id", ProjectID: "project-id"}
	o := Observation{Version: Version, Scope: scope, ObservedAt: at.Format(time.RFC3339Nano), Revision: "rev", Complete: true, Resources: []Resource{}}
	d := Desired{Version: Version, Scope: scope, Resources: []Resource{{Kind: "project-field", Name: "AOK", Value: json.RawMessage(`{"type":"TEXT"}`)}}}
	p, e := Build(o, d, at)
	if e != nil || p.Status != "draft" {
		t.Fatal(p, e)
	}
	return p, &writer{o: o}
}
func TestOneSendResponseLossAndReconciliation(t *testing.T) {
	p, w := fixture(t)
	w.fail = true
	b := &boundary{map[string]bool{}, map[string]string{}}
	r, e := Apply(context.Background(), p, 0, authority{}, b, w)
	if e == nil || r.Disposition != "unknown-awaiting-reconciliation" || w.sent != 1 {
		t.Fatal(r, e)
	}
	_, _ = Apply(context.Background(), p, 0, authority{}, b, w)
	if w.sent != 1 {
		t.Fatal("resent ambiguous creation")
	}
	r, e = Reconcile(context.Background(), p, 0, authority{}, b, w)
	if e != nil || r.Disposition != "readback-observed" || w.sent != 1 {
		t.Fatal(r, e)
	}
}
func TestBadReadbackAuthorityAndDrift(t *testing.T) {
	p, w := fixture(t)
	b := &boundary{map[string]bool{}, map[string]string{}}
	w.wrong = true
	if r, e := Apply(context.Background(), p, 0, authority{}, b, w); e == nil || r.Disposition == "readback-observed" {
		t.Fatal("unverified readback")
	}
	p, w = fixture(t)
	if _, e := Apply(context.Background(), p, 0, authority{errors.New("revoked")}, b, w); e == nil || w.sent != 0 {
		t.Fatal("revoked grant sent")
	}
	w.o.Revision = "drift"
	if _, e := Apply(context.Background(), p, 0, authority{}, b, w); e == nil || w.sent != 0 {
		t.Fatal("drift sent")
	}
}
func TestNoDestructiveAzureSystemOrDuplicateObservation(t *testing.T) {
	p, w := fixture(t)
	d := Desired{Version: Version, Scope: p.Scope, Resources: []Resource{{Kind: "delete-project", Name: "bad", Value: json.RawMessage(`{}`)}}}
	out, e := Build(w.o, d, time.Now().UTC())
	if e != nil || out.Status != "blocked" {
		t.Fatal(out, e)
	}
	w.o.Scope.Provider = "azure-devops"
	w.o.Scope.ProcessType = "system"
	d.Scope = w.o.Scope
	out, e = Build(w.o, d, time.Now().UTC())
	if e != nil || out.Status != "blocked" {
		t.Fatal(out, e)
	}
	_, w = fixture(t)
	r := Resource{ID: "1", Name: "duplicate", Kind: "project-field", Value: json.RawMessage(`{}`)}
	w.o.Resources = []Resource{r, r}
	w.o.Resources[1].ID = "2"
	d.Scope = w.o.Scope
	if _, e = Build(w.o, d, time.Now().UTC()); e == nil {
		t.Fatal("ambiguous names")
	}
}
