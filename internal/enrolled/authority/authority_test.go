package authority

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	runtimep7 "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

type authorityFixture struct {
	t        *testing.T
	dir      string
	cfg      Config
	en       Enrollment
	operator ed25519.PrivateKey
	response ed25519.PrivateKey
	issuers  map[string]ed25519.PrivateKey
}

func hashOf(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func keyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}
func fixture(t *testing.T) *authorityFixture {
	t.Helper()
	opub, operator := keyPair(t)
	_, response := keyPair(t)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	f := &authorityFixture{t: t, dir: dir, operator: operator, response: response, issuers: map[string]ed25519.PrivateKey{}}
	for _, gate := range []string{"controls", "identity", "capability", "capacity", "verification", "runtime", "authorization", "platform", "provider-read", "provider-operation", "witness", "budget", "stop-monitor", "quality"} {
		pub, priv := keyPair(t)
		f.issuers[gate] = priv
		f.cfg.Issuers = append(f.cfg.Issuers, Issuer{ID: "issuer-" + gate, Role: gate + "-issuer", PublicKey: base64.StdEncoding.EncodeToString(pub)})
	}
	now := time.Now().UTC()
	e := Enrollment{Version: Version, ID: "enrollment-fixture", ParentID: "parent-fixture", Principal: "principal-fixture", Scope: runtimeScope(), Activation: operations.Epoch{Authority: "authority-fixture", Holder: "holder-fixture", ScopeSHA256: hashOf("epoch-scope"), Generation: 1}, Operations: []Operation{{ID: "op-fixture", Boundary: "host-launch", InputSHA256: hashOf("input")}}, Bindings: map[string]string{"source": hashOf("source"), "trial-scope": hashOf("trial")}, MaximumTrials: 1, MaximumConsumption: json.Number("10"), MaximumSpend: json.Number("5"), IssuedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano)}
	e.Scope = runtimeScope()
	e.Proofs = f.proofs(e, now)
	e = SignEnrollment(e, operator)
	f.en = e
	f.cfg = Config{AllowUnanchoredForTests: true, Database: filepath.Join(dir, "authority.db"), ServerID: "authority-fixture", OperatorKey: opub, ResponseKey: response, Issuers: f.cfg.Issuers, Enrollments: []Enrollment{e}, Tokens: map[string]string{e.Principal: "fixture-token-0123456789abcdef"}}
	return f
}
func runtimeScope() runtimep7.TrialScope {
	return runtimep7.TrialScope{Purpose: "calibration-experiment", EnrollmentSHA256: hashOf("enrollment"), GrantSHA256: hashOf("grant"), RouteSHA256: hashOf("route"), TrialID: "trial-fixture", RunID: "run-fixture", InputSHA256: hashOf("scope-input"), Account: "acct-fixture", Repository: "owner/repo", BoundsSHA256: hashOf("bounds"), Currency: "USD", MaximumSpend: json.Number("5"), MaximumSeconds: 30, MaximumConsumption: json.Number("10"), ConsumptionUnit: "requests"}
}
func (f *authorityFixture) proofs(e Enrollment, now time.Time) []Proof {
	var out []Proof
	for gate, key := range f.issuers {
		if gate == "quality" {
			continue
		}
		p := Proof{Version: Version, Gate: gate, ScopeSHA256: usage.Digest(e.Scope), MeasurementSHA256: hashOf("measurement-" + gate), Origin: "live", ObservedAt: now.Add(-time.Second).Format(time.RFC3339Nano), ValidUntil: now.Add(4 * time.Minute).Format(time.RFC3339Nano), Issuer: "issuer-" + gate}
		out = append(out, SignProof(p, key))
	}
	return out
}
func (f *authorityFixture) open() *Store {
	f.t.Helper()
	s, err := Open(f.cfg)
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}
func (f *authorityFixture) client(t *testing.T, srv *httptest.Server) Client {
	t.Helper()
	return Client{Endpoint: srv.URL + "/v1/authority", ServerID: f.cfg.ServerID, EnrollmentID: f.en.ID, Scope: f.en.Scope, PublicKey: f.cfg.ResponseKey.Public().(ed25519.PublicKey), Token: func(context.Context) (string, error) { return f.cfg.Tokens[f.en.Principal], nil }, Transport: srv.Client().Transport}
}
func (f *authorityFixture) server() (*Store, *httptest.Server) {
	f.t.Helper()
	s := f.open()
	srv := httptest.NewTLSServer(s.Handler())
	f.t.Cleanup(func() { srv.Close(); _ = s.Close() })
	return s, srv
}

