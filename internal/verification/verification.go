package verification

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

func Digest(v any) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]), nil
}
func DecodeManifest(b []byte) (CandidateManifest, error) {
	if len(b) > MaxPolicyFrameBytes {
		return CandidateManifest{}, errors.New("candidate manifest exceeds frame limit")
	}
	var v CandidateManifest
	e := decodeStrict(b, &v)
	if e == nil {
		e = ValidateManifest(v)
	}
	return v, e
}
func DecodePolicy(b []byte) (VerificationPolicy, error) {
	if len(b) > MaxPolicyFrameBytes {
		return VerificationPolicy{}, errors.New("verification policy exceeds frame limit")
	}
	var v VerificationPolicy
	e := decodeStrict(b, &v)
	if e == nil {
		e = ValidatePolicy(v)
	}
	return v, e
}
func decodeStrict(b []byte, v any) error {
	if err := rejectDuplicateKeys(b); err != nil {
		return err
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
	d.UseNumber()
	if err := scanJSON(d); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing JSON data")
	}
	return nil
}
func scanJSON(d *json.Decoder) error {
	tok, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			if e != nil {
				return e
			}
			key, ok := k.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = true
			if e = scanJSON(d); e != nil {
				return e
			}
		}
		_, err = d.Token()
		return err
	case '[':
		for d.More() {
			if err := scanJSON(d); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	default:
		return errors.New("invalid JSON delimiter")
	}
}
func ValidateManifest(m CandidateManifest) error {
	if m.Version != Version {
		return errors.New("unsupported verification manifest version")
	}
	if !validID(m.ID) || m.Repository == "" || m.SourceRevision == "" || m.BaseRevision == "" {
		return errors.New("candidate identity, repository, and revisions are required")
	}
	if m.Origin != "runtime" && m.Origin != "manual" && m.Origin != "fixture" {
		return errors.New("invalid candidate origin")
	}
	if m.Origin == "runtime" && (m.RunID == "" || m.StepID == "" || m.AttemptID == "" || m.Fence == 0 || m.CandidateRevision == 0 || m.InputSHA256 == "" || m.HostReceiptSHA256 == "" || m.PreparedReceiptSHA256 == "") {
		return errors.New("runtime candidate lineage is incomplete")
	}
	if m.Origin == "runtime" {
		for _, h := range []string{m.PlanSHA256, m.GoalSHA256, m.AssignmentSHA256, m.InputSHA256, m.ConfigurationSHA256, m.ProfileSHA256, m.HostReceiptSHA256, m.PreparedReceiptSHA256, m.CheckpointSHA256} {
			if !isHash(h) {
				return errors.New("runtime candidate contains an invalid lineage digest")
			}
		}
		if m.AuthorPrincipal == "" {
			return errors.New("runtime candidate author principal is required")
		}
	}
	if len(m.Assets) == 0 {
		return errors.New("candidate inventory is empty")
	}
	seen := map[string]bool{}
	for _, a := range m.Assets {
		if strings.EqualFold(filepath.ToSlash(a.Path), "manifest.json") || !safeRel(a.Path) || seen[strings.ToLower(a.Path)] || !isHash(a.SHA256) || strings.TrimSpace(a.Role) == "" {
			return fmt.Errorf("invalid or duplicate asset %q", a.Path)
		}
		seen[strings.ToLower(a.Path)] = true
	}
	return nil
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validID(s string) bool {
	if !idPattern.MatchString(s) || s == "." || s == ".." {
		return false
	}
	base := strings.ToUpper(strings.SplitN(s, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return false
	}
	return true
}
func ValidatePolicy(p VerificationPolicy) error {
	if p.Version != Version || p.ID == "" || len(p.Criteria) == 0 || p.MaxAgeSeconds <= 0 {
		return errors.New("invalid verification policy")
	}
	ids := map[string]bool{}
	for _, c := range p.Criteria {
		if c.ID == "" || ids[c.ID] {
			return errors.New("criterion IDs must be unique and nonempty")
		}
		ids[c.ID] = true
		if c.Kind != "asset-present" && c.Kind != "asset-sha256" && c.Kind != "evidence-resolves" && c.Kind != "scope-equals" && c.Kind != "command" && c.Kind != "inventory-complete" && c.Kind != "role-covered" {
			return fmt.Errorf("unsupported criterion kind %q", c.Kind)
		}
		if c.Kind == "command" && c.VerifierID == "" {
			return errors.New("command criterion requires verifierId")
		}
	}
	verifierIDs := map[string]bool{}
	for _, v := range p.Verifiers {
		if v.ID == "" || v.Kind != "command" || !isHash(v.ImplementationSHA256) || !isHash(v.RubricSHA256) || v.TimeoutSeconds < 1 || v.TimeoutSeconds > MaxVerifierTimeoutSeconds || v.MaxOutputBytes < 1 || v.MaxOutputBytes > MaxVerifierOutputBytes {
			return errors.New("verifier pin and positive bounds required")
		}
		if verifierIDs[v.ID] {
			return errors.New("verifier IDs must be unique")
		}
		verifierIDs[v.ID] = true
	}
	issuerIDs := map[string]bool{}
	for _, i := range p.Issuers {
		pk, keyErr := base64.StdEncoding.DecodeString(i.PublicKey)
		if i.ID == "" || issuerIDs[i.ID] || keyErr != nil || len(pk) != ed25519.PublicKeySize || (i.Role != "verifier" && i.Role != "reviewer" && i.Role != "acceptance" && i.Role != "controls" && i.Role != "acceptance-projection-authority") || ((i.Role == "reviewer" || i.Role == "verifier") && i.Principal == "") || (i.Role == "controls" && !isHash(i.ExecutorSHA256)) {
			return errors.New("invalid or duplicate trusted issuer")
		}
		issuerIDs[i.ID] = true
	}
	authorBindings := map[string]bool{}
	for _, b := range p.AuthorBindings {
		if b.Principal == "" || ((b.AssignmentSHA256 == "") == (b.CandidateSHA256 == "")) || (b.AssignmentSHA256 != "" && !isHash(b.AssignmentSHA256)) || (b.CandidateSHA256 != "" && !isHash(b.CandidateSHA256)) {
			return errors.New("invalid author binding")
		}
		key := b.AssignmentSHA256 + "/" + b.CandidateSHA256
		if authorBindings[key] {
			return errors.New("duplicate author binding")
		}
		authorBindings[key] = true
	}
	for _, c := range p.Criteria {
		if c.Kind == "command" && !verifierIDs[c.VerifierID] {
			return errors.New("criterion references unknown verifier")
		}
	}
	return nil
}
func safeRel(p string) bool {
	return p != "" && !filepath.IsAbs(p) && filepath.ToSlash(filepath.Clean(p)) == p && p != "." && !strings.HasPrefix(p, "../") && !strings.Contains(p, "\\")
}
func isHash(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}

// ImportCandidate copies a manifest inventory into the exact create-only output
// directory supplied by the caller. It rejects
// links, missing/extra files, mode drift and content changes.
func ImportCandidate(m CandidateManifest, sourceRoot, outputDir string) (Candidate, error) {
	if m.Origin == "runtime" {
		return Candidate{}, errors.New("runtime candidate import requires a trusted P7 lineage verifier")
	}
	return importCandidate(m, sourceRoot, outputDir)
}

type RuntimeLineageVerifier interface{ VerifyCandidateLineage(CandidateManifest) error }

func ImportRuntimeCandidate(m CandidateManifest, sourceRoot, outputDir string, lineage RuntimeLineageVerifier) (Candidate, error) {
	if lineage == nil {
		return Candidate{}, errors.New("trusted P7 lineage verifier is required")
	}
	if m.Origin != "runtime" {
		return Candidate{}, errors.New("runtime import requires runtime origin")
	}
	if err := ValidateManifest(m); err != nil {
		return Candidate{}, err
	}
	if err := lineage.VerifyCandidateLineage(m); err != nil {
		return Candidate{}, fmt.Errorf("P7 candidate lineage rejected: %w", err)
	}
	return importCandidate(m, sourceRoot, outputDir)
}

func importCandidate(m CandidateManifest, sourceRoot, outputDir string) (Candidate, error) {
	if e := ValidateManifest(m); e != nil {
		return Candidate{}, e
	}
	root, e := filepath.Abs(sourceRoot)
	if e != nil {
		return Candidate{}, e
	}
	root, e = filepath.EvalSymlinks(root)
	if e != nil {
		return Candidate{}, e
	}
	if e = rejectSymlinkAncestors(root); e != nil {
		return Candidate{}, fmt.Errorf("unsafe source root: %w", e)
	}
	dest, e := filepath.Abs(outputDir)
	if e != nil {
		return Candidate{}, e
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(dest))
	if e != nil {
		return Candidate{}, fmt.Errorf("output parent must exist and resolve: %w", e)
	}
	dest = filepath.Join(parent, filepath.Base(dest))
	if e = rejectSymlinkAncestors(dest); e != nil {
		return Candidate{}, fmt.Errorf("unsafe candidate output: %w", e)
	}
	if _, e = os.Lstat(dest); e == nil {
		return Candidate{}, errors.New("candidate bundle already exists")
	}
	if e = os.Mkdir(dest, 0700); e != nil {
		return Candidate{}, e
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(dest)
		}
	}()
	listed := map[string]Asset{}
	var captureBytes int64
	for _, a := range m.Assets {
		listed[a.Path] = a
		src := filepath.Join(root, filepath.FromSlash(a.Path))
		st, e := os.Lstat(src)
		if e != nil {
			return Candidate{}, e
		}
		if !st.Mode().IsRegular() {
			return Candidate{}, fmt.Errorf("asset %s is not a regular file", a.Path)
		}
		captureBytes += st.Size()
		if captureBytes > MaxCandidateCaptureBytes {
			return Candidate{}, errors.New("candidate exceeds capture size limit")
		}
		if st.Mode().Perm()&0111 != 0 != a.Executable {
			return Candidate{}, fmt.Errorf("asset mode mismatch: %s", a.Path)
		}
		b, e := os.ReadFile(src)
		if e != nil {
			return Candidate{}, e
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != a.SHA256 {
			return Candidate{}, fmt.Errorf("asset digest mismatch: %s", a.Path)
		}
		out := filepath.Join(dest, filepath.FromSlash(a.Path))
		if e = os.MkdirAll(filepath.Dir(out), 0700); e != nil {
			return Candidate{}, e
		}
		if e = os.WriteFile(out, b, 0400); e != nil {
			return Candidate{}, e
		}
		mode := os.FileMode(0400)
		if a.Executable {
			mode = 0500
		}
		if e = os.Chmod(out, mode); e != nil {
			return Candidate{}, e
		}
	}
	if e = filepath.WalkDir(root, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == root {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			return nil
		}
		if _, yes := listed[rel]; !yes {
			return fmt.Errorf("unlisted source file: %s", rel)
		}
		return nil
	}); e != nil {
		return Candidate{}, e
	}
	data, e := json.Marshal(m)
	if e != nil {
		return Candidate{}, e
	}
	if e = os.WriteFile(filepath.Join(dest, "manifest.json"), data, 0400); e != nil {
		return Candidate{}, e
	}
	c, e := LoadCandidate(dest)
	if e != nil {
		return Candidate{}, e
	}
	ok = true
	return c, nil
}
func rejectSymlinkAncestors(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	vol := filepath.VolumeName(abs)
	cur := vol + string(filepath.Separator)
	parts := strings.Split(strings.TrimPrefix(abs, cur), string(filepath.Separator))
	for _, part := range parts {
		if part == "" {
			continue
		}
		cur = filepath.Join(cur, part)
		st, e := os.Lstat(cur)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink path component %s", cur)
		}
	}
	return nil
}
func LoadCandidate(bundle string) (Candidate, error) {
	rootInfo, e := os.Lstat(bundle)
	if e != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return Candidate{}, errors.New("candidate bundle root is missing or unsafe")
	}
	manifestPath := filepath.Join(bundle, "manifest.json")
	manifestInfo, e := os.Lstat(manifestPath)
	if e != nil || !manifestInfo.Mode().IsRegular() {
		return Candidate{}, errors.New("candidate manifest is missing or unsafe")
	}
	b, e := os.ReadFile(manifestPath)
	if e != nil {
		return Candidate{}, e
	}
	m, e := DecodeManifest(b)
	if e != nil {
		return Candidate{}, e
	}
	for _, a := range m.Assets {
		p := filepath.Join(bundle, filepath.FromSlash(a.Path))
		st, e := os.Lstat(p)
		if e != nil || !st.Mode().IsRegular() {
			return Candidate{}, fmt.Errorf("missing or unsafe bundle asset %s", a.Path)
		}
		if st.Mode().Perm()&0111 != 0 != a.Executable {
			return Candidate{}, fmt.Errorf("bundle mode mismatch: %s", a.Path)
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			return Candidate{}, e
		}
		h := sha256.Sum256(raw)
		if hex.EncodeToString(h[:]) != a.SHA256 {
			return Candidate{}, fmt.Errorf("bundle digest mismatch: %s", a.Path)
		}
	}
	listed := map[string]bool{}
	for _, a := range m.Assets {
		listed[a.Path] = true
	}
	if e = filepath.WalkDir(bundle, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == bundle {
			return nil
		}
		rel, _ := filepath.Rel(bundle, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			return nil
		}
		if rel == "manifest.json" {
			return nil
		}
		if !listed[rel] {
			return fmt.Errorf("unlisted bundle file %s", rel)
		}
		return nil
	}); e != nil {
		return Candidate{}, e
	}
	d, e := Digest(m)
	if e != nil {
		return Candidate{}, e
	}
	snap, e := snapshot(bundle, m)
	return Candidate{BundlePath: bundle, Manifest: m, Digest: d, SnapshotSHA256: snap}, e
}
func snapshot(root string, m CandidateManifest) (string, error) {
	type item struct {
		Path, SHA  string
		Executable bool
	}
	xs := make([]item, 0, len(m.Assets))
	for _, a := range m.Assets {
		xs = append(xs, item{a.Path, a.SHA256, a.Executable})
	}
	sort.Slice(xs, func(i, j int) bool { return xs[i].Path < xs[j].Path })
	return Digest(xs)
}

