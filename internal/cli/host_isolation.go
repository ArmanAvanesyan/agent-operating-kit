package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/toolisolation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

func hostIsolationCheck(args []string, out io.Writer) (int, error) {
	values, machine, err := selectionFlags(args, []string{"policy", "as-of", "out"})
	if err != nil {
		return 2, err
	}
	for _, key := range []string{"--policy", "--as-of", "--out"} {
		if values[key] == "" {
			return 2, fmt.Errorf("isolation-check requires --policy, --as-of and --out")
		}
	}
	at, err := time.Parse(time.RFC3339Nano, values["--as-of"])
	if err != nil {
		return 2, fmt.Errorf("--as-of must be RFC3339")
	}
	raw, err := hostbridge.ReadFile(values["--policy"])
	if err != nil {
		return 1, fmt.Errorf("tool policy unavailable")
	}
	var policy toolisolation.Policy
	if err = usage.Decode(raw, &policy); err != nil {
		return 1, fmt.Errorf("invalid tool policy")
	}
	report := toolisolation.Inspect(policy, at)
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return 1, err
	}
	if err = evidence.WriteFiles(values["--out"], map[string][]byte{"tool-isolation-report.json": append(b, '\n')}, values["--policy"]); err != nil {
		return 1, err
	}
	if machine {
		fmt.Fprintln(out, string(b))
	} else {
		fmt.Fprintln(out, "Tool isolation: blocked; separate worker implemented, native Codex tool delegation unsupported; no execution authority.")
	}
	return 1, nil
}
