package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/methodology"
)

const methodologyMarker = "<!-- AOK methodology scaffold. Edit freely; re-scaffold requires --force. -->"

type methodProfile map[string]any

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func arr(v any) []any  { a, _ := v.([]any); return a }
func str(v any) string { s, _ := v.(string); return s }
func readJSON(path string) (map[string]any, error) {
	return strictReadJSON(path)
}
func loadMethodProfiles(root string) ([]methodProfile, error) {
	dir := filepath.Join(root, "methodologies", "profiles")
	files, e := filepath.Glob(filepath.Join(dir, "*.json"))
	if e != nil {
		return nil, e
	}
	out := []methodProfile{}
	seen := map[string]bool{}
	for _, f := range files {
		m, e := readJSON(f)
		if e != nil {
			return nil, e
		}
		id := str(m["id"])
		if id == "" || seen[id] {
			return nil, fmt.Errorf("invalid or duplicate methodology profile ID: %s", id)
		}
		seen[id] = true
		out = append(out, methodProfile(m))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("Methodology profile directory is missing: %s", dir)
	}
	sort.Slice(out, func(i, j int) bool {
		ki, kj := 0, 0
		if str(out[i]["kind"]) != "flow" {
			ki = 1
		}
		if str(out[j]["kind"]) != "flow" {
			kj = 1
		}
		if ki != kj {
			return ki < kj
		}
		oi, _ := out[i]["order"].(float64)
		oj, _ := out[j]["order"].(float64)
		if oi != oj {
			return oi < oj
		}
		return str(out[i]["id"]) < str(out[j]["id"])
	})
	return out, nil
}
func methodCatalog(root string, p methodProfile, key, col string) ([]any, error) {
	rel := str(p[key])
	if rel == "" {
		return nil, nil
	}
	base := filepath.Join(root, "methodologies")
	path := filepath.Clean(filepath.Join(base, rel))
	if !within(base, path) {
		return nil, fmt.Errorf("Methodology catalog escapes the catalog root: %s", rel)
	}
	m, e := readJSON(path)
	if e != nil {
		return nil, e
	}
	a, ok := m[col].([]any)
	if !ok {
		return nil, fmt.Errorf("Methodology catalog must contain an object array %q: %s", col, path)
	}
	return a, nil
}
func within(base, path string) bool {
	r, e := filepath.Rel(base, path)
	return e == nil && r != ".." && !strings.HasPrefix(r, ".."+string(os.PathSeparator))
}
func profileContract(root string, p methodProfile) (map[string]any, error) {
	r, e := methodCatalog(root, p, "roleCatalog", "roles")
	if e != nil {
		return nil, e
	}
	a, e := methodCatalog(root, p, "artifactCatalog", "artifacts")
	if e != nil {
		return nil, e
	}
	defs := []any{}
	for _, key := range []string{"featureContract", "requirementContract", "iterationContract"} {
		var v any
		if rel := str(p[key]); rel != "" {
			path := filepath.Clean(filepath.Join(root, "methodologies", rel))
			if !within(filepath.Join(root, "methodologies"), path) {
				return nil, fmt.Errorf("Methodology contract escapes root")
			}
			v, e = readJSON(path)
			if e != nil {
				return nil, e
			}
		}
		defs = append(defs, v)
	}
	c := map[string]any{}
	for k, v := range p {
		c[k] = v
	}
	c["roles"] = r
	c["artifacts"] = a
	c["featureDefinition"] = defs[0]
	c["requirementDefinition"] = defs[1]
	c["iterationDefinition"] = defs[2]
	life := obj(p["lifecycle"])
	c["catalogCounts"] = map[string]int{"stages": len(arr(life["stages"])), "roles": len(r), "artifacts": len(a), "ceremonies": len(arr(p["ceremonies"])), "gates": len(arr(p["gates"]))}
	return c, nil
}

type MethodologyLifecycleServices struct {
	Snapshot       func(context.Context, string) (methodology.Snapshot, error)
	Keys           methodology.KeyResolver
	VerifyEvidence methodology.EvidenceVerifier
	Record         func(context.Context, methodology.Profile, methodology.Transition, methodology.KeyResolver, time.Time) (methodology.Snapshot, error)
	Now            func() time.Time
}

func runMethodology(args []string, stdout, stderr io.Writer) (int, error) {
	return runMethodologyWithServices(context.Background(), args, nil, stdout, stderr)
}

func runMethodologyWithServices(ctx context.Context, args []string, services *MethodologyLifecycleServices, stdout, stderr io.Writer) (int, error) {
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "Usage: aok methodology list [--json]\n       aok methodology scaffold <project> --flow <flow> [--force] [--json]\n       aok methodology validate <project> [--json]")
	}
	if len(args) == 0 || args[0] == "--help" || args[0] == "help" {
		usage(stdout)
		return 0, nil
	}
	root, e := requireRoot()
	if e != nil {
		return 1, e
	}
	profiles, e := loadMethodProfiles(root)
	if e != nil {
		return 1, e
	}
	switch args[0] {
	case "check-transition", "record-transition":
		return runMethodologyTransition(args[0], args[1:], root, ctx, services, stdout, stderr)
	case "validate-feature", "validate-requirement", "validate-iteration":
		return runMethodologyContractValidator(args[0], args[1:], root, stdout, stderr)
	case "list":
		jsonOut := false
		for _, a := range args[1:] {
			if a == "--json" {
				jsonOut = true
			} else {
				return 2, nil
			}
		}
		if jsonOut {
			cs := []any{}
			for _, p := range profiles {
				c, e := profileContract(root, p)
				if e != nil {
					return 1, e
				}
				cs = append(cs, c)
			}
			return 0, json.NewEncoder(stdout).Encode(map[string]any{"profiles": cs})
		}
		for _, p := range profiles {
			c, e := profileContract(root, p)
			if e != nil {
				return 1, e
			}
			counts := obj(c["catalogCounts"])
			detail := ""
			sum := 0
			for _, k := range []string{"stages", "roles", "artifacts", "ceremonies", "gates"} {
				n, _ := counts[k].(int)
				sum += n
			}
			if sum > 0 {
				detail = fmt.Sprintf("; %v stages, %v roles, %v artifacts", counts["stages"], counts["roles"], counts["artifacts"])
			}
			fmt.Fprintf(stdout, "%s: %s (%s) - %s%s.\n", p["id"], p["kind"], p["supportLevel"], strings.TrimRight(str(p["description"]), "."), detail)
		}
		return 0, nil
	case "scaffold":
		return scaffoldMethodology(args[1:], root, profiles, stdout, stderr)
	case "validate":
		if len(args) < 2 {
			return 2, nil
		}
		return runMethodologyProjectValidate(args[1:], root, stdout, stderr)
	default:
		usage(stderr)
		return 2, nil
	}
}

