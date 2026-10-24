// Package windowscert evaluates native Windows filesystem probe reports. A
// passing report covers only the named local Win11/amd64/NTFS fixture scope.
package windowscert

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const Version = "aok-windows-preservation/v1"

var RequiredFeatures = []string{"acl_privacy", "reparse_points", "alternate_streams", "case_normalization", "long_paths", "dirty_untracked_preservation", "extraction_traversal", "atomic_replace_lock", "interruption_recovery", "restore_recovery"}
var sha256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Scope struct {
	OS           string `json:"os"`
	Architecture string `json:"architecture"`
	Filesystem   string `json:"filesystem"`
	DriveType    string `json:"driveType"`
	Volume       string `json:"volume"`
}
type Feature struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}
type Report struct {
	Version      string    `json:"version"`
	Producer     string    `json:"producer"`
	ScriptSHA256 string    `json:"scriptSha256"`
	ObservedAt   string    `json:"observedAt"`
	Scope        Scope     `json:"scope"`
	Features     []Feature `json:"features"`
}
type Result struct {
	Version       string   `json:"version"`
	Status        string   `json:"status"`
	AcceptedScope string   `json:"acceptedScope"`
	Missing       []string `json:"missing"`
	Failed        []string `json:"failed"`
	Limit         string   `json:"limit"`
}

// Evaluate accepts a fixture report only for exact native scope and a complete
// feature matrix. It never asserts that the broader delivery engine is certified.
func Evaluate(r Report) Result {
	out := Result{Version: Version, Status: "pending", AcceptedScope: "Windows 11 amd64 local NTFS", Limit: "filesystem probes only; repository-delivery workflow preservation is not certified"}
	if r.Version != Version {
		out.Status = "invalid"
		out.Failed = append(out.Failed, "report.version")
		return out
	}
	if r.Producer != "aok-windows-preservation-probe/v1" || !sha256Pattern.MatchString(r.ScriptSHA256) {
		out.Status = "invalid"
		out.Failed = append(out.Failed, "report.provenance")
		return out
	}
	if _, err := time.Parse(time.RFC3339, r.ObservedAt); err != nil {
		out.Status = "invalid"
		out.Failed = append(out.Failed, "report.observedAt")
		return out
	}
	if r.Scope.OS != "Windows 11" || r.Scope.Architecture != "amd64" || r.Scope.Filesystem != "NTFS" || r.Scope.DriveType != "Fixed" || r.Scope.Volume == "" {
		out.Status = "out-of-scope"
		out.Failed = append(out.Failed, "scope")
		return out
	}
	seen := map[string]string{}
	for _, f := range r.Features {
		if _, ok := seen[f.ID]; ok {
			out.Status = "invalid"
			out.Failed = append(out.Failed, "duplicate:"+f.ID)
			return out
		}
		if f.Status != "pass" && f.Status != "fail" && f.Status != "pending" {
			out.Status = "invalid"
			out.Failed = append(out.Failed, "status:"+f.ID)
			return out
		}
		seen[f.ID] = f.Status
	}
	for _, id := range RequiredFeatures {
		switch seen[id] {
		case "pass":
		case "", "pending":
			out.Missing = append(out.Missing, id)
		default:
			out.Failed = append(out.Failed, id)
		}
	}
	for id := range seen {
		known := false
		for _, need := range RequiredFeatures {
			if id == need {
				known = true
				break
			}
		}
		if !known {
			out.Failed = append(out.Failed, "unknown:"+id)
		}
	}
	if len(out.Failed) > 0 {
		out.Status = "failed"
	} else if len(out.Missing) > 0 {
		out.Status = "pending"
	} else {
		out.Status = "fixture-passed"
	}
	return out
}

func Validate(r Report) error {
	x := Evaluate(r)
	if x.Status != "fixture-passed" {
		return fmt.Errorf("Windows preservation report %s: %s", x.Status, strings.Join(append(x.Missing, x.Failed...), ", "))
	}
	return nil
}
func ValidateReportShape(r Report) error {
	if r.Version == "" {
		return errors.New("report version is required")
	}
	return nil
}
