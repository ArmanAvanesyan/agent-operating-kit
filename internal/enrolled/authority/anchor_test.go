package authority

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"
)

type fixtureAnchor struct {
	mu   sync.Mutex
	tip  string
	lose bool
}

func (a *fixtureAnchor) Current(context.Context, string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tip, nil
}
func (a *fixtureAnchor) Advance(_ context.Context, _ string, prior, next string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.tip != prior {
		return ErrBlocked
	}
	a.tip = next
	if a.lose {
		return ErrBlocked
	}
	return nil
}
func TestProductionAuthorityRequiresAnchorAndRejectsRestoredDatabase(t *testing.T) {
	f := fixture(t)
	f.cfg.AllowUnanchoredForTests = false
	if s, e := Open(f.cfg); e == nil {
		s.Close()
		t.Fatal("production accepted missing external anchor")
	}
	a := &fixtureAnchor{tip: hashOf("genesis")}
	f.cfg.Anchor = a
	f.cfg.InitialAnchorSHA256 = a.tip
	s, e := Open(f.cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	prior, e := os.ReadFile(f.cfg.Database)
	if e != nil {
		t.Fatal(e)
	}
	s, e = Open(f.cfg)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.process(context.Background(), Request{Version: Version, Nonce: "budget1", EnrollmentID: f.en.ID, Action: "budget", ScopeSHA256: usage.Digest(f.en.Scope), LogicalID: f.en.Scope.TrialID}, f.en.Principal)
	if e != nil {
		t.Fatal(e)
	}
	s.Close()
	if e = os.WriteFile(f.cfg.Database, prior, 0600); e != nil {
		t.Fatal(e)
	}
	if s, e = Open(f.cfg); e == nil {
		s.Close()
		t.Fatal("restored authority database accepted after external append")
	}
}
func TestLostAnchorResponseNeverResendsOrRefunds(t *testing.T) {
	f := fixture(t)
	a := &fixtureAnchor{tip: hashOf("genesis")}
	f.cfg.Anchor = a
	f.cfg.InitialAnchorSHA256 = a.tip
	f.cfg.AllowUnanchoredForTests = false
	s, e := Open(f.cfg)
	if e != nil {
		t.Fatal(e)
	}
	a.lose = true
	req := Request{Version: Version, Nonce: "lost1", EnrollmentID: f.en.ID, Action: "budget", ScopeSHA256: usage.Digest(f.en.Scope), LogicalID: f.en.Scope.TrialID}
	if _, e = s.process(context.Background(), req, f.en.Principal); !errors.Is(e, ErrBlocked) {
		t.Fatal("lost append response accepted")
	}
	a.lose = false
	req.Nonce = "lost2"
	if _, e = s.process(context.Background(), req, f.en.Principal); !errors.Is(e, ErrBlocked) {
		t.Fatal("divergent authority retried after lost append")
	}
	s.Close()
	if s, e = Open(f.cfg); e == nil {
		s.Close()
		t.Fatal("reopened divergent state")
	}
}

type anchorTransport func(*http.Request) (*http.Response, error)

func (f anchorTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAnchorClientBindsSignedNonceScopeAndTip(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	mode := "valid"
	calls := 0
	next := hashOf("next")
	a := AnchorClient{Endpoint: "https://anchor.invalid/v1/anchor", StreamID: "fixture", PublicKey: pub, Token: func(context.Context) (string, error) { return "fixture-anchor-secret-0123456789", nil }}
	a.Transport = anchorTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		var q AnchorRequest
		if e := usage.Decode(raw, &q); e != nil {
			t.Fatal(e)
		}
		now := time.Now().UTC()
		out := AnchorResponse{Version: AnchorVersion, StreamID: q.StreamID, Nonce: q.Nonce, RequestSHA256: byteHash(raw), Tip: next, ObservedAt: now.Format(time.RFC3339Nano), ValidUntil: now.Add(time.Second).Format(time.RFC3339Nano)}
		if mode == "nonce" {
			out.Nonce = "wrong"
		}
		if mode == "scope" {
			out.StreamID = "wrong"
		}
		if mode == "expired" {
			out.ValidUntil = now.Add(-time.Second).Format(time.RFC3339Nano)
		}
		out.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, anchorPayload(out)))
		b, _ := json.Marshal(out)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(b)), Header: make(http.Header)}, nil
	})
	if e := a.Advance(context.Background(), "fixture", hashOf("prior"), next); e != nil {
		t.Fatal(e)
	}
	for _, m := range []string{"nonce", "scope", "expired"} {
		mode = m
		if _, e := a.Current(context.Background(), "fixture"); !errors.Is(e, ErrBlocked) {
			t.Fatalf("accepted %s", m)
		}
	}
	before := calls
	if _, e := a.Current(context.Background(), "other"); !errors.Is(e, ErrBlocked) || calls != before {
		t.Fatal("wrong stream sent request")
	}
}
