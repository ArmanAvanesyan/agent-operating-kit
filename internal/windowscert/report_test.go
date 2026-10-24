package windowscert

import (
	"strings"
	"testing"
)

func fixture() Report {
	r := Report{Version: Version, Producer: "aok-windows-preservation-probe/v1", ScriptSHA256: strings.Repeat("a", 64), ObservedAt: "2026-10-24T00:00:00Z", Scope: Scope{OS: "Windows 11", Architecture: "amd64", Filesystem: "NTFS", DriveType: "Fixed", Volume: "C:\\"}}
	for _, id := range RequiredFeatures {
		r.Features = append(r.Features, Feature{ID: id, Status: "pass", Detail: "probe completed"})
	}
	return r
}
func TestEvaluateCompleteReportIsOnlyFixturePass(t *testing.T) {
	r := Evaluate(fixture())
	if r.Status != "fixture-passed" {
		t.Fatalf("%+v", r)
	}
	if !strings.Contains(r.Limit, "not certified") {
		t.Fatalf("unsafe limit: %s", r.Limit)
	}
}
func TestEvaluateRequiresEveryFeature(t *testing.T) {
	x := fixture()
	x.Features = x.Features[:len(x.Features)-1]
	r := Evaluate(x)
	if r.Status != "pending" || len(r.Missing) != 1 {
		t.Fatalf("%+v", r)
	}
}
func TestEvaluateKeepsUnrunDeliveryWorkflowsPending(t *testing.T) {
	x := fixture()
	x.Features[5].Status = "pending"
	x.Features[8].Status = "pending"
	x.Features[9].Status = "pending"
	r := Evaluate(x)
	if r.Status != "pending" || len(r.Missing) != 3 {
		t.Fatalf("%+v", r)
	}
}
func TestEvaluateRejectsWrongScopeAndProvenance(t *testing.T) {
	x := fixture()
	x.Scope.Filesystem = "APFS"
	if r := Evaluate(x); r.Status != "out-of-scope" {
		t.Fatalf("%+v", r)
	}
	x = fixture()
	x.ScriptSHA256 = "self-asserted"
	if r := Evaluate(x); r.Status != "invalid" {
		t.Fatalf("%+v", r)
	}
}
func TestEvaluateRejectsFailureAndDuplicate(t *testing.T) {
	x := fixture()
	x.Features[0].Status = "fail"
	if r := Evaluate(x); r.Status != "failed" {
		t.Fatalf("%+v", r)
	}
	x = fixture()
	x.Features = append(x.Features, x.Features[0])
	if r := Evaluate(x); r.Status != "invalid" {
		t.Fatalf("%+v", r)
	}
}
