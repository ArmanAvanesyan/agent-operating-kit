package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"time"
)

const trackerHelp = `aok tracker discover --source FILE --read-policy FILE --out NEW-DIRECTORY [--json]
aok tracker capture --source FILE --read-policy FILE [--checkpoint FILE] --out NEW-DIRECTORY [--json]
aok tracker import --manifest FILE --out NEW-DIRECTORY [--json]
aok tracker validate BUNDLE [--json]
aok tracker inspect BUNDLE --as-of RFC3339 [--json]
aok tracker packet --snapshot BUNDLE --mapping FILE --out NEW-DIRECTORY [--json]
aok tracker diff OLD-BUNDLE NEW-BUNDLE [--json]
aok tracker plan-update --snapshot BUNDLE --mapping FILE --desired FILE --as-of RFC3339 --out NEW-DIRECTORY [--json]
Read capture and local drafts remain non-authorizing. Live-read and write conformance are provider-specific.` + "\n" + writebackHelp

func runTracker(args []string, stdout, stderr io.Writer) (int, error) {
	code, e := trackerCommand(args, stdout, stderr)
	if code == 2 && e != nil {
		fmt.Fprintln(stderr, e)
		return 2, nil
	}
	return code, e
}
func trackerCommand(args []string, stdout, stderr io.Writer) (int, error) {
	if len(args) > 0 {
		switch args[0] {
		case "authorize-plan", "enqueue", "dispatch", "reconcile", "outbox", "revoke":
			return runTrackerWriteback(args, stdout, stderr)
		}
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, trackerHelp)
		return 2, nil
	}
	if args[0] == "--help" || args[0] == "help" || (len(args) > 1 && args[1] == "--help") {
		fmt.Fprintln(stdout, trackerHelp)
		return 0, nil
	}
	op := args[0]
	keys := map[string][]string{"discover": {"source", "read-policy", "tls-roots", "out"}, "capture": {"source", "read-policy", "tls-roots", "checkpoint", "out"}, "import": {"manifest", "out"}, "validate": {}, "inspect": {"as-of"}, "packet": {"snapshot", "mapping", "out"}, "diff": {}, "plan-update": {"snapshot", "mapping", "desired", "as-of", "out"}}
	allowed, ok := keys[op]
	if !ok {
		return 2, fmt.Errorf("unknown tracker command")
	}
	flags := []string{}
	pos := []string{}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if len(a) > 0 && a[0] == '-' {
			flags = append(flags, a)
			if a != "--json" {
				if i+1 == len(args) {
					return 2, fmt.Errorf("missing argument")
				}
				i++
				flags = append(flags, args[i])
			}
		} else {
			pos = append(pos, a)
		}
	}
	values, machine, e := selectionFlags(flags, allowed)
	if e != nil {
		return 2, e
	}
	required := map[string][]string{"discover": {"source", "out"}, "capture": {"source", "out"}, "import": {"manifest", "out"}, "inspect": {"as-of"}, "packet": {"snapshot", "mapping", "out"}, "plan-update": {"snapshot", "mapping", "desired", "as-of", "out"}}[op]
	for _, k := range required {
		if values["--"+k] == "" {
			return 2, fmt.Errorf("missing --%s", k)
		}
	}
	wantPos := 0
	if op == "validate" || op == "inspect" {
		wantPos = 1
	}
	if op == "diff" {
		wantPos = 2
	}
	if len(pos) != wantPos {
		return 2, fmt.Errorf("incorrect positional arguments")
	}
	var asOf time.Time
	if text := values["--as-of"]; text != "" {
		asOf, e = time.Parse(time.RFC3339Nano, text)
		if e != nil {
			return 2, e
		}
	}
	var result any
	files := map[string][]byte{}
	status := "valid"
	protected := []string{}
	for k, v := range values {
		if k != "--out" && k != "--as-of" {
			protected = append(protected, v)
		}
	}
	protected = append(protected, pos...)
	switch op {
	case "import", "capture", "discover":
		var c *tracker.Capture
		if op == "import" {
			c, e = tracker.Import(values["--manifest"])
		} else {
			s, readErr := tracker.ReadSource(values["--source"])
			if readErr != nil {
				return 1, readErr
			}
			policyPath := values["--read-policy"]
			if policyPath == "" {
				policyPath = os.Getenv("AOK_TRACKER_READ_POLICY")
			}
			if policyPath == "" {
				return 1, fmt.Errorf("live reads require an operator-supplied --read-policy (or AOK_TRACKER_READ_POLICY); source files alone cannot authorize credential destinations")
			}
			policy, policyErr := tracker.ReadReadPolicy(policyPath)
			if policyErr != nil {
				return 1, policyErr
			}
			s.ReadPolicySHA256 = tracker.ReadPolicyHash(policy)
			protected = append(protected, policyPath)
			var roots []byte
			if s.Provider == "jira-datacenter" {
				if values["--tls-roots"] == "" {
					return 1, fmt.Errorf("Data Center requires operator-supplied --tls-roots matching protected deployment pin")
				}
				roots, e = tracker.ReadFile(values["--tls-roots"])
				if e != nil {
					return 1, e
				}
			} else if values["--tls-roots"] != "" {
				return 2, fmt.Errorf("--tls-roots applies only to Data Center")
			}
			var transport tracker.Transport = tracker.HTTPTransport{Policy: policy, DCRootsPEM: roots}
			if s.Provider == "github" {
				transport = tracker.GitHubTransport{Policy: policy}
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()
			if values["--checkpoint"] != "" {
				c, e = tracker.CaptureWithCheckpoint(ctx, s, op, values["--checkpoint"], transport)
			} else {
				c, e = tracker.CaptureSource(ctx, s, op, nil, transport)
			}
		}
		if e != nil {
			return 1, e
		}
		files = c.Files
		result = c.Snapshot
		status = c.Snapshot.Report.Status
	case "validate", "inspect":
		c, loadErr := tracker.Load(pos[0])
		if loadErr != nil {
			return 1, loadErr
		}
		if op == "inspect" {
			result, e = tracker.Inspect(c, asOf)
		} else {
			result = c.Snapshot.Report
			status = c.Snapshot.Report.Status
		}
		if e != nil {
			return 1, e
		}
	case "diff":
		a, err := tracker.Load(pos[0])
		if err != nil {
			return 1, err
		}
		b, err := tracker.Load(pos[1])
		if err != nil {
			return 1, err
		}
		diff, diffErr := tracker.Diff(a, b)
		result, e = diff, diffErr
		status = diff.Report.Status
		if e != nil {
			return 1, e
		}
	case "packet", "plan-update":
		c, err := tracker.Load(values["--snapshot"])
		if err != nil {
			return 1, err
		}
		mapping, err := tracker.ReadMapping(values["--mapping"])
		if err != nil {
			return 1, err
		}
		if op == "packet" {
			projection, err := tracker.Project(c, mapping)
			if err != nil {
				return 1, err
			}
			result = projection
			files = projection.Files
			status = projection.Report.Status
		} else {
			desired, err := tracker.ReadDesired(values["--desired"])
			if err != nil {
				return 1, err
			}
			if desired.Comment != nil {
				b, err := tracker.ReadAsset(filepath.Dir(values["--desired"]), desired.Comment.Path)
				if err != nil {
					return 1, err
				}
				if tracker.Hash(b) != desired.Comment.SHA256 {
					return 1, fmt.Errorf("comment asset hash changed")
				}
				path := "assets/comments/" + desired.Comment.SHA256 + ".txt"
				files[path] = b
				ref := *desired.Comment
				ref.Path = path
				desired.Comment = &ref
			}
			plan, err := tracker.PlanUpdate(c, mapping, desired, asOf)
			if err != nil {
				return 1, err
			}
			result = plan
			status = plan.Report.Status
			b, _ := json.MarshalIndent(plan, "", "  ")
			files["update-plan.json"] = append(b, '\n')
			mb, _ := json.MarshalIndent(mapping, "", "  ")
			files["mapping.json"] = append(mb, '\n')
		}
	}
	if len(files) > 0 {
		if e = evidence.WriteFiles(values["--out"], files, protected...); e != nil {
			return 1, e
		}
	}
	if machine {
		b, e := json.MarshalIndent(result, "", "  ")
		if e != nil {
			return 1, e
		}
		fmt.Fprintln(stdout, string(b))
	} else {
		fmt.Fprintf(stdout, "Tracker %s: %s\n", op, status)
		if values["--out"] != "" {
			fmt.Fprintln(stdout, "Evidence:", values["--out"])
		}
		if status != "valid" {
			fmt.Fprintln(stdout, "Inspect the report diagnostics; no execution authorization was granted.")
		}
	}
	if status != "valid" {
		return 1, nil
	}
	return 0, nil
}