// Check evaluates only policy-declared deterministic local predicates. Missing
// or unsupported evidence is unknown and can never be treated as passing.
func Check(c Candidate, p VerificationPolicy) (Report, error) {
	if e := ValidatePolicy(p); e != nil {
		return Report{}, e
	}
	if c.BundlePath == "" {
		return Report{}, errors.New("candidate must be loaded from a private imported bundle")
	}
	if c.BundlePath != "" {
		fresh, err := LoadCandidate(c.BundlePath)
		if err != nil {
			return Report{}, err
		}
		if fresh.Digest != c.Digest || fresh.SnapshotSHA256 != c.SnapshotSHA256 {
			return Report{}, errors.New("candidate bundle changed since import")
		}
		c = fresh
	}
	pd, e := Digest(p)
	if e != nil {
		return Report{}, e
	}
	r := Report{CandidateSHA256: c.Digest, PolicySHA256: pd, Disposition: "accepted"}
	assets := map[string]Asset{}
	for _, a := range c.Manifest.Assets {
		assets[a.Path] = a
	}
	for _, q := range p.Criteria {
		v := PredicateResult{CriterionID: q.ID, Result: "unknown"}
		switch q.Kind {
		case "asset-present":
			if _, ok := assets[q.Expected]; ok {
				v.Result = "pass"
				v.Evidence = []string{q.Expected}
			} else {
				v.Result = "fail"
			}
		case "asset-sha256":
			if a, ok := assets[q.Expected]; ok {
				v.Result = "pass"
				v.Evidence = []string{a.Path + "#sha256=" + a.SHA256}
			} else {
				v.Result = "fail"
			}
		case "evidence-resolves":
			found := false
			for _, x := range c.Manifest.Evidence {
				if x == q.Expected {
					if _, ok := assets[x]; ok {
						found = true
					}
				}
			}
			if found {
				v.Result = "pass"
				v.Evidence = []string{q.Expected}
			} else {
				v.Result = "fail"
			}
		case "scope-equals":
			if c.Manifest.Repository == q.Expected {
				v.Result = "pass"
				v.Evidence = []string{"repository=" + c.Manifest.Repository}
			} else {
				v.Result = "fail"
			}
		case "command":
			v.Rationale = "command verifier requires independently qualified controls and an execution receipt"
		case "inventory-complete":
			complete := len(c.Manifest.Assets) > 0
			for _, a := range c.Manifest.Assets {
				if a.Role == "" || !isHash(a.SHA256) {
					complete = false
				}
			}
			if complete {
				v.Result = "pass"
				v.Evidence = []string{"candidate-inventory"}
			} else {
				v.Result = "fail"
			}
		case "role-covered":
			found := false
			for _, a := range c.Manifest.Assets {
				if a.Role == q.Expected {
					found = true
					v.Evidence = append(v.Evidence, a.Path)
				}
			}
			if found {
				v.Result = "pass"
			} else {
				v.Result = "fail"
			}
		}
		if v.Result != "pass" && q.Required {
			r.Disposition = "blocked"
		}
		r.Results = append(r.Results, v)
	}
	return r, nil
}

