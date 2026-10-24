package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/agentvalidate"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/planning"
)

// workflowReport describes static consistency only. Neither declaration nor
// successful validation authorizes an operation or establishes runtime readiness.
type workflowReport struct {
	Status               string                 `json:"status"`
	ResourceCounts       map[string]int         `json:"resourceCounts"`
	AuthorizationGranted bool                   `json:"authorizationGranted"`
	ExecutionReady       bool                   `json:"executionReady"`
	Diagnostics          []contracts.Diagnostic `json:"diagnostics"`
}

func runWorkflow(args []string, stdout, stderr io.Writer) (int, error) {
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "Usage: aok workflow validate <corpus-directory> [--json]\nValidate static workflow records offline; never execute or authorize operations.")
	}
	if len(args) == 1 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		usage(stdout)
		return 0, nil
	}
	if len(args) == 0 || args[0] != "validate" {
		usage(stderr)
		return 2, nil
	}
	directory, machine := "", false
	for _, arg := range args[1:] {
		switch {
		case arg == "--help" || arg == "-h":
			usage(stdout)
			return 0, nil
		case arg == "--json":
			if machine {
				usage(stderr)
				return 2, nil
			}
			machine = true
		case strings.HasPrefix(arg, "-") || directory != "":
			usage(stderr)
			return 2, nil
		default:
			directory = arg
		}
	}
	if directory == "" {
		usage(stderr)
		return 2, nil
	}
	base := planning.Validate(directory)
	report := workflowReport{Status: base.Status, ResourceCounts: map[string]int{}, Diagnostics: base.Diagnostics}
	corpus, loaded := agentvalidate.Load(directory)
	if loaded.Status == "invalid" && report.Status == "valid" {
		// Counting must not conceal a read/parse failure if the caller changes
		// input between validation and report assembly. This is still an
		// explicit-file check, not an atomic filesystem snapshot.
		report.Status = "invalid"
		report.Diagnostics = append(report.Diagnostics, loaded.Diagnostics...)
	}
	workflowCount := 0
	if corpus != nil {
		for _, doc := range corpus.Resources {
			report.ResourceCounts[doc.Resource.Kind]++
			switch doc.Resource.Kind {
			case "WorkItemBinding", "HostTaskBinding", "RunRecord", "ConnectorCapability", "SyncOperation", "SyncReceipt":
				workflowCount++
			}
		}
	}
	if workflowCount == 0 {
		report.Status = "invalid"
		report.Diagnostics = append(report.Diagnostics, contracts.Diagnostic{
			Code: "workflow.corpus.empty", Severity: "error", Message: "corpus contains no Phase 1 workflow records",
		})
	}
	sort.SliceStable(report.Diagnostics, func(i, j int) bool {
		a, b := report.Diagnostics[i], report.Diagnostics[j]
		if a.Source != b.Source {
			return a.Source < b.Source
		}
		if a.Field != b.Field {
			return a.Field < b.Field
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		if a.Severity != b.Severity {
			return a.Severity < b.Severity
		}
		return a.Message < b.Message
	})
	if machine {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return 1, err
		}
	} else {
		fmt.Fprintf(stdout, "Workflow validation: %s\nAuthorization granted: false\nExecution ready: false\n", report.Status)
		kinds := make([]string, 0, len(report.ResourceCounts))
		for kind := range report.ResourceCounts {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			fmt.Fprintf(stdout, "%s: %d\n", kind, report.ResourceCounts[kind])
		}
		for _, d := range report.Diagnostics {
			fmt.Fprintf(stdout, "%s %s %s %s: %s\n", d.Severity, d.Code, d.Source, d.Field, d.Message)
		}
	}
	if report.Status != "valid" {
		return 1, nil
	}
	return 0, nil
}
