package authority

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/codex"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness"
	readinessv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness/v2"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

func adapterFixture(t *testing.T, extra ...map[string]string) (*authorityFixture, *Remote, *Store) {
	t.Helper()
	f := fixture(t)
	source := "codex-source-fixture"
	f.en.Bindings = map[string]string{
		"source": usage.Digest(source), "trial-scope": hashOf("trial-binding"), "grant-id": usage.Digest("grant-fixture"),
		"codex-binary": hashOf("codex-binary"), "codex-version": usage.Digest("0.162.1"), "native-account-proof": hashOf("native-account-proof"),
		"native-account-reference": usage.Digest("account-proof-reference"), "codex-home": hashOf("codex-home"), "codex-environment": hashOf("environment-census"),
		"prepared": hashOf("prepared"), "host-policy": hashOf("host-policy"), "launch": hashOf("launch"),
		"witness-proof": hashOf("witness-proof"), "guard:quality": hashOf("guard"), "readiness-route": hashOf("readiness-route"), "readiness-policy": hashOf("readiness-policy"), "readiness-proof": hashOf("readiness-proof"),
		"readiness-gate:quality": hashOf("readiness-quality"),
	}
	for _, more := range extra {
		for k, v := range more {
			f.en.Bindings[k] = v
		}
	}
	f.en = SignEnrollment(f.en, f.operator)
	f.cfg.Enrollments = []Enrollment{f.en}
	s := f.open()
	t.Cleanup(func() { _ = s.Close() })
	server := httptest.NewTLSServer(s.Handler())
	t.Cleanup(server.Close)
	client := f.client(t, server)
	remote := &Remote{Client: client, Bindings: f.en.Bindings, Epoch: f.en.Activation}
	return f, remote, s
}

func TestRemoteTrialAndLaunchChecksRejectCrossScopeAndMismatch(t *testing.T) {
	f, r, _ := adapterFixture(t)
	other := f.en.Scope
	other.Account = "different-account"
	if err := r.CheckTrial(context.Background(), other, time.Now()); !errors.Is(err, ErrBlocked) {
		t.Fatalf("cross-scope trial accepted: %v", err)
	}
	prepared := hostbridge.Prepared{Digest: hashOf("other-prepared")}
	policy := hostbridge.Policy{}
	launch := hostbridge.Launch{Executable: "/tmp/fixture"}
	if err := r.VerifyTrialLaunch(context.Background(), f.en.Scope, prepared, policy, launch); !errors.Is(err, ErrBlocked) {
		t.Fatalf("launch mismatch accepted: %v", err)
	}
}

type foreignWitness struct{}

func (*foreignWitness) Current(context.Context, string) (operations.Epoch, error) {
	return operations.Epoch{}, nil
}
func (*foreignWitness) Consume(context.Context, operations.Consumption) error { return nil }
func (*foreignWitness) VerifyWitness(context.Context, operations.AuthorityWitness, string, string, []byte, time.Time) error {
	return nil
}

func TestRemoteWitnessGuardAndReadinessRejectForeignOrFixtureEvidence(t *testing.T) {
	route := readinessv2.Route{Version: readinessv2.Version}
	policy := readinessv2.Policy{Version: readinessv2.Version}
	now := time.Now().UTC()
	q := readiness.GateRequirement{ID: "quality", EvidenceOrigin: "live", Bindings: []readiness.EvidenceBinding{{SHA256: hashOf("fixture-quality"), Origin: "fixture", ObservedAt: now.Format(time.RFC3339Nano), ValidUntil: now.Add(time.Minute).Format(time.RFC3339Nano)}}}
	_, r, _ := adapterFixture(t, map[string]string{"readiness-route": usage.Digest(route), "readiness-policy": usage.Digest(policy), "readiness-gate:quality": usage.Digest(q)})
	if err := r.VerifyWitness(context.Background(), &foreignWitness{}, r.Epoch.Authority, r.Epoch.ScopeSHA256, []byte("proof"), time.Now()); !errors.Is(err, ErrBlocked) {
		t.Fatalf("foreign witness implementation accepted: %v", err)
	}
	guard := Guard{Remote: r}
	if err := guard.Verify(pilot.GuardReceipt{Gate: "quality", ScopeSHA256: hashOf("other-scope")}, hashOf("different"), time.Now()); !errors.Is(err, ErrBlocked) {
		t.Fatalf("cross-scope guard accepted: %v", err)
	}
	out := r.ValidateRouteGate(route, policy, q, time.Now())
	if out.Status != "blocked" {
		t.Fatalf("fixture quality evidence laundered into live readiness: %+v", out)
	}
}