type Controls interface {
	Proof() ControlProof
	Execute(context.Context, []string, map[string]string, time.Duration, int64) (int, []byte, error)
}

// RunCommand delegates only to an explicitly qualified injected isolation control.
// This package never starts a host process itself.
func RunCommand(ctx context.Context, c Candidate, p VerificationPolicy, id string, controls Controls) ([]byte, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	if e := ValidatePolicy(p); e != nil {
		return nil, 0, e
	}
	var spec *VerifierSpec
	for i := range p.Verifiers {
		if p.Verifiers[i].ID == id {
			spec = &p.Verifiers[i]
		}
	}
	if spec == nil {
		return nil, 0, errors.New("verifier is not pinned by policy")
	}
	if controls == nil {
		return nil, 0, errors.New("verifier controls are not qualified")
	}
	if spec.Kind != "command" || len(spec.Argv) == 0 {
		return nil, 0, errors.New("verifier is not a pinned command")
	}
	if c.BundlePath == "" {
		return nil, 0, errors.New("command verifier requires an imported immutable candidate bundle")
	}
	before, err := LoadCandidate(c.BundlePath)
	if err != nil || before.Digest != c.Digest || before.SnapshotSHA256 != c.SnapshotSHA256 {
		return nil, 0, errors.New("candidate changed before verifier launch")
	}
	pd, err := Digest(p)
	if err != nil {
		return nil, 0, err
	}
	sd, err := Digest(*spec)
	if err != nil {
		return nil, 0, err
	}
	proof := controls.Proof()
	if proof.CandidateSHA256 != c.Digest || proof.SnapshotSHA256 != c.SnapshotSHA256 || proof.Scope != "candidate-offline" {
		return nil, 0, errors.New("control proof does not bind candidate scope")
	}
	if err := verifyControlProof(proof, pd, sd, p.Issuers, time.Now().UTC()); err != nil {
		return nil, 0, err
	}
	bounded, cancel := context.WithTimeout(ctx, time.Duration(spec.TimeoutSeconds)*time.Second)
	defer cancel()
	code, out, e := controls.Execute(bounded, spec.Argv, spec.Environment, time.Duration(spec.TimeoutSeconds)*time.Second, spec.MaxOutputBytes)
	after, loadErr := LoadCandidate(c.BundlePath)
	if loadErr != nil || after.Digest != c.Digest || after.SnapshotSHA256 != c.SnapshotSHA256 {
		return nil, code, errors.New("candidate changed during verifier execution")
	}
	if ctx.Err() != nil {
		return nil, code, ctx.Err()
	}
	if int64(len(out)) > spec.MaxOutputBytes {
		return nil, code, errors.New("verifier output exceeded policy limit")
	}
	return out, code, e
}

