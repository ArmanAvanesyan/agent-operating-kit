package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/windowscert"
)

func runWindowsPreservationEvaluate(args []string, stdout, stderr io.Writer) (int, error) {
	path := ""
	jsonOutput := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--report":
			if i+1 >= len(args) || path != "" {
				return 2, fmt.Errorf("Usage: aok delivery windows-preservation-evaluate [--report FILE] [--json]")
			}
			i++
			path = args[i]
		case "--json":
			jsonOutput = true
		case "--help", "-h":
			fmt.Fprintln(stdout, "Usage: aok delivery windows-preservation-evaluate [--report FILE] [--json]\nEvaluates native Windows probe observations; never certifies repository-delivery preservation.")
			return 0, nil
		default:
			return 2, fmt.Errorf("unknown windows-preservation-evaluate argument %q", args[i])
		}
	}
	result := windowscert.Result{Version: windowscert.Version, Status: "pending", AcceptedScope: "Windows 11 amd64 local NTFS", Missing: append([]string(nil), windowscert.RequiredFeatures...), Limit: "native Windows probe report required; fixture observations cannot certify repository-delivery workflow preservation"}
	if path != "" {
		b, err := strictReadFile(path)
		if err != nil {
			return 1, err
		}
		var report windowscert.Report
		if err = decodeWindowsReport(b, &report); err != nil {
			return 1, fmt.Errorf("invalid Windows preservation report: %w", err)
		}
		result = windowscert.Evaluate(report)
	}
	if jsonOutput {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return 1, err
		}
	} else {
		fmt.Fprintf(stdout, "Windows preservation probe: %s (%s)\n", result.Status, result.AcceptedScope)
		fmt.Fprintln(stdout, result.Limit)
		if len(result.Missing) > 0 {
			fmt.Fprintf(stdout, "Missing: %v\n", result.Missing)
		}
		if len(result.Failed) > 0 {
			fmt.Fprintf(stdout, "Failed: %v\n", result.Failed)
		}
	}
	if result.Status == "fixture-passed" {
		return 0, nil
	}
	return 1, nil
}

func decodeWindowsReport(b []byte, report *windowscert.Report) error {
	if err := strictDecodeJSON(b, report); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(report)
}
