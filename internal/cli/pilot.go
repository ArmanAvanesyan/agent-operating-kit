package cli

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/readiness"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/collectors"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/writeback"
)

const readinessHelp = `aok readiness validate --manifest FILE --policy FILE --as-of RFC3339 [--json]
aok readiness report --manifest FILE --policy FILE --as-of RFC3339 --out NEW [--json]
Readiness is an offline snapshot. Missing native qualification remains blocked.
A report never authorizes execution, publication or tracker changes.`
const pilotHelp = `aok pilot validate --manifest FILE --policy FILE --as-of RFC3339 [--json]
aok pilot gates --as-of RFC3339 --out NEW [--json]
aok pilot experiment validate --bundle FILE --as-of RFC3339 [--json]
aok pilot experiment execute --bundle FILE --once --out NEW [--json]
aok pilot calibration execute --campaign FILE --authority FILE --once --out NEW [--json]
aok pilot replay --scenario FILE --as-of RFC3339 --out NEW [--json]
aok pilot assess --campaign BUNDLE --policy FILE --as-of RFC3339 --out NEW [--json]
aok pilot execute --campaign FILE --authority FILE --once --out NEW [--json]
Offline commands never launch agents or access credentials. Execution requires a
separately installed qualified coordinator; host/provider live gates remain blocked.`

func runReadiness(args []string, out, errout io.Writer) (int, error) {
	return runReadinessServices(args, out, errout, Phase9Services{})
}

// Phase9Services is configured by a protected embedding application, not JSON,
// environment discovery or task content. The distributed binary installs none.
type Phase9Services struct {
	// Context is the protected embedding lifecycle, never task data.
	Context                   context.Context
	ExperimentExecution       *ExperimentExecutionService
	CalibrationExecution      *CalibrationExecutionService
	PilotExecution            *PilotExecutionService
	ReadinessInputs           readiness.Inputs
	ReadinessAdapters         readiness.Adapters
	PilotVerifier             pilot.QualificationVerifier
	VerifierControls          func(context.Context, verification.Candidate, verification.VerificationPolicy, verification.VerifierSpec) (verification.Controls, error)
	CodexQuotaTransport       func(context.Context, usage.Binding, usage.Policy) (collectors.ReadOnlyTransport, error)
	TrackerDispatch           func(context.Context, string) (TrackerDispatchInputs, error)
	TrackerReconciliation     func(context.Context, string) (TrackerReconciliationInputs, error)
	WritebackEvidenceVerifier func(contracts.SyncOperation, writeback.Grant) (writeback.EvidenceVerifier, error)
}

// RunWithPhase9Services integrates native evidence validators without promoting
// catalog declarations or caller-chosen policy pins into live qualification.
func RunWithPhase9Services(argv []string, out, errout io.Writer, services Phase9Services) int {
	if out == nil {
		out = io.Discard
	}
	if errout == nil {
		errout = io.Discard
	}
	if len(argv) == 0 {
		return Run(argv, out, errout)
	}
	var code int
	var err error
	switch argv[0] {
	case "readiness":
		code, err = runReadinessServices(argv[1:], out, errout, services)
	case "pilot":
		code, err = runPilotServices(argv[1:], out, errout, services)
	case "verify":
		code, err = runVerifyServices(argv[1:], out, errout, services)
	case "usage":
		code, err = runUsageServices(argv[1:], out, errout, services)
	case "tracker":
		if len(argv) > 1 && (argv[1] == "dispatch" || argv[1] == "reconcile" || argv[1] == "enqueue") {
			code, err = runTrackerWritebackServices(argv[1:], out, errout, services)
		} else {
			return Run(argv, out, errout)
		}
	default:
		return Run(argv, out, errout)
	}
	if err != nil {
		fmt.Fprintln(errout, "Error:", err)
		return 1
	}
	return code
}