func TestAuthorityTLSAuthenticationScopeAndDurableBudget(t *testing.T) {
	f := fixture(t)
	s, srv := f.server()
	c := f.client(t, srv)
	ctx := context.Background()
	if _, err := c.Call(ctx, Request{Action: "budget", LogicalID: f.en.Scope.TrialID}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(ctx, Request{Action: "budget", LogicalID: f.en.Scope.TrialID}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("duplicate budget admission: %v", err)
	}
	bad := c
	bad.Token = func(context.Context) (string, error) { return "wrong-token-0123456789012345", nil }
	if _, err := bad.Call(ctx, Request{Action: "current"}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("wrong token accepted: %v", err)
	}
	wrongScope := c
	wrongScope.Scope.Account = "other-account"
	if _, err := wrongScope.Call(ctx, Request{Action: "current"}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("wrong scope accepted: %v", err)
	}
	_ = s.Close()
	// A renewed enrollment may advance the epoch, but the parent budget survives.
	now := time.Now().UTC()
	renewed := f.en
	renewed.ID = "enrollment-renewed"
	renewed.Activation.Generation = 2
	renewed.Activation.ScopeSHA256 = hashOf("epoch-scope-2")
	renewed.IssuedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	renewed.ExpiresAt = now.Add(time.Hour).Format(time.RFC3339Nano)
	renewed.Proofs = f.proofs(renewed, now)
	renewed = SignEnrollment(renewed, f.operator)
	f.cfg.Enrollments = []Enrollment{renewed}
	f.en = renewed
	next, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err := next.process(ctx, Request{Version: Version, Nonce: "renewed-nonce", EnrollmentID: renewed.ID, Action: "budget", ScopeSHA256: usage.Digest(renewed.Scope), LogicalID: renewed.Scope.TrialID}, renewed.Principal); !errors.Is(err, ErrBlocked) {
		t.Fatalf("renewal reset consumed budget: %v", err)
	}
}

func TestAuthorityCaptureLeaseSerializesOrdinalUntilComplete(t *testing.T) {
	f := fixture(t)
	_, srv := f.server()
	c := f.client(t, srv)
	first, err := c.Call(context.Background(), Request{Action: "capture", Bindings: map[string]string{"source": f.en.Bindings["source"]}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Ordinal != 1 || first.LeaseID == "" {
		t.Fatalf("bad capture grant: %+v", first)
	}
	if _, err = c.Call(context.Background(), Request{Action: "capture", Bindings: map[string]string{"source": f.en.Bindings["source"]}}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("concurrent capture was admitted: %v", err)
	}
	if _, err = c.Call(context.Background(), Request{Action: "capture-complete", LogicalID: first.LeaseID, Generation: 1, InputSHA256: hashOf("capture-result"), Bindings: map[string]string{"source": f.en.Bindings["source"]}}); err != nil {
		t.Fatal(err)
	}
	second, err := c.Call(context.Background(), Request{Action: "capture", Bindings: map[string]string{"source": f.en.Bindings["source"]}})
	if err != nil {
		t.Fatal(err)
	}
	if second.Ordinal != 2 {
		t.Fatalf("ordinal did not advance: %+v", second)
	}
}

func TestAuthorityRejectsExactSuccessfulRequestReplay(t *testing.T) {
	f := fixture(t)
	s := f.open()
	defer s.Close()
	ctx := context.Background()
	current := Request{Version: Version, Nonce: "replay-current-once", EnrollmentID: f.en.ID, Action: "current", ScopeSHA256: usage.Digest(f.en.Scope)}
	if _, err := s.process(ctx, current, f.en.Principal); err != nil {
		t.Fatalf("initial current request: %v", err)
	}
	if _, err := s.process(ctx, current, f.en.Principal); !errors.Is(err, ErrBlocked) {
		t.Fatalf("exact successful current request replay accepted: %v", err)
	}
	capture := Request{Version: Version, Nonce: "replay-capture-once", EnrollmentID: f.en.ID, Action: "capture", ScopeSHA256: usage.Digest(f.en.Scope), Bindings: map[string]string{"source": f.en.Bindings["source"]}}
	if grant, err := s.process(ctx, capture, f.en.Principal); err != nil || grant.Ordinal != 1 || grant.LeaseID == "" {
		t.Fatalf("initial capture request: grant=%+v err=%v", grant, err)
	}
	if _, err := s.process(ctx, capture, f.en.Principal); !errors.Is(err, ErrBlocked) {
		t.Fatalf("exact successful capture request replay accepted: %v", err)
	}
}

func TestAuthorityAnchoredStateRejectsStopOrBudgetTampering(t *testing.T) {
	for _, tc := range []struct {
		name   string
		seed   func(*testing.T, *Store, *authorityFixture)
		tamper string
	}{
		{
			name: "deleted-stop",
			seed: func(t *testing.T, s *Store, f *authorityFixture) {
				_, err := s.process(context.Background(), Request{Version: Version, Nonce: "anchor-stop", EnrollmentID: f.en.ID, Action: "stop", ScopeSHA256: usage.Digest(f.en.Scope), InputSHA256: hashOf("operator-stop-reason")}, f.en.Principal)
				if err != nil {
					t.Fatal(err)
				}
			},
			tamper: `DELETE FROM stops`,
		},
		{
			name: "reset-budget",
			seed: func(t *testing.T, s *Store, f *authorityFixture) {
				_, err := s.process(context.Background(), Request{Version: Version, Nonce: "anchor-budget", EnrollmentID: f.en.ID, Action: "budget", ScopeSHA256: usage.Digest(f.en.Scope), LogicalID: f.en.Scope.TrialID}, f.en.Principal)
				if err != nil {
					t.Fatal(err)
				}
			},
			tamper: `UPDATE budget SET trials=0`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fixture(t)
			a := &fixtureAnchor{tip: hashOf("authority-genesis")}
			f.cfg.AllowUnanchoredForTests = false
			f.cfg.Anchor = a
			f.cfg.InitialAnchorSHA256 = a.tip
			s, err := Open(f.cfg)
			if err != nil {
				t.Fatal(err)
			}
			tc.seed(t, s, f)
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", f.cfg.Database)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(tc.tamper); err != nil {
				db.Close()
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			if reopened, err := Open(f.cfg); err == nil {
				reopened.Close()
				t.Fatal("tampered database reopened while external anchor tip was unchanged")
			}
		})
	}
}

func TestAuthorityCheckRequiresExactBindingSet(t *testing.T) {
	f := fixture(t)
	s := f.open()
	defer s.Close()
	base := Request{Version: Version, Nonce: "binding-check", EnrollmentID: f.en.ID, Action: "check", ScopeSHA256: usage.Digest(f.en.Scope)}
	if _, err := s.process(context.Background(), base, f.en.Principal); !errors.Is(err, ErrBlocked) {
		t.Fatalf("omitted binding set accepted: %v", err)
	}
	base.Bindings = map[string]string{}
	for k, v := range f.en.Bindings {
		base.Bindings[k] = v
	}
	base.Nonce = "binding-check-complete"
	if _, err := s.process(context.Background(), base, f.en.Principal); err != nil {
		t.Fatalf("complete enrolled binding set denied: %v", err)
	}
}

func TestAuthorityStopPersistsAcrossRestartAndRenewal(t *testing.T) {
	f := fixture(t)
	s := f.open()
	req := Request{Version: Version, Nonce: "stop-once", EnrollmentID: f.en.ID, Action: "stop", ScopeSHA256: usage.Digest(f.en.Scope), InputSHA256: hashOf("operator-stop-reason")}
	if _, err := s.process(context.Background(), req, f.en.Principal); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	now := time.Now().UTC()
	renewed := f.en
	renewed.ID = "enrollment-after-stop"
	renewed.Activation.Generation = 2
	renewed.Activation.ScopeSHA256 = hashOf("epoch-after-stop")
	renewed.IssuedAt = now.Add(-time.Minute).Format(time.RFC3339Nano)
	renewed.ExpiresAt = now.Add(time.Hour).Format(time.RFC3339Nano)
	renewed.Proofs = f.proofs(renewed, now)
	renewed = SignEnrollment(renewed, f.operator)
	f.cfg.Enrollments = []Enrollment{renewed}
	f.en = renewed
	next, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err := next.process(context.Background(), Request{Version: Version, Nonce: "still-stopped", EnrollmentID: renewed.ID, Action: "current", ScopeSHA256: usage.Digest(renewed.Scope)}, renewed.Principal); !errors.Is(err, ErrBlocked) {
		t.Fatalf("restart or renewal cleared stop: %v", err)
	}
}

func TestAuthorityRejectsNonCanonicalRepositoryScope(t *testing.T) {
	for _, repo := range []string{"owner/repo?state=all", "owner/%72epo", "owner/../repo", "owner/repo#fragment", "owner/repo/extra"} {
		t.Run(repo, func(t *testing.T) {
			f := fixture(t)
			e := f.en
			e.Scope.Repository = repo
			e.Proofs = f.proofs(e, time.Now().UTC())
			e = SignEnrollment(e, f.operator)
			f.cfg.Enrollments = []Enrollment{e}
			f.en = e
			if s, err := Open(f.cfg); err == nil {
				s.Close()
				t.Fatalf("noncanonical repository %q accepted", repo)
			}
		})
	}
}

func TestAuthorityRejectsMissingOrFixtureProofs(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		f := fixture(t)
		e := f.en
		e.Proofs = nil
		e = SignEnrollment(e, f.operator)
		f.cfg.Enrollments = []Enrollment{e}
		f.en = e
		if s, err := Open(f.cfg); err == nil {
			s.Close()
			t.Fatal("missing gate proofs accepted")
		}
	})
	t.Run("fixture-origin", func(t *testing.T) {
		f := fixture(t)
		e := f.en
		e.Proofs[0].Origin = "fixture"
		e.Proofs[0] = SignProof(e.Proofs[0], f.issuers[e.Proofs[0].Gate])
		e = SignEnrollment(e, f.operator)
		f.cfg.Enrollments = []Enrollment{e}
		f.en = e
		if s, err := Open(f.cfg); err == nil {
			s.Close()
			t.Fatal("fixture proof accepted as live")
		}
	})
}

