package deliverybridge

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/verification"
)

var hashRE = regexp.MustCompile(`^[a-f0-9]{64}$`)

type object = map[string]any

func Validate(m HandoffManifest) (ScopeContextDraft, Report) {
	r := Report{Version: Version, Diagnostics: []string{}, AuthorizationGranted: false, ExecutionReady: false}
	fail := func(s string) { r.Diagnostics = append(r.Diagnostics, s) }
	if m.Version != Version || m.ID == "" {
		fail("handoff version or id is invalid")
	}
	if m.AsOf.IsZero() {
		fail("trusted as-of time is required")
	}
	if err := verification.ValidateManifest(m.Candidate.Manifest); err != nil {
		fail("candidate manifest: " + err.Error())
	}
	if m.Candidate.BundlePath == "" {
		fail("immutable candidate bundle path is required")
	} else {
		fresh, e := verification.LoadCandidate(m.Candidate.BundlePath)
		if e != nil || fresh.Digest != m.Candidate.Digest || fresh.SnapshotSHA256 != m.Candidate.SnapshotSHA256 || !reflect.DeepEqual(fresh.Manifest, m.Candidate.Manifest) {
			fail("candidate bundle changed or cannot be validated")
		}
	}
	if err := verification.ValidatePolicy(m.Policy); err != nil {
		fail("verification policy: " + err.Error())
	}
	if err := verification.VerifyAcceptance(m.Acceptance, m.Policy.Issuers, m.AsOf, time.Duration(m.Policy.MaxAgeSeconds)*time.Second); err != nil {
		fail("acceptance signature or validity: " + err.Error())
	}
	if err := verification.ValidateAcceptanceBinding(m.Acceptance, m.Candidate, m.Policy); err != nil {
		fail("acceptance binding: " + err.Error())
	}
	if !equalTypedBytes(m.AcceptanceBytes, m.Acceptance) {
		fail("acceptance bytes do not equal typed acceptance")
	}
	if err := verifyIndependentReview(m.IndependentReview, m.Candidate, m.Policy, m.AsOf); err != nil {
		fail("independent authenticated review: " + err.Error())
	}
	if !hashRE.MatchString(m.SourceSnapshotSHA256) || !hashRE.MatchString(m.DestinationSnapshotSHA256) {
		fail("source and destination snapshot identities are required")
	}
	sourceSnap, sourceErr := decodeRecord(m.SourceSnapshotBytes, "snapshot_sha256")
	destinationSnap, destinationErr := decodeRecord(m.DestinationSnapshotBytes, "snapshot_sha256")
	if sourceErr != nil {
		fail("source snapshot: " + sourceErr.Error())
	}
	if destinationErr != nil {
		fail("destination snapshot: " + destinationErr.Error())
	}
	if sourceErr == nil && destinationErr == nil {
		if sourceSnap["snapshot_sha256"] != m.SourceSnapshotSHA256 || destinationSnap["snapshot_sha256"] != m.DestinationSnapshotSHA256 {
			fail("snapshot receipt bytes do not match declared identities")
		}
		validateSnapshot(sourceSnap, m.Candidate.Manifest, fail)
		validateSnapshot(destinationSnap, m.Candidate.Manifest, fail)
	}
	if m.SourceSnapshotSHA256 != m.DestinationSnapshotSHA256 {
		fail("destination snapshot changed; verification and review must be rerun on that exact destination")
	}
	ctx, ctxErr := decodeRecord(m.ContextBytes, "context_sha256")
	gate, gateErr := decodeRecord(m.GateBytes, "receipt_sha256")
	review, reviewErr := decodeRecord(m.ReviewBytes, "receipt_sha256")
	if ctxErr != nil {
		fail("delivery context: " + ctxErr.Error())
	}
	if gateErr != nil {
		fail("gate receipt: " + gateErr.Error())
	}
	if reviewErr != nil {
		fail("engine review receipt: " + reviewErr.Error())
	}
	var integration object
	if len(m.IntegrationBytes) > 0 {
		var e error
		integration, e = decodeRecord(m.IntegrationBytes, "receipt_sha256")
		if e != nil {
			fail("integration receipt: " + e.Error())
		}
	}
	if ctxErr == nil && gateErr == nil && reviewErr == nil {
		validateContext(ctx, m.Candidate.Manifest, fail)
		if sourceErr == nil && destinationErr == nil {
			if ctx["repository"] != sourceSnap["repository"] || ctx["repository"] != destinationSnap["repository"] {
				fail("delivery context repository differs from sealed snapshots")
			}
			if sourceSnap["repository"] != m.Candidate.Manifest.Repository || destinationSnap["repository"] != m.Candidate.Manifest.Repository {
				fail("sealed snapshot repository differs from accepted candidate repository")
			}
			if ctx["baseline_revision"] != m.Candidate.Manifest.BaseRevision {
				fail("delivery baseline revision differs from accepted candidate base")
			}
			if sourceSnap["head"] != m.Candidate.Manifest.SourceRevision || destinationSnap["head"] != m.Candidate.Manifest.SourceRevision {
				fail("snapshot head differs from accepted candidate source revision")
			}
		}
		validateGate(ctx, gate, m.DestinationSnapshotSHA256, fail)
		validateEngineReview(ctx, review, m.DestinationSnapshotSHA256, fail)
		if len(m.IntegrationBytes) > 0 {
			validateIntegration(ctx, gate, integration, m.DestinationSnapshotSHA256, fail)
		} else if ctx["integration_required_mode"] != nil {
			fail("context requires integration proof")
		}
	}
	if len(r.Diagnostics) == 0 {
		files := make([]string, 0, len(m.Candidate.Manifest.Assets))
		for _, a := range m.Candidate.Manifest.Assets {
			files = append(files, a.Path)
		}
		sort.Strings(files)
		cmds, _ := ctx["gate_commands"].([]any)
		gateCommands := make([][]string, 0, len(cmds))
		for _, raw := range cmds {
			a := raw.([]any)
			x := make([]string, len(a))
			for i, v := range a {
				x[i] = v.(string)
			}
			gateCommands = append(gateCommands, x)
		}
		envRaw, _ := json.Marshal(gate["environment"])
		ctxHash, _ := ctx["context_sha256"].(string)
		gateHash, _ := gate["receipt_sha256"].(string)
		reviewHash, _ := review["receipt_sha256"].(string)
		d := ScopeContextDraft{Version: Version, ID: m.ID, CandidateSHA256: m.Candidate.Digest, AcceptanceSHA256: shaHex(m.AcceptanceBytes), SourceSnapshotSHA256: m.SourceSnapshotSHA256, DestinationSnapshotSHA256: m.DestinationSnapshotSHA256, Repository: ctx["repository"].(string), BaselineRevision: ctx["baseline_revision"].(string), FilesInScope: files, GateCommands: gateCommands, Environment: envRaw, EngineContextSHA256: ctxHash, EngineGateSHA256: gateHash, EngineReviewSHA256: reviewHash, Actions: []string{"preserve", "prepare-isolated-handoff"}, AuthorizationGranted: false, ExecutionReady: false}
		if integration != nil {
			d.EngineIntegrationSHA256, _ = integration["receipt_sha256"].(string)
		}
		r.Valid = true
		return d, r
	}
	sort.Strings(r.Diagnostics)
	return ScopeContextDraft{}, r
}

