package operations

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func completionAsOf() time.Time { return time.Date(2026, 10, 24, 12, 30, 0, 0, time.UTC) }

func TestBuildCompletionLedgerDeterministicAndPrimaryBlockersRemainExplicit(t *testing.T) {
	one, err := BuildCompletionLedger(completionAsOf())
	if err != nil {
		t.Fatal(err)
	}
	two, err := BuildCompletionLedger(completionAsOf())
	if err != nil {
		t.Fatal(err)
	}
	b1, _ := MarshalCompletionLedger(one)
	b2, _ := MarshalCompletionLedger(two)
	if !bytes.Equal(b1, b2) || one.DigestSHA256 != two.DigestSHA256 {
		t.Fatal("ledger output is not deterministic")
	}
	if !one.InspectionOnly || one.AuthorizationGranted || one.ExecutionReady {
		t.Fatal("offline ledger granted authority", one)
	}
	for _, id := range []string{"codex-host-qualification", "p3-collector-accounting", "github-issue-comment", "independent-acceptance-handoff", "first-controlled-trial"} {
		g := completionGateByID(t, one, id)
		if !g.RequiredForFirstPilot || g.Status != "blocked" || g.Accepted || g.Live != "blocked" || g.Fixture != "partial" || len(g.Evidence) != 0 {
			t.Fatalf("primary route blocker was laundered: %+v", g)
		}
	}
	optional := completionGateByID(t, one, "jira-cloud")
	if optional.RequiredForFirstPilot || optional.Status != "pending" {
		t.Fatalf("unrelated route blocks first pilot: %+v", optional)
	}
}

func TestCompletionLedgerStrictReadDigestAndSelfAcceptance(t *testing.T) {
	ledger, err := BuildCompletionLedger(completionAsOf())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := MarshalCompletionLedger(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ReadCompletionLedger(raw); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadCompletionLedger(append(raw[:len(raw)-2], []byte(",\"unknown\":true}\n")...)); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err = ReadCompletionLedger(append(raw, raw...)); err == nil {
		t.Fatal("trailing document accepted")
	}
	ledger.Gates[0].Accepted = true
	ledger.DigestSHA256, _ = completionLedgerDigest(ledger)
	if ValidateCompletionLedger(ledger) == nil {
		t.Fatal("self-accepted record passed")
	}
	ledger, _ = BuildCompletionLedger(completionAsOf())
	ledger.Gates[0].Blocker = "tampered"
	if ValidateCompletionLedger(ledger) == nil {
		t.Fatal("tampered digest passed")
	}
}

func TestCompletionLedgerRejectsDependencyCyclesAndBadEvidence(t *testing.T) {
	ledger, err := BuildCompletionLedger(completionAsOf())
	if err != nil {
		t.Fatal(err)
	}
	first := completionGateByID(t, ledger, "scope-enrollment")
	first.Dependencies = []string{"route-applicability"}
	for i := range ledger.Gates {
		if ledger.Gates[i].ID == first.ID {
			ledger.Gates[i] = first
		}
	}
	ledger.DigestSHA256, _ = completionLedgerDigest(ledger)
	if ValidateCompletionLedger(ledger) == nil {
		t.Fatal("dependency cycle accepted")
	}
	ledger, _ = BuildCompletionLedger(completionAsOf())
	ledger.Gates[0].Evidence = []CompletionEvidenceRef{{ID: "evidence", SHA256: "invalid", Source: "fixture", ObservedAt: completionAsOf(), ValidUntil: completionAsOf().Add(time.Minute), RevocationRef: "revocation"}}
	ledger.DigestSHA256, _ = completionLedgerDigest(ledger)
	if ValidateCompletionLedger(ledger) == nil {
		t.Fatal("malformed evidence accepted")
	}
	ledger, _ = BuildCompletionLedger(completionAsOf())
	ledger.AsOf = time.Time{}
	if ValidateCompletionLedger(ledger) == nil {
		t.Fatal("missing as-of accepted")
	}
}

func TestBuildCompletionLedgerRequiresAsOf(t *testing.T) {
	if _, err := BuildCompletionLedger(time.Time{}); err == nil {
		t.Fatal("zero as-of accepted")
	}
}

func completionGateByID(t *testing.T, ledger CompletionLedger, id string) CompletionGate {
	t.Helper()
	for _, gate := range ledger.Gates {
		if gate.ID == id {
			return gate
		}
	}
	t.Fatalf("missing gate %q", id)
	return CompletionGate{}
}

func TestCompletionLedgerInputBounds(t *testing.T) {
	if _, err := ReadCompletionLedger([]byte(strings.Repeat("x", maxCompletionLedgerBytes+1))); err == nil {
		t.Fatal("oversized input accepted")
	}
	ledger, _ := BuildCompletionLedger(completionAsOf())
	raw, _ := json.Marshal(ledger)
	if _, err := ReadCompletionLedger(raw); err != nil {
		t.Fatal(err)
	}
}
