package evaluation

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

type VerifiedEvidence struct {
	policy     EvidencePolicy
	policyHash string
	rows       []OutcomeRecord
	provenance []string
	seal       [32]byte
}

func PolicySHA256(v *VerifiedEvidence) string {
	if v == nil {
		return ""
	}
	return v.policyHash
}

// EvidenceSHA256 identifies the verified policy and all loaded signed shard
// bytes without exposing outcome records or signer material.
func EvidenceSHA256(v *VerifiedEvidence) string {
	if Validate(v) != nil {
		return ""
	}
	return hex.EncodeToString(v.seal[:])
}

func Load(manifestPaths []string, policyPath string) (*VerifiedEvidence, error) {
	pb, err := safeReadFile(policyPath)
	if err != nil {
		return nil, err
	}
	var p EvidencePolicy
	if err = validateSchema(pb, "policy.json"); err != nil {
		return nil, err
	}
	if err = decode(pb, &p); err != nil {
		return nil, err
	}
	if err = validatePolicy(p); err != nil {
		return nil, err
	}
	ph := digest(pb)
	keys := map[string]Issuer{}
	for _, i := range p.Issuers {
		keys[i.ID] = i
	}
	v := &VerifiedEvidence{policy: p, policyHash: ph}
	seenAssets := map[string]bool{}
	seenIDs := map[string]bool{}
	seenVerifierUses := map[struct{ issuer, task, input, output, repetition string }]bool{}
	for _, mp := range manifestPaths {
		mb, e := safeReadFile(mp)
		if e != nil {
			return nil, e
		}
		var m ShardManifest
		if e = validateSchema(mb, "manifest.json"); e != nil {
			return nil, e
		}
		if e = decode(mb, &m); e != nil {
			return nil, e
		}
		if m.Version != Version || !portableAsset(m.Asset) || !validHash(m.SHA256) {
			return nil, errors.New("invalid shard manifest")
		}
		iss, ok := keys[m.Issuer]
		if !ok || !hasRole(iss, "shard") {
			return nil, errors.New("manifest issuer lacks shard role")
		}
		if !verifyJSONSignature(m, m.Signature, iss.PublicKey) {
			return nil, errors.New("invalid shard signature")
		}
		assetID := filepath.Clean(filepath.Join(filepath.Dir(mp), m.Asset))
		if seenAssets[assetID] {
			return nil, errors.New("duplicate shard asset")
		}
		seenAssets[assetID] = true
		b, e := modelcatalog.ReadAsset(filepath.Dir(mp), m.Asset)
		if e != nil {
			return nil, e
		}
		if digest(b) != m.SHA256 {
			return nil, errors.New("shard hash mismatch")
		}
		var rows []OutcomeRecord
		if e = validateSchema(b, "shard.json"); e != nil {
			return nil, e
		}
		if e = decode(b, &rows); e != nil {
			return nil, e
		}
		for _, r := range rows {
			if e = validateOutcome(r, p, keys); e != nil {
				return nil, e
			}
			if seenIDs[r.ID] {
				return nil, errors.New("duplicate outcome id")
			}
			seenIDs[r.ID] = true
			if r.Receipt != nil {
				key := struct{ issuer, task, input, output, repetition string }{r.Receipt.Issuer, r.TaskSHA256, r.InputSHA256, r.OutputSHA256, r.RepetitionID}
				if seenVerifierUses[key] {
					return nil, errors.New("verifier receipt replay across outcomes")
				}
				seenVerifierUses[key] = true
			}
			v.rows = append(v.rows, r)
		}
		v.provenance = append(v.provenance, digest(mb), digest(b))
	}
	groups := map[string]string{}
	for _, r := range v.rows {
		for _, key := range []string{"family:" + r.FamilyID, "repo:" + r.Repository, "near:" + r.NearDuplicateGroup} {
			if strings.HasSuffix(key, ":") {
				continue
			}
			if prior, ok := groups[key]; ok && prior != r.Partition {
				return nil, errors.New("partition leakage detected")
			}
			groups[key] = r.Partition
		}
	}
	sort.Strings(v.provenance)
	v.seal = v.computeSeal()
	return v, nil
}

func decode(data []byte, target any) error {
	if err := contracts.ValidateJSON(data); err != nil {
		return err
	}
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.UseNumber()
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON value")
	}
	return nil
}

//go:embed schemas/*.json
var schemaFS embed.FS