func runMethodologyTransition(command string, args []string, root string, ctx context.Context, s *MethodologyLifecycleServices, out, errw io.Writer) (int, error) {
	usage := func(w io.Writer) { fmt.Fprintf(w, "Usage: aok methodology %s --transition FILE [--json]\n", command) }
	if s == nil || s.Keys == nil || s.VerifyEvidence == nil {
		return 1, fmt.Errorf("methodology lifecycle services require trusted role keys and evidence verification")
	}
	path := ""
	jsonOut := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--transition":
			if path != "" || i+1 >= len(args) {
				usage(errw)
				return 2, nil
			}
			i++
			path = args[i]
		case "--json":
			jsonOut = true
		case "-h", "--help":
			usage(out)
			return 0, nil
		default:
			usage(errw)
			return 2, nil
		}
	}
	if path == "" {
		usage(errw)
		return 2, nil
	}
	b, e := strictReadFile(path)
	if e != nil {
		return 1, e
	}
	var tr methodology.Transition
	if e = strictDecodeJSON(b, &tr); e != nil {
		return 1, e
	}
	p, e := lifecycleProfile(root, tr.Profile, s.VerifyEvidence)
	if e != nil {
		return 1, e
	}
	var current methodology.Snapshot
	if command == "check-transition" {
		if s.Snapshot == nil {
			return 1, fmt.Errorf("methodology lifecycle snapshot service is required")
		}
		current, e = s.Snapshot(ctx, tr.Subject)
		if e == nil {
			e = methodology.CheckTransition(p, current, tr, s.Keys)
		}
	} else {
		if s.Record == nil {
			return 1, fmt.Errorf("P7 methodology journal service is required")
		}
		now := time.Now()
		if s.Now != nil {
			now = s.Now()
		}
		current, e = s.Record(ctx, p, tr, s.Keys, now)
	}
	valid := e == nil
	report := map[string]any{"kind": "aok-methodology-transition-validation", "valid": valid, "transitionId": tr.ID, "subject": tr.Subject, "profile": tr.Profile, "from": tr.From, "to": tr.To, "currentRevision": current.Revision}
	if !valid {
		report["error"] = e.Error()
	}
	if jsonOut {
		if x := json.NewEncoder(out).Encode(report); x != nil {
			return 1, x
		}
	} else {
		label := "failed"
		if valid {
			label = "passed"
		}
		fmt.Fprintf(out, "Methodology transition %s: %s\n", command, label)
		if !valid {
			fmt.Fprintf(out, "- %s\n", e.Error())
		}
	}
	if !valid {
		return 1, nil
	}
	return 0, nil
}

func lifecycleProfile(root, id string, verify methodology.EvidenceVerifier) (methodology.Profile, error) {
	ps, e := loadMethodProfiles(root)
	if e != nil {
		return methodology.Profile{}, e
	}
	var raw methodProfile
	for _, p := range ps {
		if str(p["id"]) == id {
			raw = p
		}
	}
	if raw == nil {
		return methodology.Profile{}, fmt.Errorf("unknown methodology profile: %s", id)
	}
	stages := []string{}
	for _, v := range arr(obj(raw["lifecycle"])["stages"]) {
		if x := str(obj(v)["id"]); x != "" {
			stages = append(stages, x)
		}
	}
	transitions := []methodology.Edge{}
	for _, v := range arr(obj(raw["lifecycle"])["transitions"]) {
		x := obj(v)
		transitions = append(transitions, methodology.Edge{From: str(x["from"]), To: str(x["to"])})
	}
	gates := map[string][]string{}
	roles := map[string][]string{}
	for _, v := range arr(raw["gates"]) {
		g := obj(v)
		key := str(g["from"]) + "\x00" + str(g["to"])
		gates[key] = append(gates[key], str(g["id"]))
		for _, r := range arr(g["requiredRoles"]) {
			roles[key] = append(roles[key], str(r))
		}
	}
	return methodology.Profile{ID: id, Stages: stages, Gates: gates, Transitions: transitions, AllowedRoles: roles, VerifyEvidence: verify}, nil
}