func SignVerification(r VerificationReceipt, key ed25519.PrivateKey) (VerificationReceipt, error) {
	if err := validatePrivateKey(key); err != nil {
		return r, err
	}
	r.Signature = ""
	r.Version = Version
	b, e := json.Marshal(r)
	if e != nil {
		return r, e
	}
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, b))
	return r, nil
}
func SignReview(r ReviewReceipt, key ed25519.PrivateKey) (ReviewReceipt, error) {
	if err := validatePrivateKey(key); err != nil {
		return r, err
	}
	r.Signature = ""
	r.Version = Version
	b, e := json.Marshal(r)
	if e != nil {
		return r, e
	}
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, b))
	return r, nil
}
func SignControlProof(p ControlProof, key ed25519.PrivateKey) (ControlProof, error) {
	if err := validatePrivateKey(key); err != nil {
		return p, err
	}
	p.Signature = ""
	p.Version = Version
	b, e := json.Marshal(p)
	if e != nil {
		return p, e
	}
	p.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, b))
	return p, nil
}

// VerifyControlProof verifies externally issued control evidence. It creates no
// enforcement, qualification or acceptance and installs no trust roots.
func VerifyControlProof(p ControlProof, policyDigest, verifierDigest string, issuers []TrustedIssuer, asOf time.Time) error {
	return verifyControlProof(p, policyDigest, verifierDigest, issuers, asOf)
}

