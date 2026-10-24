package selection_test

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/contracts"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/evaluation/replay"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/selection"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
)

type fixtureOptions struct {
	staleUsage, unknownSelector, toolRequired bool
	lowQuality                                bool
	standardFloor                             string
}
type selectionFixture struct {
	req                                                                 selection.Request
	asOf                                                                time.Time
	configID, account                                                   string
	taskPath, candidatesPath, profilePath, inputRevisionPath, usagePath string
}

func makeFixture(t *testing.T, opts fixtureOptions) selectionFixture {
	t.Helper()
	root, canonicalErr := filepath.EvalSymlinks(t.TempDir())
	if canonicalErr != nil {
		t.Fatal(canonicalErr)
	}
	asOf := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	write := func(path string, b []byte) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	putJSON := func(path string, v any) {
		t.Helper()
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		write(path, b)
	}
	hash := func(b []byte) string { x := sha256.Sum256(b); return hex.EncodeToString(x[:]) }

	profile := map[string]any{"apiVersion": "aok/v1alpha1", "kind": "ExecutionProfile", "metadata": map[string]any{"id": "codex-profile", "name": "Codex profile", "version": "1.0.0", "description": "Positive selection integration fixture"}, "client": map[string]any{"adapter": "codex", "interface": "cli", "versionConstraint": "1.2.3"}, "model": map[string]any{"selector": "model-x", "provider": "acme", "reasoning": map[string]any{"mode": "effort", "effort": "medium"}, "generation": map[string]any{"maxOutputTokens": 2000}}, "compatibility": map[string]any{"unsupportedSettings": "error"}, "environment": map[string]any{"profile": map[string]any{"kind": "EnvironmentProfile", "id": "local", "version": "1.0.0"}}}
	profileBytes, _ := json.Marshal(profile)
	profileSHA := hash(profileBytes)
	version, provider, effort, selector, mode := "1.2.3", "acme", "medium", "model-x", "effort"
	outMax := json.Number("2000")
	generation := &contracts.GenerationSettings{MaxOutputTokens: &outMax}
	cfg := modelcatalog.Configuration{Host: &[]string{"codex"}[0], HostVersion: &version, Provider: &provider, ModelID: &selector, ReasoningMode: &mode, Effort: &effort, Generation: generation}
	observations := []modelcatalog.ProbeObservation{{Dimension: "selector", Value: json.RawMessage(`"model-x"`)}, {Dimension: "provider", Value: json.RawMessage(`"acme"`)}, {Dimension: "host", Value: json.RawMessage(`"codex"`)}, {Dimension: "hostVersion", Value: json.RawMessage(`"1.2.3"`)}, {Dimension: "reasoning.mode", Value: json.RawMessage(`"effort"`)}, {Dimension: "reasoning.effort", Value: json.RawMessage(`"medium"`)}, {Dimension: "contextTokens", Value: json.RawMessage(`8000`)}, {Dimension: "generation.maxOutputTokens", Value: json.RawMessage(`2000`)}}
	probe := modelcatalog.ProbeEvidence{Version: modelcatalog.ProbeVersion, ID: "positive-invocation", Configuration: cfg, ProfileSHA256: profileSHA, ObservedAt: asOf.Add(-time.Minute).Format(time.RFC3339), AccountScope: "acct-hash", Outcome: "success", Observations: observations, EvidenceSHA256: hash([]byte("redacted independent evidence reference"))}
	probeBytes, _ := json.Marshal(probe)
	sourceHash := hash(probeBytes)
	catalogInput := filepath.Join(root, "catalog-input")
	write(filepath.Join(catalogInput, "probe.json"), probeBytes)
	cm := modelcatalog.Manifest{Version: modelcatalog.ManifestVersion, Sources: []modelcatalog.SourceInput{{ID: "probe-source", Format: "probe", Artifact: modelcatalog.Asset{Path: "probe.json", SHA256: sourceHash}, Evidence: []modelcatalog.Asset{}}}, Mappings: []modelcatalog.Mapping{}}
	cp := modelcatalog.Policy{Version: modelcatalog.PolicyVersion, Sources: []modelcatalog.SourceRule{{ID: "probe-source", ArtifactSHA256: sourceHash, Categories: []string{modelcatalog.Invoked}}}, ValidityHours: modelcatalog.DefaultValidityHours()}
	putJSON(filepath.Join(catalogInput, "manifest.json"), cm)
	putJSON(filepath.Join(catalogInput, "policy.json"), cp)
	captured, report := modelcatalog.Import(filepath.Join(catalogInput, "manifest.json"), filepath.Join(catalogInput, "policy.json"))
	if report.Status != "valid" {
		t.Fatalf("P2 positive probe import: %s %#v", report.Status, report.Diagnostics)
	}
	catalogRoot := filepath.Join(root, "catalog")
	writeFiles(t, catalogRoot, captured.Files)
	loaded, loadReport := modelcatalog.Load(catalogRoot, filepath.Join(catalogInput, "policy.json"))
	if loaded == nil || loadReport.Status != "valid" {
		t.Fatalf("P2 catalog Load: %#v", loadReport.Diagnostics)
	}
	configID := modelcatalog.ConfigurationID(cfg)
	account := "acct-hash"

	taskRoot := filepath.Join(root, "task")
	assignment := []byte(`{"apiVersion":"aok/v1alpha1","kind":"AgentAssignment","agent":{"kind":"Agent","id":"agent","version":"1.0.0"},"taskPacket":"packet.json","executionProfile":{"kind":"ExecutionProfile","id":"codex-profile","version":"1.0.0"},"inputs":{},"outputs":{"contract":{"kind":"OutputContract","id":"result","version":"1.0.0"}},"metadata":{"id":"assignment","name":"assignment","version":"1.0.0","description":"selection fixture"}}`)
	packet := []byte(`{"id":"packet-1","objective":"complete the requested task"}`)
	inputRevision := []byte("revision-1\n")
	write(filepath.Join(taskRoot, "assets/assignment.json"), assignment)
	write(filepath.Join(taskRoot, "assets/packet.json"), packet)
	write(filepath.Join(taskRoot, "assets/input-revision.txt"), inputRevision)
	assignmentSHA, packetSHA, revisionSHA := hash(assignment), hash(packet), hash(inputRevision)
	protocolSHA := hash([]byte("protocol-v1"))
	requirements := []modelcatalog.Requirement{}
	if opts.toolRequired {
		requirements = append(requirements, modelcatalog.Requirement{Dimension: "toolUse", Value: json.RawMessage(`true`)})
	}
	task := selection.TaskProfile{Version: selection.Version, Assignment: selection.Asset{Path: "assets/assignment.json", SHA256: assignmentSHA}, Packet: selection.Asset{Path: "assets/packet.json", SHA256: packetSHA}, InputRevision: selection.Asset{Path: "assets/input-revision.txt", SHA256: revisionSHA}, TaskClass: "coding", Language: "go", Risk: "standard", Requirements: requirements, NamedTools: []string{"rg"}, Regions: []string{"us"}, Permissions: []string{"read"}, ContextTokens: json.Number("4000"), OutputTokens: json.Number("2000"), Deadline: asOf.Add(time.Hour).Format(time.RFC3339Nano), SpendLimit: json.Number("1"), Currency: "USD", ProtocolSHA256: protocolSHA}
	taskPath := filepath.Join(taskRoot, "task.json")
	putJSON(taskPath, task)

	candidateRoot := filepath.Join(root, "candidates")
	candidateProfile := map[string]any{}
	if err := json.Unmarshal(profileBytes, &candidateProfile); err != nil {
		t.Fatal(err)
	}
	if opts.unknownSelector {
		candidateProfile["model"].(map[string]any)["selector"] = "unknown-model"
	}
	candidateProfileBytes, _ := json.Marshal(candidateProfile)
	candidateProfileSHA := hash(candidateProfileBytes)
	profilePath := filepath.Join(candidateRoot, "profile.json")
	write(profilePath, candidateProfileBytes)
	contextPub, contextPriv, _ := ed25519.GenerateKey(rand.Reader)
	shardPub, shardPriv, _ := ed25519.GenerateKey(rand.Reader)
	verifierPub, verifierPriv, _ := ed25519.GenerateKey(rand.Reader)
	ctxIssuer := evaluation.Issuer{ID: "context-signer", PublicKey: base64.StdEncoding.EncodeToString(contextPub), Roles: []string{"context"}, VerifierSHA256: hash([]byte("context verifier pin")), RubricSHA256: hash([]byte("context rubric pin"))}
	shardIssuer := evaluation.Issuer{ID: "shard-signer", PublicKey: base64.StdEncoding.EncodeToString(shardPub), Roles: []string{"shard"}, VerifierSHA256: hash([]byte("shard placeholder")), RubricSHA256: hash([]byte("shard placeholder rubric"))}
	verifierIssuer := evaluation.Issuer{ID: "independent-verifier", PublicKey: base64.StdEncoding.EncodeToString(verifierPub), Roles: []string{"verifier"}, VerifierSHA256: hash([]byte("verifier binary pin")), RubricSHA256: hash([]byte("rubric pin"))}
	ep := evaluation.EvidencePolicy{Version: evaluation.Version, Issuers: []evaluation.Issuer{ctxIssuer, shardIssuer, verifierIssuer}, QualityMaxAgeSeconds: 30 * 24 * 3600, DemandMaxAgeSeconds: 7 * 24 * 3600}
	evidencePolicyPath := filepath.Join(root, "evidence-policy.json")
	putJSON(evidencePolicyPath, ep)
	contextRows := []selection.ContextObservation{}
	for dim, values := range map[string][]string{"account": {account}, "region": {"us"}, "permissions": {"read"}, "namedTools": {"rg"}} {
		o := selection.ContextObservation{AssignmentSHA256: assignmentSHA, TaskSHA256: packetSHA, InputRevisionSHA256: revisionSHA, ConfigurationID: configID, ProfileSHA256: candidateProfileSHA, AccountScope: account, Dimension: dim, Values: values, ObservedAt: asOf.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: asOf.Add(20 * time.Minute).Format(time.RFC3339Nano), Issuer: ctxIssuer.ID}
		o.Signature = signCanonical(t, contextPriv, o)
		contextRows = append(contextRows, o)
	}
	candidate := selection.Candidate{ConfigurationID: configID, Profile: selection.Asset{Path: "profile.json", SHA256: candidateProfileSHA}, CatalogSHA256: loaded.CatalogSHA256, AccountScope: account, BindingIDs: []string{"usage-binding"}, Context: contextRows}
	shardRel := "outcomes/manifest.json"
	manifest := selection.CandidateManifest{Version: selection.Version, Candidates: []selection.Candidate{candidate}, OutcomeShards: []string{shardRel}}
	candidateManifestPath := filepath.Join(candidateRoot, "candidates.json")
	putJSON(candidateManifestPath, manifest)
	rows := []evaluation.OutcomeRecord{}
	repetitions := []string{"run-1"}
	successCount := 52
	if opts.lowQuality {
		successCount = 42
	}
	observed := asOf.Add(-10 * time.Second).Format(time.RFC3339)
	for i := 0; i < 52; i++ {
		family := "family-" + strconv.Itoa(i)
		terminal := "success"
		if i >= successCount {
			terminal = "failure"
		}
		inHash, outHash := hash([]byte(family+" input")), hash([]byte(family+" output"))
		cohort := evaluation.Cohort{ConfigurationID: configID, ProfileSHA256: profileSHA, TaskClass: task.TaskClass, Language: task.Language, Risk: task.Risk, AccountScope: account, ProtocolSHA256: protocolSHA}
		receipt := evaluation.Receipt{Issuer: verifierIssuer.ID, TaskSHA256: revisionSHA, Cohort: cohort, FamilyID: family, Repository: "repo-" + family, NearDuplicateGroup: "near-" + family, Partition: "locked-test", RegisteredRepetitions: repetitions, ConfigurationID: configID, ProfileSHA256: profileSHA, ProtocolSHA256: protocolSHA, RepetitionID: repetitions[0], ObservedAt: observed, VerifierSHA256: verifierIssuer.VerifierSHA256, RubricSHA256: verifierIssuer.RubricSHA256, InputSHA256: inHash, OutputSHA256: outHash, Accepted: terminal == "success"}
		receipt.Signature = signCanonical(t, verifierPriv, receipt)
		windowFamily := usage.WindowFamily(usage.Constraint{PoolID: "fixture-pool", Provider: "acme", Service: "api", LimitID: "requests-limit", Metric: "requests", Unit: "requests", Scope: "account", WindowKind: "fixed"})
		row := evaluation.OutcomeRecord{Version: evaluation.Version, ID: family + "-run-1", FamilyID: family, Repository: "repo-" + family, NearDuplicateGroup: "near-" + family, Partition: "locked-test", Cohort: evaluation.Cohort{ConfigurationID: configID, ProfileSHA256: profileSHA, TaskClass: task.TaskClass, Language: task.Language, Risk: task.Risk, AccountScope: account, ProtocolSHA256: protocolSHA}, TaskSHA256: revisionSHA, InputSHA256: inHash, OutputSHA256: outHash, RepetitionID: repetitions[0], RegisteredRepetitions: repetitions, TerminalOutcome: terminal, ObservedAt: observed, Producer: "test-harness", Receipt: &receipt, Cost: number("0.10"), Currency: "USD", Seconds: number("10"), Consumption: []evaluation.Measurement{{PoolID: "fixture-pool", WindowFamily: windowFamily, Metric: "requests", Unit: "requests", Value: number("1")}}}
		rows = append(rows, row)
	}
	// Independent development/calibration families are frozen before the locked audit.
	for i := 0; i < 52; i++ {
		base := rows[i]
		family := "validation-family-" + strconv.Itoa(i)
		base.ID = family + "-run-1"
		base.FamilyID, base.Repository, base.NearDuplicateGroup, base.Partition = family, "repo-"+family, "near-"+family, "validation"
		base.InputSHA256, base.OutputSHA256 = hash([]byte(family+" input")), hash([]byte(family+" output"))
		base.ObservedAt = asOf.Add(-24 * time.Hour).Format(time.RFC3339)
		base.Cost, base.Seconds = number("0.10"), number("10")
		base.Consumption = []evaluation.Measurement{{PoolID: "fixture-pool", WindowFamily: usage.WindowFamily(usage.Constraint{PoolID: "fixture-pool", Provider: "acme", Service: "api", LimitID: "requests-limit", Metric: "requests", Unit: "requests", Scope: "account", WindowKind: "fixed"}), Metric: "requests", Unit: "requests", Value: number("1")}}
		r := *base.Receipt
		r.FamilyID, r.Repository, r.NearDuplicateGroup, r.Partition = base.FamilyID, base.Repository, base.NearDuplicateGroup, base.Partition
		r.InputSHA256, r.OutputSHA256, r.ObservedAt = base.InputSHA256, base.OutputSHA256, base.ObservedAt
		r.Signature = ""
		r.Signature = signCanonical(t, verifierPriv, r)
		base.Receipt = &r
		rows = append(rows, base)
	}
	shardBytes, _ := json.Marshal(rows)
	shardPath := filepath.Join(candidateRoot, "outcomes/shard.json")
	write(shardPath, shardBytes)
	shardManifest := evaluation.ShardManifest{Version: evaluation.Version, Issuer: shardIssuer.ID, Asset: "shard.json", SHA256: hash(shardBytes)}
	shardManifest.Signature = signCanonical(t, shardPriv, shardManifest)
	putJSON(filepath.Join(candidateRoot, shardRel), shardManifest)

	usageRoot := filepath.Join(root, "usage")
	usagePolicy := usage.Policy{Version: usage.Version, Sources: []usage.SourceRule{{ID: "quota-source", Host: "codex", HostVersion: "1.2.3", AdapterVersion: "1.0.0", AuthModes: []string{"account"}, Metrics: []string{"requests"}, Authority: "acme/api", MaxAgeSeconds: 60, CollectionAllowed: false}}, SafetyMargins: map[string]json.Number{}}
	usagePolicyBytes, _ := json.Marshal(usagePolicy)
	usagePolicyPath := filepath.Join(usageRoot, "policy.json")
	write(usagePolicyPath, usagePolicyBytes)
	constraint := usage.Constraint{PoolID: "fixture-pool", Provider: "acme", Service: "api", LimitID: "requests-limit", Metric: "requests", Unit: "requests", Scope: "account", WindowKind: "fixed"}
	constraint.ID = usage.ConstraintID(constraint)
	bind := usage.Binding{ID: "usage-binding", PoolID: constraint.PoolID, Provider: constraint.Provider, Service: constraint.Service, Host: "codex", HostVersion: "1.2.3", AuthMode: "account", SourceID: "quota-source", StreamID: "quota-stream", ConfigurationIDs: []string{configID}, ConstraintIDs: []string{constraint.ID}, ConstraintsComplete: true}
	originTime := asOf.Add(-10 * time.Second)
	expires := asOf.Add(time.Hour)
	if opts.staleUsage {
		originTime = asOf.Add(-2 * time.Hour)
		expires = asOf.Add(-time.Hour)
	}
	origin := usage.Origin{SourceID: "quota-source", Host: "codex", HostVersion: "1.2.3", AdapterVersion: "1.0.0", AuthMode: "account", ObservedAt: originTime.Format(time.RFC3339), ReceivedAt: originTime.Format(time.RFC3339), FreshnessKnown: true}
	q := usage.Quota{ID: "quota-now", Origin: origin, Constraint: constraint, Generation: "generation-7", Revision: 1, Completeness: "full", Remaining: number("100"), Used: number("0"), Limit: number("100"), ExpiresAt: expires.Format(time.RFC3339), CoveredChargeIDs: []string{}, Precision: "exact"}
	export := usage.Export{Version: usage.Version, LedgerRevision: 7, PolicySHA256: usage.Hash(usagePolicyBytes), Bindings: []usage.Binding{bind}, Events: []usage.Event{}, Quotas: []usage.Quota{q}, Charges: []usage.Charge{}, Reservations: []usage.Reservation{}, Evidence: []usage.Asset{}}
	for i := range contextRows {
		contextRows[i].CoverageComplete = true
		contextRows[i].BindingSHA256 = usage.ConfigurationBindingsDigest(export, configID)
		contextRows[i].Signature = ""
		contextRows[i].Signature = signCanonical(t, contextPriv, contextRows[i])
	}
	candidate.Context = contextRows
	manifest.Candidates = []selection.Candidate{candidate}
	putJSON(candidateManifestPath, manifest)
	usageFiles, err := usage.Files(export)
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(usageRoot, "usage.json"), usageFiles["usage.json"])

	floor := opts.standardFloor
	if floor == "" {
		floor = "0.80"
	}
	policy := selection.SelectionPolicy{Version: selection.Version, DefaultsVersion: "aok-selection-defaults/v1", Routes: []selection.Route{{ConfigurationID: configID, AccountScope: account}}, Priority: "quality", StandardFloor: json.Number(floor), HighRiskFloor: json.Number("0.95"), Fallback: []selection.Edge{}, Pin: "", WaiveHysteresis: true}
	policyPath := filepath.Join(root, "selection-policy.json")
	putJSON(policyPath, policy)
	return selectionFixture{req: selection.Request{TaskPath: taskPath, CandidatesPath: candidateManifestPath, CatalogPath: catalogRoot, CatalogPolicyPath: filepath.Join(catalogInput, "policy.json"), UsagePath: filepath.Join(usageRoot, "usage.json"), UsagePolicyPath: usagePolicyPath, PolicyPath: policyPath, EvidencePolicyPath: evidencePolicyPath, AsOf: asOf}, asOf: asOf, configID: configID, account: account, taskPath: taskPath, candidatesPath: candidateManifestPath, profilePath: profilePath, inputRevisionPath: filepath.Join(taskRoot, "assets/input-revision.txt"), usagePath: filepath.Join(usageRoot, "usage.json")}
}