func runReadinessServices(args []string, out, errout io.Writer, services Phase9Services) (int, error) {
	if handled, code, e := readinessV2(args, out, errout, nil, nil); handled {
		return code, e
	}
	if len(args) == 0 {
		fmt.Fprintln(errout, readinessHelp)
		return 2, nil
	}
	if args[0] == "help" || args[0] == "--help" || len(args) > 1 && args[1] == "--help" {
		fmt.Fprintln(out, readinessHelp)
		return 0, nil
	}
	op := args[0]
	if op != "validate" && op != "report" {
		return 2, nil
	}
	v, _, err := selectionFlags(args[1:], []string{"manifest", "policy", "as-of", "out"})
	if err != nil {
		fmt.Fprintln(errout, err)
		return 2, nil
	}
	if v["--manifest"] == "" || v["--policy"] == "" || v["--as-of"] == "" || op == "report" && v["--out"] == "" || op == "validate" && v["--out"] != "" {
		fmt.Fprintln(errout, readinessHelp)
		return 2, nil
	}
	at, err := time.Parse(time.RFC3339Nano, v["--as-of"])
	if err != nil {
		return 2, nil
	}
	inputs := services.ReadinessInputs
	if inputs.EvidenceRoot == "" {
		inputs.EvidenceRoot = filepath.Dir(v["--policy"])
	}
	r, err := readiness.AssessPaths(v["--manifest"], v["--policy"], inputs, services.ReadinessAdapters, at)
	if err != nil {
		return 1, err
	}
	if op == "report" {
		if err = evidence.WriteJSON(v["--out"], "readiness.json", r, v["--manifest"], v["--policy"]); err != nil {
			return 1, err
		}
	}
	_, err = phase8Print(out, r)
	if r.Status != "ready-offline" {
		return 1, err
	}
	return 0, err
}
func runPilot(args []string, out, errout io.Writer) (int, error) {
	return runPilotServices(args, out, errout, Phase9Services{})
}
func runPilotServices(args []string, out, errout io.Writer, services Phase9Services) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(errout, pilotHelp)
		return 2, nil
	}
	if args[0] == "help" || args[0] == "--help" || len(args) > 1 && args[1] == "--help" {
		fmt.Fprintln(out, pilotHelp)
		return 0, nil
	}
	if args[0] == "gates" || args[0] == "experiment" || args[0] == "calibration" {
		return runPilotCompletion(args, out, errout, services)
	}
	op := args[0]
	allowed := map[string][]string{"validate": {"manifest", "policy", "as-of"}, "replay": {"scenario", "as-of", "out"}, "assess": {"campaign", "policy", "as-of", "out"}, "execute": {"campaign", "authority", "out"}}
	keys, ok := allowed[op]
	if !ok {
		return 2, nil
	}
	filtered := []string{}
	once := false
	for _, arg := range args[1:] {
		if arg == "--once" {
			if op != "execute" || once {
				return 2, nil
			}
			once = true
		} else {
			filtered = append(filtered, arg)
		}
	}
	v, _, err := selectionFlags(filtered, keys)
	if err != nil {
		fmt.Fprintln(errout, err)
		return 2, nil
	}
	for _, k := range keys {
		if v["--"+k] == "" {
			fmt.Fprintln(errout, "missing --"+k)
			return 2, nil
		}
	}
	if op == "execute" {
		if !once {
			return 2, nil
		}
		if services.PilotExecution != nil {
			return phase9PilotExecute(v["--campaign"], v["--authority"], v["--out"], services.PilotExecution, out)
		}
		// No arbitrary local command or JSON qualification can install a production
		// isolation/coordinator implementation. Do not read credentials or create intent.
		_, err = phase8Print(out, map[string]any{"status": "blocked", "reason": "qualified purpose-specific coordinator, host controls, capacity, verifier, provider and external activation witness are not installed", "authorizationGranted": false, "executionReady": false, "hostLiveGates": "blocked", "providerLiveGates": "pending"})
		return 1, err
	}
	at, err := time.Parse(time.RFC3339Nano, v["--as-of"])
	if err != nil {
		return 2, nil
	}
	var result any
	code := 0
	switch op {
	case "validate":
		m, e := pilot.ReadManifest(v["--manifest"])
		if e != nil {
			return 1, e
		}
		p, e := pilot.ReadPolicy(v["--policy"])
		if e != nil {
			return 1, e
		}
		// Assess also validates policy-to-manifest identity; an empty evidence bundle
		// cannot qualify a route. Validation only confirms structurally bound inputs.
		b := pilot.Bundle{Version: pilot.Version, ManifestSHA256: p.ManifestSHA256}
		r, e := pilot.AssessWithVerifier(m, p, b, at, services.PilotVerifier)
		if e != nil {
			return 1, e
		}
		result = map[string]any{"version": pilot.Version, "status": "valid", "assessmentStatus": r.Status, "authorizationGranted": false, "executionReady": false, "productionEligible": false}
	case "replay":
		s, e := pilot.ReadScenario(v["--scenario"])
		if e != nil {
			return 1, e
		}
		r, e := pilot.Replay(s)
		if e != nil {
			return 1, e
		}
		result = map[string]any{"asOf": at.UTC().Format(time.RFC3339Nano), "mode": "recorded-state-offline", "result": r, "authorizationGranted": false, "executionReady": false}
	case "assess":
		m, e := pilot.ReadManifest(filepath.Join(v["--campaign"], "manifest.json"))
		if e != nil {
			return 1, e
		}
		p, e := pilot.ReadPolicy(v["--policy"])
		if e != nil {
			return 1, e
		}
		b, e := pilot.ReadBundle(filepath.Join(v["--campaign"], "bundle.json"))
		if e != nil {
			return 1, e
		}
		r, e := pilot.AssessWithVerifier(m, p, b, at, services.PilotVerifier)
		if e != nil {
			return 1, e
		}
		result = r
		if r.Status != "eligible-for-review" {
			code = 1
		}
	}
	if op != "validate" {
		if err = evidence.WriteJSON(v["--out"], op+".json", result, v["--campaign"], v["--policy"], v["--scenario"]); err != nil {
			return 1, err
		}
	}
	_, err = phase8Print(out, result)
	return code, err
}

