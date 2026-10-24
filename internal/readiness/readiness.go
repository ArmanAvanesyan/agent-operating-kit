package readiness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Digest(v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	return Hash(b), nil
}

func ReadManifest(path string) (RouteManifest, string, error) {
	var m RouteManifest
	b, e := readSafe(path, MaxDocumentBytes)
	if e != nil {
		return m, "", e
	}
	if e = decodeStrict(b, &m); e == nil {
		e = ValidateManifest(m)
	}
	if e != nil {
		return m, "", e
	}
	digest, _ := Digest(m)
	return m, digest, nil
}
func ReadPolicy(path string) (Policy, string, error) {
	var p Policy
	b, e := readSafe(path, MaxDocumentBytes)
	if e != nil {
		return p, "", e
	}
	if e = decodeStrict(b, &p); e == nil {
		e = ValidatePolicy(p)
	}
	if e != nil {
		return p, "", e
	}
	digest, _ := Digest(p)
	return p, digest, nil
}
func AssessPaths(manifestPath, policyPath string, in Inputs, adapters Adapters, asOf time.Time) (Report, error) {
	m, _, e := ReadManifest(manifestPath)
	if e != nil {
		return Report{}, e
	}
	p, _, e := ReadPolicy(policyPath)
	if e != nil {
		return Report{}, e
	}
	return Assess(m, p, in, adapters, asOf)
}
func Assess(m RouteManifest, p Policy, in Inputs, adapters Adapters, asOf time.Time) (Report, error) {
	if e := ValidateManifest(m); e != nil {
		return Report{}, e
	}
	if e := ValidatePolicy(p); e != nil {
		return Report{}, e
	}
	policyDigest, e := Digest(p)
	if e != nil {
		return Report{}, e
	}
	if in.PolicyPin.SHA256 != "" && policyDigest != in.PolicyPin.SHA256 {
		return Report{}, errors.New("policy digest does not match external pin")
	}
	if asOf.IsZero() {
		return Report{}, errors.New("explicit as-of time is required")
	}
	asOf = asOf.UTC()
	routeDigest, e := Digest(m)
	if e != nil {
		return Report{}, e
	}
	report := Report{Version: Version, ManifestSHA256: routeDigest, PolicySHA256: policyDigest, AsOf: asOf.Format(time.RFC3339Nano), InputSHA256s: []string{routeDigest, policyDigest}, Gates: []GateResult{}, NextActions: []string{}, PermittedClaim: "offline-readiness-snapshot", AuthorizationGranted: false, ExecutionReady: false}
	var earliest time.Time
	trustReason := "external policy pin, identity, role or authority verifier is missing"
	trustStatus := "blocked"
	if hashPattern.MatchString(in.PolicyPin.SHA256) && strings.TrimSpace(in.PolicyPin.Principal) != "" && in.PolicyPin.Role == "readiness-policy-authority" && in.PolicyAuthority != nil {
		if err := in.PolicyAuthority.VerifyPolicyAuthority(p, in.PolicyPin); err == nil {
			trustStatus = "passed"
			trustReason = "policy authority verified by external trust boundary"
		} else {
			trustReason = "external policy authority verification failed"
		}
	}
	report.Gates = append(report.Gates, GateResult{ID: "policy-trust", Required: true, Status: trustStatus, Reason: trustReason, EvidenceSHA256: []string{policyDigest}})
	for _, req := range p.Requirements {
		gr := GateResult{ID: req.ID, Required: req.Required, Status: "unknown", Reason: "native validator unavailable", EvidenceSHA256: []string{}}
		if req.Applicability == "not-applicable" {
			if (req.ID == "p6.codex" || req.ID == "p6.claude") && routeScope(m)["host"] != req.Scope["host"] {
				gr.Status = "not-applicable"
				gr.Reason = "excluded by explicit host route applicability rule"
			} else {
				gr.Status = "blocked"
				gr.Reason = "not-applicable conflicts with exact route host"
			}
			report.Gates = append(report.Gates, gr)
			continue
		}
		if !scopeMatches(routeScope(m), req.Scope) {
			gr.Status = "blocked"
			gr.Reason = "required gate scope does not match this route"
			report.Gates = append(report.Gates, gr)
			continue
		}
		validator := adapters[req.Validator]
		if validator != nil {
			bindings := append([]EvidenceBinding(nil), req.Bindings...)
			if len(bindings) == 0 {
				gr.Status = "blocked"
				gr.Reason = "required evidence binding missing"
			} else {
				data := map[string][]byte{}
				var status string
				var reason string
				for _, b := range bindings {
					if b.Revoked {
						status = "blocked"
						reason = "evidence is revoked"
						break
					}
					if req.EvidenceOrigin != "either" && req.EvidenceOrigin != b.Origin {
						status = "blocked"
						reason = "evidence origin does not meet requirement"
						break
					}
					if !scopeMatches(routeScope(m), req.Scope) || !scopeMatches(b.Scope, req.Scope) {
						status = "blocked"
						reason = "evidence scope mismatch"
						break
					}
					observed, err := time.Parse(time.RFC3339, b.ObservedAt)
					if err != nil {
						status = "blocked"
						reason = "invalid evidence observation time"
						break
					}
					if observed.After(asOf) {
						status = "blocked"
						reason = "evidence observation is after as-of"
						break
					}
					if p.MaxEvidenceAgeSeconds > 0 && asOf.Sub(observed) > time.Duration(p.MaxEvidenceAgeSeconds)*time.Second {
						status = "stale"
						reason = "evidence exceeds maximum age"
						break
					}
					until, err := time.Parse(time.RFC3339, b.ValidUntil)
					if err != nil || !asOf.Before(until) {
						status = "stale"
						reason = "evidence expiry is missing, invalid, or expired"
						break
					}
					if earliest.IsZero() || until.Before(earliest) {
						earliest = until
					}
					asset, err := readEvidence(in.EvidenceRoot, b.Path)
					if err != nil {
						status = "blocked"
						reason = "evidence asset unavailable or unsafe"
						break
					}
					if Hash(asset) != b.SHA256 {
						status = "blocked"
						reason = "evidence digest mismatch"
						break
					}
					data[b.ID] = asset
					report.InputSHA256s = append(report.InputSHA256s, b.SHA256)
					gr.EvidenceSHA256 = append(gr.EvidenceSHA256, b.SHA256)
				}
				if status != "" {
					gr.Status = status
					gr.Reason = reason
				} else {
					v := validator.Validate(ValidationInput{Requirement: req, Route: m, EvidenceRoot: in.EvidenceRoot, Evidence: bindings, EvidenceBytes: data, AsOf: asOf})
					if req.Applicability == "applicable" && v.Status == "not-applicable" {
						gr.Status = "blocked"
						gr.Reason = "native validator cannot override manifest applicability"
					} else if validStatus(v.Status) {
						gr.Status = v.Status
						gr.Reason = v.Reason
						if v.ValidUntil != "" {
							until, err := time.Parse(time.RFC3339, v.ValidUntil)
							if err != nil || !asOf.Before(until) {
								gr.Status = "stale"
								gr.Reason = "native evidence validity has expired or is invalid"
							} else if earliest.IsZero() || until.Before(earliest) {
								earliest = until
							}
						}
						if len(v.EvidenceSHA256) > 0 {
							gr.EvidenceSHA256 = append([]string{}, v.EvidenceSHA256...)
						}
					} else {
						gr.Status = "unknown"
						gr.Reason = "native validator returned invalid or empty status"
					}
				}
			}
		}
		sort.Strings(gr.EvidenceSHA256)
		report.Gates = append(report.Gates, gr)
	}
	sort.Slice(report.Gates, func(i, j int) bool { return report.Gates[i].ID < report.Gates[j].ID })
	sort.Strings(report.InputSHA256s)
	report.InputSHA256s = uniqueStrings(report.InputSHA256s)
	if !earliest.IsZero() {
		report.EarliestValidUntil = earliest.UTC().Format(time.RFC3339)
	}
	block := false
	for _, g := range report.Gates {
		if g.Required && g.Status != "passed" && g.Status != "not-applicable" {
			block = true
			report.NextActions = append(report.NextActions, g.ID+": resolve "+g.Status+" gate")
		}
	}
	if block {
		report.Status = "blocked"
	} else {
		report.Status = "ready-offline"
	}
	return report, nil
}

