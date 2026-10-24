package cli

import (
	"encoding/json"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	readinessv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness/v2"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"io"
	"time"
)

func readinessV2(args []string, out, errout io.Writer, authority readinessv2.PolicyAuthority, validators map[string]readinessv2.Validator) (bool, int, error) {
	if len(args) < 1 || args[0] != "validate" && args[0] != "report" {
		return false, 0, nil
	}
	v, _, e := selectionFlags(args[1:], []string{"manifest", "policy", "as-of", "out"})
	if e != nil || v["--manifest"] == "" {
		return false, 0, nil
	}
	raw, e := tracker.ReadFile(v["--manifest"])
	if e != nil {
		return false, 0, nil
	}
	var head struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(raw, &head) != nil || head.Version != readinessv2.Version {
		return false, 0, nil
	}
	if v["--policy"] == "" || v["--as-of"] == "" || args[0] == "report" && v["--out"] == "" || args[0] == "validate" && v["--out"] != "" {
		return true, 2, nil
	}
	at, e := time.Parse(time.RFC3339Nano, v["--as-of"])
	if e != nil {
		return true, 2, e
	}
	var r readinessv2.Route
	var p readinessv2.Policy
	if e = readinessv2.DecodeStrict(raw, &r); e != nil {
		return true, 1, e
	}
	b, e := tracker.ReadFile(v["--policy"])
	if e != nil {
		return true, 1, e
	}
	if e = readinessv2.DecodeStrict(b, &p); e != nil {
		return true, 1, e
	}
	report, e := readinessv2.Assess(r, p, at, authority, validators)
	if e != nil {
		return true, 1, e
	}
	if args[0] == "report" {
		if e = evidence.WriteJSON(v["--out"], "readiness.json", report, v["--manifest"], v["--policy"]); e != nil {
			return true, 1, e
		}
	}
	_, e = phase8Print(out, report)
	if report.Report.Status != "ready-offline" {
		return true, 1, e
	}
	return true, 0, e
}
