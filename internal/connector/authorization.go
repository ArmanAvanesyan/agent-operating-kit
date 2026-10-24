package connector

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

type authorizationLease struct {
	expires                    time.Time
	origin, principal, authRef string
	guard                      func() error
	used                       atomic.Bool
}

// authorize verifies a separately approved signed grant. The mandatory guard
// checks the durable outbox fence and protected revocation state at admission.
// It never converts an operation's authority declaration into approval.
func authorize(op contracts.SyncOperation, capability contracts.ConnectorCapability, identity contracts.RemoteIdentity, grant writeback.Grant, v writeback.VerifiedGrant, now time.Time, box *writeback.Outbox, owner string, fence int64) (AuthorizationContext, error) {
	if box == nil {
		return AuthorizationContext{}, errors.New("durable outbox is required")
	}
	if _, ds := ValidateOperation(op, capability, identity); len(ds) != 0 {
		return AuthorizationContext{}, errors.New("operation validation failed")
	}
	if v.ID() == "" || v.Scope() != identity || v.OperationDigest() != op.OperationDigest || !now.Before(v.ExpiresAt()) {
		return AuthorizationContext{}, errors.New("verified grant does not match")
	}
	guard := func() error {
		row, err := box.Get(op.OperationID)
		if err != nil {
			return err
		}
		if row.Digest != v.OperationDigest() || row.GrantID != v.ID() || row.GrantHash != v.GrantHash() || row.PolicyHash != v.PolicyHash() || row.ProviderOrigin != grant.ProviderOrigin || row.Owner != owner || row.Fence != fence || row.State != "dispatch-intent" || row.Attempts != 1 {
			return errors.New("no exact durable send intent")
		}
		return nil
	}
	if err := guard(); err != nil {
		return AuthorizationContext{}, err
	}
	if op.Authority.Reference != v.ID() {
		return AuthorizationContext{}, errors.New("authority does not reference the approved grant")
	}
	observed, err := time.Parse(time.RFC3339Nano, grant.SnapshotObservedAt)
	if err != nil || observed.After(now) {
		return AuthorizationContext{}, errors.New("source observation time is invalid")
	}
	expires := v.ExpiresAt()
	if freshUntil := observed.Add(5 * time.Minute); freshUntil.Before(expires) {
		expires = freshUntil
	}
	if !now.Before(expires) {
		return AuthorizationContext{}, errors.New("source evidence has expired")
	}
	return AuthorizationContext{reference: v.ID(), scope: identity, operation: op.OperationKind, digest: op.OperationDigest, verified: &authorizationLease{expires: expires, origin: grant.ProviderOrigin, principal: v.Principal(), authRef: v.AuthRef(), guard: guard}}, nil
}

func authorizationCurrent(a AuthorizationContext, v ValidatedOperation, now time.Time) bool {
	return a.verified != nil && now.Before(a.verified.expires) && a.reference == v.operation.Authority.Reference && a.digest == v.operation.OperationDigest && a.operation == v.operation.OperationKind && sameIdentity(a.scope, v.identity) && a.verified.guard != nil && a.verified.guard() == nil
}

// AuthorizationValid consumes this process-local transport token once. Durable
// one-send guarantees are enforced by the outbox before the token is supplied.
func AuthorizationValid(a AuthorizationContext, v ValidatedOperation, now time.Time) bool {
	return authorizationCurrent(a, v, now) && a.verified.used.CompareAndSwap(false, true)
}

// AuthorizationScope binds a provider transport to its separately approved
// origin, principal and credential reference; values never contain credentials.
func AuthorizationScope(a AuthorizationContext) (origin, principal, authRef string) {
	if a.verified == nil {
		return "", "", ""
	}
	return a.verified.origin, a.verified.principal, a.verified.authRef
}
