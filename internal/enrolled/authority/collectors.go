package authority

import (
	"context"
	codexcollector "github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/codex"
	githubcollector "github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/github"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"time"
)

// CollectorAuthority uses the same protected enrollment as runtime admission.
// Source and native identity are approved independently; no caller label maps
// itself to the authenticated account. Capture ordinals are authority-owned.
type CollectorAuthority struct{ Remote *Remote }

func (a CollectorAuthority) Check(ctx context.Context, s codexcollector.Scope) error {
	if a.Remote == nil || s.AccountRef != a.Remote.Client.Scope.Account || s.Ordinal == 0 || s.Revision != int64(s.Ordinal) || s.LeaseID == "" || !time.Now().Before(s.ValidUntil) || s.CaptureProofRef == "" || !validHash(s.ProofSHA256) {
		return ErrBlocked
	}
	// The proof reference/digest is for the authority response, while native
	// identity measurements bind the protected HOME and environment census.
	if _, e := a.Remote.check(ctx, map[string]string{"source": usage.Digest(s.SourceID), "codex-binary": s.ExecutableSHA256, "codex-version": usage.Digest(s.Version), "native-account-proof": s.NativeAccountProofSHA256, "native-account-reference": usage.Digest(s.NativeAccountProofRef), "codex-home": s.HomeSHA256, "codex-environment": s.EnvironmentCensusSHA256}); e != nil {
		return e
	}
	_, e := a.Remote.Client.Call(ctx, Request{Action: "capture-check", LogicalID: s.LeaseID, Generation: uint64(s.Revision), Bindings: map[string]string{"source": usage.Digest(s.SourceID)}})
	return e
}
func (a CollectorAuthority) Acquire(ctx context.Context, account, source string) (codexcollector.CaptureGrant, error) {
	if a.Remote == nil || account != a.Remote.Client.Scope.Account || a.Remote.Bindings["source"] != usage.Digest(source) {
		return codexcollector.CaptureGrant{}, ErrBlocked
	}
	if _, e := a.Remote.check(ctx, map[string]string{"source": usage.Digest(source)}); e != nil {
		return codexcollector.CaptureGrant{}, e
	}
	out, e := a.Remote.Client.Call(ctx, Request{Action: "capture", Bindings: map[string]string{"source": usage.Digest(source)}})
	if e != nil {
		return codexcollector.CaptureGrant{}, e
	}
	until, err := time.Parse(time.RFC3339Nano, out.ValidUntil)
	if err != nil || out.Ordinal < 1 || out.LeaseID == "" {
		return codexcollector.CaptureGrant{}, ErrBlocked
	}
	if t := time.Now().Add(5 * time.Second); t.Before(until) {
		until = t
	}
	return codexcollector.CaptureGrant{Ordinal: uint64(out.Ordinal), Revision: out.Ordinal, LeaseID: out.LeaseID, ValidUntil: until, AccountRef: account, SourceID: source, ProofRef: a.Remote.Client.ServerID + ":" + out.LeaseID, ProofSHA256: usage.Digest(out)}, nil
}
func (a CollectorAuthority) Complete(ctx context.Context, g codexcollector.CaptureGrant, result codexcollector.CaptureResult) error {
	if a.Remote == nil || g.AccountRef != a.Remote.Client.Scope.Account || a.Remote.Bindings["source"] != usage.Digest(g.SourceID) || g.LeaseID == "" || g.Ordinal == 0 || g.Revision != int64(g.Ordinal) || (result.Status != "complete" && result.Status != "uncertain") || result.Bytes < 0 || result.Frames < 0 || !validHash(result.SHA256) || (result.Status == "complete" && (result.Bytes == 0 || result.Frames == 0)) {
		return ErrBlocked
	}
	_, e := a.Remote.Client.Call(ctx, Request{Action: "capture-complete", LogicalID: g.LeaseID, Generation: g.Ordinal, InputSHA256: usage.Digest(result), Bindings: map[string]string{"source": usage.Digest(g.SourceID)}})
	return e
}

// GitHubAuthority validates the whole route/operation request, not only token
// possession. Read and comment-send authorizations are separate binding names.
type GitHubAuthority struct{ Remote *Remote }

func (a GitHubAuthority) CheckRead(ctx context.Context, s githubcollector.Authorization) error {
	if a.Remote == nil {
		return ErrBlocked
	}
	_, e := a.Remote.check(ctx, map[string]string{"github-read:" + usage.Digest(s): usage.Digest(s)})
	return e
}
func (a GitHubAuthority) CheckWrite(ctx context.Context, s githubcollector.Authorization) error {
	if a.Remote == nil {
		return ErrBlocked
	}
	_, e := a.Remote.check(ctx, map[string]string{"github-write:" + usage.Digest(s): usage.Digest(s)})
	return e
}