func verifyControlProof(p ControlProof, policyDigest, verifierDigest string, issuers []TrustedIssuer, asOf time.Time) error {
	if p.Version != Version || p.PolicySHA256 != policyDigest || p.VerifierSHA256 != verifierDigest {
		return errors.New("control proof does not bind policy and verifier")
	}
	observed, e := time.Parse(time.RFC3339, p.ObservedAt)
	if e != nil || observed.After(asOf) {
		return errors.New("invalid or future control-proof timestamp")
	}
	until, e := time.Parse(time.RFC3339, p.ValidUntil)
	if e != nil || asOf.After(until) || !until.After(observed) {
		return errors.New("control proof is expired or invalid")
	}
	required := map[string]bool{"filesystem-isolation": false, "readonly-candidate": false, "network-deny": false, "credential-isolation": false, "time-bound": false, "output-bound": false}
	for _, c := range p.Controls {
		if _, ok := required[c]; ok {
			required[c] = true
		}
	}
	for _, ok := range required {
		if !ok {
			return errors.New("control proof lacks required isolation bounds")
		}
	}
	trustedExecutor := ""
	for _, i := range issuers {
		if i.ID == p.Issuer && i.Role == "controls" {
			trustedExecutor = i.ExecutorSHA256
		}
	}
	if trustedExecutor == "" || p.ExecutorSHA256 != trustedExecutor {
		return errors.New("executor identity is not approved by trusted controls policy")
	}
	return verifySignedRole(p, p.Signature, p.Issuer, "controls", issuers)
}
func VerifyVerification(r VerificationReceipt, issuers []TrustedIssuer) error {
	if r.Version != Version || r.SnapshotBefore == "" || r.SnapshotBefore != r.SnapshotAfter {
		return errors.New("verification receipt is incomplete or source changed")
	}
	return verifySignedRole(r, r.Signature, r.VerifierID, "verifier", issuers)
}
func findVerifier(p VerificationPolicy, id string) *VerifierSpec {
	for i := range p.Verifiers {
		if p.Verifiers[i].ID == id {
			return &p.Verifiers[i]
		}
	}
	return nil
}
func trustedPrincipal(issuers []TrustedIssuer, id, role string) string {
	for _, i := range issuers {
		if i.ID == id && i.Role == role {
			return i.Principal
		}
	}
	return ""
}
func SignAcceptance(r AcceptanceReceipt, key ed25519.PrivateKey) (AcceptanceReceipt, error) {
	if err := validatePrivateKey(key); err != nil {
		return r, err
	}
	r.Signature = ""
	r.Version = Version
	r.Disposition = "accepted"
	b, e := json.Marshal(r)
	if e != nil {
		return r, e
	}
	r.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, b))
	return r, nil
}
func validatePrivateKey(key ed25519.PrivateKey) error {
	if len(key) != ed25519.PrivateKeySize {
		return errors.New("invalid Ed25519 private key length")
	}
	derived := ed25519.NewKeyFromSeed(key[:ed25519.SeedSize])
	if !bytes.Equal(derived, key) {
		return errors.New("inconsistent Ed25519 private key")
	}
	return nil
}
func verifySigned(v any, sig, issuer string, issuers []TrustedIssuer) error {
	return verifySignedRole(v, sig, issuer, "", issuers)
}
func verifySignedRole(v any, sig, issuer, role string, issuers []TrustedIssuer) error {
	raw, e := base64.StdEncoding.DecodeString(sig)
	if e != nil {
		return errors.New("invalid signature encoding")
	}
	switch x := v.(type) {
	case VerificationReceipt:
		x.Signature = ""
		v = x
	case AcceptanceReceipt:
		x.Signature = ""
		v = x
	case ReviewReceipt:
		x.Signature = ""
		v = x
	case ControlProof:
		x.Signature = ""
		v = x
	}
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	for _, i := range issuers {
		if i.ID != issuer || (role != "" && i.Role != role) {
			continue
		}
		pk, e := base64.StdEncoding.DecodeString(i.PublicKey)
		if e == nil && len(pk) == ed25519.PublicKeySize && ed25519.Verify(ed25519.PublicKey(pk), b, raw) {
			return nil
		}
	}
	return errors.New("signature is not trusted")
}
func VerifyAcceptance(r AcceptanceReceipt, issuers []TrustedIssuer, asOf time.Time, maxAge time.Duration) error {
	if r.Version != Version || r.Disposition != "accepted" {
		return errors.New("acceptance is not accepted")
	}
	issued, e := time.Parse(time.RFC3339, r.IssuedAt)
	if e != nil || asOf.Before(issued) {
		return errors.New("invalid acceptance time")
	}
	if r.ValidUntil != "" {
		until, e := time.Parse(time.RFC3339, r.ValidUntil)
		if e != nil || asOf.After(until) {
			return errors.New("acceptance expired")
		}
	}
	if maxAge > 0 && asOf.Sub(issued) > maxAge {
		return errors.New("acceptance stale")
	}
	return verifySignedRole(r, r.Signature, r.Issuer, "acceptance", issuers)
}
func IssueAcceptance(c Candidate, p VerificationPolicy, verifications []VerificationReceipt, reviews []ReviewReceipt, issuer string, at time.Time, validUntil string, key ed25519.PrivateKey) (AcceptanceReceipt, error) {
	if at.IsZero() {
		return AcceptanceReceipt{}, errors.New("trusted acceptance time is required")
	}
	if err := ValidatePolicy(p); err != nil {
		return AcceptanceReceipt{}, err
	}
	if (p.RequireQuiescent || c.Manifest.Origin == "runtime") && !c.Manifest.Quiescent {
		return AcceptanceReceipt{}, errors.New("runtime candidate is not quiescent")
	}
	if c.BundlePath == "" {
		return AcceptanceReceipt{}, errors.New("acceptance requires a private imported candidate bundle")
	}
	if c.BundlePath != "" {
		fresh, e := LoadCandidate(c.BundlePath)
		if e != nil || fresh.Digest != c.Digest || fresh.SnapshotSHA256 != c.SnapshotSHA256 {
			return AcceptanceReceipt{}, errors.New("candidate changed before acceptance")
		}
	}
	report, err := Check(c, p)
	if err != nil {
		return AcceptanceReceipt{}, err
	}
	pdCheck, err := Digest(p)
	if err != nil {
		return AcceptanceReceipt{}, err
	}
	byCriterion := map[string]PredicateResult{}
	ids := []string{}
	verificationHashes := []string{}
	reviewHashes := []string{}
	seenIDs := map[string]bool{}
	verifierPrincipals := map[string]bool{}
	evidenceExpiry := at.Add(time.Duration(p.MaxAgeSeconds) * time.Second)
	for _, vr := range verifications {
		if e := VerifyVerification(vr, p.Issuers); e != nil {
			return AcceptanceReceipt{}, e
		}
		if vr.CandidateSHA256 != c.Digest || vr.PolicySHA256 != pdCheck || vr.SnapshotBefore != c.SnapshotSHA256 || vr.SnapshotAfter != c.SnapshotSHA256 {
			return AcceptanceReceipt{}, errors.New("verification receipt binding mismatch")
		}
		observed, e := time.Parse(time.RFC3339, vr.ObservedAt)
		if e != nil || observed.After(at) {
			return AcceptanceReceipt{}, errors.New("verification receipt has invalid or future observation time")
		}
		if p.MaxAgeSeconds > 0 && at.Sub(observed) > time.Duration(p.MaxAgeSeconds)*time.Second {
			return AcceptanceReceipt{}, errors.New("verification receipt is stale")
		}
		if candidateExpiry := observed.Add(time.Duration(p.MaxAgeSeconds) * time.Second); candidateExpiry.Before(evidenceExpiry) {
			evidenceExpiry = candidateExpiry
		}
		spec := findVerifier(p, vr.VerifierID)
		if spec == nil || spec.RubricSHA256 != vr.RubricSHA256 || spec.ImplementationSHA256 != vr.ImplementationSHA256 {
			return AcceptanceReceipt{}, errors.New("verification receipt verifier pin mismatch")
		}
		expectedEnv, e := Digest(struct {
			Toolchain   string
			Environment map[string]string
		}{spec.Toolchain, spec.Environment})
		if e != nil {
			return AcceptanceReceipt{}, e
		}
		if vr.EnvironmentSHA256 != expectedEnv {
			return AcceptanceReceipt{}, errors.New("verification environment does not match pinned policy")
		}
		expectedSpec, e := Digest(*spec)
		if e != nil {
			return AcceptanceReceipt{}, e
		}
		if vr.VerifierSpecSHA256 != expectedSpec {
			return AcceptanceReceipt{}, errors.New("verification receipt does not bind full verifier specification")
		}
		if seenIDs[vr.ID] {
			return AcceptanceReceipt{}, errors.New("duplicate verification receipt")
		}
		seenIDs[vr.ID] = true
		ids = append(ids, vr.ID)
		verificationDigest, e := Digest(vr)
		if e != nil {
			return AcceptanceReceipt{}, e
		}
		verificationHashes = append(verificationHashes, verificationDigest)
		if vr.Principal == "" || trustedPrincipal(p.Issuers, vr.VerifierID, "verifier") != vr.Principal {
			return AcceptanceReceipt{}, errors.New("verifier principal does not match protected issuer policy")
		}
		for _, criterion := range p.Criteria {
			if criterion.VerifierID == vr.VerifierID && criterion.Independent && vr.Principal == c.Manifest.AuthorPrincipal {
				return AcceptanceReceipt{}, errors.New("independent verifier is the candidate author")
			}
		}
		verifierPrincipals[vr.Principal] = true
		resultSeen := map[string]bool{}
		for _, res := range vr.Results {
			if resultSeen[res.CriterionID] {
				return AcceptanceReceipt{}, errors.New("duplicate verifier predicate result")
			}
			resultSeen[res.CriterionID] = true
			var criterion *Criterion
			for i := range p.Criteria {
				if p.Criteria[i].ID == res.CriterionID {
					criterion = &p.Criteria[i]
				}
			}
			if criterion == nil || criterion.Kind != "command" || criterion.VerifierID != vr.VerifierID {
				return AcceptanceReceipt{}, errors.New("verifier result does not match configured command criterion")
			}
			if _, duplicate := byCriterion[res.CriterionID]; duplicate {
				return AcceptanceReceipt{}, errors.New("multiple receipts assert the same criterion; select an explicitly approved evidence set")
			}
			byCriterion[res.CriterionID] = res
		}
	}
	for i := range report.Results {
		if result, ok := byCriterion[report.Results[i].CriterionID]; ok {
			report.Results[i] = result
		}
	}
	independent := false
	for _, criterion := range p.Criteria {
		if criterion.Independent {
			independent = true
		}
	}
	if independent || p.ReviewMinimum > 0 {
		boundAuthor := false
		for _, binding := range p.AuthorBindings {
			if binding.Principal == c.Manifest.AuthorPrincipal && ((binding.AssignmentSHA256 != "" && binding.AssignmentSHA256 == c.Manifest.AssignmentSHA256) || (binding.CandidateSHA256 != "" && binding.CandidateSHA256 == c.Digest)) {
				boundAuthor = true
			}
		}
		if !boundAuthor {
			return AcceptanceReceipt{}, errors.New("candidate author is not bound by protected policy")
		}
		reviewCount := 0
		reviewers := map[string]bool{}
		for _, rr := range reviews {
			if rr.Version != Version || rr.CandidateSHA256 != c.Digest || rr.SnapshotSHA256 != c.SnapshotSHA256 || rr.PolicySHA256 != pdCheck || rr.Disposition != "pass" || rr.Principal == "" || rr.Principal != trustedPrincipal(p.Issuers, rr.Issuer, "reviewer") || rr.Principal == c.Manifest.AuthorPrincipal || (independent && verifierPrincipals[rr.Principal]) || reviewers[rr.Principal] {
				return AcceptanceReceipt{}, errors.New("review receipt is invalid or not independent")
			}
			if e := verifySignedRole(rr, rr.Signature, rr.Issuer, "reviewer", p.Issuers); e != nil {
				return AcceptanceReceipt{}, e
			}
			reviewed, e := time.Parse(time.RFC3339, rr.ObservedAt)
			if e != nil || reviewed.After(at) || (p.MaxAgeSeconds > 0 && at.Sub(reviewed) > time.Duration(p.MaxAgeSeconds)*time.Second) {
				return AcceptanceReceipt{}, errors.New("review receipt has invalid or stale observation time")
			}
			if reviewExpiry := reviewed.Add(time.Duration(p.MaxAgeSeconds) * time.Second); reviewExpiry.Before(evidenceExpiry) {
				evidenceExpiry = reviewExpiry
			}
			reviewers[rr.Principal] = true
			reviewDigest, e := Digest(rr)
			if e != nil {
				return AcceptanceReceipt{}, e
			}
			reviewHashes = append(reviewHashes, reviewDigest)
			reviewCount++
		}
		minimum := p.ReviewMinimum
		if independent && minimum < 1 {
			minimum = 1
		}
		if reviewCount < minimum {
			return AcceptanceReceipt{}, errors.New("independent review coverage is insufficient")
		}
	}
	if len(report.Results) != len(p.Criteria) {
		return AcceptanceReceipt{}, errors.New("verification report does not cover exact policy")
	}
	seen := map[string]bool{}
	for _, v := range report.Results {
		if seen[v.CriterionID] {
			return AcceptanceReceipt{}, errors.New("duplicate criterion result")
		}
		seen[v.CriterionID] = true
		var criterion *Criterion
		for i := range p.Criteria {
			if p.Criteria[i].ID == v.CriterionID {
				criterion = &p.Criteria[i]
			}
		}
		if criterion == nil {
			return AcceptanceReceipt{}, errors.New("report contains unknown criterion")
		}
		if criterion.Required && v.Result != "pass" {
			return AcceptanceReceipt{}, errors.New("required criterion did not pass")
		}
		if v.Result != "pass" && v.Result != "fail" && v.Result != "unknown" && v.Result != "not-applicable" {
			return AcceptanceReceipt{}, errors.New("invalid criterion result")
		}
		if v.Result == "not-applicable" && (!criterion.AllowNotApplicable || strings.TrimSpace(v.Rationale) == "") {
			return AcceptanceReceipt{}, errors.New("criterion exemption is not allowed or lacks rationale")
		}
	}
	for _, criterion := range p.Criteria {
		if !seen[criterion.ID] {
			return AcceptanceReceipt{}, errors.New("verification report omits a policy criterion")
		}
	}
	pd := pdCheck
	cd, e := Digest(p.Criteria)
	if e != nil {
		return AcceptanceReceipt{}, e
	}
	r := AcceptanceReceipt{ID: "accept-" + c.Digest[:16], CandidateSHA256: c.Digest, CandidateRevision: c.Manifest.CandidateRevision, SnapshotSHA256: c.SnapshotSHA256, RunID: c.Manifest.RunID, StepID: c.Manifest.StepID, AttemptID: c.Manifest.AttemptID, Fence: c.Manifest.Fence, InputSHA256: c.Manifest.InputSHA256, PolicySHA256: pd, PlanSHA256: c.Manifest.PlanSHA256, GoalSHA256: c.Manifest.GoalSHA256, CriteriaSHA256: cd, VerificationIDs: ids, Results: append([]PredicateResult(nil), report.Results...), VerificationSHA256s: verificationHashes, ReviewSHA256s: reviewHashes, Disposition: "accepted", Issuer: issuer, IssuedAt: at.UTC().Format(time.RFC3339), ValidUntil: validUntil}
	if validUntil != "" {
		requested, e := time.Parse(time.RFC3339, validUntil)
		if e != nil || !requested.After(at) || requested.After(evidenceExpiry) {
			return AcceptanceReceipt{}, errors.New("acceptance validity exceeds evidence freshness")
		}
	} else {
		if !evidenceExpiry.After(at) {
			return AcceptanceReceipt{}, errors.New("required evidence has expired")
		}
		validUntil = evidenceExpiry.UTC().Format(time.RFC3339)
	}
	r.ValidUntil = validUntil
	r, err = SignAcceptance(r, key)
	if err != nil {
		return AcceptanceReceipt{}, err
	}
	if err = verifySignedRole(r, r.Signature, issuer, "acceptance", p.Issuers); err != nil {
		return AcceptanceReceipt{}, err
	}
	if err = ValidateAcceptanceBinding(r, c, p); err != nil {
		return AcceptanceReceipt{}, err
	}
	return r, nil
}

