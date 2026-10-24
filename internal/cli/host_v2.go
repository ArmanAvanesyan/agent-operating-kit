package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	hostv2 "github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge/v2"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostdriver"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/tracker"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

type driverPolicyFile struct {
	Version string            `json:"version"`
	Policy  hostdriver.Policy `json:"policy"`
}
type driverAssignmentFile struct {
	Version    string                `json:"version"`
	Assignment hostdriver.Assignment `json:"assignment"`
}
type driverReplayManifest struct {
	Version    string               `json:"version"`
	Host       string               `json:"host"`
	AttemptID  string               `json:"attemptId"`
	ObservedAt string               `json:"observedAt"`
	Frames     []contracts.AssetRef `json:"frames"`
}

func runHostList(args []string, out io.Writer) (int, error) {
	_, machine, e := selectionFlags(args, nil)
	if e != nil {
		return 2, e
	}
	rows := []map[string]any{}
	for _, d := range hostdriver.List() {
		rows = append(rows, map[string]any{"descriptor": d, "implementation": "registered", "fixtureConformance": "see-host-specific-tests", "liveRouteQualification": "blocked-or-pending", "platformCertification": "pending", "productionActivation": "disabled", "authorizationGranted": false, "executionReady": false})
	}
	if machine {
		b, _ := json.Marshal(rows)
		fmt.Fprintln(out, string(b))
	} else {
		for _, r := range rows {
			d := r["descriptor"].(hostdriver.Descriptor)
			fmt.Fprintf(out, "%s\tprotocol=%s\tprotocolSupport=%s\tlive=pending activation=disabled\n", d.ID, d.Protocol, d.Support)
		}
	}
	return 0, nil
}
func runHostV2(args []string, out io.Writer) (int, error) {
	if len(args) == 0 {
		return 2, fmt.Errorf("host operation required")
	}
	op := args[0]
	allowed := map[string][]string{"inspect": {"host", "policy", "as-of", "out"}, "prepare": {"host", "assignment", "policy", "as-of", "out"}, "replay": {"host", "manifest", "as-of", "out"}, "qualify": {"host", "policy", "as-of", "out"}}
	keys, ok := allowed[op]
	if !ok {
		return 2, fmt.Errorf("new drivers support inspect, prepare, replay and qualification reporting; execution requires the protected P7 route")
	}
	v, machine, e := selectionFlags(args[1:], keys)
	if e != nil {
		return 2, e
	}
	for _, k := range keys {
		if v["--"+k] == "" {
			return 2, fmt.Errorf("missing --%s", k)
		}
	}
	at, e := time.Parse(time.RFC3339Nano, v["--as-of"])
	if e != nil {
		return 2, e
	}
	d, ok := hostdriver.Lookup(v["--host"])
	if !ok {
		return 2, fmt.Errorf("unregistered host")
	}
	protected := []string{}
	read := func(path string, target any) error {
		protected = append(protected, path)
		b, e := tracker.ReadFile(path)
		if e != nil {
			return e
		}
		return usage.Decode(b, target)
	}
	result := map[string]any{"version": hostv2.Version, "host": d.ID, "descriptorSha256": d.Digest, "asOf": at.UTC().Format(time.RFC3339Nano), "implementation": "registered", "fixtureConformance": "protocol-specific-tests", "liveRouteQualification": "blocked", "platformCertification": "pending", "productionActivation": "disabled", "authorizationGranted": false, "executionReady": false}
	status := "valid"
	files := map[string][]byte{}
	if op == "replay" {
		var m driverReplayManifest
		if e = read(v["--manifest"], &m); e != nil {
			return 1, e
		}
		observed, e := time.Parse(time.RFC3339Nano, m.ObservedAt)
		if e != nil || observed.After(at) || m.Version != hostv2.Version || m.Host != d.ID || m.AttemptID == "" || len(m.Frames) == 0 || len(m.Frames) > hostv2.MaxFrames {
			return 1, fmt.Errorf("invalid exact-host replay manifest")
		}
		frames := [][]byte{}
		total := 0
		for _, a := range m.Frames {
			b, e := tracker.ReadAsset(filepath.Dir(v["--manifest"]), a.Path)
			if e != nil {
				return 1, e
			}
			total += len(b)
			if usage.Hash(b) != a.SHA256 || total > 8<<20 {
				return 1, fmt.Errorf("changed or oversized replay asset")
			}
			frames = append(frames, b)
		}
		observation, e := hostv2.ReplayScoped(d.ID, "", m.AttemptID, frames)
		if e != nil {
			return 1, e
		}
		result["observation"] = observation
		result["attemptId"] = m.AttemptID
		result["observedAt"] = m.ObservedAt
	} else {
		var p driverPolicyFile
		if e = read(v["--policy"], &p); e != nil {
			return 1, e
		}
		if p.Version != "aok-host-driver-input/v1" || p.Policy.Host != d.ID {
			return 1, fmt.Errorf("v2 exact-host policy required")
		}
		result["policySha256"] = usage.Digest(p.Policy)
		if d.Support != hostdriver.Supported {
			result["status"] = "unsupported"
			result["reason"] = d.Reason
			status = "blocked"
		} else {
			bound, e := hostdriver.BindPolicy(p.Policy)
			if e != nil {
				return 1, e
			}
			_ = bound
			if op == "prepare" {
				var a driverAssignmentFile
				if e = read(v["--assignment"], &a); e != nil {
					return 1, e
				}
				if a.Version != "aok-host-driver-input/v1" {
					return 1, fmt.Errorf("v2 assignment required")
				}
				prepared, e := hostdriver.Prepare(a.Assignment, p.Policy)
				if e != nil {
					return 1, e
				}
				result["prepared"] = prepared
			}
			if op == "inspect" {
				result["installedBinary"] = "unknown"
				result["authentication"] = "unknown"
				result["availableModels"] = "unknown"
				result["controls"] = "unknown"
			}
			if op == "qualify" {
				status = "blocked"
				result["status"] = "blocked"
				result["reason"] = "live qualification requires protected external enrollment and independently observed controls; offline policy is not qualification"
			}
		}
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	files["report.json"] = append(b, '\n')
	if e = evidence.WriteFiles(v["--out"], files, protected...); e != nil {
		return 1, e
	}
	if machine {
		fmt.Fprintln(out, string(b))
	} else {
		fmt.Fprintf(out, "Host %s %s: %s; live blocked, production disabled\n", d.ID, op, status)
	}
	if status == "blocked" {
		return 1, nil
	}
	return 0, nil
}