const operationsHelp = `aok operations snapshot --database FILE --store-id ID --out NEW [--json]
aok operations inspect-snapshot BUNDLE [--json]
Snapshots are single-store SQLite captures, quarantined for inspection only.
They do not activate restored runtime/accounting/outbox authorities.`

func runOperations(args []string, out, errout io.Writer) (int, error) {
	if len(args) == 0 {
		fmt.Fprintln(errout, operationsHelp)
		return 2, nil
	}
	if args[0] == "--help" || args[0] == "help" || len(args) > 1 && args[1] == "--help" {
		fmt.Fprintln(out, operationsHelp)
		return 0, nil
	}
	if args[0] == "inspect-snapshot" {
		if len(args) < 2 {
			return 2, nil
		}
		if _, _, e := selectionFlags(args[2:], nil); e != nil {
			return 2, e
		}
		r, e := operations.VerifySnapshot(args[1])
		if e != nil {
			return 1, e
		}
		return phase8Print(out, r)
	}
	if args[0] != "snapshot" {
		return 2, nil
	}
	v, _, e := selectionFlags(args[1:], []string{"database", "store-id", "out"})
	if e != nil {
		return 2, nil
	}
	for _, k := range []string{"database", "store-id", "out"} {
		if v["--"+k] == "" {
			return 2, nil
		}
	}
	r, e := operations.SnapshotSQLite(context.Background(), v["--database"], v["--store-id"], v["--out"])
	if e != nil {
		return 1, e
	}
	return phase8Print(out, r)
}
