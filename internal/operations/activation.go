// Package operations contains rollback-aware activation boundaries. It is not an
// authority service: production witnesses live outside the restored state domain.
package operations

import (
	"context"
	"errors"
	"regexp"
	"time"
)

const Version = "aok-operations/v1"

var hash = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Epoch is an external single-writer activation lease. Consumed logical IDs must
// survive replacement epochs in the witness, not just in a restored local DB.
type Epoch struct {
	Authority   string    `json:"authority"`
	Holder      string    `json:"holder"`
	ScopeSHA256 string    `json:"scopeSha256"`
	Generation  uint64    `json:"generation"`
	ObservedAt  time.Time `json:"observedAt"`
	ValidUntil  time.Time `json:"validUntil"`
}

type Consumption struct {
	Epoch       Epoch  `json:"epoch"`
	LogicalID   string `json:"logicalId"`
	Boundary    string `json:"boundary"`
	InputSHA256 string `json:"inputSha256"`
}

// AuthorityWitness must atomically check current epoch and remember consumption
// across epochs. A restored/copied file is not a qualified implementation.
type AuthorityWitness interface {
	Current(context.Context, string) (Epoch, error)
	Consume(context.Context, Consumption) error
}

// WitnessVerifier is installed from protected operator configuration. It must
// verify rollback resistance, independent custody and exact witness identity.
// No implementation is installed by default and no JSON boolean substitutes.
type WitnessVerifier interface {
	VerifyWitness(context.Context, AuthorityWitness, string, string, []byte, time.Time) error
}

type Activation struct {
	witness  AuthorityWitness
	verifier WitnessVerifier
	proof    []byte
	epoch    Epoch
	clock    func() time.Time
}

func Bind(ctx context.Context, w AuthorityWitness, v WitnessVerifier, authority, holder, scope string, proof []byte, clock func() time.Time) (*Activation, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if w == nil || v == nil || authority == "" || holder == "" || !hash.MatchString(scope) || len(proof) == 0 || len(proof) > 1<<20 {
		return nil, errors.New("qualified external authority witness required; restore remains inspection-only")
	}
	if clock == nil {
		clock = time.Now
	}
	now := clock().UTC()
	if err := v.VerifyWitness(ctx, w, authority, scope, proof, now); err != nil {
		return nil, err
	}
	e, err := w.Current(ctx, authority)
	if err != nil {
		return nil, err
	}
	if err = validEpoch(e, authority, holder, scope, now); err != nil {
		return nil, err
	}
	return &Activation{witness: w, verifier: v, proof: append([]byte(nil), proof...), epoch: e, clock: clock}, nil
}
func validEpoch(e Epoch, authority, holder, scope string, now time.Time) error {
	if e.Authority != authority || e.Holder != holder || e.ScopeSHA256 != scope || e.Generation == 0 || now.IsZero() || e.ObservedAt.IsZero() || now.Before(e.ObservedAt) || !now.Before(e.ValidUntil) || !e.ValidUntil.After(e.ObservedAt) || e.ValidUntil.Sub(e.ObservedAt) > 5*time.Minute {
		return errors.New("external activation epoch is stale or mismatched")
	}
	return nil
}
func (a *Activation) Check(ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if a == nil || a.witness == nil || a.verifier == nil {
		return errors.New("activation unavailable: inspection-only")
	}
	now := a.clock().UTC()
	if err := a.verifier.VerifyWitness(ctx, a.witness, a.epoch.Authority, a.epoch.ScopeSHA256, a.proof, now); err != nil {
		return err
	}
	e, err := a.witness.Current(ctx, a.epoch.Authority)
	if err != nil {
		return err
	}
	if err = validEpoch(e, a.epoch.Authority, a.epoch.Holder, a.epoch.ScopeSHA256, now); err != nil {
		return err
	}
	if e.Generation != a.epoch.Generation {
		return errors.New("activation superseded")
	}
	return nil
}

// Consume must precede any potentially effective launch/send. Failure after
// consumption never refunds an ID: the effect is uncertain until reconciled.
func (a *Activation) Consume(ctx context.Context, id, boundary, inputSHA256 string) error {
	if id == "" || len(id) > 512 || (boundary != "host-launch" && boundary != "provider-send") || !hash.MatchString(inputSHA256) {
		return errors.New("invalid logical operation binding")
	}
	if err := a.Check(ctx); err != nil {
		return err
	}
	return a.witness.Consume(ctx, Consumption{Epoch: a.epoch, LogicalID: id, Boundary: boundary, InputSHA256: inputSHA256})
}
func (a *Activation) ScopeSHA256() string {
	if a == nil {
		return ""
	}
	return a.epoch.ScopeSHA256
}
