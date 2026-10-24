package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	localruntime "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
	"io"
	"os"
	"os/signal"
	"time"
)

const runtimeHelp = `aok runtime init --policy FILE --state NEW-DIRECTORY [--json]
aok runtime submit STATE --manifest FILE [--json]
aok runtime validate STATE [--json]
aok runtime dispatch STATE --run ID --once [--json]
aok runtime capture-candidate STATE --manifest FILE --source-root DIRECTORY --out NEW --verification-policy FILE --verification-store FILE [--json]
aok runtime attach-acceptance STATE --receipt FILE --candidate FILE --verification-policy FILE --verification-store FILE [--json]
aok runtime status STATE [--run ID] [--json]
aok runtime explain STATE --run ID [--json]
aok runtime cancel STATE --run ID --reason TEXT [--json]
aok runtime resume STATE --run ID --checkpoint ID [--json]
aok runtime reconcile STATE --run ID --evidence FILE [--json]
aok runtime export STATE --run ID --out NEW-DIRECTORY [--json]
aok runtime replay --scenario FILE --as-of RFC3339 --out NEW-DIRECTORY [--json]
Local foreground execution only. P6 live qualification and P8 acceptance remain separate gates.
Dispatch accepts the opt-in pair --verification-policy FILE --verification-store FILE (fresh signed authority state).`

func runRuntime(args []string, out, errout io.Writer) (int, error) {
	code, err := runtimeCommand(args, out)
	if err != nil {
		fmt.Fprintln(errout, "Error:", err)
		if code == 0 {
			code = 1
		}
	}
	return code, nil
}
func runtimeCommand(args []string, out io.Writer) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(out, runtimeHelp)
		return 2, nil
	}
	if args[0] == "help" || args[0] == "--help" || len(args) > 1 && args[1] == "--help" {
		fmt.Fprintln(out, runtimeHelp)
		return 0, nil
	}
	op := args[0]
	args = args[1:]
	keys, ok := map[string][]string{"init": {"policy", "state"}, "submit": {"manifest"}, "validate": {}, "capture-candidate": {"manifest", "source-root", "out", "verification-policy", "verification-store"}, "attach-acceptance": {"receipt", "candidate", "verification-policy", "verification-store"}, "dispatch": {"run", "verification-policy", "verification-store"}, "status": {"run"}, "explain": {"run"}, "cancel": {"run", "reason"}, "resume": {"run", "checkpoint"}, "reconcile": {"run", "evidence"}, "export": {"run", "out"}, "replay": {"scenario", "as-of", "out"}}[op]
	if !ok {
		return 2, fmt.Errorf("unknown runtime operation")
	}
	state := ""
	if op != "init" && op != "replay" {
		if len(args) == 0 {
			return 2, fmt.Errorf("state required")
		}
		state = args[0]
		args = args[1:]
	}
	once := false
	filtered := []string{}
	for _, a := range args {
		if a == "--once" {
			if once || op != "dispatch" {
				return 2, fmt.Errorf("unexpected or repeated --once")
			}
			once = true
		} else {
			filtered = append(filtered, a)
		}
	}
	values, machine, err := selectionFlags(filtered, keys)
	if err != nil {
		return 2, err
	}
	if op == "dispatch" && !once {
		return 2, fmt.Errorf("bounded dispatch requires --once")
	}
	required := keys
	if op == "status" || op == "validate" {
		required = nil
	}
	for _, k := range required {
		if op == "dispatch" && (k == "verification-policy" || k == "verification-store") {
			continue
		}
		if values["--"+k] == "" {
			return 2, fmt.Errorf("missing --%s", k)
		}
	}
	if (values["--verification-policy"] == "") != (values["--verification-store"] == "") {
		fmt.Fprintln(out, "--verification-policy and --verification-store are required together")
		return 2, nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	var report records.Report
	if op == "replay" {
		at, err := time.Parse(time.RFC3339Nano, values["--as-of"])
		if err != nil {
			return 2, fmt.Errorf("RFC3339 as-of required")
		}
		files, r, err := localruntime.Replay(values["--scenario"], at)
		if err != nil {
			return 1, err
		}
		if err = evidence.WriteFiles(values["--out"], files, values["--scenario"]); err != nil {
			return 1, err
		}
		report = r
	} else {
		var c *localruntime.Controller
		if op == "init" {
			c, err = localruntime.Init(values["--state"], values["--policy"])
		} else {
			c, err = localruntime.Open(state)
		}
		if err != nil {
			return 1, err
		}
		defer c.Close()
		if values["--verification-policy"] != "" {
			b, readErr := modelcatalog.ReadFile(values["--verification-policy"])
			if readErr != nil {
				return 1, readErr
			}
			p, readErr := verification.DecodePolicy(b)
			if readErr != nil {
				return 1, readErr
			}
			var authority records.AcceptanceAuthorityState
			if readErr = phase8Read(values["--verification-store"], &authority); readErr != nil {
				return 1, readErr
			}
			if readErr = c.ConfigureAcceptanceAuthority(ctx, p, authority); readErr != nil {
				return 1, readErr
			}
		}
		switch op {
		case "init":
			report, err = c.Status(ctx, "")
		case "capture-candidate":
			b, readErr := modelcatalog.ReadFile(values["--manifest"])
			if readErr != nil {
				return 1, readErr
			}
			m, readErr := verification.DecodeManifest(b)
			if readErr != nil {
				return 1, readErr
			}
			_, err = c.CaptureCandidate(ctx, m, values["--source-root"], values["--out"])
			if err == nil {
				report, err = c.Status(ctx, m.RunID)
			}
		case "attach-acceptance":
			var receipt verification.AcceptanceReceipt
			var m verification.CandidateManifest
			if err = phase8Read(values["--receipt"], &receipt); err != nil {
				return 1, err
			}
			if err = phase8Read(values["--candidate"], &m); err != nil {
				return 1, err
			}
			err = c.AttachAcceptance(ctx, receipt, m, time.Now().UTC())
			if err == nil {
				report, err = c.Status(ctx, receipt.RunID)
			}
		case "submit":
			report, err = c.Submit(ctx, values["--manifest"])
		case "validate":
			report, err = c.Validate(ctx)
		case "status", "explain":
			report, err = c.Status(ctx, values["--run"])
		case "dispatch":
			report, err = c.Dispatch(ctx, values["--run"])
		case "cancel":
			report, err = c.Cancel(ctx, values["--run"], values["--reason"])
		case "resume":
			report, err = c.Resume(ctx, values["--run"], values["--checkpoint"])
		case "reconcile":
			report, err = c.Reconcile(ctx, values["--run"], values["--evidence"])
		case "export":
			err = c.Export(ctx, values["--run"], values["--out"])
			if err == nil {
				report, err = c.Status(ctx, values["--run"])
			}
		}
		if err != nil {
			return 1, err
		}
	}
	if machine {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		if err = encoder.Encode(report); err != nil {
			return 1, err
		}
	} else {
		fmt.Fprintln(out, "Runtime:", report.Status)
		for _, reason := range report.Reasons {
			fmt.Fprintln(out, "-", reason)
		}
	}
	if op != "status" && op != "explain" && report.Status == "blocked" {
		return 1, nil
	}
	return 0, nil
}