func TestCollectorAuthorityBindsAccountSourceAndActiveLease(t *testing.T) {
	f, r, s := adapterFixture(t)
	a := CollectorAuthority{Remote: r}
	ctx := context.Background()
	if _, err := a.Acquire(ctx, "wrong-account", "codex-source-fixture"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("wrong native account accepted: %v", err)
	}
	grant, err := a.Acquire(ctx, f.en.Scope.Account, "codex-source-fixture")
	if err != nil {
		t.Fatal(err)
	}
	base := codex.Scope{ExecutableSHA256: r.Bindings["codex-binary"], Version: "0.162.1", AccountRef: f.en.Scope.Account, SourceID: "codex-source-fixture", Ordinal: grant.Ordinal, CaptureProofRef: grant.ProofRef, LeaseID: grant.LeaseID, Revision: grant.Revision, ValidUntil: grant.ValidUntil, ProofSHA256: grant.ProofSHA256, NativeAccountProofRef: "account-proof-reference", NativeAccountProofSHA256: r.Bindings["native-account-proof"], HomeSHA256: r.Bindings["codex-home"], EnvironmentCensusSHA256: r.Bindings["codex-environment"]}
	if err := a.Check(ctx, base); err != nil {
		t.Fatalf("active authority lease rejected: %v", err)
	}
	wrong := base
	wrong.SourceID = "another-source"
	if err := a.Check(ctx, wrong); !errors.Is(err, ErrBlocked) {
		t.Fatalf("source mismatch accepted: %v", err)
	}
	wrong = base
	wrong.LeaseID = "forged-lease"
	if err := a.Check(ctx, wrong); !errors.Is(err, ErrBlocked) {
		t.Fatalf("forged active lease accepted: %v", err)
	}
	if err := a.Complete(ctx, grant, codex.CaptureResult{Status: "complete", Bytes: 0, Frames: 0, SHA256: hashOf("empty")}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("empty completion accepted: %v", err)
	}
	result := codex.CaptureResult{Status: "complete", Bytes: 128, Frames: 4, SHA256: hashOf("captured-payload")}
	if err := a.Complete(ctx, grant, result); err != nil {
		t.Fatal(err)
	}
	var persisted string
	if err := s.db.QueryRow(`SELECT result_sha256 FROM capture_results WHERE ordinal=?`, grant.Ordinal).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if persisted != usage.Digest(result) {
		t.Fatalf("completion digest not persisted: %q", persisted)
	}
	if err := a.Check(ctx, base); !errors.Is(err, ErrBlocked) {
		t.Fatalf("completed lease remained active: %v", err)
	}
	// Missing result hashes cannot release a lease through the protocol either.
	if _, err := r.Client.Call(ctx, Request{Action: "capture-complete", LogicalID: grant.LeaseID, Generation: grant.Ordinal, Bindings: map[string]string{"source": usage.Digest(grant.SourceID)}}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("empty completion hash accepted: %v", err)
	}
}

func TestCollectorAuthorityRejectsWrongRevisionAndAccountProof(t *testing.T) {
	f, r, _ := adapterFixture(t)
	a := CollectorAuthority{Remote: r}
	grant, err := a.Acquire(context.Background(), f.en.Scope.Account, "codex-source-fixture")
	if err != nil {
		t.Fatal(err)
	}
	scope := codex.Scope{ExecutableSHA256: r.Bindings["codex-binary"], Version: "0.162.1", AccountRef: f.en.Scope.Account, SourceID: "codex-source-fixture", Ordinal: grant.Ordinal, CaptureProofRef: grant.ProofRef, LeaseID: grant.LeaseID, Revision: grant.Revision + 1, ValidUntil: grant.ValidUntil, ProofSHA256: grant.ProofSHA256, NativeAccountProofRef: "account-proof-reference", NativeAccountProofSHA256: r.Bindings["native-account-proof"], HomeSHA256: r.Bindings["codex-home"], EnvironmentCensusSHA256: r.Bindings["codex-environment"]}
	if err := a.Check(context.Background(), scope); !errors.Is(err, ErrBlocked) {
		t.Fatalf("mismatched capture revision accepted: %v", err)
	}
	scope.Revision = grant.Revision
	scope.NativeAccountProofSHA256 = hashOf("wrong-account-proof")
	if err := a.Check(context.Background(), scope); !errors.Is(err, ErrBlocked) {
		t.Fatalf("wrong native account proof accepted: %v", err)
	}
}

