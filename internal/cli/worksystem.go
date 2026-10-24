package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/worksystem"
)

func runWorkSystem(args []string, stdout, stderr io.Writer) (int, error) {
	return runWorkSystemWithReader(context.Background(), args, nil, stdout, stderr)
}

// runWorkSystemWithReader is the injection point for the application/provider
// adapter. The default CLI intentionally has no ambient credential discovery.
func runWorkSystemWithReader(ctx context.Context, args []string, reader worksystem.Reader, stdout, stderr io.Writer) (int, error) {
	if len(args) > 0 && (args[0] == "plan" || args[0] == "validate-plan" || args[0] == "apply" || args[0] == "reconcile") {
		return runProvision(args, stdout, stderr)
	}
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "Usage: aok work-system setup github --owner OWNER (--project-number N | --project-title TITLE) [--path DIR] [--write-binding] [--json]\n       aok work-system setup azure-devops --organization URL --project PROJECT [--team TEAM] [--board BOARD] [--work-item-type TYPE] [--path DIR] [--write-binding] [--json]\n       aok work-system validate [DIR] [--json]")
	}
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		usage(stdout)
		return 0, nil
	}
	if args[0] == "setup" {
		if len(args) < 2 {
			return 2, nil
		}
		provider := args[1]
		req := worksystem.Request{Provider: provider}
		path := "."
		write, jsonOut := false, false
		projectTitle := ""
		for i := 2; i < len(args); i++ {
			switch args[i] {
			case "--owner", "--organization", "--project", "--project-title", "--team", "--board", "--work-item-type", "--project-number", "--path":
				if i+1 >= len(args) {
					usage(stderr)
					return 2, nil
				}
				i++
				v := args[i]
				switch args[i-1] {
				case "--owner":
					req.Owner = v
				case "--organization":
					req.Organization = v
				case "--project":
					req.Project = v
				case "--project-title":
					projectTitle = v
				case "--team":
					req.Team = v
				case "--board":
					req.Board = v
				case "--work-item-type":
					req.WorkItemType = v
				case "--project-number":
					n, e := fmt.Sscanf(v, "%d", &req.ProjectNumber)
					if e != nil || n != 1 || req.ProjectNumber < 1 {
						return 2, nil
					}
				case "--path":
					path = v
				}
			case "--write-binding":
				write = true
			case "--json":
				jsonOut = true
			case "-h", "--help":
				usage(stdout)
				return 0, nil
			default:
				usage(stderr)
				return 2, nil
			}
		}
		if provider == "github" && projectTitle != "" {
			req.Project = projectTitle
		}
		report, e := worksystem.Setup(ctx, reader, req)
		if e != nil {
			return 1, e
		}
		if write {
			if report["conformant"] != true || obj(report["binding"]) == nil {
				return emitWorkSystem(report, jsonOut, stdout), fmt.Errorf("refusing to write a nonconformant work-system binding")
			}
			target, e := filepath.Abs(path)
			if e != nil {
				return 1, e
			}
			manifestPath := filepath.Join(target, ".aok", "project.json")
			manifest, e := readJSON(manifestPath)
			if os.IsNotExist(e) {
				manifest = map[string]any{}
			} else if e != nil {
				return 1, e
			}
			binding := obj(report["binding"])
			binding["verifiedAt"] = time.Now().UTC().Format(time.RFC3339)
			manifest["workSystem"] = binding
			if e = os.MkdirAll(filepath.Dir(manifestPath), 0755); e != nil {
				return 1, e
			}
			b, e := json.MarshalIndent(manifest, "", "  ")
			if e != nil {
				return 1, e
			}
			if e = os.WriteFile(manifestPath, append(b, '\n'), 0644); e != nil {
				return 1, e
			}
			report["bindingWritten"] = filepath.Join(target, ".aok", "project.json")
		}
		return emitWorkSystem(report, jsonOut, stdout), nil
	}
	if args[0] == "validate" {
		path := "."
		jsonOut := false
		for _, a := range args[1:] {
			if a == "--json" {
				jsonOut = true
			} else if a == "-h" || a == "--help" {
				usage(stdout)
				return 0, nil
			} else if strings.HasPrefix(a, "-") {
				usage(stderr)
				return 2, nil
			} else if path == "." {
				path = a
			} else {
				usage(stderr)
				return 2, nil
			}
		}
		target, e := filepath.Abs(path)
		if e != nil {
			return 1, e
		}
		manifestPath := filepath.Join(target, ".aok", "project.json")
		manifest, e := readJSON(manifestPath)
		if e != nil {
			return 1, fmt.Errorf("No work-system binding in %s", manifestPath)
		}
		binding := obj(manifest["workSystem"])
		if binding == nil || len(binding) == 0 {
			return 1, fmt.Errorf("No work-system binding in %s", manifestPath)
		}
		provider := str(binding["provider"])
		req := worksystem.Request{Provider: provider}
		switch provider {
		case "github":
			req.Owner = str(binding["owner"])
			n, _ := binding["projectNumber"].(float64)
			req.ProjectNumber = int(n)
			if req.Owner == "" || n == 0 {
				return 1, fmt.Errorf("GitHub work-system binding is missing owner or projectNumber")
			}
		case "azure-devops":
			req.Organization = str(binding["organization"])
			pr := obj(binding["project"])
			req.Project = defaultString(str(pr["id"]), str(pr["name"]))
			req.Team = str(obj(binding["team"])["name"])
			req.Board = str(obj(binding["board"])["name"])
			wit := obj(binding["workItemType"])
			req.WorkItemType = defaultString(str(wit["referenceName"]), str(wit["name"]))
			if req.Organization == "" || req.Project == "" {
				return 1, fmt.Errorf("Azure DevOps work-system binding is missing organization or project")
			}
		default:
			return 1, fmt.Errorf("Unsupported work-system provider in %s: %q", manifestPath, provider)
		}
		report, e := worksystem.Setup(ctx, reader, req)
		if e != nil {
			return 1, e
		}
		if report["conformant"] == true && obj(report["binding"]) != nil {
			old := map[string]any{}
			for k, v := range binding {
				if k != "verifiedAt" {
					old[k] = v
				}
			}
			fresh := map[string]any{}
			for k, v := range obj(report["binding"]) {
				if k != "verifiedAt" {
					fresh[k] = v
				}
			}
			a, _ := json.Marshal(old)
			b, _ := json.Marshal(fresh)
			if string(a) != string(b) {
				checks := arr(report["checks"])
				checks = append(checks, map[string]any{"name": "binding", "status": "mismatched", "detail": "stored IDs or mappings differ from provider"})
				report["checks"] = checks
				report["conformant"] = false
			} else {
				checks := arr(report["checks"])
				checks = append(checks, map[string]any{"name": "binding", "status": "satisfied", "detail": "stored IDs and mappings match provider"})
				report["checks"] = checks
			}
		}
		if emitWorkSystem(report, jsonOut, stdout) != 0 {
			return 1, nil
		}
		return 0, nil
	}
	usage(stderr)
	return 2, nil
}