func scaffoldMethodology(args []string, root string, ps []methodProfile, out, errw io.Writer) (int, error) {
	if len(args) < 1 {
		return 2, nil
	}
	project, e := filepath.Abs(args[0])
	if e != nil {
		return 1, e
	}
	flow := ""
	force, jsonOut := false, false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--flow":
			if flow != "" || i+1 >= len(args) {
				return 2, nil
			}
			i++
			flow = args[i]
		case "--force":
			force = true
		case "--json":
			jsonOut = true
		default:
			fmt.Fprintln(errw, "invalid methodology scaffold arguments")
			return 2, nil
		}
	}
	if flow == "" {
		return 2, nil
	}
	st, e := os.Stat(project)
	if e != nil || !st.IsDir() {
		return 1, fmt.Errorf("Target is not a directory: %s", project)
	}
	var p methodProfile
	for _, v := range ps {
		if str(v["id"]) == flow {
			p = v
			break
		}
	}
	if p == nil {
		return 1, fmt.Errorf("Unknown methodology flow: %s", flow)
	}
	if str(p["kind"]) != "flow" {
		return 1, fmt.Errorf("Methodology profile %q is not a flow", flow)
	}
	if str(p["supportLevel"]) != "scaffolded" {
		return 1, fmt.Errorf("Methodology flow %q is %s; only scaffolded flows can be exported", flow, p["supportLevel"])
	}
	roles, e := methodCatalog(root, p, "roleCatalog", "roles")
	if e != nil {
		return 1, e
	}
	arts, e := methodCatalog(root, p, "artifactCatalog", "artifacts")
	if e != nil {
		return 1, e
	}
	assets := map[string]string{}
	for _, v := range roles {
		r := obj(v)
		target := str(obj(obj(r["targets"])["codex"])["path"])
		if target == "" {
			return 1, fmt.Errorf("Methodology role %q has no Codex output path", r["name"])
		}
		assets[target] = renderMethodRole(r, flow)
	}
	for _, v := range arts {
		a := obj(v)
		target := str(obj(a["template"])["outputPath"])
		if target == "" {
			return 1, fmt.Errorf("Methodology artifact %q has no output path", a["id"])
		}
		content := ""
		source := str(obj(a["template"])["sourcePath"])
		if source != "" {
			path := filepath.Clean(filepath.Join(root, "methodologies", source))
			if !within(filepath.Join(root, "methodologies"), path) {
				return 1, fmt.Errorf("Methodology artifact template escapes the methodology root")
			}
			b, e := strictReadFile(path)
			if e != nil {
				return 1, e
			}
			content = methodologyMarker + "\n" + strings.TrimSpace(string(b)) + "\n"
		} else {
			content = renderMethodArtifact(a, flow)
		}
		assets[target] = content
	}
	assets[filepath.Join("docs", "methodology", flow, "README.md")] = renderMethodReadme(p, roles, arts)
	keys := make([]string, 0, len(assets))
	for k := range assets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	conflicts := []string{}
	for _, k := range keys {
		dest := filepath.Join(project, k)
		if !within(project, dest) {
			return 1, fmt.Errorf("Methodology scaffold output escapes target: %s", k)
		}
		b, e := strictReadFile(dest)
		if e == nil && string(b) != assets[k] {
			conflicts = append(conflicts, k)
		} else if e != nil && !os.IsNotExist(e) {
			return 1, e
		}
	}
	if len(conflicts) > 0 && !force {
		return 1, fmt.Errorf("Refusing to overwrite modified or conflicting methodology scaffold files: %s; use --force to back them up", strings.Join(conflicts, ", "))
	}
	stamp := time.Now().UTC().Format("20060102150405")
	changes := []map[string]string{}
	for _, k := range keys {
		dest := filepath.Join(project, k)
		old, e := strictReadFile(dest)
		if e == nil && string(old) == assets[k] {
			changes = append(changes, map[string]string{"path": filepath.ToSlash(k), "status": "unchanged"})
			continue
		}
		c := map[string]string{"path": filepath.ToSlash(k), "status": "created"}
		if e == nil {
			backup := filepath.Join(project, ".aok-backups", stamp, k)
			if e = os.MkdirAll(filepath.Dir(backup), 0755); e != nil {
				return 1, e
			}
			if e = os.WriteFile(backup, old, 0644); e != nil {
				return 1, e
			}
			c["status"] = "updated"
			c["backup"] = filepath.ToSlash(strings.TrimPrefix(backup, project+string(os.PathSeparator)))
		}
		if e = os.MkdirAll(filepath.Dir(dest), 0755); e != nil {
			return 1, e
		}
		if e = os.WriteFile(dest, []byte(assets[k]), 0644); e != nil {
			return 1, e
		}
		changes = append(changes, c)
	}
	c, e := profileContract(root, p)
	if e != nil {
		return 1, e
	}
	result := map[string]any{"kind": "aok-methodology-scaffold", "projectPath": project, "flow": flow, "supportLevel": p["supportLevel"], "selectionChanged": false, "lifecycleStateChanged": false, "counts": c["catalogCounts"], "files": changes}
	if jsonOut {
		return 0, json.NewEncoder(out).Encode(result)
	}
	fmt.Fprintf(out, "Scaffolded %s for %s\n- methodology selection: unchanged\n- lifecycle state: unchanged\n", p["displayName"], project)
	for _, v := range changes {
		extra := ""
		if b := v["backup"]; b != "" {
			extra = "; backup " + b
		}
		fmt.Fprintf(out, "- %s %s%s\n", v["status"], v["path"], extra)
	}
	return 0, nil
}