func validateSnapshot(s object, candidate verification.CandidateManifest, fail func(string)) {
	if !exactKeys(s, strings.Fields("schema_version kind repository head index_sha256 files included_ignored snapshot_sha256")) {
		fail("snapshot receipt has missing or unknown fields")
		return
	}
	if s["kind"] != "source-snapshot" || numberInt(s["schema_version"]) != 1 || asString(s["repository"]) == "" || !hashRE.MatchString(asString(s["snapshot_sha256"])) || !hashRE.MatchString(asString(s["index_sha256"])) {
		fail("snapshot receipt identity is invalid")
	}
	files, ok := s["files"].([]any)
	if !ok || len(files) != len(candidate.Assets) {
		fail("sealed snapshot inventory does not equal candidate inventory")
		return
	}
	byPath := map[string]verification.Asset{}
	for _, a := range candidate.Assets {
		byPath[a.Path] = a
	}
	seen := map[string]bool{}
	for _, raw := range files {
		rec, ok := raw.(map[string]any)
		if !ok {
			fail("snapshot file record is invalid")
			continue
		}
		path := asString(rec["path"])
		asset, exists := byPath[path]
		if !exists || seen[path] {
			fail("snapshot contains unaccepted or duplicate file path")
			continue
		}
		seen[path] = true
		if rec["type"] != "file" || !exactKeys(rec, strings.Fields("path type sha256 mode")) || rec["sha256"] != asset.SHA256 {
			fail("snapshot file content or type differs from candidate")
		}
		mode := numberInt(rec["mode"])
		if mode < 0 || mode > 4095 || (mode&0111 != 0) != asset.Executable {
			fail("snapshot file mode differs from candidate executable inventory")
		}
	}
	if len(stringList(s["included_ignored"])) != 0 {
		fail("ignored-file snapshot selection is unsupported for candidate handoff")
	}
}