func emitWorkSystem(report map[string]any, jsonOut bool, out io.Writer) int {
	if jsonOut {
		if e := json.NewEncoder(out).Encode(report); e != nil {
			return 1
		}
		return 0
	}
	fmt.Fprintf(out, "Work-system plan: %s (%s)\nConformant: %s\n", report["provider"], report["supportLevel"], map[bool]string{true: "yes", false: "no"}[report["conformant"] == true])
	p := obj(report["project"])
	if len(p) > 0 {
		fmt.Fprintf(out, "Project: %s\n", defaultString(str(p["title"]), defaultString(str(p["name"]), defaultString(str(p["id"]), "unknown"))))
	}
	if report["bindingWritten"] != nil {
		fmt.Fprintf(out, "Binding written: %s\n", report["bindingWritten"])
	}
	fmt.Fprintln(out, "Checks:")
	for _, v := range arr(report["checks"]) {
		c := obj(v)
		fmt.Fprintf(out, "- %s %s: %s\n", strings.ToUpper(str(c["status"])), c["name"], c["detail"])
	}
	fmt.Fprintln(out, "Future operations (not executed):")
	ops := arr(report["futureOperations"])
	if len(ops) == 0 {
		fmt.Fprintln(out, "- none")
	} else {
		for _, v := range ops {
			fmt.Fprintf(out, "- %v\n", v)
		}
	}
	if report["conformant"] != true {
		return 1
	}
	return 0
}