func renderMethodRole(r map[string]any, flow string) string {
	assignment := str(r["assignmentMode"])
	if assignment == "" {
		assignment = "either"
	}
	title := str(r["callsign"])
	if title == "" {
		title = str(r["name"])
	}
	title = strings.Title(strings.ReplaceAll(title, "-", " "))
	lines := []string{methodologyMarker, "# " + title, "", "Flow: `" + flow + "`", "Role ID: `" + str(r["name"]) + "`", "Assignment: `" + assignment + "`", "", "## Mission", "", str(r["description"]), "", "## Responsibilities", ""}
	for _, v := range arr(r["responsibilities"]) {
		lines = append(lines, "- "+str(v))
	}
	for _, pair := range [][2]string{{"Capabilities", "capabilities"}, {"Escalation", "escalationRules"}} {
		a := arr(r[pair[1]])
		if len(a) > 0 {
			lines = append(lines, "", "## "+pair[0], "")
			for _, v := range a {
				lines = append(lines, "- "+str(v))
			}
		}
	}
	if assignment == "human" {
		lines = append(lines, "", "## Authority Guardrail", "", "This role requires a named human. Agents may prepare evidence and recommendations but must not assume its approval authority.")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}
func renderMethodArtifact(a map[string]any, flow string) string {
	owners := []string{}
	for _, x := range arr(a["owners"]) {
		owners = append(owners, "`"+str(x)+"`")
	}
	approvers := []string{}
	for _, x := range arr(a["approvers"]) {
		approvers = append(approvers, "`"+str(x)+"`")
	}
	stages := []string{}
	for _, x := range arr(a["lifecycleStages"]) {
		stages = append(stages, "`"+str(x)+"`")
	}
	lines := []string{methodologyMarker, "# " + str(a["title"]), "", "Flow: `" + flow + "`", "", "Requirement: `" + str(a["requirement"]) + "`", "", "Evolution: `" + str(a["evolution"]) + "`", "", "Owners: " + strings.Join(owners, ", "), "Approvers: " + strings.Join(approvers, ", "), "Lifecycle stages: " + strings.Join(stages, ", "), "", str(a["description"])}
	for _, x := range arr(a["sections"]) {
		lines = append(lines, "", "## "+str(x), "", "<!-- Complete this section for the project. -->")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}
func renderMethodReadme(p methodProfile, roles, arts []any) string {
	life := obj(p["lifecycle"])
	l := []string{methodologyMarker, "# " + str(p["displayName"]) + " Delivery Scaffold", "", "> This flow is scaffolded, not active. These files do not select the flow, enforce gates, or change project lifecycle state.", "", str(p["description"]), "", "## Lifecycle", ""}
	for _, v := range arr(life["stages"]) {
		s := obj(v)
		repeat := ""
		if s["repeatable"] == true {
			repeat = "; repeatable"
		}
		ph := []string{}
		for _, x := range arr(s["allowedPhases"]) {
			ph = append(ph, str(x))
		}
		l = append(l, fmt.Sprintf("- `%s` — %s (%s%s); neutral phases: %s", s["id"], s["name"], s["scope"], repeat, strings.Join(ph, ", ")))
	}
	l = append(l, "", "## Roles", "")
	for _, v := range roles {
		r := obj(v)
		l = append(l, fmt.Sprintf("- `%s` (%s): %s", r["name"], r["assignmentMode"], r["description"]))
	}
	l = append(l, "", "## Artifacts", "")
	for _, v := range arts {
		a := obj(v)
		l = append(l, fmt.Sprintf("- `%s` (%s): %s", a["id"], a["requirement"], a["description"]))
	}
	l = append(l, "", "## Ceremonies", "")
	for _, v := range arr(p["ceremonies"]) {
		c := obj(v)
		cp := ""
		if c["humanCheckpoint"] == true {
			cp = "; human checkpoint"
		}
		l = append(l, fmt.Sprintf("- `%s` — %s (%s%s)", c["id"], c["name"], c["trigger"], cp))
	}
	l = append(l, "", "## Gates", "")
	for _, v := range arr(p["gates"]) {
		g := obj(v)
		approval := ""
		if g["humanApproval"] == true {
			approval = "; human approval"
		}
		l = append(l, fmt.Sprintf("- `%s`: `%s` → `%s` (%s%s)", g["id"], g["from"], g["to"], g["severity"], approval))
	}
	return strings.TrimRight(strings.Join(l, "\n"), "\n") + "\n"
}

func runMethodologyContractValidator(kind string, args []string, root string, out, errw io.Writer) (int, error) {
	values := map[string][]string{}
	jsonOut := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--json" {
			jsonOut = true
			continue
		}
		if !strings.HasPrefix(a, "--") || i+1 >= len(args) {
			fmt.Fprintln(errw, "invalid methodology validator arguments")
			return 2, nil
		}
		values[a] = append(values[a], args[i+1])
		i++
	}
	need := func(name string) string {
		v := values[name]
		if len(v) == 0 {
			return ""
		}
		return v[len(v)-1]
	}
	var report map[string]any
	var e error
	switch kind {
	case "validate-feature":
		if len(values["--name"]) != 1 {
			return 2, nil
		}
		report, e = validateFeatureName(root, need("--name"))
	case "validate-requirement":
		if len(values["--id"]) != 1 || len(values["--outcome"]) != 1 || len(values["--project-moscow"]) != 1 || len(values["--acceptance"]) == 0 {
			return 2, nil
		}
		report, e = validateRequirement(root, values)
	case "validate-iteration":
		if len(values["--id"]) != 1 || len(values["--stage"]) != 1 || len(values["--objective"]) != 1 {
			return 2, nil
		}
		report, e = validateIteration(root, values)
	}
	if e != nil {
		return 1, e
	}
	if jsonOut {
		if e = json.NewEncoder(out).Encode(report); e != nil {
			return 1, e
		}
	} else {
		renderMethodValidation(kind, report, out)
	}
	if report["valid"] != true {
		return 1, nil
	}
	return 0, nil
}