// ValidateAcceptanceBinding checks the receipt against the exact candidate and
// policy before a runtime projection may consider it. Trust and revocation
// generations remain inputs owned by the caller's protected policy store.
func ValidateAcceptanceBinding(r AcceptanceReceipt, c Candidate, p VerificationPolicy) error {
	if c.BundlePath == "" {
		return errors.New("acceptance candidate has no private bundle")
	}
	if c.BundlePath != "" {
		fresh, err := LoadCandidate(c.BundlePath)
		if err != nil || fresh.Digest != c.Digest || fresh.SnapshotSHA256 != c.SnapshotSHA256 {
			return errors.New("candidate bundle changed since receipt issuance")
		}
	}
	if r.CandidateSHA256 != c.Digest || r.SnapshotSHA256 != c.SnapshotSHA256 || r.CandidateRevision != c.Manifest.CandidateRevision || r.RunID != c.Manifest.RunID || r.StepID != c.Manifest.StepID || r.AttemptID != c.Manifest.AttemptID || r.Fence != c.Manifest.Fence || r.InputSHA256 != c.Manifest.InputSHA256 {
		return errors.New("acceptance does not bind the current candidate lineage")
	}
	return ValidateAcceptanceManifestBinding(r, c.Manifest, c.Digest, c.SnapshotSHA256, p)
}

