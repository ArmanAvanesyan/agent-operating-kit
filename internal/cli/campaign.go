package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/campaign"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

func runCampaign(args []string, out, errout io.Writer) (int, error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(out, "aok campaign plan --spec FILE --as-of RFC3339 --out NEW [--json]\naok campaign evaluate --proposal FILE --evidence-manifest FILE --evidence-policy FILE --as-of RFC3339 --out NEW [--json]\naok campaign compare --proposal FILE --manifest FILE --partition development|validation --as-of RFC3339 --out NEW [--json]\nAdvisory only; activation requires a separately qualified fixed-DAG route.")
		return 0, nil
	}
	op := args[0]
	allowed := map[string][]string{"plan": {"spec", "as-of", "out"}, "evaluate": {"proposal", "evidence-manifest", "evidence-policy", "as-of", "out"}, "compare": {"proposal", "manifest", "partition", "as-of", "out"}}
	keys, ok := allowed[op]
	if !ok {
		return 2, fmt.Errorf("unknown campaign operation")
	}
	v, machine, e := selectionFlags(args[1:], keys)
	if e != nil {
		return 2, e
	}
	for _, key := range keys {
		if v["--"+key] == "" {
			return 2, fmt.Errorf("missing --%s", key)
		}
	}
	at, e := time.Parse(time.RFC3339Nano, v["--as-of"])
	if e != nil {
		return 2, e
	}
	var result any
	status := "advisory"
	protected := []string{}
	for k, path := range v {
		if k == "--spec" || k == "--proposal" || k == "--manifest" || k == "--evidence-manifest" || k == "--evidence-policy" {
			protected = append(protected, path)
		}
	}
	var p campaign.Proposal
	if op != "plan" {
		b, e := tracker.ReadFile(v["--proposal"])
		if e != nil {
			return 1, e
		}
		if e = usage.Decode(b, &p); e != nil {
			return 1, e
		}
		if e = campaign.Validate(p); e != nil {
			return 1, e
		}
	}
	switch op {
	case "plan":
		p, e = campaign.Build(v["--spec"], at)
		result = p
		for _, node := range p.Spec.Nodes {
			protected = append(protected, filepath.Join(filepath.Dir(v["--spec"]), node.Corpus))
		}
		protected = append(protected, filepath.Join(filepath.Dir(v["--spec"]), p.Spec.BasePolicy.Path))
		status = p.Status
	case "evaluate":
		result, e = campaign.Evaluate(p, []string{v["--evidence-manifest"]}, v["--evidence-policy"], at)
	case "compare":
		result, e = campaign.Compare(p, filepath.Dir(v["--proposal"]), v["--manifest"], v["--partition"], at)
	}
	if e != nil {
		return 1, e
	}
	if e = evidence.WriteJSON(v["--out"], "report.json", result, protected...); e != nil {
		return 1, e
	}
	if machine {
		b, _ := json.Marshal(result)
		fmt.Fprintln(out, string(b))
	} else {
		fmt.Fprintf(out, "Campaign %s: %s; authorizationGranted=false executionReady=false\n", op, status)
	}
	if status == "abstain-unresolved-requirements" {
		return 1, nil
	}
	return 0, nil
}
