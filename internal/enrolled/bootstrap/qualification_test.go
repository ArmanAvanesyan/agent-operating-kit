package bootstrap

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/authority"
	codex "github.com/ArmanAvanesyan/agent-operating-kit/internal/enrolled/codex"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/hostbridge"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/modelcatalog"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/collectors"
	accounting "github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/store"
)

type qualificationTestAnchor struct {
	mu                  sync.Mutex
	tip                 string
	currentCalls        int
	advanceCalls        int
	advanceCommits      int
	loseAdvanceResponse bool
}

func (a *qualificationTestAnchor) Current(ctx context.Context, stream string) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.currentCalls++
	if ctx == nil || ctx.Err() != nil || stream == "" {
		return "", context.Canceled
	}
	return a.tip, nil
}
func (a *qualificationTestAnchor) Advance(ctx context.Context, stream, expected, next string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.advanceCalls++
	if ctx == nil || ctx.Err() != nil || stream == "" {
		return context.Canceled
	}
	if a.tip != expected {
		return errors.New("anchor compare-and-append lost")
	}
	a.tip = next
	a.advanceCommits++
	if a.loseAdvanceResponse {
		return errors.New("simulated lost anchor response")
	}
	return nil
}

// Force both callers to observe the same tip before either attempts CAS. This
// exercises the losing append as well as the winner, regardless of scheduling.
type qualificationContendingAnchor struct {
	*qualificationTestAnchor
	observed chan struct{}
	release  chan struct{}
}