func TestRemoteReadinessRecomputesExactCodexReport(t *testing.T) {
	driver, ok := hostdriver.Lookup("codex")
	if !ok {
		t.Fatal("Codex host driver is not registered")
	}
	h := hashOf("codex-route-fixture")
	route := readinessv2.Route{
		Version:      readinessv2.Version,
		Legacy:       readiness.RouteManifest{Version: readiness.Version, ID: "codex-readiness-fixture", Host: "codex", HostVersion: "0.162.1", HostSHA256: h, Transport: driver.Protocol, ConfigurationSHA256: h, ProfileSHA256: h, ControlPolicySHA256: h, Account: "acct-fixture", AuthMode: "native-login", Pool: "fixture", OS: "darwin", Arch: "arm64", VerifierSHA256: h, RubricSHA256: h, EnvironmentSHA256: h},
		DriverSHA256: driver.Digest, Protocol: driver.Protocol, PreparedSHA256: h, PolicySHA256: h, LaunchSHA256: h, InputSHA256: h,
	}
	at := time.Now().UTC().Truncate(time.Millisecond)
	policy := readinessv2.Policy{Version: readinessv2.Version, ID: "protected-codex-policy", RouteSHA256: usage.Digest(route), MaxEvidenceAgeSeconds: 300}
	gateBindings := map[string]string{}
	for _, id := range readinessv2.UniversalGateIDs(route) {
		evidence := hashOf("evidence-" + id)
		q := readiness.GateRequirement{ID: id, Validator: "remote", EvidenceOrigin: "live", Applicability: "applicable", Required: true,
			Scope:    map[string]string{"routeSha256": policy.RouteSHA256},
			Bindings: []readiness.EvidenceBinding{{SHA256: evidence, Origin: "live", ObservedAt: at.Format(time.RFC3339Nano), ValidUntil: at.Add(2 * time.Minute).Format(time.RFC3339Nano), Scope: map[string]string{"routeSha256": policy.RouteSHA256}}},
		}
		policy.Requirements = append(policy.Requirements, q)
		gateBindings["readiness-gate:"+id] = usage.Digest(q)
	}
	proof := json.RawMessage(`{"kind":"protected-readiness-fixture","nonce":"unique-proof"}`)
	extra := map[string]string{"readiness-route": usage.Digest(route), "readiness-policy": usage.Digest(policy), "readiness-proof": byteHash(proof)}
	for k, v := range gateBindings {
		extra[k] = v
	}
	_, remote, _ := adapterFixture(t, extra)
	remote.ReadinessPolicy = &policy
	validators := map[string]readinessv2.Validator{}
	for _, q := range policy.Requirements {
		validators[q.Validator] = remote
	}
	assessed, err := readinessv2.Assess(route, policy, at, remote, validators)
	if err != nil || assessed.Report.Status != "ready-offline" {
		t.Fatalf("could not create valid protected report: status=%s err=%v", assessed.Report.Status, err)
	}
	report := assessed.Report
	if err := remote.VerifyReadinessRoute(route, report, proof); err != nil {
		t.Fatalf("recomputed valid report rejected: %v", err)
	}
	shorter := report
	shorter.EarliestValidUntil = at.Add(30 * time.Second).Format(time.RFC3339Nano)
	if err := remote.VerifyReadinessRoute(route, shorter, proof); err != nil {
		t.Fatalf("report with shorter validity boundary rejected: %v", err)
	}
	for name, mutate := range map[string]func(*readiness.Report){
		"gate-hash":    func(q *readiness.Report) { q.Gates[0].EvidenceSHA256[0] = hashOf("forged-gate-evidence") },
		"as-of":        func(q *readiness.Report) { q.AsOf = at.Add(-time.Second).Format(time.RFC3339Nano) },
		"later-expiry": func(q *readiness.Report) { q.EarliestValidUntil = at.Add(3 * time.Minute).Format(time.RFC3339Nano) },
		"status":       func(q *readiness.Report) { q.Status = "blocked" },
	} {
		t.Run(name, func(t *testing.T) {
			forged := report
			forged.Gates = append([]readiness.GateResult(nil), report.Gates...)
			for i := range forged.Gates {
				forged.Gates[i].EvidenceSHA256 = append([]string(nil), report.Gates[i].EvidenceSHA256...)
			}
			mutate(&forged)
			if err := remote.VerifyReadinessRoute(route, forged, proof); !errors.Is(err, ErrBlocked) {
				t.Fatalf("forged report accepted: %v", err)
			}
		})
	}
	withoutPolicy := *remote
	withoutPolicy.ReadinessPolicy = nil
	if err := withoutPolicy.VerifyReadinessRoute(route, report, proof); !errors.Is(err, ErrBlocked) {
		t.Fatalf("missing protected policy accepted: %v", err)
	}
}