func loadMethodContract(root, profileID, key string) (map[string]any, error) {
	ps, e := loadMethodProfiles(root)
	if e != nil {
		return nil, e
	}
	var p methodProfile
	for _, v := range ps {
		if str(v["id"]) == profileID {
			p = v
		}
	}
	if p == nil {
		return nil, fmt.Errorf("%s methodology profile is missing", strings.ToUpper(profileID))
	}
	rel := str(p[key])
	if rel == "" {
		return nil, fmt.Errorf("%s methodology contract is missing", strings.ToUpper(profileID))
	}
	path := filepath.Clean(filepath.Join(root, "methodologies", rel))
	if !within(filepath.Join(root, "methodologies"), path) {
		return nil, fmt.Errorf("Methodology contract escapes the methodology root")
	}
	return readJSON(path)
}
func normalizeWS(v string) string { return strings.Join(strings.Fields(v), " ") }
func validateFeatureName(root, name string) (map[string]any, error) {
	contract, e := loadMethodContract(root, "fdd", "featureContract")
	if e != nil {
		return nil, e
	}
	naming := obj(contract["naming"])
	preps := []string{}
	for _, v := range arr(naming["prepositions"]) {
		preps = append(preps, regexp.QuoteMeta(str(v)))
	}
	if len(preps) == 0 {
		return nil, fmt.Errorf("FDD feature contract has no naming prepositions")
	}
	pre := strings.Join(preps, "|")
	classic := regexp.MustCompile(`(?i)^(\S+)\s+the\s+(.+?)\s+(` + pre + `)\s+(a|an)\s+(.+)$`)
	short := regexp.MustCompile(`(?i)^(\S+)\s+(.+?)\s+(` + pre + `)\s+(.+)$`)
	normalized := normalizeWS(name)
	cm, sm := classic.FindStringSubmatch(normalized), short.FindStringSubmatch(normalized)
	errs := []any{}
	if cm == nil && sm != nil && (strings.HasPrefix(strings.ToLower(sm[2]), "the ") || strings.HasPrefix(strings.ToLower(sm[4]), "a ") || strings.HasPrefix(strings.ToLower(sm[4]), "an ")) {
		errs = append(errs, "feature name has ambiguous article structure; use either the short form without articles or the complete classic form")
		return map[string]any{"kind": "aok-fdd-feature-name-validation", "valid": false, "inputName": name, "normalizedName": normalized, "style": nil, "canonicalName": nil, "parts": nil, "errors": errs}, nil
	}
	m := cm
	style := "classic"
	if m == nil {
		m = sm
		style = "short"
	}
	if normalized == "" {
		errs = append(errs, "feature name is empty")
	} else if m == nil {
		errs = append(errs, "feature name must match '<action> <result> <by|for|of|to> <object>' or the accepted classic article form")
	}
	if m == nil {
		return map[string]any{"kind": "aok-fdd-feature-name-validation", "valid": false, "inputName": name, "normalizedName": normalized, "style": nil, "canonicalName": nil, "parts": nil, "errors": errs}, nil
	}
	objectIndex := 4
	if style == "classic" {
		objectIndex = 5
	}
	parts := map[string]any{"action": m[1], "result": m[2], "preposition": strings.ToLower(m[3]), "object": m[objectIndex], "style": style}
	if style == "classic" {
		parts["article"] = strings.ToLower(m[4])
	}
	canonical := m[1] + " " + m[2] + " " + strings.ToLower(m[3]) + " " + m[objectIndex]
	return map[string]any{"kind": "aok-fdd-feature-name-validation", "valid": true, "inputName": name, "normalizedName": normalized, "style": style, "canonicalName": canonical, "parts": parts, "errors": errs}, nil
}

func validateRequirement(root string, v map[string][]string) (map[string]any, error) {
	c, e := loadMethodContract(root, "dsdm", "requirementContract")
	if e != nil {
		return nil, e
	}
	get := func(k string) string {
		if len(v[k]) == 0 {
			return ""
		}
		return v[k][0]
	}
	n := normalizeWS
	id, outcome, theme, increment, timebox := n(get("--id")), n(get("--outcome")), n(get("--theme")), n(get("--increment")), n(get("--timebox"))
	accept := []string{}
	for _, x := range v["--acceptance"] {
		accept = append(accept, n(x))
	}
	errs := []any{}
	pattern := str(obj(c["identifier"])["pattern"])
	if pattern == "" {
		return nil, fmt.Errorf("DSDM requirement contract has invalid identifier pattern")
	}
	re, e := regexp.Compile("^(?:" + pattern + ")$")
	if e != nil {
		return nil, e
	}
	if !re.MatchString(id) {
		errs = append(errs, "requirement id must use an uppercase namespace and numeric sequence (for example REQ-014)")
	}
	if outcome == "" {
		errs = append(errs, "requirement outcome must be non-empty")
	}
	if len(v["--theme"]) > 0 && theme == "" {
		errs = append(errs, "requirement theme must be non-empty when supplied")
	}
	if len(v["--increment"]) > 0 && increment == "" {
		errs = append(errs, "project increment must be non-empty when supplied")
	}
	if len(v["--timebox"]) > 0 && timebox == "" {
		errs = append(errs, "timebox must be non-empty when supplied")
	}
	if len(accept) == 0 {
		errs = append(errs, "at least one non-empty acceptance criterion is required")
	} else {
		seen := map[string]bool{}
		for _, a := range accept {
			if a == "" {
				errs = append(errs, "at least one non-empty acceptance criterion is required")
			}
			if seen[a] {
				errs = append(errs, "acceptance criteria must be unique after whitespace normalization")
				break
			}
			seen[a] = true
		}
	}
	validPriorities := []string{}
	for _, x := range arr(obj(c["moscow"])["values"]) {
		if s := str(obj(x)["id"]); s != "" {
			validPriorities = append(validPriorities, s)
		}
	}
	for _, key := range []string{"--project-moscow", "--increment-moscow", "--timebox-moscow"} {
		if len(v[key]) > 0 && !containsString(validPriorities, v[key][0]) {
			label := map[string]string{"--project-moscow": "project", "--increment-moscow": "project increment", "--timebox-moscow": "timebox"}[key]
			errs = append(errs, fmt.Sprintf("%s MoSCoW priority must be one of: %s", label, strings.Join(validPriorities, ", ")))
		}
	}
	if (increment != "") != (len(v["--increment-moscow"]) > 0) {
		errs = append(errs, "project increment and increment MoSCoW priority must be supplied together")
	}
	if (timebox != "") != (len(v["--timebox-moscow"]) > 0) {
		errs = append(errs, "timebox and timebox MoSCoW priority must be supplied together")
	}
	rq := map[string]any{"id": id, "outcome": outcome, "acceptanceCriteria": accept}
	if len(v["--theme"]) > 0 {
		rq["theme"] = theme
	}
	prior := map[string]any{"project": get("--project-moscow")}
	if increment != "" {
		prior["projectIncrement"] = map[string]any{"id": increment, "moscow": get("--increment-moscow")}
	}
	if timebox != "" {
		prior["timebox"] = map[string]any{"id": timebox, "moscow": get("--timebox-moscow")}
	}
	return map[string]any{"kind": "aok-dsdm-requirement-validation", "valid": len(errs) == 0, "requirement": rq, "priorities": prior, "errors": errs}, nil
}