func (a *qualificationContendingAnchor) Current(ctx context.Context, stream string) (string, error) {
	tip, err := a.qualificationTestAnchor.Current(ctx, stream)
	if err != nil {
		return "", err
	}
	select {
	case a.observed <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	select {
	case <-a.release:
		return tip, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

type qualificationFixture struct {
	c                         Config
	q                         *QualificationRecords
	anchor                    *qualificationTestAnchor
	rootPath, configPath, pin string
	countPath                 string
}

func newCodexQualificationFixture(t *testing.T, actionID string) qualificationFixture {
	t.Helper()
	dir := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	parentID, operationID := "qual-parent", actionID
	anchorPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tokenName := "AOK_QUALIFICATION_ANCHOR_TOKEN"
	t.Setenv(tokenName, "fixture-anchor-token")
	initial := strings.Repeat("a", 64)
	home := filepath.Join(dir, "codex-home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	countPath := filepath.Join(dir, "codex-starts.log")
	executable := filepath.Join(dir, "codex-fixture")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> " + shellQuote(countPath) + "\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 'codex-cli 0.162.1'; exit 0; fi\n" +
		"while IFS= read -r line; do\n" +
		"case \"$line\" in\n" +
		"*\\\"initialize\\\"*) echo '{\"id\":1,\"result\":{\"protocolVersion\":1}}' ;;\n" +
		"*\\\"account/rateLimits/read\\\"*) echo '{\"id\":2,\"result\":{\"rateLimitsByLimitId\":{\"codex\":{\"limitId\":\"codex\",\"primary\":{\"usedPercent\":20,\"windowDurationMins\":300,\"resetsAt\":2051222400}}}}}' ;;\n" +
		"esac\ndone\n"
	if err := os.WriteFile(executable, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	bin, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	binHash := sha256.Sum256(bin)
	sourceID := "qualification:" + usage.Digest(struct{ Parent, Operation string }{parentID, operationID})
	streamID := "qualification-usage-stream"
	collector := codex.Config{Executable: executable, ExecutableSHA256: hex.EncodeToString(binHash[:]), Version: codex.PinnedVersion, AccountRef: "codex-fixture-account", SourceID: sourceID, Home: home, Path: "/usr/bin:/bin", NativeAccountProofRef: "native-account-observation", NativeAccountProofSHA256: strings.Repeat("b", 64), HomeSHA256: strings.Repeat("c", 64), EnvironmentCensusSHA256: strings.Repeat("d", 64), UsageBindingID: "qualification-binding", UsageStreamID: streamID}
	binding := usage.Binding{ID: collector.UsageBindingID, PoolID: "codex-pool", Provider: "openai", Service: "codex-app", Host: "codex", HostVersion: codex.PinnedVersion, AuthMode: "chatgpt", SourceID: sourceID, StreamID: streamID, ConfigurationIDs: []string{"fixture-config"}, ConstraintsComplete: true}
	policy := usage.Policy{Version: usage.Version, Sources: []usage.SourceRule{{ID: sourceID, Host: "codex", HostVersion: codex.PinnedVersion, AdapterVersion: collectors.AdapterVersion, AuthModes: []string{"chatgpt"}, Metrics: []string{"percentage"}, Authority: "openai", MaxAgeSeconds: 120, CollectionAllowed: true}}}
	q := &QualificationRecords{Version: QualificationVersion, Purpose: "synthetic-conformance-only", ParentID: parentID, OperationID: operationID, Action: "codex-quota", IssuedAt: now.Format(time.RFC3339Nano), ExpiresAt: now.Add(3 * time.Minute).Format(time.RFC3339Nano), AnchorEndpoint: "https://anchor.fixture.test/v1/anchor", AnchorPublicKey: base64.StdEncoding.EncodeToString(anchorPub), AnchorTokenRef: "anchor-token", ExpectedTipSHA256: initial, OutputPath: filepath.Join(dir, "observation"), CodexQuota: &CodexQuotaConfig{Collector: collector, Binding: binding, Policy: policy, OutputPath: filepath.Join(dir, "quota-output")}}
	q.AnchorStreamID = qualificationStream(*q)
	c := Config{Version: ConfigVersion, Service: "qualification", SecretEnv: map[string]string{"anchor-token": tokenName}, Qualification: q}
	rootPath, configPath, pin := writeQualificationEnvelope(t, c, now)
	return qualificationFixture{c: c, q: q, anchor: &qualificationTestAnchor{tip: initial}, rootPath: rootPath, configPath: configPath, pin: pin, countPath: countPath}
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func writeQualificationEnvelope(t *testing.T, c Config, now time.Time) (string, string, string) {
	t.Helper()
	dir := t.TempDir()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(dir, "root.pub")
	if err := os.WriteFile(rootPath, pub, 0600); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	e := Envelope{Version: EnvelopeVersion, IssuedAt: now.Add(-time.Minute).Format(time.RFC3339Nano), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339Nano), Payload: payload}
	e.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, SigningBytes(e)))
	body, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "bootstrap.json")
	if err := os.WriteFile(configPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(pub)
	return rootPath, configPath, hex.EncodeToString(h[:])
}

func prepareQualificationRun(t *testing.T, f qualificationFixture) *WorkerRun {
	t.Helper()
	_, c, _, err := loadAndVerify(f.configPath, f.rootPath, f.pin, true, time.Now().UTC())
	if err != nil {
		t.Fatalf("root-signed qualification config: %v", err)
	}
	if err := validateServiceConfig(c, time.Now().UTC()); err != nil {
		t.Fatalf("qualification service validation: %v", err)
	}
	run, err := buildQualification(c, f.anchor, f.configPath, f.rootPath)
	if err != nil {
		t.Fatalf("build qualification worker: %v", err)
	}
	return run
}

func runQualificationFixture(t *testing.T, f qualificationFixture, ctx context.Context) (int, string, string) {
	t.Helper()
	run := prepareQualificationRun(t, f)
	var out, errout strings.Builder
	code := run.Run(ctx, &out, &errout)
	return code, out.String(), errout.String()
}

func TestPinnedQualificationRunsObservationWithoutThirteenProofSet(t *testing.T) {
	f := newCodexQualificationFixture(t, "qual-op-observe")
	if f.c.Authority != nil || f.c.Worker != nil || f.c.Qualification.Host != nil || f.c.Qualification.GitHubWrite != nil {
		t.Fatal("qualification fixture accidentally depends on authority or host pilot proofs")
	}
	code, out, stderr := runQualificationFixture(t, f, context.Background())
	if code != 0 {
		t.Fatalf("qualification failed: %s %s", out, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"observationOnly", "authorizationGranted", "executionReady", "pilotAdmission", "productionEligible"} {
		if k == "observationOnly" {
			if result[k] != true {
				t.Fatalf("observation marker missing: %+v", result)
			}
		} else if result[k] != false {
			t.Fatalf("qualification granted %s: %+v", k, result)
		}
	}
	if result["liveQualification"] != "independent-review-required" || result["status"] != "observed" {
		t.Fatalf("qualification auto-promoted: %+v", result)
	}
	if f.anchor.tip == f.q.ExpectedTipSHA256 || f.anchor.advanceCalls != 1 {
		t.Fatalf("one-use anchor not consumed: %+v", f.anchor)
	}
	files, err := os.ReadDir(f.q.OutputPath)
	if err != nil || len(files) < 2 {
		t.Fatalf("observation assets missing: %v %v", files, err)
	}
}

func TestQualificationPinnedRootTamperAndExpiryFailBeforeAction(t *testing.T) {
	f := newCodexQualificationFixture(t, "qual-op-invalid")
	if _, err := BuildQualification(f.configPath, f.rootPath, "", time.Now().UTC()); err == nil {
		t.Fatal("empty root pin accepted")
	}
	if err := os.WriteFile(f.configPath, []byte("tampered"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildQualification(f.configPath, f.rootPath, f.pin, time.Now().UTC()); err == nil {
		t.Fatal("tampered qualification envelope accepted")
	}
	if _, err := os.Stat(f.countPath); !os.IsNotExist(err) {
		t.Fatalf("host was launched before enrollment validation: %v", err)
	}
	if f.anchor.currentCalls != 0 {
		t.Fatal("invalid envelope accessed anchor")
	}

	f = newCodexQualificationFixture(t, "qual-op-expired")
	f.q.ExpiresAt = time.Now().UTC().Add(-time.Second).Format(time.RFC3339Nano)
	f.q.AnchorStreamID = qualificationStream(*f.q)
	_, config, _, err := loadAndVerify(f.configPath, f.rootPath, f.pin, true, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	config.Qualification = f.q
	if validateQualification(config, time.Now().UTC()) == nil {
		t.Fatal("expired operator grant accepted")
	}
	if f.anchor.currentCalls != 0 {
		t.Fatal("expired grant reached anchor")
	}
}

func TestQualificationSameParentOperationRenewalAndReplayCannotRepeatIO(t *testing.T) {
	f := newCodexQualificationFixture(t, "qual-op-once")
	code, _, stderr := runQualificationFixture(t, f, context.Background())
	if code != 0 {
		t.Fatalf("first qualification failed: %s", stderr)
	}
	firstCount, err := os.ReadFile(f.countPath)
	if err != nil {
		t.Fatal(err)
	}
	f.q.ExpiresAt = time.Now().UTC().Add(2 * time.Minute).Format(time.RFC3339Nano)
	f.q.IssuedAt = time.Now().UTC().Format(time.RFC3339Nano)
	f.q.OutputPath = filepath.Join(filepath.Dir(f.q.OutputPath), "renewed-observation")
	f.c.Qualification = f.q
	// Re-sign the renewed envelope with the fixture's new paths and current expiry.
	f.rootPath, f.configPath, f.pin = writeQualificationEnvelope(t, f.c, time.Now().UTC())
	_, config, _, err := loadAndVerify(f.configPath, f.rootPath, f.pin, true, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	run, err := buildQualification(config, f.anchor, f.configPath, f.rootPath)
	if err != nil {
		t.Fatal(err)
	}
	var out, errout strings.Builder
	if code = run.Run(context.Background(), &out, &errout); code == 0 {
		t.Fatal("renewal/replay of consumed parent+operation succeeded")
	}
	secondCount, err := os.ReadFile(f.countPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstCount) != string(secondCount) {
		t.Fatalf("renewed authorization caused more Codex I/O: %q => %q", firstCount, secondCount)
	}
	f.q.ExpectedTipSHA256 = qualificationConsumeDigest(*f.q)
	if _, err := buildQualification(f.c, f.anchor); err == nil {
		t.Fatal("renewal adopted consumed tip and reopened the same operation")
	}
}

func TestQualificationParallelSameOperationHasOneAnchorWinner(t *testing.T) {
	f := newCodexQualificationFixture(t, "qual-op-parallel")
	anchor := &qualificationContendingAnchor{qualificationTestAnchor: f.anchor, observed: make(chan struct{}, 2), release: make(chan struct{})}
	// Two independently assembled invocations race on the same immutable stream tip.
	outputs := []string{filepath.Join(filepath.Dir(f.q.OutputPath), "parallel-a"), filepath.Join(filepath.Dir(f.q.OutputPath), "parallel-b")}
	runs := make([]*WorkerRun, 2)
	for i := range runs {
		q := *f.q
		q.OutputPath = outputs[i]
		c := f.c
		c.Qualification = &q
		rootPath, configPath, pin := writeQualificationEnvelope(t, c, time.Now().UTC())
		_, decoded, _, err := loadAndVerify(configPath, rootPath, pin, true, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		var buildErr error
		runs[i], buildErr = buildQualification(decoded, anchor, configPath, rootPath)
		if buildErr != nil {
			t.Fatal(buildErr)
		}
	}
	codes := make(chan int, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	for i, run := range runs {
		wg.Add(1)
		go func(i int, run *WorkerRun) {
			defer wg.Done()
			codes <- run.Run(ctx, io.Discard, io.Discard)
		}(i, run)
	}
	for range runs {
		select {
		case <-anchor.observed:
		case <-ctx.Done():
			t.Fatal("parallel callers did not both reach the same-tip read")
		}
	}
	close(anchor.release)
	wg.Wait()
	close(codes)
	wins := 0
	for code := range codes {
		if code == 0 {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("parallel one-use qualification winners=%d", wins)
	}
	if f.anchor.advanceCalls != 2 || f.anchor.advanceCommits != 1 || f.anchor.tip != qualificationConsumeDigest(*f.q) {
		t.Fatalf("same-tip contenders: attempts=%d committed=%d tip=%s", f.anchor.advanceCalls, f.anchor.advanceCommits, f.anchor.tip)
	}
	if raw, err := os.ReadFile(f.countPath); err != nil || strings.Count(string(raw), "--version") != 1 {
		t.Fatalf("parallel loser performed host IO: %q (%v)", raw, err)
	}
}

func TestQualificationLostAnchorResponseAndCanceledContextPerformNoAction(t *testing.T) {
	f := newCodexQualificationFixture(t, "qual-op-lost")
	f.anchor.loseAdvanceResponse = true
	code, _, _ := runQualificationFixture(t, f, context.Background())
	if code == 0 {
		t.Fatal("lost anchor response was treated as consumed success")
	}
	if _, err := os.Stat(f.countPath); !os.IsNotExist(err) {
		t.Fatalf("host I/O began after uncertain consume: %v", err)
	}
	f.anchor.loseAdvanceResponse = false
	code, _, _ = runQualificationFixture(t, f, context.Background())
	if code == 0 {
		t.Fatal("lost consume was retried")
	}
	if _, err := os.Stat(f.countPath); !os.IsNotExist(err) {
		t.Fatalf("replay after lost response performed host I/O: %v", err)
	}

	f = newCodexQualificationFixture(t, "qual-op-canceled")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, _, _ = runQualificationFixture(t, f, ctx)
	if code == 0 || f.anchor.advanceCalls != 0 {
		t.Fatal("canceled qualification consumed anchor or succeeded")
	}
	if _, err := os.Stat(f.countPath); !os.IsNotExist(err) {
		t.Fatalf("canceled qualification launched host: %v", err)
	}
}

func TestQualificationScopeOutputOverlapBlocksBeforeAnchor(t *testing.T) {
	f := newCodexQualificationFixture(t, "qual-op-overlap")
	f.q.OutputPath = f.q.CodexQuota.Collector.Home
	f.c.Qualification = f.q
	f.rootPath, f.configPath, f.pin = writeQualificationEnvelope(t, f.c, time.Now().UTC())
	_, config, _, err := loadAndVerify(f.configPath, f.rootPath, f.pin, true, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	run, err := buildQualification(config, f.anchor)
	if err != nil {
		t.Fatal(err)
	}
	code := run.Run(context.Background(), io.Discard, io.Discard)
	if code == 0 || f.anchor.currentCalls != 0 || f.anchor.advanceCalls != 0 {
		t.Fatalf("overlapping output path was not rejected before consume: code=%d anchor=%+v", code, f.anchor)
	}
}

func TestHostQualificationRequiresPositiveExplicitMatrixDemand(t *testing.T) {
	f := newCodexQualificationFixture(t, "qual-op-host-demand")
	q := *f.q
	q.Action = "host-conformance"
	q.CodexQuota = nil
	now := time.Now().UTC().Truncate(time.Second)
	h := &HostConformance{
		DemandScope: "entire-matrix-upper-bound", DemandEvidenceSHA256: strings.Repeat("1", 64),
		MaximumNativeTurns: 10, MaximumTurnSeconds: 30, PolicyPath: filepath.Join(t.TempDir(), "policy.json"), PolicySHA256: strings.Repeat("2", 64),
		AccountingPath: filepath.Join(t.TempDir(), "accounting.json"), AccountingAuthority: "fixture-authority",
		UsagePolicy: usage.Policy{Version: usage.Version, Sources: []usage.SourceRule{{ID: "fixture", Host: "codex", HostVersion: codex.PinnedVersion, AdapterVersion: collectors.AdapterVersion, AuthModes: []string{"chatgpt"}, Metrics: []string{"percentage"}, Authority: "openai", MaxAgeSeconds: 120}}},
		Reserve:     accounting.ReserveRequest{ID: q.OperationID, AttemptID: q.OperationID, LeaseUntil: now.Add(4 * time.Minute).Format(time.RFC3339Nano), Demand: usage.Demand{Version: usage.Version, ConfigurationID: "fixture-config", ProfileSHA256: strings.Repeat("3", 64), Source: "operator-upper-bound", ValidUntil: now.Add(4 * time.Minute).Format(time.RFC3339Nano), Amounts: []usage.Amount{{ConstraintID: strings.Repeat("4", 64), Generation: "all", Unit: "tokens", Amount: "0"}}}},
	}
	q.Host = h
	c := f.c
	c.Qualification = &q
	if err := validateQualification(c, now); err == nil {
		t.Fatal("zero-token upper bound accepted for native matrix qualification")
	}
	// An empty bound is also unknown, even though there is no measured usage to
	// infer a safer value from.
	h.Reserve.Demand.Amounts = nil
	if err := validateQualification(c, now); err == nil {
		t.Fatal("unknown/empty matrix upper bound accepted")
	}
	if f.anchor.currentCalls != 0 || f.anchor.advanceCalls != 0 {
		t.Fatalf("invalid host demand reached anchor: %+v", f.anchor)
	}
}

func TestHostQualificationProfileMismatchIsRejectedBeforeAccounting(t *testing.T) {
	f := newCodexQualificationFixture(t, "qual-op-host-profile")
	q := *f.q
	q.Action = "host-conformance"
	q.CodexQuota = nil
	now := time.Now().UTC().Truncate(time.Second)
	q.ExpiresAt = now.Add(3 * time.Minute).Format(time.RFC3339Nano)
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	executable := "/bin/sh"
	exeBytes, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	exeHash := sha256.Sum256(exeBytes)
	model := "fixture-model"
	policy := hostbridge.Policy{Version: hostbridge.Version, Host: "codex", HostVersion: codex.PinnedVersion, Executable: executable, ExecutableSHA256: hex.EncodeToString(exeHash[:]), AuthMode: "native-login", AccountScope: "synthetic", WorkspaceRoots: []string{root}, Workspace: workspace, Models: []string{model}, TimeoutSeconds: 30, Settings: json.RawMessage(`{}`), CodexConfig: map[string]json.RawMessage{}}
	policyRaw, err := json.Marshal(policy)
	if err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(root, "policy.json")
	if err := os.WriteFile(policyPath, policyRaw, 0600); err != nil {
		t.Fatal(err)
	}
	accountingPath := filepath.Join(root, "must-not-create.db")
	h := &HostConformance{PolicyPath: policyPath, PolicySHA256: usage.Hash(policyRaw), AccountingPath: accountingPath, AccountingAuthority: "fixture-authority", UsagePolicy: usage.Policy{Version: usage.Version, Sources: []usage.SourceRule{{ID: "fixture", Host: "codex", HostVersion: codex.PinnedVersion, AdapterVersion: collectors.AdapterVersion, AuthModes: []string{"chatgpt"}, Metrics: []string{"percentage"}, Authority: "openai", MaxAgeSeconds: 120}}}, DemandScope: "entire-matrix-upper-bound", DemandEvidenceSHA256: strings.Repeat("1", 64), MaximumNativeTurns: 10, MaximumTurnSeconds: 30}
	h.Reserve = accounting.ReserveRequest{ID: q.OperationID, AttemptID: q.OperationID, LeaseUntil: now.Add(4 * time.Minute).Format(time.RFC3339Nano), Demand: usage.Demand{Version: usage.Version, ConfigurationID: modelcatalog.ConfigurationID(policy.Configuration), ProfileSHA256: strings.Repeat("3", 64), Source: "operator-upper-bound", ValidUntil: now.Add(4 * time.Minute).Format(time.RFC3339Nano), Amounts: []usage.Amount{{ConstraintID: strings.Repeat("4", 64), Generation: "all", Unit: "tokens", Amount: "1"}}}}
	q.Host = h
	c := f.c
	c.Qualification = &q
	if err := validateQualification(c, now); err != nil {
		t.Fatalf("valid-shape host enrollment rejected too early: %v", err)
	}
	if _, err := qualifyHost(context.Background(), q); err == nil {
		t.Fatal("host policy/profile mismatch accepted")
	}
	if _, err := os.Stat(accountingPath); !os.IsNotExist(err) {
		t.Fatalf("accounting ledger touched before host policy/profile binding: %v", err)
	}
	if f.anchor.currentCalls != 0 || f.anchor.advanceCalls != 0 {
		t.Fatalf("invalid host policy reached anchor: %+v", f.anchor)
	}
}

// Compile-time shape assertion: the qualification consumes only the narrow
// monotonic anchor interface, not a task issuer or the 13-proof pilot authority.
var _ authority.Anchor = (*qualificationTestAnchor)(nil)

func ExamplequalificationStream() {
	q := QualificationRecords{ParentID: "parent", OperationID: "operation"}
	fmt.Println(strings.HasPrefix(qualificationStream(q), "qualification-"))
	// Output: true
}
