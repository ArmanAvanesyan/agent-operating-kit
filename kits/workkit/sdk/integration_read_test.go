package sdk

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	i "github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/api/integrationv1"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/internal/engine"
	"github.com/ArmanAvanesyan/agent-operating-kit/kits/workkit/readsource"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type p7ReadTransport struct {
	calls int
	body  []byte
}

func (t *p7ReadTransport) Do(ctx context.Context, _ readsource.Source, _ readsource.Request) (readsource.Response, error) {
	t.calls++
	if e := ctx.Err(); e != nil {
		return readsource.Response{}, e
	}
	return readsource.Response{Status: 200, Body: t.body}, nil
}
func TestP7OperationalEndpointAndAggregateBudgets(t *testing.T) {
	source := readsource.Source{Provider: "azure-devops", Origin: "https://dev.azure.com", APIVersion: "7.1"}
	req := readsource.Request{Method: "GET", Kind: "items"}
	tr := &p7ReadTransport{body: []byte(`{}`)}
	revoked := false
	request := i.OperationalReadRequest{AllowedEndpointSHA256: []string{OperationalEndpointSHA(source, req)}, Limits: DefaultIntegrationLimits()}
	guarded := guardedOperationalRead{tr, request, func(context.Context) error {
		if revoked {
			return errors.New("revoked")
		}
		return nil
	}}
	budget := &integrationBudget{limits: DefaultIntegrationLimits()}
	budget.limits.Requests = 1
	ctx := context.WithValue(context.Background(), integrationBudgetKey{}, budget)
	if _, e := guarded.Do(ctx, source, req); e != nil {
		t.Fatal(e)
	}
	if _, e := guarded.Do(ctx, source, req); e == nil || tr.calls != 1 {
		t.Fatal("aggregate requests bypass", e, tr.calls)
	}
	req.Method = "POST"
	if _, e := guarded.Do(context.Background(), source, req); e == nil {
		t.Fatal("unenrolled endpoint")
	}
	req.Method = "GET"
	source.Origin = "https://other.invalid"
	if _, e := guarded.Do(context.Background(), source, req); e == nil {
		t.Fatal("origin substitution")
	}
	source.Origin = "https://dev.azure.com"
	revoked = true
	if _, e := guarded.Do(context.Background(), source, req); e == nil {
		t.Fatal("revoked read")
	}
	revoked = false
	guarded.request.Limits.ResponseBytes = 1
	if _, e := guarded.Do(context.Background(), source, req); e == nil {
		t.Fatal("response ceiling")
	}
	budget = &integrationBudget{limits: DefaultIntegrationLimits()}
	budget.limits.CaptureBytes = 1
	guarded.request = request
	if _, e := guarded.Do(context.WithValue(context.Background(), integrationBudgetKey{}, budget), source, req); e == nil {
		t.Fatal("cycle byte ceiling")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := guarded.Do(ctx, source, req); e == nil {
		t.Fatal("cancel ignored")
	}
	if _, e := New(nil).CaptureOperational(context.Background(), "missing", "missing", "missing", "missing", "", "missing"); e == nil {
		t.Fatal("default operational bootstrap")
	}
}
func TestP7SharedOperationSessionPreservesVersionedAuthority(t *testing.T) {
	q, _ := operationFixture(t, "github")
	p, e := PrepareOperation(context.Background(), q.Out)
	if e != nil {
		t.Fatal(e)
	}
	_, en, key, raw := p7authority(t, p.Digest(), "operation-reconcile")
	en.ProcessID = os.Getpid()
	en.ProcessStart, e = CurrentProcessStart()
	if e != nil {
		t.Fatal(e)
	}
	en.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, IntegrationSigningBytes(en)))
	driver := &operationTestDriver{}
	session, e := NewIntegrationOperationSession(context.Background(), p, raw, engine.Encode(en), driver)
	if e != nil {
		t.Fatal(e)
	}
	if session.enrollment.Version != i.Version || session.enrollment.Store.Version != i.JournalVersion {
		t.Fatal("v2 authority relabeled")
	}
	if _, e = session.Run(context.Background()); e != nil {
		t.Fatal(e)
	}
	if _, e = session.Run(context.Background()); e == nil {
		t.Fatal("shared session reuse")
	}
	if _, e = NewOperationSession(context.Background(), p, raw, engine.Encode(en), driver); e == nil {
		t.Fatal("cross-version grant")
	}
	en.Purpose = "operational-read"
	en.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, IntegrationSigningBytes(en)))
	if _, e = NewIntegrationOperationSession(context.Background(), p, raw, engine.Encode(en), driver); e == nil {
		t.Fatal("read purpose used for operation")
	}
}