func validateSchema(data []byte, name string) error {
	if err := contracts.ValidateJSON(data); err != nil {
		return err
	}
	raw, err := schemaFS.ReadFile("schemas/" + name)
	if err != nil {
		return err
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(evidenceDenyLoader{})
	var schema any
	if err = json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	uri := "https://aok.invalid/evaluation/" + name
	if err = c.AddResource(uri, schema); err != nil {
		return err
	}
	compiled, err := c.Compile(uri)
	if err != nil {
		return err
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err = d.Decode(&value); err != nil {
		return err
	}
	return compiled.Validate(value)
}

type evidenceDenyLoader struct{}

func (evidenceDenyLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference forbidden: %s", url)
}

func Validate(v *VerifiedEvidence) error {
	if v == nil {
		return errors.New("nil evidence")
	}
	if v.computeSeal() != v.seal {
		return errors.New("verified evidence was modified")
	}
	return nil
}

func (v *VerifiedEvidence) computeSeal() [32]byte {
	b, _ := json.Marshal(struct {
		P EvidencePolicy
		H string
		R []OutcomeRecord
		S []string
	}{v.policy, v.policyHash, v.rows, v.provenance})
	return sha256.Sum256(b)
}

func Estimate(v *VerifiedEvidence, cohort Cohort, asOf time.Time) (EstimateArtifact, error) {
	if err := Validate(v); err != nil {
		return EstimateArtifact{}, err
	}
	audit, err := estimatePartition(v, cohort, asOf, "locked-test")
	if err != nil {
		return EstimateArtifact{}, err
	}
	audit.CalibrationPartition = "validation"
	validationFamilies := map[string]bool{}
	var validationOldest, validationLatest, auditOldest time.Time
	for _, r := range v.rows {
		if r.Cohort != cohort {
			continue
		}
		observed, _ := time.Parse(time.RFC3339, r.ObservedAt)
		if r.Partition == "validation" {
			validationFamilies[r.FamilyID] = true
			if validationOldest.IsZero() || observed.Before(validationOldest) {
				validationOldest = observed
			}
			if validationLatest.IsZero() || observed.After(validationLatest) {
				validationLatest = observed
			}
		}
		if r.Partition == "locked-test" && (auditOldest.IsZero() || observed.Before(auditOldest)) {
			auditOldest = observed
		}
	}
	audit.CalibrationFamilies = len(validationFamilies)
	// Locked-test observations measure quality only; never retain demand fitted by the audit helper.
	audit.CostUpper, audit.SecondsUpper, audit.Bounds = nil, nil, nil
	audit.Currency, audit.DemandValidUntil = "", ""
	if len(validationFamilies) < 30 {
		return audit, nil
	}
	if validationLatest.IsZero() || auditOldest.IsZero() || !validationLatest.Before(auditOldest) {
		return EstimateArtifact{}, errors.New("validation calibration must be observed strictly before locked-test audit")
	}
	calibration, err := estimatePartition(v, cohort, asOf, "validation")
	if err != nil {
		return EstimateArtifact{}, err
	}
	audit.CostUpper, audit.Currency = calibration.CostUpper, calibration.Currency
	audit.SecondsUpper = calibration.SecondsUpper
	audit.Bounds = calibration.Bounds
	audit.DemandValidUntil = calibration.DemandValidUntil
	audit.CalibrationObservedAt = calibration.ObservedAt
	audit.DemandProvenance = append([]string(nil), calibration.Provenance...)
	return audit, nil
}

func estimatePartition(v *VerifiedEvidence, cohort Cohort, asOf time.Time, partition string) (EstimateArtifact, error) {
	if err := Validate(v); err != nil {
		return EstimateArtifact{}, err
	}
	if asOf.IsZero() {
		return EstimateArtifact{}, errors.New("asOf is required")
	}
	byFamily := map[string][]OutcomeRecord{}
	var oldest, latest time.Time
	for _, r := range v.rows {
		if r.Cohort != cohort || r.Partition != partition {
			continue
		}
		observed, _ := time.Parse(time.RFC3339, r.ObservedAt)
		if oldest.IsZero() || observed.Before(oldest) {
			oldest = observed
		}
		if latest.IsZero() || observed.After(latest) {
			latest = observed
		}
		byFamily[r.FamilyID] = append(byFamily[r.FamilyID], r)
	}
	if latest.IsZero() || asOf.Before(latest) {
		return EstimateArtifact{}, errors.New("asOf precedes evidence observations")
	}
	if len(byFamily) < 30 {
		return EstimateArtifact{}, errors.New("at least 30 independent locked-test families are required")
	}
	families := make([]string, 0, len(byFamily))
	for f := range byFamily {
		families = append(families, f)
	}
	sort.Strings(families)
	good := 0
	costs := []*big.Rat{}
	seconds := []*big.Rat{}
	costComplete, secondsComplete := true, true
	currencies := map[string]bool{}
	metricFamilies := map[string][]*big.Rat{}
	metricAttributable := map[string]bool{}
	metricAttributionInitialized := map[string]bool{}
	metricSamples := map[string]int{}
	hasUnknownDemand := false
	for _, f := range families {
		rs := byFamily[f]
		expected := rs[0].RegisteredRepetitions
		got := map[string]OutcomeRecord{}
		familyGood := true
		for _, r := range rs {
			if !sameStrings(expected, r.RegisteredRepetitions) {
				return EstimateArtifact{}, errors.New("family repetition registration differs")
			}
			if _, ok := got[r.RepetitionID]; ok {
				return EstimateArtifact{}, errors.New("duplicate family repetition")
			}
			got[r.RepetitionID] = r
		}
		familyCost, familySeconds := new(big.Rat), new(big.Rat)
		costSeen, secondsSeen := 0, 0
		familyMetrics := map[string]*big.Rat{}
		familyMetricSeen := map[string]int{}
		familyMetricAttrib := map[string]bool{}
		censored := false
		for _, rep := range expected {
			r, ok := got[rep]
			if !ok {
				familyGood = false
				censored = true
				continue
			}
			if r.TerminalOutcome == "censored" {
				censored = true
			}
			if r.TerminalOutcome != "success" || r.Receipt == nil || !r.Receipt.Accepted {
				familyGood = false
			}
			if r.Cost != nil {
				x, e := usage.NumberRat(*r.Cost)
				if e != nil {
					return EstimateArtifact{}, e
				}
				familyCost.Add(familyCost, x)
				costSeen++
				currencies[r.Currency] = true
			}
			if r.Seconds != nil {
				x, e := usage.NumberRat(*r.Seconds)
				if e != nil {
					return EstimateArtifact{}, e
				}
				familySeconds.Add(familySeconds, x)
				secondsSeen++
			}
			for _, m := range r.Consumption {
				if m.Attributable && (m.Metric != "attributable" || m.Unit != "percentage-point") {
					return EstimateArtifact{}, errors.New("attributable measurement must use attributable metric and percentage-point unit")
				}
				if m.Value == nil {
					continue
				}
				x, e := usage.NumberRat(*m.Value)
				if e != nil {
					return EstimateArtifact{}, e
				}
				k := measurementKey{m.PoolID, m.WindowFamily, m.Metric, m.Unit}.string()
				if familyMetrics[k] == nil {
					familyMetrics[k] = new(big.Rat)
					familyMetricAttrib[k] = true
				}
				familyMetrics[k].Add(familyMetrics[k], x)
				familyMetricSeen[k]++
				familyMetricAttrib[k] = familyMetricAttrib[k] && m.Attributable
			}
		}
		if costSeen != len(expected) || censored {
			costComplete = false
		} else {
			costs = append(costs, new(big.Rat).Set(familyCost))
		}
		if secondsSeen != len(expected) || censored {
			secondsComplete = false
		} else {
			seconds = append(seconds, new(big.Rat).Set(familySeconds))
		}
		if censored {
			hasUnknownDemand = true
		}
		for k, val := range familyMetrics {
			if familyMetricSeen[k] == len(expected) && !censored {
				metricFamilies[k] = append(metricFamilies[k], new(big.Rat).Set(val))
				metricSamples[k]++
				if !metricAttributionInitialized[k] {
					metricAttributable[k] = familyMetricAttrib[k]
					metricAttributionInitialized[k] = true
				} else {
					metricAttributable[k] = metricAttributable[k] && familyMetricAttrib[k]
				}
			}
		}
		if familyGood {
			good++
		}
	}
	lo := wilsonLower(good, len(families))
	lo = math.Floor(lo*1e12) / 1e12
	art := EstimateArtifact{Version: Version, Cohort: cohort, Families: len(families), SuccessfulFamilies: good, QualityLower: json.Number(strconv.FormatFloat(lo, 'f', 12, 64)), ObservedAt: oldest.UTC().Format(time.RFC3339), Method: "one-sided-95%-Wilson; family-clustered; censor-as-failure; demand=sum-registered-repetitions", PolicySHA256: v.policyHash, Provenance: append([]string(nil), v.provenance...)}
	qualityAge := time.Duration(v.policy.QualityMaxAgeSeconds) * time.Second
	if qualityAge > 30*24*time.Hour {
		qualityAge = 30 * 24 * time.Hour
	}
	art.QualityValidUntil = oldest.Add(qualityAge).UTC().Format(time.RFC3339)
	demandAge := time.Duration(v.policy.DemandMaxAgeSeconds) * time.Second
	if demandAge > 7*24*time.Hour {
		demandAge = 7 * 24 * time.Hour
	}
	art.DemandValidUntil = oldest.Add(demandAge).UTC().Format(time.RFC3339)
	if costComplete && len(costs) >= 30 && len(currencies) == 1 {
		art.Currency = first(currencies)
		art.CostUpper = usageNumber(conformalRat(costs))
	}
	if secondsComplete && len(seconds) >= 30 {
		art.SecondsUpper = usageNumber(conformalRat(seconds))
	}
	keys := make([]string, 0, len(metricFamilies))
	for k := range metricFamilies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		key := parseMeasurementKey(k)
		vals := metricFamilies[k]
		bound := Bound{PoolID: key.pool, WindowFamily: key.window, Metric: key.metric, Unit: key.unit, Method: "family-conformal-95%; summed registered repetitions; censor or incomplete family makes bound unknown", Families: metricSamples[k]}
		if !hasUnknownDemand && metricSamples[k] == len(families) && len(vals) >= 30 {
			bound.Upper = usageNumber(conformalRat(vals))
		}
		if key.metric == "attributable" && key.unit == "percentage-point" && metricAttributable[k] && len(vals) >= 30 {
			bound.Method += "; attributable samples only"
		} else if key.metric == "attributable" {
			continue
		}
		art.Bounds = append(art.Bounds, bound)
	}
	return art, nil
}

func validatePolicy(p EvidencePolicy) error {
	if p.Version != Version || len(p.Issuers) == 0 {
		return errors.New("invalid evidence policy")
	}
	seen := map[string]bool{}
	roleKeys := map[string]string{}
	for _, i := range p.Issuers {
		if i.ID == "" || seen[i.ID] {
			return errors.New("duplicate or empty issuer")
		}
		seen[i.ID] = true
		if len(i.Roles) == 0 {
			return errors.New("issuer must declare a role")
		}
		for _, role := range i.Roles {
			if role != "shard" && role != "verifier" && role != "context" {
				return errors.New("unknown issuer role")
			}
		}
		if hasRole(i, "shard") && hasRole(i, "verifier") {
			return errors.New("shard and verifier roles require independent issuers")
		}
		for _, role := range i.Roles {
			if role != "shard" && role != "verifier" && role != "context" {
				return errors.New("unknown issuer role")
			}
			if role != "context" {
				if _, ok := roleKeys[role]; ok {
					return errors.New("evidence policy must define one issuer per shard/verifier role")
				}
				roleKeys[role] = i.PublicKey
			}
		}
		k, e := base64.StdEncoding.DecodeString(i.PublicKey)
		if e != nil || len(k) != ed25519.PublicKeySize || base64.StdEncoding.EncodeToString(k) != i.PublicKey {
			return errors.New("invalid issuer public key")
		}
		for _, h := range []string{i.VerifierSHA256, i.RubricSHA256} {
			if h != "" && !validHash(h) {
				return errors.New("invalid verifier or rubric pin")
			}
		}
		if hasRole(i, "verifier") && (!validHash(i.VerifierSHA256) || !validHash(i.RubricSHA256)) {
			return errors.New("verifier issuer requires verifier and rubric pins")
		}
	}
	if _, ok := roleKeys["shard"]; !ok {
		return errors.New("evidence policy requires a shard issuer")
	}
	if _, ok := roleKeys["verifier"]; !ok {
		return errors.New("evidence policy requires a verifier issuer")
	}
	if roleKeys["shard"] == roleKeys["verifier"] {
		return errors.New("shard and verifier public keys must be independent")
	}
	if p.QualityMaxAgeSeconds <= 0 || p.DemandMaxAgeSeconds <= 0 {
		return errors.New("invalid evidence freshness policy")
	}
	return nil
}

func safeReadFile(path string) ([]byte, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	for cur := abs; ; cur = filepath.Dir(cur) {
		info, e := os.Lstat(cur)
		if e != nil {
			return nil, e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, errors.New("symlink path component forbidden")
		}
		if filepath.Dir(cur) == cur {
			break
		}
	}
	return modelcatalog.ReadFile(abs)
}
func validateOutcome(r OutcomeRecord, _ EvidencePolicy, keys map[string]Issuer) error {
	if r.Version != Version || r.ID == "" || r.FamilyID == "" || r.Repository == "" || r.Partition == "" || r.RepetitionID == "" || r.Producer == "" {
		return errors.New("incomplete outcome")
	}
	for _, h := range []string{r.TaskSHA256, r.InputSHA256, r.OutputSHA256} {
		if !validHash(h) {
			return errors.New("invalid outcome hash")
		}
	}
	if !sameStringsSorted(r.RegisteredRepetitions) {
		return errors.New("repetitions must be unique and sorted")
	}
	idx := sort.SearchStrings(r.RegisteredRepetitions, r.RepetitionID)
	if idx == len(r.RegisteredRepetitions) || r.RegisteredRepetitions[idx] != r.RepetitionID {
		return errors.New("repetition is not registered")
	}
	switch r.TerminalOutcome {
	case "success", "failure", "censored":
	default:
		return errors.New("unknown terminal outcome")
	}
	if _, e := time.Parse(time.RFC3339, r.ObservedAt); e != nil {
		return errors.New("invalid observation time")
	}
	if r.Receipt != nil {
		rec := *r.Receipt
		iss, ok := keys[rec.Issuer]
		if !ok || !hasRole(iss, "verifier") || rec.VerifierSHA256 != iss.VerifierSHA256 || rec.RubricSHA256 != iss.RubricSHA256 || rec.InputSHA256 != r.InputSHA256 || rec.OutputSHA256 != r.OutputSHA256 || rec.TaskSHA256 != r.TaskSHA256 || rec.Cohort != r.Cohort || rec.FamilyID != r.FamilyID || rec.Repository != r.Repository || rec.NearDuplicateGroup != r.NearDuplicateGroup || rec.Partition != r.Partition || !sameStrings(rec.RegisteredRepetitions, r.RegisteredRepetitions) || rec.ConfigurationID != r.Cohort.ConfigurationID || rec.ProfileSHA256 != r.Cohort.ProfileSHA256 || rec.ProtocolSHA256 != r.Cohort.ProtocolSHA256 || rec.RepetitionID != r.RepetitionID || rec.ObservedAt != r.ObservedAt {
			return errors.New("receipt pins or hashes do not bind outcome")
		}
		if !verifyJSONSignature(rec, rec.Signature, iss.PublicKey) {
			return errors.New("invalid verifier receipt signature")
		}
	}
	return nil
}
func verifyJSONSignature(value any, sig, key string) bool {
	b, e := json.Marshal(value)
	if e != nil {
		return false
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return false
	}
	m["signature"] = ""
	canonical, e := json.Marshal(m)
	if e != nil {
		return false
	}
	pk, e := base64.StdEncoding.DecodeString(key)
	if e != nil {
		return false
	}
	sb, e := base64.StdEncoding.DecodeString(sig)
	return e == nil && ed25519.Verify(ed25519.PublicKey(pk), canonical, sb)
}
func portableAsset(s string) bool {
	return s != "" && !strings.HasPrefix(s, "/") && !strings.Contains(s, "\\") && !strings.Contains(s, "..") && !strings.Contains(s, ":")
}
func validHash(s string) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(b) == 32 && strings.ToLower(s) == s
}
func digest(b []byte) string { x := sha256.Sum256(b); return hex.EncodeToString(x[:]) }
func hasRole(i Issuer, r string) bool {
	for _, x := range i.Roles {
		if x == r {
			return true
		}
	}
	return false
}
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}
func sameStringsSorted(a []string) bool {
	for i, x := range a {
		if x == "" || (i > 0 && a[i-1] >= x) {
			return false
		}
	}
	return len(a) > 0
}
func first(m map[string]bool) string {
	for x := range m {
		return x
	}
	return ""
}
func wilsonLower(success, n int) float64 {
	if n == 0 {
		return 0
	}
	z := 1.6448536269514722
	p := float64(success) / float64(n)
	z2 := z * z
	return (p + z2/(2*float64(n)) - z*math.Sqrt((p*(1-p)+z2/(4*float64(n)))/float64(n))) / (1 + z2/float64(n))
}

type measurementKey struct{ pool, window, metric, unit string }

func (k measurementKey) string() string {
	b, _ := json.Marshal([]string{k.pool, k.window, k.metric, k.unit})
	return string(b)
}
func parseMeasurementKey(s string) measurementKey {
	var p []string
	_ = json.Unmarshal([]byte(s), &p)
	if len(p) != 4 {
		return measurementKey{}
	}
	return measurementKey{p[0], p[1], p[2], p[3]}
}
func conformalRat(values []*big.Rat) *big.Rat {
	sort.Slice(values, func(i, j int) bool { return values[i].Cmp(values[j]) < 0 })
	rank := (95*(len(values)+1) + 99) / 100
	if rank > len(values) {
		rank = len(values)
	}
	return values[rank-1]
}
func usageNumber(r *big.Rat) *json.Number { n := usage.Decimal(r); return &n }