func ValidateManifest(m RouteManifest) error {
	if m.Version != Version || !idPattern.MatchString(m.ID) {
		return errors.New("unsupported readiness version or invalid route ID")
	}
	if m.Host != "codex" && m.Host != "claude-code" {
		return errors.New("unsupported exact route host")
	}
	for name, value := range map[string]string{"host": m.Host, "hostVersion": m.HostVersion, "hostSha256": m.HostSHA256, "os": m.OS, "arch": m.Arch, "transport": m.Transport, "controlPolicySha256": m.ControlPolicySHA256, "configurationSha256": m.ConfigurationSHA256, "profileSha256": m.ProfileSHA256, "account": m.Account, "authMode": m.AuthMode, "pool": m.Pool, "verifierSha256": m.VerifierSHA256, "rubricSha256": m.RubricSHA256, "environmentSha256": m.EnvironmentSHA256} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("route field %s is required", name)
		}
	}
	for _, h := range []string{m.HostSHA256, m.ControlPolicySHA256, m.ConfigurationSHA256, m.ProfileSHA256, m.VerifierSHA256, m.RubricSHA256, m.EnvironmentSHA256} {
		if !hashPattern.MatchString(h) {
			return errors.New("route contains invalid digest")
		}
	}
	providerFields := []string{m.Provider, m.Origin, m.NativeTenant, m.Container, m.ItemKind, m.Operation, m.API, m.ProviderAuth, m.DeliveryMilestone}
	n := 0
	for _, s := range providerFields {
		if s != "" {
			n++
		}
	}
	if n != 0 && n != len(providerFields) {
		return errors.New("provider route scope must be complete")
	}
	return nil
}
func ValidatePolicy(p Policy) error {
	if p.Version != Version || !idPattern.MatchString(p.ID) || len(p.Requirements) == 0 || p.MaxEvidenceAgeSeconds < 1 || p.MaxEvidenceAgeSeconds > 365*24*60*60 {
		return errors.New("invalid readiness policy")
	}
	seen := map[string]bool{}
	for _, r := range p.Requirements {
		if !idPattern.MatchString(r.ID) || seen[r.ID] || strings.TrimSpace(r.Validator) == "" {
			return errors.New("invalid or duplicate gate requirement")
		}
		seen[r.ID] = true
		if r.EvidenceOrigin != "fixture" && r.EvidenceOrigin != "live" && r.EvidenceOrigin != "either" {
			return errors.New("invalid evidence origin requirement")
		}
		if r.Applicability != "applicable" && r.Applicability != "not-applicable" {
			return errors.New("invalid gate applicability")
		}
		requiredScope := scopeDimensions(r.ID)
		if len(r.Scope) == 0 || len(requiredScope) == 0 {
			return errors.New("gate exact scope is required")
		}
		for _, k := range requiredScope {
			if strings.TrimSpace(r.Scope[k]) == "" {
				return fmt.Errorf("gate %q is missing exact scope dimension %q", r.ID, k)
			}
			if strings.HasSuffix(k, "Sha256") && !hashPattern.MatchString(r.Scope[k]) {
				return fmt.Errorf("gate %q has invalid digest scope %q", r.ID, k)
			}
		}
		knownScope := routeScope(RouteManifest{})
		for k := range r.Scope {
			if _, ok := knownScope[k]; !ok {
				return fmt.Errorf("gate %q has unknown route scope dimension %q", r.ID, k)
			}
		}
		if !knownValidator(r.Validator) || expectedValidator(r.ID) != r.Validator {
			return fmt.Errorf("gate %q has no matching native validator", r.ID)
		}
		bid := map[string]bool{}
		paths := map[string]bool{}
		for _, b := range r.Bindings {
			if !idPattern.MatchString(b.ID) || bid[b.ID] || !safeRel(b.Path) || paths[b.Path] || !hashPattern.MatchString(b.SHA256) || b.Issuer == "" || b.Role == "" || (b.Origin != "fixture" && b.Origin != "live") || len(b.Scope) == 0 || b.ValidUntil == "" {
				return errors.New("invalid or conflicting evidence binding")
			}
			if !reflect.DeepEqual(b.Scope, r.Scope) {
				return errors.New("evidence scope must equal the exact required route scope")
			}
			observed, oe := time.Parse(time.RFC3339, b.ObservedAt)
			until, ue := time.Parse(time.RFC3339, b.ValidUntil)
			if oe != nil || ue != nil || !until.After(observed) || until.Sub(observed) > time.Duration(p.MaxEvidenceAgeSeconds)*time.Second {
				return errors.New("evidence validity window is invalid or exceeds policy age")
			}
			bid[b.ID] = true
			paths[b.Path] = true
			for _, x := range b.Supersedes {
				if x == b.ID {
					return errors.New("evidence cannot supersede itself")
				}
			}
		}
	}
	for _, id := range mandatoryGateIDs {
		r, ok := findRequirement(p.Requirements, id)
		if !ok || !r.Required || r.EvidenceOrigin != "live" {
			return fmt.Errorf("mandatory readiness gate %q must be required and live", id)
		}
		if id == "p6.codex" || id == "p6.claude" {
			expectedHost := "codex"
			if id == "p6.claude" {
				expectedHost = "claude-code"
			}
			if r.Applicability == "not-applicable" {
				if r.Scope["host"] != expectedHost {
					return fmt.Errorf("host gate %q has invalid not-applicable scope", id)
				}
			} else if r.Applicability != "applicable" || r.Scope["host"] != expectedHost {
				return fmt.Errorf("host gate %q must bind its exact host applicability", id)
			}
		} else if r.Applicability != "applicable" {
			return fmt.Errorf("mandatory gate %q cannot be marked not-applicable", id)
		}
	}
	return nil
}

