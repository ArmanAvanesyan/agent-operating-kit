package operations

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fixtureWitness struct {
	mu    sync.Mutex
	epoch Epoch
	used  map[string]bool
}

func (w *fixtureWitness) Current(ctx context.Context, id string) (Epoch, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.epoch, nil
}
func (w *fixtureWitness) Consume(ctx context.Context, c Consumption) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c.Epoch != w.epoch || w.used[c.LogicalID] {
		return errors.New("superseded or spent")
	}
	w.used[c.LogicalID] = true
	return nil
}

type fixtureVerifier struct{}

func (fixtureVerifier) VerifyWitness(context.Context, AuthorityWitness, string, string, []byte, time.Time) error {
	return nil
}
func fixtureActivation(t *testing.T) (*Activation, *fixtureWitness) {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	w := &fixtureWitness{epoch: Epoch{"external-authority", "holder", stringHash, 1, now, now.Add(time.Minute)}, used: map[string]bool{}}
	a, e := Bind(context.Background(), w, fixtureVerifier{}, "external-authority", "holder", stringHash, []byte("fixture"), func() time.Time { return now })
	if e != nil {
		t.Fatal(e)
	}
	return a, w
}

const stringHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestCrossEpochConsumption(t *testing.T) {
	a, w := fixtureActivation(t)
	ctx := context.Background()
	if e := a.Consume(ctx, "trial-1", "host-launch", stringHash); e != nil {
		t.Fatal(e)
	}
	w.mu.Lock()
	w.epoch.Generation++
	w.mu.Unlock()
	if a.Check(ctx) == nil {
		t.Fatal("stale clone active")
	}
	b, e := Bind(ctx, w, fixtureVerifier{}, "external-authority", "holder", stringHash, []byte("fixture"), a.clock)
	if e != nil {
		t.Fatal(e)
	}
	if b.Consume(ctx, "trial-1", "host-launch", stringHash) == nil {
		t.Fatal("restored trial replayed")
	}
}
func TestWitnessRequired(t *testing.T) {
	if _, e := Bind(context.Background(), nil, nil, "a", "b", stringHash, []byte("x"), nil); e == nil {
		t.Fatal("unqualified witness accepted")
	}
	var a *Activation
	if a.Check(context.Background()) == nil {
		t.Fatal("nil active")
	}
}
func TestConcurrentConsumption(t *testing.T) {
	a, _ := fixtureActivation(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	n := 0
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if a.Consume(context.Background(), "one", "provider-send", stringHash) == nil {
				mu.Lock()
				n++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if n != 1 {
		t.Fatal(n)
	}
}
func TestRevokedDuringConsume(t *testing.T) {
	a, w := fixtureActivation(t)
	w.mu.Lock()
	w.epoch.Holder = "replacement"
	w.mu.Unlock()
	if a.Consume(context.Background(), "id", "provider-send", stringHash) == nil {
		t.Fatal("revoked holder consumed")
	}
}