func writeFiles(t *testing.T, root string, files map[string][]byte) {
	t.Helper()
	for name, b := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func number(s string) *json.Number { n := json.Number(s); return &n }
func signCanonical(t *testing.T, key ed25519.PrivateKey, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err = json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["signature"] = ""
	b, err = json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key, b))
}

func TestSelectPositiveValidatedP2P3AndSignedEvidence(t *testing.T) {
	f := makeFixture(t, fixtureOptions{})
	d, err := selection.Select(f.req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Disposition != "selected" || d.ConfigurationID != f.configID || d.AccountScope != f.account {
		t.Fatalf("expected selected positive route, got %+v", d)
	}
	second, err := selection.Select(f.req)
	if err != nil || second.Digest != d.Digest {
		t.Fatalf("nonreproducible selection %v %s %s", err, d.Digest, second.Digest)
	}
	bytes, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	if err = selection.ValidateJSON(bytes, "decision"); err != nil {
		t.Fatalf("decision does not satisfy its closed schema: %v", err)
	}
	if d.AuthorizationGranted || d.ExecutionReady {
		t.Fatal("advisory selection granted execution authority")
	}
	if len(d.Candidates) != 1 || !d.Candidates[0].Eligible || d.Candidates[0].Capacity == nil || d.Candidates[0].Capacity.Disposition != "observed-fit" {
		t.Fatalf("candidate gates did not pass: %+v", d.Candidates)
	}
	if d.Candidates[0].Estimate == nil || d.Candidates[0].Estimate.Families < 52 {
		t.Fatal("signed independent outcomes missing")
	}
}

func TestSelectStaleUsageRequestsRefresh(t *testing.T) {
	f := makeFixture(t, fixtureOptions{staleUsage: true})
	d, err := selection.Select(f.req)
	if err != nil {
		t.Fatal(err)
	}
	if d.Disposition == "selected" || d.Disposition != "refresh-needed" {
		t.Fatalf("stale P3 generation should require refresh: %s", d.Disposition)
	}
}

func TestSelectFailClosedInputsAndEvidence(t *testing.T) {
	t.Run("changed input hash", func(t *testing.T) {
		f := makeFixture(t, fixtureOptions{})
		if err := os.WriteFile(f.inputRevisionPath, []byte("changed\n"), 0600); err != nil {
			t.Fatal(err)
		}
		d, err := selection.Select(f.req)
		if err == nil && d.Disposition == "selected" {
			t.Fatal("changed revision hash selected")
		}
	})
	t.Run("unknown selector", func(t *testing.T) {
		f := makeFixture(t, fixtureOptions{unknownSelector: true})
		d, err := selection.Select(f.req)
		if err != nil {
			t.Fatal(err)
		}
		if d.Disposition == "selected" {
			t.Fatal("unknown selector selected")
		}
	})
	t.Run("unknown capability", func(t *testing.T) {
		f := makeFixture(t, fixtureOptions{toolRequired: true})
		d, err := selection.Select(f.req)
		if err != nil {
			t.Fatal(err)
		}
		if d.Disposition == "selected" {
			t.Fatal("unproven tool capability selected")
		}
	})
	t.Run("quality below configured floor", func(t *testing.T) {
		f := makeFixture(t, fixtureOptions{lowQuality: true})
		d, err := selection.Select(f.req)
		if err != nil {
			t.Fatal(err)
		}
		if d.Disposition == "selected" {
			t.Fatal("candidate below quality floor selected")
		}
	})
}

func TestBuiltP4CLIWithoutPATHCredentialsOrNetworkTools(t *testing.T) {
	f := makeFixture(t, fixtureOptions{})
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(filepath.Dir(f.req.TaskPath), "aok")
	build := exec.Command("go", "build", "-o", bin, "./cmd/aok")
	build.Dir = repo
	if b, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, b)
	}
	run := func(want int, args ...string) []byte {
		t.Helper()
		cmd := exec.Command(bin, args...)
		cmd.Env = []string{"PATH=", "HOME=" + filepath.Dir(bin)}
		var out, stderr bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &stderr
		e := cmd.Run()
		got := 0
		if e != nil {
			ee, ok := e.(*exec.ExitError)
			if !ok {
				t.Fatal(e)
			}
			got = ee.ExitCode()
		}
		if got != want {
			t.Fatalf("%v exit %d want %d: %s %s", args, got, want, out.String(), stderr.String())
		}
		return out.Bytes()
	}
	output := filepath.Join(filepath.Dir(bin), "selection-output")
	args := []string{"models", "select", "--task", f.req.TaskPath, "--candidates", f.req.CandidatesPath, "--catalog", f.req.CatalogPath, "--catalog-policy", f.req.CatalogPolicyPath, "--usage", f.req.UsagePath, "--usage-policy", f.req.UsagePolicyPath, "--policy", f.req.PolicyPath, "--evidence-policy", f.req.EvidencePolicyPath, "--as-of", f.asOf.Format(time.RFC3339), "--out", output, "--json"}
	b := run(0, args...)
	if !bytes.Contains(b, []byte(`"disposition": "selected"`)) || bytes.Contains(b, []byte("complete the requested task")) {
		t.Fatalf("invalid/private decision: %s", b)
	}
	run(1, args...) // immutable output cannot be overwritten
	shard := filepath.Join(filepath.Dir(f.req.CandidatesPath), "outcomes", "manifest.json")
	run(0, "evaluation", "validate", "--manifest", shard, "--evidence-policy", f.req.EvidencePolicyPath, "--json")
	var d selection.SelectionDecision
	if e := json.Unmarshal(b, &d); e != nil {
		t.Fatal(e)
	}
	cohortPath := filepath.Join(filepath.Dir(bin), "cohort.json")
	cb, _ := json.Marshal(d.Candidates[0].Estimate.Cohort)
	if e := os.WriteFile(cohortPath, cb, 0600); e != nil {
		t.Fatal(e)
	}
	run(0, "evaluation", "estimate", "--manifest", shard, "--evidence-policy", f.req.EvidencePolicyPath, "--cohort", cohortPath, "--as-of", f.asOf.Format(time.RFC3339), "--out", filepath.Join(filepath.Dir(bin), "estimate-output"), "--json")
	root := filepath.Dir(f.req.EvidencePolicyPath)
	rel := func(p string) string {
		r, e := filepath.Rel(root, p)
		if e != nil {
			t.Fatal(e)
		}
		return filepath.ToSlash(r)
	}
	basePolicyBytes, e := os.ReadFile(f.req.PolicyPath)
	if e != nil {
		t.Fatal(e)
	}
	var basePolicy selection.SelectionPolicy
	if e = json.Unmarshal(basePolicyBytes, &basePolicy); e != nil {
		t.Fatal(e)
	}
	frozen := []replay.FrozenPolicy{}
	for name, priority := range map[string]string{"pinned": "quality", "quality-only": "quality", "cost-aware": "cost", "quota-aware": "capacity", "reservation-aware": "capacity"} {
		p := basePolicy
		p.Priority = priority
		if name == "pinned" {
			p.Pin = f.configID
		}
		pb, _ := json.Marshal(p)
		path := filepath.Join(root, name+"-policy.json")
		if e := os.WriteFile(path, pb, 0600); e != nil {
			t.Fatal(e)
		}
		frozen = append(frozen, replay.FrozenPolicy{Name: name, Asset: rel(path), SHA256: usage.Hash(pb)})
	}
	manifest := replay.SelectionReplayManifest{Version: evaluation.Version, FrozenAt: f.asOf.Add(-time.Minute).Format(time.RFC3339), AuditAt: f.asOf.Add(time.Minute).Format(time.RFC3339), Policies: frozen, Cases: []replay.SelectionCase{{ID: "case", Partition: "locked-test", Repository: "fixture", Family: "family", NearDuplicateGroup: "group", Task: rel(f.req.TaskPath), Candidates: rel(f.req.CandidatesPath), Catalog: rel(f.req.CatalogPath), CatalogPolicy: rel(f.req.CatalogPolicyPath), Usage: rel(f.req.UsagePath), UsagePolicy: rel(f.req.UsagePolicyPath), EvidencePolicy: rel(f.req.EvidencePolicyPath), AsOf: f.asOf.Format(time.RFC3339)}}}
	manifestPath := filepath.Join(root, "selector-replay.json")
	mb, _ := json.Marshal(manifest)
	if e := os.WriteFile(manifestPath, mb, 0600); e != nil {
		t.Fatal(e)
	}
	for _, op := range []string{"replay", "compare"} {
		report := run(0, "evaluation", op, "--selection-manifest", manifestPath, "--partition", "locked-test", "--json")
		var rr replay.SelectionReplayReport
		if e = json.Unmarshal(report, &rr); e != nil || len(rr.Decisions) != 5 {
			t.Fatalf("actual selector replay: %v %s", e, report)
		}
		for _, d := range rr.Decisions {
			if d.Decision.Disposition != "selected" {
				t.Fatal(d.Decision.Disposition)
			}
		}
	}
	// A single policy may not change hysteresis while claiming a controlled comparison.
	policyAsset := filepath.Join(root, frozen[0].Asset)
	originalPolicy, e := os.ReadFile(policyAsset)
	if e != nil {
		t.Fatal(e)
	}
	var alteredPolicy selection.SelectionPolicy
	if e = json.Unmarshal(originalPolicy, &alteredPolicy); e != nil {
		t.Fatal(e)
	}
	alteredPolicy.WaiveHysteresis = !alteredPolicy.WaiveHysteresis
	alteredBytes, _ := json.Marshal(alteredPolicy)
	if e = os.WriteFile(policyAsset, alteredBytes, 0600); e != nil {
		t.Fatal(e)
	}
	originalDigest := manifest.Policies[0].SHA256
	manifest.Policies[0].SHA256 = usage.Hash(alteredBytes)
	alteredManifest, _ := json.Marshal(manifest)
	if e = os.WriteFile(manifestPath, alteredManifest, 0600); e != nil {
		t.Fatal(e)
	}
	run(1, "evaluation", "compare", "--selection-manifest", manifestPath, "--json")
	if e = os.WriteFile(policyAsset, originalPolicy, 0600); e != nil {
		t.Fatal(e)
	}
	manifest.Policies[0].SHA256 = originalDigest
	if e = os.WriteFile(manifestPath, mb, 0600); e != nil {
		t.Fatal(e)
	}
	// A frozen hash cannot be silently repointed at edited policy bytes.
	if e := os.WriteFile(filepath.Join(root, frozen[0].Asset), []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	run(1, "evaluation", "replay", "--selection-manifest", manifestPath, "--json")
	for _, bad := range [][]string{{"evaluation", "run"}, {"models", "select", "--task", "missing"}} {
		run(2, bad...)
	}
}
