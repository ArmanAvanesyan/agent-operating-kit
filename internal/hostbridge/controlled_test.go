package hostbridge

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type controlledGuard struct {
	calls    int
	failAt   int
	attempts []string
	order    *[]string
}

func (g *controlledGuard) Check(_ context.Context, attempt string) error {
	g.calls++
	g.attempts = append(g.attempts, attempt)
	*g.order = append(*g.order, "guard")
	if g.calls == g.failAt {
		return errors.New("stale attempt")
	}
	return nil
}

type controlledProcess struct{ stopped, closed bool }

func (*controlledProcess) Send([]byte) error                       { return nil }
func (*controlledProcess) Receive(context.Context) ([]byte, error) { return nil, errors.New("unused") }
func (p *controlledProcess) Stop() error                           { p.stopped = true; return nil }
func (p *controlledProcess) Close() error                          { p.closed = true; return nil }
func (*controlledProcess) Wait() error                             { return nil }

type controlledInner struct {
	order   *[]string
	starts  int
	process *controlledProcess
}

func (r *controlledInner) Start(context.Context, Launch) (Process, error) {
	r.starts++
	*r.order = append(*r.order, "inner-start")
	if r.process == nil {
		r.process = &controlledProcess{}
	}
	return r.process, nil
}

func validControlledRunner(order *[]string, guard *controlledGuard, inner *controlledInner, id string, hookErr, startedErr error) controlledRunner {
	return controlledRunner{inner: inner, options: AttemptOptions{AttemptID: id, Guard: guard,
		BeforeStart: func(_ context.Context, got string) error {
			*order = append(*order, "hook")
			if got != id {
				return errors.New("wrong attempt id")
			}
			return hookErr
		},
		Started: func(_ context.Context, got string) error {
			*order = append(*order, "started")
			if got != id {
				return errors.New("wrong started attempt id")
			}
			return startedErr
		},
	}}
}

func TestControlledRunnerChecksGuardHookThenStartsAndObserves(t *testing.T) {
	var order []string
	guard := &controlledGuard{order: &order}
	inner := &controlledInner{order: &order}
	runner := validControlledRunner(&order, guard, inner, "attempt-caller", nil, nil)
	p, e := runner.Start(context.Background(), Launch{})
	if e != nil || p == nil {
		t.Fatalf("Start: %v", e)
	}
	want := []string{"guard", "hook", "guard", "inner-start", "started"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("ordering %v, want %v", order, want)
	}
	if !reflect.DeepEqual(guard.attempts, []string{"attempt-caller", "attempt-caller"}) {
		t.Fatalf("guard received wrong caller attempt id: %v", guard.attempts)
	}
}

func TestRunControlledRequiresCallerIdentityGuardAndHook(t *testing.T) {
	for name, options := range map[string]AttemptOptions{
		"id":    {Guard: &controlledGuard{}, BeforeStart: func(context.Context, string) error { return nil }},
		"guard": {AttemptID: "caller-id", BeforeStart: func(context.Context, string) error { return nil }},
		"hook":  {AttemptID: "caller-id", Guard: &controlledGuard{}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := RunControlled(context.Background(), Prepared{}, Policy{}, &fakeClaudeRunner{}, nil, options); e == nil {
				t.Fatal("incomplete controlled attempt accepted")
			}
		})
	}
}

func TestControlledRunnerSecondGuardRejectsBeforeInnerStart(t *testing.T) {
	var order []string
	guard := &controlledGuard{failAt: 2, order: &order}
	inner := &controlledInner{order: &order}
	runner := validControlledRunner(&order, guard, inner, "attempt-guarded", nil, nil)
	if _, e := runner.Start(context.Background(), Launch{}); e == nil {
		t.Fatal("stale second guard passed")
	}
	if inner.starts != 0 || !reflect.DeepEqual(order, []string{"guard", "hook", "guard"}) {
		t.Fatalf("stale attempt reached host: starts=%d order=%v", inner.starts, order)
	}
}

