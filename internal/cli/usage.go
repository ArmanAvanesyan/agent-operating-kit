package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evidence"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/collectors"
)

func runUsage(args []string, stdout, stderr io.Writer) (int, error) {
	return runUsageServices(args, stdout, stderr, Phase9Services{})
}
func runUsageServices(args []string, stdout, stderr io.Writer, services Phase9Services) (int, error) {
	help := func(w io.Writer) {
		fmt.Fprintln(w, `Usage:
  aok usage validate <export> --policy <file> [--json]
  aok usage import --source <host> --input <file> --binding <file> --policy <file> --received-at <RFC3339> --out <new-directory> [--stream-revision <positive-integer>] [--json]
  aok usage replay --manifest <file> --policy <file> --as-of <RFC3339> --out <new-directory> [--json]
  aok usage summary <export> --policy <file> --as-of <RFC3339> [--group-by host,model,pool,project,work-item,run] [--json]
  aok usage capacity <export> --catalog <bundle> --catalog-policy <file> --profile <file> --policy <file> --as-of <RFC3339> [--demand <file>] [--require-fit] [--json]
  aok usage collectors [--host <host>] [--json]
  aok usage collect --host <host> --binding <file> --policy <file> --out <new-directory> [--json]
Offline accounting does not grant authority or execute models. Live and runtime gates are separate.`)
	}
	if len(args) == 0 {
		help(stderr)
		return 2, nil
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		help(stdout)
		return 0, nil
	}
	op := args[0]
	allowed := map[string][]string{
		"validate": {"--policy"}, "import": {"--source", "--input", "--binding", "--policy", "--received-at", "--out", "--stream-revision"}, "replay": {"--manifest", "--policy", "--as-of", "--out"}, "summary": {"--policy", "--as-of", "--group-by"}, "capacity": {"--catalog", "--catalog-policy", "--profile", "--policy", "--as-of", "--demand"}, "collectors": {"--host"}, "collect": {"--host", "--binding", "--policy", "--out"},
	}
	flags, known := allowed[op]
	if !known {
		help(stderr)
		return 2, nil
	}
	values := map[string]string{}
	pos := []string{}
	machine, requireFit := false, false
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--help" || a == "-h" {
			help(stdout)
			return 0, nil
		}
		if a == "--json" {
			if machine {
				return 2, nil
			}
			machine = true
			continue
		}
		if a == "--require-fit" && op == "capacity" {
			if requireFit {
				return 2, nil
			}
			requireFit = true
			continue
		}
		if strings.HasPrefix(a, "-") {
			ok := false
			for _, f := range flags {
				if f == a {
					ok = true
				}
			}
			if !ok || values[a] != "" || i+1 >= len(args) || args[i+1] == "" || strings.HasPrefix(args[i+1], "-") {
				help(stderr)
				return 2, nil
			}
			i++
			values[a] = args[i]
		} else {
			pos = append(pos, a)
		}
	}
	want := 0
	if op == "validate" || op == "summary" || op == "capacity" {
		want = 1
	}
	if len(pos) != want {
		help(stderr)
		return 2, nil
	}
	required := map[string][]string{"validate": {"--policy"}, "import": {"--source", "--input", "--binding", "--policy", "--received-at", "--out"}, "replay": {"--manifest", "--policy", "--as-of", "--out"}, "summary": {"--policy", "--as-of"}, "capacity": {"--catalog", "--catalog-policy", "--profile", "--policy", "--as-of"}, "collect": {"--host", "--binding", "--policy", "--out"}}
	for _, f := range required[op] {
		if values[f] == "" {
			help(stderr)
			return 2, nil
		}
	}
	var at time.Time
	for _, flag := range []string{"--as-of", "--received-at"} {
		if values[flag] != "" {
			var e error
			at, e = time.Parse(time.RFC3339Nano, values[flag])
			if e != nil {
				help(stderr)
				return 2, nil
			}
		}
	}
	write := func(value any, code int) (int, error) {
		if !machine {
			fmt.Fprintf(stdout, "Usage accounting %s\n", op)
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return code, enc.Encode(value)
	}
	invalid := func(code string, err error) (int, error) {
		r := usage.BaseReport()
		r.Status = "invalid"
		r.Diagnostics = append(r.Diagnostics, usage.Diagnostic{Code: code, Message: err.Error()})
		return write(r, 1)
	}
	if op == "collectors" {
		ds := collectors.Describe()
		if values["--host"] != "" {
			filtered := []usage.CollectorDescriptor{}
			for _, d := range ds {
				if d.Host == values["--host"] {
					filtered = append(filtered, d)
				}
			}
			if len(filtered) == 0 {
				return 2, nil
			}
			ds = filtered
		}
		return write(struct {
			usage.Report
			Collectors []usage.CollectorDescriptor `json:"collectors"`
		}{usage.BaseReport(), ds}, 0)
	}
	p, pb, err := usage.ReadPolicy(values["--policy"])
	if err != nil {
		return invalid("usage.policy", err)
	}
	if op == "import" || op == "collect" {
		bb, e := modelcatalog.ReadFile(values["--binding"])
		if e != nil {
			return invalid("usage.binding", e)
		}
		if e = usage.ValidateJSON(bb, "binding.json"); e != nil {
			return invalid("usage.binding", e)
		}
		var b usage.Binding
		if e = usage.Decode(bb, &b); e != nil {
			return invalid("usage.binding", e)
		}
		if e = usage.ValidateBinding(b); e != nil {
			return invalid("usage.binding", e)
		}
		if op == "collect" {
			if b.Host != values["--host"] {
				return invalid("usage.binding", fmt.Errorf("host does not match binding"))
			}
			if b.Host == "codex" && services.CodexQuotaTransport != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				transport, err := services.CodexQuotaTransport(ctx, b, p)
				if err != nil || transport == nil {
					return invalid("collector.transport", fmt.Errorf("qualified exact-account transport unavailable"))
				}
				export, report := collectors.ReadCodexRateLimits(ctx, transport, b, p, time.Now().UTC())
				if report.Status != "valid" {
					return write(report, 1)
				}
				export.PolicySHA256 = usage.Hash(pb)
				if err = usage.ValidateExport(export, p); err != nil {
					return invalid("usage.export", err)
				}
				files, err := usage.Files(export)
				if err != nil {
					return invalid("usage.export", err)
				}
				files["collection-report.json"] = mustPhase8JSON(report)
				if err = evidence.WriteFiles(values["--out"], files, values["--binding"], values["--policy"]); err != nil {
					return invalid("usage.output", err)
				}
				return write(report, 0)
			}
			r := usage.BaseReport()
			r.Status = "blocked"
			r.Diagnostics = append(r.Diagnostics, usage.Diagnostic{Code: "collector.transport.pending", Message: "No authorized read-only transport is configured by this CLI. Use offline import or the injected runtime collector interface; no collection was performed and no output bundle was written."})
			return write(r, 1)
		}
		input, e := modelcatalog.ReadFile(values["--input"])
		if e != nil {
			return invalid("usage.input", e)
		}
		var revision int64
		if values["--stream-revision"] != "" {
			var e error
			revision, e = strconv.ParseInt(values["--stream-revision"], 10, 64)
			if e != nil || revision <= 0 {
				return 2, nil
			}
		}
		export, report := collectors.NormalizeCapture(values["--source"], input, b, p, at, revision)
		if report.Status == "invalid" || report.Status == "conflict" {
			return write(report, 1)
		}
		if len(export.Bindings) == 0 {
			return write(report, 0)
		}
		export.PolicySHA256 = usage.Hash(pb)
		if e = usage.ValidateExport(export, p); e != nil {
			return invalid("usage.export", e)
		}
		files, e := usage.Files(export)
		if e != nil {
			return invalid("usage.export", e)
		}
		reportBytes, _ := json.MarshalIndent(report, "", "  ")
		files["collection-report.json"] = append(reportBytes, '\n')
		if e = evidence.WriteFiles(values["--out"], files, values["--input"], values["--binding"], values["--policy"]); e != nil {
			return invalid("usage.output", e)
		}
		return write(report, 0)
	}
	if op == "replay" {
		mb, e := modelcatalog.ReadFile(values["--manifest"])
		if e != nil {
			return invalid("usage.manifest", e)
		}
		var manifest struct {
			Version string        `json:"version"`
			Inputs  []usage.Asset `json:"inputs"`
		}
		if e = usage.Decode(mb, &manifest); e != nil {
			return invalid("usage.manifest", e)
		}
		if manifest.Version != usage.Version || len(manifest.Inputs) == 0 || len(manifest.Inputs) > 128 {
			return invalid("usage.manifest", fmt.Errorf("unsupported version or input count"))
		}
		merged := usage.EmptyExport()
		merged.PolicySHA256 = usage.Hash(pb)
		bindings := map[string]usage.Binding{}
		quotas := map[string]usage.Quota{}
		charges := map[string]usage.Charge{}
		reservations := map[string]usage.Reservation{}
		merge := func(id string, v any, m map[string]string) error {
			d := usage.Digest(v)
			if old, ok := m[id]; ok && old != d {
				return fmt.Errorf("conflicting replay identity")
			}
			m[id] = d
			return nil
		}
		keys := map[string]string{}
		for _, input := range manifest.Inputs {
			raw, e := modelcatalog.ReadAsset(filepath.Dir(values["--manifest"]), input.Path)
			if e != nil {
				return invalid("usage.replay", e)
			}
			if usage.Hash(raw) != input.SHA256 {
				return invalid("usage.replay", fmt.Errorf("input hash changed"))
			}
			ex, _, e := usage.Load(filepath.Join(filepath.Dir(values["--manifest"]), filepath.FromSlash(input.Path)), values["--policy"])
			if e != nil {
				return invalid("usage.replay", e)
			}
			if len(ex.Evidence) > 0 {
				return invalid("usage.replay", fmt.Errorf("replay requires self-contained normalized exports without auxiliary assets"))
			}
			for _, b := range ex.Bindings {
				if e = merge("binding:"+b.ID, b, keys); e != nil {
					return invalid("usage.replay", e)
				}
				bindings[b.ID] = b
			}
			for _, q := range ex.Quotas {
				if e = merge("quota:"+q.ID, q, keys); e != nil {
					return invalid("usage.replay", e)
				}
				quotas[q.ID] = q
			}
			for _, ch := range ex.Charges {
				if e = merge("charge:"+ch.ID, ch, keys); e != nil {
					return invalid("usage.replay", e)
				}
				charges[ch.ID] = ch
			}
			for _, r := range ex.Reservations {
				if e = merge("reservation:"+r.ID, r, keys); e != nil {
					return invalid("usage.replay", e)
				}
				reservations[r.ID] = r
			}
			merged.Events = append(merged.Events, ex.Events...)
		}
		for _, b := range bindings {
			merged.Bindings = append(merged.Bindings, b)
		}
		for _, q := range quotas {
			merged.Quotas = append(merged.Quotas, q)
		}
		for _, ch := range charges {
			merged.Charges = append(merged.Charges, ch)
		}
		for _, r := range reservations {
			merged.Reservations = append(merged.Reservations, r)
		}
		var ds []usage.Diagnostic
		merged.Events, ds = usage.NormalizeEvents(merged.Events)
		if len(ds) > 0 {
			return invalid("usage.replay", fmt.Errorf("conflicting usage events"))
		}
		sortUsageExport(&merged)
		if e = usage.ValidateExport(merged, p); e != nil {
			return invalid("usage.replay", e)
		}
		files, e := usage.Files(merged)
		if e != nil {
			return invalid("usage.replay", e)
		}
		summary := usage.SummaryFor(merged, p, at, []string{"host", "model", "pool"})
		b, _ := json.MarshalIndent(summary, "", "  ")
		files["replay-report.json"] = append(b, '\n')
		if e = evidence.WriteFiles(values["--out"], files, filepath.Dir(values["--manifest"]), values["--policy"]); e != nil {
			return invalid("usage.output", e)
		}
		return write(summary, usageReportExit(summary.Report))
	}
	ex, p, err := usage.Load(pos[0], values["--policy"])
	if err != nil {
		return invalid("usage.export", err)
	}
	if op == "validate" {
		return write(usage.BaseReport(), 0)
	}
	if op == "summary" {
		groups := []string{"host", "model", "pool"}
		if values["--group-by"] != "" {
			groups = strings.Split(values["--group-by"], ",")
		}
		seen := map[string]bool{}
		for _, g := range groups {
			ok := false
			for _, known := range []string{"host", "provider", "model", "configuration", "pool", "project", "work-item", "run", "attempt", "parent", "child"} {
				if g == known {
					ok = true
				}
			}
			if !ok || seen[g] {
				return 2, nil
			}
			seen[g] = true
		}
		report := usage.SummaryFor(ex, p, at, groups)
		return write(report, usageReportExit(report.Report))
	}
	catalog, cr := modelcatalog.Load(values["--catalog"], values["--catalog-policy"])
	if cr.Status != "valid" {
		return invalid("usage.catalog", fmt.Errorf("catalog validation failed"))
	}
	profileBytes, e := modelcatalog.ReadFile(values["--profile"])
	if e != nil {
		return invalid("usage.profile", e)
	}
	parsed, ds, parseErr := contracts.Parse(profileBytes)
	if parseErr != nil || len(ds) > 0 {
		return invalid("usage.profile", fmt.Errorf("invalid execution profile"))
	}
	resource := parsed.(contracts.Resource)
	if resource.Kind != "ExecutionProfile" {
		return invalid("usage.profile", fmt.Errorf("expected execution profile"))
	}
	typed, e := contracts.DecodeTyped(resource)
	if e != nil {
		return invalid("usage.profile", e)
	}
	profile, ok := typed.(contracts.ExecutionProfile)
	if !ok {
		return invalid("usage.profile", fmt.Errorf("expected execution profile"))
	}
	var demand *usage.Demand
	config := ""
	if values["--demand"] != "" {
		raw, e := modelcatalog.ReadFile(values["--demand"])
		if e != nil {
			return invalid("usage.demand", e)
		}
		demand = &usage.Demand{}
		if e = usage.ValidateJSON(raw, "demand.json"); e != nil {
			return invalid("usage.demand", e)
		}
		if e = usage.Decode(raw, demand); e != nil {
			return invalid("usage.demand", e)
		}
		if e = usage.ValidateDemand(*demand); e != nil {
			return invalid("usage.demand", e)
		}
		if demand.ProfileSHA256 != usage.Hash(profileBytes) {
			return invalid("usage.demand", fmt.Errorf("demand does not bind exact profile bytes"))
		}
		config = demand.ConfigurationID
	}
	profileMatch := "unknown"
	if config != "" {
		found := false
		for _, record := range catalog.Catalog.Configurations {
			if record.ID == config {
				found = true
				profileMatch = usageProfileMatch(record.Identity, profile)
				if profileMatch == "unsupported" {
					return invalid("usage.configuration", fmt.Errorf("catalog configuration contradicts execution profile"))
				}
			}
		}
		if !found {
			return invalid("usage.configuration", fmt.Errorf("configuration is not in the supplied catalog"))
		}
	}
	cap := usage.Capacity(ex, p, config, demand, at)
	if profileMatch != "supported" {
		cap.Disposition = "unknown"
		cap.Diagnostics = append(cap.Diagnostics, usage.Diagnostic{Code: "usage.profile-binding.unknown", Message: "catalog identity does not establish an exact profile binding"})
		cap.Digest = ""
		cap.Digest = usage.Digest(cap)
	}
	check := modelcatalog.CheckWithProfileDigest(catalog, profile, usage.Hash(profileBytes), at)
	result := struct {
		usage.CapacitySnapshot
		CatalogSHA256       string                   `json:"catalogSha256"`
		CatalogPolicySHA256 string                   `json:"catalogPolicySha256"`
		ProfileSHA256       string                   `json:"profileSha256"`
		Compatibility       modelcatalog.CheckReport `json:"compatibility"`
		ReportDigest        string                   `json:"reportDigest"`
	}{cap, catalog.CatalogSHA256, catalog.PolicySHA256, usage.Hash(profileBytes), check, ""}
	result.ReportDigest = usage.Digest(result)
	code := usageReportExit(cap.Report)
	if requireFit && (cap.Disposition != "observed-fit" || check.Compatibility == "unsupported" || check.Status == "invalid") {
		code = 1
	}
	return write(result, code)
}