func validateIteration(root string, v map[string][]string) (map[string]any, error) {
	c, e := loadMethodContract(root, "aup", "iterationContract")
	if e != nil {
		return nil, e
	}
	get := func(k string) string {
		if len(v[k]) == 0 {
			return ""
		}
		return v[k][0]
	}
	id, stage, objective := normalizeWS(get("--id")), strings.ToLower(normalizeWS(get("--stage"))), normalizeWS(get("--objective"))
	errs := []any{}
	pattern := str(obj(c["identifier"])["pattern"])
	re, e := regexp.Compile("^(?:" + pattern + ")$")
	if e != nil {
		return nil, e
	}
	if !re.MatchString(id) {
		errs = append(errs, "iteration id must use an uppercase namespace and numeric sequence (for example I-12)")
	}
	if objective == "" {
		errs = append(errs, "iteration objective must be non-empty")
	}
	milestones := arr(c["milestones"])
	var milestone map[string]any
	validStages := []string{}
	for _, x := range milestones {
		m := obj(x)
		s := str(m["lifecycleStage"])
		validStages = append(validStages, s)
		if s == stage {
			milestone = m
		}
	}
	if milestone == nil {
		errs = append(errs, "iteration stage must be one of: "+strings.Join(validStages, ", "))
	}
	workTypes := []string{}
	for _, x := range arr(obj(c["selectedWork"])["types"]) {
		workTypes = append(workTypes, str(x))
	}
	selected := []any{}
	seenIDs := map[string]bool{}
	for _, raw := range v["--work-item"] {
		at := strings.Index(raw, "=")
		if at < 0 {
			errs = append(errs, fmt.Sprintf("selected work item %q must use '<type>=<id>'", raw))
			continue
		}
		typ, wid := strings.ToLower(normalizeWS(raw[:at])), normalizeWS(raw[at+1:])
		if !containsString(workTypes, typ) {
			errs = append(errs, fmt.Sprintf("selected work type %s must be one of: %s", defaultString(typ, "<empty>"), strings.Join(workTypes, ", ")))
		}
		if !re.MatchString(wid) {
			errs = append(errs, fmt.Sprintf("selected work id %s must use an uppercase namespace and numeric sequence", defaultString(wid, "<empty>")))
		}
		if seenIDs[wid] {
			errs = append(errs, fmt.Sprintf("selected work id %q is duplicated", wid))
		}
		seenIDs[wid] = true
		selected = append(selected, map[string]string{"type": typ, "id": wid})
	}
	if len(v["--work-item"]) == 0 {
		errs = append(errs, "at least one selected work item is required")
	}
	disciplines := arr(c["disciplines"])
	byID := map[string]map[string]any{}
	order := []string{}
	for _, x := range disciplines {
		m := obj(x)
		d := str(m["id"])
		byID[d] = m
		order = append(order, d)
	}
	evidence := map[string]any{}
	seen := map[string]bool{}
	for _, raw := range v["--discipline"] {
		at := strings.Index(raw, "=")
		if at < 0 {
			errs = append(errs, fmt.Sprintf("discipline entry %q must use '<discipline>=<evidence>'", raw))
			continue
		}
		did, value := strings.ToLower(normalizeWS(raw[:at])), normalizeWS(raw[at+1:])
		d := byID[did]
		if d == nil {
			errs = append(errs, fmt.Sprintf("unknown discipline %s; expected: %s", defaultString(did, "<empty>"), strings.Join(order, ", ")))
			continue
		}
		if seen[did] {
			errs = append(errs, fmt.Sprintf("discipline %q is duplicated", did))
			continue
		}
		seen[did] = true
		field := str(d["taskField"])
		if strings.HasPrefix(value, "not-applicable:") {
			r := normalizeWS(strings.TrimPrefix(value, "not-applicable:"))
			if r == "" {
				errs = append(errs, fmt.Sprintf("discipline %q needs a non-empty not-applicable rationale", did))
			}
			evidence[field] = map[string]string{"notApplicableRationale": r}
		} else if strings.HasPrefix(value, "not-applicable") {
			errs = append(errs, fmt.Sprintf("discipline %q must use 'not-applicable:<rationale>'", did))
			evidence[field] = map[string]any{"evidence": []string{value}}
		} else if value == "" {
			errs = append(errs, fmt.Sprintf("discipline %q needs non-empty evidence", did))
			evidence[field] = map[string]any{"evidence": []string{}}
		} else {
			evidence[field] = map[string][]string{"evidence": {value}}
		}
	}
	missing := []string{}
	for _, d := range order {
		if !seen[d] {
			missing = append(missing, d)
		}
	}
	if len(missing) > 0 {
		errs = append(errs, "missing discipline evidence: "+strings.Join(missing, ", "))
	}
	orderedEvidence := map[string]any{}
	for _, x := range disciplines {
		field := str(obj(x)["taskField"])
		if val, ok := evidence[field]; ok {
			orderedEvidence[field] = val
		}
	}
	var mt any
	if milestone != nil {
		mt = milestone
	}
	return map[string]any{"kind": "aok-aup-iteration-validation", "valid": len(errs) == 0, "iteration": map[string]any{"id": id, "stage": stage, "objective": objective, "selectedWork": selected, "disciplineEvidence": orderedEvidence}, "milestoneTarget": mt, "errors": errs}, nil
}

