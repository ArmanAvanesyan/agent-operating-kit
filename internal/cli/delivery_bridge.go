package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/deliverybridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

func runDeliveryBridge(args []string, out, errout io.Writer) (int, error) {
	help := `aok delivery handoff --acceptance HANDOFF-MANIFEST --policy FILE --out NEW [--json]
aok delivery validate-handoff BUNDLE --policy FILE --as-of RFC3339 [--json]
Local receipt translation only. Existing publication commands retain their own action authority.`
	op := args[0]
	args = args[1:]
	if len(args) > 0 && args[0] == "--help" {
		fmt.Fprintln(out, help)
		return 0, nil
	}
	path := ""
	keys := []string{"acceptance", "policy", "out"}
	if op == "validate-handoff" {
		if len(args) == 0 {
			return 2, nil
		}
		path = args[0]
		args = args[1:]
		keys = []string{"policy", "as-of"}
	}
	v, _, e := selectionFlags(args, keys)
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
	if op == "handoff" {
		path = v["--acceptance"]
	}
	if st, e := os.Lstat(path); e == nil && st.IsDir() {
		path = filepath.Join(path, "handoff-manifest.json")
	}
	var m deliverybridge.HandoffManifest
	if e = phase8Read(path, &m); e != nil {
		return 1, e
	}
	b, e := modelcatalog.ReadFile(v["--policy"])
	if e != nil {
		return 1, e
	}
	p, e := verification.DecodePolicy(b)
	if e != nil {
		return 1, e
	}
	// Embedded issuer data never sets trust: replace it with the independently
	// supplied operator policy before validating signatures and receipt bindings.
	m.Policy = p
	m.AsOf = time.Now().UTC()
	if op == "validate-handoff" {
		m.AsOf, e = phase8AsOf(v["--as-of"])
		if e != nil {
			fmt.Fprintln(errout, e)
			return 2, nil
		}
	}
	draft, report := deliverybridge.Validate(m)
	if op == "handoff" {
		if e = evidence.WriteFiles(v["--out"], map[string][]byte{"handoff-manifest.json": mustPhase8JSON(m), "context-draft.json": mustPhase8JSON(draft), "report.json": mustPhase8JSON(report)}, path, v["--policy"], m.Candidate.BundlePath); e != nil {
			return 1, e
		}
	}
	_, e = phase8Print(out, report)
	if !report.Valid {
		return 1, e
	}
	return 0, e
}

func mustPhase8JSON(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }
