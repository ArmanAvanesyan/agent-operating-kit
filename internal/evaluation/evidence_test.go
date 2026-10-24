package evaluation

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func fixture(t *testing.T, n, reps int) (string, string, Cohort, ed25519.PrivateKey, ed25519.PrivateKey) {
	t.Helper()
	root, canonicalErr := filepath.EvalSymlinks(t.TempDir())
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	shardPub, shardPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	verifierPub, verifierPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	hash := func(s string) string { x := sha256.Sum256([]byte(s)); return hex.EncodeToString(x[:]) }
	cohort := Cohort{ConfigurationID: "cfg", ProfileSHA256: hash("profile"), TaskClass: "code", Language: "go", Risk: "low", AccountScope: "local", ProtocolSHA256: hash("protocol")}
	shardIssuer := Issuer{ID: "shard-signer", PublicKey: base64.StdEncoding.EncodeToString(shardPub), Roles: []string{"shard"}, VerifierSHA256: hash("verifier"), RubricSHA256: hash("rubric")}
	verifierIssuer := Issuer{ID: "verifier", PublicKey: base64.StdEncoding.EncodeToString(verifierPub), Roles: []string{"verifier"}, VerifierSHA256: hash("verifier"), RubricSHA256: hash("rubric")}
	contextPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	contextIssuer := Issuer{ID: "context", PublicKey: base64.StdEncoding.EncodeToString(contextPub), Roles: []string{"context"}, VerifierSHA256: hash("context-placeholder"), RubricSHA256: hash("context-rubric")}
	policy := EvidencePolicy{Version: Version, Issuers: []Issuer{shardIssuer, verifierIssuer, contextIssuer}, QualityMaxAgeSeconds: 8640000, DemandMaxAgeSeconds: 864000}
	writeJSON := func(name string, v any) string {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		p := filepath.Join(root, name)
		if e = os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
		return p
	}
	policyPath := writeJSON("policy.json", policy)
	registered := make([]string, reps)
	for i := range registered {
		registered[i] = "r" + strconv.Itoa(i+1)
	}
	now := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
	rows := make([]OutcomeRecord, 0, n*reps*2)
	for i := 0; i < n; i++ {
		for _, partition := range []string{"validation", "locked-test"} {
			family := partition + "-f" + strconv.Itoa(i)
			observed := now
			if partition == "validation" {
				observed = time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)
			}
			for j := range registered {
				in, out := hash(family+registered[j]+"in"), hash(family+registered[j]+"out")
				rec := Receipt{Issuer: verifierIssuer.ID, TaskSHA256: hash("task"), Cohort: cohort, FamilyID: family, Repository: "repo-" + family, NearDuplicateGroup: "near-" + family, Partition: partition, RegisteredRepetitions: registered, ConfigurationID: cohort.ConfigurationID, ProfileSHA256: cohort.ProfileSHA256, ProtocolSHA256: cohort.ProtocolSHA256, RepetitionID: registered[j], ObservedAt: observed, VerifierSHA256: verifierIssuer.VerifierSHA256, RubricSHA256: verifierIssuer.RubricSHA256, InputSHA256: in, OutputSHA256: out, Accepted: true}
				rec.Signature = signJSON(t, verifierPriv, rec)
				rows = append(rows, OutcomeRecord{Version: Version, ID: family + registered[j], FamilyID: family, Repository: "repo-" + family, NearDuplicateGroup: "near-" + family, Partition: partition, Cohort: cohort, TaskSHA256: hash("task"), InputSHA256: in, OutputSHA256: out, RepetitionID: registered[j], RegisteredRepetitions: registered, TerminalOutcome: "success", ObservedAt: observed, Producer: "producer", Receipt: &rec, Cost: ptrNumber("0.10"), Currency: "USD", Seconds: ptrNumber("2"), Consumption: []Measurement{{PoolID: "pool", WindowFamily: "7d", Metric: "tokens", Unit: "token", Value: ptrNumber("100"), Attributable: false}}})
			}
		}
	}
	shard, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "shard.json"), shard, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := ShardManifest{Version: Version, Issuer: shardIssuer.ID, Asset: "shard.json", SHA256: hash(string(shard))}
	manifest.Signature = signJSON(t, shardPriv, manifest)
	manifestPath := writeJSON("manifest.json", manifest)
	return manifestPath, policyPath, cohort, shardPriv, verifierPriv
}
func signJSON(t *testing.T, key ed25519.PrivateKey, v any) string {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e = json.Unmarshal(b, &m); e != nil {
		t.Fatal(e)
	}
	m["signature"] = ""
	b, e = json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, b))
}
func ptrNumber(s string) *json.Number { n := json.Number(s); return &n }