// ValidateAcceptanceManifestBinding validates receipt identity against a
// manifest already authenticated by the caller's runtime authority. It does
// not validate the manifest's asset bytes; use ValidateAcceptanceBinding when
// the private candidate bundle is available.
func ValidateAcceptanceManifestBinding(r AcceptanceReceipt, m CandidateManifest, candidateDigest, snapshotDigest string, p VerificationPolicy) error {
	if err := ValidateManifest(m); err != nil {
		return err
	}
	d, err := Digest(m)
	if err != nil || d != candidateDigest {
		return errors.New("candidate manifest digest mismatch")
	}
	expectedSnapshot, err := snapshot("", m)
	if err != nil || expectedSnapshot != snapshotDigest {
		return errors.New("candidate snapshot digest mismatch")
	}
	if r.CandidateSHA256 != candidateDigest || r.SnapshotSHA256 != snapshotDigest || r.CandidateRevision != m.CandidateRevision || r.RunID != m.RunID || r.StepID != m.StepID || r.AttemptID != m.AttemptID || r.Fence != m.Fence || r.InputSHA256 != m.InputSHA256 {
		return errors.New("acceptance does not bind the current candidate lineage")
	}
	pd, err := Digest(p)
	if err != nil {
		return err
	}
	if r.PolicySHA256 != pd {
		return errors.New("acceptance policy digest mismatch")
	}
	cd, err := Digest(p.Criteria)
	if err != nil {
		return err
	}
	if r.CriteriaSHA256 != cd {
		return errors.New("acceptance criteria digest mismatch")
	}
	if err := validateAcceptanceCoverage(r, p); err != nil {
		return err
	}
	if r.PlanSHA256 != m.PlanSHA256 || r.GoalSHA256 != m.GoalSHA256 {
		return errors.New("acceptance plan or goal digest mismatch")
	}
	return nil
}