// Only compare dimensions represented by ExecutionProfile. Unknown identity or
// uninterpreted host constraints cannot establish a matching executable route.
func usageProfileMatch(c modelcatalog.Configuration, p contracts.ExecutionProfile) string {
	status := "supported"
	compare := func(a, b *string) {
		if a == nil || b == nil {
			if a != nil || b != nil {
				if status != "unsupported" {
					status = "unknown"
				}
			}
			return
		}
		if *a != *b {
			status = "unsupported"
		}
	}
	compare(c.Host, &p.Client.Adapter)
	compare(c.ModelID, &p.Model.Selector)
	compare(c.Provider, p.Model.Provider)
	compare(c.ReasoningMode, &p.Model.Reasoning.Mode)
	compare(c.Effort, p.Model.Reasoning.Effort)
	if p.Client.VersionConstraint != nil {
		if strings.ContainsAny(*p.Client.VersionConstraint, "<>=*|^~ ,") {
			if status != "unsupported" {
				status = "unknown"
			}
		} else {
			compare(c.HostVersion, p.Client.VersionConstraint)
		}
	}
	if usage.Digest(c.ReasoningBudgetTokens) != usage.Digest(p.Model.Reasoning.BudgetTokens) || usage.Digest(c.Generation) != usage.Digest(p.Model.Generation) {
		if status != "unsupported" {
			status = "unknown"
		}
	}
	return status
}
func usageReportExit(r usage.Report) int {
	if r.Status != "valid" {
		return 1
	}
	return 0
}