func verifyIndependentReview(rr verification.ReviewReceipt, c verification.Candidate, p verification.VerificationPolicy, asOf time.Time) error {
	boundAuthor := false
	for _, binding := range p.AuthorBindings {
		if binding.Principal == c.Manifest.AuthorPrincipal && c.Manifest.AuthorPrincipal != "" && ((binding.CandidateSHA256 != "" && binding.CandidateSHA256 == c.Digest) || (binding.AssignmentSHA256 != "" && binding.AssignmentSHA256 == c.Manifest.AssignmentSHA256)) {
			boundAuthor = true
		}
	}
	if !boundAuthor {
		return errors.New("candidate author lacks a protected policy binding")
	}
	if rr.Version != verification.Version || rr.ID == "" || rr.CandidateSHA256 != c.Digest || rr.SnapshotSHA256 != c.SnapshotSHA256 || rr.Disposition != "approved" || rr.Principal == "" || rr.Principal == c.Manifest.AuthorPrincipal || rr.PolicySHA256 != verificationDigest(p) {
		return errors.New("review does not bind an independent approved candidate and policy")
	}
	at, e := time.Parse(time.RFC3339, rr.ObservedAt)
	if e != nil || at.After(asOf) || (p.MaxAgeSeconds > 0 && asOf.Sub(at) > time.Duration(p.MaxAgeSeconds)*time.Second) {
		return errors.New("review is stale or has invalid time")
	}
	var issuer *verification.TrustedIssuer
	for i := range p.Issuers {
		if p.Issuers[i].ID == rr.Issuer && p.Issuers[i].Role == "reviewer" {
			issuer = &p.Issuers[i]
			break
		}
	}
	if issuer == nil || issuer.Principal != rr.Principal {
		return errors.New("reviewer is not a trusted independent principal")
	}
	pk, e := base64.StdEncoding.DecodeString(issuer.PublicKey)
	sig, se := base64.StdEncoding.DecodeString(rr.Signature)
	if e != nil || len(pk) != ed25519.PublicKeySize || se != nil {
		return errors.New("invalid review signature encoding")
	}
	rr.Signature = ""
	b, e := json.Marshal(rr)
	if e != nil || !ed25519.Verify(ed25519.PublicKey(pk), b, sig) {
		return errors.New("review signature is invalid")
	}
	return nil
}
func verificationDigest(p verification.VerificationPolicy) string {
	d, _ := verification.Digest(p)
	return d
}
func equalTypedBytes(raw []byte, v any) bool {
	if len(raw) == 0 {
		return false
	}
	got := reflect.New(reflect.TypeOf(v)).Interface()
	if strictDecode(raw, &got) != nil {
		return false
	}
	return reflect.DeepEqual(reflect.ValueOf(got).Elem().Interface(), v)
}