func TestAuthorityConcurrentBudgetNeverOverAdmits(t *testing.T) {
	f := fixture(t)
	_, srv := f.server()
	c := f.client(t, srv)
	const n = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Call(context.Background(), Request{Action: "budget", LogicalID: f.en.Scope.TrialID})
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if successes != 1 {
		t.Fatalf("concurrent budget admitted %d callers, want 1", successes)
	}
}

type replayTransport struct {
	base   http.RoundTripper
	mu     sync.Mutex
	body   []byte
	status int
}

func (r *replayTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.body != nil {
		return &http.Response{StatusCode: r.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(bytes.NewReader(r.body)), Request: req}, nil
	}
	resp, err := r.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	r.body, err = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	r.status = resp.StatusCode
	resp.Body = io.NopCloser(bytes.NewReader(r.body))
	return resp, nil
}

func TestClientRejectsReplayedNonceAndExpiredSignedResponse(t *testing.T) {
	f := fixture(t)
	_, srv := f.server()
	c := f.client(t, srv)
	c.Transport = &replayTransport{base: srv.Client().Transport}
	if _, err := c.Call(context.Background(), Request{Action: "current"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Call(context.Background(), Request{Action: "current"}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("replayed signed response accepted: %v", err)
	}

	stale := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req Request
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &req)
		at := time.Now().Add(-time.Minute).UTC()
		out := Response{Version: Version, ServerID: f.cfg.ServerID, Nonce: req.Nonce, RequestSHA256: usage.Digest(req), ObservedAt: at.Format(time.RFC3339Nano), ValidUntil: at.Add(time.Second).Format(time.RFC3339Nano), Epoch: operations.Epoch{Authority: "a", Holder: "h", ScopeSHA256: hashOf("s"), Generation: 1}}
		out.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(f.response, responsePayload(out)))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	defer stale.Close()
	bad := f.client(t, stale)
	if _, err := bad.Call(context.Background(), Request{Action: "current"}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("expired signed response accepted: %v", err)
	}
}

