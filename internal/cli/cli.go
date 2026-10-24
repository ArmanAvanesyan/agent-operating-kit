// Package cli implements the aok command router and shared UX helpers.
package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

// Run parses argv and dispatches to a subcommand. Returns process exit code.
func Run(argv []string, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	if len(argv) == 0 {
		printUsage(stderr)
		return 2
	}

	cmd := argv[0]
	args := argv[1:]

	var err error
	var code int
	switch cmd {
	case "help", "-h", "--help":
		printUsage(stdout)
		return 0
	case "version":
		code, err = runVersion(args, stdout, stderr)
	case "doctor":
		code, err = runDoctor(args, stdout, stderr)
	case "delivery":
		code, err = runDelivery(args, stdout, stderr)
	case "validate":
		code, err = runValidate(args, stdout, stderr)
	case "install":
		code, err = runInstall(args, stdout, stderr)
	case "claude":
		code, err = runClaude(args, stdout, stderr)
	case "codex":
		code, err = runCodex(args, stdout, stderr)
	case "project":
		code, err = runProject(args, stdout, stderr)
	case "render":
		code, err = runRender(args, stdout, stderr)
	case "campaign":
		code, err = runCampaign(args, stdout, stderr)
	case "methodology":
		code, err = runMethodology(args, stdout, stderr)
	case "work-item":
		code, err = runWorkItem(args, stdout, stderr)
	case "work-system":
		code, err = runWorkSystem(args, stdout, stderr)
	case "planning":
		code, err = runPlanning(args, stdout, stderr)
	case "workflow":
		code, err = runWorkflow(args, stdout, stderr)
	case "readiness":
		code, err = runReadiness(args, stdout, stderr)
	case "pilot":
		code, err = runPilot(args, stdout, stderr)
	case "operations":
		code, err = runOperations(args, stdout, stderr)
	case "runtime":
		code, err = runRuntime(args, stdout, stderr)
	case "verify":
		code, err = runVerify(args, stdout, stderr)
	case "writeback":
		code, err = runWriteback(args, stdout, stderr)
	case "host":
		code, err = runHost(args, stdout, stderr)
	case "tracker":
		code, err = runTracker(args, stdout, stderr)
	case "evaluation":
		code, err = runEvaluation(args, stdout, stderr)
	case "models":
		code, err = runModels(args, stdout, stderr)
	case "usage":
		code, err = runUsage(args, stdout, stderr)
	case "decision":
		code, err = runDecision(args, stdout, stderr)
	case "agent":
		code, err = runAgent(args, stdout, stderr)
	case "task":
		code, err = runTask(args, stdout, stderr)
	case "hooks":
		code, err = runHooks(args, stdout, stderr)
	case "import":
		code, err = runImport(args, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", cmd)
		printUsage(stderr)
		return 2
	}

	if err != nil {
		var aokErr *kit.Error
		if errors.As(err, &aokErr) {
			fmt.Fprintf(stderr, "Error: %s\n", aokErr.Msg)
			return 1
		}
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return code
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `Agent Operating Kit CLI

Usage:
  aok <command> [flags]

Commands:
  version              Print kit version from .codex-plugin/plugin.json
  doctor               Check install and required kit files
  delivery             Repository delivery workflow
  validate             Validate kit content
  agent validate/resolve  Validate contracts or emit static resolution evidence
  campaign             Advisory static DAG proposals and frozen policy comparison
  methodology          Inspect catalogs and validate methodology artifacts
  work-item            List, scaffold and validate local work items
  work-system          Read-only setup and guarded provisioning plans
  planning validate/ready Validate plans or calculate eligibility from evidence
  workflow validate    Validate offline workflow bindings and records
  host                 Prepare, execute and observe opt-in native host attempts
  readiness            Inspect exact-route readiness and blocking evidence
  pilot                Validate, replay and assess controlled pilots offline
  operations           Capture and inspect quarantined SQLite snapshots
  runtime              Durable local claims, admission and recovery
  verify               Verify immutable candidates and signed acceptance
  writeback            Replay controlled tracker writeback offline
  tracker              Capture evidence, draft updates and manage controlled outbox
  models               Import and inspect offline model evidence catalogs
  usage                Inspect usage and remaining capacity; no model execution
  decision validate-result Validate typed decision outcomes
  install              Register marketplace plugin and install PATH binary
  claude setup         Configure Claude Code marketplace (aok-claude-code)
  codex setup          Install + doctor (+ optional project / claude)
  codex local-env      init|validate|actions
  project init|validate
  render pack|all|bundle|bundles
  task sync|packet      Sync GitHub issue/PR or emit task packet (requires gh)
  hooks pre|after       Pre/after task packet checks
  import history        Keyword digest of agent history files

Delivery:
  aok delivery --help  Show repository delivery commands
  aok delivery <args>  Run the repository-delivery pack

Environment:
  AOK_ROOT             Override kit root discovery`)
}

func requireRoot() (string, error) {
	return kit.FindRoot("")
}