func decodeRecord(raw []byte, sealField string) (object, error) {
	if len(raw) == 0 || len(raw) > 4<<20 {
		return nil, errors.New("receipt bytes are missing or exceed limit")
	}
	var value any
	if e := strictDecode(raw, &value); e != nil {
		return nil, e
	}
	m, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("receipt must be an object")
	}
	seal, ok := m[sealField].(string)
	if !ok || !hashRE.MatchString(seal) {
		return nil, errors.New("receipt seal missing or malformed")
	}
	payload := make(map[string]any, len(m)-1)
	for k, v := range m {
		if k != sealField {
			payload[k] = v
		}
	}
	canonical, e := pythonCanonical(payload)
	if e != nil {
		return nil, fmt.Errorf("unsupported canonical content: %w", e)
	}
	if seal != shaHex(canonical) {
		return nil, errors.New("receipt seal mismatch")
	}
	return m, nil
}

func strictDecode(raw []byte, out any) error {
	if e := rejectDuplicateJSON(raw); e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
func rejectDuplicateJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := scanJSON(d); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
func scanJSON(d *json.Decoder) error {
	t, e := d.Token()
	if e != nil {
		return e
	}
	z, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch z {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			x, e := d.Token()
			if e != nil {
				return e
			}
			k := x.(string)
			if seen[k] {
				return fmt.Errorf("duplicate key %q", k)
			}
			seen[k] = true
			if e = scanJSON(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	case '[':
		for d.More() {
			if e := scanJSON(d); e != nil {
				return e
			}
		}
		_, e = d.Token()
		return e
	}
	return errors.New("invalid JSON delimiter")
}

func validateContext(c object, candidate verification.CandidateManifest, fail func(string)) {
	allow := strings.Fields("schema_version kind repository baseline_revision objective files_in_scope gate_commands review_policy authorized_actions actor_id actor_context environment_keys include_ignored preservation gate_timeout_seconds github_repository github_review_required integration_required_mode initial_snapshot_sha256 context_sha256")
	if !exactKeys(c, allow) {
		fail("delivery context has missing or unknown fields")
	}
	if c["kind"] != "delivery-context" || numberInt(c["schema_version"]) != 1 {
		fail("unsupported delivery context schema")
	}
	if !hashRE.MatchString(asString(c["context_sha256"])) || !hashRE.MatchString(asString(c["initial_snapshot_sha256"])) || !regexp.MustCompile(`^(?:[a-f0-9]{40}|[a-f0-9]{64})$`).MatchString(asString(c["baseline_revision"])) || asString(c["repository"]) == "" {
		fail("delivery context identity is incomplete")
	}
	for _, a := range stringList(c["authorized_actions"]) {
		if a != "isolate" {
			fail("delivery context grants action outside isolated local preparation")
		}
	}
	if asString(c["review_policy"]) != "independent" || asString(c["actor_id"]) == "" || asString(c["actor_context"]) == "" {
		fail("delivery context requires independent review and actor identity")
	}
	if !validCommands(c["gate_commands"]) {
		fail("delivery context gate argv is invalid")
	}
	if !validStrings(c["files_in_scope"]) || len(stringList(c["files_in_scope"])) == 0 {
		fail("delivery context file scope is empty or invalid")
	}
	scope := stringList(c["files_in_scope"])
	sort.Strings(scope)
	candidateFiles := make([]string, 0, len(candidate.Assets))
	for _, asset := range candidate.Assets {
		candidateFiles = append(candidateFiles, asset.Path)
	}
	sort.Strings(candidateFiles)
	if !reflect.DeepEqual(scope, candidateFiles) {
		fail("delivery file scope does not exactly match candidate inventory")
	}
}
func validateGate(c, g object, destination string, fail func(string)) {
	allow := strings.Fields("schema_version kind context_sha256 snapshot_sha256 environment source_unchanged passed results receipt_sha256")
	if !exactKeys(g, allow) {
		fail("gate receipt has missing or unknown fields")
	}
	if g["kind"] != "gate-receipt" || numberInt(g["schema_version"]) != 1 || g["context_sha256"] != c["context_sha256"] || g["snapshot_sha256"] != destination || g["passed"] != true || g["source_unchanged"] != true {
		fail("gate receipt does not pass for exact context and destination")
	}
	validateEnvironment(g["environment"], fail)
	if env, ok := g["environment"].(map[string]any); ok {
		selected, _ := env["selected_variables"].(map[string]any)
		keys := stringList(c["environment_keys"])
		sort.Strings(keys)
		selectedKeys := make([]string, 0, len(selected))
		for key := range selected {
			selectedKeys = append(selectedKeys, key)
		}
		sort.Strings(selectedKeys)
		if !reflect.DeepEqual(keys, selectedKeys) {
			fail("gate environment fingerprints do not match context-selected variables")
		}
	}
	got, ok := g["results"].([]any)
	want, _ := c["gate_commands"].([]any)
	if !ok || len(got) != len(want) {
		fail("gate results do not cover the configured argv")
	} else {
		for i, item := range got {
			x, ok := item.(map[string]any)
			if !ok || !exactKeys(x, strings.Fields("argv returncode timed_out stdout_sha256 stderr_sha256 duration_seconds")) {
				fail("gate result shape is invalid")
				continue
			}
			if !reflect.DeepEqual(x["argv"], want[i]) || numberInt(x["returncode"]) != 0 || x["timed_out"] != false || !hashRE.MatchString(asString(x["stdout_sha256"])) || !hashRE.MatchString(asString(x["stderr_sha256"])) || !validCanonicalNumber(x["duration_seconds"]) {
				fail("gate result failed or differs from configured argv")
			}
		}
	}
}
func validateEngineReview(c, r object, destination string, fail func(string)) {
	if !exactKeys(r, strings.Fields("schema_version kind context_sha256 reviewer reviewer_context snapshot_sha256 outcome report receipt_sha256")) {
		fail("review receipt has missing or unknown fields")
	}
	if r["kind"] != "review-receipt" || numberInt(r["schema_version"]) != 1 || r["context_sha256"] != c["context_sha256"] || r["snapshot_sha256"] != destination || r["outcome"] != "approved" || asString(r["reviewer"]) == "" || r["reviewer"] == c["actor_id"] || r["reviewer_context"] == c["actor_context"] {
		fail("engine review is missing, stale, or not independent")
	}
}
func validateIntegration(c, g, i object, destination string, fail func(string)) {
	allow := strings.Fields("schema_version kind context_sha256 snapshot_sha256 environment specification mode services gates_executed gates source_unchanged passed blockers receipt_sha256")
	if !exactKeys(i, allow) {
		fail("integration receipt has missing or unknown fields")
		return
	}
	if i["kind"] != "integration-proof" || numberInt(i["schema_version"]) != 1 || i["context_sha256"] != c["context_sha256"] || i["snapshot_sha256"] != destination || i["passed"] != true || i["source_unchanged"] != true || len(stringList(i["blockers"])) != 0 || i["gates_executed"] != true {
		fail("integration proof is not successful for the exact destination")
	}
	validateEnvironment(i["environment"], fail)
	if !reflect.DeepEqual(i["environment"], g["environment"]) {
		fail("integration and gate environments differ")
	}
	nested, ok := i["gates"].(map[string]any)
	if !ok || !reflect.DeepEqual(nested, g) {
		fail("integration gate bytes differ from explicit gate receipt")
	}
	if c["integration_required_mode"] != nil && i["mode"] != c["integration_required_mode"] {
		fail("integration mode differs from context policy")
	}
	spec, ok := i["specification"].(map[string]any)
	if !ok || !exactKeys(spec, strings.Fields("required_mode services timeout_seconds")) {
		fail("integration specification shape is invalid")
		return
	}
	if spec["required_mode"] != i["mode"] || !validCanonicalNumber(spec["timeout_seconds"]) {
		fail("integration specification does not match observed mode or timeout")
	}
	wantServices, wok := spec["services"].([]any)
	gotServices, gok := i["services"].([]any)
	if !wok || !gok || len(wantServices) != len(gotServices) {
		fail("integration service observations are incomplete")
		return
	}
	for n, item := range wantServices {
		want, ok := item.(map[string]any)
		got, gok := gotServices[n].(map[string]any)
		if !ok || !gok || !validIntegrationService(want, got) {
			fail("integration service evidence differs from its pinned specification")
		}
	}
}

func validIntegrationService(spec, observed map[string]any) bool {
	if asString(spec["name"]) == "" {
		return false
	}
	switch spec["mode"] {
	case "mock":
		if !exactKeys(spec, strings.Fields("name mode version")) || !exactKeys(observed, strings.Fields("name mode endpoint observed_version response_sha256 available")) {
			return false
		}
		return observed["name"] == spec["name"] && observed["mode"] == "mock" && observed["endpoint"] == nil && observed["observed_version"] == spec["version"] && observed["response_sha256"] == nil && observed["available"] == true
	case "real":
		if !exactKeys(spec, strings.Fields("name mode endpoint version_field expected_version")) || !exactKeys(observed, strings.Fields("name mode endpoint observed_version response_sha256 available")) {
			return false
		}
		return observed["name"] == spec["name"] && observed["mode"] == "real" && observed["endpoint"] == spec["endpoint"] && observed["observed_version"] == spec["expected_version"] && hashRE.MatchString(asString(observed["response_sha256"])) && observed["available"] == true
	default:
		return false
	}
}
func validateEnvironment(v any, fail func(string)) {
	m, ok := v.(map[string]any)
	if !ok || !exactKeys(m, strings.Fields("platform python git selected_variables")) {
		fail("environment receipt shape invalid")
		return
	}
	for _, k := range []string{"platform", "python", "git"} {
		if asString(m[k]) == "" {
			fail("environment fingerprint incomplete")
		}
	}
	vars, ok := m["selected_variables"].(map[string]any)
	if !ok {
		fail("environment variable fingerprints invalid")
		return
	}
	for _, v := range vars {
		if !hashRE.MatchString(asString(v)) {
			fail("environment variable fingerprint invalid")
		}
	}
}

func exactKeys(m map[string]any, allowed []string) bool {
	if m == nil || len(m) != len(allowed) {
		return false
	}
	set := map[string]bool{}
	for _, k := range allowed {
		set[k] = true
	}
	for k := range m {
		if !set[k] {
			return false
		}
	}
	return true
}
func asString(v any) string { s, _ := v.(string); return s }
func numberInt(v any) int {
	n, ok := v.(json.Number)
	if !ok {
		return -1
	}
	i, e := strconv.Atoi(string(n))
	if e != nil {
		return -1
	}
	return i
}
func stringList(v any) []string {
	a, ok := v.([]any)
	if !ok {
		return nil
	}
	r := make([]string, 0, len(a))
	for _, x := range a {
		s, ok := x.(string)
		if !ok {
			return nil
		}
		r = append(r, s)
	}
	return r
}
func validStrings(v any) bool {
	a, ok := v.([]any)
	if !ok {
		return false
	}
	for _, x := range a {
		if _, ok := x.(string); !ok {
			return false
		}
	}
	return true
}
func validCommands(v any) bool {
	a, ok := v.([]any)
	if !ok || len(a) == 0 {
		return false
	}
	for _, v := range a {
		cmd, ok := v.([]any)
		if !ok || len(cmd) == 0 {
			return false
		}
		for _, x := range cmd {
			s, ok := x.(string)
			if !ok || s == "" || strings.ContainsRune(s, 0) {
				return false
			}
		}
	}
	return true
}
func validCanonicalNumber(v any) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	_, e := pythonNumber(string(n))
	return e == nil
}

func shaHex(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// pythonCanonical matches repo_delivery.core.canonical for the JSON subset
// used by its version-1 receipts. Exponents and unsupported float magnitudes
// fail closed instead of silently producing a different seal.
func pythonCanonical(v any) ([]byte, error) {
	var b bytes.Buffer
	if e := writePy(&b, v, 0); e != nil {
		return nil, e
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}
func writePy(b *bytes.Buffer, v any, depth int) error {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			b.WriteString("{}")
			return nil
		}
		b.WriteString("{\n")
		for i, k := range keys {
			indent(b, depth+1)
			if e := writePyString(b, k); e != nil {
				return e
			}
			b.WriteString(": ")
			if e := writePy(b, x[k], depth+1); e != nil {
				return e
			}
			if i < len(keys)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		indent(b, depth)
		b.WriteByte('}')
		return nil
	case []any:
		if len(x) == 0 {
			b.WriteString("[]")
			return nil
		}
		b.WriteString("[\n")
		for i, y := range x {
			indent(b, depth+1)
			if e := writePy(b, y, depth+1); e != nil {
				return e
			}
			if i < len(x)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		indent(b, depth)
		b.WriteByte(']')
		return nil
	case string:
		return writePyString(b, x)
	case json.Number:
		n, e := pythonNumber(string(x))
		if e != nil {
			return e
		}
		b.WriteString(n)
		return nil
	case bool:
		if x {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
		return nil
	case nil:
		b.WriteString("null")
		return nil
	default:
		return fmt.Errorf("unsupported canonical JSON type %T", v)
	}
}
func indent(b *bytes.Buffer, n int) {
	for i := 0; i < n*2; i++ {
		b.WriteByte(' ')
	}
}
func writePyString(b *bytes.Buffer, s string) error {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(b, `\u%04x`, r)
			} else if r <= 0x7e {
				b.WriteRune(r)
			} else if r <= 0xffff {
				fmt.Fprintf(b, `\u%04x`, r)
			} else {
				r -= 0x10000
				hi := 0xd800 + (r >> 10)
				lo := 0xdc00 + (r & 0x3ff)
				fmt.Fprintf(b, `\u%04x\u%04x`, hi, lo)
			}
		}
	}
	b.WriteByte('"')
	return nil
}
func pythonNumber(s string) (string, error) {
	if strings.ContainsAny(s, "eE+") {
		return "", errors.New("exponent notation unsupported")
	}
	if !strings.Contains(s, ".") {
		n, e := strconv.ParseInt(s, 10, 64)
		if e == nil {
			return strconv.FormatInt(n, 10), nil
		}
		u, ue := strconv.ParseUint(s, 10, 64)
		if ue == nil {
			return strconv.FormatUint(u, 10), nil
		}
		return "", errors.New("integer outside supported range")
	}
	f, e := strconv.ParseFloat(s, 64)
	if e != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return "", errors.New("invalid float")
	}
	if math.Abs(f) >= 1e15 || (f != 0 && math.Abs(f) < 1e-4) {
		return "", errors.New("float magnitude requires unsupported Python exponent formatting")
	}
	out := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.Contains(out, ".") {
		out += ".0"
	}
	if f == 0 && strings.HasPrefix(s, "-") {
		out = "-0.0"
	}
	return out, nil
}
