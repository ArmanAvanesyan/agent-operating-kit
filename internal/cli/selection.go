package cli

import (
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation/replay"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/selection"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"io"
	"path/filepath"
	"strings"
	"time"
)

func selectionFlags(args []string, allowed []string) (map[string]string, bool, error) {
	values := map[string]string{}
	machine := false
	for i := 0; i < len(args); i++ {
		s := args[i]
		if s == "--json" {
			if machine {
				return nil, false, fmt.Errorf("duplicate --json")
			}
			machine = true
			continue
		}
		found := false
		for _, k := range allowed {
			if s == "--"+k {
				found = true
			}
		}
		if !found || values[s] != "" || i+1 == len(args) || strings.HasPrefix(args[i+1], "--") {
			return nil, false, fmt.Errorf("invalid or duplicate argument %s", s)
		}
		i++
		values[s] = args[i]
	}
	return values, machine, nil
}
func runSelectionCommand(args []string, out, errout io.Writer) (int, error) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		fmt.Fprintln(out, "aok models select --task FILE --candidates FILE --catalog DIR --catalog-policy FILE --usage FILE --usage-policy FILE --policy FILE --evidence-policy FILE --as-of RFC3339 --out NEW-DIRECTORY [--json]\nOffline advisory only; no execution authorization.")
		return 0, nil
	}
	keys := []string{"task", "candidates", "catalog", "catalog-policy", "usage", "usage-policy", "policy", "evidence-policy", "as-of", "out"}
	v, m, e := selectionFlags(args, keys)
	if e != nil {
		fmt.Fprintln(errout, e)
		return 2, nil
	}
	for _, k := range keys {
		if v["--"+k] == "" {
			fmt.Fprintln(errout, "missing --"+k)
			return 2, nil
		}
	}
	at, e := time.Parse(time.RFC3339Nano, v["--as-of"])
	if e != nil {
		return 2, e
	}
	d, e := selection.Select(selection.Request{TaskPath: v["--task"], CandidatesPath: v["--candidates"], CatalogPath: v["--catalog"], CatalogPolicyPath: v["--catalog-policy"], UsagePath: v["--usage"], UsagePolicyPath: v["--usage-policy"], PolicyPath: v["--policy"], EvidencePolicyPath: v["--evidence-policy"], AsOf: at})
	if e != nil {
		return 1, e
	}
	b, e := json.MarshalIndent(d, "", "  ")
	if e != nil {
		return 1, e
	}
	protected := []string{}
	for _, k := range keys {
		if k != "out" && k != "as-of" {
			protected = append(protected, v["--"+k])
		}
	}
	if e = evidence.WriteFiles(v["--out"], map[string][]byte{"decision.json": append(b, '\n')}, protected...); e != nil {
		return 1, e
	}
	if m {
		fmt.Fprintln(out, string(b))
	} else {
		fmt.Fprintf(out, "%s: %s\nEvidence: %s\n", d.Disposition, d.ConfigurationID, v["--out"])
	}
	if d.Disposition != "selected" {
		return 1, nil
	}
	return 0, nil
}
func runEvaluationCommand(args []string, out, errout io.Writer) (int, error) {
	help := "aok evaluation validate --manifest FILE --evidence-policy FILE\naok evaluation estimate --manifest FILE --evidence-policy FILE --cohort FILE --as-of RFC3339 --out NEW-DIRECTORY\naok evaluation replay|compare --dataset FILE | --selection-manifest FILE [--partition locked-test] [--out NEW-DIRECTORY] [--json]\nOnly signed offline evidence and supplied recorded/synthetic traces. No live evaluation run."
	if len(args) == 0 {
		fmt.Fprintln(errout, help)
		return 2, nil
	}
	if args[0] == "--help" || args[0] == "help" || (len(args) > 1 && (args[1] == "--help" || args[1] == "-h")) {
		fmt.Fprintln(out, help)
		return 0, nil
	}
	op := args[0]
	allowed := map[string][]string{"validate": {"manifest", "evidence-policy"}, "estimate": {"manifest", "evidence-policy", "cohort", "as-of", "out"}, "replay": {"dataset", "selection-manifest", "partition", "out"}, "compare": {"dataset", "selection-manifest", "partition", "out"}}
	keys, ok := allowed[op]
	if !ok {
		fmt.Fprintln(errout, help)
		return 2, nil
	}
	v, _, e := selectionFlags(args[1:], keys)
	if e != nil {
		return 2, e
	}
	var result any
	if op == "validate" || op == "estimate" {
		if v["--manifest"] == "" || v["--evidence-policy"] == "" {
			return 2, fmt.Errorf("manifest and evidence-policy required")
		}
		verified, e := evaluation.Load([]string{v["--manifest"]}, v["--evidence-policy"])
		if e != nil {
			return 1, e
		}
		if op == "validate" {
			result = map[string]any{"version": evaluation.Version, "status": "valid", "authorizationGranted": false, "executionReady": false}
		} else {
			if v["--cohort"] == "" || v["--as-of"] == "" || v["--out"] == "" {
				return 2, fmt.Errorf("cohort/as-of/out required")
			}
			b, e := modelcatalog.ReadFile(v["--cohort"])
			if e != nil {
				return 1, e
			}
			var cohort evaluation.Cohort
			if e = usage.Decode(b, &cohort); e != nil {
				return 1, e
			}
			at, e := time.Parse(time.RFC3339Nano, v["--as-of"])
			if e != nil {
				return 2, e
			}
			result, e = evaluation.Estimate(verified, cohort, at)
			if e != nil {
				return 1, e
			}
		}
	} else {
		if v["--selection-manifest"] != "" {
			if v["--dataset"] != "" {
				return 2, fmt.Errorf("choose dataset or selection-manifest")
			}
			result, e = replay.ReplaySelections(v["--selection-manifest"], v["--partition"])
			if e != nil {
				return 1, e
			}
		} else {
			if v["--dataset"] == "" {
				return 2, fmt.Errorf("dataset required")
			}
			dataset, e := replay.Load(v["--dataset"])
			if e != nil {
				return 1, e
			}
			cfg := replay.Config{Partition: v["--partition"]}
			if op == "replay" {
				result, e = replay.Replay(dataset, cfg)
			} else {
				result, e = replay.Compare(dataset, cfg)
			}
			if e != nil {
				return 1, e
			}
		}
	}
	b, e := json.MarshalIndent(result, "", "  ")
	if e != nil {
		return 1, e
	}
	if v["--out"] != "" {
		roots := []string{}
		for k, p := range v {
			if k != "--out" && k != "--as-of" && k != "--partition" {
				roots = append(roots, filepath.Clean(p))
			}
		}
		if e = evidence.WriteFiles(v["--out"], map[string][]byte{op + ".json": append(b, '\n')}, roots...); e != nil {
			return 1, e
		}
	}
	fmt.Fprintln(out, string(b))
	return 0, nil
}

func runSelection(args []string, out, errout io.Writer) (int, error) {
	code, err := runSelectionCommand(args, out, errout)
	if code == 2 && err != nil {
		fmt.Fprintln(errout, err)
		return 2, nil
	}
	return code, err
}
func runEvaluation(args []string, out, errout io.Writer) (int, error) {
	code, err := runEvaluationCommand(args, out, errout)
	if code == 2 && err != nil {
		fmt.Fprintln(errout, err)
		return 2, nil
	}
	return code, err
}