func TestAuthorityRejectsClockRollbackAndStaleEpoch(t *testing.T) {
	f := fixture(t)
	s := f.open()
	defer s.Close()
	now := time.Now().UTC()
	s.clock = func() time.Time { return now }
	r := Request{Version: Version, Nonce: "clock-one", EnrollmentID: f.en.ID, Action: "current", ScopeSHA256: usage.Digest(f.en.Scope)}
	if _, err := s.process(context.Background(), r, f.en.Principal); err != nil {
		t.Fatal(err)
	}
	s.clock = func() time.Time { return now.Add(-time.Second) }
	r.Nonce = "clock-two"
	if _, err := s.process(context.Background(), r, f.en.Principal); !errors.Is(err, ErrBlocked) {
		t.Fatalf("clock rollback accepted: %v", err)
	}
	// A different signed enrollment may supersede an epoch; the old store then fails closed.
	_ = s.Close()
	renewed := f.en
	renewed.ID = "new-epoch"
	renewed.Activation.Generation = 2
	renewed.Activation.ScopeSHA256 = hashOf("new-scope")
	renewed.IssuedAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339Nano)
	renewed.ExpiresAt = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	renewed.Proofs = f.proofs(renewed, time.Now().UTC())
	renewed = SignEnrollment(renewed, f.operator)
	f.cfg.Enrollments = []Enrollment{renewed}
	next, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if _, err = s.process(context.Background(), Request{Version: Version, Nonce: "stale", EnrollmentID: f.en.ID, Action: "current", ScopeSHA256: usage.Digest(f.en.Scope)}, f.en.Principal); err == nil {
		t.Fatal("stale epoch remained usable")
	}
}
