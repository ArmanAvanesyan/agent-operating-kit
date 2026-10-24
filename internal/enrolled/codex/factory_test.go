package codex

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage"
	"github.com/ArmanAvanesyan/agent-operating-kit/internal/usage/collectors"
)

type testAuthority struct {
	mu     sync.Mutex
	scopes []Scope
	deny   bool
}

func (a *testAuthority) Check(_ context.Context, s Scope) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.scopes = append(a.scopes, s)
	if a.deny {
		return context.Canceled
	}
	return nil
}

type testCapture struct {
	mu        sync.Mutex
	completed []CaptureResult
}

func (c *testCapture) Acquire(_ context.Context, account, source string) (CaptureGrant, error) {
	return CaptureGrant{Ordinal: 42, LeaseID: "lease-fixture", Revision: 42, ValidUntil: time.Now().Add(time.Minute), AccountRef: account, SourceID: source, ProofRef: "proof-fixture", ProofSHA256: strings.Repeat("a", 64)}, nil
}
func (c *testCapture) Complete(_ context.Context, _ CaptureGrant, result CaptureResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.completed = append(c.completed, result)
	return nil
}

func fixtureFactory(t *testing.T, auth *testAuthority, capture *testCapture) *Factory {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "codex-fixture")
	// This script exercises the real stdio boundary without a Codex binary,
	// account, credential file, network connection, or model invocation.
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--version\" ]; then echo 'codex-cli 0.162.1'; exit 0; fi\n" +
		"while IFS= read -r line; do\n" +
		"case \"$line\" in\n" +
		"*\\\"initialize\\\"*) echo '{\"id\":1,\"result\":{\"protocolVersion\":1}}' ;;\n" +
		"*\\\"account/rateLimits/read\\\"*) echo '{\"id\":2,\"result\":{\"rateLimitsByLimitId\":{\"codex\":{\"limitId\":\"codex\",\"primary\":{\"usedPercent\":20,\"windowDurationMins\":300,\"resetsAt\":2051222400}}}}}' ;;\n" +
		"esac\n" +
		"done\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(data)
	cfg := Config{Executable: bin, ExecutableSHA256: hex.EncodeToString(digest[:]), Version: PinnedVersion, AccountRef: "acct-pseudonym", SourceID: "source-fixture", Home: dir, Path: "/usr/bin:/bin", NativeAccountProofRef: "native-account-fixture", NativeAccountProofSHA256: strings.Repeat("b", 64), HomeSHA256: strings.Repeat("c", 64), EnvironmentCensusSHA256: strings.Repeat("d", 64), UsageBindingID: "usage-binding-fixture", UsageStreamID: "usage-stream-fixture"}
	got, err := New(cfg, auth, capture)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestFactoryPerformsBoundedReadOnlyAppServerCapture(t *testing.T) {
	auth := &testAuthority{}
	capture := &testCapture{}
	f := fixtureFactory(t, auth, capture)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observation, payload, err := f.Collect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"rateLimitsByLimitId"`) {
		t.Fatalf("unexpected quota payload: %s", payload)
	}
	if observation.Ordinal != 42 || observation.Revision != 42 || observation.Result.Status != "complete" {
		t.Fatalf("capture observation was not bound to returned payload: %+v", observation)
	}
	if len(auth.scopes) != 2 || auth.scopes[0].Ordinal != 42 || auth.scopes[0].Revision != 42 || auth.scopes[0].AccountRef != "acct-pseudonym" {
		t.Fatalf("authority did not receive leased scope: %+v", auth.scopes)
	}
	if len(capture.completed) != 1 || capture.completed[0].Status != "complete" || capture.completed[0].Bytes == 0 || capture.completed[0].SHA256 == "" {
		t.Fatalf("capture lease not completed with evidence: %+v", capture.completed)
	}
}

func TestFactoryFailsClosedWhenAuthorityDeniesLaunch(t *testing.T) {
	auth := &testAuthority{deny: true}
	capture := &testCapture{}
	f := fixtureFactory(t, auth, capture)
	if _, err := f.Open(context.Background()); err == nil {
		t.Fatal("authority denial was ignored")
	}
	if len(auth.scopes) != 1 {
		t.Fatalf("expected denial before binary probe or launch, got %d checks", len(auth.scopes))
	}
	if len(capture.completed) != 1 || capture.completed[0].Status != "uncertain" {
		t.Fatalf("failed launch did not retain uncertain capture lease: %+v", capture.completed)
	}
}

func TestCollectUsageNormalizesOrdinalAndIncludesCaptureAsset(t *testing.T) {
	auth := &testAuthority{}
	capture := &testCapture{}
	f := fixtureFactory(t, auth, capture)
	binding := usage.Binding{ID: "usage-binding-fixture", PoolID: "codex-pool", Provider: "openai", Service: "codex-app", Host: "codex", HostVersion: PinnedVersion, AuthMode: "chatgpt", SourceID: "source-fixture", StreamID: "usage-stream-fixture", ConfigurationIDs: []string{"cfg"}, ConstraintsComplete: true}
	policy := usage.Policy{Version: usage.Version, Sources: []usage.SourceRule{{ID: binding.SourceID, Host: "codex", HostVersion: PinnedVersion, AdapterVersion: collectors.AdapterVersion, AuthModes: []string{"chatgpt"}, Metrics: []string{"percentage"}, Authority: "openai", MaxAgeSeconds: 60, CollectionAllowed: true}}}
	asOf := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	got, err := f.CollectUsage(context.Background(), binding, policy, asOf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Report.Status != "valid" || len(got.Export.Quotas) != 1 || got.Export.Quotas[0].Revision != 42 {
		t.Fatalf("serialized capture ordinal was not normalized: %+v %+v", got.Report, got.Export.Quotas)
	}
	if got.Export.Quotas[0].Origin.ReceivedAt != asOf.Format(time.RFC3339Nano) {
		t.Fatalf("caller asOf was renewed: %s", got.Export.Quotas[0].Origin.ReceivedAt)
	}
	if err := usage.ValidateExport(got.Export, policy); err != nil {
		t.Fatalf("normalized export invalid: %v", err)
	}
	if len(got.Export.Evidence) != 1 {
		t.Fatalf("capture metadata reference missing: %+v", got.Export.Evidence)
	}
	asset := got.Export.Evidence[0]
	raw, ok := got.Assets[asset.Path]
	if !ok || usage.Hash(raw) != asset.SHA256 {
		t.Fatalf("capture metadata asset missing or unhashed: %+v", asset)
	}
	var metadata struct {
		Ordinal       uint64 `json:"ordinal"`
		Revision      int64  `json:"revision"`
		AccountRef    string `json:"accountRef"`
		SourceID      string `json:"sourceId"`
		ProofSHA256   string `json:"proofSha256"`
		PayloadSHA256 string `json:"payloadSha256"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.Ordinal != 42 || metadata.Revision != 42 || metadata.AccountRef != "acct-pseudonym" || metadata.SourceID != binding.SourceID || metadata.ProofSHA256 != strings.Repeat("a", 64) || metadata.PayloadSHA256 == "" {
		t.Fatalf("capture metadata did not retain authority binding: %+v", metadata)
	}
	if bytes.Contains(raw, []byte("rateLimits")) || bytes.Contains(got.Assets["usage.json"], []byte("rateLimits")) {
		t.Fatal("raw native payload leaked into returned usage assets")
	}
}

