package toolisolation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

// Session is the parent-facing boundary. The protected caller supplies the
// immutable policy once; untrusted tool messages contain requests only. Parent
// authentication stays outside this API: no credential/env/header/FD channel is
// accepted or forwarded. This is not a P7 grant, durable claim or native adapter.
// A production caller must hold its existing runtime admission and fences.
type Admission interface {
	CheckToolDispatch(context.Context, string) error
}

type Session struct {
	admission Admission
	digest    string
	policy    Policy
	mu        sync.Mutex
	attempted bool
	reads     int
}

func NewSession(p Policy, admission Admission) (*Session, error) {
	if err := ValidatePolicy(p); err != nil || admission == nil {
		return nil, errors.New("invalid tool policy")
	}
	p.ReadPaths = append([]string(nil), p.ReadPaths...)
	return &Session{policy: p, admission: admission, digest: usage.Digest(p)}, nil
}

// Handle sends at most one create attempt in this session, even if the result
// is ambiguous. There is no automatic resend, fallback or credential switching.
func (s *Session) Handle(ctx context.Context, raw []byte) (Result, error) {
	if ctx == nil || ctx.Err() != nil {
		return Result{}, errors.New("tool dispatch canceled")
	}
	var q Request
	if len(raw) > maxRequestBytes || contracts.ValidateJSON(raw) != nil {
		return Result{}, errors.New("invalid tool request")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&q) != nil || ValidateRequest(q) != nil {
		return Result{}, errors.New("invalid tool request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.admission.CheckToolDispatch(ctx, s.digest); err != nil {
		return Result{}, errors.New("tool dispatch admission denied")
	}
	if q.Operation == "create-target" {
		if s.attempted {
			return Result{}, errors.New("create attempt already consumed")
		}
		s.attempted = true
	} else {
		if s.reads >= 32 {
			return Result{}, errors.New("tool read limit reached")
		}
		s.reads++
	}
	return Run(ctx, s.policy, q)
}
