package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/operations"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/pilot/experiment"
	workflow "github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/runtime/records"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

// Service enrollments are installed by an external protected embedding. CLI
// arguments can identify the exact captured enrollment, never its keys/services.
type ExperimentExecutionService struct {
	Controller *workflow.Controller
	Enrollment workflow.ExperimentDispatch
}
type CalibrationExecutionService struct {
	Controller *workflow.Controller
	Enrollment workflow.CalibrationDispatch
}

func runPilotCompletion(args []string, out, errout io.Writer, s Phase9Services) (int, error) {
	misuse := func(e error) (int, error) {
		if e != nil {
			fmt.Fprintln(errout, e)
		}
		return 2, nil
	}
	if args[0] == "gates" {
		v, _, e := selectionFlags(args[1:], []string{"as-of", "out"})
		if e != nil || v["--as-of"] == "" || v["--out"] == "" {
			return misuse(e)
		}
		at, e := time.Parse(time.RFC3339Nano, v["--as-of"])
		if e != nil {
			return misuse(e)
		}
		ledger, e := operations.BuildCompletionLedger(at)
		if e != nil {
			return 1, e
		}
		if e = evidence.WriteJSON(v["--out"], "completion-gates.json", ledger); e != nil {
			return 1, e
		}
		_, e = phase8Print(out, ledger)
		return 1, e // live gates intentionally blocked
	}
	if len(args) < 2 {
		return 2, nil
	}
	mode, op := args[0], args[1]
	filtered := []string{}
	once := false
	for _, arg := range args[2:] {
		if arg == "--once" {
			if once || op != "execute" {
				return 2, nil
			}
			once = true
		} else {
			filtered = append(filtered, arg)
		}
	}
	var keys []string
	if mode == "experiment" && op == "validate" {
		keys = []string{"bundle", "as-of"}
	} else if mode == "experiment" && op == "execute" {
		keys = []string{"bundle", "out"}
	} else if mode == "calibration" && op == "execute" {
		keys = []string{"campaign", "authority", "out"}
	} else {
		return 2, nil
	}
	v, _, e := selectionFlags(filtered, keys)
	if e != nil {
		return misuse(e)
	}
	for _, k := range keys {
		if v["--"+k] == "" {
			return 2, nil
		}
	}
	if op == "validate" {
		at, e := time.Parse(time.RFC3339Nano, v["--as-of"])
		if e != nil {
			return misuse(e)
		}
		b, e := experiment.ReadFile(v["--bundle"], experiment.Limits{})
		if e != nil {
			return 1, e
		}
		if e = experiment.ValidateManifest(b.Manifest); e != nil {
			return 1, e
		}
		if b.Version != experiment.BundleVersion || b.Grant.Version != experiment.GrantVersion {
			return 1, fmt.Errorf("unsupported experiment bundle/grant version")
		}
		_, e = phase8Print(out, map[string]any{"version": experiment.BundleVersion, "asOf": at.UTC().Format(time.RFC3339Nano), "status": "valid", "structuralOnly": true, "authorizationGranted": false, "executionReady": false, "productionEligible": false, "liveGate": "blocked"})
		return 0, e
	}
	if !once {
		return 2, nil
	}
	if mode == "experiment" && (s.ExperimentExecution == nil || s.ExperimentExecution.Controller == nil) || mode == "calibration" && (s.CalibrationExecution == nil || s.CalibrationExecution.Controller == nil) {
		_, e = phase8Print(out, map[string]any{"status": "blocked", "reason": "protected purpose-specific enrollment, live controls, capacity, verifier, budget and witness are not installed", "authorizationGranted": false, "executionReady": false, "productionEligible": false, "hostLiveGates": "blocked", "providerLiveGates": "pending"})
		return 1, e
	}
	parent := s.Context
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	var root string
	var dispatch func() (records.Report, error)
	if mode == "experiment" {
		service := s.ExperimentExecution
		b, e := experiment.ReadFile(v["--bundle"], experiment.Limits{})
		if e != nil {
			return 1, e
		}
		if usage.Digest(b) != usage.Digest(service.Enrollment.Bundle) {
			return 1, fmt.Errorf("experiment differs from protected enrollment")
		}
		root = service.Controller.Root
		dispatch = func() (records.Report, error) {
			return service.Controller.DispatchExperimentOnce(ctx, service.Enrollment)
		}
	} else {
		service := s.CalibrationExecution
		m, e := pilot.ReadManifest(v["--campaign"])
		if e != nil {
			return 1, e
		}
		var g pilot.CalibrationGrant
		if e = phase8Read(v["--authority"], &g); e != nil {
			return 1, e
		}
		if usage.Digest(m) != usage.Digest(service.Enrollment.Manifest) || usage.Digest(g) != usage.Digest(service.Enrollment.Grant) {
			return 1, fmt.Errorf("calibration differs from protected enrollment")
		}
		root = service.Controller.Root
		dispatch = func() (records.Report, error) {
			return service.Controller.DispatchCalibrationOnce(ctx, service.Enrollment)
		}
	}
	writer, e := evidence.Reserve(v["--out"], root, v["--bundle"], v["--campaign"], v["--authority"])
	if e != nil {
		return 1, e
	}
	report, runErr := dispatch()
	// Execution observations never constitute acceptance or a production grant.
	result := map[string]any{"version": "aok-trial-observation/v1", "purpose": mode, "report": report, "authorizationGranted": false, "executionReady": false, "productionEligible": false, "providerWritebackGate": "separate-qualified-operation-required"}
	if e = writer.Write(map[string][]byte{"trial-observation.json": mustPhase8JSON(result)}); e != nil {
		return 1, e
	}
	_, e = phase8Print(out, result)
	if runErr != nil {
		return 1, runErr
	}
	if report.Status == "blocked" || report.Status == "invalid" {
		return 1, e
	}
	return 0, e
}