func renderMethodValidation(kind string, r map[string]any, out io.Writer) {
	errs := []any{}
	for _, v := range arr(r["errors"]) {
		errs = append(errs, v)
	}
	status := "passed"
	if r["valid"] != true {
		status = "failed"
	}
	switch kind {
	case "validate-feature":
		fmt.Fprintf(out, "FDD feature name validation: %s\nInput: %s\nNormalized: %s\n", status, r["inputName"], defaultString(str(r["normalizedName"]), "<empty>"))
		if r["valid"] == true {
			p := obj(r["parts"])
			for _, k := range []string{"style"} {
				fmt.Fprintf(out, "Style: %s\n", p[k])
			}
			fmt.Fprintf(out, "Canonical short name: %s\nAction: %s\nResult: %s\nPreposition: %s\nObject: %s\n", r["canonicalName"], p["action"], p["result"], p["preposition"], p["object"])
		} else {
			for _, e := range errs {
				fmt.Fprintf(out, "- %v\n", e)
			}
		}
	case "validate-requirement":
		q := obj(r["requirement"])
		fmt.Fprintf(out, "DSDM requirement validation: %s\nRequirement ID: %s\nOutcome: %s\n", status, defaultString(str(q["id"]), "<empty>"), defaultString(str(q["outcome"]), "<empty>"))
		if q["theme"] != nil {
			fmt.Fprintf(out, "Theme: %s\n", defaultString(str(q["theme"]), "<empty>"))
		}
		fmt.Fprintf(out, "Project MoSCoW: %v\nAcceptance criteria:\n", obj(r["priorities"])["project"])
		for _, a := range arr(q["acceptanceCriteria"]) {
			fmt.Fprintf(out, "- %s\n", defaultString(str(a), "<empty>"))
		}
		for _, e := range errs {
			fmt.Fprintf(out, "- %v\n", e)
		}
	case "validate-iteration":
		i := obj(r["iteration"])
		fmt.Fprintf(out, "AUP iteration validation: %s\nIteration ID: %s\nStage: %s\nObjective: %s\n", status, defaultString(str(i["id"]), "<empty>"), defaultString(str(i["stage"]), "<empty>"), defaultString(str(i["objective"]), "<empty>"))
		mt := obj(r["milestoneTarget"])
		label := "<unknown>"
		if len(mt) > 0 {
			label = fmt.Sprintf("%v (%s)", mt["name"], strings.ToUpper(str(mt["id"])))
		}
		fmt.Fprintf(out, "Milestone target: %s\nSelected work:\n", label)
		for _, v := range arr(i["selectedWork"]) {
			w := obj(v)
			fmt.Fprintf(out, "- %s=%s\n", w["type"], w["id"])
		}
		fmt.Fprintln(out, "Discipline evidence:")
		for k, v := range obj(i["disciplineEvidence"]) {
			x := obj(v)
			if x["notApplicableRationale"] != nil {
				fmt.Fprintf(out, "- %s: not applicable — %v\n", k, x["notApplicableRationale"])
			} else {
				fmt.Fprintf(out, "- %s: %v\n", k, arr(x["evidence"]))
			}
		}
		for _, e := range errs {
			fmt.Fprintf(out, "- %v\n", e)
		}
	}
}

