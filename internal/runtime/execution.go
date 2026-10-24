package runtime

import (
	"context"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/staging"
	ledger "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/store"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	accounting "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

type executionGuard struct {
	controller   *Controller
	token        records.Token
	account      accounting.AccountingStore
	accountToken accounting.Token
	demand       usage.Demand
	policy       usage.Policy
	hostPolicy   hostbridge.Policy
	validUntil   time.Time
	executionEnd atomic.Pointer[time.Time]
	acceptance   ledger.AcceptanceGuard
}

func (g *executionGuard) Check(ctx context.Context, id string) error {
	if err := g.controller.CheckMethodology(ctx, g.token.RunID, g.token.StepID, "execution"); err != nil {
		return err
	}
	if g.controller.pilotActivation != nil {
		if err := g.controller.pilotActivation.Check(ctx); err != nil {
			return err
		}
		if g.controller.pilotRevalidate == nil {
			return fmt.Errorf("pilot qualification revalidator absent")
		}
		if err := g.controller.pilotRevalidate(ctx); err != nil {
			return err
		}
	}
	if id != g.token.AttemptID || !g.controller.now().Before(g.validUntil) {
		return fmt.Errorf("attempt admission expired or identity mismatch")
	}
	if err := g.controller.Store.CheckWithAcceptance(ctx, g.token, g.controller.now(), g.acceptance); err != nil {
		return err
	}
	if err := hostbridge.VerifyControls(g.hostPolicy, g.controller.now()); err != nil {
		return err
	}
	state, err := g.account.ReadSnapshot(ctx)
	if err != nil {
		return err
	}
	found := false
	// Reuse the projector with our own reservation excluded, as ConfirmLaunch does.
	projected := state.Export
	projected.Reservations = append([]usage.Reservation{}, state.Export.Reservations...)
	for i, r := range projected.Reservations {
		if r.ID != g.accountToken.ID {
			continue
		}
		expiry, e := time.Parse(time.RFC3339Nano, r.LeaseUntil)
		if e != nil || !g.controller.now().Before(expiry) || r.Owner != g.accountToken.Owner || r.AuthorityEpoch != g.accountToken.Epoch || r.Fence != g.accountToken.Fence || r.ProfileSHA256 != g.demand.ProfileSHA256 || r.DemandSHA256 != usage.Digest(g.demand) || (r.State != "held" && r.State != "in-flight") {
			return fmt.Errorf("accounting lease/fence no longer permits attempt")
		}
		found = true
		projected.Reservations[i].State = "released"
	}
	if !found {
		return fmt.Errorf("reservation missing")
	}
	cap := usage.Capacity(projected, g.policy, g.demand.ConfigurationID, &g.demand, g.controller.now())
	if cap.Status != "valid" || cap.Disposition != "observed-fit" {
		return fmt.Errorf("current capacity no longer observed-fit")
	}
	if end := g.executionEnd.Load(); end != nil {
		for _, balance := range cap.Balances {
			if balance.ResetAt != nil {
				reset, e := time.Parse(time.RFC3339Nano, *balance.ResetAt)
				if e != nil || !end.Before(reset) {
					return fmt.Errorf("qualified execution would cross quota reset; hold retained")
				}
			}
		}
	}
	return nil
}
func (c *Controller) execute(ctx context.Context, run records.Run, spec records.StepSpec, step records.Step, token records.Token, prepared hostbridge.Prepared, p hostbridge.Policy, account *accounting.Store, up usage.Policy, validUntil time.Time) (records.Report, error) {
	report := NewReport()
	c.routeGates(&report)
	acceptance := c.acceptanceGuard
	dependencyIDs, _, bindErr := validatePlanBinding(filepath.Join(c.runRoot(run.Manifest.ID), "inputs"), run.Manifest, spec)
	if bindErr != nil {
		return report, bindErr
	}
	acceptance.Dependencies = dependencyIDs
	guard := &executionGuard{controller: c, token: token, account: account, accountToken: *step.AccountingToken, demand: *step.Demand, policy: up, hostPolicy: p, validUntil: validUntil, acceptance: acceptance}
	attempt := token.AttemptID
	observationRoot := filepath.Join(c.runRoot(token.RunID), "attempts", attempt, "observation")
	writer, writeErr := evidence.Reserve(observationRoot, step.Workspace)
	if writeErr != nil {
		return report, writeErr
	}
	child, cancel := context.WithTimeout(ctx, validUntil.Sub(c.now()))
	defer cancel()
	var mu sync.Mutex
	launchConfirmed := false
	started := false
	stopped := false
	// Only the guarded transport receives this runner; version/auth discovery is
	// separate and does not confirm a model launch.
	runner := c.Runner
	if c.pilotActivation != nil {
		if _, ok := runner.(hostbridge.NativeRunner); !ok {
			return report, fmt.Errorf("pilot runner changed before launch")
		}
		runner = hostbridge.NativeRunner{}
	}
	observer := &observedRunner{inner: runner, onStop: func() { mu.Lock(); stopped = true; mu.Unlock() }}
	options := hostbridge.AttemptOptions{AttemptID: attempt, Guard: guard, BeforeStart: func(ctx context.Context, id string) error {
		// Discovery can take time. Recheck the full remaining host bound at the
		// actual launch boundary; subsequent guards retain this fixed stop horizon.
		end := c.now().Add(time.Duration(p.TimeoutSeconds) * time.Second)
		guard.executionEnd.Store(&end)
		if err := guard.Check(ctx, id); err != nil {
			return err
		}
		if c.trialConsume != nil {
			if err := c.trialConsume(ctx); err != nil {
				return err
			}
		}
		if c.pilotActivation != nil {
			b, ok := c.pilotTrials[token.RunID+"\x00"+token.StepID]
			if !ok || b.ManifestSHA256 != run.ManifestSHA256 || b.HostPolicySHA256 != hostbridge.PolicyHash(p) || b.ConfigurationSHA256 != prepared.ConfigurationID || b.ProfileSHA256 != prepared.ProfileSHA256 {
				return fmt.Errorf("pilot trial binding changed")
			}
			if err := c.pilotActivation.Consume(ctx, b.TrialID, "host-launch", run.ManifestSHA256); err != nil {
				return err
			}
		}
		step.LaunchPossible = true
		updated, err := c.Store.Update(ctx, "launch-intent-"+attempt, token, "launch-intent", step, c.now(), guard.acceptance)
		if err != nil {
			return err
		}
		step = updated
		if _, err = account.ConfirmLaunch(ctx, accounting.Transition{Key: "confirm-" + attempt, Token: *step.AccountingToken, ProfileSHA256: prepared.ProfileSHA256}, up, c.now()); err != nil {
			return err
		}
		mu.Lock()
		launchConfirmed = true
		mu.Unlock()
		return guard.Check(ctx, id)
	}, BeforeLaunch: func(ctx context.Context, id string, launch hostbridge.Launch) error {
		if check := v2LaunchAdmission(ctx); check != nil {
			return check(ctx, run, spec, step, prepared, p, launch)
		}
		return nil
	}, Started: func(ctx context.Context, id string) error {
		mu.Lock()
		started = true
		mu.Unlock()
		updated, err := c.Store.Update(ctx, "running-"+attempt, token, "running", step, c.now())
		if err == nil {
			step = updated
		}
		return err
	}}
	done := make(chan struct{})
	monitorDone := make(chan struct{})
	go func() {
		defer close(monitorDone)
		ticker := time.NewTicker(time.Duration(c.Policy.HeartbeatSeconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-child.Done():
				return
			case <-ticker.C:
				if err := guard.Check(child, attempt); err != nil {
					cancel()
					return
				}
				// Runtime renewal and accounting renewal are independently journaled. A
				// partial renewal cannot authorize publication under an expired peer lease.
				at := c.now()
				if err := c.Store.Renew(child, newID("renew-"), token, at); err != nil {
					cancel()
					return
				}
				state, err := account.ReadSnapshot(child)
				if err != nil {
					cancel()
					return
				}
				for _, r := range state.Export.Reservations {
					if r.ID != guard.accountToken.ID {
						continue
					}
					old, _ := time.Parse(time.RFC3339Nano, r.LeaseUntil)
					next := at.Add(time.Duration(c.Policy.LeaseSeconds) * time.Second)
					limit, _ := time.Parse(time.RFC3339Nano, r.DemandValidUntil)
					if validUntil.Before(limit) {
						limit = validUntil
					}
					if limit.Before(next) {
						next = limit
					}
					if next.After(old) {
						if _, err = account.Renew(child, accounting.Transition{Key: newID("account-renew-"), Token: guard.accountToken, LeaseUntil: next.Format(time.RFC3339Nano)}, up, at); err != nil {
							cancel()
							return
						}
					}
				}
			}
		}
	}()
	bundle, runErr := hostbridge.RunControlled(child, prepared, p, observer, nil, options)
	close(done)
	<-monitorDone
	mu.Lock()
	didConfirm, didStart, didStop := launchConfirmed, started, stopped
	mu.Unlock()
	// Preserve quarantined evidence before any authoritative result transition.
	if bundle.Files == nil {
		bundle.Files = map[string][]byte{"runtime-error.json": Encode(map[string]string{"status": "unknown"})}
	}
	for _, a := range prepared.Sources {
		raw, err := hostbridge.ReadAsset(filepath.Join(c.runRoot(token.RunID), "attempts", attempt, "prepared"), a.Path)
		if err != nil {
			return report, err
		}
		bundle.Files[a.Path] = raw
	}
	if err := writer.Write(bundle.Files); err != nil {
		return report, err
	}
	cp, cpErr := staging.CheckpointFor(context.Background(), step.Workspace, attempt, run.Manifest.InputRevision, run.Manifest.Repository, didStop || !didStart)
	if cpErr == nil {
		step.Checkpoint = &cp
	}
	step.StopObserved = didStop || !didStart
	if bundle.Receipt != nil {
		step.HostReceiptSHA256 = usage.Hash(bundle.Files["receipt.json"])
	}
	// A confirmed intent remains uncertain unless complete attributable usage can
	// be settled. We never settle estimates as actual subscription percentages.
	if didConfirm {
		_, _ = account.MarkUncertain(context.Background(), accounting.Transition{Key: "uncertain-" + attempt, Token: guard.accountToken}, up, c.now())
	} else if !step.LaunchPossible {
		_, _ = account.ReleaseUnlaunched(context.Background(), accounting.Transition{Key: "unlaunched-" + attempt, Token: guard.accountToken}, up, c.now())
	}
	phase := "blocked"
	step.Reason = "host outcome or accounting usage requires reconciliation"
	if runErr == nil && bundle.Receipt != nil && bundle.Receipt.OutputValidated && step.StopObserved && cpErr == nil {
		phase = "awaiting-verification"
		step.Reason = "candidate output preserved; P8 acceptance pending; missing attributable quota actuals retain uncertain hold"
	}
	// The admission guard requires an active accounting lease; after uncertainty,
	// use runtime CAS directly. Cancelled/stale owners cannot promote the result.
	if err := c.Store.Check(context.Background(), token, c.now()); err != nil {
		return blockedReport(report, "output quarantined: ownership/cancellation changed; reconciliation required"), nil
	}
	if _, err := c.Store.Update(context.Background(), "result-"+attempt, token, phase, step, c.now()); err != nil {
		return blockedReport(report, "output preserved; result promotion conflicted"), err
	}
	report.Status = phase
	report.Reasons = append(report.Reasons, step.Reason)
	report.Admission = &records.Admission{Version: Version, AttemptID: attempt, DecisionSHA256: step.DecisionSHA256, ProfileSHA256: prepared.ProfileSHA256, ConfigurationID: prepared.ConfigurationID, Status: phase, Reasons: []string{step.Reason}, Accepted: false}
	return report, nil
}

type observedRunner struct {
	inner  hostbridge.Runner
	onStop func()
}

func (r *observedRunner) Start(ctx context.Context, l hostbridge.Launch) (hostbridge.Process, error) {
	p, err := r.inner.Start(ctx, l)
	if err != nil {
		return nil, err
	}
	if len(l.Args) > 0 && (l.Args[0] == "--version" || l.Args[0] == "auth") {
		return p, nil
	}
	return &observedProcess{Process: p, onStop: r.onStop}, nil
}

type observedProcess struct {
	hostbridge.Process
	onStop func()
	once   sync.Once
}

func (p *observedProcess) Close() error {
	err := p.Process.Close()
	if err == nil {
		p.once.Do(p.onStop)
	}
	return err
}