func TestProtectedLaunchAdmissionRunsOnExactLaunchBeforeRunner(t *testing.T) {
	var order []string
	guard := &controlledGuard{order: &order}
	inner := &controlledInner{order: &order}
	launch := Launch{Executable: "/approved/host", Dir: "/workspace", Args: []string{"--json"}}
	called := false
	r := controlledRunner{inner: inner, options: AttemptOptions{AttemptID: "attempt-x", Guard: guard,
		BeforeStart: func(context.Context, string) error { order = append(order, "intent"); return nil },
		BeforeLaunch: func(_ context.Context, id string, got Launch) error {
			called = true
			order = append(order, "qualified")
			if id != "attempt-x" || !reflect.DeepEqual(got, launch) {
				return errors.New("qualification did not receive exact launch")
			}
			return errors.New("revoked proof")
		},
	}}
	if _, err := r.Start(context.Background(), launch); err == nil {
		t.Fatal("revoked proof passed")
	}
	if !called || inner.starts != 0 || !reflect.DeepEqual(order, []string{"guard", "intent", "guard", "qualified"}) {
		t.Fatalf("unsafe launch: called=%v starts=%d order=%v", called, inner.starts, order)
	}
}

func TestControlledRunnerHookErrorAndCancelledContextNeverStart(t *testing.T) {
	t.Run("hook error", func(t *testing.T) {
		var order []string
		guard := &controlledGuard{order: &order}
		inner := &controlledInner{order: &order}
		runner := validControlledRunner(&order, guard, inner, "attempt-hook", errors.New("durable intent failed"), nil)
		if _, e := runner.Start(context.Background(), Launch{}); e == nil {
			t.Fatal("hook error ignored")
		}
		if inner.starts != 0 {
			t.Fatalf("inner start count %d", inner.starts)
		}
	})
	t.Run("cancelled context", func(t *testing.T) {
		var order []string
		guard := &controlledGuard{order: &order}
		inner := &controlledInner{order: &order}
		runner := validControlledRunner(&order, guard, inner, "attempt-cancelled", nil, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, e := runner.Start(ctx, Launch{}); !errors.Is(e, context.Canceled) {
			t.Fatalf("cancelled start error: %v", e)
		}
		if inner.starts != 0 {
			t.Fatalf("cancelled context started host %d times", inner.starts)
		}
	})
}

func TestControlledRunnerStartedFailureStopsAndClosesProcess(t *testing.T) {
	var order []string
	guard := &controlledGuard{order: &order}
	proc := &controlledProcess{}
	inner := &controlledInner{order: &order, process: proc}
	runner := validControlledRunner(&order, guard, inner, "attempt-observe", nil, errors.New("ledger unavailable"))
	if _, e := runner.Start(context.Background(), Launch{}); e == nil {
		t.Fatal("unobserved start accepted")
	}
	if inner.starts != 1 || !proc.stopped || !proc.closed {
		t.Fatalf("process cleanup missing: starts=%d stopped=%t closed=%t", inner.starts, proc.stopped, proc.closed)
	}
}

func TestRunControlledPreflightFailureDoesNotInvokeLaunchHook(t *testing.T) {
	var calls int
	const id = "attempt-preflight"
	result, e := RunControlled(context.Background(), Prepared{}, Policy{}, &fakeClaudeRunner{}, nil, AttemptOptions{AttemptID: id, Guard: &controlledGuard{}, BeforeStart: func(context.Context, string) error { calls++; return nil }})
	if e != nil {
		t.Fatal(e)
	}
	if calls != 0 {
		t.Fatalf("preflight invoked start hook %d times", calls)
	}
	if result.Receipt == nil || result.Receipt.AttemptID != id {
		t.Fatalf("caller attempt id not preserved in preflight result: %+v", result.Receipt)
	}
}