var mandatoryGateIDs = []string{"p6.codex", "p6.claude", "p6.native-task-variants", "p3.collectors", "p4.quality", "p6.command-verifier", "p9.identity", "p5.github", "p5.azure", "p5.linear", "p5.jira", "p8.writeback", "p8.acceptance-evidence", "p8.delivery-evidence", "p8.publication", "p7.connected-runtime", "p9.operational-drills"}

func findRequirement(rs []GateRequirement, id string) (GateRequirement, bool) {
	for _, r := range rs {
		if r.ID == id {
			return r, true
		}
	}
	return GateRequirement{}, false
}
func expectedValidator(id string) string {
	switch id {
	case "p6.codex":
		return "native.host.codex"
	case "p6.claude":
		return "native.host.claude"
	case "p6.native-task-variants":
		return "native.native-task-variants"
	case "p3.collectors":
		return "native.collectors"
	case "p4.quality":
		return "native.quality"
	case "p6.command-verifier":
		return "native.command-verifier"
	case "p9.identity":
		return "native.identity"
	case "p5.github":
		return "native.tracker.github"
	case "p5.azure":
		return "native.tracker.azure"
	case "p5.linear":
		return "native.tracker.linear"
	case "p5.jira":
		return "native.tracker.jira"
	case "p8.writeback":
		return "native.writeback"
	case "p8.acceptance-evidence":
		return "native.acceptance"
	case "p8.delivery-evidence":
		return "native.delivery-evidence"
	case "p8.publication":
		return "native.publication"
	case "p7.connected-runtime":
		return "native.connected-runtime"
	case "p9.operational-drills":
		return "native.operational-drills"
	}
	return ""
}
func scopeDimensions(id string) []string {
	switch id {
	case "p6.codex", "p6.claude":
		return []string{"host", "hostVersion", "hostSha256", "os", "arch", "transport", "controlPolicySha256", "account", "authMode"}
	case "p6.native-task-variants":
		return []string{"host", "hostVersion", "hostSha256", "os", "arch", "transport", "controlPolicySha256", "configurationSha256", "profileSha256"}
	case "p3.collectors":
		return []string{"host", "hostVersion", "account", "authMode", "pool"}
	case "p4.quality":
		return []string{"configurationSha256", "profileSha256", "account", "verifierSha256", "rubricSha256"}
	case "p6.command-verifier":
		return []string{"host", "hostVersion", "verifierSha256", "environmentSha256"}
	case "p9.identity":
		return []string{"account", "authMode"}
	case "p5.github", "p5.azure", "p5.linear", "p5.jira", "p8.writeback":
		return []string{"provider", "origin", "nativeTenant", "container", "itemKind", "operation", "api", "providerAuth", "deliveryMilestone"}
	case "p8.delivery-evidence":
		return []string{"provider", "origin", "nativeTenant", "container", "itemKind", "operation", "api", "providerAuth"}
	case "p8.publication":
		return []string{"host", "configurationSha256", "environmentSha256", "provider", "origin", "nativeTenant", "container", "itemKind", "operation", "api", "providerAuth", "deliveryMilestone"}
	case "p8.acceptance-evidence":
		return []string{"configurationSha256", "profileSha256", "account", "verifierSha256", "rubricSha256"}
	case "p7.connected-runtime":
		return []string{"host", "hostVersion", "controlPolicySha256", "configurationSha256", "profileSha256", "account", "authMode", "pool"}
	case "p9.operational-drills":
		return []string{"controlPolicySha256", "environmentSha256", "account"}
	}
	return nil
}
func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := in[:1]
	for _, s := range in[1:] {
		if s != out[len(out)-1] {
			out = append(out, s)
		}
	}
	return out
}
func knownValidator(s string) bool {
	switch s {
	case "native.host.codex", "native.host.claude", "native.native-task-variants", "native.collectors", "native.quality", "native.command-verifier", "native.identity", "native.tracker.github", "native.tracker.azure", "native.tracker.linear", "native.tracker.jira", "native.writeback", "native.acceptance", "native.delivery-evidence", "native.publication", "native.connected-runtime", "native.operational-drills":
		return true
	}
	return false
}
func routeScope(m RouteManifest) map[string]string {
	return map[string]string{"host": m.Host, "hostVersion": m.HostVersion, "hostSha256": m.HostSHA256, "os": m.OS, "arch": m.Arch, "transport": m.Transport, "controlPolicySha256": m.ControlPolicySHA256, "configurationSha256": m.ConfigurationSHA256, "profileSha256": m.ProfileSHA256, "account": m.Account, "authMode": m.AuthMode, "pool": m.Pool, "verifierSha256": m.VerifierSHA256, "rubricSha256": m.RubricSHA256, "environmentSha256": m.EnvironmentSHA256, "provider": m.Provider, "origin": m.Origin, "nativeTenant": m.NativeTenant, "container": m.Container, "itemKind": m.ItemKind, "operation": m.Operation, "api": m.API, "providerAuth": m.ProviderAuth, "deliveryMilestone": m.DeliveryMilestone}
}
func scopeMatches(actual, required map[string]string) bool {
	for k, v := range required {
		if actual[k] != v {
			return false
		}
	}
	return true
}
func validStatus(s string) bool {
	return s == "passed" || s == "blocked" || s == "stale" || s == "unknown" || s == "not-applicable"
}
func safeRel(s string) bool {
	return s != "" && !filepath.IsAbs(s) && filepath.Clean(s) == s && s != "." && s != ".." && !strings.HasPrefix(s, ".."+string(filepath.Separator)) && !strings.Contains(s, "\\")
}
func readEvidence(root, rel string) ([]byte, error) {
	if root == "" {
		return nil, errors.New("evidence root required")
	}
	if !safeRel(rel) {
		return nil, errors.New("unsafe evidence path")
	}
	return readSafe(filepath.Join(root, rel), MaxEvidenceBytes)
}
func readSafe(path string, max int64) ([]byte, error) {
	if path == "" {
		return nil, errors.New("path required")
	}
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	for cur := abs; ; cur = filepath.Dir(cur) {
		st, err := os.Lstat(cur)
		if err != nil {
			return nil, err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symlink input forbidden")
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	st, e := os.Lstat(abs)
	if e != nil {
		return nil, e
	}
	if !st.Mode().IsRegular() || st.Size() > max {
		return nil, errors.New("regular bounded file required")
	}
	return os.ReadFile(abs)
}

func decodeStrict(b []byte, v any) error {
	if len(b) > MaxDocumentBytes {
		return errors.New("document exceeds size limit")
	}
	if e := rejectDuplicateKeys(b); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
func rejectDuplicateKeys(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	if e := scan(d); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
func scan(d *json.Decoder) error {
	t, e := d.Token()
	if e != nil {
		return e
	}
	x, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch x {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			s, ok := k.(string)
			if !ok || seen[s] {
				return errors.New("invalid or duplicate JSON key")
			}
			seen[s] = true
			if e = scan(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	case '[':
		for d.More() {
			if e := scan(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	}
	return errors.New("invalid JSON delimiter")
}
