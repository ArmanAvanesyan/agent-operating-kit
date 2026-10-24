package campaign

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/selection"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"testing"
	"time"
)

func TestStaticDAGDeterministicAndUnresolved(t *testing.T) {
	a := Node{ID: "a", Requirements: []string{"explicit"}}
	b := Node{ID: "b", Requirements: []string{"explicit"}, DependsOn: []string{"a"}}
	one, e := Order([]Node{b, a})
	if e != nil {
		t.Fatal(e)
	}
	two, e := Order([]Node{a, b})
	if e != nil || usage.Digest(one) != usage.Digest(two) || one[0] != "a" {
		t.Fatal(one, two, e)
	}
	a.DependsOn = []string{"b"}
	if _, e = Order([]Node{a, b}); e == nil {
		t.Fatal("cycle")
	}
	a.DependsOn = []string{"unknown"}
	if _, e = Order([]Node{a, b}); e == nil {
		t.Fatal("unknown edge")
	}
	a.Requirements = nil
	if _, e = Order([]Node{a}); e == nil {
		t.Fatal("invented requirement")
	}
}
func TestSoftComparisonCannotChangeHardGates(t *testing.T) {
	p := selection.SelectionPolicy{Version: selection.Version, Priority: "quality", StandardFloor: json.Number("0.80"), HighRiskFloor: json.Number("0.95")}
	c := p
	c.Priority = "cost"
	if e := SoftChange(p, c, []string{"quality", "cost"}); e != nil {
		t.Fatal(e)
	}
	c.StandardFloor = "0.79"
	if SoftChange(p, c, []string{"cost"}) == nil {
		t.Fatal("floor changed")
	}
	c = p
	c.Pin = "different"
	if SoftChange(p, c, []string{"quality"}) == nil {
		t.Fatal("unapproved soft pin")
	}
}
func TestLockedAuditAndTamperedProposalBlocked(t *testing.T) {
	p := Proposal{Version: Version, Spec: Spec{Version: Version, Nodes: []Node{{ID: "a", Requirements: []string{"explicit"}}}}, Order: []string{"a"}, Activation: "pending-qualified-fixed-dag-route", BasePolicyBytes: json.RawMessage(`{}`)}
	p.Spec.BasePolicy.SHA256 = usage.Hash(p.BasePolicyBytes)
	p.Digest = usage.Digest(p)
	if e := Validate(p); e == nil {
		t.Fatal("missing plan evidence accepted")
	}
	if _, e := Compare(p, "", "", "locked-test", time.Now()); e == nil {
		t.Fatal("audit tuning allowed")
	}
	p.ExecutionReady = true
	if Validate(p) == nil {
		t.Fatal("authority spoof")
	}
}
func TestCorpusDirectoryEntriesAreBounded(t *testing.T) {
	root := t.TempDir()
	count := 2000
	var total int64
	if e := boundedCorpus(root, &total, &count); e == nil {
		t.Fatal("directory entry bypassed bound")
	}
}
