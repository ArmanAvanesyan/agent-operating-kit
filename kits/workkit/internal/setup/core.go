// Package setup contains pure, non-authorizing setup and mapping checks.
package setup

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	s "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/setupv1"
	api "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/v1alpha1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

func Hash(v any) string { return engine.Hash(engine.Encode(v)) }
func NewReport(kind, asOf string) s.Report {
	return s.Report{Version: s.Version, Kind: kind, Status: "ok", AsOf: asOf, Inputs: map[string]string{}, Checks: []api.WorkBindingSpecChecksItem{}, Changes: []s.Change{}, Diagnostics: []api.Diagnostic{}, LiveQualification: "not-established"}
}
func Block(r *s.Report, code, field, message string) {
	r.Status = "blocked"
	r.Checks = append(r.Checks, api.WorkBindingSpecChecksItem{Name: code + ":" + field, Status: "missing"})
	r.Diagnostics = append(r.Diagnostics, api.Diagnostic{Code: code, Field: field, Message: message, Severity: "error"})
}
func OK(r *s.Report, name string) {
	r.Checks = append(r.Checks, api.WorkBindingSpecChecksItem{Name: name, Status: "satisfied"})
}
func ValidScope(c s.Scope) bool {
	u, e := url.Parse(c.Origin)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || c.TenantID == "" || c.ContainerID == "" || c.Principal == "" || c.APIVersion == "" {
		return false
	}
	switch c.Provider {
	case "github", "azure-devops", "jira-cloud", "linear":
		return c.BasePath == ""
	case "jira-datacenter":
		return c.DeploymentSHA256 != "" && strings.HasPrefix(c.BasePath+"/", "/") && !strings.Contains(c.BasePath, "..")
	}
	return false
}
func Category(o s.Observation, c string) bool {
	for _, v := range o.Coverage {
		if v.Category == c {
			return v.Complete && v.Status == "observed"
		}
	}
	return false
}
func Fresh(r *s.Report, o s.Observation, asOf string, age int) {
	at, e := time.Parse(time.RFC3339Nano, asOf)
	t, e2 := time.Parse(time.RFC3339Nano, o.ObservedAt)
	if e != nil || e2 != nil || age < 1 || age > 300 || at.Before(t) || at.Sub(t) >= time.Duration(age)*time.Second {
		Block(r, "refresh-needed", "observation", "fresh non-future observation required")
	} else {
		OK(r, "observation-fresh")
	}
}
func Find(o s.Observation, kind, id string) (s.Entity, bool) {
	for _, v := range o.Entities {
		if v.Kind == kind && v.ID == id {
			return v, true
		}
	}
	return s.Entity{}, false
}
func Validate(o s.Observation) error {
	if o.Version != s.Version || o.Kind != "SetupObservation" || !ValidScope(o.Scope) || o.ConnectionRef.Kind != "ConnectionProfile" || o.ConnectionRef.Id == "" || o.ConnectionRef.Revision < 1 || !validHash(string(o.ConnectionRef.Sha256)) || !validHash(o.ConfigurationSHA256) || !validHash(o.CaptureSHA256) || o.AdapterVersion == "" || o.Transport.Principal != o.Scope.Principal || len(o.Entities) > 10000 || len(o.Coverage) > 32 || len(o.Assets) > 1024 {
		return errors.New("invalid setup observation identity or bounds")
	}
	if _, e := time.Parse(time.RFC3339Nano, o.ObservedAt); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, v := range o.Entities {
		key := v.Kind + "\x00" + v.ID
		if v.ID == "" || v.Kind == "" || seen[key] || len(v.Options) > 10000 {
			return errors.New("duplicate or invalid metadata identity")
		}
		seen[key] = true
		optionIDs := map[string]bool{}
		optionNames := map[string]bool{}
		for _, op := range v.Options {
			if op.ID == "" || op.Name == "" || optionIDs[op.ID] || optionNames[op.Name] {
				return errors.New("invalid or ambiguous option domain")
			}
			optionIDs[op.ID] = true
			optionNames[op.Name] = true
		}
	}
	seen = map[string]bool{}
	for _, c := range o.Coverage {
		if seen[c.Category] || c.Category == "" || c.Complete && c.Status != "observed" {
			return errors.New("invalid category coverage")
		}
		seen[c.Category] = true
	}
	return nil
}

// Pointer implements RFC6901 without numeric coercion or path evaluation.
func Pointer(raw []byte, p string) (any, error) {
	v, e := engine.JSONValue(raw)
	if e != nil {
		return nil, e
	}
	if p == "" {
		return v, nil
	}
	if !strings.HasPrefix(p, "/") {
		return nil, errors.New("invalid JSON pointer")
	}
	for _, k := range strings.Split(p[1:], "/") {
		for i := 0; i < len(k); i++ {
			if k[i] == '~' && (i+1 >= len(k) || k[i+1] != '0' && k[i+1] != '1') {
				return nil, errors.New("invalid pointer escape")
			}
		}
		k = strings.ReplaceAll(strings.ReplaceAll(k, "~1", "/"), "~0", "~")
		switch x := v.(type) {
		case map[string]any:
			var ok bool
			v, ok = x[k]
			if !ok {
				return nil, errors.New("pointer missing")
			}
		case []any:
			i, e := strconv.Atoi(k)
			if e != nil || i < 0 || i >= len(x) || strconv.Itoa(i) != k {
				return nil, errors.New("pointer index")
			}
			v = x[i]
		default:
			return nil, errors.New("pointer traverses scalar")
		}
	}
	return v, nil
}
func Sort(o *s.Observation) {
	sort.Slice(o.Entities, func(i, j int) bool {
		return o.Entities[i].Kind+"\x00"+o.Entities[i].ID < o.Entities[j].Kind+"\x00"+o.Entities[j].ID
	})
	sort.Slice(o.Coverage, func(i, j int) bool { return o.Coverage[i].Category < o.Coverage[j].Category })
}
func Canonical(raw []byte) string {
	v, e := engine.JSONValue(raw)
	if e != nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func validHash(v string) bool {
	b, e := hex.DecodeString(v)
	return e == nil && len(b) == 32 && v == strings.ToLower(v)
}