type p7OperationalBackend struct {
	transport readsource.Transport
	revoked   bool
}

func (b *p7OperationalBackend) Current(ctx context.Context, _ i.OperationalReadRequest, _ i.Policy, _ i.Enrollment) error {
	if b.revoked {
		return errors.New("revoked")
	}
	return ctx.Err()
}
func (b *p7OperationalBackend) Transport(ctx context.Context, _ i.OperationalReadRequest, _ []byte, guard func(context.Context) error) (readsource.Transport, error) {
	if e := guard(ctx); e != nil {
		return nil, e
	}
	return b.transport, nil
}

type p7Pages struct {
	manifest readsource.Manifest
	root     string
	calls    int
}

func (p *p7Pages) Do(ctx context.Context, s readsource.Source, q readsource.Request) (readsource.Response, error) {
	p.calls++
	for _, v := range p.manifest.Pages {
		if v.Request.Kind == q.Kind && v.Request.Path == q.Path {
			raw, e := os.ReadFile(filepath.Join(p.root, v.Asset.Path))
			return readsource.Response{Status: v.Status, Headers: v.Headers, Body: raw}, e
		}
	}
	return readsource.Response{}, errors.New("unexpected typed request")
}
func TestP7OperationalCaptureKeepsP2Unqualified(t *testing.T) {
	ctx := context.Background()
	root := "../../../internal/tracker/testdata/offline/azure-devops"
	raw, e := os.ReadFile(filepath.Join(root, "manifest.json"))
	if e != nil {
		t.Fatal(e)
	}
	var m readsource.Manifest
	if DecodeRead(raw, &m) != nil {
		t.Fatal("manifest")
	}
	cfg := readConfig(t, "operational", m.Source)
	_, configuration, e := writeReadConfig(t, cfg)
	if e != nil {
		t.Fatal(e)
	}
	request := i.OperationalReadRequest{Version: i.Version, Kind: "OperationalReadRequest", RouteID: "route", ConfigurationSHA256: strings.Repeat("b", 64), ConnectionSHA256: SHA256(cfg), Principal: m.Source.PrincipalPseudonym, AuthenticationMode: m.Source.Auth.Mode, Origin: m.Source.Origin, QuerySHA256: SHA256(engine.Encode(m.Source.Scope)), Limits: DefaultIntegrationLimits(), AllowedEndpointSHA256: []string{}}
	for _, page := range m.Pages {
		request.AllowedEndpointSHA256 = append(request.AllowedEndpointSHA256, OperationalEndpointSHA(m.Source, page.Request))
	}
	dir := readTestDir(t)
	req := p7write(t, dir, "request.json", request)
	p, en, key, praw := p7authority(t, SHA256(engine.Encode(request)), "operational-read")
	_ = p
	en.OutputPath = filepath.Join(dir, "capture")
	en.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(key, IntegrationSigningBytes(en)))
	policy := filepath.Join(dir, "policy.json")
	os.WriteFile(policy, praw, 0600)
	enrol := p7write(t, dir, "enrollment.json", en)
	pages := &p7Pages{manifest: m, root: root}
	b := &p7OperationalBackend{transport: pages}
	svc := New(nil).WithOperationalReadBackend(b)
	result, e := svc.CaptureOperational(ctx, req, configuration, policy, enrol, "", en.OutputPath)
	if e != nil {
		t.Fatal(e)
	}
	if !result.Complete || result.LiveQualification != "not-established" || result.AuthorizationGranted || result.ExecutionReady || result.Transport.Kind != "captured-source" {
		t.Fatal("qualification promotion", result)
	}
	if _, e = LoadReadBundle(ctx, filepath.Join(en.OutputPath, "capture")); e != nil {
		t.Fatal(e)
	}
	var receipt i.OperationalReadObservation
	obs, e := os.ReadFile(filepath.Join(en.OutputPath, "operational-read.json"))
	if e != nil || DecodeRead(obs, &receipt) != nil || receipt.RequestSHA256 != SHA256(engine.Encode(request)) {
		t.Fatal("missing separate read evidence", e)
	}
	calls := pages.calls
	b.revoked = true
	if _, e = svc.CaptureOperational(ctx, req, configuration, policy, enrol, "", en.OutputPath); e == nil || pages.calls != calls {
		t.Fatal("revoked transport called")
	}
}