func validateAcceptanceCoverage(r AcceptanceReceipt, p VerificationPolicy) error {
	if len(r.Results) != len(p.Criteria) {
		return errors.New("acceptance omits criterion result coverage")
	}
	criteria := map[string]Criterion{}
	for _, c := range p.Criteria {
		criteria[c.ID] = c
	}
	seen := map[string]bool{}
	for _, result := range r.Results {
		c, ok := criteria[result.CriterionID]
		if !ok || seen[result.CriterionID] {
			return errors.New("acceptance has unknown or duplicate criterion result")
		}
		seen[result.CriterionID] = true
		if c.Required && result.Result != "pass" {
			return errors.New("acceptance has a nonpassing required criterion")
		}
		if result.Result != "pass" && result.Result != "fail" && result.Result != "unknown" && result.Result != "not-applicable" {
			return errors.New("acceptance has invalid criterion result")
		}
		if result.Result == "not-applicable" && (!c.AllowNotApplicable || strings.TrimSpace(result.Rationale) == "") {
			return errors.New("acceptance contains an unauthorized criterion exemption")
		}
	}
	if len(r.VerificationIDs) != len(r.VerificationSHA256s) {
		return errors.New("acceptance verification references are incomplete")
	}
	if err := uniqueDigests(r.VerificationSHA256s); err != nil {
		return err
	}
	if err := uniqueDigests(r.ReviewSHA256s); err != nil {
		return err
	}
	independent := false
	for _, c := range p.Criteria {
		if c.Independent {
			independent = true
			if c.Kind == "command" && len(r.VerificationSHA256s) == 0 {
				return errors.New("independent verifier receipt reference is missing")
			}
		}
	}
	minimum := p.ReviewMinimum
	if independent && minimum < 1 {
		minimum = 1
	}
	if len(r.ReviewSHA256s) < minimum {
		return errors.New("acceptance review receipt references are insufficient")
	}
	return nil
}
func uniqueDigests(digests []string) error {
	seen := map[string]bool{}
	for _, d := range digests {
		if !isHash(d) || seen[d] {
			return errors.New("acceptance evidence references contain an invalid or duplicate digest")
		}
		seen[d] = true
	}
	return nil
}

// BuildHandoff creates a local-only, reviewable handoff. Actions are restricted
// to the repository-delivery engine's non-publishing preparation steps.
func BuildHandoff(c Candidate, acceptance AcceptanceReceipt, acceptanceDigest, destinationSnapshot string, receipts []ExistingReceipt) (DeliveryHandoff, error) {
	if acceptance.Disposition != "accepted" || acceptance.CandidateSHA256 != c.Digest || acceptance.SnapshotSHA256 != c.SnapshotSHA256 || !isHash(acceptanceDigest) {
		return DeliveryHandoff{}, errors.New("handoff requires current accepted candidate")
	}
	for _, r := range receipts {
		if len(r.ReceiptBytes) == 0 || !isHash(r.Digest) || r.Digest != hashBytes(r.ReceiptBytes) || r.CandidateSHA256 != c.Digest || r.SnapshotSHA256 != c.SnapshotSHA256 {
			return DeliveryHandoff{}, errors.New("delivery receipt does not bind candidate snapshot")
		}
	}
	return DeliveryHandoff{Version: Version, ID: "handoff-" + c.Digest[:16], CandidateSHA256: c.Digest, AcceptanceSHA256: acceptanceDigest, SourceSnapshotSHA256: c.SnapshotSHA256, DestinationSnapshotSHA256: destinationSnapshot, Receipts: receipts, Actions: []string{"preserve", "prepare-isolated-handoff"}}, nil
}
func ValidateHandoff(h DeliveryHandoff) (DeliveryProof, error) {
	proof := DeliveryProof{Version: Version, Valid: false}
	fail := func(s string) { proof.Valid = false; proof.Diagnostics = append(proof.Diagnostics, s) }
	if h.Version != Version || !isHash(h.CandidateSHA256) || !isHash(h.AcceptanceSHA256) || !isHash(h.SourceSnapshotSHA256) {
		fail("invalid handoff identity")
	}
	if h.SourceSnapshotSHA256 != h.DestinationSnapshotSHA256 {
		fail("destination snapshot differs; verification and review must be rerun")
	}
	for _, a := range h.Actions {
		if a != "preserve" && a != "prepare-isolated-handoff" {
			fail("handoff contains an unauthorized action")
		}
	}
	for _, r := range h.Receipts {
		if len(r.ReceiptBytes) == 0 || !isHash(r.Digest) || r.Digest != hashBytes(r.ReceiptBytes) || r.CandidateSHA256 != h.CandidateSHA256 || r.SnapshotSHA256 != h.SourceSnapshotSHA256 {
			fail("delivery receipt snapshot mismatch")
		}
	}
	fail("existing repository-delivery receipt schemas are not yet recomputed; handoff remains a draft")
	d, _ := Digest(h)
	proof.HandoffSHA256 = d
	if !proof.Valid {
		return proof, errors.New(strings.Join(proof.Diagnostics, "; "))
	}
	return proof, nil
}

func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
