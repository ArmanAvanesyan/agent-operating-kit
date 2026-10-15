package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/doctor"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/kit"
)

func runDoctor(args []string, stdout, _ io.Writer) (int, error) {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	homeFlag := fs.String("home", "", "override home directory")
	jsonFlag := fs.Bool("json", false, "print JSON report")
	reportFlag := fs.String("report", "", "write markdown setup report")
	if err := fs.Parse(args); err != nil {
		return 2, nil
	}

	root, err := requireRoot()
	if err != nil {
		return 1, err
	}
	home, err := kit.UserHome(*homeFlag)
	if err != nil {
		return 1, err
	}

	report := doctor.Collect(root, home)
	if *reportFlag != "" {
		out := kitExpand(*reportFlag)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return 1, err
		}
		if err := os.WriteFile(out, []byte(doctor.ReportText(report)), 0o644); err != nil {
			return 1, err
		}
		fmt.Fprintf(stdout, "Wrote setup report: %s\n", out)
	}
	if *jsonFlag {
		if err := doctor.WriteJSON(stdout, report); err != nil {
			return 1, err
		}
	} else {
		doctor.PrintHuman(stdout, report)
	}
	if report["requiredOk"].(bool) {
		return 0, nil
	}
	return 1, nil
}

func kitExpand(path string) string {
	if path == "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if len(path) >= 2 && path[0] == '~' && (path[1] == '/' || path[1] == '\\') {
		return filepath.Join(home, path[2:])
	}
	return path
}