func runMethodologyProjectValidate(args []string, root string, out, errw io.Writer) (int, error) {
	path := "."
	jsonOut := false
	for _, a := range args {
		if a == "--json" {
			jsonOut = true
		} else if strings.HasPrefix(a, "-") {
			return 2, nil
		} else if path == "." {
			path = a
		} else {
			return 2, nil
		}
	}
	target, e := filepath.Abs(path)
	if e != nil {
		return 1, e
	}
	manifestPath := filepath.Join(target, ".aok", "project.json")
	report := map[string]any{"kind": "aok-methodology-validation", "projectPath": target, "manifestPath": manifestPath, "implicit": false, "valid": true, "selection": map[string]any{"flow": "kanban", "overlays": []any{}}, "checks": []any{}}
	checks := []any{}
	add := func(n, s, d string) {
		c := map[string]any{"name": n, "status": s}
		if d != "" {
			c["detail"] = d
		}
		checks = append(checks, c)
		if s != "satisfied" {
			report["valid"] = false
		}
	}
	var b []byte
	var policy string
	normalized := []any{}
	selectedOverlays := []methodProfile{}
	seen := map[string]bool{}
	allowed := map[string]bool{}
	byID := map[string]methodProfile{}
	var prof methodProfile
	var profiles []methodProfile
	over := []any{}
	overlaysRaw := []any{}
	flow := ""
	selectionObj := map[string]any{}
	var s any
	selected := false
	m, e := readJSON(manifestPath)
	if e != nil {
		report["valid"] = false
		add("manifest", "missing", "")
		report["checks"] = checks
		goto done
	}
	s, selected = m["methodology"]
	if !selected {
		report["implicit"] = true
	}
	if selected {
		if _, ok := s.(map[string]any); !ok {
			add("selection", "invalid", "Project methodology selection must be an object")
			report["checks"] = checks
			goto done
		}
	}
	selectionObj = obj(s)
	flow = str(selectionObj["flow"])
	if selectionObj["overlays"] != nil {
		var ok bool
		overlaysRaw, ok = selectionObj["overlays"].([]any)
		if !ok {
			add("selection", "invalid", "Project methodology selection must contain a flow string and overlay array")
			report["checks"] = checks
			goto done
		}
		for _, v := range overlaysRaw {
			if _, ok := v.(string); !ok {
				add("selection", "invalid", "Project methodology selection must contain a flow string and overlay array")
				report["checks"] = checks
				goto done
			}
		}
	}
	if selected && flow == "" {
		add("selection", "invalid", "Project methodology selection must contain a flow string and overlay array")
		report["checks"] = checks
		goto done
	}
	if flow == "" {
		flow = "kanban"
	}
	over = overlaysRaw
	report["selection"] = map[string]any{"flow": flow, "overlays": over}
	profiles, e = loadMethodProfiles(root)
	if e != nil {
		return 1, e
	}
	for _, p := range profiles {
		if str(p["id"]) == flow {
			prof = p
		}
	}
	if prof == nil || str(prof["kind"]) != "flow" || str(prof["supportLevel"]) != "active" {
		add("selection", "invalid", "unknown methodology flow: "+flow)
		report["checks"] = checks
		goto done
	}
	for _, p := range profiles {
		byID[str(p["id"])] = p
		if str(p["kind"]) == "overlay" {
			allowed[str(p["id"])] = true
		}
	}
	for _, v := range over {
		o := str(v)
		op := byID[o]
		compatible := false
		if op != nil {
			for _, cf := range arr(op["compatibleFlows"]) {
				if str(cf) == flow {
					compatible = true
				}
			}
		}
		if !allowed[o] || seen[o] || op == nil || str(op["supportLevel"]) != "active" || !compatible {
			add("selection", "invalid", "unknown or duplicate methodology overlay: "+o)
			report["checks"] = checks
			goto done
		}
		seen[o] = true
		selectedOverlays = append(selectedOverlays, op)
	}
	sort.Slice(selectedOverlays, func(i, j int) bool {
		a, _ := selectedOverlays[i]["order"].(float64)
		b, _ := selectedOverlays[j]["order"].(float64)
		if a != b {
			return a < b
		}
		return str(selectedOverlays[i]["id"]) < str(selectedOverlays[j]["id"])
	})
	for _, x := range selectedOverlays {
		normalized = append(normalized, x["id"])
	}
	report["selection"] = map[string]any{"flow": flow, "overlays": normalized}
	if selected {
		requested := []string{}
		for _, x := range over {
			requested = append(requested, str(x))
		}
		norm := []string{}
		for _, x := range normalized {
			norm = append(norm, str(x))
		}
		if strings.Join(requested, "\x00") != strings.Join(norm, "\x00") {
			add("selection", "invalid", "Project methodology overlays are not normalized")
			report["checks"] = checks
			goto done
		}
	}
	add("selection", "satisfied", "")
	policy = filepath.Join(target, "agents", "methodology.md")
	b, e = strictReadFile(policy)
	if os.IsNotExist(e) {
		if report["implicit"] == true {
			add("policy", "implicit-default", "")
		} else {
			add("policy", "missing", "")
		}
	} else if e != nil {
		return 1, e
	} else if !strings.EqualFold(strings.TrimSpace(string(b)), renderMethodologyPolicy(root, flow, normalized)) {
		add("policy", "mismatched", "methodology policy mismatched the selected profile")
	} else {
		add("policy", "satisfied", "")
	}
done:
	report["checks"] = checks
	if jsonOut {
		if e = json.NewEncoder(out).Encode(report); e != nil {
			return 1, e
		}
	} else {
		fmt.Fprintf(out, "Methodology validation: %s\n", map[bool]string{true: "passed", false: "failed"}[report["valid"] == true])
		for _, v := range checks {
			c := obj(v)
			detail := ""
			if c["detail"] != nil {
				detail = " - " + str(c["detail"])
			}
			fmt.Fprintf(out, "- %s: %s%s\n", c["name"], c["status"], detail)
		}
	}
	if report["valid"] != true {
		return 1, nil
	}
	return 0, nil
}

func renderMethodologyPolicy(root, flow string, overlays []any) string {
	profiles, e := loadMethodProfiles(root)
	if e != nil {
		return ""
	}
	var fp methodProfile
	byID := map[string]methodProfile{}
	for _, p := range profiles {
		byID[str(p["id"])] = p
		if str(p["id"]) == flow {
			fp = p
		}
	}
	if fp == nil {
		return ""
	}
	fragment := func(p methodProfile) string {
		rel := str(p["fragment"])
		if rel == "" {
			return ""
		}
		path := filepath.Clean(filepath.Join(root, "methodologies", rel))
		if !within(filepath.Join(root, "methodologies"), path) {
			return ""
		}
		b, _ := strictReadFile(path)
		return strings.TrimSpace(string(b))
	}
	lines := []string{"<!-- Generated by AOK. Do not edit directly. -->", "# Project Methodology", "", "Flow: `" + flow + "`", "Overlays: " + renderBacktickList(overlays), "", "## Flow: " + str(fp["displayName"]), "", fragment(fp)}
	for _, v := range overlays {
		p := byID[str(v)]
		if p != nil {
			lines = append(lines, "", "## Overlay: "+str(p["displayName"]), "", fragment(p))
		}
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}
func renderBacktickList(items []any) string {
	if len(items) == 0 {
		return "none"
	}
	parts := []string{}
	for _, v := range items {
		parts = append(parts, "`"+str(v)+"`")
	}
	return strings.Join(parts, ", ")
}