func TestLoadEstimateFamilyAggregationAndTamperSeal(t *testing.T) {
	manifest, policy, cohort, _, _ := fixture(t, 52, 2)
	v, err := Load([]string{manifest}, policy)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := Estimate(v, cohort, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if artifact.Families != 52 || artifact.SuccessfulFamilies != 52 {
		t.Fatalf("family counts: %+v", artifact)
	}
	if artifact.QualityLower == "1" || artifact.QualityLower == "1.000000000000" {
		t.Fatalf("Wilson lower bound rounded up: %s", artifact.QualityLower)
	}
	if artifact.CostUpper == nil || artifact.CostUpper.String() != "0.2" {
		t.Fatalf("cost should sum both registered attempts: %v", artifact.CostUpper)
	}
	if artifact.CalibrationFamilies != 52 || artifact.CalibrationPartition != "validation" || artifact.CalibrationObservedAt != "2026-10-20T00:00:00Z" {
		t.Fatalf("missing calibration provenance: %+v", artifact)
	}
	if artifact.DemandValidUntil != "2026-10-27T00:00:00Z" {
		t.Fatalf("demand freshness should use calibration observations: %s", artifact.DemandValidUntil)
	}
	if artifact.SecondsUpper == nil || artifact.SecondsUpper.String() != "4" {
		t.Fatalf("seconds should sum both attempts: %v", artifact.SecondsUpper)
	}
	if len(artifact.Bounds) != 1 || artifact.Bounds[0].Upper == nil || artifact.Bounds[0].Upper.String() != "200" {
		t.Fatalf("consumption should sum both attempts per family: %+v", artifact.Bounds)
	}
	if artifact.QualityValidUntil != "2026-11-22T00:00:00Z" {
		t.Fatalf("freshness renewed from asOf: %s", artifact.QualityValidUntil)
	}
	privateBytes, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if string(privateBytes) != "{}" {
		t.Fatalf("private evidence leaked through JSON: %s", privateBytes)
	}
	artifactBytes, err := json.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(artifactBytes, []byte("taskSha256")) || bytes.Contains(artifactBytes, []byte("outputSha256")) {
		t.Fatalf("estimate leaked task or output hashes: %s", artifactBytes)
	}
	v.rows[0].FamilyID = "tampered"
	if Validate(v) == nil {
		t.Fatal("tampered verified evidence passed seal")
	}
}

func TestLoadRejectsDuplicateManifestAndClosedPolicy(t *testing.T) {
	manifest, policy, _, _, _ := fixture(t, 30, 1)
	if _, err := Load([]string{manifest, manifest}, policy); err == nil {
		t.Fatal("duplicate shard asset accepted")
	}
	original, err := os.ReadFile(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(policy, append(original[:len(original)-1], []byte(`,"unexpected":true}`)...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load([]string{manifest}, policy); err == nil {
		t.Fatal("unknown policy field accepted")
	}
}

func TestLoadAllowsContextRoleAndRejectsSharedShardVerifierKey(t *testing.T) {
	manifest, policy, _, _, _ := fixture(t, 30, 1)
	// Empty shards remain valid captured evidence for advisory abstention.
	v, err := Load([]string{}, policy)
	if err != nil {
		t.Fatalf("context issuer role rejected: %v", err)
	}
	if EvidenceSHA256(v) == "" {
		t.Fatal("empty verified evidence has no provenance digest")
	}
	_ = manifest
	var p EvidencePolicy
	raw, err := os.ReadFile(policy)
	if err != nil {
		t.Fatal(err)
	}
	if err = decode(raw, &p); err != nil {
		t.Fatal(err)
	}
	shardKey := ""
	for _, i := range p.Issuers {
		if hasRole(i, "shard") {
			shardKey = i.PublicKey
		}
	}
	for i := range p.Issuers {
		if hasRole(p.Issuers[i], "verifier") {
			p.Issuers[i].PublicKey = shardKey
		}
	}
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(policy, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load([]string{}, policy); err == nil {
		t.Fatal("shared shard/verifier public key accepted")
	}
}
func TestLoadRejectsSymlinkShard(t *testing.T) {
	manifest, policy, _, _, _ := fixture(t, 30, 1)
	root := filepath.Dir(manifest)
	actual := filepath.Join(root, "shard.json")
	target := filepath.Join(root, "actual.json")
	if err := os.Rename(actual, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, actual); err != nil {
		t.Fatal(err)
	}
	if _, err := Load([]string{manifest}, policy); err == nil {
		t.Fatal("symlink shard accepted")
	}
}
func TestEstimateRequiresIndependentFamilies(t *testing.T) {
	manifest, policy, cohort, _, _ := fixture(t, 29, 1)
	v, err := Load([]string{manifest}, policy)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Estimate(v, cohort, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("underpowered cohort accepted")
	}
}

func TestCensorMakesDemandBoundsUnknown(t *testing.T) {
	manifest, policy, cohort, _, _ := fixture(t, 30, 1)
	v, err := Load([]string{manifest}, policy)
	if err != nil {
		t.Fatal(err)
	}
	for i := range v.rows {
		if v.rows[i].Partition == "validation" {
			v.rows[i].TerminalOutcome = "censored"
			v.rows[i].Receipt = nil
			break
		}
	}
	v.seal = v.computeSeal()
	artifact, err := Estimate(v, cohort, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if artifact.SuccessfulFamilies != 30 {
		t.Fatalf("calibration censor changed locked-test quality: %d", artifact.SuccessfulFamilies)
	}
	if artifact.CostUpper != nil || artifact.SecondsUpper != nil {
		t.Fatal("censored demand was assigned a finite bound")
	}
	for _, bound := range artifact.Bounds {
		if bound.Upper != nil {
			t.Fatal("censored consumption was assigned a finite bound")
		}
	}
}

func TestPolicyRejectsBase64KeyAlias(t *testing.T) {
	manifest, policy, _, _, _ := fixture(t, 30, 1)
	_ = manifest
	b, e := os.ReadFile(policy)
	if e != nil {
		t.Fatal(e)
	}
	var p EvidencePolicy
	if e = json.Unmarshal(b, &p); e != nil {
		t.Fatal(e)
	}
	p.Issuers[1].PublicKey = p.Issuers[0].PublicKey[:8] + "\n" + p.Issuers[0].PublicKey[8:]
	if validatePolicy(p) == nil {
		t.Fatal("same verifier key under newline alias")
	}
}

func rewriteSignedShard(t *testing.T, manifestPath string, shardPriv ed25519.PrivateKey, rows []OutcomeRecord) {
	t.Helper()
	b, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	shardPath := filepath.Join(filepath.Dir(manifestPath), "shard.json")
	if err = os.WriteFile(shardPath, b, 0600); err != nil {
		t.Fatal(err)
	}
	mb, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var m ShardManifest
	if err = json.Unmarshal(mb, &m); err != nil {
		t.Fatal(err)
	}
	m.SHA256 = digest(b)
	m.Signature = ""
	m.Signature = signJSON(t, shardPriv, m)
	mb, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(manifestPath, mb, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReceiptSignatureBindsCohortAndFamily(t *testing.T) {
	manifest, policy, _, shardPriv, _ := fixture(t, 30, 1)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(manifest), "shard.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []OutcomeRecord
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	rows[0].Cohort.Risk = "high"
	rewriteSignedShard(t, manifest, shardPriv, rows)
	if _, err = Load([]string{manifest}, policy); err == nil {
		t.Fatal("cohort relabel with stale receipt accepted")
	}
}

func TestReceiptCannotBeReplayedAcrossFamiliesEvenWhenResigned(t *testing.T) {
	manifest, policy, _, shardPriv, verifierPriv := fixture(t, 30, 1)
	b, err := os.ReadFile(filepath.Join(filepath.Dir(manifest), "shard.json"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []OutcomeRecord
	if err = json.Unmarshal(b, &rows); err != nil {
		t.Fatal(err)
	}
	replay := rows[0]
	replay.ID = "forged-second-family"
	replay.FamilyID = "forged-second-family"
	replay.Repository = "forged-second-repository"
	replay.NearDuplicateGroup = "forged-second-near-duplicate"
	replay.Receipt = cloneReceipt(rows[0].Receipt)
	replay.Receipt.FamilyID = replay.FamilyID
	replay.Receipt.Repository = replay.Repository
	replay.Receipt.NearDuplicateGroup = replay.NearDuplicateGroup
	replay.Receipt.Signature = ""
	replay.Receipt.Signature = signJSON(t, verifierPriv, *replay.Receipt)
	rows = append(rows, replay)
	rewriteSignedShard(t, manifest, shardPriv, rows)
	if _, err = Load([]string{manifest}, policy); err == nil {
		t.Fatal("same verifier task/input/output/repetition reused across families")
	}
}

func cloneReceipt(r *Receipt) *Receipt {
	c := *r
	c.RegisteredRepetitions = append([]string(nil), r.RegisteredRepetitions...)
	return &c
}

func TestHighRiskWilsonBoundaryAndConformalRank(t *testing.T) {
	if wilsonLower(51, 51) >= 0.95 || wilsonLower(52, 52) < 0.95 {
		t.Fatal("high-risk Wilson feasibility boundary changed")
	}
	values := make([]*big.Rat, 30)
	for i := range values {
		values[i] = big.NewRat(int64(i+1), 1)
	}
	bound := conformalRat(values)
	if bound == nil || bound.Cmp(big.NewRat(30, 1)) != 0 {
		t.Fatal("wrong conformal rank for30 calibrationfamilies", bound)
	}
}

func TestAuditOnlyKeepsQualityButDoesNotFitDemand(t *testing.T) {
	manifest, policy, cohort, _, _ := fixture(t, 30, 1)
	v, err := Load([]string{manifest}, policy)
	if err != nil {
		t.Fatal(err)
	}
	rows := v.rows[:0]
	for _, row := range v.rows {
		if row.Partition == "locked-test" {
			rows = append(rows, row)
		}
	}
	v.rows = rows
	v.seal = v.computeSeal()
	art, err := Estimate(v, cohort, time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if art.Families != 30 || art.SuccessfulFamilies != 30 || art.QualityLower == "0" {
		t.Fatalf("quality unavailable: %+v", art)
	}
	if art.CostUpper != nil || art.SecondsUpper != nil || len(art.Bounds) != 0 || art.DemandValidUntil != "" {
		t.Fatalf("audit data incorrectly fitted demand: %+v", art)
	}
}

func TestFutureValidationCannotCalibrateLockedAudit(t *testing.T) {
	manifest, policy, cohort, _, _ := fixture(t, 30, 1)
	v, err := Load([]string{manifest}, policy)
	if err != nil {
		t.Fatal(err)
	}
	for i := range v.rows {
		if v.rows[i].Partition == "validation" {
			v.rows[i].ObservedAt = "2026-10-24T00:00:00Z"
		}
	}
	v.seal = v.computeSeal()
	if _, err = Estimate(v, cohort, time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("future validation calibration accepted")
	}
}