func TestCollectUsageRejectsMismatchedBindingBeforeOpeningAppServer(t *testing.T) {
	capture := &testCapture{}
	f := fixtureFactory(t, &testAuthority{}, capture)
	binding := usage.Binding{ID: "other-binding", PoolID: "pool", Provider: "openai", Service: "codex-app", Host: "codex", HostVersion: PinnedVersion, AuthMode: "chatgpt", SourceID: "source-fixture", StreamID: "usage-stream-fixture"}
	policy := usage.Policy{Version: usage.Version, Sources: []usage.SourceRule{{ID: binding.SourceID, Host: "codex", HostVersion: PinnedVersion, AdapterVersion: collectors.AdapterVersion, AuthModes: []string{"chatgpt"}, Metrics: []string{"percentage"}, Authority: "openai", MaxAgeSeconds: 60, CollectionAllowed: true}}}
	if _, err := f.CollectUsage(context.Background(), binding, policy, time.Now()); err == nil {
		t.Fatal("caller-selected usage binding accepted")
	}
	if len(capture.completed) != 0 {
		t.Fatalf("mismatched usage binding opened capture: %+v", capture.completed)
	}
}

func TestAppServerRequestSurfaceIsReadOnlyAndNonRefreshing(t *testing.T) {
	for _, request := range []string{
		`{"id":3,"method":"turn/start","params":{}}`,
		`{"id":2,"method":"account/rateLimits/read","params":{"refreshToken":true}}`,
		`{"id":2,"method":"account/rateLimits/read","params":{"refreshToken":false,"force":true}}`,
	} {
		if allowedRequest([]byte(request)) {
			t.Fatalf("accepted out-of-scope RPC: %s", request)
		}
	}
	for _, request := range []string{
		`{"id":1,"method":"initialize","params":{}}`,
		`{"method":"initialized"}`,
		`{"id":2,"method":"account/rateLimits/read","params":{"refreshToken":false}}`,
	} {
		if !allowedRequest([]byte(request)) {
			t.Fatalf("rejected collector RPC: %s", request)
		}
	}
}
